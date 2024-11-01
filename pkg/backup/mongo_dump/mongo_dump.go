package mongo_dump

import (
	"bytes"
	"fmt"
	"github.com/denisakp/sentinel/internal/backup/mongo"
	"github.com/denisakp/sentinel/internal/storage"
	"os/exec"
)

// Backup backs up a MongoDB database using mongo_dump
func Backup(da *DumpMongoArgs) error {
	// get the storage handler
	storageHandler, err := storage.NewStorage(da.StorageType)
	if err != nil {
		return err
	}

	// get the backup path
	backupPath, err := storageHandler.GetBackupPath(da.StoragePath)
	if err != nil {
		return err
	}

	args, err := argsBuilder(da, backupPath) // build mongo_dump arguments
	if err != nil {
		return fmt.Errorf("failed to build mongo_dump arguments: %w", err)
	}

	// check connectivity
	if err := mongo.CheckConnectivity(da.Uri); err != nil {
		return err
	}

	cmd := exec.Command("mongodump", args...) // run mongo_dump command

	// capture the command error
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr

	// capture the command output
	var stdOut bytes.Buffer
	cmd.Stdout = &stdOut

	// run the command
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to run mongo_dump: %w", err)
	}

	if err := storageHandler.WriteBackup(stdOut.Bytes(), da.OutName); err != nil {
		return fmt.Errorf("failed to write backup to storage: %w", err)
	}

	fmt.Printf("Backup complete !\n")

	return nil
}
