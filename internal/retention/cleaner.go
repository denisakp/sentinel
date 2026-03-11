package retention

import (
	"fmt"
	"os"
	"time"
)

// DeleteCandidates removes backup files from storage for supported backends.
func DeleteCandidates(candidates []BackupCandidate, storageType string) ([]DeletedBackup, []error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	if storageType != "local" {
		return nil, []error{fmt.Errorf("retention delete not supported for storage type '%s'", storageType)}
	}

	deleted := make([]DeletedBackup, 0, len(candidates))
	var errs []error
	for _, cand := range candidates {
		if err := os.RemoveAll(cand.FilePath); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete %s: %w", cand.FilePath, err))
			continue
		}
		deleted = append(deleted, DeletedBackup{
			FilePath:      cand.FilePath,
			FileSize:      cand.FileSize,
			DeletionTime:  nowUTC(),
			ReasonDeleted: cand.ReasonDeleted,
		})
	}
	return deleted, errs
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
