package pg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/adapters/storage"
)

// Backup backs up a PostgresSQL database using pg_dump. The prober checks
// connectivity to the target database before pg_dump runs.
func Backup(prober ports.DBProber, pda *PgDumpArgs) (string, error) {
	// get the storage handler
	storageHandler, err := storage.NewStorage(pda.Storage)
	if err != nil {
		return "", err
	}

	// get the backup path
	backupPath, err := storageHandler.GetBackupPath(pda.Storage.LocalPath)
	if err != nil {
		return "", err
	}

	// build pg_dump arguments
	args, err := argsBuilder(pda, backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to build pg_dump args - %w", err)
	}

	// check connectivity to the target database
	port, _ := strconv.Atoi(pda.Port)
	if err := prober.Ping(context.Background(), ports.DatabaseConfig{
		Type: "postgres", Host: pda.Host, Port: port,
		Username: pda.Username, Password: pda.Password, Database: pda.Database,
	}); err != nil {
		return "", err
	}

	// run pg_dump command
	cmd := exec.Command("pg_dump", args...)

	// capture the command error
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr

	// capture the command output
	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	// remove the password from the environment after the command is done
	cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", pda.Password)) // set the password in the environment
	defer func() {
		cmd.Env = cmd.Env[:len(cmd.Env)-1]
	}()

	err = cmd.Run()
	if err != nil {
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		return "", fmt.Errorf("failed to execute pg_dump command - %w, %s", err, redacted)
	}

	sum := sha256.Sum256(stdOut.Bytes())
	digest := hex.EncodeToString(sum[:])

	// write the backup to the storage
	if err := storageHandler.WriteBackup(stdOut.Bytes(), pda.Storage.OutName); err != nil {
		return "", fmt.Errorf("failed to write backup to storage - %w", err)
	}

	fmt.Printf("Backup complete !\n")

	return digest, nil
}
