package pg

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
func BackupAll(ctx context.Context, pda *PgDumpAllArgs) (string, error) {
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

	cmd := exec.CommandContext(ctx, "pg_dumpall", args...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", pda.Password))
	defer func() {
		cmd.Env = cmd.Env[:len(cmd.Env)-1]
	}()

	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr

	pda.Storage.OutName = utils.FinalOutName(pda.Storage.OutName)
	fullPath := utils.FullPath(backupPath, pda.Storage.OutName)

	digest, err := streamsink.Run(ctx, cmd, pda.Storage, fullPath)
	if err != nil {
		return "", streamsink.Describe(err, "pg_dumpall", stdErr.Bytes())
	}

	fmt.Printf("Backup complete !\n")

	return digest, nil
}
