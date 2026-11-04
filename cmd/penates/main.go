// Command penates installs kit items into agent harness configuration directories.
//
// The product design is specified in docs/rfc-penates.md; the modules and their rules
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
	_, err = tea.NewProgram(app.Initial(kitPath), tea.WithAltScreen()).Run()
	return err
}
