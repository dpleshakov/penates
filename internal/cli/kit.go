package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/kit"
)

// The kit management commands. The CLI registers freely: kit add works on
// an empty registry, and the trust a first run decides lives in recorded
// ownership, not in who ran first — `penates setup` is the agent's own
// way to begin a machine. The immediate variants of the deferred
// operations are the CLI's only variants: detach detaches at once, the
// installed items staying installed and owned, listed for remove. None
// of these commands asks, and none touches which target the terminal UI
// opens.

// newKitAddCmd builds "penates kit add <path>": read the kit at the path,
// register it under its own name, attach it to the target the required
// --target names, registering the target when it is new.
func newKitAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Register the kit at the path and attach it to the target",
		Long: "Register the kit directory at the path under the name its kit.yaml carries\n" +
			"and attach it to the target the --target flag names, registering the target\n" +
			"when it is new.",
		Args: exactArgs(1, "kit directory path"),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, sessErr := openSession()
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
			targetPath, _ := cmd.Flags().GetString("target")
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

// newKitAttachCmd builds "penates kit attach <kit>": attach the
// registered kit to the target the required --target names, registering
// the target when it is new.
func newKitAttachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach <kit>",
		Short: "Attach a registered kit to the target",
		Args:  exactArgs(1, "kit name"),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetPath, _ := cmd.Flags().GetString("target")
			s, sessErr := openSession()
			if sessErr != nil {
				return sessErr
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

// newKitMoveCmd builds "penates kit move <kit> <dir>": point the
// registered kit at the directory that now holds it — the CLI form of
// the terminal UI's "Edit kit path". The directory must hold a kit of
// the same name: the kit's name is its identity, so its attachments and
// pending actions survive the move.
func newKitMoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move <kit> <dir>",
		Short: "Point a registered kit at the directory that now holds it",
		Long: "Point the registered kit at the directory it moved to. The directory must\n" +
			"hold a kit.yaml of the same name — one holding another kit is refused with\n" +
			"both names. The kit's attachments and pending actions survive the move:\n" +
			"the name is the identity, not the path.",
		Args: exactArgs(2, "kit name and directory path"),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, sessErr := openSession()
			if sessErr != nil {
				return sessErr
			}
			if _, ok := s.registry.Kits[args[0]]; !ok {
				return &Error{
					Category: CategoryNotFound,
					Message:  fmt.Sprintf("kit %q is not registered (run %q to see what is)", args[0], "penates kit list"),
				}
			}
			k, _, readErr := kit.Read(args[1])
			if readErr != nil {
				return &Error{Category: CategoryKit, Message: readErr.Error(), Cause: readErr}
			}
			if err := s.state.EditKitPath(args[0], k.Dir, k.Name); err != nil {
				return stateOpError(err)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), k.Dir)
			return err
		},
	}
	return cmd
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
