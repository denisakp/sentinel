package factory_test

import (
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/storage/factory"
)

func TestNewBackupBackend(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/dev/null")

	tests := []struct {
		name        string
		params      *storage.Params
		wantErr     bool
		errContains string
	}{
		{
			name:   "local default empty storage type",
			params: &storage.Params{LocalPath: t.TempDir()},
		},
		{
			name:   "local explicit",
			params: &storage.Params{StorageType: "local", LocalPath: t.TempDir()},
		},
		{
			name: "s3",
			params: &storage.Params{
				StorageType:        "s3",
				AWSBucket:          "test-bucket",
				AWSRegion:          "us-east-1",
				AWSAccessKeyID:     "key",
				AWSSecretAccessKey: "secret",
			},
		},
		{
			name: "azure connection string",
			params: &storage.Params{
				StorageType:         "azure",
				AzureStorageAccount: "acct",
				AzureContainer:      "cont",
				AzureStorageKey:     "a2V5",
			},
		},
		{
			name:        "unsupported",
			params:      &storage.Params{StorageType: "bogus"},
			wantErr:     true,
			errContains: "unsupported backup backend type",
		},
		{
			name:        "nil params",
			params:      nil,
			wantErr:     true,
			errContains: "storage params are required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := factory.NewBackupBackend(tt.params)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewBackupBackend() err=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("err %q missing %q", err.Error(), tt.errContains)
				}
				return
			}
			if got == nil {
				t.Error("nil backend on success")
			}
		})
	}
}
