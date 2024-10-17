package mongo_dump

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/backup/mongo"
	"os/exec"
	"path/filepath"
	"time"
)

// Backup backs up a MongoDB database using mongo_dump
func Backup(da *DumpMongoArgs) error {
	if err := validateRequiredArgs(da); err != nil {
		return err
	}

	// check connectivity
	if err := mongo.CheckConnectivity(da.Uri); err != nil {
		return fmt.Errorf("failed to check connectivity: %w", err)
	}

	backupDirectory, err := backup.CreateBackupDirectory() // create backup directory
	if err != nil {
		return err
	}

	da.OutName = backup.DefaultString(da.OutName, fmt.Sprintf("BACKUP_%s", time.Now().Format("2006-01-02-15-04-05"))) // set default output name

	da.OutName = filepath.Join(backupDirectory, da.OutName) // set the backup file path

	args, err := argsBuilder(da) // build mongo_dump arguments
	if err != nil {
		return fmt.Errorf("failed to build arguments: %w", err)
	}

	cmd := exec.Command("mongodump", args...) // run mongo_dump command

	output, err := cmd.CombinedOutput() // get the output of the command
	if err != nil {
		return fmt.Errorf("failed to execute mongo_dump: %w, output: %s", err, output)
	}

	fmt.Printf("Backup completed successfully %s \n", da.OutName)

	return nil
}
