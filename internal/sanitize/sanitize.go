package sanitize

import (
	"regexp"
	"strings"
)

// credentialPatterns lists regex patterns that match credential-bearing arguments.
// Each pattern captures two groups: (prefix)(value).
var credentialPatterns = []*regexp.Regexp{
	// --password=VALUE  (inline value after '=')
	regexp.MustCompile(`(?i)(--password=)(\S+)`),
	// -pVALUE  (mysqldump style: single-dash p immediately followed by non-dash chars)
	// Must NOT match '--p...' (double-dash) or '-p ' (space-separated, handled by two-token pass).
	regexp.MustCompile(`(?i)^(-p)([^- ]\S*)`),
	// PGPASSWORD=VALUE  (env style)
	regexp.MustCompile(`(?i)(PGPASSWORD=)(\S+)`),
	// MYSQL_PWD=VALUE
	regexp.MustCompile(`(?i)(MYSQL_PWD=)(\S+)`),
}

const redacted = "*****"

// redactString applies all credentialPatterns to a single string token.
func redactString(s string) string {
	for _, pat := range credentialPatterns {
		if pat.MatchString(s) {
			s = pat.ReplaceAllStringFunc(s, func(match string) string {
				locs := pat.FindStringSubmatchIndex(match)
				if len(locs) < 6 {
					return match
				}
				prefix := match[locs[2]:locs[3]]
				return prefix + redacted
			})
		}
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
		s = pat.ReplaceAllStringFunc(s, func(match string) string {
			locs := pat.FindStringSubmatchIndex(match)
			if len(locs) < 6 {
				return match
			}
			prefix := match[locs[2]:locs[3]]
			return prefix + redacted
		})
	}
	return s
}
