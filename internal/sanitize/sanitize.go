package sanitize

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// credentialPatterns lists regex patterns that match credential-bearing arguments.
// Each pattern captures groups whose values must be replaced by the placeholder.
var credentialPatterns = []*regexp.Regexp{
	// P1: --password=VALUE  (inline value after '=')
	regexp.MustCompile(`(?i)(--password=)(\S+)`),
	// P2: -pVALUE  (mysqldump style: single-dash p immediately followed by non-dash chars)
	// Must NOT match '--p...' (double-dash) or '-p ' (space-separated, handled by two-token pass).
	regexp.MustCompile(`(?im)^(-p)([^- ]\S*)`),
	// P3: PGPASSWORD=VALUE  (env style)
	regexp.MustCompile(`(?i)(PGPASSWORD=)(\S+)`),
	// P4: MYSQL_PWD=VALUE
	regexp.MustCompile(`(?i)(MYSQL_PWD=)(\S+)`),
	// P5: URI userinfo — scheme://user:secret@host  (redact only the password segment)
	regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^/\s:@]+:)([^@\s]+)(@)`),
	// P6: MONGO_INITDB_ROOT_PASSWORD=VALUE
	regexp.MustCompile(`(?i)(MONGO_INITDB_ROOT_PASSWORD=)(\S+)`),
	// P7: libpq-style "password = VALUE" / "password: VALUE" with optional whitespace
	regexp.MustCompile(`(?i)(password\s*[:=]\s*)(\S+)`),
}

const redacted = "*****"

// stderrCap bounds the embedded stderr buffer in bytes (64 KiB).
const stderrCap = 65536

// truncationMarkerFmt formats the suffix appended when input exceeds stderrCap.
const truncationMarkerFmt = "\n[stderr truncated, %d bytes elided]"

// RedactStats summarizes a RedactStderr invocation. Safe to log; contains no secret material.
type RedactStats struct {
	BytesIn         int
	BytesRedacted   int
	PatternsMatched int
	Truncated       bool
	BytesOut        int
}

// redactWithPattern applies a single pattern and returns the new string plus
// count of matches and bytes replaced (input value bytes minus placeholder bytes).
func redactWithPattern(s string, pat *regexp.Regexp) (string, int, int) {
	matches := pat.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, 0, 0
	}
	var b strings.Builder
	b.Grow(len(s))
	prev := 0
	bytesRedacted := 0
	for _, m := range matches {
		// Pattern P5 has three capture groups (prefix, secret, suffix); others have two (prefix, secret).
		// Last capture group is the secret position when group count == 2; for P5 the secret is group 2 (index 4..5).
		var prefixEnd, secretStart, secretEnd, tailStart int
		if len(m) >= 8 {
			// 3 groups → P5: prefix, secret, suffix
			prefixEnd = m[3]
			secretStart = m[4]
			secretEnd = m[5]
			tailStart = m[6]
		} else if len(m) >= 6 {
			prefixEnd = m[3]
			secretStart = m[4]
			secretEnd = m[5]
			tailStart = secretEnd
		} else {
			continue
		}
		b.WriteString(s[prev:prefixEnd])
		b.WriteString(redacted)
		bytesRedacted += secretEnd - secretStart
		// Preserve suffix (e.g. '@' for URI form) between secret and tail.
		if tailStart > secretEnd {
			b.WriteString(s[secretEnd:tailStart])
		}
		prev = tailStart
	}
	b.WriteString(s[prev:])
	return b.String(), len(matches), bytesRedacted
}

// redactString applies all credentialPatterns to a single string token.
func redactString(s string) string {
	for _, pat := range credentialPatterns {
		s, _, _ = redactWithPattern(s, pat)
	}
	return s
}

// RedactArgs returns a copy of args with credential values replaced by "*****".
// The original slice is not modified.
func RedactArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	result := make([]string, len(args))
	copy(result, args)

	// Single-token redaction (e.g. --password=secret, PGPASSWORD=val, -psecret)
	for i, arg := range result {
		result[i] = redactString(arg)
	}

	// Two-token credential patterns: ["--password", "secret"] or ["-p", "secret"]
	for i := 0; i < len(result)-1; i++ {
		lower := strings.ToLower(result[i])
		if lower == "--password" || lower == "-p" || lower == "pgpassword" {
			result[i+1] = redacted
		}
	}

	return result
}

// RedactLog replaces credential patterns in a log string with "*****".
func RedactLog(s string) string {
	for _, pat := range credentialPatterns {
		s, _, _ = redactWithPattern(s, pat)
	}
	return s
}

// RedactStderr applies all credentialPatterns to b, caps the result at 64 KiB
// (appending a truncation marker if the cap is exceeded), and returns the
// redacted string plus statistics suitable for debug logging.
//
// When redaction or truncation occurred, the function emits a single
// slog.Debug record with the numeric keys bytes_redacted, patterns_matched,
// and truncated. The record contains no secret material.
func RedactStderr(b []byte) (string, RedactStats) {
	stats := RedactStats{BytesIn: len(b)}
	if len(b) == 0 {
		return "", stats
	}

	s := string(b)
	for _, pat := range credentialPatterns {
		out, matched, redactedBytes := redactWithPattern(s, pat)
		s = out
		stats.PatternsMatched += matched
		stats.BytesRedacted += redactedBytes
	}

	if len(s) > stderrCap {
		elided := len(s) - stderrCap
		s = s[:stderrCap] + fmt.Sprintf(truncationMarkerFmt, elided)
		stats.Truncated = true
	}
	stats.BytesOut = len(s)

	if stats.PatternsMatched > 0 || stats.Truncated {
		slog.Debug("sanitize.stderr_redacted",
			"bytes_redacted", stats.BytesRedacted,
			"patterns_matched", stats.PatternsMatched,
			"truncated", stats.Truncated,
		)
	}

	return s, stats
}
