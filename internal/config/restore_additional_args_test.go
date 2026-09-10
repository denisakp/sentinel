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
