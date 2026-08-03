package mariadb

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

// Backup backs up a MariaDB database using mariadb-dump. The prober checks
// connectivity to the target database before the dump runs. The probe uses the
// "mysql" scheme, as the prior checkConnectivity call did.
func Backup(prober ports.DBProber, mda *MariaDBDumpArgs) (string, error) {
	// Validate the required arguments
	args, err := argsBuilder(mda)
	if err != nil {
		return "", fmt.Errorf("failed to build arguments: %w", err)
	}

	// check connectivity (mysql scheme, matching the prior behaviour)
	port, _ := strconv.Atoi(mda.Port)
	if err := prober.Ping(context.Background(), ports.DatabaseConfig{
		Type: "mysql", Host: mda.Host, Port: port,
		Username: mda.Username, Password: mda.Password, Database: mda.Database,
	}); err != nil {
		return "", err
	}

	// execute mariadb-dump command
	cmd := exec.Command("mariadb-dump", args...)
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
		return "", fmt.Errorf("failed to execute maridb-dump command - %w, %s", err, redacted)
	}

	// get the storage handler
	storageHandler, err := storage.NewStorage(mda.Storage)
	if err != nil {
		return "", err
	}

	// get the backup path
	backupPath, err := storageHandler.GetBackupPath(mda.Storage.LocalPath)

	// set output name with customizable extension (default is .sql)
	mda.Storage.OutName = utils.FinalOutName(mda.Storage.OutName)

	// get the full path
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
