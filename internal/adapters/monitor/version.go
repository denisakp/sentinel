package monitor

import (
	"database/sql"
	"errors"
	"fmt"
)

// BinarySchemaVersion is the monitor schema version required by this binary.
// It MUST equal the highest version present under migrations/. The
// version-invariant test in version_test.go enforces this at build time.
const BinarySchemaVersion = 4

// ErrForwardIncompatible is returned by NewMonitor when the on-disk
// schema_version is ahead of BinarySchemaVersion. Callers map this to a
// non-zero exit at the CLI boundary. Wrap via ForwardIncompatibleError to
// expose the found/required versions to the CLI formatter.
var ErrForwardIncompatible = errors.New("monitor schema is ahead of this binary")

// ForwardIncompatibleError carries the structured versions so the CLI
// formatter can render them without parsing the error string. It wraps
// ErrForwardIncompatible so existing errors.Is checks keep working.
type ForwardIncompatibleError struct {
	Found    int
	Required int
}

func (e *ForwardIncompatibleError) Error() string {
	return fmt.Sprintf("%s: schema is at version %d, this binary requires version %d",
		ErrForwardIncompatible.Error(), e.Found, e.Required)
}

func (e *ForwardIncompatibleError) Unwrap() error { return ErrForwardIncompatible }

// newForwardIncompatible builds the typed error for migrate.go.
func newForwardIncompatible(found, required int) error {
	return &ForwardIncompatibleError{Found: found, Required: required}
}

// ErrUnsupportedSourceVersion is returned by the migration runner when the
// recorded schema_version cannot be reconciled with the available migration
// files (e.g., a negative version or one that exceeds BinarySchemaVersion
// without matching any forward migration).
var ErrUnsupportedSourceVersion = errors.New("monitor schema_version is outside the supported range")

// SchemaVersionTable holds the single-row monotonic integer that records the
// monitor schema version applied to the database. Bumped inside the same
// transaction as the migration step that advances the schema.
const SchemaVersionTable = `
CREATE TABLE IF NOT EXISTS schema_version (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	version INTEGER NOT NULL,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// readSchemaVersion returns the integer stored in schema_version. If the
// table is empty (pre-versioning DB), returns 0 with no error.
func readSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow(`SELECT version FROM schema_version WHERE id = 1`).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read schema_version: %w", err)
	}
	return version, nil
}

// writeSchemaVersion upserts the singleton row with the given version inside
// the supplied transaction.
func writeSchemaVersion(tx *sql.Tx, version int) error {
	if _, err := tx.Exec(
		`INSERT INTO schema_version (id, version, updated_at) VALUES (1, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET version = excluded.version, updated_at = CURRENT_TIMESTAMP`,
		version,
	); err != nil {
		return fmt.Errorf("failed to write schema_version=%d: %w", version, err)
	}
	return nil
}
