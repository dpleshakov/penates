package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/builtin"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// The kit management commands. Registration is human for the first kit:
// kit add refuses while the registry holds none, and the refusal names
// the bare command as the first run's way in (concept 10.1). Past it the
// CLI registers freely. The immediate variants of the deferred
// operations are the CLI's only variants: detach detaches at once, the
// installed items staying installed and owned, listed for remove. None
// of these commands asks, and none writes the last active target.

// newKitAddCmd builds "penates kit add <path>": read the kit at the path,
// register it under its own name, attach it to the target — the explicit
// --target, or the last active one; neither is a wrong command line.
func newKitAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Register the kit at the path and attach it to the target",
		Long: "Register the kit directory at the path under the name its kit.yaml carries\n" +
			"and attach it to the target: the one --target names, or the last active one.\n" +
			"The first kit on a machine is registered by a person, through the terminal\n" +
			"UI a bare \"penates\" opens.",
		Args: exactArgs(1, "kit directory path"),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, sessErr := openSessionForAdd()
			if sessErr != nil {
				return sessErr
			}
			k, _, readErr := kit.Read(args[0])
			if readErr != nil {
				return &Error{Category: CategoryKit, Message: readErr.Error(), Cause: readErr}
			}
			if err := s.state.RegisterKit(k.Name, k.Dir); err != nil {
				return stateOpError(err)
			}
			explicit, _ := cmd.Flags().GetString("target")
			targetPath, targetErr := targetOrLast(s, explicit)
			if targetErr != nil {
				return targetErr
			}
			if err := s.state.AttachKit(k.Name, targetPath); err != nil {
				return stateOpError(err)
			}
			out := cmd.OutOrStdout()
			lines := []string{
				"kit " + k.Name + "  " + k.Dir,
				"  attached  " + targetPath,
			}
			return printSections(out, [][]string{lines})
		},
	}
	targetFlag(cmd)
	return cmd
}

// openSessionForAdd opens the state the way kit add needs it: the
// registry is read before the built-in kit's startup sync runs, because
// the guard of the first run checks that no kit is registered at all —
// and the sync itself registers one while the name is free. The guard
// judged before the sync is live; judged after it, it is dead in the
// same process.
func openSessionForAdd() (*session, *Error) {
	dataDir, err := state.DefaultDir()
	if err != nil {
		return nil, &Error{Category: CategoryState, Message: fmt.Sprintf("cannot resolve the penates data directory: %v", err), Cause: err}
	}
	st, err := state.Open(dataDir)
	if err != nil {
		return nil, &Error{Category: CategoryState, Message: err.Error(), Cause: err}
	}
	reg, err := st.LoadRegistry()
	if err != nil {
		return nil, &Error{Category: CategoryState, Message: err.Error(), Cause: err}
	}
	if len(reg.Kits) == 0 {
		return nil, &Error{
			Category: CategoryFirstRun,
			Message:  fmt.Sprintf("no kit is registered yet (run %q — the first kit is registered through the terminal UI)", "penates"),
		}
	}
	report, err := builtin.Sync(dataDir, st, builtin.Content)
	if err != nil {
		return nil, &Error{Category: CategoryIO, Message: fmt.Sprintf("cannot bring the built-in kit in step in %s: %v", dataDir, err), Cause: err}
	}
	reg, err = st.LoadRegistry()
	if err != nil {
		return nil, &Error{Category: CategoryState, Message: err.Error(), Cause: err}
	}
	return &session{
		dataDir:  dataDir,
		state:    st,
		locks:    lock.NewStore(dataDir),
		registry: reg,
		builtin:  &report,
	}, nil
}

// newKitAttachCmd builds "penates kit attach <kit>": attach the
// registered kit to the target, registering the target when it is new.
func newKitAttachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach <kit>",
		Short: "Attach a registered kit to the target",
		Args:  exactArgs(1, "kit name"),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit, _ := cmd.Flags().GetString("target")
			s, sessErr := openSession()
			if sessErr != nil {
				return sessErr
			}
			targetPath, targetErr := targetOrLast(s, explicit)
			if targetErr != nil {
				return targetErr
			}
			if err := s.state.AttachKit(args[0], targetPath); err != nil {
				return stateOpError(err)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "attached  %s  %s\n", args[0], targetPath)
			return err
		},
	}
	targetFlag(cmd)
	return cmd
}

// newKitDetachCmd builds "penates kit detach <kit>": the immediate
// variant of the deferred detach. The attachment goes at once; the
// installed items stay installed and owned, and the report lists them
// for remove.
func newKitDetachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detach <kit>",
		Short: "Detach a kit from the target; the installed items stay",
		Long: "Detach the kit from the target at once. The installed items stay installed\n" +
			"and owned — the files are not touched — and are listed for remove, since\n" +
			"no kit provides them any more. Waiting for the apply is the terminal UI's\n" +
			"mechanics; the command line detaches now.",
		Args: exactArgs(1, "kit name"),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit, _ := cmd.Flags().GetString("target")
			s, sessErr := openSession()
			if sessErr != nil {
				return sessErr
			}
			if err := s.resolveTarget(explicit); err != nil {
				return err
			}
			if err := s.state.DetachKit(args[0], s.target); err != nil {
				return stateOpError(err)
			}
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "detached  %s  %s\n", args[0], s.target); err != nil {
				return err
			}
			lk, loadErr := s.locks.Load(s.target)
			if loadErr != nil {
				return &Error{Category: CategoryIO, Message: loadErr.Error(), Cause: loadErr}
			}
			prefix := args[0] + "/"
			var keys []string
			for key := range lk.Items {
				if strings.HasPrefix(key, prefix) {
					keys = append(keys, key)
				}
			}
			if len(keys) == 0 {
				return nil
			}
			sort.Strings(keys)
			lines := []string{
				"",
				fmt.Sprintf("installed items remain owned (remove them with %q):", "penates remove <kit/item>"),
			}
			for _, key := range keys {
				line := "  " + key
				if rec := lk.Items[key]; rec != nil && rec.Version != "" {
					line += "  " + rec.Version
				}
				lines = append(lines, line)
			}
			return printSections(out, [][]string{lines})
		},
	}
	targetFlag(cmd)
	return cmd
}

// newKitPathCmd builds "penates kit path <kit>": the registered path, on
// its own line — the working round of editing a kit where it lives and
// running update after.
func newKitPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <kit>",
		Short: "Print the registered path of a kit",
		Args:  exactArgs(1, "kit name"),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, sessErr := openSession()
			if sessErr != nil {
				return sessErr
			}
			k, ok := s.registry.Kits[args[0]]
			if !ok {
				return &Error{
					Category: CategoryNotFound,
					Message:  fmt.Sprintf("kit %q is not registered (run %q to see what is)", args[0], "penates kit list"),
				}
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), k.Dir)
			return err
		},
	}
}

// targetOrLast resolves the target a kit management command attaches to:
// the explicit --target, else the last active one. Neither is given —
// the command line is incomplete, and the refusal says what to pass.
func targetOrLast(s *session, explicit string) (string, *Error) {
	if explicit != "" {
		return explicit, nil
	}
	if s.registry.LastTarget != "" {
		return s.registry.LastTarget, nil
	}
	return "", &Error{
		Category: CategoryUsage,
		Message:  "no target is given (pass --target <path>)",
	}
}

// stateOpError maps a registry operation's refusal to the contract
// categories: a thing that does not exist is not_found, a name reserved
// for the built-in kit is the kit content to fix, and a collision with
// what the registry already holds is a conflict.
func stateOpError(err error) *Error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "reserved"):
		return &Error{Category: CategoryKit, Message: msg, Cause: err}
	case strings.Contains(msg, "is not registered"), strings.Contains(msg, "is not attached"):
		return &Error{Category: CategoryNotFound, Message: msg, Cause: err}
	default:
		return &Error{Category: CategoryConflict, Message: msg, Cause: err}
	}
}
