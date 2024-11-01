package mysql_dump

import (
	"bytes"
	"fmt"
	"github.com/denisakp/sentinel/internal/backup/sql"
	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/utils"
	"os/exec"
)

// Backup backs up a MySQL database using mysqldump
func Backup(mda *MySqlDumpArgs) error {
	args, err := argsBuilder(mda)
	if err != nil {
		return fmt.Errorf("failed to build mysql_dump args - %w", err)
	}

	// check database connectivity
	if ok, err := sql.CheckConnectivity("mysql", mda.Host, mda.Port, mda.Username, mda.Password, mda.Database); !ok {
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

	// get storage handler
	storageHandler, err := storage.NewStorage(mda.StorageType)
	if err != nil {
		return err
	}

	// get backup path
	backupPath, err := storageHandler.GetBackupPath(mda.StoragePath)

	// set outName with customizable extension (default is .sql)
	mda.OutName = utils.FinalOutName(mda.OutName)

	// get full path
	fullPath := utils.FullPath(backupPath, mda.OutName)

	// write backup to storage
	if err := storageHandler.WriteBackup(stdOut.Bytes(), fullPath); err != nil {
		return fmt.Errorf("failed to write backup to storage - %w", err)
	}

	fmt.Printf("Backup complete !\n")

	return nil
}
