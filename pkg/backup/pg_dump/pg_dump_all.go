package pg_dump

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"

	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/utils"
)

// PgDumpAllArgs defines arguments for pg_dumpall.
type PgDumpAllArgs struct {
	Host           string          // PostgresSQL host
	Port           string          // PostgresSQL port
	Username       string          // PostgresSQL username
	Password       string          // PostgresSQL password
	AdditionalArgs string          // Additional arguments for pg_dumpall
	Storage        *storage.Params // Storage parameters
}

// BackupAll backs up all PostgresSQL databases using pg_dumpall.
func BackupAll(pda *PgDumpAllArgs) (string, error) {
	storageHandler, err := storage.NewStorage(pda.Storage)
	if err != nil {
		return "", err
	}

	backupPath, err := storageHandler.GetBackupPath(pda.Storage.LocalPath)
	if err != nil {
		return "", err
	}

	args := []string{
		fmt.Sprintf("--host=%s", utils.DefaultValue(pda.Host, "127.0.0.1")),
		fmt.Sprintf("--port=%s", utils.DefaultValue(pda.Port, "5432")),
		fmt.Sprintf("--username=%s", pda.Username),
	}

	if pda.AdditionalArgs != "" {
		additionalArgs, err := backup.ParseAdditionalArgs(pda.AdditionalArgs)
		if err != nil {
			return "", fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, additionalArgs...)
	}

	cmd := exec.Command("pg_dumpall", args...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", pda.Password))
	defer func() {
		cmd.Env = cmd.Env[:len(cmd.Env)-1]
	}()

	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	if err := cmd.Run(); err != nil {
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		return "", fmt.Errorf("failed to execute pg_dumpall command - %w, %s", err, redacted)
	}

	pda.Storage.OutName = utils.FinalOutName(pda.Storage.OutName)
	fullPath := utils.FullPath(backupPath, pda.Storage.OutName)

	sum := sha256.Sum256(stdOut.Bytes())
	digest := hex.EncodeToString(sum[:])

	if err := storageHandler.WriteBackup(stdOut.Bytes(), fullPath); err != nil {
		return "", fmt.Errorf("failed to write backup to storage - %w", err)
	}

	fmt.Printf("Backup complete !\n")

	return digest, nil
}
