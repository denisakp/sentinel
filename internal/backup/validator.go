package backup

import "fmt"

// ValidateDbType validates the database type provided by the user
func ValidateDbType(dbType string) error {
	validTypes := map[string]bool{
		"mysql":    true,
		"postgres": true,
		"mariadb":  true,
		"mongodb":  true,
	}

	if _, ok := validTypes[dbType]; !ok {
		return fmt.Errorf("invalid database type: %s", dbType)
	}

	return nil
}
