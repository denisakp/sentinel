package cli

// Backup Executor factory (spec 038 Sub-PR K, FR-010). Single construction
// point translating config.BackupJob (+ resolved storage params and engine
// args) into a domain backup.Executor + backup.Job. Used by every backup
// call site: CLI single jobs, auto-discovery, and the scheduled path (which
// flows through executeBackupJobWithMode).

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/denisakp/sentinel/internal/adapters/crypto"
	dbprobe "github.com/denisakp/sentinel/internal/adapters/db_probe"
	dumpmongo "github.com/denisakp/sentinel/internal/adapters/dump/mongo"
	manifest "github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/notifier"
	"github.com/denisakp/sentinel/internal/adapters/restore/incremental/mysqlbinlog"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	backup "github.com/denisakp/sentinel/internal/domain/backup"
	"github.com/denisakp/sentinel/internal/ports"
)

// verifyIncrementalArtifactHash is the incremental-hash verification seam
// (overridden in tests). Wired into backup.Job.VerifyArtifactHash.
var verifyIncrementalArtifactHash = manifest.VerifyBackupHash

// dumpBuilderFunc adapts a closure to ports.DumpBuilder (used for the
// BackupAll auto-discovery "single" strategy, which has no port-side Build).
type dumpBuilderFunc func(ctx ports.BuildContext) (ports.BuildResult, error)

func (f dumpBuilderFunc) Build(ctx ports.BuildContext) (ports.BuildResult, error) { return f(ctx) }

// monitorBackedRecorder is the per-call history recorder handed to the
// Executor. It preserves the pre-carve open-per-operation monitor lifecycle
// and always feeds the incremental metrics observer, even without a history
// DB. Only the three methods the Executor touches are implemented; the
// embedded nil interface covers the rest of ports.Recorder at compile time.
type monitorBackedRecorder struct {
	ports.Recorder
	historyDBPath string
	jobName       string
}

func (r *monitorBackedRecorder) RecordExecution(ctx context.Context, exec *ports.Execution) error {
	monitor.ObserveIncrementalBackup(r.jobName, exec)
	if strings.TrimSpace(r.historyDBPath) == "" {
		return nil
	}
	mon, err := monitor.NewMonitor(r.historyDBPath)
	if err != nil {
		return err
	}
	defer mon.Close()
	return mon.RecordExecution(ctx, exec)
}

func (r *monitorBackedRecorder) RecordSecurityInfo(ctx context.Context, id, hashAlgo, hashValue, plaintextHash, manifestPath string, encrypted bool, keyHint string) error {
	if strings.TrimSpace(r.historyDBPath) == "" {
		return nil
	}
	mon, err := monitor.NewMonitor(r.historyDBPath)
	if err != nil {
		return err
	}
	defer mon.Close()
	return mon.RecordSecurityInfo(ctx, id, hashAlgo, hashValue, plaintextHash, manifestPath, encrypted, keyHint)
}

func (r *monitorBackedRecorder) ListExecutions(ctx context.Context, filter *ports.Filter, limit, offset int) ([]ports.Execution, error) {
	if strings.TrimSpace(r.historyDBPath) == "" {
		return nil, nil
	}
	mon, err := monitor.NewMonitor(r.historyDBPath)
	if err != nil {
		return nil, err
	}
	defer mon.Close()
	return mon.ListExecutions(ctx, filter, limit, offset)
}

// backupExecution bundles a constructed Executor + translated Job.
type backupExecution struct {
	exec *backup.Executor
	job  backup.Job
	// notifWarn carries a dispatcher construction failure; non-fatal,
	// printed by the caller after Run (pre-carve parity).
	notifWarn error
}

// NewBackupExecutorFromConfig constructs the domain backup Executor and Job
// for one configured backup job. engineOpts/dumps may be nil when only the
// post-dump pipeline is exercised (applyBackupSecurity wrapper).
func NewBackupExecutorFromConfig(
	cfg *config.Configuration,
	job config.BackupJob,
	storageParams *storage.Params,
	scheduled, forceFull bool,
	engineOpts ports.EngineOptions,
	dumps ports.DumpBuilder,
) (*backupExecution, error) {
	historyPath := ""
	if cfg != nil {
		historyPath = cfg.HistoryDBPath
	}
	rec := &monitorBackedRecorder{historyDBPath: historyPath, jobName: job.Name}

	var notif ports.Dispatcher
	var notifWarn error
	if len(job.Notifications) > 0 {
		dispatcher, err := notifier.NewDispatcherFromConfig(job.Notifications)
		if err != nil {
			notifWarn = err
		} else {
			notif = dispatcher
		}
	}

	exec := backup.NewExecutor(
		dumps,
		nil, // ports.StorageBackend — reserved; retention sweep stays driving-side (progress.md Sub-PR K)
		nil, // ports.EncryptWriter — per-file encryption goes through Job.EncryptArtifact
		nil, // ports.Hasher — digests computed inline by dump adapters
		rec,
		notif,
		nil, // ports.LockManager — job serialization owned by the scheduler runtime
		dbprobe.NewAdapter(),
		manifest.Adapter{},
	)

	return &backupExecution{
		exec:      exec,
		job:       buildDomainBackupJob(cfg, job, storageParams, scheduled, forceFull, engineOpts),
		notifWarn: notifWarn,
	}, nil
}

// buildDomainBackupJob translates the config shapes into the pure domain Job.
func buildDomainBackupJob(
	cfg *config.Configuration,
	job config.BackupJob,
	storageParams *storage.Params,
	scheduled, forceFull bool,
	engineOpts ports.EngineOptions,
) backup.Job {
	normalized := config.NormalizeIncrementalBackupConfig(job)
	incrementalEnabled := normalized.IncrementalBackup != nil && normalized.IncrementalBackup.Enabled
	maxChainDepth := 0
	if incrementalEnabled {
		maxChainDepth = normalized.IncrementalBackup.MaxChainDepth
	}

	djob := backup.Job{
		Name:     job.Name,
		Engine:   job.Type,
		Database: job.Database,
		DBConn: ports.DatabaseConfig{
			Type:     job.Type,
			Host:     job.Host,
			Port:     job.Port,
			Username: job.Username,
		},
		Options:            engineOpts,
		IncrementalEnabled: incrementalEnabled,
		MaxChainDepth:      maxChainDepth,
		ForceFull:          forceFull,
		Scheduled:          scheduled,
		Retention: buildRetentionPolicy(job.Retention, false),
		VerifyArtifactHash: verifyIncrementalArtifactHash,
	}

	if storageParams != nil {
		djob.StorageType = storageParams.StorageType
		djob.LocalPath = storageParams.LocalPath
		djob.OutName = storageParams.OutName
		djob.GCSBucket = storageParams.GCSBucket
	}

	if cfg != nil && (cfg.EncryptionKeyEnv != "" || cfg.EncryptionKeyFile != "") {
		djob.EncryptionKeyHint = cfg.EncryptionKeyEnv
		djob.EncryptArtifact = func(path, backupID string) (bool, *ports.EncryptionInfo, string, error) {
			return encryptBackupFile(cfg, path, backupID)
		}
	}

	switch job.Type {
	case "mysql", "mariadb":
		jobCopy := job
		djob.ArchiveBinlogs = func(ctx context.Context, artifactPath string) (backup.IncrementalArtifacts, error) {
			return archiveMySQLBinlogArtifacts(ctx, jobCopy, artifactPath)
		}
	case "mongodb":
		jobCopy := job
		djob.ArchiveOplog = func(ctx context.Context, artifactPath string) (backup.IncrementalArtifacts, error) {
			return archiveMongoOplogArtifacts(ctx, jobCopy, artifactPath)
		}
	}

	return djob
}

// applyBackupSecurity preserves the historical CLI entry point (and its test
// surface): it runs only the post-dump pipeline for an already-produced
// artifact. Relocated orchestration lives in
// internal/domain/backup/pipeline.go.
func applyBackupSecurity(cfg *config.Configuration, job config.BackupJob, storageParams *storage.Params, forceFull bool, plaintextDigest string) (*backup.SecurityOutcome, error) {
	be, err := NewBackupExecutorFromConfig(cfg, job, storageParams, false, forceFull, nil, nil)
	if err != nil {
		return nil, err
	}
	outcome, warnings, secErr := be.exec.ApplyArtifactSecurity(context.Background(), be.job, plaintextDigest)
	for _, w := range warnings {
		fmt.Println(w)
	}
	return outcome, secErr
}

func archiveMySQLBinlogArtifacts(ctx context.Context, job config.BackupJob, backupFilePath string) (backup.IncrementalArtifacts, error) {
	binlogPath := strings.TrimSpace(job.MySQL.BinlogPath)
	if binlogPath == "" {
		return backup.IncrementalArtifacts{}, fmt.Errorf("mysql.binlog_path is required for incremental %s backup", job.Type)
	}

	archiveName := fmt.Sprintf("%s.binlogs.tar", filepath.Base(backupFilePath))
	archiveResult, err := mysqlbinlog.Archive(ctx, &mysqlbinlog.ArchiveArgs{
		BinlogDir:   binlogPath,
		OutputDir:   filepath.Dir(backupFilePath),
		ArchiveName: archiveName,
	})
	if err != nil {
		return backup.IncrementalArtifacts{}, fmt.Errorf("failed to archive %s binlogs: %w", job.Type, err)
	}

	return backup.IncrementalArtifacts{
		BinlogStartFile: archiveResult.StartFile,
		BinlogEndFile:   archiveResult.EndFile,
		BinlogArtifacts: []string{archiveResult.ArchivePath},
	}, nil
}

func archiveMongoOplogArtifacts(ctx context.Context, job config.BackupJob, backupFilePath string) (backup.IncrementalArtifacts, error) {
	mongoURI := strings.TrimSpace(job.URI)
	if mongoURI == "" {
		return backup.IncrementalArtifacts{}, fmt.Errorf("mongodb uri is required for incremental oplog archival")
	}

	archiveName := fmt.Sprintf("%s.oplog.archive", filepath.Base(backupFilePath))
	archiveResult, err := dumpmongo.ArchiveOplog(ctx, &dumpmongo.OplogArchiveArgs{
		URI:         mongoURI,
		OutputDir:   filepath.Dir(backupFilePath),
		ArchiveName: archiveName,
	})
	if err != nil {
		return backup.IncrementalArtifacts{}, fmt.Errorf("failed to archive mongodb oplog: %w", err)
	}

	return backup.IncrementalArtifacts{
		OplogArtifactPath: archiveResult.ArchivePath,
	}, nil
}

// encryptBackupFile encrypts filePath in-place using AES-256-GCM via
// ChunkEncryptWriter. Returns (encrypted, encInfo, hashOfEncryptedFile, err).
func encryptBackupFile(cfg *config.Configuration, filePath, backupID string) (bool, *ports.EncryptionInfo, string, error) {
	kp := &crypto.FileKeyProvider{
		EnvVar:   cfg.EncryptionKeyEnv,
		FilePath: cfg.EncryptionKeyFile,
	}
	masterKey, err := kp.GetKey()
	if err != nil {
		return false, nil, "", fmt.Errorf("failed to get encryption key: %w", err)
	}

	salt, err := crypto.GenerateSalt()
	if err != nil {
		return false, nil, "", err
	}
	derivedKey := crypto.DeriveKey(masterKey, salt)

	in, err := os.Open(filePath)
	if err != nil {
		return false, nil, "", fmt.Errorf("failed to open file for encryption: %w", err)
	}

	encPath := filePath + ".enc"
	out, err := os.Create(encPath)
	if err != nil {
		in.Close()
		return false, nil, "", fmt.Errorf("failed to create encrypted output: %w", err)
	}

	hw := crypto.NewHashingWriter(out)
	enc, err := crypto.NewChunkEncryptWriter(hw, derivedKey, backupID)
	if err != nil {
		in.Close()
		out.Close()
		os.Remove(encPath)
		return false, nil, "", err
	}

	_, copyErr := io.Copy(enc, in)
	in.Close()
	if copyErr != nil {
		out.Close()
		os.Remove(encPath)
		return false, nil, "", fmt.Errorf("failed during encryption: %w", copyErr)
	}

	if flushErr := enc.Flush(); flushErr != nil {
		out.Close()
		os.Remove(encPath)
		return false, nil, "", fmt.Errorf("failed to flush encrypted data: %w", flushErr)
	}

	encHash := hw.Sum()
	nonce := enc.BaseNonce()
	authTag := enc.LastAuthTag()
	out.Close()

	if renameErr := os.Rename(encPath, filePath); renameErr != nil {
		os.Remove(encPath)
		return false, nil, "", fmt.Errorf("failed to replace file with encrypted version: %w", renameErr)
	}

	encInfo := &ports.EncryptionInfo{
		Algorithm:       "AES-256-GCM",
		KeyDerivation:   "PBKDF2-HMAC-SHA256",
		Iterations:      100_000,
		Salt:            base64.StdEncoding.EncodeToString(salt),
		IV:              hex.EncodeToString(nonce),
		AuthTag:         hex.EncodeToString(authTag),
		EnvelopeVersion: 2,
	}

	return true, encInfo, encHash, nil
}
