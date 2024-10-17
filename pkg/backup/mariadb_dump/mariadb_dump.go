package mariadb_dump

import (
	"bytes"
	"fmt"
	backup2 "github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/backup/sql"
	"os"
	"os/exec"
	"path/filepath"
)

func Backup(mda *MariaDBDumpArgs) error {
	// Validate the required arguments
	args, err := ArgsBuilder(mda)
	if err != nil {
		return fmt.Errorf("failed to build arguments: %w", err)
	}

	// check connectivity
	if ok, err := sql.CheckConnectivity("mysql", mda.Host, mda.Port, mda.Username, mda.Password, mda.Database); !ok {
		return err
	}

	// execute mariadb command
	cmd := exec.Command("mysqldump", args...)

	// capture command error
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr

	// capture command output
	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to execute maridb-dump command - %w, %s", err, stdErr.String())
	}

	// create database backup directory
	backupDirectory, err := backup2.CreateBackupDirectory()
	if err != nil {
		return err
	}

	// set output name with customizable extension (default is .sql)
	extension := backup2.DefaultString(filepath.Ext(mda.OutName), ".sql")
	mda.OutName = backup2.DefaultString(mda.OutName, backup2.GenerateBackupOutName(mda.Database)) + extension

	// define path for the backup file
	backupFilePath := filepath.Join(backupDirectory, mda.OutName)

	// create output file
	outfile, err := os.Create(backupFilePath)
	if err != nil {
		return fmt.Errorf("failed to create output file - %w", err)
	}
	defer outfile.Close()

	// write command output to the backup file
	_, err = outfile.Write(stdOut.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write to output file - %w", err)
	}

	fmt.Printf("Backup file created at %s\n", backupFilePath)

	return nil
}
