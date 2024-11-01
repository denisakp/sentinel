package storage

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/storage/local"
	"github.com/denisakp/sentinel/internal/utils"
)

// Storage interface defines the methods that a storage type must implement
type Storage interface {
	GetBackupPath(outName string) (string, error)  // GetBackupPath returns the path to store the backup
	WriteBackup(data []byte, outName string) error // WriteBackup writes the backup data to the specified path
}

// NewStorage returns a new storage based on the storage type
func NewStorage(storageType string) (Storage, error) {
	// set the default storage type to local if not provided
	storageType = utils.DefaultValue(storageType, "local")

	// validate the storage type before creating a new storage
	if err := ValidateStorageType(storageType); err != nil {
		return nil, err
	}

	switch storageType {
	case "local":
		return &local.LocalStorage{}, nil // return a new instance of LocalStorage
	case "s3":
		return nil, fmt.Errorf("unsupported storage type %s", storageType)
	case "google-drive":
		return nil, fmt.Errorf("unsupported storage type %s", storageType)
	default:
		return nil, fmt.Errorf("unsupported storage type %s", storageType)
	}
}
