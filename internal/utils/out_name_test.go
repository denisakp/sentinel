package utils

import (
	"strings"
	"testing"
	"time"
)

// TestFinalOutNameLiteralIsUnchanged is the compatibility half of #193.
//
// An explicit `output:` must be used verbatim. Silently interpolating a timestamp
// would rename the artifact every existing installation produces, breaking
// scripts, alerts and external tooling that expect the configured name. The
// maintainer chose placeholder-gated expansion for exactly that reason.
func TestFinalOutNameLiteralIsUnchanged(t *testing.T) {
	for _, name := range []string{"shop.sql", "dumps/shop.sql", "shop.archive", "shop.sql.gz"} {
		if got := FinalOutName(name); got != name {
			t.Errorf("FinalOutName(%q) = %q, want it unchanged: interpolating into a literal "+
				"value renames what every existing install produces", name, got)
		}
	}
}

// TestFinalOutNamePlaceholdersExpandPerRun is the fix half of #193.
//
// With a literal name, every run truncates the previous artifact, so retention has
// nothing to prune, `keep_last: 30` keeps one file, and an incremental chain has
// every member resolving to the same path. A placeholder opts into a distinct
// artifact per run.
func TestFinalOutNamePlaceholdersExpandPerRun(t *testing.T) {
	restore := SetNowForTest(func() time.Time { return time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC) })
	t.Cleanup(restore)

	first := FinalOutName("shop-{timestamp}.sql")
	if first != "shop-2026-01-02T15-04-05.sql" {
		t.Fatalf("FinalOutName with {timestamp} = %q", first)
	}

	SetNowForTest(func() time.Time { return time.Date(2026, 1, 2, 16, 30, 0, 0, time.UTC) })
	second := FinalOutName("shop-{timestamp}.sql")
	if second == first {
		t.Errorf("two runs produced the same filename %q, so the second truncates the first", first)
	}

	SetNowForTest(func() time.Time { return time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC) })
	if got := FinalOutName("shop-{date}.sql"); got != "shop-2026-03-04.sql" {
		t.Errorf("FinalOutName with {date} = %q", got)
	}
}

// TestFinalOutNamePlaceholderKeepsTheExtension: expansion must not disturb the
// suffix, or compression and restore auto-detection would misread the artifact.
func TestFinalOutNamePlaceholderKeepsTheExtension(t *testing.T) {
	restore := SetNowForTest(func() time.Time { return time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC) })
	t.Cleanup(restore)

	for name, wantSuffix := range map[string]string{
		"shop-{timestamp}.sql":     ".sql",
		"shop-{timestamp}.archive": ".archive",
		"shop-{date}.sql.gz":       ".gz",
	} {
		if got := FinalOutName(name); !strings.HasSuffix(got, wantSuffix) {
			t.Errorf("FinalOutName(%q) = %q, lost the %q suffix", name, got, wantSuffix)
		}
	}
}

// TestFinalOutNameEmptyStillGenerates: omitting `output:` must keep producing a
// unique generated name. That path is what the fix for #151 made verifiable, so
// it is now the recommended way to get both integrity and retention.
func TestFinalOutNameEmptyStillGenerates(t *testing.T) {
	restore := SetNowForTest(func() time.Time { return time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC) })
	t.Cleanup(restore)

	first := FinalOutName("")
	SetNowForTest(func() time.Time { return time.Date(2026, 1, 2, 16, 30, 0, 0, time.UTC) })
	second := FinalOutName("")

	if first == second {
		t.Errorf("an omitted output: produced the same name twice: %q", first)
	}
	if !strings.HasSuffix(first, ".sql") {
		t.Errorf("generated name %q has no extension", first)
	}
}

func TestHasOutNamePlaceholder(t *testing.T) {
	for name, want := range map[string]bool{
		"shop.sql":              false,
		"shop-{timestamp}.sql":  true,
		"shop-{date}.sql":       true,
		"shop-{unknown}.sql":    false,
		"{timestamp}":           true,
		"dumps/{date}/shop.sql": true,
	} {
		if got := HasOutNamePlaceholder(name); got != want {
			t.Errorf("HasOutNamePlaceholder(%q) = %v, want %v", name, got, want)
		}
	}
}
