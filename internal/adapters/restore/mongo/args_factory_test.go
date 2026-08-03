package mongo

import (
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestArgsFactory_BuildRestoreArgs_Primary(t *testing.T) {
	f := ArgsFactory{}
	spec := ports.RestoreJobSpec{
		Engine: "mongodb", URI: "mongodb://h", Database: "db",
		BackupPath: "/staged/dump", OnConflict: "replace",
		Gzip: true, Archive: true, AdditionalArgs: "--gzip",
	}
	want := &RestoreArgs{
		URI: "mongodb://h", Database: "db", BackupPath: "/staged/dump",
		OnConflict: "replace", Gzip: true, Archive: true, AdditionalArgs: "--gzip",
	}
	got, err := f.BuildRestoreArgs(spec, ports.PrimaryRestore)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("primary mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestArgsFactory_BuildRestoreArgs_OplogReplay(t *testing.T) {
	f := ArgsFactory{}
	ok := ports.RestoreJobSpec{Engine: "mongodb", URI: "mongodb://h", ArchivePath: "/oplog.bson"}
	got, err := f.BuildRestoreArgs(ok, ports.OplogReplay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &OplogReplayArgs{URI: "mongodb://h", ArchivePath: "/oplog.bson"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("oplog mismatch:\n got  %+v\n want %+v", got, want)
	}

	for _, tc := range []struct {
		name string
		spec ports.RestoreJobSpec
	}{
		{"non-mongodb engine", ports.RestoreJobSpec{Engine: "postgres", URI: "mongodb://h", ArchivePath: "/a"}},
		{"empty archive path", ports.RestoreJobSpec{Engine: "mongodb", URI: "mongodb://h", ArchivePath: "  "}},
		{"empty uri", ports.RestoreJobSpec{Engine: "mongodb", URI: "", ArchivePath: "/a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.BuildRestoreArgs(tc.spec, ports.OplogReplay); err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}
