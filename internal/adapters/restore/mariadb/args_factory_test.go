package mariadb

import (
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestArgsFactory_BuildRestoreArgs(t *testing.T) {
	f := ArgsFactory{}
	spec := ports.RestoreJobSpec{
		Engine: "mariadb", Host: "h", Port: 3306, Username: "u", Password: "p",
		Database: "db", BackupPath: "/staged/db.sql", OnConflict: "ignore",
	}
	want := &RestoreArgs{
		Host: "h", Port: 3306, Username: "u", Password: "p", Database: "db",
		BackupPath: "/staged/db.sql", OnConflict: "ignore",
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
		t.Error("expected error for OplogReplay phase on mariadb")
	}
}
