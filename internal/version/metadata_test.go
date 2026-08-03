package version

import (
	"strings"
	"testing"
)

func TestGet_FallbacksWhenVarsUnset(t *testing.T) {
	origV := Version
	origC := Commit
	origBD := BuildDate
	t.Cleanup(func() {
		Version = origV
		Commit = origC
		BuildDate = origBD
	})

	tests := []struct {
		name          string
		version       string
		commit        string
		buildDate     string
		wantVersion   string
		wantCommit    string
		wantBuildDate string
	}{
		{
			name:          "all unset returns deterministic fallbacks",
			version:       "",
			commit:        "",
			buildDate:     "",
			wantVersion:   "dev",
			wantCommit:    "unknown",
			wantBuildDate: "unknown",
		},
		{
			name:          "linker-injected values are preserved",
			version:       "v1.2.3",
			commit:        "abc1234",
			buildDate:     "2026-03-20T12:00:00Z",
			wantVersion:   "v1.2.3",
			wantCommit:    "abc1234",
			wantBuildDate: "2026-03-20T12:00:00Z",
		},
		{
			name:          "only version injected, others fall back",
			version:       "v2.0.0",
			commit:        "",
			buildDate:     "",
			wantVersion:   "v2.0.0",
			wantCommit:    "unknown",
			wantBuildDate: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			Commit = tt.commit
			BuildDate = tt.buildDate

			got := Get()

			if got.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", got.Version, tt.wantVersion)
			}
			if got.Commit != tt.wantCommit {
				t.Errorf("Commit = %q, want %q", got.Commit, tt.wantCommit)
			}
			if got.BuildDate != tt.wantBuildDate {
				t.Errorf("BuildDate = %q, want %q", got.BuildDate, tt.wantBuildDate)
			}
			if !strings.HasPrefix(got.GoVersion, "go") {
				t.Errorf("GoVersion = %q does not start with 'go'", got.GoVersion)
			}
			if got.GoVersion == "" {
				t.Error("GoVersion must never be empty")
			}
		})
	}
}
