package gcs

import "testing"

// TestExtractObjectKeyKeepsPrefixes is half the regression guard for #182.
//
// Delete and Exists resolved their target with extractObjectPath, which reduces
// "backups/pg/2026-01-01.sql" to "2026-01-01.sql" via filepath.Base. That
// addresses a different object than the caller named, almost always one that
// does not exist. Paired with a Delete that treated "not there" as "done",
// retention reported deletions it had never performed.
func TestExtractObjectKeyKeepsPrefixes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"gs URL with prefix", "gs://bucket-a/backups/pg/2026-01-01.sql", "backups/pg/2026-01-01.sql"},
		{"gs URL flat", "gs://bucket-a/dump.sql", "dump.sql"},
		{"bare key with prefix", "backups/pg/2026-01-01.sql", "backups/pg/2026-01-01.sql"},
		{"bare key flat", "dump.sql", "dump.sql"},
		{"leading slash trimmed", "/backups/pg/dump.sql", "backups/pg/dump.sql"},
		{"empty", "", ""},
		{"bucket only", "gs://bucket-a", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractObjectKey(tc.in); got != tc.want {
				t.Errorf("extractObjectKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractObjectPathStillFlattens pins the distinction rather than leaving it
// to be rediscovered. The upload path keeps its flattening behaviour on purpose:
// there it is a naming convention, and changing it would rename where new
// artifacts land. Only the operations that must address an existing object were
// moved onto the exact-key resolver.
func TestExtractObjectPathStillFlattens(t *testing.T) {
	if got := extractObjectPath("backups/pg/2026-01-01.sql"); got != "2026-01-01.sql" {
		t.Errorf("extractObjectPath = %q; the upload naming convention changed unintentionally", got)
	}
}
