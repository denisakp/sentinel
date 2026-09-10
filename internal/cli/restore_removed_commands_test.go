package cli

import (
	"bytes"
	"strings"
	"testing"
)

// removedRestoreSubcommands are the four that printed a success message and
// persisted nothing. Removed by the maintainer's decision on #137: printing
// "enabled" while changing nothing is the one option that must not survive, and
// the only thing that actually enables a job is `enabled: true` in the YAML.
var removedRestoreSubcommands = []string{"enable", "disable", "pause", "resume"}

// TestRemovedRestoreSubcommandsAreGone is the regression guard for #137.
func TestRemovedRestoreSubcommandsAreGone(t *testing.T) {
	present := map[string]bool{}
	for _, c := range restoreCmd.Commands() {
		present[strings.Fields(c.Use)[0]] = true
	}
	for _, name := range removedRestoreSubcommands {
		if present[name] {
			t.Errorf("restore %s is registered again. It cannot persist anything, so it would "+
				"report success while leaving the job exactly as it was", name)
		}
	}
}

// TestRemovedRestoreSubcommandsFailLoudly is the half that matters more, and the
// half that was nearly shipped wrong.
//
// A Cobra command with no Run is not Runnable, and Cobra returns flag.ErrHelp for
// those BEFORE validating Args, while Execute treats ErrHelp as success. Removing
// the subcommands was therefore not enough on its own: `restore enable nightly`
// printed the whole help and exited 0, so a script calling it would have carried
// on as though it had worked. That is the exact failure this batch exists to
// remove, reintroduced by the removal.
//
// restoreCmd now has both Args: NoArgs and a RunE, which makes it Runnable so
// NoArgs actually runs.
func TestRemovedRestoreSubcommandsFailLoudly(t *testing.T) {
	for _, name := range removedRestoreSubcommands {
		t.Run(name, func(t *testing.T) {
			// Through RootCmd: Cobra's Execute on a child delegates to the root,
			// so driving the child directly would run the root with the process's
			// own arguments instead of these.
			var out, errOut bytes.Buffer
			RootCmd.SetOut(&out)
			RootCmd.SetErr(&errOut)
			RootCmd.SetArgs([]string{"restore", name, "nightly"})
			t.Cleanup(func() {
				RootCmd.SetArgs(nil)
				RootCmd.SetOut(nil)
				RootCmd.SetErr(nil)
			})

			err := RootCmd.Execute()
			if err == nil {
				t.Fatalf("restore %s returned no error, so the process exits 0 and a script "+
					"calling it cannot tell that nothing happened", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the error does not name the subcommand the caller typed: %v", err)
			}
		})
	}
}

// TestBareRestorePrintsHelpAndSucceeds: adding RunE must not turn the command
// group itself into an error. `sentinel restore` with no arguments should show
// help and exit 0, as a command group conventionally does.
func TestBareRestorePrintsHelpAndSucceeds(t *testing.T) {
	var out, errOut bytes.Buffer
	RootCmd.SetOut(&out)
	RootCmd.SetErr(&errOut)
	RootCmd.SetArgs([]string{"restore"})
	t.Cleanup(func() {
		RootCmd.SetArgs(nil)
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
	})

	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("bare `restore` should print help and succeed, got %v", err)
	}
	if !strings.Contains(out.String(), "enabled: true") {
		t.Error("the help no longer points at the configuration key, which is now the only way " +
			"to enable a job")
	}
}

// TestSurvivingRestoreSubcommandsStillRegistered guards against the removal
// having taken a neighbour with it.
func TestSurvivingRestoreSubcommandsStillRegistered(t *testing.T) {
	present := map[string]bool{}
	for _, c := range restoreCmd.Commands() {
		present[strings.Fields(c.Use)[0]] = true
	}
	for _, name := range []string{"list", "status", "dry-run", "run", "history", "validate-chain"} {
		if !present[name] {
			t.Errorf("restore %s went missing", name)
		}
	}
}
