package mongo_dump

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/storage"
)

func Test_argsBuilder(t *testing.T) {

	backupPath := filepath.Join("tmp", "backups")
	outPath := filepath.Join(backupPath, "test.archive")

	tests := []struct {
		name    string
		args    *DumpMongoArgs
		want    []string
		wantErr bool
	}{
		{
			name:    "Args with default URI",
			args:    &DumpMongoArgs{Compress: false, Storage: &storage.Params{OutName: "test.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=" + outPath, "--quiet"},
			wantErr: false,
		},
		{
			name: "Args with custom URI",
			args: &DumpMongoArgs{Uri: "mongodb://username@password:192.168.1.34:27017/?timeoutMS=5000", Compress: false, Storage: &storage.Params{OutName: "test.archive"}},
			want: []string{"--uri=mongodb://username@password:192.168.1.34:27017/?timeoutMS=5000", "--out=" + outPath, "--quiet"},
		},
		{
			name:    "Args with compression enabled",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: true, Storage: &storage.Params{OutName: "test.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=" + outPath, "--quiet", "--gzip"},
			wantErr: false,
		},
		{
			name:    "Args with additional arguments",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: false, AdditionalArgs: "--authenticationDatabase=admin", Storage: &storage.Params{OutName: "test.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=" + outPath, "--quiet", "--authenticationDatabase=admin"},
			wantErr: false,
		},
		{
			name:    "Remove duplicate arguments",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: false, AdditionalArgs: "--authenticationDatabase=admin --authenticationDatabase=admin", Storage: &storage.Params{OutName: "test.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=" + outPath, "--quiet", "--authenticationDatabase=admin"},
			wantErr: false,
		},
		{
			name:    "Archive out name remains consistent",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: false, AdditionalArgs: "--archive", Storage: &storage.Params{OutName: "mongo-dev.archive"}},
			want:    []string{"--uri=mongodb://localhost:27017", "--quiet", "--archive=" + filepath.Join(backupPath, "mongo-dev.archive")},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := argsBuilder(tt.args, backupPath)
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
