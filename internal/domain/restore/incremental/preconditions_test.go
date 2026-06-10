package incremental

import (
	"strings"
	"testing"
)

func TestValidateAssemblyPreconditions(t *testing.T) {
	cases := []struct {
		name    string
		req     AssemblyRequest
		wantSub string
	}{
		{"missing_staging_dir", AssemblyRequest{}, "insufficient_staging_space"},
		{"insufficient_space", AssemblyRequest{StagingDir: "/tmp", EstimatedBytes: 100, AvailableBytes: 10}, "insufficient_staging_space"},
		{"missing_combine_tool", AssemblyRequest{StagingDir: "/tmp", RequiresCombineTool: true}, "required_tool_missing"},
		{"ok", AssemblyRequest{StagingDir: "/tmp", EstimatedBytes: 10, AvailableBytes: 100}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAssemblyPreconditions(tc.req)
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("want err containing %q, got %v", tc.wantSub, err)
			}
		})
	}
}
