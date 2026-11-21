package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/state"
)

// The target management commands. Registration and renaming write the
// registry alone; the forget is the immediate variant of the deferred
// forget: the target's lock is deleted and its files are left alone —
// what the lock recorded is no longer owned. None of these commands
// asks, and none writes the last active target.

// newTargetAddCmd builds "penates target add <path>": register the
// target directory without attaching a kit to it.
func newTargetAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Register a target directory",
		Args:  exactArgs(1, "target directory path"),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, sessErr := openSession()
			if sessErr != nil {
				return sessErr
			}
			if err := s.state.RegisterTarget(args[0]); err != nil {
				return stateOpError(err)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "target "+args[0])
			return err
		},
	}
}

// newTargetRenameCmd builds "penates target rename <path> <name>": set
// the display name the interfaces show beside the path; an empty name
// resets it. The path is the target's identity, so the command renames
// by path.
func newTargetRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <path> <name>",
		Short: "Set the display name of a target, or reset it with an empty name",
		Args:  exactArgs(2, "target path and display name"),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, name := args[0], args[1]
			s, sessErr := openSession()
			if sessErr != nil {
				return sessErr
			}
			if err := s.state.Update(func(reg *state.Registry) error {
				t, ok := reg.Targets[path]
				if !ok {
					return fmt.Errorf("target %s is not registered", path)
				}
				t.Name = name
				reg.Targets[path] = t
				return nil
			}); err != nil {
				return stateOpError(err)
			}
			line := "target " + path
			if name != "" {
				line += "  name " + name
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), line)
			return err
		},
	}
}

// newTargetForgetCmd builds "penates target forget <path>": forget the
// target at once — the lock is deleted, the files in the target are not
// touched.
func newTargetForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <path>",
		Short: "Forget a target: the lock is deleted, the files stay",
		Args:  exactArgs(1, "target path"),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, sessErr := openSession()
			if sessErr != nil {
				return sessErr
			}
			if err := s.state.ForgetTarget(args[0], s.locks); err != nil {
				return stateOpError(err)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "forgotten  "+args[0])
			return err
		},
	}
}
