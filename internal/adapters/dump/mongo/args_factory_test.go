package mongo

import (
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestArgsFactory_BuildDumpArgs(t *testing.T) {
	tests := []struct {
		name string
		spec ports.DumpJobSpec
		want *DumpMongoArgs
	}{
		{
			name: "no gzip",
			spec: ports.DumpJobSpec{Engine: "mongodb", URI: "mongodb://h", Database: "db", AdditionalArgs: "--numParallelCollections=4"},
			want: &DumpMongoArgs{Uri: "mongodb://h", Database: "db", AdditionalArgs: "--numParallelCollections=4"},
		},
		{
			name: "gzip",
			spec: ports.DumpJobSpec{Engine: "mongodb", URI: "mongodb://h", Database: "db", Compress: true},
			want: &DumpMongoArgs{Uri: "mongodb://h", Database: "db", Compress: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ArgsFactory{}.BuildDumpArgs(tc.spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mismatch:\n got  %+v\n want %+v", got, tc.want)
			}
		})
	}
}
