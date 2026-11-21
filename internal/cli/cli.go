// Package cli is the command-line client of penates — the client for AI
// agents and automation, of equal capability with the terminal UI for
// humans. The cobra command tree and the client behavior live here:
// penates with no arguments opens the TUI through the injected runner, the
// commands report and change the state through the core unchanged, and
// failures print one stderr line in the Error [category]: form with the
// exit code the category maps to (errors.go). cmd/penates stays the
// process entry, wiring the build stamp, the streams, and the TUI runner
// through to this package.
//
// The reading commands write nothing outside the penates data directory —
// the startup sync of the built-in kit writes there on every run, and
// nothing else does.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/licenses"
)

// Build is the version stamp linked into the binary.
type Build struct {
	Version string
	Commit  string
}

// Options carries the dependencies the command tree calls into.
type Options struct {
	// TUI opens the terminal interface over the penates data directory;
	// penates with no arguments runs it. Tests pin it to keep the tree
	// from opening an interface.
	TUI func(stdout io.Writer) error
	// Stdin carries the answers of the mutating commands' one
	// confirmation. The process passes os.Stdin; nil means the same.
	Stdin io.Reader
	// IsTerminal reports whether stdin holds a terminal — a mutating
	// command without --yes asks only then. The process passes the
	// isatty test; nil means the same.
	IsTerminal func() bool
}

// Run executes the command tree the way main does and returns the process
// exit code: 0 on success, 2 for a usage error, 1 for every other failure
// (errors.go). Errors print here, one stderr line in the contract format;
// cobra must not write its own message or dump the usage text on top of it
// (SilenceErrors, SilenceUsage). A command that has already reported its
// outcome and pinned its code — a declined confirmation, a run with
// excluded or failed items — exits without anything printed on top.
func Run(b Build, opts Options, args []string, stdout, stderr io.Writer) int {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.IsTerminal == nil {
		opts.IsTerminal = stdinIsTerminal
	}
	root := newRoot(b, opts, stdout, stderr)
	// The command path has to open the command line: permission rules
	// match the command text by its prefix (flagorder.go).
	if err := checkFlagOrder(root, args); err != nil {
		Print(err, stderr)
		return ExitCode(err)
	}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var pinned *exitError
		if errors.As(err, &pinned) {
			return pinned.code
		}
		err = typedUsage(root, args, err)
		Print(err, stderr)
		return ExitCode(err)
	}
	return 0
}

// typedUsage retypes the one refusal cobra reports untyped on its own
// road around SetFlagErrorFunc: a required flag it finds missing after
// the parse. It is a wrong command line like any other — the message
// keeps cobra's wording and gains the command's help as the next step;
// every other error passes unchanged.
func typedUsage(root *cobra.Command, args []string, err error) error {
	if !strings.HasPrefix(err.Error(), "required flag(s) ") {
		return err
	}
	_, name, _ := findPath(root, args)
	return &Error{
		Category: CategoryUsage,
		Message:  fmt.Sprintf("%s (run %q)", err.Error(), name+" --help"),
		Cause:    err,
	}
}

// newRoot builds the command tree: penates with no arguments is the TUI,
// the reading commands of this client stand beside version and licenses,
// and cobra's own help and completion fill the rest of the surface.
func newRoot(b Build, opts Options, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "penates",
		Short: "Install kit items into agent harness configuration directories",
		Long: "penates installs, updates, and removes agent-harness customization — skills,\n" +
			"instruction files, plugins, configuration fragments — from local kit\n" +
			"directories into a harness configuration directory such as ~/.config/opencode.\n\n" +
			"Run penates with no arguments to open the terminal UI: the first run explains\n" +
			"what a kit is and offers to add one. \"penates status\" shows what is installed.\n\n" +
			"Flags come after the command path, so permission rules can match the command\n" +
			"text by its prefix.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.TUI(cmd.OutOrStdout())
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
	// Every flag-parse failure is a wrong command line: a usage error,
	// with pflag's wording and the command's help as the next step.
	root.SetFlagErrorFunc(usageError)

	root.AddCommand(newVersionCmd(b))
	root.AddCommand(newLicensesCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newShowCmd())
	ask := &confirmer{
		stdin:      opts.Stdin,
		stdout:     stdout,
		stderr:     stderr,
		isTerminal: opts.IsTerminal,
	}
	root.AddCommand(newInstallCmd(ask))
	root.AddCommand(newUpdateCmd(ask))
	root.AddCommand(newRemoveCmd(ask))
	root.AddCommand(newResetCmd(ask))
	root.AddCommand(newApplyCmd(ask))
	root.AddCommand(newSetupCmd(ask))
	pendingCmd := newPendingCmd()
	pendingCmd.AddCommand(newPendingClearCmd(ask))
	root.AddCommand(pendingCmd)
	kitCmd := newKitCmd()
	kitCmd.AddCommand(newKitListCmd())
	kitCmd.AddCommand(newKitAddCmd())
	kitCmd.AddCommand(newKitAttachCmd())
	kitCmd.AddCommand(newKitDetachCmd())
	kitCmd.AddCommand(newKitPathCmd())
	root.AddCommand(kitCmd)
	targetCmd := newTargetCmd()
	targetCmd.AddCommand(newTargetListCmd())
	targetCmd.AddCommand(newTargetAddCmd())
	targetCmd.AddCommand(newTargetRenameCmd())
	targetCmd.AddCommand(newTargetForgetCmd())
	root.AddCommand(targetCmd)
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
	// penates help <topic>: an unknown topic is a wrong command line like
	// an unknown command is. Cobra's default prints a note over the usage
	// and exits 0, which the contract cannot tell an agent from success.
	for _, cmd := range root.Commands() {
		if cmd.Name() != "help" {
			continue
		}
		help := cmd
		help.RunE = func(cmd *cobra.Command, args []string) error {
			top, _, e := root.Find(args)
			if e != nil || (top == root && len(args) > 0) {
				return &Error{
					Category: CategoryUsage,
					Message:  fmt.Sprintf("unknown help topic %q (run %q)", args[0], root.CommandPath()+" --help"),
				}
			}
			top.InitDefaultHelpFlag()
			top.InitDefaultVersionFlag()
			return top.Help()
		}
	}
	// The Args validators cobra builds in — the completion shells' among
	// them — report untyped, and a refused argument list is a wrong
	// command line like any other.
	typeArgs(root)
	return root
}

// typeArgs walks the tree and wraps every command's Args validator, so a
// refusal it returns becomes a usage error. Validators that already return
// typed errors pass through their errors unchanged, making the wrap safe
// on validators of this package's own.
func typeArgs(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		typeArgs(sub)
		if sub.Args == nil {
			continue
		}
		next := sub.Args
		sub.Args = func(cmd *cobra.Command, args []string) error {
			if err := next(cmd, args); err != nil {
				var e *Error
				if errors.As(err, &e) {
					return err
				}
				return usageError(cmd, err)
			}
			return nil
		}
	}
}

// usageError turns an error the command tree reports on its own — a flag
// parse failure, a refused argument list — into a usage error: pflag's and
// cobra's wording names what is wrong, the command's help names the next
// step.
func usageError(cmd *cobra.Command, err error) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return &Error{
		Category: CategoryUsage,
		Message:  fmt.Sprintf("%s (run %q)", err, cmd.CommandPath()+" --help"),
		Cause:    err,
	}
}

// rejectArgs is the Args validator of a command that takes no arguments of
// its own: any argument left over after the subcommand lookup is an unknown
// command. The wording names the next step — or the closest command when
// one is close; an empty argument prefix-matches every command, so it gets
// the help wording without a lookup.
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
	return &Error{
		Category: CategoryUsage,
		Message:  fmt.Sprintf("unknown command %q for %q (%s)", args[0], cmd.CommandPath(), next),
	}
}

// exactArgs is the Args validator of a command that takes exactly n
// positional arguments, with the wording of the contract.
func exactArgs(n int, what string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return &Error{
				Category: CategoryUsage,
				Message: fmt.Sprintf("%s takes %d %s, got %d (run %q)",
					cmd.Name(), n, what, len(args), cmd.CommandPath()+" --help"),
			}
		}
		return nil
	}
}

// minimumArgs is the Args validator of a command that takes at least n
// positional arguments, with the wording of the contract.
func minimumArgs(n int, what string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return &Error{
				Category: CategoryUsage,
				Message: fmt.Sprintf("%s takes at least %d %s, got %d (run %q)",
					cmd.Name(), n, what, len(args), cmd.CommandPath()+" --help"),
			}
		}
		return nil
	}
}

// newVersionCmd builds the version command: the build stamp in the
// `penates version (commit)` form the release process stamps into the
// archives.
func newVersionCmd(b Build) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of the penates binary",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "penates %s (%s)\n", b.Version, b.Commit)
			return err
		},
	}
}

// newLicensesCmd builds "penates licenses", a local command like version:
// it reads no state and opens no interface.
func newLicensesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "licenses",
		Short: "Print the licenses of the code included in penates",
		Long: "Print the components included in this binary — penates itself, the Go\n" +
			"standard library, and the third-party modules — followed by the license text\n" +
			"of each. The third-party texts are embedded only in the release builds.",
		Args: rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), licenses.Text())
			return err
		},
	}
}
