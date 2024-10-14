package mysql_dump

import (
	"bytes"
	"fmt"
	"github.com/denisakp/sentinel/pkg/backup"
	"os"
	"os/exec"
	"path/filepath"
)

// Backup backs up a MySQL database using mysqldump
func Backup(mda *MySqlDumpArgs) error {
	args, err := ArgsBuilder(mda)
	if err != nil {
		return fmt.Errorf("failed to build mysql_dump args - %w", err)
	}

	// check database connectivity
	if ok, err := backup.CheckConnectivity("mysql", mda.Host, mda.Port, mda.Username, mda.Password, mda.Database); !ok {
		return err
	}

	// execute mysqldump command
	cmd := exec.Command("mysqldump", args...)
	if mda.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", mda.Password))
	}

	// capture command error
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr

	// capture command output
	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to execute mysqldump command - %w, %s", err, stdErr.String())
	}

	// create database backup directory
	backupDirectory, err := backup.CreateBackupDirectory()
	if err != nil {
		return err
	}

	// set output name with customizable extension (default is .sql)
	extension := backup.DefaultString(filepath.Ext(mda.OutName), ".sql")
	mda.OutName = backup.DefaultString(mda.OutName, backup.GenerateBackupOutName(mda.Database)) + extension

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
