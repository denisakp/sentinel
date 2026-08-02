package mariadb

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// buildExecCmd mirrors the construction inside Backup so callers can assert env
// shape without driving a real subprocess.
func buildExecCmd(mda *MariaDBDumpArgs) (*exec.Cmd, error) {
	args, err := argsBuilder(mda)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("mariadb-dump", args...)
	if mda.Password != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("MYSQL_PWD=%s", mda.Password))
	}
	return cmd, nil
}

func TestEnvInjectionNoArgvLeak(t *testing.T) {
	mda := &MariaDBDumpArgs{Username: "root", Database: "app", Password: "hunter2"}
	cmd, err := buildExecCmd(mda)
	if err != nil {
		t.Fatalf("buildExecCmd: %v", err)
	}

	wantEnv := "MYSQL_PWD=hunter2"
	count := 0
	for _, e := range cmd.Env {
		if e == wantEnv {
			count++
		}
	}
	if count != 1 {
		t.Errorf("MYSQL_PWD env count = %d, want 1; env=%v", count, cmd.Env)
	}

	for _, a := range cmd.Args {
		if strings.Contains(a, "hunter2") {
			t.Errorf("argv leaked password: %q in %v", a, cmd.Args)
		}
		if strings.HasPrefix(a, "--password") {
			t.Errorf("argv contains --password*: %q", a)
		}
	}
}

func TestEnvInjectionEmptyPasswordNoEnv(t *testing.T) {
	mda := &MariaDBDumpArgs{Username: "root", Database: "app"}
	cmd, err := buildExecCmd(mda)
	if err != nil {
		t.Fatalf("buildExecCmd: %v", err)
	}
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "MYSQL_PWD=") {
			t.Errorf("unexpected MYSQL_PWD env when password empty: %q", e)
		}
	}
}
