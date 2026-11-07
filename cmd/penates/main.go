// Command penates installs kit items into agent harness configuration directories.
//
// The product behavior is specified in docs/concept.md; the modules and their rules
// in docs/architecture.md.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "penates:", err)
		os.Exit(1)
	}
}

func run() error {
	dataDir, err := state.DefaultDir()
	if err != nil {
		return err
	}
	stateStore, err := state.Open(dataDir)
	if err != nil {
		return err
	}
	app := tui.NewApp(stateStore, lock.NewStore(dataDir))

	kitPath := ""
	if len(os.Args) > 1 {
		kitPath = os.Args[1]
	}
	final, err := tea.NewProgram(app.Initial(kitPath), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	// The changes applied during the session print to the regular output
	// after the interface closes, so a record remains in the terminal
	// history (10.4); nothing prints when nothing was applied.
	if rec := tui.ExitRecord(final); rec != "" {
		fmt.Print(rec)
	}
	return nil
}
