package pg

import (
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

// TestArgsFactory_BuildDumpArgs asserts the factory produces exactly the
// *PgDumpArgs the former config.BuildPgDumpArgs produced. Storage/TLS/PITR
// fields stay zero (set elsewhere).
func TestArgsFactory_BuildDumpArgs(t *testing.T) {
	tests := []struct {
		name string
		spec ports.DumpJobSpec
		want *PgDumpArgs
	}{
		{
			name: "plain",
			spec: ports.DumpJobSpec{
				Engine: "postgres", Host: "h", Port: "5432", Username: "u",
				Password: "p", Database: "db", AdditionalArgs: "--verbose",
				CompressionLevel: 1,
			},
			want: &PgDumpArgs{
				Host: "h", Port: "5432", Username: "u", Password: "p",
				Database: "db", AdditionalArgs: "--verbose", CompressionLevel: 1,
			},
		},
		{
			name: "compression + format",
			spec: ports.DumpJobSpec{
				Engine: "postgres", Host: "h", Port: "5432", Database: "db",
				PgOutFormat: "custom", Compress: true,
				CompressionAlgorithm: "zstd", CompressionLevel: 6,
			},
			want: &PgDumpArgs{
				Host: "h", Port: "5432", Database: "db", PgOutFormat: "custom",
				Compress: true, CompressionAlgorithm: "zstd", CompressionLevel: 6,
			},
		},
		{
			name: "zero port",
			spec: ports.DumpJobSpec{Engine: "postgres", Database: "db", CompressionLevel: 1},
			want: &PgDumpArgs{Database: "db", CompressionLevel: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ArgsFactory{}.BuildDumpArgs(tc.spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BuildDumpArgs mismatch:\n got  %+v\n want %+v", got, tc.want)
			}
		})
	}
}
