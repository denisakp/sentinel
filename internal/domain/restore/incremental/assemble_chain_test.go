package incremental

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func validLineage() []ChainArtifact {
	T0 := time.Unix(1_700_000_000, 0).UTC()
	out := make([]ChainArtifact, 4)
	for i := 0; i < 4; i++ {
		entry := ChainArtifact{
			BackupID:        fmt.Sprintf("b%d", i),
			ChainIndex:      i,
			TimelineID:      "1",
			ManifestPresent: true,
			HashVerified:    true,
			EndTimestamp:    T0.Add(time.Duration(i) * time.Hour),
		}
		if i > 0 {
			entry.BaselineBackupID = "b0"
		}
		out[i] = entry
	}
	return out
}

func TestAssembleChain(t *testing.T) {
	baselineOnly := []ChainArtifact{validLineage()[0]}

	missingIntermediate := validLineage()
	missingIntermediate[1].ManifestPresent = false

	duplicateArtifact := validLineage()
	duplicateArtifact[2].BackupID = duplicateArtifact[1].BackupID

	outOfOrder := validLineage()
	outOfOrder[2].EndTimestamp = outOfOrder[0].EndTimestamp.Add(-1 * time.Hour)

	T0 := validLineage()[0].EndTimestamp
	T1 := validLineage()[1].EndTimestamp
	T3 := validLineage()[3].EndTimestamp
	cutInside := T1.Add(30 * time.Minute)
	cutAtBoundary := T1
	cutBefore := T0.Add(-1 * time.Hour)
	cutAfter := T3.Add(1 * time.Hour)

	zeroTs := validLineage()
	for i := range zeroTs {
		zeroTs[i].EndTimestamp = time.Time{}
	}
	cutZero := T1

	cases := []struct {
		name          string
		lineage       []ChainArtifact
		cutoff        *time.Time
		wantErrSubstr string
		wantIDs       []string
		wantDepth     int
	}{
		{
			name:      "happy_full_plus_three_incrementals",
			lineage:   validLineage(),
			cutoff:    nil,
			wantIDs:   []string{"b0", "b1", "b2", "b3"},
			wantDepth: 4,
		},
		{
			name:      "happy_full_only_no_incrementals",
			lineage:   baselineOnly,
			cutoff:    nil,
			wantIDs:   []string{"b0"},
			wantDepth: 1,
		},
		{
			name:          "empty_chain",
			lineage:       nil,
			cutoff:        nil,
			wantErrSubstr: "rule_1_baseline_missing",
		},
		{
			name:          "missing_intermediate_delegates_to_rule_2",
			lineage:       missingIntermediate,
			cutoff:        nil,
			wantErrSubstr: "rule_2_intermediate_missing",
		},
		{
			name:          "duplicate_artifact",
			lineage:       duplicateArtifact,
			cutoff:        nil,
			wantErrSubstr: "duplicate_artifact",
		},
		{
			name:          "out_of_order_timestamps",
			lineage:       outOfOrder,
			cutoff:        nil,
			wantErrSubstr: "out_of_order",
		},
		{
			name:      "pitr_cutoff_inside_segment",
			lineage:   validLineage(),
			cutoff:    &cutInside,
			wantIDs:   []string{"b0", "b1"},
			wantDepth: 2,
		},
		{
			name:      "pitr_cutoff_at_exact_boundary_inclusive",
			lineage:   validLineage(),
			cutoff:    &cutAtBoundary,
			wantIDs:   []string{"b0", "b1"},
			wantDepth: 2,
		},
		{
			name:          "pitr_cutoff_before_baseline",
			lineage:       validLineage(),
			cutoff:        &cutBefore,
			wantErrSubstr: "cutoff_before_baseline",
		},
		{
			name:      "pitr_cutoff_after_final_entry_noop",
			lineage:   validLineage(),
			cutoff:    &cutAfter,
			wantIDs:   []string{"b0", "b1", "b2", "b3"},
			wantDepth: 4,
		},
		{
			name:      "all_zero_timestamps_skip_monotonicity_and_cutoff",
			lineage:   zeroTs,
			cutoff:    &cutZero,
			wantIDs:   []string{"b0", "b1", "b2", "b3"},
			wantDepth: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AssembleChain(tc.lineage, tc.cutoff)
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErrSubstr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatalf("expected non-nil ResolvedChain")
			}
			if got.Depth != tc.wantDepth {
				t.Errorf("Depth: got %d want %d", got.Depth, tc.wantDepth)
			}
			if len(got.ArtifactIDs) != len(tc.wantIDs) {
				t.Fatalf("ArtifactIDs len: got %d want %d", len(got.ArtifactIDs), len(tc.wantIDs))
			}
			for i, id := range tc.wantIDs {
				if got.ArtifactIDs[i] != id {
					t.Errorf("ArtifactIDs[%d]: got %q want %q", i, got.ArtifactIDs[i], id)
				}
			}
		})
	}
}
