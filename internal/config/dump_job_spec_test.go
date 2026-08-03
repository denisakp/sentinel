package config

import (
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

// TestBuildDumpJobSpec covers the YAML BackupJob -> ports.DumpJobSpec
// translation, including DatabaseOptions resolution.
func TestBuildDumpJobSpec(t *testing.T) {
	tests := []struct {
		name string
		job  BackupJob
		want ports.DumpJobSpec
	}{
		{
			name: "postgres defaults compression level 1",
			job:  BackupJob{Type: "postgres", Host: "h", Port: 5432, Username: "u", Database: "db"},
			want: ports.DumpJobSpec{Engine: "postgres", Host: "h", Port: "5432", Username: "u", Password: "pw", Database: "db", AdditionalArgs: "-a", CompressionLevel: 1},
		},
		{
			name: "postgres compression algo + level + format",
			job: BackupJob{Type: "postgres", Port: 5432, Database: "db", DatabaseOptions: map[string]interface{}{
				"pg_out_format": "custom", "pg_compression_algo": "zstd", "pg_compression_level": 6,
			}},
			want: ports.DumpJobSpec{Engine: "postgres", Port: "5432", Password: "pw", Database: "db", AdditionalArgs: "-a", PgOutFormat: "custom", Compress: true, CompressionAlgorithm: "zstd", CompressionLevel: 6},
		},
		{
			name: "mongodb gzip",
			job:  BackupJob{Type: "mongodb", URI: "mongodb://h", Database: "db", DatabaseOptions: map[string]interface{}{"gzip": true}},
			want: ports.DumpJobSpec{Engine: "mongodb", URI: "mongodb://h", Password: "pw", Database: "db", AdditionalArgs: "-a", Compress: true},
		},
		{
			name: "mysql zero port -> empty string",
			job:  BackupJob{Type: "mysql", Database: "db"},
			want: ports.DumpJobSpec{Engine: "mysql", Password: "pw", Database: "db", AdditionalArgs: "-a"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildDumpJobSpec(tc.job, "pw", "-a")
			if got != tc.want {
				t.Errorf("BuildDumpJobSpec mismatch:\n got  %+v\n want %+v", got, tc.want)
			}
		})
	}
}
