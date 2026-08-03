package mongo_tls

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

var materialPIDRegex = regexp.MustCompile(`sentinel-mongo-tls-(\d+)-`)

// SweepOrphanMaterial scans dir for combined-material temp files whose owning
// PID is no longer alive, removes them, and returns the count removed. Errors
// are aggregated; partial progress is preserved. Intended to run once on
// Sentinel startup as a backstop for hard-killed prior processes.
func SweepOrphanMaterial(dir string) (int, error) {
	matches, err := filepath.Glob(materialGlob(dir))
	if err != nil {
		return 0, fmt.Errorf("glob mongo-tls material in %q: %w", dir, err)
	}
	if len(matches) == 0 {
		return 0, nil
	}

	var (
		removed int
		errs    []error
	)
	for _, path := range matches {
		base := filepath.Base(path)
		sub := materialPIDRegex.FindStringSubmatch(base)
		if len(sub) != 2 {
			continue
		}
		pid, perr := strconv.Atoi(sub[1])
		if perr != nil {
			continue
		}
		if processAlive(pid) {
			continue
		}
		if rerr := os.Remove(path); rerr != nil {
			errs = append(errs, fmt.Errorf("remove %q: %w", path, rerr))
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("mongo-tls: orphan sweep removed files",
			"event", "mongo_tls_orphan_swept",
			"count", removed,
			"dir", dir)
	}
	if len(errs) > 0 {
		return removed, errors.Join(errs...)
	}
	return removed, nil
}
