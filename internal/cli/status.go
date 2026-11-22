package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/view"
)

// The reading commands: they print what the state holds and write nothing
// but the startup sync's data-directory work. The output skeleton is
// stable: the target header first, the state word as the first token of
// every item line, the item's identity beside it, one reason per line
// indented by four, sections separated by blank lines, and `key  value`
// lines joined by two spaces.

// noTargetRegistered is the line the reading commands print on an empty
// state, exit 0: the question was answered, and the answer is that there
// is nothing yet.
const noTargetRegistered = `no target is registered (run "penates setup --target <path>", or open "penates" for the first run)`

// newStatusCmd builds "penates status": the target, its kits with their
// paths, the items with their state words and reasons, and the target's
// staged pending actions.
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the kits, the items and their states of one target",
		Long: "Show one target: the kits attached to it, every item with its state —\n" +
			"available, installed, update-available, modified, conflict — and the pending\n" +
			"actions waiting for the apply in the terminal UI.",
		Args: rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			explicit, _ := cmd.Flags().GetString("target")
			s, err := openSession()
			if err != nil {
				return err
			}
			if len(s.registry.Targets) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), noTargetRegistered)
				return err
			}
			if err := s.resolveTarget(explicit); err != nil {
				return err
			}
			if err := s.loadTarget(); err != nil {
				return err
			}
			return printStatus(cmd.OutOrStdout(), s)
		},
	}
	targetFlag(cmd)
	return cmd
}

// newKitCmd builds the kit command group.
func newKitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kit",
		Short: "Register kits and show what is registered",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}

// newKitListCmd builds "penates kit list": the registered kits with their
// paths and the targets each is attached to.
func newKitListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the registered kits",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			return printKitList(cmd.OutOrStdout(), s)
		},
	}
}

// newTargetCmd builds the target command group.
func newTargetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "target",
		Short: "Register targets and show what is registered",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}

// newTargetListCmd builds "penates target list": the registered targets
// with their display names and the counts a line can be read by.
func newTargetListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the registered targets",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			if len(s.registry.Targets) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), noTargetRegistered)
				return err
			}
			return printTargetList(cmd.OutOrStdout(), s)
		},
	}
}

// newPendingListCmd builds "penates pending list": the staged pending
// actions of every registered target — the audit of where something
// waits for an apply. It takes no --target: the command acts on no
// single target, and the flag names one.
func newPendingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the staged pending actions across the registered targets",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			if len(s.registry.Targets) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), noTargetRegistered)
				return err
			}
			return printPendingList(cmd.OutOrStdout(), s)
		},
	}
}

// targetFlag adds the --target flag of a target-scoped command and marks
// it required: the command line names the target it acts on. There is no
// default — the CLI neither reads nor writes which target the terminal UI
// opens.
func targetFlag(cmd *cobra.Command) {
	flagTarget(cmd)
	_ = cmd.MarkFlagRequired("target")
}

// flagTarget adds --target without the required mark. reset --builtin
// shares the command with the target form and runs without one, so its
// RunE enforces the requirement itself (requireTarget).
func flagTarget(cmd *cobra.Command) {
	cmd.Flags().String("target", "", "target directory to act on")
}

// printStatus lays the target's state out: the header, one section per
// attached kit in attachment order, the trailing group of the installed
// items whose kit is not attached, the target's staged pending actions,
// and the notes of what the state load dropped.
func printStatus(w io.Writer, s *session) error {
	var sections [][]string
	sections = append(sections, []string{targetHeader(s)})

	names := s.registry.KitsFor(s.target)
	covered := map[string]bool{}
	for _, name := range names {
		k := s.loadedKit(name)
		var lines []string
		regKit := s.registry.Kits[name]
		if k == nil {
			lines = append(lines, "kit "+name+"  "+regKit.Dir)
			lines = append(lines, "  "+view.StateUnavailable.String()+"  (the kit directory cannot be read)")
			lines = append(lines, statusHeldLines(s, name, covered, false)...)
			sections = append(sections, lines)
			continue
		}
		if name == state.BuiltinKitName {
			// The built-in kit's row names the kit itself, and annotates
			// what the startup sync found.
			row := name + "  " + regKit.Dir + "  built-in"
			if s.builtin != nil && s.builtin.Modified {
				row += ", modified locally"
			}
			if s.builtin != nil && s.builtin.Conflict {
				row += ", update blocked by local modifications"
			}
			lines = append(lines, row)
		} else {
			lines = append(lines, "kit "+name+"  "+regKit.Dir)
		}
		var items []string
		for _, it := range k.Items {
			key := kitItemKey(name, it)
			covered[key] = true
			decided := view.Decide(key, it, s.lock.Items[key], s.reconcil, s.plans[key])
			line := "  " + decided.State.String() + "  " + key
			if v := versionText(s.lock.Items[key], it, decided); v != "" {
				line += "  " + v
			}
			items = append(items, line)
			if decided.Reason != "" {
				items = append(items, "    "+decided.Reason)
			}
		}
		items = append(items, statusHeldLines(s, name, covered, true)...)
		lines = append(lines, items...)
		sections = append(sections, lines)
	}
	if lines := statusOrphanLines(s, covered); len(lines) > 0 {
		sections = append(sections, lines)
	}
	if lines := statusPendingLines(s); len(lines) > 0 {
		sections = append(sections, lines)
	}
	for _, note := range s.registry.Report {
		sections = append(sections, []string{"note  " + note})
	}
	return printSections(w, sections)
}

// targetHeader is the target's opening line: the display name when one is
// set, the path beside it.
func targetHeader(s *session) string {
	if s.name != "" {
		return "target " + s.name + " (" + s.target + ")"
	}
	return "target " + s.target
}

// kitItemKey is the identity a status line keys an item by: the kit name
// and the loader's token for the id.
func kitItemKey(kitName string, it *kit.Item) string {
	return kitName + "/" + view.RowID(it)
}

// orphanNote is the line under an orphan in its kit's section: the kit
// stopped providing the item. The trailing orphans group carries no such
// line — the group header says as much already.
const orphanNote = "removed from the kit"

// statusHeldLines builds the lines of the kit's installed items the kit's
// own item list does not cover: the items of a kit that did not load are
// unavailable, the items of a loaded kit that stopped providing them are
// orphans. Both stay frozen in the kit's section.
func statusHeldLines(s *session, name string, covered map[string]bool, loaded bool) []string {
	if s.lock == nil {
		return nil
	}
	prefix := name + "/"
	var keys []string
	for key := range s.lock.Items {
		if strings.HasPrefix(key, prefix) && !covered[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	base := view.StateOrphan
	if !loaded {
		base = view.StateUnavailable
	}
	var lines []string
	for _, key := range keys {
		covered[key] = true
		decided := view.DecideFrozen(key, base, s.reconcil)
		line := "  " + decided.State.String() + "  " + key
		if rec := s.lock.Items[key]; rec != nil {
			line += "  " + rec.Version
		}
		lines = append(lines, line)
		switch {
		case decided.Reason != "":
			lines = append(lines, "    "+decided.Reason)
		case loaded && decided.State == view.StateOrphan:
			lines = append(lines, "    "+orphanNote)
		}
	}
	return lines
}

// statusOrphanLines builds the trailing section of the installed items
// whose kit is not attached to the target. It shows only when it holds
// items.
func statusOrphanLines(s *session, covered map[string]bool) []string {
	if s.lock == nil {
		return nil
	}
	var keys []string
	for key := range s.lock.Items {
		if !covered[key] {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	lines := []string{"orphans"}
	for _, key := range keys {
		decided := view.DecideFrozen(key, view.StateOrphan, s.reconcil)
		line := "  " + decided.State.String() + "  " + key
		if rec := s.lock.Items[key]; rec != nil {
			line += "  " + rec.Version
		}
		lines = append(lines, line)
		if decided.Reason != "" {
			lines = append(lines, "    "+decided.Reason)
		}
	}
	return lines
}

// statusPendingLines builds the section of the target's staged pending
// actions: one line per item action — the verb the apply would run — and
// one per deferred operation. The header counts the item actions; the
// deferred operations follow as their own lines.
func statusPendingLines(s *session) []string {
	if !hasPending(s) {
		return nil
	}
	lines := []string{fmt.Sprintf("pending %d", len(s.pending.Items))}
	return append(lines, pendingActionLines(s)...)
}

// printRemainingPending writes the note an apply that leaves staged
// actions behind prints after its result: the sentence naming the count
// and the two ways out, the staged actions one line each under it. It
// prints nothing when the settle left nothing behind.
func printRemainingPending(w io.Writer, s *session) error {
	note := s.pending.RemainingNote(s.target)
	if note == "" {
		return nil
	}
	lines := append([]string{"note  " + note}, pendingActionLines(s)...)
	return printSections(w, [][]string{lines})
}

// pendingActionCount counts everything the target's pending actions
// hold: the item actions and the deferred operations together — the
// number a pending clear reports as cleared.
func pendingActionCount(s *session) int {
	return len(s.pending.Items) + len(s.pending.Deferred)
}

// pendingActionLines lays the staged pending actions out, one line per
// item action — the verb the apply would run, the versions it reports —
// and one per deferred operation.
func pendingActionLines(s *session) []string {
	keys := make([]string, 0, len(s.pending.Items))
	for key := range s.pending.Items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var lines []string
	for _, key := range keys {
		item := s.pending.Items[key]
		verb, versions := statusPendingVerb(s, key, item)
		line := "  " + verb + "  " + key
		if versions != "" {
			line += "  " + versions
		}
		lines = append(lines, line)
	}
	return append(lines, pendingDeferredLines(s)...)
}

// pendingDeferredLines lays the target's deferred operations out, the
// lines the status section shows and a deferred-only apply confirms.
func pendingDeferredLines(s *session) []string {
	var lines []string
	for _, d := range s.pending.Deferred {
		switch d.Kind {
		case state.DeferredDetachKit:
			lines = append(lines, "  deferred  detach "+d.Kit+" from "+s.target)
		case state.DeferredForgetTarget:
			lines = append(lines, "  deferred  forget "+s.target)
		}
	}
	return lines
}

// statusPendingVerb names what the apply would do to one pending item, and
// the versions its line reports.
func statusPendingVerb(s *session, key string, item *state.ItemPending) (string, string) {
	if item.Remove {
		return "remove", ""
	}
	if item.Reset {
		return "reset", ""
	}
	rec := s.lock.Items[key]
	kitName, id, _ := strings.Cut(key, "/")
	for _, k := range s.kits {
		if k.Name != kitName {
			continue
		}
		for _, it := range k.Items {
			if view.RowID(it) != id {
				continue
			}
			decided := view.Decide(key, it, rec, s.reconcil, s.plans[key])
			switch decided.State {
			case view.StateAvailable:
				return "install", it.Version
			case view.StateUpdate:
				return "update", versionText(rec, it, decided)
			case view.StateDowngrade:
				return "downgrade", versionText(rec, it, decided)
			}
			return "apply", ""
		}
	}
	return "apply", ""
}

// versionText is the version text an item line reports: the installed and
// the kit version joined with an arrow when they differ, the kit version
// alone for an item not installed, the installed version alone for an item
// its kit stopped providing.
func versionText(rec *lock.Item, it *kit.Item, decided view.ItemState) string {
	switch decided.State {
	case view.StateUpdate, view.StateDowngrade:
		if rec != nil {
			return rec.Version + " → " + it.Version
		}
	case view.StateOrphan, view.StateUnavailable:
		if rec != nil {
			return rec.Version
		}
	case view.StateWarning:
		// A warning from a broken declaration carries no kit version: the
		// installed version is all that is known.
		if len(it.Problems) > 0 && rec != nil {
			return rec.Version
		}
	}
	if it != nil {
		return it.Version
	}
	// An item its kit stopped providing: the installed version is the only
	// one there is.
	if rec != nil {
		return rec.Version
	}
	return ""
}

// printKitList lays the registered kits out: one section per kit with its
// path, the built-in annotation, and the targets it is attached to.
func printKitList(w io.Writer, s *session) error {
	if len(s.registry.Kits) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.registry.Kits))
	for name := range s.registry.Kits {
		names = append(names, name)
	}
	sort.Strings(names)
	attached := map[string][]string{}
	for _, a := range s.registry.Attachments {
		attached[a.Kit] = append(attached[a.Kit], a.Target)
	}
	var sections [][]string
	for _, name := range names {
		k := s.registry.Kits[name]
		line := "kit " + name + "  " + k.Dir
		if name == state.BuiltinKitName {
			line += "  built-in"
		}
		section := []string{line}
		targets := attached[name]
		sort.Strings(targets)
		if len(targets) == 0 {
			section = append(section, "  no attachments")
		}
		for _, t := range targets {
			section = append(section, "  attached  "+t)
		}
		sections = append(sections, section)
	}
	return printSections(w, sections)
}

// printTargetList lays the registered targets out: the path, the display
// name, and the counts a line can be read by. One line per target,
// contiguous — the list is one block.
func printTargetList(w io.Writer, s *session) error {
	for _, path := range s.registry.TargetPaths() {
		t := s.registry.Targets[path]
		line := "target " + path
		if t.Name != "" {
			line += "  name " + t.Name
		}
		kits := 0
		for _, a := range s.registry.Attachments {
			if a.Target == path {
				kits++
			}
		}
		lk, err := s.locks.Load(path)
		if err != nil {
			return &Error{Category: CategoryIO, Message: err.Error(), Cause: err}
		}
		line += fmt.Sprintf("  kits %d  installed %d", kits, len(lk.Items))
		if n := registryPendingCount(s.registry.Pending[path]); n > 0 {
			line += fmt.Sprintf("  pending %d", n)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// registryPendingCount counts the staged pending actions recorded for a
// target: the item actions and the deferred operations together — the
// number a pending clear reports as cleared.
func registryPendingCount(p *state.Pending) int {
	if p == nil {
		return 0
	}
	return len(p.Items) + len(p.Deferred)
}

// printPendingList lays the staged pending actions out across the
// registered targets, in the registry's order, one block per target that
// holds any: the target header, the count, the lines the status section
// shows. A target without staged actions prints nothing — the audit
// reads signals, and a clean target is silence; when no target holds
// any, the answer is one line. Only the reported targets are loaded.
func printPendingList(w io.Writer, s *session) error {
	var sections [][]string
	for _, path := range s.registry.TargetPaths() {
		if registryPendingCount(s.registry.Pending[path]) == 0 {
			continue
		}
		if err := s.focusTarget(path); err != nil {
			return err
		}
		if err := s.loadTarget(); err != nil {
			return err
		}
		sections = append(sections, append([]string{targetHeader(s)}, statusPendingLines(s)...))
	}
	if len(sections) == 0 {
		// The same word pending clear answers with: nothing is waiting
		// anywhere.
		_, err := fmt.Fprintln(w, "nothing staged")
		return err
	}
	return printSections(w, sections)
}

// printSections writes the sections separated by blank lines.
func printSections(w io.Writer, sections [][]string) error {
	for i, section := range sections {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		for _, line := range section {
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}
