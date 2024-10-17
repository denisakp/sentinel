package sql

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/backup"
	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"net/url"
)

func CheckConnectivity(dbType, host, port, user, password, database string) (bool, error) {
	// Todo: the user maybe wants to use a tcp6 or unix socket, so this should be configurable in the future
	scheme, err := defineScheme(dbType) // define the scheme based on the database type
	if err != nil {
		return false, err
	}

	switch scheme {
	case "mysql":
		cfg := (&mysql.Config{
			User:   user,
			Passwd: password,
			Net:    "tcp",
			Addr:   fmt.Sprintf("%s:%s", host, port),
			DBName: database,
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
	if err := backup.ValidateDbType(dbType); err != nil {
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
