package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	storage "github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/storage/azure"
	"github.com/denisakp/sentinel/internal/storage/gcs"
	"github.com/denisakp/sentinel/internal/storage/gdrive"
	"github.com/denisakp/sentinel/internal/storage/local"
	sentinel_s3 "github.com/denisakp/sentinel/internal/storage/sentinel_s3"
)

// Compile-time interface compliance assertions.
// These will fail at compile time if any backend type no longer satisfies the interface.
var (
	_ ports.StorageBackend = (*local.LocalBackend)(nil)
	_ ports.StorageBackend = (*sentinel_s3.S3Backend)(nil)
	_ ports.StorageBackend = (*gdrive.GDriveBackend)(nil)
	_ ports.StorageBackend = (*azure.AzureBlobBackend)(nil)
	_ ports.StorageBackend = (*gcs.GCSBackend)(nil)
)

// TestLocalBackend_InterfaceSmoke verifies the LocalBackend honours the
// StorageBackend contract: Upload → List → Exists → Delete.
func TestLocalBackend_InterfaceSmoke(t *testing.T) {
	// Arrange: root directory
	root := t.TempDir()
	backend := local.NewLocalBackend(root)

	ctx := context.Background()

	// Create a source file to upload
	srcFile := filepath.Join(t.TempDir(), "test-backup.sql")
	if err := os.WriteFile(srcFile, []byte("-- sentinel test backup content"), 0o644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	destPath := "backups/test-backup.sql"

	// Act: Upload
	if err := backend.Upload(ctx, srcFile, destPath); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	// Assert: Exists returns true
	ok, err := backend.Exists(ctx, destPath)
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !ok {
		t.Error("Exists() = false, want true after Upload")
	}

	// Assert: List contains the uploaded object
	objects, err := backend.List(ctx, "backups/")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(objects) == 0 {
		t.Fatal("List() returned 0 objects, expected at least 1")
	}
	found := false
	for _, o := range objects {
		if strings.HasSuffix(o.Path, "test-backup.sql") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List() did not contain uploaded object %q", destPath)
	}

	// Act: Download to a new destination
	downloadDest := filepath.Join(t.TempDir(), "downloaded.sql")
	if err := backend.Download(ctx, destPath, downloadDest); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	gotData, err := os.ReadFile(downloadDest)
	if err != nil {
		t.Fatalf("ReadFile() after Download error = %v", err)
	}
	if string(gotData) != "-- sentinel test backup content" {
		t.Errorf("Download content mismatch: got %q", string(gotData))
	}

	// Act: Delete
	if err := backend.Delete(ctx, destPath); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Assert: Exists returns false after deletion
	ok, err = backend.Exists(ctx, destPath)
	if err != nil {
		t.Fatalf("Exists() post-delete error = %v", err)
	}
	if ok {
		t.Error("Exists() = true after Delete, want false")
	}
}

// TestValidateAzureConfig_Validation exercises the azure config validation helper.
func TestValidateAzureConfig_Validation(t *testing.T) {
	tests := []struct {
		name        string
		accountName string
		container   string
		tier        string
		authType    string
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid minimal config",
			accountName: "myaccount",
			container:   "mycontainer",
			tier:        "",
			authType:    "",
			wantErr:     false,
		},
		{
			name:        "valid full config",
			accountName: "myaccount",
			container:   "mycontainer",
			tier:        "Cool",
			authType:    "connection_string",
			wantErr:     false,
		},
		{
			name:        "missing account_name",
			accountName: "",
			container:   "mycontainer",
			tier:        "",
			authType:    "",
			wantErr:     true,
			errContains: "account_name",
		},
		{
			name:        "missing container",
			accountName: "myaccount",
			container:   "",
			tier:        "",
			authType:    "",
			wantErr:     true,
			errContains: "container",
		},
		{
			name:        "invalid tier",
			accountName: "myaccount",
			container:   "mycontainer",
			tier:        "Premium",
			authType:    "",
			wantErr:     true,
			errContains: "tier",
		},
		{
			name:        "invalid auth type",
			accountName: "myaccount",
			container:   "mycontainer",
			tier:        "Hot",
			authType:    "basic",
			wantErr:     true,
			errContains: "auth.type",
		},
		{
			name:        "Archive tier is valid",
			accountName: "myaccount",
			container:   "mycontainer",
			tier:        "Archive",
			authType:    "sas_token",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.ValidateAzureConfig(tt.accountName, tt.container, tt.tier, tt.authType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAzureConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}
