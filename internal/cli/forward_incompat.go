package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/denisakp/sentinel/internal/adapters/monitor"
)

// printForwardIncompatible renders a constitution III (what / why / how)
// formatted message for a monitor forward-incompatible error. If the wrapped
// error is a *monitor.ForwardIncompatibleError the version numbers are
// surfaced explicitly; otherwise a generic version-less block is emitted.
func printForwardIncompatible(out io.Writer, err error) {
	var fie *monitor.ForwardIncompatibleError
	fmt.Fprintln(out, "Error: monitor schema is ahead of this binary")
	fmt.Fprintln(out)
	if errors.As(err, &fie) {
		fmt.Fprintf(out, "  What: schema is at version %d, this binary requires version %d\n", fie.Found, fie.Required)
	} else {
		fmt.Fprintln(out, "  What: monitor database schema is newer than this binary supports")
	}
	fmt.Fprintln(out, "  Why:  running an older binary against a newer monitor database can drop")
	fmt.Fprintln(out, "        columns or corrupt rows the newer binary depends on.")
	fmt.Fprintln(out, "  How:  upgrade Sentinel to a build that ships the required schema version,")
	fmt.Fprintln(out, "        or restore a prior monitor database snapshot. No rows were written.")
}
