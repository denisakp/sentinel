package mysqlargs

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ClientCreds holds the credential fields read from a my.cnf-format option
// file's [client] section (spec 056 / PRD 44).
type ClientCreds struct {
	Host, User, Password, Port string
}

// ErrDefaultsFileUnreadable indicates the defaults file could not be opened,
// stat'd, or parsed as a valid option file.
var ErrDefaultsFileUnreadable = errors.New("mysqlargs: cannot read defaults file")

// ErrDefaultsFileNoClientSection indicates the file parsed cleanly but has no
// [client] section, or the section is empty. Callers treat this as "nothing
// to contribute" rather than a hard failure on its own (spec 056 edge cases).
var ErrDefaultsFileNoClientSection = errors.New("mysqlargs: defaults file has no [client] section")

// ParseDefaultsFile reads path (a my.cnf-format option file) and returns the
// [client] section's host/user/password/port, plus the file's stat mode so
// the caller can apply a group/world-readable permission warning. Only the
// [client] section is parsed; other sections (e.g. [mysqldump]) and
// !include/!includedir directives are ignored, not errors.
func ParseDefaultsFile(path string) (ClientCreds, os.FileMode, error) {
	f, err := os.Open(path)
	if err != nil {
		return ClientCreds{}, 0, fmt.Errorf("%w '%s': %v", ErrDefaultsFileUnreadable, path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ClientCreds{}, 0, fmt.Errorf("%w '%s': %v", ErrDefaultsFileUnreadable, path, err)
	}

	creds, sawClient, err := parseClientSection(f)
	if err != nil {
		return ClientCreds{}, info.Mode(), fmt.Errorf("%w '%s': %v", ErrDefaultsFileUnreadable, path, err)
	}
	if !sawClient || creds == (ClientCreds{}) {
		return ClientCreds{}, info.Mode(), fmt.Errorf("%w: '%s'", ErrDefaultsFileNoClientSection, path)
	}
	return creds, info.Mode(), nil
}

func parseClientSection(f *os.File) (ClientCreds, bool, error) {
	var creds ClientCreds
	inClient := false
	sawClient := false

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return ClientCreds{}, false, fmt.Errorf("malformed section header: %q", line)
			}
			section := strings.TrimSpace(line[1 : len(line)-1])
			inClient = strings.EqualFold(section, "client")
			if inClient {
				sawClient = true
			}
			continue
		}
		if !inClient {
			continue
		}

		key, value, ok := splitOptionLine(line)
		if !ok {
			continue
		}
		value = unquote(value)
		switch strings.ToLower(key) {
		case "host":
			creds.Host = value
		case "user":
			creds.User = value
		case "password":
			creds.Password = value
		case "port":
			creds.Port = value
		}
	}
	if err := sc.Err(); err != nil {
		return ClientCreds{}, false, err
	}
	return creds, sawClient, nil
}

// splitOptionLine splits "key=value" or "key = value" into trimmed key/value.
// A bare "key" (no '=') is a valid my.cnf option but carries no value we care
// about here, so it is skipped (ok=false).
func splitOptionLine(line string) (key, value string, ok bool) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
