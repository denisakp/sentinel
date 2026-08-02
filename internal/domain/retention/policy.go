package retention

import (
	"sort"
	"strings"
	"time"
)

// CalculateCandidates determines which backups should be deleted under the
// given Policy at the given time. Pure: same inputs → same output.
//
// Relocated from internal/retention/calculator.go by spec 037.
func CalculateCandidates(records []BackupRecord, policy Policy, now time.Time) []BackupCandidate {
	filtered := filterSuccess(records)
	if len(filtered) == 0 {
		return nil
	}

	hasFlat := policy.KeepLast > 0 || policy.KeepDays > 0
	hasGFS := !policy.GFS.IsZero()
	if !hasFlat && !hasGFS {
		return nil
	}

	// Newest-first, with FilePath as a total-order tiebreaker so anchor
	// selection (and the returned order) is deterministic even when two backups
	// share an identical timestamp.
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].Timestamp.Equal(filtered[j].Timestamp) {
			return filtered[i].Timestamp.After(filtered[j].Timestamp)
		}
		return filtered[i].FilePath < filtered[j].FilePath
	})

	// Flat delete-set: FilePath -> combined flat reason. A record is in this map
	// only when the flat rules would discard it.
	flatDeleteReason := map[string]string{}
	if policy.KeepLast > 0 && len(filtered) > policy.KeepLast {
		for _, rec := range filtered[policy.KeepLast:] {
			flatDeleteReason[rec.FilePath] = "exceeded keep_last"
		}
	}
	if policy.KeepDays > 0 {
		cutoff := now.AddDate(0, 0, -policy.KeepDays)
		for _, rec := range filtered {
			if rec.Timestamp.Before(cutoff) {
				mergeReason(flatDeleteReason, rec.FilePath, "exceeded keep_days")
			}
		}
	}

	// GFS keep-set: FilePaths retained by any configured GFS tier.
	var gfsKeep map[string]struct{}
	if hasGFS {
		gfsKeep = computeGFSKeepSet(filtered, policy.GFS)
	}

	// Union of keeps: a record is a candidate only when EVERY configured rule
	// would discard it. An unconfigured rule abstains (does not block deletion).
	candidateMap := map[string]*BackupCandidate{}
	for i := range filtered {
		rec := filtered[i]

		flatReason, flatDeletes := flatDeleteReason[rec.FilePath]
		flatWantsDelete := !hasFlat || flatDeletes

		_, gfsKeeps := gfsKeep[rec.FilePath]
		gfsWantsDelete := !hasGFS || !gfsKeeps

		if !(flatWantsDelete && gfsWantsDelete) {
			continue // kept by at least one configured rule
		}

		reason := ""
		if hasFlat && flatDeletes {
			reason = flatReason
		}
		if hasGFS {
			if reason == "" {
				reason = ReasonNotRetainedByGFS
			} else {
				reason = reason + ", " + ReasonNotRetainedByGFS
			}
		}

		candidateMap[rec.FilePath] = &BackupCandidate{
			FilePath:      rec.FilePath,
			Timestamp:     rec.Timestamp,
			FileSize:      rec.FileSize,
			Status:        rec.Status,
			ReasonDeleted: reason,
			BackupType:    rec.BackupType,
			ChainID:       rec.ChainID,
			ChainIndex:    rec.ChainIndex,
		}
	}

	// Safety: keep at least one backup (the newest).
	if len(candidateMap) >= len(filtered) {
		delete(candidateMap, filtered[0].FilePath)
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

// mergeReason records or appends a flat deletion reason for a FilePath,
// preserving order and avoiding duplicate reason fragments.
func mergeReason(reasons map[string]string, filePath, reason string) {
	if existing, ok := reasons[filePath]; ok {
		if !strings.Contains(existing, reason) {
			reasons[filePath] = existing + ", " + reason
		}
		return
	}
	reasons[filePath] = reason
}

// ProtectActiveBaseline removes from candidates the active baseline backup of
// the chain currently in flight. Caller passes the same records slice used to
// derive candidates. Pure.
//
// Relocated from internal/retention/retention.go by spec 037.
func ProtectActiveBaseline(candidates []BackupCandidate, records []BackupRecord) []BackupCandidate {
	if len(candidates) == 0 || len(records) == 0 {
		return candidates
	}

	latest := records[0]
	if latest.ChainID == "" {
		return candidates
	}

	protectedPath := ""
	for i := range records {
		record := records[i]
		if record.ChainID != latest.ChainID {
			continue
		}
		if record.BackupType == "full" || record.ChainIndex == 0 {
			protectedPath = record.FilePath
			break
		}
	}

	if protectedPath == "" {
		return candidates
	}

	filtered := make([]BackupCandidate, 0, len(candidates))
	for i := range candidates {
		candidate := candidates[i]
		if candidate.FilePath == protectedPath {
			continue
		}
		filtered = append(filtered, candidate)
	}

	return filtered
}
