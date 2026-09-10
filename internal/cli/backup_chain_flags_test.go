package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestBackupChainSubcommandsAcceptConfig is the regression guard for #136.
//
// All three of these read --config in their handlers and none of them registered
// it. BackupCmd's --config is a local flag, so a subcommand does not inherit it,
// and the result was that no invocation worked at all: omit the flag and the
// handler refuses for want of a config, pass it and Cobra refuses an unknown
// flag. The entire chain-inspection and forced-full surface was unreachable from
// the command line, and README documented it with --config, so the documented
// usage had never worked.
//
// The assertion parses the flag rather than looking it up, because looking it up
// would also pass for a flag declared on the parent that the subcommand cannot
// actually accept. That distinction is the bug.
func TestBackupChainSubcommandsAcceptConfig(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		args []string
	}{
		{"force-full", backupForceFullCmd, []string{"--config", "/tmp/x.yaml", "--job", "j"}},
		{"chain-status", backupChainStatusCmd, []string{"--config", "/tmp/x.yaml", "--job", "j"}},
		{"chain-list", backupChainListCmd, []string{"--config", "/tmp/x.yaml", "--chain-id", "c1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cmd.ParseFlags(tc.args); err != nil {
				t.Fatalf("backup %s cannot accept its documented flags: %v\n"+
					"Without --config registered there is no invocation of this command that works.",
					tc.name, err)
			}
			got, err := tc.cmd.Flags().GetString("config")
			if err != nil {
				t.Fatalf("backup %s has no --config flag: %v", tc.name, err)
			}
			if got != "/tmp/x.yaml" {
				t.Errorf("backup %s parsed --config as %q, want %q", tc.name, got, "/tmp/x.yaml")
			}
		})
	}
}

// TestBackupChainSubcommandsRequireConfig: these commands read configured jobs
// from a config file and cannot do anything without one, so the requirement
// belongs in flag validation rather than in a runtime error.
func TestBackupChainSubcommandsRequireConfig(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"force-full":   backupForceFullCmd,
		"chain-status": backupChainStatusCmd,
		"chain-list":   backupChainListCmd,
	} {
		t.Run(name, func(t *testing.T) {
			flag := cmd.Flags().Lookup("config")
			if flag == nil {
				t.Fatalf("backup %s has no --config flag", name)
			}
			annotations := flag.Annotations[cobra.BashCompOneRequiredFlag]
			if len(annotations) == 0 || annotations[0] != "true" {
				t.Errorf("backup %s does not mark --config required, so it fails later and "+
					"less clearly than it could", name)
			}
		})
	}
}

// TestBackupParentConfigStaysLocal pins the shape of the fix.
//
// The alternative was promoting BackupCmd's --config to a persistent flag. That
// would leave two flags of the same name in play on `backup diff` and
// `backup verify`, which already declare their own local --config, and a reader
// would have to know Cobra's shadowing rules to predict which one wins. Each
// subcommand declaring its own is the pattern those two already follow.
func TestBackupParentConfigStaysLocal(t *testing.T) {
	if BackupCmd.PersistentFlags().Lookup("config") != nil {
		t.Error("BackupCmd declares --config persistently; `backup diff` and `backup verify` " +
			"declare their own local --config, so this puts two same-named flags in play on them")
	}
	if BackupCmd.Flags().Lookup("config") == nil {
		t.Error("BackupCmd lost its own --config")
	}
	for _, name := range []string{"diff", "verify"} {
		var found *cobra.Command
		for _, c := range BackupCmd.Commands() {
			if strings.HasPrefix(c.Use, name) {
				found = c
			}
		}
		if found == nil {
			continue // not registered in this build
		}
		if found.Flags().Lookup("config") == nil {
			t.Errorf("backup %s lost its own --config", name)
		}
	}
}
