package storage

import (
	"fmt"
	"github.com/denisakp/sentinel/internal/storage/gdrive"
	"github.com/denisakp/sentinel/internal/storage/local"
	"github.com/denisakp/sentinel/internal/utils"
)

// Storage interface defines the methods that a storage type must implement
type Storage interface {
	GetBackupPath(outName string) (string, error)  // GetBackupPath returns the path to store the backup
	WriteBackup(data []byte, outName string) error // WriteBackup writes the backup data to the specified path
}

type Params struct {
	OutName              string
	StorageType          string
	LocalPath            string
	GoogleDriveFolderId  string
	GoogleServiceAccount string
}

// NewStorage returns a new storage based on the storage type
func NewStorage(p *Params) (Storage, error) {
	// set the default storage type to local if not provided
	storageType := utils.DefaultValue(p.StorageType, "local")

	switch storageType {
	case "local":
		return &local.LocalStorage{}, nil // return a new instance of LocalStorage
	case "s3":
		return nil, fmt.Errorf("unsupported storage type %s", storageType)
	case "google-drive":
		gDriveStorage, err := gdrive.NewGoogleDriveStorage(p.GoogleDriveFolderId, p.GoogleServiceAccount)
		if err != nil {
			return nil, fmt.Errorf("error initializing Google Drive storage: %w", err)
		}
		return gDriveStorage, nil
	default:
		return nil, fmt.Errorf("unsupported storage type %s", storageType)
	}
}
