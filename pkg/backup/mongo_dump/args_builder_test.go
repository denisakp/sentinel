package mongo_dump

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/storage"
)

func Test_argsBuilder(t *testing.T) {

	backupPath := filepath.Join("tmp", "backups")
	outPath := filepath.Join(backupPath, "test.archive")
	staging := filepath.Join(backupPath, ".staging", "j1", "dump.archive")

	tests := []struct {
		name           string
		args           *DumpMongoArgs
		stagingArchive string
		want           []string
		wantErr        bool
	}{
		{
			name:    "Args with default URI (local)",
			args:    &DumpMongoArgs{Compress: false, Storage: &storage.Params{OutName: "test.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=" + outPath, "--quiet"},
			wantErr: false,
		},
		{
			name: "Args with custom URI (local)",
			args: &DumpMongoArgs{Uri: "mongodb://username@password:192.168.1.34:27017/?timeoutMS=5000", Compress: false, Storage: &storage.Params{OutName: "test.archive"}},
			want: []string{"--uri=mongodb://username@password:192.168.1.34:27017/?timeoutMS=5000", "--out=" + outPath, "--quiet"},
		},
		{
			name:    "Args with compression enabled (local)",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: true, Storage: &storage.Params{OutName: "test.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=" + outPath, "--quiet", "--gzip"},
			wantErr: false,
		},
		{
			name:    "Args with additional arguments (local)",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: false, AdditionalArgs: "--authenticationDatabase=admin", Storage: &storage.Params{OutName: "test.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=" + outPath, "--quiet", "--authenticationDatabase=admin"},
			wantErr: false,
		},
		{
			name:    "Remove duplicate arguments (local)",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: false, AdditionalArgs: "--authenticationDatabase=admin --authenticationDatabase=admin", Storage: &storage.Params{OutName: "test.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=" + outPath, "--quiet", "--authenticationDatabase=admin"},
			wantErr: false,
		},
		{
			name:    "Operator --archive omits --out (local)",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: false, AdditionalArgs: "--archive", Storage: &storage.Params{OutName: "mongo-dev.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--quiet", "--archive"},
			wantErr: false,
		},
		{
			name:           "Remote s3 forces --archive staging",
			args:           &DumpMongoArgs{Uri: "mongodb://localhost:27017", Storage: &storage.Params{StorageType: "s3", OutName: "test.archive"}},
			stagingArchive: staging,
			want:           []string{"--uri=mongodb://localhost:27017", "--archive=" + staging, "--quiet"},
		},
		{
			name:           "Remote gcs forces --archive staging",
			args:           &DumpMongoArgs{Uri: "mongodb://localhost:27017", Storage: &storage.Params{StorageType: "gcs", OutName: "test.archive"}},
			stagingArchive: staging,
			want:           []string{"--uri=mongodb://localhost:27017", "--archive=" + staging, "--quiet"},
		},
		{
			name:           "Remote google-drive forces --archive staging",
			args:           &DumpMongoArgs{Uri: "mongodb://localhost:27017", Storage: &storage.Params{StorageType: "google-drive", OutName: "test.archive"}},
			stagingArchive: staging,
			want:           []string{"--uri=mongodb://localhost:27017", "--archive=" + staging, "--quiet"},
		},
		{
			name:           "Remote azure forces --archive staging",
			args:           &DumpMongoArgs{Uri: "mongodb://localhost:27017", Storage: &storage.Params{StorageType: "azure", OutName: "test.archive"}},
			stagingArchive: staging,
			want:           []string{"--uri=mongodb://localhost:27017", "--archive=" + staging, "--quiet"},
		},
		{
			name:           "Remote + --gzip keeps both",
			args:           &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: true, Storage: &storage.Params{StorageType: "s3", OutName: "test.archive"}},
			stagingArchive: staging + ".gz",
			want:           []string{"--uri=mongodb://localhost:27017", "--archive=" + staging + ".gz", "--quiet", "--gzip"},
		},
		{
			name:           "Remote + operator --archive=value keeps operator verbatim",
			args:           &DumpMongoArgs{Uri: "mongodb://localhost:27017", AdditionalArgs: "--archive=/tmp/op.archive", Storage: &storage.Params{StorageType: "s3", OutName: "test.archive"}},
			stagingArchive: staging,
			want:           []string{"--uri=mongodb://localhost:27017", "--quiet", "--archive=/tmp/op.archive"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, material, err := argsBuilder(tt.args, backupPath, tt.stagingArchive)
			defer material.Close()
			if (err != nil) != tt.wantErr {
				t.Errorf("argsBuilder() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("argsBuilder() got = %v, want %v", got, tt.want)
			}
		})
	}
}
