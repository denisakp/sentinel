package pg

import (
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestArgsFactory_BuildRestoreArgs(t *testing.T) {
	f := ArgsFactory{}
	spec := ports.RestoreJobSpec{
		Engine: "postgres", Host: "h", Port: 5432, Username: "u", Password: "p",
		Database: "db", BackupPath: "/staged/db.dump", OnConflict: "replace",
		AllowCascade: true, AdditionalArgs: "--clean",
	}
	want := &RestoreArgs{
		Host: "h", Port: 5432, Username: "u", Password: "p", Database: "db",
		BackupPath: "/staged/db.dump", OnConflict: "replace", AllowCascade: true,
		AdditionalArgs: "--clean",
	}
	got, err := f.BuildRestoreArgs(spec, ports.PrimaryRestore)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mismatch:\n got  %+v\n want %+v", got, want)
	}
	_, errPhase := f.BuildRestoreArgs(spec, ports.OplogReplay)
	if errPhase == nil {
		t.Error("expected error for OplogReplay phase on postgres, got nil")
	}
}
