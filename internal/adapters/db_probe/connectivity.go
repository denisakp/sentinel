package db_probe

import (
	"fmt"
	"net/url"

	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// CheckConnectivity pings the target database via PingSqlDatabase. Relocated
// from internal/backup/sql/connectivity.go by spec 037. The engine validation
// previously delegated to backup.ValidateDbType is inlined here so the
// db_probe adapter does not depend on internal/backup.
func CheckConnectivity(dbType, host, port, user, password, database string) (bool, error) {
	// Todo: the user maybe wants to use a tcp6 or unix socket, so this should
	// be configurable in the future.
	scheme, err := defineScheme(dbType)
	if err != nil {
		return false, err
	}

	switch scheme {
	case "mysql":
		cfg := (&mysql.Config{
			User:                 user,
			Passwd:               password,
			Net:                  "tcp",
			Addr:                 fmt.Sprintf("%s:%s", host, port),
			DBName:               database,
			AllowNativePasswords: true,
		}).FormatDSN()

		if err := PingSqlDatabase("mysql", cfg); err != nil {
			return false, fmt.Errorf("failed to ping database - %w", err)
		}
	case "postgres":
		cfg := (&url.URL{
			Scheme:   dbType,
			User:     url.UserPassword(user, password),
			Host:     fmt.Sprintf("%s:%s", host, port),
			Path:     "/" + database,
			RawQuery: "sslmode=disable",
		}).String()
		if err := PingSqlDatabase("postgres", cfg); err != nil {
			return false, fmt.Errorf("failed to ping database - %w", err)
		}
	default:
		return false, err
	}

	return true, nil
}

func defineScheme(dbType string) (string, error) {
	if err := validateDbType(dbType); err != nil {
		return "", err
	}

	switch dbType {
	case "mysql", "mariadb":
		return "mysql", nil
	case "postgres":
		return "postgres", nil
	default:
		return "", fmt.Errorf("invalid database type: %s", dbType)
	}
}

// validateDbType is the locally inlined twin of backup.ValidateDbType. Kept
// private to avoid leaking duplicate API surface; eventual consolidation
// (with backup.ValidateDbType) is out of spec 037's scope.
func validateDbType(dbType string) error {
	switch dbType {
	case "mysql", "postgres", "mariadb", "mongodb":
		return nil
	default:
		return fmt.Errorf("invalid database type: %s", dbType)
	}
}
