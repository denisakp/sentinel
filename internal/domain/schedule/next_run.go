package schedule

// Pure schedule model + next-run abstraction.
//
// Spec 037: the cron parser dependency (github.com/robfig/cron/v3) lives in
// internal/scheduler/scheduler.go (the runtime adapter). Domain code accepts
// a parsed Schedule via the interface below — cron.Schedule already satisfies
// it implicitly (it exposes Next(time.Time) time.Time). Callers receive a
// Schedule value from the runtime adapter constructor; domain code only
// invokes Next.

import (
	"errors"
	"strings"
	"time"
)

// JobKind enumerates the high-level categories of scheduled jobs Sentinel
// owns. Spec 037 carved this from internal/scheduler/types.go where job kind
// was implicit (every job was a backup).
type JobKind string

const (
	KindBackup         JobKind = "backup"
	KindRestore        JobKind = "restore"
	KindRetention      JobKind = "retention"
	KindIntegrityCheck JobKind = "integrity"
)

// ScheduledJob is the pure-data descriptor of a scheduled job, suitable for
// validation and persistence at the configuration boundary.
type ScheduledJob struct {
	Name     string
	CronExpr string
	Kind     JobKind
	Enabled  bool
}

// Schedule is an opaque parsed cron expression. The runtime adapter
// (internal/scheduler/scheduler.go) provides the concrete implementation via
// github.com/robfig/cron/v3 — cron.Schedule satisfies this interface
// implicitly with its Next(time.Time) time.Time method.
type Schedule interface {
	Next(after time.Time) time.Time
}

// NextRun forwards to Schedule.Next; provided as a package-level helper so
// callers don't import a cron library transitively when they only want the
// next firing time.
func NextRun(s Schedule, after time.Time) time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.Next(after)
}

// ErrInvalidJob is returned by Validate when a ScheduledJob is structurally
// invalid (empty name, empty cron expression, unknown kind).
var ErrInvalidJob = errors.New("invalid scheduled job")

// Validate returns an error if a ScheduledJob is structurally invalid. It
// does NOT parse the cron expression — parsing belongs to the runtime
// adapter that owns the cron library dependency.
func Validate(j ScheduledJob) error {
	if strings.TrimSpace(j.Name) == "" {
		return errors.Join(ErrInvalidJob, errors.New("name is required"))
	}
	if strings.TrimSpace(j.CronExpr) == "" {
		return errors.Join(ErrInvalidJob, errors.New("cron_expr is required"))
	}
	switch j.Kind {
	case KindBackup, KindRestore, KindRetention, KindIntegrityCheck:
	case "":
		return errors.Join(ErrInvalidJob, errors.New("kind is required"))
	default:
		return errors.Join(ErrInvalidJob, errors.New("kind must be backup, restore, retention, or integrity"))
	}
	return nil
}
