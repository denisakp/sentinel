package utils

import (
	"strings"
	"testing"
	"time"
)

func TestBuildScheduledOutName(t *testing.T) {
	fixed := time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		rawOutput      string
		canonicalExt   string
		collisionScope string
		ts             time.Time
		want           string
		wantPrefix     string
	}{
		{
			name:           "configured output uses prefix and extension",
			rawOutput:      "postgres-dev",
			canonicalExt:   ".sql",
			collisionScope: "job-a",
			ts:             fixed,
			want:           "postgres-dev_2026-03-15T02-00-00.sql",
		},
		{
			name:           "strip one canonical extension",
			rawOutput:      "postgres-dev.sql",
			canonicalExt:   ".sql",
			collisionScope: "job-b",
			ts:             fixed,
			want:           "postgres-dev_2026-03-15T02-00-00.sql",
		},
		{
			name:           "no canonical extension configured",
			rawOutput:      "mongo-archive",
			canonicalExt:   "",
			collisionScope: "job-c",
			ts:             fixed,
			want:           "mongo-archive_2026-03-15T02-00-00",
		},
		{
			name:           "empty output uses sentinel default prefix",
			rawOutput:      "",
			canonicalExt:   ".sql",
			collisionScope: "job-d",
			ts:             fixed,
			wantPrefix:     "SENTINEL_2026-03-15T02-00-00_2026-03-15T02-00-00.sql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetScheduledOutCountersForTest()
			restore := SetNowForTest(func() time.Time { return fixed })
			defer restore()

			got := BuildScheduledOutName(tt.rawOutput, tt.canonicalExt, tt.collisionScope, tt.ts)
			if tt.want != "" && got != tt.want {
				t.Fatalf("BuildScheduledOutName() = %q, want %q", got, tt.want)
			}
			if tt.wantPrefix != "" && !strings.HasPrefix(got, tt.wantPrefix) {
				t.Fatalf("BuildScheduledOutName() = %q, want prefix %q", got, tt.wantPrefix)
			}
		})
	}
}

func TestBuildScheduledOutNameCollisionSuffix(t *testing.T) {
	resetScheduledOutCountersForTest()
	fixed := time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC)

	first := BuildScheduledOutName("postgres-dev", ".sql", "job-a", fixed)
	second := BuildScheduledOutName("postgres-dev", ".sql", "job-a", fixed)
	thirdDifferentScope := BuildScheduledOutName("postgres-dev", ".sql", "job-b", fixed)

	if first != "postgres-dev_2026-03-15T02-00-00.sql" {
		t.Fatalf("first name = %q", first)
	}
	if second != "postgres-dev_2026-03-15T02-00-00-1.sql" {
		t.Fatalf("second name = %q", second)
	}
	if thirdDifferentScope != "postgres-dev_2026-03-15T02-00-00.sql" {
		t.Fatalf("thirdDifferentScope name = %q", thirdDifferentScope)
	}
}

func TestNormalizeScheduledOutputPrefix(t *testing.T) {
	tests := []struct {
		name       string
		rawOutput  string
		ext        string
		wantPrefix string
	}{
		{name: "strip exactly one suffix", rawOutput: "db.sql", ext: ".sql", wantPrefix: "db"},
		{name: "keep mismatched extension", rawOutput: "db.txt", ext: ".sql", wantPrefix: "db.txt"},
		{name: "no ext configured", rawOutput: "db.sql", ext: "", wantPrefix: "db.sql"},
		{name: "empty output", rawOutput: "", ext: ".sql", wantPrefix: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeScheduledOutputPrefix(tt.rawOutput, tt.ext)
			if got != tt.wantPrefix {
				t.Fatalf("NormalizeScheduledOutputPrefix() = %q, want %q", got, tt.wantPrefix)
			}
		})
	}
}
