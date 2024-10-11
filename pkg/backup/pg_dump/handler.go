package pg_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/pkg/backup"
	"os/exec"
	"path/filepath"
)

// BackupPgDatabase backs up a PostgresSQL database
func BackupPgDatabase(pda *PgDumpArgs) error {
	// enable compression if compression algorithm is set
	if pda.CompressionAlgorithm != "" {
		pda.Compress = true
	}

	if pda.OutFormat == "" {
		pda.OutFormat = "p"
	} else if err := ValidatePgOutFormat(pda.OutFormat); err != nil {
		return err
	}

	// handle the backup output name
	if err := setOutName(pda); err != nil {
		return err
	}

	backupDirectory, err := backup.CreateBackupDirectory()
	if err != nil {
		return err
	}

	backupFilePath := filepath.Join(backupDirectory, pda.OutName)

	pgArgs := &PgDumpArgs{
		Host:                 pda.Host,
		Port:                 pda.Port,
		Username:             pda.Username,
		Password:             pda.Password,
		Database:             pda.Database,
		OutName:              backupFilePath,
		OutFormat:            pda.OutFormat,
		Compress:             pda.Compress,
		CompressionAlgorithm: pda.CompressionAlgorithm,
		CompressionLevel:     pda.CompressionLevel,
		AdditionalArgs:       pda.AdditionalArgs,
	}

	args, err := PgDumpArgsBuilder(pgArgs)
	if err != nil {
		return fmt.Errorf("failed to build pg_dump args - %w", err)
	}

	// check connectivity
	if ok, err := backup.CheckConnectivity("postgres", pda.Host, pda.Port, pda.Username, pda.Password, pda.Database); !ok {
		return fmt.Errorf("failed to connect to the database: %w", err)
	}

	// execute pg_dump command
	cmd := exec.Command("pg_dump", args...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD="+pda.Password))
	defer func() {
		cmd.Env = cmd.Env[:len(cmd.Env)-1]
	}()

	// capture the output of the command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run pg_dump command: %w, %s", err, output)
	}

	return nil
}
