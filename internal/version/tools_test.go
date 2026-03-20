package version

import (
	"testing"
)

func TestProbeTools_FixedOrder(t *testing.T) {
	results := ProbeTools()

	want := []string{"pg_dump", "mysqldump", "mariadb-dump", "mongodump"}
	if len(results) != len(want) {
		t.Fatalf("ProbeTools() returned %d results; want %d", len(results), len(want))
	}
	for i, name := range want {
		if results[i].Name != name {
			t.Errorf("results[%d].Name = %q; want %q", i, results[i].Name, name)
		}
	}
}

func TestProbeTools_ValidStates(t *testing.T) {
	results := ProbeTools()
	validStates := map[string]bool{
		StateAvailable: true,
		StateMissing:   true,
		StateError:     true,
	}
	for _, r := range results {
		if !validStates[r.State] {
			t.Errorf("tool %q: invalid state %q", r.Name, r.State)
		}
	}
}

func TestProbeTools_AvailableHasVersionLine(t *testing.T) {
	results := ProbeTools()
	for _, r := range results {
		if r.State == StateAvailable && r.VersionLine == "" {
			t.Errorf("tool %q: state=available but VersionLine is empty", r.Name)
		}
	}
}

func TestProbeTools_MissingHasNoVersionLine(t *testing.T) {
	results := ProbeTools()
	for _, r := range results {
		if r.State == StateMissing && r.VersionLine != "" {
			t.Errorf("tool %q: state=missing but VersionLine = %q", r.Name, r.VersionLine)
		}
	}
}

func TestProbeTools_AllMissingOnEmptyPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // override PATH to directory with no executables

	results := ProbeTools()
	for _, r := range results {
		if r.State != StateMissing {
			t.Errorf("tool %q: expected state=missing on empty PATH; got %q", r.Name, r.State)
		}
	}
}

func TestFirstNonEmptyLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"single line no newline", "pg_dump (PostgreSQL) 18.3", "pg_dump (PostgreSQL) 18.3"},
		{"multi line returns first", "pg_dump (PostgreSQL) 18.3\nCopyright ...", "pg_dump (PostgreSQL) 18.3"},
		{"leading empty lines skipped", "\n\npg_dump 18.3\n", "pg_dump 18.3"},
		{"whitespace-only lines skipped", "   \n\tpg_dump 18.3\n", "pg_dump 18.3"},
		{"trims surrounding whitespace", "  pg_dump 18.3  ", "pg_dump 18.3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstNonEmptyLine(tt.input)
			if got != tt.want {
				t.Errorf("firstNonEmptyLine(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestProbeToolSpec_NonZeroExitIsError exercises the error-state path by
// invoking probe() against a real tool that is present on PATH but whose
// version command returns non-zero. We simulate this by using a spec pointing
// to a known-available binary (the test process's own `go` tool) with an
// argument that causes non-zero exit.
func TestProbeToolSpec_NonZeroExitIsError(t *testing.T) {
	spec := externalToolSpec{
		name:        "go",
		versionArgs: []string{"this-subcommand-does-not-exist"},
	}

	result := probe(spec)
	// `go this-subcommand-does-not-exist` exits non-zero.
	if result.State != StateError {
		t.Errorf("probe(): expected state=error for non-zero exit; got %q", result.State)
	}
	if result.Error == "" {
		t.Error("probe(): error field must be non-empty when state=error")
	}
}
