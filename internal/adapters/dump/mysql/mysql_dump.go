package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/adapters/streamsink"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/utils"
	"os/exec"
)

// Backup backs up a MySQL database using mysqldump. The prober checks
// connectivity to the target database before mysqldump runs.
func Backup(ctx context.Context, prober ports.DBProber, mda *MySqlDumpArgs) (string, error) {
	args, err := argsBuilder(mda)
	if err != nil {
		return "", fmt.Errorf("failed to build mysql_dump args - %w", err)
	}

	// check database connectivity
	port, _ := strconv.Atoi(mda.Port)
	if err := prober.Ping(context.Background(), ports.DatabaseConfig{
		Type: "mysql", Host: mda.Host, Port: port,
		Username: mda.Username, Password: mda.Password, Database: mda.Database,
	}); err != nil {
		return "", err
	}

	// execute mysqldump command
	cmd := exec.CommandContext(ctx, "mysqldump", args...)
	if mda.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", mda.Password))
	}

	// capture command error
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr

	// capture command output
	// get storage handler
	storageHandler, err := storage.NewStorage(mda.Storage)
	if err != nil {
		return "", err
	}

	// get backup path
	backupPath, err := storageHandler.GetBackupPath(mda.Storage.LocalPath)

	// set outName with customizable extension (default is .sql)
	mda.Storage.OutName = utils.FinalOutName(mda.Storage.OutName)

	// get full path. This must be resolved BEFORE the dump runs: streaming to
	// Storage.OutName instead wrote the artifact to a relative path, which lands in
	// the process's working directory rather than the configured output directory.
	fullPath := utils.FullPath(backupPath, mda.Storage.OutName)

	// Local storage streams straight to the destination file, hashing on the way.
	// Reading the whole dump into a bytes.Buffer first made peak memory track the
	// uncompressed dump size, so a large database was killed by the OOM killer
	// rather than failing with a useful error (#164).
	//
	// Remote storage still buffers, deliberately: that path writes through
	// Storage.WriteBackup, whose object key is derived differently per backend, and
	// converting it to a streaming Upload without untangling that first risks
	// breaking remote backups that work today.
	if streamsink.IsLocal(mda.Storage) {
		digest, serr := streamsink.RunToSink(ctx, cmd, streamsink.Sink{LocalPath: fullPath})
		if serr != nil {
			redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
			return "", fmt.Errorf("failed to execute mysqldump command - %w, %s", serr, redacted)
		}
		fmt.Printf("Backup complete !\n")
		return digest, nil
	}

	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	err = cmd.Run()
	if err != nil {
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		return "", fmt.Errorf("failed to execute mysqldump command - %w, %s", err, redacted)
	}

	sum := sha256.Sum256(stdOut.Bytes())
	digest := hex.EncodeToString(sum[:])

	// write backup to storage
	if err := storageHandler.WriteBackup(stdOut.Bytes(), fullPath); err != nil {
		return "", fmt.Errorf("failed to write backup to storage - %w", err)
	}

	fmt.Printf("Backup complete !\n")

	return digest, nil
}
