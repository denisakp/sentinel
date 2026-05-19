package mysql_dump

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/storage"
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

// BackupAll backs up all MySQL databases using mysqldump --all-databases.
func BackupAll(mda *MySqlDumpAllArgs) error {
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
		additionalArgs := backup.ParseAdditionalArgs(mda.AdditionalArgs)
		args = append(args, additionalArgs...)
	}

	args = backup.RemoveArgsDuplicate(args)

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
		return fmt.Errorf("failed to execute mysqldump command - %w, %s", err, redacted)
	}

	storageHandler, err := storage.NewStorage(mda.Storage)
	if err != nil {
		return err
	}
	backupPath, err := storageHandler.GetBackupPath(mda.Storage.LocalPath)
	if err != nil {
		return err
	}

	mda.Storage.OutName = utils.FinalOutName(mda.Storage.OutName)
	fullPath := utils.FullPath(backupPath, mda.Storage.OutName)

	if err := storageHandler.WriteBackup(stdOut.Bytes(), fullPath); err != nil {
		return fmt.Errorf("failed to write backup to storage - %w", err)
	}

	fmt.Printf("Backup complete !\n")
	return nil
}
