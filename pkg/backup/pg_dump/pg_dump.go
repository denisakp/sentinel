package pg_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/backup/sql"
	"os/exec"
	"path/filepath"
)

// Backup backs up a PostgresSQL database using pg_dump
func Backup(pda *PgDumpArgs) error {
	// enable compression if compression algorithm is set
	if pda.CompressionAlgorithm != "" {
		pda.Compress = true
	}

	// set default output format
	pda.OutFormat = backup.DefaultString(pda.OutFormat, "p")
	if err := validatePgOutFormat(pda.OutFormat); err != nil {
		return err
	}

	// handle the backup output name
	if err := setOutName(pda); err != nil {
		return err
	}

	// check connectivity
	if ok, err := sql.CheckConnectivity("postgres", pda.Host, pda.Port, pda.Username, pda.Password, pda.Database); !ok {
		return err
	}

	// create backup directory
	backupDirectory, err := backup.CreateBackupDirectory() // create backup directory
	if err != nil {
		return err
	}

	// define path for the backup file
	pda.OutName = filepath.Join(backupDirectory, pda.OutName) // set the backup file path

	pgArgs := &PgDumpArgs{
		Host:                 pda.Host,
		Port:                 pda.Port,
		Username:             pda.Username,
		Password:             pda.Password,
		Database:             pda.Database,
		OutName:              pda.OutName,
		OutFormat:            pda.OutFormat,
		Compress:             pda.Compress,
		CompressionAlgorithm: pda.CompressionAlgorithm,
		CompressionLevel:     pda.CompressionLevel,
		AdditionalArgs:       pda.AdditionalArgs,
	}

	args, err := argsBuilder(pgArgs) // build pg_dump arguments
	if err != nil {
		return fmt.Errorf("failed to build pg_dump args - %w", err)
	}

	cmd := exec.Command("pg_dump", args...)                               // run pg_dump command
	cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", pda.Password)) // set the password in the environment
	defer func() {
		cmd.Env = cmd.Env[:len(cmd.Env)-1]
	}() // remove the password from the environment

	output, err := cmd.CombinedOutput() // get the output of the command
	if err != nil {
		return fmt.Errorf("failed to run pg_dump command: %w, output: %s", err, output)
	}

	fmt.Printf("Backup file created at %s\n", pda.OutName)

	return nil
}
