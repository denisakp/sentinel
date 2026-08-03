package cli

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"30d", 30 * 24 * time.Hour, false},
		{"4w", 4 * 7 * 24 * time.Hour, false},
		{"720h", 720 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"0d", 0, false},
		{"90m", 90 * time.Minute, false},
		{"  7d  ", 7 * 24 * time.Hour, false}, // surrounding whitespace tolerated
		{"", 0, true},
		{"invalid", 0, true},
		{"d", 0, true},   // no number before the unit
		{"-5d", 0, true}, // negative rejected
		{"5x", 0, true},  // unknown suffix, not a valid duration
	}

	for _, c := range cases {
		got, err := parseSince(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseSince(%q): want error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSince(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSince(%q): want %v, got %v", c.in, c.want, got)
		}
	}
}
