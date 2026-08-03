package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	mongotls "github.com/denisakp/sentinel/internal/adapters/mongo_tls"
	"github.com/denisakp/sentinel/internal/version"
	"github.com/spf13/cobra"
)

var longDesc = "\"Sentinel is a cloud-native CLI tool designed for secure and reliable database backup and restoration," +
	" supporting MySQL, MariaDB, PostgreSQL, and MongoDB. With advanced features like AES-256 encryption for data " +
	"security, scheduled backups, and real-time notifications, Sentinel ensures your backups are protected and " +
	"accessible. Store backups on popular cloud services such as AWS S3, Google Drive, or MinIO, and easily monitor" +
	" operations with success or failure notifications. Sentinel is built to simplify database management, " +
	"allowing users to automate, secure, and manage their backup workflows efficiently.\""

var RootCmd = &cobra.Command{
	Use:               "sentinel",
	Short:             "Open-source tool for automated backup and restoration supporting SQL and NoSQL databases",
	Long:              longDesc,
	PersistentPreRunE: rootPreRun,
}

var (
	preRunOnce sync.Once
)

func rootPreRun(cmd *cobra.Command, _ []string) error {
	preRunOnce.Do(func() {
		// Backstop cleanup for prepared mongo TLS material left by hard-killed
		// prior processes (FR-006a). Non-fatal: hygiene only.
		if removed, err := mongotls.SweepOrphanMaterial(os.TempDir()); err != nil {
			slog.Warn("mongo-tls: orphan sweep failed",
				"event", "mongo_tls_orphan_sweep_failed",
				"error", err.Error())
		} else if removed > 0 {
			slog.Debug("mongo-tls: orphan sweep completed",
				"event", "mongo_tls_orphan_swept_total",
				"count", removed)
		}

		// Install signal handler so SIGINT/SIGTERM triggers material cleanup
		// before the process exits (FR-006). Long-running commands like
		// `schedule` install their own handlers; this is the backstop for
		// one-shot CLI invocations.
		installMongoTLSSignalHandler()
	})
	return nil
}

func installMongoTLSSignalHandler() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		if err := mongotls.CloseAll(); err != nil {
			slog.Warn("mongo-tls: signal-driven cleanup encountered errors",
				"event", "mongo_tls_signal_cleanup_failed",
				"error", err.Error())
		}
		// Restore default disposition and re-raise so the process exits with
		// the conventional 128 + signum status.
		signal.Reset(syscall.SIGINT, syscall.SIGTERM)
		if s, ok := sig.(syscall.Signal); ok {
			os.Exit(128 + int(s))
		}
		os.Exit(130)
	}()
}

func init() {
	meta := version.Get()
	RootCmd.Version = meta.Version
	RootCmd.SetVersionTemplate(fmt.Sprintf("sentinel %s\n", meta.Version))

	RootCmd.AddCommand(BackupCmd)
	RootCmd.AddCommand(scheduleCmd)
	RootCmd.AddCommand(retentionCmd)
	RootCmd.AddCommand(monitorCmd)
	RootCmd.AddCommand(configCmd)
	RootCmd.AddCommand(restoreCmd)
	RootCmd.AddCommand(dbCmd)
	RootCmd.AddCommand(SecurityCmd)
	RootCmd.AddCommand(StorageCmd)
	RootCmd.AddCommand(repairCmd)
	RootCmd.AddCommand(versionCmd)
}

func Execute() error {
	// Silence cobra's default "Error: ..." emission so the CLI boundary owns
	// error formatting (constitution III: what/why/how for known sentinels,
	// the legacy single-line shape for everything else).
	RootCmd.SilenceErrors = true
	err := RootCmd.Execute()
	if err == nil {
		return nil
	}
	out := RootCmd.ErrOrStderr()
	switch {
	case errors.Is(err, monitor.ErrForwardIncompatible):
		printForwardIncompatible(out, err)
	default:
		fmt.Fprintf(out, "Error: %s\n", err.Error())
	}
	return err
}
