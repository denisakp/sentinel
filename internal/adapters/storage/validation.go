package storage

import (
	"fmt"
	"strings"
)

// ValidateStorageType validates the storage type
// and returns an error if the storage type is not supported
func ValidateStorageType(storageType string) error {
	validStorage := map[string]bool{
		"local":        true,
		"s3":           true,
		"gcs":          true,
		"google-drive": true,
		"azure":        true,
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

	if param.StorageType == "gcs" {
		if err := ValidateGCSConfig(param.GCSBucket); err != nil {
			return err
		}
	}

	return nil
}

// ValidateGCSConfig validates the required GCS storage fields.
func ValidateGCSConfig(bucket string) error {
	if strings.TrimSpace(bucket) == "" {
		return fmt.Errorf("gcs: bucket is required")
	}
	return nil
}

// ValidateAzureConfig validates Azure Blob Storage configuration fields.
// Parameters mirror config.AzureConfig fields to avoid circular imports.
//
// accountName: Azure storage account name (required)
// container:   Blob container name (required)
// tier:        Access tier — Hot, Cool, Archive (empty defaults to Hot)
// authType:    Auth method — managed_identity, connection_string, sas_token
func ValidateAzureConfig(accountName, container, tier, authType string) error {
	if strings.TrimSpace(accountName) == "" {
		return fmt.Errorf("azure: account_name is required")
	}

	if strings.TrimSpace(container) == "" {
		return fmt.Errorf("azure: container is required")
	}

	if tier != "" {
		validTiers := map[string]bool{
			"Hot":     true,
			"Cool":    true,
			"Archive": true,
		}
		if !validTiers[tier] {
			return fmt.Errorf("azure: tier must be Hot, Cool, or Archive (got %q)", tier)
		}
	}

	if authType != "" {
		validAuthTypes := map[string]bool{
			"managed_identity":  true,
			"connection_string": true,
			"sas_token":         true,
		}
		if !validAuthTypes[authType] {
			return fmt.Errorf(
				"azure: auth.type must be managed_identity, connection_string, or sas_token (got %q)",
				authType,
			)
		}
	}

	return nil
}
