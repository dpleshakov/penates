package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/view"
)

// show <kit/item>: the details of one item, the CLI's reading of what the
// TUI's details pane shows. The item resolves in the kits attached to the
// target first, then in the lock alone — an orphan, or an item of an
// attached kit that did not load; nowhere is a not_found. The skeleton is
// the status one: `key  value` lines, sections separated by blank lines,
// reasons indented by four, the changelog verbatim.

// newShowCmd builds "penates show <kit/item>".
func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <kit/item>",
		Short: "Show the details of one item",
		Long: "Show one item: its state with the reason, the pending action, the declared\n" +
			"variables with their saved answers, the contributions, and the changelog.",
		Args: exactArgs(1, "item named \"kit/item\""),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The argument's shape is the command line's business: it is
			// judged before any state is touched.
			if _, _, err := splitItemArg(args[0]); err != nil {
				return err
			}
			explicit, _ := cmd.Flags().GetString("target")
			s, err := openSession()
			if err != nil {
				return err
			}
			if err := s.resolveTarget(explicit); err != nil {
				return err
			}
			if err := s.loadTarget(); err != nil {
				return err
			}
			f, err := resolveItem(s, args[0])
			if err != nil {
				return err
			}
			return printShow(cmd.OutOrStdout(), s, args[0], f)
		},
	}
	targetFlag(cmd)
	return cmd
}

// found is one item show resolved: the kit item when a loaded kit provides
// it, the lock record when it is installed, and the state an item shows
// under when no kit item backs it — an orphan, or an item of an attached
// kit that did not load.
type found struct {
	it     *kit.Item // nil when no loaded kit provides the item
	rec    *lock.Item
	frozen view.State
}

// resolveItem finds the named item in the target's loaded kits, then in
// the lock. An argument that does not split into kit and id never names an
// identity: a usage error.
func resolveItem(s *session, arg string) (*found, *Error) {
	kitName, id, err := splitItemArg(arg)
	if err != nil {
		return nil, err
	}
	for _, k := range s.kits {
		if k.Name != kitName {
			continue
		}
		for _, it := range k.Items {
			if view.RowID(it) != id && it.ID != id {
				continue
			}
			return &found{
				it:  it,
				rec: s.lock.Items[kitName+"/"+view.RowID(it)],
			}, nil
		}
	}
	key := kitName + "/" + id
	rec, installed := s.lock.Items[key]
	if !installed {
		return nil, &Error{
			Category: CategoryNotFound,
			Message:  fmt.Sprintf("no kit attached to %s provides %q and it is not installed (run %q to see what is)", s.target, arg, "penates status"),
		}
	}
	frozen := view.StateOrphan
	if _, attached := s.registry.Kits[kitName]; attached && s.loadedKit(kitName) == nil {
		// Attached but did not load: its items show as unavailable.
		frozen = view.StateUnavailable
	}
	return &found{rec: rec, frozen: frozen}, nil
}

// splitItemArg splits "kit/item" into its parts. An argument that does not
// split is not an item identity, and the command line is at fault.
func splitItemArg(arg string) (kitName, id string, err *Error) {
	kitName, id, ok := strings.Cut(arg, "/")
	if !ok || kitName == "" || id == "" {
		return "", "", &Error{
			Category: CategoryUsage,
			Message:  fmt.Sprintf("%q does not name an item as \"kit/item\" (run %q)", arg, "penates show --help"),
		}
	}
	return kitName, id, nil
}

// printShow lays one item's details out. The identity opens the line; the
// state section follows, then the pending action, the declared facts, and
// the changelog — the mockup's sections contiguous, one line per fact. An
// item the lock alone holds shows the state line and nothing else: its kit
// facts are gone with its kit.
func printShow(w io.Writer, s *session, arg string, f *found) error {
	decided := view.DecideFrozen(arg, f.frozen, s.reconcil)
	if f.it != nil {
		decided = view.Decide(arg, f.it, f.rec, s.reconcil, s.plans[arg])
	}
	var lines []string
	line := "  status  " + decided.State.String()
	if v := versionText(f.rec, f.it, decided); v != "" {
		line += "  " + v
	}
	if decided.Major {
		line += "  major"
	}
	lines = append(lines, arg, line)
	if decided.Reason != "" {
		lines = append(lines, "    "+decided.Reason)
	}
	if verb := showPendingVerb(s, arg, decided); verb != "" {
		lines = append(lines, "  pending  "+verb)
	}

	if f.it != nil {
		if f.it.Description != "" {
			lines = append(lines, "  description  "+f.it.Description)
		}
		if len(f.it.Tags) > 0 {
			lines = append(lines, "  tags  "+strings.Join(f.it.Tags, ", "))
		}
		lines = append(lines, showVariables(f)...)
		lines = append(lines, showContributions(f)...)
		lines = append(lines, showChangelog(f.it)...)
	}
	for _, out := range lines {
		if _, err := fmt.Fprintln(w, out); err != nil {
			return err
		}
	}
	return nil
}

// showPendingVerb is the pending action of the item, as the verb the apply
// would run; empty when none is staged.
func showPendingVerb(s *session, key string, decided view.ItemState) string {
	if s.pending == nil {
		return ""
	}
	item := s.pending.Items[key]
	if item == nil {
		return ""
	}
	switch {
	case item.Remove:
		return "remove"
	case item.Reset:
		return "reset"
	}
	switch decided.State {
	case view.StateAvailable:
		return "install"
	case view.StateUpdate:
		return "update"
	case view.StateDowngrade:
		return "downgrade"
	}
	return "apply"
}

// showVariables lays the declared variables out: the saved answer of the
// lock — the staged answers live on the summary of a mutating command, not
// here — and the required mark. An unanswered variable shows a dash.
func showVariables(f *found) []string {
	if len(f.it.Vars) == 0 {
		return nil
	}
	lines := []string{"  variables"}
	for _, v := range f.it.Vars {
		line := "    " + v.ID
		answer := ""
		if f.rec != nil {
			answer = f.rec.Answers[v.ID]
		}
		if answer != "" {
			line += "  " + answer
		} else {
			line += "  —"
		}
		if v.Required {
			line += "  required"
		}
		lines = append(lines, line)
	}
	return lines
}

// showContributions lays the declared effects out: the kind, the source in
// the kit, the destination in the target.
func showContributions(f *found) []string {
	if len(f.it.Contributes) == 0 {
		return nil
	}
	lines := []string{"  contributions"}
	for _, c := range f.it.Contributes {
		lines = append(lines, "    "+string(c.Kind)+"  "+c.Src+" → "+c.To)
	}
	return lines
}

// showChangelog reads the item's CHANGELOG.md verbatim — the file the
// terminal UI shows on a major update; absent without one.
func showChangelog(it *kit.Item) []string {
	data, err := os.ReadFile(filepath.Join(it.Dir, "CHANGELOG.md")) //nolint:gosec // the kit item built the path
	if err != nil {
		return nil
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil
	}
	return append([]string{"  changelog"}, strings.Split(text, "\n")...)
}
