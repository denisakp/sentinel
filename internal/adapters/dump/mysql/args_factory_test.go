package mysql

import (
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestArgsFactory_BuildDumpArgs(t *testing.T) {
	spec := ports.DumpJobSpec{
		Engine: "mysql", Host: "h", Port: "3306", Username: "u",
		Password: "p", Database: "db", AdditionalArgs: "--single-transaction",
	}
	want := &MySqlDumpArgs{
		Host: "h", Port: "3306", Username: "u", Password: "p",
		Database: "db", AdditionalArgs: "--single-transaction",
	}
	got, err := ArgsFactory{}.BuildDumpArgs(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mismatch:\n got  %+v\n want %+v", got, want)
	}
}
