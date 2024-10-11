package backup

import "fmt"

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
