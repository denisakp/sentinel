package mariadb

import (
	"bytes"
	"context"
	"fmt"
	"strconv"

	"os/exec"

	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/adapters/streamsink"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/utils"
)

// Backup backs up a MariaDB database using mariadb-dump. The prober checks
// connectivity to the target database before the dump runs. The probe uses the
// "mysql" scheme, as the prior checkConnectivity call did.
func Backup(ctx context.Context, prober ports.DBProber, mda *MariaDBDumpArgs) (string, error) {
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
	cmd := exec.CommandContext(ctx, "mariadb-dump", args...)
	if mda.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", mda.Password))
	}

	// capture command error
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr

	// capture command output
	// get the storage handler
	storageHandler, err := storage.NewStorage(mda.Storage)
	if err != nil {
		return "", err
	}

	// get the backup path
	backupPath, err := storageHandler.GetBackupPath(mda.Storage.LocalPath)

	// set output name with customizable extension (default is .sql)
	mda.Storage.OutName = utils.FinalOutName(mda.Storage.OutName)

	// get the full path. This must be resolved BEFORE the dump runs: streaming to
	// Storage.OutName instead wrote the artifact to a relative path, which lands in
	// the process's working directory rather than the configured output directory.
	fullPath := utils.FullPath(backupPath, mda.Storage.OutName)

	// Local storage streams straight to the destination file, hashing on the way.
	// Reading the whole dump into a bytes.Buffer first made peak memory track the
	// uncompressed dump size, so a large database was killed by the OOM killer
	// rather than failing with a useful error (#164).
	//
	// Remote storage streams too: the dump goes to a staging file and is uploaded
	// under the same object key WriteBackup used, so memory stays flat there as well.
	digest, serr := streamsink.Run(ctx, cmd, mda.Storage, fullPath)
	if serr != nil {
		return "", streamsink.Describe(serr, "mariadb-dump", stdErr.Bytes())
	}

	fmt.Printf("Backup complete !\n")

	return digest, nil
}
