package mariadb_dump

import (
	"fmt"

	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	internaltls "github.com/denisakp/sentinel/internal/tls"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/utils"
)

type MariaDBDumpArgs struct {
	Host           string          // MariaDB host
	Port           string          // MariaDB port
	Username       string          // MariaDB username
	Password       string          // MariaDB password
	Database       string          // MariaDB database name
	AdditionalArgs string          // Additional arguments for the mariadb_dump command
	Storage        *storage.Params // Storage parameters
	TLS            *ports.Config
}

// engineOptions satisfies ports.EngineOptions.
func (*MariaDBDumpArgs) IsEngineOptions() {}

// ArgsBuilder builds the arguments for the mariadb_dump command
func ArgsBuilder(mda *MariaDBDumpArgs) ([]string, error) {
	if err := validateRequiredArgs(mda); err != nil {
		return nil, err
	}

	// set the default host and port if not provided
	mda.Host = utils.DefaultValue(mda.Host, "127.0.0.1")
	mda.Port = utils.DefaultValue(mda.Port, "3306")

	// build the required arguments
	args := []string{
		fmt.Sprintf("--host=%s", mda.Host),
		fmt.Sprintf("--port=%s", mda.Port),
		fmt.Sprintf("--user=%s", mda.Username),
	}

	if mda.AdditionalArgs != "" {
		additionalArgs, err := backup.ParseAdditionalArgs(mda.AdditionalArgs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, additionalArgs...)
	} // add additional arguments if provided

	args = append(args, internaltls.BuildTLSArgs("mariadb", mda.TLS)...)

	args = backup.RemoveArgsDuplicate(args) // remove duplicated arguments
	args = append(args, mda.Database)       // add the database name to the arguments

	return args, nil
}
