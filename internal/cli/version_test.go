package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/version"
)

// resetVersionCmd resets the per-invocation flag state so tests are isolated.
func resetVersionFlags() {
	versionFormat = "text"
	versionTools = false
}

// ── T007: sentinel version (text output) ────────────────────────────────────

func TestVersionCmd_TextOutput(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantFields     []string
		wantNotContain []string
	}{
		{
			name:       "default text includes all four metadata fields",
			args:       []string{"version"},
			wantFields: []string{"Version:", "Commit:", "Build Date:", "Go Version:"},
		},
		{
			name:           "default text does not include tools section",
			args:           []string{"version"},
			wantNotContain: []string{"Tools:"},
		},
		{
			name:       "explicit --format text produces same output",
			args:       []string{"version", "--format", "text"},
			wantFields: []string{"Version:", "Commit:", "Build Date:", "Go Version:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetVersionFlags()
			buf := &bytes.Buffer{}
			RootCmd.SetOut(buf)
			RootCmd.SetErr(buf)
			RootCmd.SetArgs(tt.args)

			if err := RootCmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			out := buf.String()
			for _, field := range tt.wantFields {
				if !strings.Contains(out, field) {
					t.Errorf("output missing %q\ngot:\n%s", field, out)
				}
			}
			for _, absent := range tt.wantNotContain {
				if strings.Contains(out, absent) {
					t.Errorf("output should not contain %q\ngot:\n%s", absent, out)
				}
			}
		})
	}
}

func TestVersionCmd_TextFieldsNonEmpty(t *testing.T) {
	// Independently verify that all four metadata fields have non-empty values
	// even when the binary is built without linker metadata (SC-003).
	orig := version.Version
	origC := version.Commit
	origBD := version.BuildDate
	t.Cleanup(func() {
		version.Version = orig
		version.Commit = origC
		version.BuildDate = origBD
	})
	version.Version = ""
	version.Commit = ""
	version.BuildDate = ""

	resetVersionFlags()
	buf := &bytes.Buffer{}
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"version"})

	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	out := buf.String()
	// None of the value positions should be empty (i.e. no "Version:    \n")
	for _, prefix := range []string{"Version:", "Commit:", "Build Date:", "Go Version:"} {
		idx := strings.Index(out, prefix)
		if idx == -1 {
			t.Fatalf("field %q not found in output:\n%s", prefix, out)
		}
		rest := strings.TrimSpace(out[idx+len(prefix):])
		if rest == "" {
			t.Errorf("field %q has empty value in output:\n%s", prefix, out)
		}
	}
}

// ── T009: sentinel --version (root shortcut) ────────────────────────────────

func TestRootVersion_Shortcut(t *testing.T) {
	buf := &bytes.Buffer{}
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"--version"})

	// Cobra's native --version flag returns nil error and prints the template.
	_ = RootCmd.Execute()

	out := buf.String()
	if !strings.HasPrefix(out, "sentinel ") {
		t.Errorf("--version output = %q; want to start with 'sentinel '", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("--version should produce exactly one line; got %d lines:\n%s", len(lines), out)
	}
}

// ── T013: sentinel version --tools (text output) ─────────────────────────────

func TestVersionCmd_ToolsTextOutput(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantContain    []string
		wantNotContain []string
	}{
		{
			name:        "--tools flag triggers tools section",
			args:        []string{"version", "--tools"},
			wantContain: []string{"Version:", "Commit:", "Build Date:", "Go Version:", "Tools:"},
		},
		{
			name: "--tools section contains tool names in fixed order",
			args: []string{"version", "--tools"},
			// All four tool names must appear.
			wantContain: []string{"pg_dump", "mysqldump", "mariadb-dump", "mongodump"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetVersionFlags()
			buf := &bytes.Buffer{}
			RootCmd.SetOut(buf)
			RootCmd.SetErr(buf)
			RootCmd.SetArgs(tt.args)

			if err := RootCmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			out := buf.String()
			for _, want := range tt.wantContain {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, out)
				}
			}
			for _, absent := range tt.wantNotContain {
				if strings.Contains(out, absent) {
					t.Errorf("output should not contain %q\ngot:\n%s", absent, out)
				}
			}
		})
	}
}

func TestVersionCmd_ToolsTextOrder(t *testing.T) {
	// Tools must appear in the fixed registry order: pg_dump, mysqldump,
	// mariadb-dump, mongodump.
	resetVersionFlags()
	buf := &bytes.Buffer{}
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"version", "--tools"})

	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	out := buf.String()
	order := []string{"pg_dump", "mysqldump", "mariadb-dump", "mongodump"}
	pos := -1
	for _, name := range order {
		idx := strings.Index(out, name)
		if idx == -1 {
			t.Fatalf("tool %q not found in output:\n%s", name, out)
		}
		if idx <= pos {
			t.Errorf("tool %q appears before previous tool (pos=%d idx=%d); wrong order", name, pos, idx)
		}
		pos = idx
	}
}

func TestVersionCmd_ToolsExitZeroWhenAllMissing(t *testing.T) {
	// Even if PATH has no database tools, the command must exit 0.
	t.Setenv("PATH", t.TempDir()) // override PATH to an empty directory
	resetVersionFlags()
	buf := &bytes.Buffer{}
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"version", "--tools"})

	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, but want nil (missing tools must not fail the command)", err)
	}

	out := buf.String()
	if !strings.Contains(out, "missing") {
		t.Errorf("expected at least one 'missing' state in output;\ngot:\n%s", out)
	}
}

// ── T017: sentinel version --format json ────────────────────────────────────

func TestVersionCmd_JSONOutput(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		checkFn func(t *testing.T, out string)
	}{
		{
			name: "json output has sentinel object with all fields",
			args: []string{"version", "--format", "json"},
			checkFn: func(t *testing.T, out string) {
				t.Helper()
				var env struct {
					Sentinel struct {
						Version   string `json:"version"`
						Commit    string `json:"commit"`
						BuildDate string `json:"build_date"`
						GoVersion string `json:"go_version"`
					} `json:"sentinel"`
					Tools *json.RawMessage `json:"tools"`
				}
				if err := json.Unmarshal([]byte(out), &env); err != nil {
					t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, out)
				}
				if env.Sentinel.Version == "" {
					t.Error("sentinel.version must not be empty")
				}
				if env.Sentinel.Commit == "" {
					t.Error("sentinel.commit must not be empty")
				}
				if env.Sentinel.BuildDate == "" {
					t.Error("sentinel.build_date must not be empty")
				}
				if env.Sentinel.GoVersion == "" {
					t.Error("sentinel.go_version must not be empty")
				}
				if env.Tools != nil {
					t.Errorf("tools field must be absent when --tools not requested; got: %s", *env.Tools)
				}
			},
		},
		{
			name: "json --tools adds tools array",
			args: []string{"version", "--format", "json", "--tools"},
			checkFn: func(t *testing.T, out string) {
				t.Helper()
				var env struct {
					Sentinel interface{}   `json:"sentinel"`
					Tools    []interface{} `json:"tools"`
				}
				if err := json.Unmarshal([]byte(out), &env); err != nil {
					t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, out)
				}
				if env.Tools == nil {
					t.Error("tools array must be present when --tools is requested")
				}
			},
		},
		{
			name: "json --tools schema conditionals: available has version_line, error has error field",
			args: []string{"version", "--format", "json", "--tools"},
			checkFn: func(t *testing.T, out string) {
				t.Helper()
				var env struct {
					Tools []struct {
						Name        string  `json:"name"`
						State       string  `json:"state"`
						VersionLine *string `json:"version_line"`
						Error       *string `json:"error"`
					} `json:"tools"`
				}
				if err := json.Unmarshal([]byte(out), &env); err != nil {
					t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, out)
				}
				for _, tool := range env.Tools {
					switch tool.State {
					case "available":
						if tool.VersionLine == nil || *tool.VersionLine == "" {
							t.Errorf("tool %q: state=available requires version_line", tool.Name)
						}
					case "error":
						if tool.Error == nil || *tool.Error == "" {
							t.Errorf("tool %q: state=error requires error field", tool.Name)
						}
					case "missing":
						// no extra fields required
					default:
						t.Errorf("tool %q: unknown state %q", tool.Name, tool.State)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetVersionFlags()
			buf := &bytes.Buffer{}
			RootCmd.SetOut(buf)
			RootCmd.SetErr(buf)
			RootCmd.SetArgs(tt.args)

			err := RootCmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, buf.String())
			}
		})
	}
}

func TestVersionCmd_UnsupportedFormatErrors(t *testing.T) {
	resetVersionFlags()
	buf := &bytes.Buffer{}
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"version", "--format", "yaml"})

	err := RootCmd.Execute()
	if err == nil {
		t.Fatal("Execute() should return an error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("error should mention 'unsupported format'; got: %v", err)
	}
}
