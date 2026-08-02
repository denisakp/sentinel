package mysqlargs

import (
	"reflect"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestBuildArgs_MySQLEmptyPasswordEmitsSkipPassword(t *testing.T) {
	got, err := BuildArgs(Input{Username: "u", Database: "db"}, FlavorMySQL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--host=127.0.0.1", "--port=3306", "--user=u", "--skip-password", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mismatch:\n got  %v\n want %v", got, want)
	}
}

func TestBuildArgs_MariaDBEmptyPasswordNoSkipPassword(t *testing.T) {
	got, err := BuildArgs(Input{Username: "u", Database: "db"}, FlavorMariaDB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--host=127.0.0.1", "--port=3306", "--user=u", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mismatch:\n got  %v\n want %v", got, want)
	}
}

func TestBuildArgs_NonEmptyPasswordNoSkip(t *testing.T) {
	got, _ := BuildArgs(Input{Host: "h", Port: "3307", Username: "u", Password: "p", Database: "db"}, FlavorMySQL)
	want := []string{"--host=h", "--port=3307", "--user=u", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mismatch:\n got  %v\n want %v", got, want)
	}
}

func TestBuildArgs_AdditionalArgs(t *testing.T) {
	got, err := BuildArgs(Input{Username: "u", Password: "p", Database: "db", AdditionalArgs: "--single-transaction"}, FlavorMariaDB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// order: host/port/user, additional-args, database
	want := []string{"--host=127.0.0.1", "--port=3306", "--user=u", "--single-transaction", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mismatch:\n got  %v\n want %v", got, want)
	}
}

func TestBuildArgs_TLSEngineString(t *testing.T) {
	tls := &ports.Config{Enabled: true, CACertPath: "/ca.pem"}
	mysqlArgs, _ := BuildArgs(Input{Username: "u", Password: "p", Database: "db", TLS: tls}, FlavorMySQL)
	mariaArgs, _ := BuildArgs(Input{Username: "u", Password: "p", Database: "db", TLS: tls}, FlavorMariaDB)
	// The two flavors must differ only in the TLS flags produced by the engine string.
	if reflect.DeepEqual(mysqlArgs, mariaArgs) {
		t.Errorf("expected differing TLS args between mysql and mariadb flavors")
	}
}

func TestValidateRequired(t *testing.T) {
	if err := ValidateRequired("u", "db"); err != nil {
		t.Errorf("valid input errored: %v", err)
	}
	if err := ValidateRequired("u", ""); err == nil {
		t.Error("missing database should error")
	}
	if err := ValidateRequired("", "db"); err == nil {
		t.Error("missing username should error")
	}
}

func TestBuildArgs_ValidationPropagates(t *testing.T) {
	if _, err := BuildArgs(Input{Username: "u"}, FlavorMySQL); err == nil {
		t.Error("missing database should error from BuildArgs")
	}
}
