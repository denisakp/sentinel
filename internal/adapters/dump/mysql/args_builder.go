package mysql

import (
	"github.com/denisakp/sentinel/internal/adapters/mysqlargs"
	"github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/ports"
)

type MySqlDumpArgs struct {
	Host           string          // MySQL host
	Port           string          // MySQL port
	Username       string          // MySQL username
	Password       string          // MySQL password
	Database       string          // MySQL database name
	AdditionalArgs string          // Additional arguments for the mysql_dump command
	Storage        *storage.Params // Storage parameters
	TLS            *ports.Config
}

// engineOptions satisfies ports.EngineOptions.
func (*MySqlDumpArgs) IsEngineOptions() {}

// argsBuilder builds the mysqldump arguments by delegating to the shared
// MySQL-family core with the MySQL flavor (spec 044 / PRD 15). MySQL emits
// --skip-password on an empty password.
func argsBuilder(mda *MySqlDumpArgs) ([]string, error) {
	return mysqlargs.BuildArgs(mysqlargs.Input{
		Host:           mda.Host,
		Port:           mda.Port,
		Username:       mda.Username,
		Password:       mda.Password,
		Database:       mda.Database,
		AdditionalArgs: mda.AdditionalArgs,
		TLS:            mda.TLS,
	}, mysqlargs.FlavorMySQL)
}
