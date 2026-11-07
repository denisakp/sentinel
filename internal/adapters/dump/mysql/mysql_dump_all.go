package mysql

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/adapters/streamsink"
	backup "github.com/denisakp/sentinel/internal/domain/backup"
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
func BackupAll(ctx context.Context, mda *MySqlDumpAllArgs) (string, error) {
	args, err := argsBuilderAll(mda)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "mysqldump", args...)
	if mda.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", mda.Password))
	}

	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr

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

	digest, err := streamsink.Run(ctx, cmd, mda.Storage, fullPath)
	if err != nil {
		return "", streamsink.Describe(err, "mysqldump", stdErr.Bytes())
	}

	fmt.Printf("Backup complete !\n")
	return digest, nil
}
