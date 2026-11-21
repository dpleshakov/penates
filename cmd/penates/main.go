// Command penates installs kit items into agent harness configuration directories.
//
// The command tree and the client behavior live in internal/cli; this
// main passes the build stamp, the streams, and the TUI runner in, and
// exits with the code the run returned. The product behavior is specified
// in docs/concept.md; the modules and their rules in docs/architecture.md.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dpleshakov/penates/internal/builtin"
	"github.com/dpleshakov/penates/internal/cli"
	"github.com/dpleshakov/penates/internal/guard"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/tui"
)

// version and commit are stamped at build time with
// -ldflags "-X main.version=... -X main.commit=...". The defaults are what a
// plain `go build` produces.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(cli.Run(cli.Build{Version: version, Commit: commit}, cli.Options{TUI: runTUI}, os.Args[1:], os.Stdout, os.Stderr))
}

// runTUI opens the terminal interface over the penates data directory.
// The run guard is taken before the state opens and held for the whole
// session: the interface keeps its lock objects in memory until it
// closes, so no other penates process — no CLI command, reading ones
// included — may touch the directory meanwhile. A refused run reports
// busy, like every command of the client.
func runTUI(stdout io.Writer) error {
	dataDir, err := state.DefaultDir()
	if err != nil {
		return &cli.Error{Category: cli.CategoryState, Message: err.Error(), Cause: err}
	}
	run, err := guard.Acquire(dataDir)
	if err != nil {
		return guardError(err)
	}
	defer func() { _ = run.Release() }()
	stateStore, err := state.Open(dataDir)
	if err != nil {
		return &cli.Error{Category: cli.CategoryState, Message: err.Error(), Cause: err}
	}
	// The built-in kit is brought in step with the binary before the
	// interface opens; its report rides into the model for the screens.
	report, err := builtin.Sync(dataDir, stateStore, builtin.Content)
	if err != nil {
		return &cli.Error{Category: cli.CategoryIO, Message: err.Error(), Cause: err}
	}
	app := tui.NewApp(stateStore, lock.NewStore(dataDir))
	app.Startup = &report
	app.BuiltinFS = builtin.Content

	final, err := tea.NewProgram(app.Initial(), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	// The changes applied during the session print to the regular output
	// after the interface closes, so a record remains in the terminal
	// history (10.4); nothing prints when nothing was applied. A failure
	// to write it leaves the interface closed either way.
	if rec := tui.ExitRecord(final); rec != "" {
		_, _ = fmt.Fprint(stdout, rec)
	}
	return nil
}

// guardError maps the run guard's refusal into the client's contract: the
// busy refusal keeps its category, every other failure — the directory or
// its lock file cannot be made — is the state's.
func guardError(err error) error {
	var busy *guard.BusyError
	if errors.As(err, &busy) {
		return &cli.Error{Category: cli.CategoryBusy, Message: busy.Error(), Cause: busy}
	}
	return &cli.Error{Category: cli.CategoryState, Message: err.Error(), Cause: err}
}
