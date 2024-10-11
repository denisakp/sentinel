package backup

import (
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
	"log"
	"net/url"
)

func CheckConnectivity(dbType, host, port, user, password, database string) (bool, error) {
	var err error
	var db *sql.DB

	conStr, err := buildConnectionString(dbType, host, port, user, password, database)
	if err != nil {
		return false, err
	}

	switch conStr.Scheme {
	case "mysql", "postgres":
		db, err = sql.Open(conStr.Scheme, conStr.String())
		if err != nil {
			log.Fatalf("failed to open database connection: %v", err)
		}
		pingErr := db.Ping()
		if pingErr != nil {
			log.Fatalf("failed to ping database - %v", pingErr)
		}
	default:
		return false, fmt.Errorf("invalid database type: %s", conStr.Scheme)
	}

	for i := 0; i < 3; i++ {

	}

	return true, nil
}

// buildConnectionString builds a connection string for the given database type
func buildConnectionString(dbType, host, port, user, password, database string) (*url.URL, error) {

	switch dbType {
	case "postgres":
		return &url.URL{
			Scheme:   dbType,
			User:     url.UserPassword(user, password),
			Host:     fmt.Sprintf("%s:%s", host, port),
			Path:     "/" + database,
			RawQuery: "sslmode=disable",
		}, nil
	case "mysql", "mariadb":
		return &url.URL{
			Scheme: "mysql",
			User:   url.UserPassword(user, password),
			Host:   fmt.Sprintf("%s:%s", host, port),
			Path:   "/" + database,
		}, nil

	default:
		return nil, fmt.Errorf("invalid database type: %s", dbType)
	}
}
