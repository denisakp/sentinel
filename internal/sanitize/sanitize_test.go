package sanitize_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/sanitize"
)

func TestRedactArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no credentials passthrough",
			args: []string{"--host=localhost", "--port=5432"},
			want: []string{"--host=localhost", "--port=5432"},
		},
		{
			name: "empty slice",
			args: []string{},
			want: []string{},
		},
		{
			name: "nil slice",
			args: nil,
			want: nil,
		},
		{
			name: "two-token password flag",
			args: []string{"--host=db", "--password", "secret123", "--user=root"},
			want: []string{"--host=db", "--password", "*****", "--user=root"},
		},
		{
			name: "PGPASSWORD env style",
			args: []string{"PGPASSWORD=mysecret", "--host=db"},
			want: []string{"PGPASSWORD=*****", "--host=db"},
		},
		{
			name: "MYSQL_PWD env style",
			args: []string{"MYSQL_PWD=mysecret", "--host=db"},
			want: []string{"MYSQL_PWD=*****", "--host=db"},
		},
		{
			name: "password-env var name is non-secret passthrough",
			args: []string{"--password-env", "DB_PWD"},
			want: []string{"--password-env", "DB_PWD"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args == nil {
				got := sanitize.RedactArgs(tt.args)
				if got != nil {
					t.Errorf("RedactArgs(nil) = %v, want nil", got)
				}
				return
			}
			got := sanitize.RedactArgs(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("RedactArgs() len = %d, want %d; got %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("RedactArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRedactLog(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, result string)
	}{
		{
			name:  "no credentials unchanged",
			input: "backup started for host=localhost",
			check: func(t *testing.T, result string) {
				if result != "backup started for host=localhost" {
					t.Errorf("unexpected change: %q", result)
				}
			},
		},
		{
			name:  "PGPASSWORD redacted",
			input: "env PGPASSWORD=supersecret host=db",
			check: func(t *testing.T, result string) {
				if strings.Contains(result, "supersecret") {
					t.Errorf("RedactLog did not redact PGPASSWORD: %q", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitize.RedactLog(tt.input)
			tt.check(t, result)
		})
	}
}

func TestRedactStderr(t *testing.T) {
	const cap64KiB = 65536

	tests := []struct {
		name           string
		input          []byte
		mustNotContain []string
		mustContain    []string
		wantTruncated  bool
		minPatterns    int
	}{
		{
			name:  "empty input",
			input: nil,
		},
		{
			name:        "no match passthrough",
			input:       []byte("nothing sensitive here\nhost=localhost"),
			mustContain: []string{"nothing sensitive here", "host=localhost"},
		},
		{
			name:           "P1 --password= still matches",
			input:          []byte("--password=hunter2"),
			mustNotContain: []string{"hunter2"},
			mustContain:    []string{"--password=*****"},
			minPatterns:    1,
		},
		{
			name:           "P3 PGPASSWORD still matches",
			input:          []byte("PGPASSWORD=hunter2 ./pg_dump"),
			mustNotContain: []string{"hunter2"},
			mustContain:    []string{"PGPASSWORD=*****"},
			minPatterns:    1,
		},
		{
			name:           "P4 MYSQL_PWD still matches",
			input:          []byte("MYSQL_PWD=hunter2"),
			mustNotContain: []string{"hunter2"},
			mustContain:    []string{"MYSQL_PWD=*****"},
			minPatterns:    1,
		},
		{
			name:           "P5 URI userinfo",
			input:          []byte("connecting to postgres://alice:hunter2@db.example/postgres"),
			mustNotContain: []string{"hunter2"},
			mustContain:    []string{"postgres://alice:*****@db.example/postgres"},
			minPatterns:    1,
		},
		{
			name:           "P6 MONGO_INITDB_ROOT_PASSWORD",
			input:          []byte("MONGO_INITDB_ROOT_PASSWORD=hunter2\n"),
			mustNotContain: []string{"hunter2"},
			mustContain:    []string{"MONGO_INITDB_ROOT_PASSWORD=*****"},
			minPatterns:    1,
		},
		{
			name:           "P7 libpq whitespace form",
			input:          []byte("password = hunter2"),
			mustNotContain: []string{"hunter2"},
			mustContain:    []string{"password = *****"},
			minPatterns:    1,
		},
		{
			name:           "multi-line only one line carries secret",
			input:          []byte("line 1 normal\npassword=hunter2\nline 3 normal\n"),
			mustNotContain: []string{"hunter2"},
			mustContain:    []string{"line 1 normal", "line 3 normal", "password=*****"},
			minPatterns:    1,
		},
		{
			name:        "invalid utf-8 no panic",
			input:       []byte{0xff, 0xfe, 0xfd, '\n', 'o', 'k'},
			mustContain: []string{"ok"},
		},
		{
			name:        "exactly 64 KiB no truncation",
			input:       bytes.Repeat([]byte("a"), cap64KiB),
			mustContain: []string{strings.Repeat("a", 16)},
		},
		{
			name:          "over 64 KiB truncates with marker",
			input:         bytes.Repeat([]byte("a"), cap64KiB+1),
			mustContain:   []string{"[stderr truncated, 1 bytes elided]"},
			wantTruncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, stats := sanitize.RedactStderr(tt.input)
			for _, want := range tt.mustNotContain {
				if strings.Contains(got, want) {
					t.Errorf("output unexpectedly contained %q: %q", want, truncate(got))
				}
			}
			for _, want := range tt.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q: %q", want, truncate(got))
				}
			}
			if stats.Truncated != tt.wantTruncated {
				t.Errorf("stats.Truncated = %v, want %v", stats.Truncated, tt.wantTruncated)
			}
			if stats.PatternsMatched < tt.minPatterns {
				t.Errorf("stats.PatternsMatched = %d, want ≥ %d", stats.PatternsMatched, tt.minPatterns)
			}
			if stats.BytesIn != len(tt.input) {
				t.Errorf("stats.BytesIn = %d, want %d", stats.BytesIn, len(tt.input))
			}
			if stats.BytesOut != len(got) {
				t.Errorf("stats.BytesOut = %d, want %d", stats.BytesOut, len(got))
			}
		})
	}
}

func TestRedactStderr_KnownMixedPayload(t *testing.T) {
	payload := []byte("PGPASSWORD=hunter2\npostgres://u:hunter2@h/db\n")
	got, stats := sanitize.RedactStderr(payload)
	if strings.Contains(got, "hunter2") {
		t.Fatalf("hunter2 leaked: %q", got)
	}
	if stats.PatternsMatched < 2 {
		t.Errorf("PatternsMatched = %d, want ≥ 2", stats.PatternsMatched)
	}
	if stats.BytesRedacted < 14 { // two "hunter2" occurrences
		t.Errorf("BytesRedacted = %d, want ≥ 14", stats.BytesRedacted)
	}
	if stats.Truncated {
		t.Error("Truncated unexpectedly true")
	}
}

// recordingHandler captures slog records for assertions.
type recordingHandler struct {
	records []slog.Record
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

func swapLogger(t *testing.T) *recordingHandler {
	t.Helper()
	prev := slog.Default()
	h := &recordingHandler{}
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func TestRedactStderr_DebugLogOnRedaction(t *testing.T) {
	h := swapLogger(t)
	_, _ = sanitize.RedactStderr([]byte("PGPASSWORD=hunter2\n"))
	if len(h.records) != 1 {
		t.Fatalf("expected exactly 1 record, got %d", len(h.records))
	}
	r := h.records[0]
	if strings.Contains(r.Message, "hunter2") {
		t.Errorf("message leaked secret: %q", r.Message)
	}
	allowed := map[string]bool{"bytes_redacted": true, "patterns_matched": true, "truncated": true}
	r.Attrs(func(a slog.Attr) bool {
		if !allowed[a.Key] {
			t.Errorf("unexpected attr key %q", a.Key)
		}
		if strings.Contains(a.Value.String(), "hunter2") {
			t.Errorf("attr %q leaked secret: %s", a.Key, a.Value.String())
		}
		return true
	})
}

func TestRedactStderr_NoLogWhenClean(t *testing.T) {
	h := swapLogger(t)
	_, _ = sanitize.RedactStderr([]byte("plain diagnostic output\n"))
	if len(h.records) != 0 {
		t.Fatalf("expected 0 records on clean input, got %d", len(h.records))
	}
}

func truncate(s string) string {
	const max = 160
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
