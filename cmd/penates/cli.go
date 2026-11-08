package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/tui"
)

// build is the version stamp linked into the binary.
type build struct {
	version string
	commit  string
}

// run executes the command tree and returns the process exit code. It is
// main with the arguments and the streams passed in, so that tests can drive
// the tree and read what it wrote.
func run(b build, args []string, stdout, stderr io.Writer) int {
	root := newRoot(b, stdout, stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		// One stderr line in the Error: form; cobra must not write its own
		// message or dump the usage text on top of it (SilenceErrors,
		// SilenceUsage). Nothing can be done about a failure to write the
		// error message itself: the writer is stderr and the process is
		// already on its way out.
		_, _ = fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}

// newRoot builds the command tree: penates with no arguments is the TUI,
// `version` and `licenses` report the build stamp and the carried licenses,
// and cobra's own help and completion fill the rest of the surface.
func newRoot(b build, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "penates",
		Short: "Install kit items into agent harness configuration directories",
		Long: "penates installs, updates, and removes agent-harness customization — skills,\n" +
			"instruction files, plugins, configuration fragments — from local kit\n" +
			"directories into a harness configuration directory such as ~/.config/opencode.\n\n" +
			"Run penates with no arguments to open the terminal UI: the first run explains\n" +
			"what a kit is and offers to add one.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd.OutOrStdout())
		},
		// The first argument is always a command.
		Args: rejectArgs,
	}
	// The suggestion distance is set because rejectArgs calls
	// SuggestionsFor itself: cobra applies its default only when it looks
	// up a suggestion on its own, which it no longer does on this root.
	root.SuggestionsMinimumDistance = 2
	root.SetOut(stdout)
	root.SetErr(stderr)

	root.AddCommand(newVersionCmd(b))
	root.AddCommand(newLicensesCmd())
	// Cobra adds help and completion only when it executes the tree;
	// adding them here keeps the surface the same however the tree runs.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	// The completion command only holds subcommands — the shells — so an
	// unknown one must not pass for success: cobra would print the group's
	// help and exit 0 for it.
	completion, _, _ := root.Find([]string{"completion"})
	completion.SuggestionsMinimumDistance = 2
	completion.Args = rejectArgs
	completion.RunE = func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	}
	return root
}

// rejectArgs is the Args validator of a command that takes no arguments of
// its own: any argument left over after the subcommand lookup is an unknown
// command. The wording names the next step — or the closest command when
// one is close; an empty argument prefix-matches every command, so it gets
// the help wording without a lookup. The root and the completion group
// share it now, the licenses command will too.
func rejectArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	next := fmt.Sprintf("run %q for the available commands", cmd.CommandPath()+" --help")
	if args[0] != "" {
		if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
			next = fmt.Sprintf("did you mean %q?", suggestions[0])
		}
	}
	return fmt.Errorf("unknown command %q for %q (%s)", args[0], cmd.CommandPath(), next)
}

// newVersionCmd builds the version command: the build stamp in the
// `penates version (commit)` form the release process stamps into the
// archives.
func newVersionCmd(b build) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of the penates binary",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "penates %s (%s)\n", b.version, b.commit)
			return err
		},
	}
}

// runTUI is the TUI runner behind the root command. It is a variable so the
// tests can pin the root's behavior — zero arguments run the interface —
// without opening one.
var runTUI = runInterface

// runInterface opens the terminal interface over the penates data directory.
func runInterface(stdout io.Writer) error {
	dataDir, err := state.DefaultDir()
	if err != nil {
		return err
	}
	stateStore, err := state.Open(dataDir)
	if err != nil {
		return err
	}
	app := tui.NewApp(stateStore, lock.NewStore(dataDir))

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
