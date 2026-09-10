package mysqlbinlog

import (
	"os"
	"path/filepath"
	"strings"
)

// indexSuffix is the server's own list of binary log segments, written next
// to them as <log_bin_basename>.index.
const indexSuffix = ".index"

// minSequenceDigits is the width MySQL and MariaDB use for the numbered
// suffix of a binary log segment. It grows past six digits once the sequence
// rolls over, so this is a floor, not an exact length.
const minSequenceDigits = 6

// isBinlogSegment reports whether name is a binary log segment.
//
// The name of a segment is "<log_bin_basename>.<sequence>", and the basename
// is a server setting: stock MySQL 8 uses "binlog", MariaDB and older MySQL
// use "mariadb-bin" and "mysql-bin", and an operator may set anything. Before
// #190 this matched the two legacy prefixes only, so a default MySQL 8 server
// archived zero files and reported nothing, leaving an incremental chain with
// no logs to replay. The numbered suffix is the part that does not vary.
func isBinlogSegment(name string) bool {
	if strings.HasSuffix(name, indexSuffix) {
		return false
	}

	dot := strings.LastIndex(name, ".")
	if dot <= 0 {
		return false
	}

	sequence := name[dot+1:]
	if len(sequence) < minSequenceDigits {
		return false
	}
	for _, r := range sequence {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// segmentNamesFromIndex reads the server's binary log index, if one is
// present in dir, and returns the segment names it lists. The index is the
// authoritative answer to "which files are binary logs"; it is advisory here
// because a staged or copied binlog directory may arrive without it.
// Entries are reduced to their base names: the server writes them relative
// to its own datadir, which is not necessarily this directory.
func segmentNamesFromIndex(dir string) map[string]bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	names := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), indexSuffix) {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			names[filepath.Base(line)] = true
		}
	}

	return names
}
