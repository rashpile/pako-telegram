// Package version provides build version information.
// Variables are set at build time via ldflags.
package version

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)
