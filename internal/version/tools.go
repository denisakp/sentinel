package version

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Tool state constants used in ToolResult.
const (
	// StateAvailable indicates the tool was found on PATH and its version
	// command exited successfully, yielding a non-empty first-line output.
	StateAvailable = "available"

	// StateMissing indicates the tool executable was not found on PATH.
	StateMissing = "missing"

	// StateError indicates the tool was found on PATH but its version command
	// returned a non-zero exit code or produced no usable output.
	StateError = "error"
)

// ToolResult is the per-tool inspection outcome produced during --tools probing.
// It maps to the data-model entity ExternalToolVersionResult.
type ToolResult struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	VersionLine string `json:"version_line,omitempty"`
	Error       string `json:"error,omitempty"`
}

// externalToolSpec declares a supported external client binary and its
// native version flag. The registry is ordered and must never be reordered.
type externalToolSpec struct {
	name        string
	versionArgs []string
}

// toolRegistry is the fixed, ordered list of supported external tools.
var toolRegistry = []externalToolSpec{
	{name: "pg_dump", versionArgs: []string{"--version"}},
	{name: "mysqldump", versionArgs: []string{"--version"}},
	{name: "mariadb-dump", versionArgs: []string{"--version"}},
	{name: "mongodump", versionArgs: []string{"--version"}},
}

// ProbeTools iterates the fixed tool registry in order and returns one
// ToolResult per entry. Each tool is probed independently so a failure or
// missing binary for one tool never prevents reporting of the others.
// Each subprocess is given a 5-second context timeout.
func ProbeTools() []ToolResult {
	results := make([]ToolResult, 0, len(toolRegistry))
	for _, spec := range toolRegistry {
		results = append(results, probe(spec))
	}
	return results
}

// probe resolves a single tool on PATH and runs its version command.
func probe(spec externalToolSpec) ToolResult {
	_, err := exec.LookPath(spec.name)
	if err != nil {
		return ToolResult{Name: spec.name, State: StateMissing}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// #nosec G204 — arguments are from a fixed internal registry, not user input.
	cmd := exec.CommandContext(ctx, spec.name, spec.versionArgs...)
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	runErr := cmd.Run()
	if runErr != nil {
		detail := runErr.Error()
		if line := firstNonEmptyLine(combined.String()); line != "" {
			detail = fmt.Sprintf("%s: %s", runErr.Error(), line)
		}
		return ToolResult{Name: spec.name, State: StateError, Error: detail}
	}

	line := firstNonEmptyLine(combined.String())
	if line == "" {
		return ToolResult{Name: spec.name, State: StateError, Error: "version command produced no output"}
	}

	return ToolResult{Name: spec.name, State: StateAvailable, VersionLine: line}
}

// firstNonEmptyLine returns the first non-empty trimmed line from s, or "".
func firstNonEmptyLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
