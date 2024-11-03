package storage

import (
	"fmt"
)

// ValidateStorageType validates the storage type
// and returns an error if the storage type is not supported
func ValidateStorageType(storageType string) error {
	validStorage := map[string]bool{
		"local":        true,
		"s3":           true,
		"google-drive": true,
	}

	if _, ok := validStorage[storageType]; !ok {
		return fmt.Errorf("unsupported storage type: %s", storageType)
	}

	return nil
}

func ValidateStorage(param *Params) error {
	if err := ValidateStorageType(param.StorageType); err != nil {
		return err
	}

	if param.StorageType == "google-drive" {
		if param.GoogleDriveFolderId == "" {
			return fmt.Errorf("google Drive folder ID is required")
		}

		if param.GoogleServiceAccount == "" {
			return fmt.Errorf("google Drive service account file is required")
		}
	}

	return nil
}
