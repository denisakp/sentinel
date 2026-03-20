package version

import "runtime"

// Build-time variables injected via -ldflags. Fall back to safe defaults for
// development builds built without linker metadata.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// BuildMetadata holds the Sentinel binary's build-time information.
type BuildMetadata struct {
	Version   string
	Commit    string
	BuildDate string
	GoVersion string
}

// Get returns the resolved build metadata for this binary. Linker-injected
// values override the package-level defaults; GoVersion is always sourced from
// runtime.Version() and is never empty.
func Get() BuildMetadata {
	v := Version
	if v == "" {
		v = "dev"
	}
	c := Commit
	if c == "" {
		c = "unknown"
	}
	bd := BuildDate
	if bd == "" {
		bd = "unknown"
	}
	return BuildMetadata{
		Version:   v,
		Commit:    c,
		BuildDate: bd,
		GoVersion: runtime.Version(),
	}
}
