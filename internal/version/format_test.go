package version

import (
	"encoding/json"
	"strings"
	"testing"
)

var testMeta = BuildMetadata{
	Version:   "v1.0.1",
	Commit:    "abc1234",
	BuildDate: "2026-03-20T12:00:00Z",
	GoVersion: "go1.24.0",
}

// ── RenderText ────────────────────────────────────────────────────────────────

func TestRenderText_ContainsAllFields(t *testing.T) {
	out := RenderText(testMeta)
	for _, want := range []string{"Version:", "v1.0.1", "Commit:", "abc1234", "Build Date:", "Go Version:", "go1.24.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderText() missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestRenderText_Deterministic(t *testing.T) {
	a := RenderText(testMeta)
	b := RenderText(testMeta)
	if a != b {
		t.Errorf("RenderText() is not deterministic:\n%s\n!=\n%s", a, b)
	}
}

// ── RenderTextWithTools ───────────────────────────────────────────────────────

func TestRenderTextWithTools_IncludesToolsSection(t *testing.T) {
	tools := []ToolResult{
		{Name: "pg_dump", State: StateAvailable, VersionLine: "pg_dump (PostgreSQL) 18.3"},
		{Name: "mysqldump", State: StateMissing},
		{Name: "mariadb-dump", State: StateError, Error: "exit status 1"},
		{Name: "mongodump", State: StateMissing},
	}
	out := RenderTextWithTools(testMeta, tools)

	if !strings.Contains(out, "Tools:") {
		t.Error("RenderTextWithTools() missing 'Tools:' section header")
	}
	if !strings.Contains(out, "pg_dump") {
		t.Error("missing pg_dump in tools output")
	}
	if !strings.Contains(out, "available") {
		t.Error("missing 'available' state")
	}
	if !strings.Contains(out, "missing") {
		t.Error("missing 'missing' state")
	}
	if !strings.Contains(out, "error") {
		t.Error("missing 'error' state")
	}
	if !strings.Contains(out, "pg_dump (PostgreSQL) 18.3") {
		t.Error("missing version_line in tools output")
	}
}

// ── RenderJSON ────────────────────────────────────────────────────────────────

func TestRenderJSON(t *testing.T) {
	tools := []ToolResult{
		{Name: "pg_dump", State: StateAvailable, VersionLine: "pg_dump (PostgreSQL) 18.3"},
		{Name: "mysqldump", State: StateMissing},
	}

	tests := []struct {
		name         string
		meta         BuildMetadata
		tools        []ToolResult
		includeTools bool
		checkFn      func(t *testing.T, data []byte)
	}{
		{
			name:         "sentinel-only: tools field absent",
			meta:         testMeta,
			tools:        nil,
			includeTools: false,
			checkFn: func(t *testing.T, data []byte) {
				t.Helper()
				var env map[string]json.RawMessage
				if err := json.Unmarshal(data, &env); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if _, ok := env["tools"]; ok {
					t.Error("tools field must be absent when includeTools=false")
				}
				if _, ok := env["sentinel"]; !ok {
					t.Error("sentinel field must always be present")
				}
			},
		},
		{
			name:         "sentinel-only with nil tools and includeTools=false",
			meta:         testMeta,
			tools:        tools,
			includeTools: false,
			checkFn: func(t *testing.T, data []byte) {
				t.Helper()
				var env map[string]json.RawMessage
				if err := json.Unmarshal(data, &env); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if _, ok := env["tools"]; ok {
					t.Error("tools field must be absent when includeTools=false, even if tools slice is provided")
				}
			},
		},
		{
			name:         "with tools: tools array present",
			meta:         testMeta,
			tools:        tools,
			includeTools: true,
			checkFn: func(t *testing.T, data []byte) {
				t.Helper()
				var env VersionJSONEnvelope
				if err := json.Unmarshal(data, &env); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if env.Tools == nil {
					t.Error("tools must be present when includeTools=true")
				}
				if len(env.Tools) != 2 {
					t.Errorf("want 2 tools; got %d", len(env.Tools))
				}
			},
		},
		{
			name:         "sentinel fields are correct",
			meta:         testMeta,
			tools:        nil,
			includeTools: false,
			checkFn: func(t *testing.T, data []byte) {
				t.Helper()
				var env VersionJSONEnvelope
				if err := json.Unmarshal(data, &env); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if env.Sentinel.Version != "v1.0.1" {
					t.Errorf("sentinel.version = %q; want v1.0.1", env.Sentinel.Version)
				}
				if env.Sentinel.Commit != "abc1234" {
					t.Errorf("sentinel.commit = %q; want abc1234", env.Sentinel.Commit)
				}
				if env.Sentinel.BuildDate != "2026-03-20T12:00:00Z" {
					t.Errorf("sentinel.build_date = %q; want 2026-03-20T12:00:00Z", env.Sentinel.BuildDate)
				}
				if env.Sentinel.GoVersion != "go1.24.0" {
					t.Errorf("sentinel.go_version = %q; want go1.24.0", env.Sentinel.GoVersion)
				}
			},
		},
		{
			name:         "schema conditional: available tool has version_line",
			meta:         testMeta,
			tools:        tools,
			includeTools: true,
			checkFn: func(t *testing.T, data []byte) {
				t.Helper()
				var env VersionJSONEnvelope
				if err := json.Unmarshal(data, &env); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				for _, tool := range env.Tools {
					if tool.State == StateAvailable && tool.VersionLine == "" {
						t.Errorf("tool %q: state=available requires version_line", tool.Name)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := RenderJSON(tt.meta, tt.tools, tt.includeTools)
			if err != nil {
				t.Fatalf("RenderJSON() error = %v", err)
			}
			tt.checkFn(t, data)
		})
	}
}
