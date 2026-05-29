package mysql

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/denisakp/sentinel/internal/backup/sql"
	"github.com/denisakp/sentinel/internal/sanitize"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/utils"
	"os/exec"
)

// checkConnectivity is overridable in tests.
var checkConnectivity = sql.CheckConnectivity

// Backup backs up a MySQL database using mysqldump
func Backup(mda *MySqlDumpArgs) (string, error) {
	args, err := argsBuilder(mda)
	if err != nil {
		return "", fmt.Errorf("failed to build mysql_dump args - %w", err)
	}

	// check database connectivity
	if ok, err := checkConnectivity("mysql", mda.Host, mda.Port, mda.Username, mda.Password, mda.Database); !ok {
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
