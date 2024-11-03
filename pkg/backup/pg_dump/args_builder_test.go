package pg_dump

import (
	"reflect"
	"testing"
)

func TestPgDumpArgsBuilder(t *testing.T) {
	tests := []struct {
		name    string
		args    *PgDumpArgs
		want    []string
		wantErr bool
	}{
		{
			name:    "Valid args without compression",
			args:    &PgDumpArgs{Host: "192.168.1.26", Port: "5423", Username: "test", Database: "test", OutName: "test.sql", PgOutFormat: "p", Compress: false},
			want:    []string{"--host=192.168.1.26", "--port=5423", "--username=test", "--dbname=test", "--file=test.sql", "--format=p"},
			wantErr: false,
		},
		{
			name:    "Database missing - error expected",
			args:    &PgDumpArgs{Username: "test", OutName: "test.sql", PgOutFormat: "p", Compress: false},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Username missing - error expected",
			args:    &PgDumpArgs{Host: "localhost", Port: "5432", Database: "test", OutName: "test.sql", PgOutFormat: "p", Compress: false},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Default host and port with compression",
			args:    &PgDumpArgs{Username: "test", Database: "test", OutName: "test.backup", PgOutFormat: "c", Compress: true, CompressionAlgorithm: "gzip", CompressionLevel: 4},
			want:    []string{"--host=127.0.0.1", "--port=5432", "--username=test", "--dbname=test", "--file=test.backup", "--format=c", "--compress=gzip:4"},
			wantErr: false,
		},
		{
			name:    "Invalid compression algorithm - error expected",
			args:    &PgDumpArgs{Username: "test", Database: "test", OutName: "test.backup", PgOutFormat: "c", Compress: true, CompressionAlgorithm: "invalid"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid compression level - error expected",
			args:    &PgDumpArgs{Username: "user", Database: "test", OutName: "test.backup", PgOutFormat: "c", Compress: true, CompressionAlgorithm: "gzip", CompressionLevel: 10},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Additional args with no duplicates",
			args: &PgDumpArgs{Username: "test", Database: "test", OutName: "test.backup", PgOutFormat: "c", Compress: false, AdditionalArgs: "--attribute-inserts --no-privileges"},
			want: []string{"--host=127.0.0.1", "--port=5432", "--username=test", "--dbname=test", "--file=test.backup", "--format=c", "--attribute-inserts", "--no-privileges"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := argsBuilder(tt.args)
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
