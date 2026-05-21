package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/spf13/cobra"
)

var monitorDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Inspect and optionally repair monitor schema state",
	Long: "Diagnose the monitor history database and optionally apply pending schema " +
		"migrations. See specs/017-monitor-schema-migration/contracts/monitor-doctor-cli.md " +
		"for exit codes and output shape.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfigFromFlags(cmd)
		if err != nil {
			return err
		}
		repair, _ := cmd.Flags().GetBool("repair")
		asJSON, _ := cmd.Flags().GetBool("json")

		var report monitor.DoctorReport
		if repair {
			report, err = monitor.Repair(cfg.HistoryDBPath)
		} else {
			report, err = monitor.Diagnose(cfg.HistoryDBPath)
		}
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		if asJSON {
			if err := renderDoctorJSON(out, report); err != nil {
				return err
			}
		} else {
			renderDoctorTable(out, report, repair)
		}
		return doctorExitErr(report.Status, repair)
	},
}

// doctorExitErr maps DoctorReport.Status (+ repair mode) to the sentinel
// error whose mapping in Code() produces the contract-mandated exit code.
// Returning nil means exit 0.
func doctorExitErr(status string, repair bool) error {
	switch status {
	case monitor.StatusCurrent:
		return nil
	case monitor.StatusStalePending:
		if repair {
			// repair succeeded → DB should now be current; the matrix says
			// stale-pending under repair maps to 0 only when the migrations
			// actually applied. If we still see stale-pending after repair,
			// something refused to apply; surface as exit 1.
			return ErrDoctorStalePending
		}
		return ErrDoctorStalePending
	case monitor.StatusForwardIncompatible:
		return ErrDoctorForwardIncompat
	case monitor.StatusMissing:
		return ErrDoctorMissing
	case monitor.StatusCorrupt:
		return ErrDoctorCorrupt
	default:
		return fmt.Errorf("unknown doctor status %q", status)
	}
}

// renderDoctorJSON serialises the report per the schema contract.
func renderDoctorJSON(w io.Writer, r monitor.DoctorReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// renderDoctorTable produces the human-readable layout from
// contracts/monitor-doctor-cli.md.
func renderDoctorTable(w io.Writer, r monitor.DoctorReport, repaired bool) {
	fmt.Fprintln(w, "Monitor schema doctor")

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  database:\t%s\n", r.DatabasePath)

	switch r.Status {
	case monitor.StatusMissing:
		fmt.Fprintf(tw, "  status:\t%s\n", r.Status)
		if r.Hint != "" {
			fmt.Fprintf(tw, "  hint:\t%s\n", r.Hint)
		}
		tw.Flush()
		return
	case monitor.StatusCorrupt:
		fmt.Fprintf(tw, "  status:\t%s\n", r.Status)
		if r.Error != "" {
			fmt.Fprintf(tw, "  error:\t%s\n", r.Error)
		}
		if r.Hint != "" {
			fmt.Fprintf(tw, "  hint:\t%s\n", r.Hint)
		}
		tw.Flush()
		return
	}

	statusLabel := r.Status
	if repaired && r.Status == monitor.StatusCurrent && len(r.AppliedThisRun) > 0 {
		statusLabel = "current (repaired)"
	}
	fmt.Fprintf(tw, "  status:\t%s\n", statusLabel)
	fmt.Fprintf(tw, "  current version:\t%d\n", r.CurrentVersion)
	fmt.Fprintf(tw, "  required version:\t%d\n", r.RequiredVersion)
	tw.Flush()

	if len(r.PendingMigrations) > 0 {
		fmt.Fprintln(w, "  pending:")
		for _, n := range r.PendingMigrations {
			fmt.Fprintf(w, "    - %s\n", n)
		}
	}
	if len(r.AppliedThisRun) > 0 {
		fmt.Fprintln(w, "  applied this run:")
		for _, n := range r.AppliedThisRun {
			fmt.Fprintf(w, "    - %s\n", n)
		}
	}
	if len(r.Tables) > 0 {
		fmt.Fprintln(w, "  tables:")
		ttw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
		for _, t := range r.Tables {
			fmt.Fprintf(ttw, "    %s\t%d rows\n", t.Name, t.RowCount)
		}
		ttw.Flush()
	}
	if r.Status == monitor.StatusForwardIncompatible && r.Hint != "" {
		fmt.Fprintf(w, "  hint: %s\n", r.Hint)
	}
}
