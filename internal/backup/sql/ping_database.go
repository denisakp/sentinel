package sql

import (
	"database/sql"
	"fmt"
	"log"
)

// PingSqlDatabase pings the sql database
func PingSqlDatabase(driver, sourceName string) error {
	db, err := sql.Open(driver, sourceName) // open a database connection

	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Fatalf("failed to close database connection: %v", err)
		}
	}(db)

	err = db.Ping() // ping the database
	if err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}
