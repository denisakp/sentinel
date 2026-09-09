package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storage "github.com/denisakp/sentinel/internal/adapters/storage"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/storagetesting"
)

// retentionFixture wires a job whose history names three artifacts, and lets the
// caller decide which of them actually exist in storage.
func retentionFixture(t *testing.T, present []string, all []string) (*config.Configuration, *storagetesting.MockBackend, ports.Recorder) {
	t.Helper()
	mock := storagetesting.NewMockBackend()
	for _, k := range present {
		mock.PutBytes(k, []byte("x"))
	}
	stubRetentionBackend(t, func(_ *storage.BackendParams) (ports.StorageBackend, error) {
		return mock, nil
	})

	historyPath := filepath.Join(t.TempDir(), "history.db")
	cfg := &config.Configuration{
		Version:       "1.0",
		HistoryDBPath: historyPath,
		Databases: map[string]config.BackupJob{
			"gcs-job": {
				Name:      "gcs-job",
				Type:      "postgres",
				Storage:   config.StorageConfig{Type: "gcs", GCSBucket: "bucket-a"},
				Retention: config.RetentionPolicy{KeepLast: 1},
			},
		},
	}

	mon := newRetentionTestMonitor(t, historyPath)
	now := time.Now().UTC()
	for i, name := range all {
		if err := mon.RecordExecution(context.Background(), &ports.Execution{
			BackupName: "gcs-job", DatabaseType: "postgres",
			Timestamp: now.Add(-time.Duration(len(all)-i) * time.Hour),
			Status:    "success", FilePath: "gs://bucket-a/" + name, FileSizeBytes: 100,
		}); err != nil {
			t.Fatalf("RecordExecution() error = %v", err)
		}
	}
	return cfg, mock, mon
}

// TestRetentionDoesNotReportDeletingWhatWasNotThere is the regression guard for
// #182.
//
// Deletion was reported from the Delete call alone. Rule R4 of the storage
// contract makes Delete idempotent, so every backend returns nil for an object
// that is not there, and retention could not tell "removed it" from "there was
// nothing to remove". It then deleted the history row, so the history forgot an
// object that, in the GCS case, was still in the bucket: the backend had been
// resolving a flattened key and deleting nothing at all.
//
// Storage grew while every signal said it was being pruned.
func TestRetentionDoesNotReportDeletingWhatWasNotThere(t *testing.T) {
	// "old.sql" is named in history but absent from the bucket.
	cfg, _, mon := retentionFixture(t, []string{"mid.sql", "new.sql"}, []string{"old.sql", "mid.sql", "new.sql"})

	deleted, err := applyJobRetention(context.Background(), cfg, mon, "gcs-job", false)
	if err == nil {
		t.Fatal("an artifact that could not be found must be reported, not counted as deleted")
	}
	if !strings.Contains(err.Error(), "old.sql") {
		t.Errorf("the error does not name the artifact it could not delete.\ngot: %v", err)
	}

	for _, d := range deleted {
		if strings.HasSuffix(d.FilePath, "old.sql") {
			t.Error("old.sql was reported as deleted although no object existed at that path")
		}
	}

	// Its history row must survive: either the object was removed out of band and
	// the row is stale, or the path is wrong. Keeping the row is what lets anyone
	// notice; deleting it destroys the evidence either way.
	records, ferr := fetchRetentionRecords(context.Background(), mon, "gcs-job")
	if ferr != nil {
		t.Fatalf("fetchRetentionRecords() error = %v", ferr)
	}
	var foundOld bool
	for _, r := range records {
		if strings.HasSuffix(r.FilePath, "old.sql") {
			foundOld = true
		}
	}
	if !foundOld {
		t.Error("the history row for old.sql was deleted even though no artifact was removed")
	}
}

// TestPartialFailureStillClearsHistoryForWhatWasDeleted is the other half of
// #182. The old code returned on the first error, before touching history, so a
// partial failure left every successfully deleted artifact still recorded as
// present: the same inconsistency in the opposite direction.
func TestPartialFailureStillClearsHistoryForWhatWasDeleted(t *testing.T) {
	cfg, _, mon := retentionFixture(t, []string{"mid.sql", "new.sql"}, []string{"old.sql", "mid.sql", "new.sql"})

	if _, err := applyJobRetention(context.Background(), cfg, mon, "gcs-job", false); err == nil {
		t.Fatal("expected the missing artifact to be reported")
	}

	records, err := fetchRetentionRecords(context.Background(), mon, "gcs-job")
	if err != nil {
		t.Fatalf("fetchRetentionRecords() error = %v", err)
	}
	// keep_last: 1 keeps new.sql. mid.sql was really deleted, so its row must be
	// gone despite old.sql having failed in the same run. old.sql's row stays.
	for _, r := range records {
		if strings.HasSuffix(r.FilePath, "mid.sql") {
			t.Error("mid.sql was deleted from storage but its history row survived, " +
				"because another candidate in the same run failed")
		}
	}
}
