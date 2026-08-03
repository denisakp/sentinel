package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/utils"
	"os/exec"
)

// Backup backs up a MySQL database using mysqldump. The prober checks
// connectivity to the target database before mysqldump runs.
func Backup(prober ports.DBProber, mda *MySqlDumpArgs) (string, error) {
	args, err := argsBuilder(mda)
	if err != nil {
		return "", fmt.Errorf("failed to build mysql_dump args - %w", err)
	}

	// check database connectivity
	port, _ := strconv.Atoi(mda.Port)
	if err := prober.Ping(context.Background(), ports.DatabaseConfig{
		Type: "mysql", Host: mda.Host, Port: port,
		Username: mda.Username, Password: mda.Password, Database: mda.Database,
	}); err != nil {
		return "", err
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
		redacted, _ := sanitize.RedactStderr(stdErr.Bytes())
		return "", fmt.Errorf("failed to execute mysqldump command - %w, %s", err, redacted)
	}

	// get storage handler
	storageHandler, err := storage.NewStorage(mda.Storage)
	if err != nil {
		return "", err
	}

	// get backup path
	backupPath, err := storageHandler.GetBackupPath(mda.Storage.LocalPath)

	// set outName with customizable extension (default is .sql)
	mda.Storage.OutName = utils.FinalOutName(mda.Storage.OutName)

	// get full path
	fullPath := utils.FullPath(backupPath, mda.Storage.OutName)

	sum := sha256.Sum256(stdOut.Bytes())
	digest := hex.EncodeToString(sum[:])

	// write backup to storage
	if err := storageHandler.WriteBackup(stdOut.Bytes(), fullPath); err != nil {
		return "", fmt.Errorf("failed to write backup to storage - %w", err)
	}

	fmt.Printf("Backup complete !\n")

	return digest, nil
}
