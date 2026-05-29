package mariadb

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"

	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/utils"
)

// MariaDBDumpAllArgs defines arguments for mariadb-dump --all-databases.
type MariaDBDumpAllArgs struct {
	Host           string          // MariaDB host
	Port           string          // MariaDB port
	Username       string          // MariaDB username
	Password       string          // MariaDB password
	AdditionalArgs string          // Additional arguments for mariadb-dump
	Storage        *storage.Params // Storage parameters
}

// BackupAll backs up all MariaDB databases using mariadb-dump --all-databases.
func BackupAll(mda *MariaDBDumpAllArgs) (string, error) {
	args := []string{
		fmt.Sprintf("--host=%s", utils.DefaultValue(mda.Host, "127.0.0.1")),
		fmt.Sprintf("--port=%s", utils.DefaultValue(mda.Port, "3306")),
		fmt.Sprintf("--user=%s", mda.Username),
		"--all-databases",
	}

	if mda.AdditionalArgs != "" {
		additionalArgs, err := backup.ParseAdditionalArgs(mda.AdditionalArgs)
		if err != nil {
			return "", fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, additionalArgs...)
	}

	args = backup.RemoveArgsDuplicate(args)

	cmd := exec.Command("mariadb-dump", args...)
	if mda.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", mda.Password))
	}

	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	if err := cmd.Run(); err != nil {
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		return "", fmt.Errorf("failed to execute mariadb-dump command - %w, %s", err, redacted)
	}

	storageHandler, err := storage.NewStorage(mda.Storage)
	if err != nil {
		return "", err
	}
	backupPath, err := storageHandler.GetBackupPath(mda.Storage.LocalPath)
	if err != nil {
		return "", err
	}

	mda.Storage.OutName = utils.FinalOutName(mda.Storage.OutName)
	fullPath := utils.FullPath(backupPath, mda.Storage.OutName)

	sum := sha256.Sum256(stdOut.Bytes())
	digest := hex.EncodeToString(sum[:])

	if err := storageHandler.WriteBackup(stdOut.Bytes(), fullPath); err != nil {
		return "", fmt.Errorf("failed to write backup to storage - %w", err)
	}

	fmt.Printf("Backup complete !\n")
	return digest, nil
}
