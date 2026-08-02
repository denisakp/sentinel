package mariadb

import (
	"github.com/denisakp/sentinel/internal/adapters/mysqlargs"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
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

// argsBuilder builds the mariadb-dump arguments by delegating to the shared
// MySQL-family core with the MariaDB flavor (spec 044 / PRD 15). MariaDB does
// not emit --skip-password on an empty password.
func argsBuilder(mda *MariaDBDumpArgs) ([]string, error) {
	return mysqlargs.BuildArgs(mysqlargs.Input{
		Host:           mda.Host,
		Port:           mda.Port,
		Username:       mda.Username,
		Password:       mda.Password,
		Database:       mda.Database,
		AdditionalArgs: mda.AdditionalArgs,
		TLS:            mda.TLS,
	}, mysqlargs.FlavorMariaDB)
}
