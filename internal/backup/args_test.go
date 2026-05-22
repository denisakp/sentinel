package backup

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseAdditionalArgs(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      []string
		wantErrIs error
	}{
		{name: "empty", input: "", want: []string{}},
		{name: "whitespace only", input: "   \t  ", want: []string{}},
		{name: "single unquoted flag", input: "--flush-privileges", want: []string{"--flush-privileges"}},
		{name: "flag=value unquoted", input: "--port=3306", want: []string{"--port=3306"}},
		{name: "two flags space-separated", input: "--no-data --no-acl", want: []string{"--no-data", "--no-acl"}},
		{
			name:  "double-quoted value with space",
			input: `--exclude-table-data="audit logs"`,
			want:  []string{"--exclude-table-data=audit logs"},
		},
		{
			name:  "single-quoted value with space",
			input: `--where='id > 100'`,
			want:  []string{"--where=id > 100"},
		},
		{
			name:  "escaped quotes inside double quotes",
			input: `"a \"b\" c"`,
			want:  []string{`a "b" c`},
		},
		{
			name:  "single quotes nested inside double quotes",
			input: `--where="updated_at > '2026-01-01'"`,
			want:  []string{`--where=updated_at > '2026-01-01'`},
		},
		{
			name:  "backslash-escaped space outside quotes",
			input: `--name=foo\ bar`,
			want:  []string{"--name=foo bar"},
		},
		{name: "dollar var literal", input: "$VAR", want: []string{"$VAR"}},
		{name: "glob char literal", input: "*.dump", want: []string{"*.dump"}},
		{name: "hash at word boundary is POSIX comment", input: "#hash", want: []string{}},
		{name: "hash mid-token is literal", input: "--prefix=#tag", want: []string{"--prefix=#tag"}},
		{name: "escaped hash at start is literal", input: `\#hash`, want: []string{"#hash"}},
		{name: "quoted hash at start is literal", input: `"#hash"`, want: []string{"#hash"}},
		{name: "backtick literal", input: "`cmd`", want: []string{"`cmd`"}},
		{name: "dollar paren literal", input: "$(cmd)", want: []string{"$(cmd)"}},
		{
			name:      "unterminated double quote",
			input:     `--where="x > 1`,
			wantErrIs: ErrUnterminatedQuote,
		},
		{
			name:      "unterminated single quote",
			input:     `--where='x > 1`,
			wantErrIs: ErrUnterminatedQuote,
		},
		{
			name:      "NUL byte rejected",
			input:     "--foo\x00bar",
			wantErrIs: ErrNULByte,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAdditionalArgs(tt.input)
			if tt.wantErrIs != nil {
				if err == nil {
					t.Fatalf("expected error %v, got tokens %v", tt.wantErrIs, got)
				}
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("expected errors.Is(err, %v), got %v", tt.wantErrIs, err)
				}
				if got != nil {
					t.Fatalf("expected nil tokens on error, got %v", got)
				}
				if !strings.Contains(err.Error(), "additional_args") {
					t.Fatalf("error message should reference additional_args; got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestParseAdditionalArgs_Backcompat(t *testing.T) {
	// Locks SC-002: canonical unquoted inputs the prior regex handled produce
	// byte-identical token slices vs golden values. Quote-leaking cases are
	// covered separately in TestParseAdditionalArgs above.
	tests := []struct {
		input string
		want  []string
	}{
		{"--flush-privileges", []string{"--flush-privileges"}},
		{"--port=3306", []string{"--port=3306"}},
		{"--skip-lock-tables", []string{"--skip-lock-tables"}},
		{"--no-data", []string{"--no-data"}},
		{"--password secret", []string{"--password", "secret"}},
		{"--port=3306 --single-transaction", []string{"--port=3306", "--single-transaction"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseAdditionalArgs(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestRemoveArgsDuplicate(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "empty", in: nil, want: nil},
		{name: "no dups", in: []string{"a", "b", "c"}, want: []string{"a", "b", "c"}},
		{name: "drops dups preserves first-seen order", in: []string{"a", "b", "a", "c", "b"}, want: []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveArgsDuplicate(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}
