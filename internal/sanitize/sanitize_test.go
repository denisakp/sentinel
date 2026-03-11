package sanitize_test

import (
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
				if contains(result, "supersecret") {
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
