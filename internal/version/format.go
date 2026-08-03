package version

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderText returns a deterministic multi-line key-value block for the
// Sentinel build metadata suitable for human-readable terminal output.
func RenderText(m BuildMetadata) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Version:    %s\n", m.Version)
	fmt.Fprintf(&sb, "Commit:     %s\n", m.Commit)
	fmt.Fprintf(&sb, "Build Date: %s\n", m.BuildDate)
	fmt.Fprintf(&sb, "Go Version: %s\n", m.GoVersion)
	return sb.String()
}

// RenderTextWithTools appends a tools section to the Sentinel metadata block
// when tool inspection results are provided. Each tool is rendered on its own
// line in fixed registry order.
func RenderTextWithTools(m BuildMetadata, tools []ToolResult) string {
	var sb strings.Builder
	sb.WriteString(RenderText(m))
	sb.WriteString("\nTools:\n")
	for _, t := range tools {
		switch t.State {
		case StateAvailable:
			fmt.Fprintf(&sb, "  %-14s available  %s\n", t.Name, t.VersionLine)
		case StateMissing:
			fmt.Fprintf(&sb, "  %-14s missing\n", t.Name)
		case StateError:
			fmt.Fprintf(&sb, "  %-14s error      %s\n", t.Name, t.Error)
		}
	}
	return sb.String()
}

// VersionJSONEnvelope is the machine-readable contract for
// `sentinel version --format json`. The sentinel field is always present;
// the tools field is omitted entirely when tool inspection is not requested.
type VersionJSONEnvelope struct {
	Sentinel SentinelJSON `json:"sentinel"`
	Tools    []ToolResult `json:"tools,omitempty"`
}

// SentinelJSON holds the Sentinel build metadata fields in the JSON envelope.
type SentinelJSON struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
}

// RenderJSON serialises the version output as indented JSON conforming to the
// contract defined in contracts/version-output.schema.json. When includeTools
// is false the tools field is omitted from the output.
func RenderJSON(m BuildMetadata, tools []ToolResult, includeTools bool) ([]byte, error) {
	env := VersionJSONEnvelope{
		Sentinel: SentinelJSON{
			Version:   m.Version,
			Commit:    m.Commit,
			BuildDate: m.BuildDate,
			GoVersion: m.GoVersion,
		},
	}
	if includeTools {
		env.Tools = tools
	}
	return json.MarshalIndent(env, "", "  ")
}
