package mongo_dump

import "fmt"

// validateRequiredArgs validates the required arguments
func validateRequiredArgs(da *DumpMongoArgs) error {
	if da.Uri == "" {
		return fmt.Errorf("uri is missing")
	}
	return nil
}
