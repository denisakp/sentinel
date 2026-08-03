package db_probe

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// PingSqlDatabase opens a sql connection with the given driver and DSN, pings
// it, and closes it. Original failure-mode matrix preserved.
//
// PingSqlDatabase MUST NOT call log.Fatal*, log.Panic*, or os.Exit.
func PingSqlDatabase(driver, sourceName string) (retErr error) {
	db, err := sql.Open(driver, sourceName)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer func() {
		closeErr := db.Close()
		if closeErr == nil {
			return
		}
		if retErr == nil {
			retErr = fmt.Errorf("close ping conn: %w", closeErr)
			return
		}
		slog.Warn("sql_ping_close_error", "event", "sql_ping_close_error", "error", closeErr.Error())
	}()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	return nil
}
