// Command penates installs kit items into agent harness configuration directories.
//
// The command line is cobra's: the first argument is always a command, and
// penates with no arguments opens the terminal UI — the kit is registered
// from the first-run screen, never from the arguments. The product behavior
// is specified in docs/concept.md; the modules and their rules in
// docs/architecture.md.
package main

import (
	"os"
)

// version and commit are stamped at build time with
// -ldflags "-X main.version=... -X main.commit=...". The defaults are what a
// plain `go build` produces.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run(build{version: version, commit: commit}, os.Args[1:], os.Stdout, os.Stderr))
}
