package retention

import (
	"sort"
	"strings"
	"time"
)

// CalculateCandidates determines which backups should be deleted.
func CalculateCandidates(records []BackupRecord, policy Policy, now time.Time) []BackupCandidate {
	filtered := filterSuccess(records)
	if len(filtered) == 0 {
		return nil
	}

	if policy.KeepLast == 0 && policy.KeepDays == 0 {
		return nil
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})

	candidateMap := map[string]*BackupCandidate{}

	if policy.KeepLast > 0 && len(filtered) > policy.KeepLast {
		for _, rec := range filtered[policy.KeepLast:] {
			addCandidate(candidateMap, rec, "exceeded keep_last")
		}
	}

	if policy.KeepDays > 0 {
		cutoff := now.AddDate(0, 0, -policy.KeepDays)
		for _, rec := range filtered {
			if rec.Timestamp.Before(cutoff) {
				addCandidate(candidateMap, rec, "exceeded keep_days")
			}
		}
	}

	// Safety: keep at least one backup
	if len(candidateMap) >= len(filtered) {
		latest := filtered[0].FilePath
		delete(candidateMap, latest)
	}

	candidates := make([]BackupCandidate, 0, len(candidateMap))
	for _, cand := range candidateMap {
		candidates = append(candidates, *cand)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Timestamp.After(candidates[j].Timestamp)
	})

	return candidates
}

func filterSuccess(records []BackupRecord) []BackupRecord {
	filtered := make([]BackupRecord, 0, len(records))
	for _, rec := range records {
		if rec.Status != "success" {
			continue
		}
		if rec.FilePath == "" {
			continue
		}
		filtered = append(filtered, rec)
	}
	return filtered
}

func addCandidate(candidateMap map[string]*BackupCandidate, rec BackupRecord, reason string) {
	if existing, ok := candidateMap[rec.FilePath]; ok {
		if !strings.Contains(existing.ReasonDeleted, reason) {
			existing.ReasonDeleted = existing.ReasonDeleted + ", " + reason
		}
		return
	}
	candidateMap[rec.FilePath] = &BackupCandidate{
		FilePath:      rec.FilePath,
		Timestamp:     rec.Timestamp,
		FileSize:      rec.FileSize,
		Status:        rec.Status,
		ReasonDeleted: reason,
	}
}
