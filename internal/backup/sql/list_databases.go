package sql

import (
	"database/sql"
	"fmt"

	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// ListDatabases returns database names for the given SQL engine.
func ListDatabases(dbType, host, port, user, password string) ([]string, error) {
	switch dbType {
	case "postgres":
		return listPostgresDatabases(host, port, user, password)
	case "mysql", "mariadb":
		return listMySQLDatabases(host, port, user, password)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

func listPostgresDatabases(host, port, user, password string) ([]string, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", user, password, host, port)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT datname FROM pg_database WHERE datistemplate = false")
	if err != nil {
		return nil, fmt.Errorf("failed to query databases: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to read database name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read database rows: %w", err)
	}

	return names, nil
}

func listMySQLDatabases(host, port, user, password string) ([]string, error) {
	cfg := (&mysql.Config{
		User:                 user,
		Passwd:               password,
		Net:                  "tcp",
		Addr:                 fmt.Sprintf("%s:%s", host, port),
		AllowNativePasswords: true,
	}).FormatDSN()

	db, err := sql.Open("mysql", cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to mysql: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SHOW DATABASES")
	if err != nil {
		return nil, fmt.Errorf("failed to query databases: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to read database name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read database rows: %w", err)
	}

	return names, nil
}
