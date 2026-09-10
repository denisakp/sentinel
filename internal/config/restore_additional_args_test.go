package config

import (
	"strings"
	"testing"
)

// TestRestoreAdditionalArgsReachesTheCommand is the regression guard for #172.
//
// restore_options.additional_args was parsed and validated at configuration load
// and then never read when the argument string was built, so engine-specific
// restore flags were accepted, reported as valid, and silently ignored. Nothing
// in the output said so. Silently ignored configuration is worse than rejected
// configuration: the operator has no way to learn the difference.
func TestRestoreAdditionalArgsReachesTheCommand(t *testing.T) {
	job := RestoreJob{
		Type: "postgres",
		RestoreOptions: map[string]interface{}{
			"additional_args": "--exclude-schema=audit --jobs=4",
		},
	}

	got := BuildRestoreAdditionalArgs(job)
	for _, want := range []string{"--exclude-schema=audit", "--jobs=4"} {
		if !strings.Contains(got, want) {
			t.Errorf("additional_args was dropped: %q missing from %q", want, got)
		}
	}
}

// TestRestoreAdditionalArgsComesAfterDerivedFlags pins the ordering. The
// operator's own arguments sit nearest the engine invocation, so where a tool
// honours the later occurrence of a flag they can override what the boolean
// options produced.
func TestRestoreAdditionalArgsComesAfterDerivedFlags(t *testing.T) {
	job := RestoreJob{
		Type: "postgres",
		RestoreOptions: map[string]interface{}{
			"clean":           true,
			"no_owner":        true,
			"additional_args": "--jobs=4",
		},
	}

	got := BuildRestoreAdditionalArgs(job)
	cleanAt := strings.Index(got, "--clean")
	extraAt := strings.Index(got, "--jobs=4")
	if cleanAt < 0 || extraAt < 0 {
		t.Fatalf("expected both derived and explicit arguments, got %q", got)
	}
	if extraAt < cleanAt {
		t.Errorf("additional_args must come after the derived flags, got %q", got)
	}
}

// TestRestoreFlagOrderIsDeterministic guards a second defect found while fixing
// the first.
//
// The builder ranged over the RestoreOptions map, and Go randomises map
// iteration, so the same configuration produced a different argument order from
// one run to the next. That makes the restore command non-reproducible and any
// assertion on it flaky, which is the kind of test nobody trusts and everybody
// eventually deletes.
func TestRestoreFlagOrderIsDeterministic(t *testing.T) {
	job := RestoreJob{
		Type: "postgres",
		RestoreOptions: map[string]interface{}{
			"clean":         true,
			"if_exists":     true,
			"no_owner":      true,
			"no_privileges": true,
		},
	}

	first := BuildRestoreAdditionalArgs(job)
	for i := 0; i < 50; i++ {
		if got := BuildRestoreAdditionalArgs(job); got != first {
			t.Fatalf("argument order is not stable across calls:\n  %q\n  %q", first, got)
		}
	}
	if want := "--clean --if-exists --no-owner --no-privileges"; first != want {
		t.Errorf("flag order = %q, want %q", first, want)
	}
}

// TestRestoreGzipStaysMongoOnly keeps the one engine-specific flag engine-specific
// after the rewrite.
func TestRestoreGzipStaysMongoOnly(t *testing.T) {
	opts := map[string]interface{}{"gzip": true}

	if got := BuildRestoreAdditionalArgs(RestoreJob{Type: "postgres", RestoreOptions: opts}); strings.Contains(got, "--gzip") {
		t.Errorf("--gzip leaked onto a postgres restore: %q", got)
	}
	if got := BuildRestoreAdditionalArgs(RestoreJob{Type: "mongodb", RestoreOptions: opts}); !strings.Contains(got, "--gzip") {
		t.Errorf("--gzip missing from a mongodb restore: %q", got)
	}
}

// TestRestoreAdditionalArgsIgnoresNonStringAndEmpty: the validator rejects a
// non-string at load, so the builder must not panic or emit rubbish if it ever
// sees one through another path.
func TestRestoreAdditionalArgsIgnoresNonStringAndEmpty(t *testing.T) {
	for name, raw := range map[string]interface{}{
		"int":    42,
		"bool":   true,
		"empty":  "",
		"spaces": "   ",
	} {
		t.Run(name, func(t *testing.T) {
			job := RestoreJob{Type: "postgres", RestoreOptions: map[string]interface{}{"additional_args": raw}}
			if got := BuildRestoreAdditionalArgs(job); got != "" {
				t.Errorf("expected no arguments for %v, got %q", raw, got)
			}
		})
	}
}

// TestIncrementalRestoreModeMatchesThePlanner is the regression guard for #186.
//
// The validator whitelisted postgres, mysql, mariadb and mongodb for
// restore_mode: incremental, while domain/restore.planIncremental rejects
// anything but postgres with ReasonCodeUnsupportedDatabaseType. A MySQL, MariaDB
// or MongoDB job therefore validated cleanly and failed at `restore run`, which
// for a restore is the worst available moment to learn the mode was never
// supported. `restore dry-run` gave no warning either, because it does not invoke
// the planner (#152, still open).
//
// The sibling `pitr` mode was already restricted here; this is the same rule.
func TestIncrementalRestoreModeMatchesThePlanner(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb", "mongodb"} {
		t.Run(engine+" rejected", func(t *testing.T) {
			job := RestoreJob{
				Type:                  engine,
				RestoreMode:           "incremental",
				IncrementalFromBackup: "baseline-1",
			}
			err := validateAdvancedRestoreOptions(job)
			if err == nil {
				t.Fatalf("restore_mode incremental validated for %s, but the planner rejects "+
					"every engine except postgres, so this fails at run time instead", engine)
			}
			if !strings.Contains(err.Error(), "supported only for postgres") {
				t.Errorf("the error does not say which engines can do this: %v", err)
			}
		})
	}

	t.Run("postgres accepted", func(t *testing.T) {
		job := RestoreJob{
			Type:                  "postgres",
			RestoreMode:           "incremental",
			IncrementalFromBackup: "baseline-1",
		}
		if err := validateAdvancedRestoreOptions(job); err != nil {
			t.Errorf("postgres incremental must still validate: %v", err)
		}
	})
}

// TestIncrementalAndPITRAgreeOnEngineSupport pins the two modes against each
// other. They are planned by the same package and restricted to the same engine,
// so a future change that loosens one and not the other reintroduces #186 in the
// other direction.
func TestIncrementalAndPITRAgreeOnEngineSupport(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb", "mongodb"} {
		inc := validateAdvancedRestoreOptions(RestoreJob{Type: engine, RestoreMode: "incremental", IncrementalFromBackup: "b1"})
		pitr := validateAdvancedRestoreOptions(RestoreJob{Type: engine, RestoreMode: "pitr", PITRTimestamp: "2026-01-01T00:00:00Z"})
		if (inc == nil) != (pitr == nil) {
			t.Errorf("%s: incremental and pitr disagree on engine support (incremental=%v, pitr=%v); "+
				"both are planned by domain/restore and restricted to postgres", engine, inc, pitr)
		}
	}
}
