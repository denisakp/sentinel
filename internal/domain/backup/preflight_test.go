package backup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/ports/dbprobertesting"
)

func TestCheckIncrementalPrerequisites(t *testing.T) {
	cases := []struct {
		name     string
		job      Job
		caps     *ports.IncrementalCapabilities
		wantErr  string
		wantWarn bool
	}{
		{"disabled skips probe", Job{Engine: "mysql"}, &ports.IncrementalCapabilities{}, "", false},
		{"mysql log_bin off", Job{Engine: "mysql", IncrementalEnabled: true}, &ports.IncrementalCapabilities{}, "log_bin_off", false},
		{"mariadb log_bin off", Job{Engine: "mariadb", IncrementalEnabled: true}, &ports.IncrementalCapabilities{}, "log_bin_off", false},
		{"mysql log_bin on", Job{Engine: "mysql", IncrementalEnabled: true}, &ports.IncrementalCapabilities{LogBinEnabled: true}, "", false},
		{"mongo standalone", Job{Engine: "mongodb", IncrementalEnabled: true}, &ports.IncrementalCapabilities{}, "oplog_unavailable_standalone", false},
		{"mongo replica ok", Job{Engine: "mongodb", IncrementalEnabled: true, OplogWindowWarnHours: 24}, &ports.IncrementalCapabilities{ReplicaSet: true, OplogWindowHours: 48}, "", false},
		{"mongo window low warns", Job{Engine: "mongodb", IncrementalEnabled: true, OplogWindowWarnHours: 24}, &ports.IncrementalCapabilities{ReplicaSet: true, OplogWindowHours: 2}, "", true},
		{"mongo window check disabled", Job{Engine: "mongodb", IncrementalEnabled: true}, &ports.IncrementalCapabilities{ReplicaSet: true, OplogWindowHours: 1}, "", false},
		{"postgres old version", Job{Engine: "postgres", IncrementalEnabled: true}, &ports.IncrementalCapabilities{VersionMajor: 16}, "postgres_version_insufficient", false},
		{"postgres wal summary off", Job{Engine: "postgres", IncrementalEnabled: true}, &ports.IncrementalCapabilities{VersionMajor: 17}, "wal_summary_disabled", false},
		{"postgres ok", Job{Engine: "postgres", IncrementalEnabled: true}, &ports.IncrementalCapabilities{VersionMajor: 17, WALSummaryEnabled: true}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := dbprobertesting.NewMockProber()
			p.SeedIncrementalCapabilities(c.caps, nil)
			warns, err := CheckIncrementalPrerequisites(context.Background(), p, c.job)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
			if (len(warns) > 0) != c.wantWarn {
				t.Fatalf("warnings=%v wantWarn=%v", warns, c.wantWarn)
			}
		})
	}
}

func TestCheckIncrementalPrerequisitesProbeFailureIsError(t *testing.T) {
	p := dbprobertesting.NewMockProber()
	p.SeedIncrementalCapabilities(nil, errors.New("connection refused"))
	if _, err := CheckIncrementalPrerequisites(context.Background(), p, Job{Engine: "mysql", IncrementalEnabled: true}); err == nil {
		t.Fatal("probe failure must not be treated as a pass")
	}
}

func TestCheckIncrementalPrerequisitesNilProber(t *testing.T) {
	if _, err := CheckIncrementalPrerequisites(context.Background(), nil, Job{Engine: "mysql", IncrementalEnabled: true}); err == nil {
		t.Fatal("nil prober with incremental enabled must fail loudly")
	}
}

func TestExecutorRunFailsBeforeDumpWhenLogBinOff(t *testing.T) {
	job, artifact := testJob(t)
	job.Engine = "mysql"
	job.IncrementalEnabled = true
	dumps := &fakeDumpBuilder{digest: "abc", writePath: artifact}
	prober := dbprobertesting.NewMockProber()
	prober.SeedIncrementalCapabilities(&ports.IncrementalCapabilities{LogBinEnabled: false}, nil)

	e := NewExecutor(dumps, nil, nil, nil, &stubRecorder{}, nil, nil, prober, &stubManifestStore{})
	_, err := e.Run(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "log_bin_off") {
		t.Fatalf("want log_bin_off error, got %v", err)
	}
	if dumps.calls != 0 {
		t.Fatalf("dump must not run, ran %d times", dumps.calls)
	}
}
