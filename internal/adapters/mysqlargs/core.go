// Package mysqlargs holds the shared MySQL-family dump-argument core used by the
// mysql and mariadb dump adapters. It lives outside
// internal/adapters/dump/ because the adapter-dump-axis lint rule forbids the
// engine sub-packages from importing the internal/adapters/dump prefix.
package mysqlargs

import (
	"fmt"

	backup "github.com/denisakp/sentinel/internal/domain/backup"
	internaltls "github.com/denisakp/sentinel/internal/adapters/tls"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/utils"
)

// Flavor selects the engine-specific knobs of the shared arg builder: whether an
// empty password emits --skip-password (MySQL only) and the TLS engine string.
type Flavor int

const (
	FlavorMySQL Flavor = iota
	FlavorMariaDB
)

// Input carries the engine-agnostic fields the shared core needs, copied from
// the caller's *DumpArgs.
type Input struct {
	Host, Port, Username, Password, Database, AdditionalArgs string
	TLS                                                      *ports.Config
}

// ValidateRequired enforces the shared required-field checks.
func ValidateRequired(username, database string) error {
	if database == "" {
		return fmt.Errorf("database name is missing")
	}
	if username == "" {
		return fmt.Errorf("username is missing")
	}
	return nil
}

func tlsEngine(f Flavor) string {
	if f == FlavorMariaDB {
		return "mariadb"
	}
	return "mysql"
}

// BuildArgs builds the mysqldump/mariadb-dump argument list shared by both
// engines. The argument order is preserved exactly from the pre-dedupe builders:
// [--host,--port,--user] -> (MySQL-only --skip-password on empty password) ->
// additional-args -> TLS args -> de-dup -> database.
func BuildArgs(in Input, flavor Flavor) ([]string, error) {
	if err := ValidateRequired(in.Username, in.Database); err != nil {
		return nil, err
	}

	host := utils.DefaultValue(in.Host, "127.0.0.1")
	port := utils.DefaultValue(in.Port, "3306")

	args := []string{
		fmt.Sprintf("--host=%s", host),
		fmt.Sprintf("--port=%s", port),
		fmt.Sprintf("--user=%s", in.Username),
	}

	if flavor == FlavorMySQL && in.Password == "" {
		args = append(args, "--skip-password")
	}

	if in.AdditionalArgs != "" {
		parsed, err := backup.ParseAdditionalArgs(in.AdditionalArgs)
		if err != nil {
			return nil, fmt.Errorf("failed to parse additional_args: %w", err)
		}
		args = append(args, parsed...)
	}

	args = append(args, internaltls.BuildTLSArgs(tlsEngine(flavor), in.TLS)...)

	args = backup.RemoveArgsDuplicate(args)
	args = append(args, in.Database)

	return args, nil
}
