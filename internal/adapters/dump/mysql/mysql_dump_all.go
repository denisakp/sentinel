package mysql

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"

	backup "github.com/denisakp/sentinel/internal/domain/backup"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/utils"
)

// MySqlDumpAllArgs defines arguments for mysqldump --all-databases.
type MySqlDumpAllArgs struct {
	Host           string          // MySQL host
	Port           string          // MySQL port
	Username       string          // MySQL username
	Password       string          // MySQL password
	AdditionalArgs string          // Additional arguments for mysqldump
	Storage        *storage.Params // Storage parameters
}

// argsBuilderAll builds the arguments for `mysqldump --all-databases`.
func argsBuilderAll(mda *MySqlDumpAllArgs) ([]string, error) {
	args := []string{
		fmt.Sprintf("--host=%s", utils.DefaultValue(mda.Host, "127.0.0.1")),
		fmt.Sprintf("--port=%s", utils.DefaultValue(mda.Port, "3306")),
		fmt.Sprintf("--user=%s", mda.Username),
		"--all-databases",
	}

	if mda.Password == "" {
		args = append(args, "--skip-password")
	}

	if mda.AdditionalArgs != "" {
		extra, err := backup.ParseAdditionalArgs(mda.AdditionalArgs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, extra...)
	}

	return backup.RemoveArgsDuplicate(args), nil
}

// BackupAll backs up all MySQL databases using mysqldump --all-databases.
func BackupAll(mda *MySqlDumpAllArgs) (string, error) {
	args, err := argsBuilderAll(mda)
	if err != nil {
		return "", err
	}

	cmd := exec.Command("mysqldump", args...)
	if mda.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", mda.Password))
	}

	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	if err := cmd.Run(); err != nil {
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		return "", fmt.Errorf("failed to execute mysqldump command - %w, %s", err, redacted)
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
