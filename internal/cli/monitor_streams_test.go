package cli

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/spf13/cobra"
)

// captureProcessStreams replaces the process's real stdout and stderr with pipes
// and returns what each received.
//
// This has to work at the process level, and that is the whole lesson of #165.
// Cobra's Print family writes to OutOrStderr(), which returns the command's own
// writer when one has been set and os.Stderr otherwise. So a test that calls
// cmd.SetOut, which is the normal idiom and what every existing monitor test
// does, makes Print write to the test buffer and therefore passes whether the
// production code is right or wrong.
//
// That is not a hypothetical. Written the idiomatic way first, this test passed
// with the bug deliberately reinstated. A test that cannot fail is worse than no
// test: it reports coverage over a defect. Leaving the command's writers unset is
// the only way to observe where the output really goes.
func captureProcessStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stderr pipe: %v", err)
	}

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	outCh, errCh := make(chan string, 1), make(chan string, 1)
	go func() { b, _ := io.ReadAll(outR); outCh <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); errCh <- string(b) }()

	fn()

	_ = outW.Close()
	_ = errW.Close()
	return <-outCh, <-errCh
}

var streamTestExecutions = []ports.Execution{{
	BackupName:    "pg-job",
	DatabaseType:  "postgres",
	Status:        "success",
	Timestamp:     time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
	FilePath:      "/backups/pg-job.sql",
	FileSizeBytes: 2048,
}}

// TestMonitorDataGoesToStdout is the regression guard for #165.
//
// Every monitor subcommand sent its output to stderr, so
// `monitor export --format json > history.json` produced an empty file while the
// data scrolled past on the terminal, and `monitor list --format json | jq` piped
// nothing. No error either way. Redirection is the entire point of export.
//
// Each case uses a bare command with no writers set, so the output goes wherever
// production code sends it. See captureProcessStreams for why that is essential.
func TestMonitorDataGoesToStdout(t *testing.T) {
	cases := []struct {
		name string
		run  func(*cobra.Command) error
		want string
	}{
		{"list table", func(c *cobra.Command) error { return printTable(c, streamTestExecutions) }, "pg-job"},
		{"list json", func(c *cobra.Command) error { return printJSON(c, streamTestExecutions) }, "pg-job"},
		{"show execution", func(c *cobra.Command) error {
			printExecution(c, &streamTestExecutions[0])
			return nil
		}, "pg-job"},
		{"stats", func(c *cobra.Command) error {
			printStats(c, &monitor.Statistics{BackupName: "pg-job", JobsPeriod: "all time", TotalExecutions: 1})
			return nil
		}, "pg-job"},
		{"no matching records", func(c *cobra.Command) error {
			printNoMatchingRecords(c)
			return nil
		}, noMatchingRecordsMessage},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			stdout, stderr := captureProcessStreams(t, func() {
				// Deliberately no SetOut or SetErr: that is what makes this
				// observable at all.
				runErr = tc.run(&cobra.Command{})
			})
			if runErr != nil {
				t.Fatalf("run error = %v", runErr)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("data did not reach stdout, so redirecting this command yields an empty file.\n"+
					"stdout: %q\nstderr: %q", stdout, stderr)
			}
			if strings.Contains(stderr, tc.want) {
				t.Errorf("data reached stderr; it belongs on stdout so it can be redirected or piped.\n"+
					"stderr: %q", stderr)
			}
		})
	}
}
