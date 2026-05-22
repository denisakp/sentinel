//go:build integration

package azure_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/storage/azure"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// azuriteConnString builds the Azurite connection string for the given host:port.
func azuriteConnString(host, port string) string {
	return fmt.Sprintf(
		"DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;"+
			"AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;"+
			"BlobEndpoint=http://%s:%s/devstoreaccount1;",
		host, port,
	)
}

// startAzurite starts an Azurite container and returns (host, port, cleanup).
func startAzurite(t *testing.T, ctx context.Context) (host, port string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "mcr.microsoft.com/azure-storage/azurite:latest",
		ExposedPorts: []string{"10000/tcp"},
		Cmd:          []string{"azurite-blob", "--blobHost", "0.0.0.0", "--loose"},
		WaitingFor:   wait.ForLog("Azurite Blob service is successfully listening").WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start Azurite container: %v", err)
	}
	t.Cleanup(func() {
		if termErr := container.Terminate(context.Background()); termErr != nil {
			t.Logf("Failed to terminate Azurite container: %v", termErr)
		}
	})

	h, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get Azurite host: %v", err)
	}

	mappedPort, err := container.MappedPort(ctx, "10000")
	if err != nil {
		t.Fatalf("Failed to get Azurite port: %v", err)
	}

	return h, mappedPort.Port()
}

// newTestBackend creates an AzureBlobBackend connected to the given Azurite instance.
func newTestBackend(t *testing.T, connStr, container string) *azure.AzureBlobBackend {
	t.Helper()

	cfg := azure.Config{
		AccountName: "devstoreaccount1",
		Container:   container,
		Tier:        "Hot",
		Auth: azure.AuthConfig{
			Type:             "connection_string",
			ConnectionString: connStr,
		},
	}

	backend, err := azure.NewAzureBlobBackend(cfg)
	if err != nil {
		t.Fatalf("NewAzureBlobBackend() error = %v", err)
	}
	return backend
}

// writeTempFile creates a temp file with the given content and returns its path.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "sentinel-*.sql")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		t.Fatalf("failed to write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// TestAzureBlobBackend_Upload_Download_Delete_Roundtrip verifies the complete
// upload → download → delete workflow against a live Azurite instance.
func TestAzureBlobBackend_Upload_Download_Delete_Roundtrip(t *testing.T) {
	ctx := context.Background()
	host, port := startAzurite(t, ctx)

	connStr := azuriteConnString(host, port)
	backend := newTestBackend(t, connStr, "sentinel-test")

	// Create container in Azurite (the SDK creates it on first use if it doesn't exist)
	// Upload a test file
	src := writeTempFile(t, "-- roundtrip test backup")
	dest := "backups/roundtrip.sql"

	if err := backend.Upload(ctx, src, dest); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	// Download and verify content
	downloadDest := filepath.Join(t.TempDir(), "downloaded.sql")
	if err := backend.Download(ctx, dest, downloadDest); err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	got, err := os.ReadFile(downloadDest)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "-- roundtrip test backup" {
		t.Errorf("Download content = %q, want %q", string(got), "-- roundtrip test backup")
	}

	// Delete and verify gone
	if err := backend.Delete(ctx, dest); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	ok, err := backend.Exists(ctx, dest)
	if err != nil {
		t.Fatalf("Exists() post-delete error = %v", err)
	}
	if ok {
		t.Error("Exists() = true after Delete, want false")
	}
}

// TestAzureBlobBackend_List verifies that uploaded blobs are enumerable.
func TestAzureBlobBackend_List(t *testing.T) {
	ctx := context.Background()
	host, port := startAzurite(t, ctx)

	connStr := azuriteConnString(host, port)
	backend := newTestBackend(t, connStr, "sentinel-list-test")

	// Upload two objects
	files := map[string]string{
		"backups/db1.sql": "-- db1 backup",
		"backups/db2.sql": "-- db2 backup",
	}
	for dest, content := range files {
		src := writeTempFile(t, content)
		if err := backend.Upload(ctx, src, dest); err != nil {
			t.Fatalf("Upload(%q) error = %v", dest, err)
		}
	}

	// List should return both
	objects, err := backend.List(ctx, "backups/")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(objects) < 2 {
		t.Errorf("List() returned %d objects, want >= 2", len(objects))
	}

	// Each object should have a non-empty path
	for _, o := range objects {
		if o.Path == "" {
			t.Error("List() returned object with empty Path")
		}
	}
}

// TestAzureBlobBackend_Exists verifies Exists returns false for non-existent blobs.
func TestAzureBlobBackend_Exists(t *testing.T) {
	ctx := context.Background()
	host, port := startAzurite(t, ctx)

	connStr := azuriteConnString(host, port)
	backend := newTestBackend(t, connStr, "sentinel-exists-test")

	ok, err := backend.Exists(ctx, "nonexistent/file.sql")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if ok {
		t.Error("Exists() = true for non-existent blob, want false")
	}
}

// TestAzureBlobBackend_Status verifies Status reflects uploaded backup metadata.
func TestAzureBlobBackend_Status(t *testing.T) {
	ctx := context.Background()
	host, port := startAzurite(t, ctx)

	connStr := azuriteConnString(host, port)
	backend := newTestBackend(t, connStr, "sentinel-status-test")

	// Initially no backups → BackupCount should be 0
	status, err := backend.Status(ctx)
	if err != nil {
		t.Fatalf("Status() initial error = %v", err)
	}
	if !status.Reachable {
		t.Errorf("Status().Reachable = false for connected backend")
	}
	if status.BackupCount != 0 {
		t.Errorf("Status().BackupCount = %d, want 0 before any uploads", status.BackupCount)
	}

	// Upload one backup
	src := writeTempFile(t, "-- status test backup content with some data")
	if err := backend.Upload(ctx, src, "backups/status-check.sql"); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	// Status should now reflect one backup
	status, err = backend.Status(ctx)
	if err != nil {
		t.Fatalf("Status() post-upload error = %v", err)
	}
	if !status.Reachable {
		t.Error("Status().Reachable = false after upload")
	}
	if status.BackupCount < 1 {
		t.Errorf("Status().BackupCount = %d, want >= 1 after upload", status.BackupCount)
	}
	if status.TotalSizeBytes <= 0 {
		t.Errorf("Status().TotalSizeBytes = %d, want > 0", status.TotalSizeBytes)
	}
	if status.LastBackup == nil {
		t.Error("Status().LastBackup = nil, want non-nil after upload")
	}
}

// TestAzureBlobBackend_InvalidConfig verifies that missing required fields are rejected.
func TestAzureBlobBackend_InvalidConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         azure.Config
		wantErr     bool
		errContains string
	}{
		{
			name: "missing account_name",
			cfg: azure.Config{
				AccountName: "",
				Container:   "mycontainer",
				Auth:        azure.AuthConfig{Type: "connection_string", ConnectionString: "x"},
			},
			wantErr:     true,
			errContains: "account_name",
		},
		{
			name: "missing container",
			cfg: azure.Config{
				AccountName: "myaccount",
				Container:   "",
				Auth:        azure.AuthConfig{Type: "connection_string", ConnectionString: "x"},
			},
			wantErr:     true,
			errContains: "container",
		},
		{
			name: "invalid tier",
			cfg: azure.Config{
				AccountName: "myaccount",
				Container:   "mycontainer",
				Tier:        "Glacier",
				Auth:        azure.AuthConfig{Type: "connection_string", ConnectionString: "x"},
			},
			wantErr:     true,
			errContains: "tier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := azure.NewAzureBlobBackend(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAzureBlobBackend() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}
