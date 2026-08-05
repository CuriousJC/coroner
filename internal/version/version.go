// Package version holds the build metadata injected at link time.
//
// The defaults are what you get from a plain `go build` with no ldflags. They
// are deliberately obvious rather than plausible, so an unversioned binary
// cannot be mistaken for a released one -- which matters here, because the
// version is written into every digested manifest as part of the determinism
// record.
package version

import (
	"fmt"

	"github.com/curiousjc/coroner/internal/corlog"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

// String is the one-line form recorded in a digest manifest.
func String() string {
	return fmt.Sprintf("%s (%s, built %s)", Version, Commit, BuildTime)
}

// Print writes the build metadata to the console and the log.
func Print() {
	corlog.Heading(true, "coroner %s", Version)
	corlog.Field(true, "Commit", Commit)
	corlog.Field(true, "Built", BuildTime)
}
