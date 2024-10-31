package mongo_dump

import (
	"bytes"
	"fmt"
	"github.com/denisakp/sentinel/internal/backup/mongo"
	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/utils"
	"os/exec"
	"time"
)

// Backup backs up a MongoDB database using mongo_dump
func Backup(da *DumpMongoArgs) error {
	args, err := argsBuilder(da) // build mongo_dump arguments
	if err != nil {
		return fmt.Errorf("failed to build arguments: %w", err)
	}

	if err := validateRequiredArgs(da); err != nil {
		return err
	}

	// check connectivity
	if err := mongo.CheckConnectivity(da.Uri); err != nil {
		return fmt.Errorf("failed to check connectivity: %w", err)
	}

	// get the storage handler
	storageHandler, err := storage.NewStorage(da.StorageType)
	if err != nil {
		return err
	}

	backupPath, err := storageHandler.GetBackupPath(da.StoragePath)
	if err != nil {
		return err
	}

	da.OutName = utils.DefaultValue(da.OutName, fmt.Sprintf("SENTINEL_%s", time.Now().Format("2006-01-02-15-04-05"))) // set default output name

	da.OutName = utils.FullPath(backupPath, da.OutName) // set the backup file path

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
		return err
	}

	fmt.Printf("Backup completed successfully %s \n", da.OutName)

	return nil
}
