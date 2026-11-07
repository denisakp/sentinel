package db_probe

import (
	"context"
	"errors"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestParseBoolSetting(t *testing.T) {
	for in, want := range map[string]bool{"1": true, "ON": true, "on": true, "true": true, "0": false, "OFF": false, "": false, "garbage": false} {
		if got := parseBoolSetting(in); got != want {
			t.Errorf("parseBoolSetting(%q)=%v want %v", in, got, want)
		}
	}
}

func TestParsePostgresMajor(t *testing.T) {
	for in, want := range map[string]int{"170002": 17, "160004": 16, "90624": 9} {
		got, err := parsePostgresMajor(in)
		if err != nil || got != want {
			t.Errorf("parsePostgresMajor(%q)=%d,%v want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "0"} {
		if _, err := parsePostgresMajor(bad); err == nil {
			t.Errorf("parsePostgresMajor(%q) expected error", bad)
		}
	}
}

func TestOplogWindowHours(t *testing.T) {
	cases := []struct {
		first, last uint32
		want        int
	}{{0, 0, 0}, {100, 50, 0}, {1000, 1000 + 3600*25 + 5, 25}, {1000, 1000 + 3599, 0}}
	for _, c := range cases {
		if got := oplogWindowHours(c.first, c.last); got != c.want {
			t.Errorf("oplogWindowHours(%d,%d)=%d want %d", c.first, c.last, got, c.want)
		}
	}
}

func TestProbeIncrementalCapabilitiesUnsupportedEngine(t *testing.T) {
	_, err := NewAdapter().ProbeIncrementalCapabilities(context.Background(), ports.DatabaseConfig{Type: "oracle"})
	if !errors.Is(err, ports.ErrUnsupportedEngine) {
		t.Fatalf("want ErrUnsupportedEngine, got %v", err)
	}
}

func TestProbeMongoCapabilitiesRequiresURI(t *testing.T) {
	if _, err := NewAdapter().ProbeIncrementalCapabilities(context.Background(), ports.DatabaseConfig{Type: "mongodb"}); err == nil {
		t.Fatal("expected error for empty uri")
	}
}
