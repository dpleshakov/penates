package cli

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/apply"
	"github.com/dpleshakov/penates/internal/builtin"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
	"github.com/dpleshakov/penates/internal/view"
)

// The mutating item commands — install, update, remove, reset, apply,
// pending clear — walk one common path: plan, summary on stdout, the one
// confirmation, apply, result. They never mix with the target's staged
// pending actions: while the target holds any, a one-shot command
// refuses and names the three ways out; apply is the way that consumes
// them. Before planning, a mutating command reconciles — the cleanup of
// an interrupted apply — which is a write to the target and the reason
// the reading commands never run it.

// dryRunFlag adds --dry-run: print the summary, write nothing, ask
// nothing.
func dryRunFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "print the summary of what would be done and write nothing")
}

// yesFlag adds --yes: confirm the apply in advance.
func yesFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("yes", false, "confirm the apply in advance, without asking")
}

// setFlag adds --set: a variable value as id=value, repeatable. Only the
// commands whose items carry variables define it.
func setFlag(cmd *cobra.Command) {
	cmd.Flags().StringArray("set", nil, "set a variable value, as --set id=value (repeatable)")
}

// newInstallCmd builds "penates install <kit/item>…": plan and apply in
// one run. A version below the installed one applies as a downgrade —
// install is the CLI's way to downgrade.
func newInstallCmd(ask *confirmer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <kit/item>...",
		Short: "Install items into the target",
		Long: "Plan and apply in one run: the summary of the planned writes first, then\n" +
			"one confirmation — or none with --yes. A version below the installed one\n" +
			"applies as a downgrade. Items are named kit/item.",
		Args: minimumArgs(1, `items named "kit/item"`),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := beginMutate(cmd)
			if err != nil {
				return err
			}
			if err := stagedRefusal(s); err != nil {
				return err
			}
			keys, items, err := resolveNamed(s, cmd.Name(), args)
			if err != nil {
				return err
			}
			fresh, err := setAnswers(cmd, keys, items)
			if err != nil {
				return err
			}
			if err := requireVars(s, keys, fresh); err != nil {
				return err
			}
			installs, err := buildInstalls(s, keys, fresh, nil)
			if err != nil {
				return err
			}
			dry, yes := dryOf(cmd), yesOf(cmd)
			m := &mutation{s: s, out: cmd.OutOrStdout(), errOut: cmd.ErrOrStderr(), conf: ask, dry: dry, yes: yes, installs: installs}
			return m.run()
		},
	}
	targetFlag(cmd)
	dryRunFlag(cmd)
	yesFlag(cmd)
	setFlag(cmd)
	return cmd
}

// newUpdateCmd builds "penates update [items…]": every available update,
// or the named items. It never downgrades — a named downgrade is a wrong
// command line, and the refusal names install as the way.
func newUpdateCmd(ask *confirmer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [<kit/item>...]",
		Short: "Update the items whose kit offers a newer version",
		Long: "Update every item whose kit offers a newer version, or only the named\n" +
			"items. A major update is marked in the summary with its changelog. Never\n" +
			"downgrades: a version below the installed one is refused, and install is\n" +
			"the way to go there.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := beginMutate(cmd)
			if err != nil {
				return err
			}
			if err := stagedRefusal(s); err != nil {
				return err
			}
			var keys []string
			var items []*kit.Item
			if len(args) == 0 {
				keys = availableUpdates(s)
				if len(keys) == 0 {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), "nothing to update")
					return err
				}
				for _, key := range keys {
					items = append(items, kitItemByKey(s, key))
				}
			} else {
				keys, items, err = resolveNamed(s, cmd.Name(), args)
				if err != nil {
					return err
				}
				for i, key := range keys {
					rec := s.lock.Items[key]
					switch {
					case rec == nil:
						return &Error{
							Category: CategoryNotFound,
							Message:  fmt.Sprintf("%s is not installed (install it with %q)", key, "penates install "+key),
						}
					case kit.CompareVersions(items[i].Version, rec.Version) == -1:
						return &Error{
							Category: CategoryUsage,
							Message: fmt.Sprintf("%s %s is below the installed %s; downgrade with %q",
								key, items[i].Version, rec.Version, "penates install "+key),
						}
					}
				}
			}
			fresh, err := setAnswers(cmd, keys, items)
			if err != nil {
				return err
			}
			if err := requireVars(s, keys, fresh); err != nil {
				return err
			}
			installs, err := buildInstalls(s, keys, fresh, nil)
			if err != nil {
				return err
			}
			dry, yes := dryOf(cmd), yesOf(cmd)
			m := &mutation{s: s, out: cmd.OutOrStdout(), errOut: cmd.ErrOrStderr(), conf: ask, dry: dry, yes: yes, installs: installs}
			return m.run()
		},
	}
	targetFlag(cmd)
	dryRunFlag(cmd)
	yesFlag(cmd)
	setFlag(cmd)
	return cmd
}

// newRemoveCmd builds "penates remove <kit/item>…": plan and apply the
// removal in one run, from the lock alone — orphans and the items of
// kits that cannot be read remove like the rest.
func newRemoveCmd(ask *confirmer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <kit/item>...",
		Short: "Remove installed items from the target",
		Long: "Plan and apply the removal in one run. Content the user modified stays in\n" +
			"the target and is reported; unchanged owned content goes away. Orphans and\n" +
			"the items of kits that cannot be read remove like the rest: the removal is\n" +
			"planned from the lock alone.",
		Args: minimumArgs(1, `items named "kit/item"`),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := beginMutate(cmd)
			if err != nil {
				return err
			}
			if err := stagedRefusal(s); err != nil {
				return err
			}
			remove := map[string]bool{}
			for _, arg := range args {
				kitName, id, err := splitItemArg(arg, cmd.Name())
				if err != nil {
					return err
				}
				key := kitName + "/" + id
				if s.lock.Items[key] == nil {
					return &Error{
						Category: CategoryNotFound,
						Message:  fmt.Sprintf("%s is not installed (run %q to see what is)", key, "penates status"),
					}
				}
				remove[key] = true
			}
			removals := plan.BuildRemoval(plan.Input{Lock: s.lock, Snapshot: s.snap, Remove: remove}).Items
			dry, yes := dryOf(cmd), yesOf(cmd)
			m := &mutation{s: s, out: cmd.OutOrStdout(), errOut: cmd.ErrOrStderr(), conf: ask, dry: dry, yes: yes, removals: removals}
			return m.run()
		},
	}
	targetFlag(cmd)
	dryRunFlag(cmd)
	yesFlag(cmd)
	return cmd
}

// newResetCmd builds "penates reset": reset to kit of the named item, or
// the reset of the built-in kit's directory with --builtin. The two
// forms never mix.
func newResetCmd(ask *confirmer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset [<kit/item> | --builtin]",
		Short: "Reset an item to its kit content, or the built-in kit to this binary's content",
		Long: "Reset to kit: the item's owned content is replaced with the kit content even\n" +
			"where the user modified it. Any installed item may be named — the name is\n" +
			"the review. reset --builtin resets the built-in kit's directory to the\n" +
			"content this binary ships.",
		RunE: func(cmd *cobra.Command, args []string) error {
			toBuiltin, _ := cmd.Flags().GetBool("builtin")
			dry, yes := dryOf(cmd), yesOf(cmd)
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			// reset --builtin touches the built-in kit's directory inside
			// the penates data directory and no target: the command runs
			// without one, targetless as it is.
			if toBuiltin {
				if len(args) > 0 {
					return &Error{
						Category: CategoryUsage,
						Message:  fmt.Sprintf("reset takes an item or --builtin, not both (run %q)", cmd.CommandPath()+" --help"),
					}
				}
				s, sessErr := openSession()
				if sessErr != nil {
					return sessErr
				}
				return resetBuiltin(s, out, errOut, ask, dry, yes)
			}
			if len(args) != 1 {
				return &Error{
					Category: CategoryUsage,
					Message:  fmt.Sprintf("reset takes one item named %q, or --builtin (run %q)", "kit/item", cmd.CommandPath()+" --help"),
				}
			}
			if err := requireTarget(cmd); err != nil {
				return err
			}
			s, err := beginMutate(cmd)
			if err != nil {
				return err
			}
			if err := stagedRefusal(s); err != nil {
				return err
			}
			return resetItem(s, out, errOut, ask, dry, yes, args[0])
		},
	}
	flagTarget(cmd)
	dryRunFlag(cmd)
	yesFlag(cmd)
	cmd.Flags().Bool("builtin", false, "reset the built-in kit's directory to the content this binary ships")
	return cmd
}

// resetItem plans and applies one item's reset to kit: the explicit name
// is the review, so any installed item the kit provides may be named.
func resetItem(s *session, out, errOut io.Writer, ask *confirmer, dry, yes bool, arg string) error {
	key, _, err := resolveNamedOne(s, "reset", arg)
	if err != nil {
		return err
	}
	if s.lock.Items[key] == nil {
		return &Error{
			Category: CategoryNotFound,
			Message:  fmt.Sprintf("%s is not installed (run %q to see what is)", key, "penates status"),
		}
	}
	installs, err := buildInstalls(s, []string{key}, nil, map[string]bool{key: true})
	if err != nil {
		return err
	}
	m := &mutation{s: s, out: out, errOut: errOut, conf: ask, dry: dry, yes: yes, installs: installs, resets: map[string]bool{key: true}}
	return m.run()
}

// resetBuiltin resets the built-in kit's directory to the embedded
// content. The startup sync has just compared the directory with the
// embedded content: a directory it reported as modified locally is the
// one a reset has something to take back, and an in-step one has nothing
// to reset — reported as the empty success it is.
func resetBuiltin(s *session, out, errOut io.Writer, ask *confirmer, dry, yes bool) error {
	if s.builtin == nil || !s.builtin.Modified {
		_, err := fmt.Fprintln(out, "nothing to reset")
		return err
	}
	line := view.VerbReset + "  " + state.BuiltinKitName + "  " + builtin.KitDir(s.dataDir)
	// A dry-run never asks; it prints the one line the run would print.
	if !dry {
		if err := ask.ready(yes); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out, line); err != nil {
		return err
	}
	if dry {
		return nil
	}
	if !yes && !ask.ask() {
		return ask.declined()
	}
	if err := builtin.Reset(s.dataDir, builtin.Content); err != nil {
		return &Error{Category: CategoryIO, Message: err.Error(), Cause: err}
	}
	return nil
}

// newApplyCmd builds "penates apply": apply the staged pending actions —
// the bridge to the terminal UI's staging. Nothing is asked beyond the
// one confirmation: the answers the selections were given are applied as
// they stand. After the run the staged actions settle and the target's
// deferred operations complete, with the report.
func newApplyCmd(ask *confirmer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the pending actions staged for the target",
		Long: "Apply what the terminal UI staged for the target: the selections with the\n" +
			"answers they were given, the resets, the removals. Nothing is asked beyond\n" +
			"one confirmation. After the run the deferred operations — a detach or a\n" +
			"forget that waited for this apply — complete, and the report tells which.",
		Args: rejectArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := beginMutate(cmd)
			if err != nil {
				return err
			}
			staged := collectStaged(s)
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			dry, yes := dryOf(cmd), yesOf(cmd)
			if len(staged.selection) == 0 && len(staged.removals) == 0 {
				// Item actions nothing can apply: the ones the check
				// dropped leave the registry here, with their notes —
				// one-shot commands keep refusing a pending nothing can
				// ever consume, and this command is the way out its own
				// refusal names. A pending that holds deferred
				// operations is what this very command exists to finish.
				if !dry {
					if err := dropStaged(s, staged.droppedKeys); err != nil {
						return err
					}
				}
				if staged.droppedEverything(s.pending) {
					if _, err := fmt.Fprintln(out, "nothing staged"); err != nil {
						return err
					}
					return printNotes(out, staged.dropped)
				}
				return completeDeferredOnly(s, out, ask, dry, yes, staged)
			}
			var removals []*plan.ItemPlan
			if len(staged.removals) > 0 {
				remove := map[string]bool{}
				for _, key := range staged.removals {
					remove[key] = true
				}
				removals = plan.BuildRemoval(plan.Input{Lock: s.lock, Snapshot: s.snap, Remove: remove}).Items
			}
			var installs []*plan.ItemPlan
			if len(staged.selection) > 0 {
				sources, err := kit.ReadSources(s.kits, staged.selection)
				if err != nil {
					return &Error{Category: CategoryKit, Message: err.Error(), Cause: err}
				}
				installs = plan.Build(plan.Input{
					Kits:       s.kits,
					Selection:  staged.selection,
					Lock:       s.lock,
					Snapshot:   s.snap,
					Sources:    sources,
					Answers:    staged.answers,
					Reset:      staged.resets,
					Reconciled: s.reconcil,
				}).Items
			}
			m := &mutation{
				s: s, out: out, errOut: errOut, conf: ask,
				dry: dry, yes: yes,
				installs: installs, removals: removals, resets: staged.resets,
				notes: staged.dropped,
				replan: func() ([]*plan.ItemPlan, *Error) {
					return replanInstalls(s, staged.selection, staged.answers, staged.resets)
				},
				after: func(satisfied, removed map[string]bool) ([]string, *Error) {
					return settleAndComplete(s, satisfied, removed)
				},
			}
			return m.run()
		},
	}
	targetFlag(cmd)
	dryRunFlag(cmd)
	yesFlag(cmd)
	return cmd
}

// newPendingCmd builds the pending command group.
func newPendingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pending",
		Short: "Show and clear the staged pending actions",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}

// newPendingClearCmd builds "penates pending clear": drop the staged
// pending actions without applying them. It asks once, like every
// mutating command — dropping staged work is a mutation of the state,
// and the summary of what is dropped stands before the question.
func newPendingClearCmd(ask *confirmer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Drop the staged pending actions without applying them",
		Args:  rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			explicit, _ := cmd.Flags().GetString("target")
			dry, yes := dryOf(cmd), yesOf(cmd)
			out := cmd.OutOrStdout()
			s, err := openSession()
			if err != nil {
				return err
			}
			if err := s.resolveTarget(explicit); err != nil {
				return err
			}
			if err := s.loadTargetForWrite(); err != nil {
				return err
			}
			if !hasPending(s) {
				_, err := fmt.Fprintln(out, "nothing staged")
				return err
			}
			// A dry-run never asks; it prints what would go.
			if !dry {
				if err := ask.ready(yes); err != nil {
					return err
				}
			}
			n := pendingActionCount(s)
			if err := printSections(out, [][]string{
				{"target " + s.target},
				statusPendingLines(s),
			}); err != nil {
				return err
			}
			if dry {
				return nil
			}
			if !yes && !ask.ask() {
				return ask.declined()
			}
			if err := s.state.Update(func(reg *state.Registry) error {
				delete(reg.Pending, s.target)
				return nil
			}); err != nil {
				return &Error{Category: CategoryState, Message: err.Error(), Cause: err}
			}
			lines := []string{fmt.Sprintf("cleared  %d pending action%s for %s", n, plural(n), s.target)}
			lines = append(lines, pendingActionLines(s)...)
			_, werr := fmt.Fprintln(out)
			if werr != nil {
				return werr
			}
			return printSections(out, [][]string{lines})
		},
	}
	targetFlag(cmd)
	dryRunFlag(cmd)
	yesFlag(cmd)
	return cmd
}

// beginMutate runs the prologue every target-scoped mutating command
// shares: open the session, resolve the target, load for writing — the
// reconciliation that cleans an interrupted apply included.
func beginMutate(cmd *cobra.Command) (*session, *Error) {
	explicit, _ := cmd.Flags().GetString("target")
	s, err := openSession()
	if err != nil {
		return nil, err
	}
	if err := s.resolveTarget(explicit); err != nil {
		return nil, err
	}
	if err := s.loadTargetForWrite(); err != nil {
		return nil, err
	}
	return s, nil
}

// requireTarget is the required-flag check of the one command cobra
// cannot hold to it: reset --builtin shares the --target definition with
// the target form and runs without one. The wording is pflag's for a
// missing required flag, the same the marked commands are refused with.
func requireTarget(cmd *cobra.Command) *Error {
	v, _ := cmd.Flags().GetString("target")
	if v != "" {
		return nil
	}
	return &Error{
		Category: CategoryUsage,
		Message:  fmt.Sprintf(`required flag(s) "target" not set (run %q)`, cmd.CommandPath()+" --help"),
	}
}

// dryOf and yesOf read the flags every mutating command defines.
func dryOf(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("dry-run")
	return v
}

func yesOf(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("yes")
	return v
}

// stagedRefusal refuses a one-shot command while the target holds staged
// pending actions: one-shot commands never consume or mix them. The
// Error line names the staged actions and the three ways out; the
// details carry the same list one action per line.
func stagedRefusal(s *session) *Error {
	if !hasPending(s) {
		return nil
	}
	keys := view.SortedKeys(s.pending.Items)
	var parts, details []string
	for _, key := range keys {
		item := s.pending.Items[key]
		verb, versions := statusPendingVerb(s, key, item)
		parts = append(parts, verb+" "+key)
		line := "  " + verb + "  " + key
		if versions != "" {
			line += "  " + versions
		}
		details = append(details, line)
	}
	for _, d := range s.pending.Deferred {
		switch d.Kind {
		case state.DeferredDetachKit:
			parts = append(parts, "the deferred detach of "+d.Kit)
			details = append(details, "  deferred  detach "+d.Kit+" from "+s.target)
		case state.DeferredForgetTarget:
			parts = append(parts, "the deferred forget of the target")
			details = append(details, "  deferred  forget "+s.target)
		}
	}
	n := len(parts)
	what := "actions are"
	if n == 1 {
		what = "action is"
	}
	return &Error{
		Category: CategoryStaged,
		Message: fmt.Sprintf("%d pending %s staged for %s: %s (apply them with %q, clear them with %q, or ask the person who staged them)",
			n, what, s.target, strings.Join(parts, ", "), "penates apply", "penates pending clear"),
		Details: details,
	}
}

// resolveNamed resolves the named arguments against the kits attached to
// the target: each must name an item a loaded kit provides, by its id or
// row identity. An item whose declaration is broken is refused with its
// first problem — the kit is to fix, not the command line.
func resolveNamed(s *session, cmdName string, args []string) ([]string, []*kit.Item, *Error) {
	var keys []string
	var items []*kit.Item
	for _, arg := range args {
		key, it, err := resolveNamedOne(s, cmdName, arg)
		if err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		items = append(items, it)
	}
	return keys, items, nil
}

// resolveNamedOne resolves one named item.
func resolveNamedOne(s *session, cmdName, arg string) (string, *kit.Item, *Error) {
	kitName, id, err := splitItemArg(arg, cmdName)
	if err != nil {
		return "", nil, err
	}
	for _, k := range s.kits {
		if k.Name != kitName {
			continue
		}
		for _, it := range k.Items {
			if view.RowID(it) != id && it.ID != id {
				continue
			}
			if len(it.Problems) > 0 {
				return "", nil, &Error{
					Category: CategoryKit,
					Message:  fmt.Sprintf("item %s has a broken declaration: %v", kitName+"/"+view.RowID(it), it.Problems[0]),
				}
			}
			return kitName + "/" + view.RowID(it), it, nil
		}
	}
	return "", nil, &Error{
		Category: CategoryNotFound,
		Message: fmt.Sprintf("no kit attached to %s provides %q (run %q to see what is attached)",
			s.target, arg, "penates status"),
	}
}

// availableUpdates lists the keys of the installed items whose kit offers
// a higher version: the states an update can be chosen in. Broken and
// modified items hold their own states and are not updates; the
// planning of the chosen ones still decides every verdict.
func availableUpdates(s *session) []string {
	var keys []string
	for _, k := range s.kits {
		for _, it := range k.Items {
			if len(it.Problems) > 0 {
				continue
			}
			key := k.Name + "/" + view.RowID(it)
			rec := s.lock.Items[key]
			if rec == nil {
				continue
			}
			if kit.CompareVersions(it.Version, rec.Version) == 1 {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// kitItemByKey returns the loaded kit item of that key, or nil.
func kitItemByKey(s *session, key string) *kit.Item {
	kitName, id, _ := strings.Cut(key, "/")
	for _, k := range s.kits {
		if k.Name != kitName {
			continue
		}
		for _, it := range k.Items {
			if view.RowID(it) == id || it.ID == id {
				return it
			}
		}
	}
	return nil
}

// setAnswers reads the --set values and builds the fresh answers of the
// selected items. The form is flat: both items declaring one id take the
// one value. An id no selected item declares is a wrong command line.
func setAnswers(cmd *cobra.Command, keys []string, items []*kit.Item) (map[string]map[string]string, *Error) {
	raw, err := cmd.Flags().GetStringArray("set")
	if err != nil {
		return nil, &Error{Category: CategoryUsage, Message: err.Error(), Cause: err}
	}
	flat := map[string]string{}
	for _, entry := range raw {
		id, value, ok := strings.Cut(entry, "=")
		if !ok || id == "" {
			return nil, &Error{
				Category: CategoryUsage,
				Message:  fmt.Sprintf("malformed --set %q (pass a variable as --set id=value)", entry),
			}
		}
		flat[id] = value
	}
	if len(flat) == 0 {
		return nil, nil
	}
	declared := map[string]bool{}
	for _, it := range items {
		for _, v := range it.Vars {
			declared[v.ID] = true
		}
	}
	for id := range flat {
		if !declared[id] {
			return nil, &Error{
				Category: CategoryUsage,
				Message: fmt.Sprintf("unknown variable %q: none of the selected items declares it (run %q to see the declared variables)",
					id, "penates show "+keys[0]),
			}
		}
	}
	fresh := map[string]map[string]string{}
	for _, key := range keys {
		answers := map[string]string{}
		for id, value := range flat {
			answers[id] = value
		}
		fresh[key] = answers
	}
	return fresh, nil
}

// requireVars refuses the run while a required variable of the selected
// items holds no value — the recorded answer and --set both count. The
// message names every missing id, the way the contract asks.
func requireVars(s *session, keys []string, fresh map[string]map[string]string) *Error {
	missing := plan.MissingVars(s.kits, keys, s.lock, fresh)
	if len(missing) == 0 {
		return nil
	}
	keys = view.SortedKeys(missing)
	var parts, ids []string
	for _, key := range keys {
		var names []string
		for _, v := range missing[key] {
			names = append(names, v.ID)
			ids = append(ids, v.ID)
		}
		parts = append(parts, fmt.Sprintf("item %s requires a value for %s", key, strings.Join(names, ", ")))
	}
	return &Error{
		Category: CategoryUsage,
		Message:  fmt.Sprintf("%s (rerun with --set %s=<value>)", strings.Join(parts, "; "), ids[0]),
	}
}

// buildInstalls plans the installation of the selected items: the
// sources read once, the planner pure over the values.
func buildInstalls(s *session, keys []string, fresh map[string]map[string]string, resets map[string]bool) ([]*plan.ItemPlan, *Error) {
	sources, err := kit.ReadSources(s.kits, keys)
	if err != nil {
		return nil, &Error{Category: CategoryKit, Message: err.Error(), Cause: err}
	}
	return plan.Build(plan.Input{
		Kits:       s.kits,
		Selection:  keys,
		Lock:       s.lock,
		Snapshot:   s.snap,
		Sources:    sources,
		Answers:    fresh,
		Reset:      resets,
		Reconciled: s.reconcil,
	}).Items, nil
}

// staged holds what the target's pending actions would apply, after the
// restore-time check: an action whose item has moved to another version
// since it was staged, or whose kit item is gone, drops with a note —
// the way the terminal UI drops them when it restores a selection.
type staged struct {
	selection []string
	removals  []string
	resets    map[string]bool
	answers   map[string]map[string]string
	dropped   []string
	// droppedKeys holds the item keys the dropped notes belong to: the
	// apply removes them from the registry when nothing else can.
	droppedKeys []string
}

// drop records one staged action the check dropped.
func (st *staged) drop(key, note string) {
	st.dropped = append(st.dropped, note)
	st.droppedKeys = append(st.droppedKeys, key)
}

// droppedEverything reports whether the target's pending actions hold
// nothing the check left no note for: once the dropped actions leave the
// registry, nothing staged remains.
func (st *staged) droppedEverything(p *state.Pending) bool {
	if p == nil {
		return true
	}
	if len(p.Deferred) > 0 {
		return false
	}
	dropped := make(map[string]bool, len(st.droppedKeys))
	for _, key := range st.droppedKeys {
		dropped[key] = true
	}
	for key := range p.Items {
		if !dropped[key] {
			return false
		}
	}
	return true
}

// collectStaged gathers the target's staged pending actions and checks
// each against the kit and the lock as they stand now. A removal wins
// over a held selection, as at planning; a reset joins the selection it
// rides on — or carries its own item into it.
func collectStaged(s *session) *staged {
	st := &staged{resets: map[string]bool{}, answers: map[string]map[string]string{}}
	if s.pending == nil {
		return st
	}
	for key, item := range s.pending.Items {
		kitIt := kitItemByKey(s, key)
		stale := kitIt != nil && item.Version != "" && item.Version != kitIt.Version
		installed := s.lock.Items[key] != nil
		switch {
		case item.Remove && installed:
			st.removals = append(st.removals, key)
		case item.Remove:
			st.drop(key, "dropped the staged removal of "+key+": the item is not installed")
		case item.Select && kitIt == nil:
			st.drop(key, "dropped the staged selection of "+key+": no kit provides the item")
		case item.Select && stale:
			st.drop(key, fmt.Sprintf("dropped the staged selection of %s: the item moved to %s since it was staged", key, kitIt.Version))
		case item.Select:
			st.selection = append(st.selection, key)
			if len(item.Answers) > 0 {
				st.answers[key] = item.Answers
			}
		}
		if item.Reset {
			switch {
			case kitIt == nil || !installed:
				st.drop(key, "dropped the staged reset of "+key+": the item is not installed or not provided any more")
			case stale:
				st.drop(key, fmt.Sprintf("dropped the staged reset of %s: the item moved to %s since it was staged", key, kitIt.Version))
			default:
				st.resets[key] = true
				if !slices.Contains(st.selection, key) && !slices.Contains(st.removals, key) {
					st.selection = append(st.selection, key)
				}
			}
		}
	}
	sort.Strings(st.selection)
	sort.Strings(st.removals)
	sort.Strings(st.dropped)
	return st
}

// replanInstalls plans the installs again from the target as the removals
// of the same run leave it: a removed item's content must not rise again
// in a shared file the installs compose. The lock comes back reloaded.
func replanInstalls(s *session, selection []string, answers map[string]map[string]string, resets map[string]bool) ([]*plan.ItemPlan, *Error) {
	lk, err := s.locks.Load(s.target)
	if err != nil {
		return nil, &Error{Category: CategoryIO, Message: err.Error(), Cause: err}
	}
	s.lock = lk
	files, shared := s.snapshotPaths()
	snap, err := target.Read(s.target, files, shared)
	if err != nil {
		return nil, &Error{Category: CategoryIO, Message: err.Error(), Cause: err}
	}
	s.snap = snap
	sources, err := kit.ReadSources(s.kits, selection)
	if err != nil {
		return nil, &Error{Category: CategoryKit, Message: err.Error(), Cause: err}
	}
	return plan.Build(plan.Input{
		Kits:       s.kits,
		Selection:  selection,
		Lock:       lk,
		Snapshot:   snap,
		Sources:    sources,
		Answers:    answers,
		Reset:      resets,
		Reconciled: s.reconcil,
	}).Items, nil
}

// dropStaged removes the item actions the restore-time check dropped
// from the target's pending: nothing can apply them ever again, and a
// registry that keeps them refuses every one-shot command while the
// apply that alone could consume them does nothing. The pending object
// goes with them when they were all it held.
func dropStaged(s *session, keys []string) *Error {
	if len(keys) == 0 {
		return nil
	}
	if err := s.state.Update(func(reg *state.Registry) error {
		p, ok := reg.Pending[s.target]
		if !ok {
			return nil
		}
		for _, key := range keys {
			delete(p.Items, key)
		}
		if len(p.Items) == 0 && len(p.Deferred) == 0 {
			delete(reg.Pending, s.target)
		}
		return nil
	}); err != nil {
		return &Error{Category: CategoryState, Message: "could not save the state: " + err.Error(), Cause: err}
	}
	return nil
}

// completeDeferredOnly runs the apply of a pending that holds deferred
// operations alone: nothing to plan, the one confirmation like every
// mutating command, then the deferred operations complete or drop with
// the reason — the report is the whole output. The item actions the
// restore-time check dropped leave the registry here too:
// CompleteDeferred consumes the deferred operations alone. Every
// operation completed exits 0; one dropped exits 1 without an Error
// line: the dropped reason is the honest signal of what still stands in
// the way, and clearing or removing it is the next step.
func completeDeferredOnly(s *session, out io.Writer, ask *confirmer, dry, yes bool, st *staged) error {
	// A dry-run never asks; it prints what would go.
	if !dry {
		if err := ask.ready(yes); err != nil {
			return err
		}
	}
	if err := printSections(out, [][]string{
		{"target " + s.target},
		pendingDeferredLines(s),
	}); err != nil {
		return err
	}
	if dry {
		return nil
	}
	if !yes && !ask.ask() {
		return ask.declined()
	}
	if err := dropStaged(s, st.droppedKeys); err != nil {
		return err
	}
	installed := make([]string, 0, len(s.lock.Items))
	for key := range s.lock.Items {
		installed = append(installed, key)
	}
	sort.Strings(installed)
	report, err := s.state.CompleteDeferred(s.target, installed, s.locks)
	if err != nil {
		return &Error{Category: CategoryState, Message: "could not complete the deferred operations: " + err.Error(), Cause: err}
	}
	dropped := append([]string{}, st.dropped...)
	for _, p := range s.stagedRemoved {
		dropped = append(dropped, "removed the staged file "+p+" left by an interrupted apply")
	}
	if len(report) > 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		if err := printSections(out, [][]string{report}); err != nil {
			return err
		}
	}
	if err := printNotes(out, dropped); err != nil {
		return err
	}
	for _, line := range report {
		if strings.HasPrefix(line, "dropped ") {
			return exitFailed()
		}
	}
	return nil
}

// settleAndComplete folds a finished apply into the pending actions and
// completes the target's deferred operations: the settle is the state
// operation both clients share, and the deferred detach or forget
// completes when the apply removed what it waited for — or is dropped
// with the reason in the report.
func settleAndComplete(s *session, satisfied, removed map[string]bool) ([]string, *Error) {
	if err := s.state.SettlePending(s.target, satisfied, removed); err != nil {
		return nil, &Error{Category: CategoryState, Message: "could not save the state: " + err.Error(), Cause: err}
	}
	lk, err := s.locks.Load(s.target)
	if err != nil {
		return nil, &Error{Category: CategoryState, Message: "could not complete the deferred operations: " + err.Error(), Cause: err}
	}
	installed := make([]string, 0, len(lk.Items))
	for key := range lk.Items {
		installed = append(installed, key)
	}
	sort.Strings(installed)
	report, err := s.state.CompleteDeferred(s.target, installed, s.locks)
	if err != nil {
		return nil, &Error{Category: CategoryState, Message: "could not complete the deferred operations: " + err.Error(), Cause: err}
	}
	return report, nil
}

// mutation carries one planned mutation through the common path: the
// summary, the one confirmation, the run, the result. A run with both
// removals and installs executes the removals first and plans the
// installs again from the target as the removals leave it; after — when
// the command consumes staged actions — settles them and completes the
// deferred operations.
type mutation struct {
	s      *session
	out    io.Writer
	errOut io.Writer
	conf   *confirmer
	dry    bool
	yes    bool

	installs []*plan.ItemPlan
	removals []*plan.ItemPlan
	resets   map[string]bool

	// replan builds the install plans afresh after the removals, nil
	// when the run installs nothing or plans nothing after removals.
	replan func() ([]*plan.ItemPlan, *Error)

	// after runs once the apply has landed: staged actions settle,
	// deferred operations complete, the report comes back.
	after func(satisfied, removed map[string]bool) ([]string, *Error)

	// notes are the plain lines the run reports behind the result: the
	// staged actions the restore-time check dropped, the staged temp
	// files the reconciliation cleaned.
	notes []string

	// installsRun holds the plans the run executes: the re-planned ones
	// when the removals ran first.
	installsRun []*plan.ItemPlan
}

// run walks the path. The exit code says whether the command fully did
// what it was asked: a declined confirmation and a run with excluded or
// failed items leave 1, and everything they have to say is already on
// stdout and stderr.
func (m *mutation) run() error {
	summ := buildSummary(m.s, m.installs, m.removals, m.resets)
	canApply := len(summ.items) > 0
	// Nothing applicable — every planned item was excluded: there is
	// nothing to confirm and nothing to run, and the summary with the
	// exclusions is the whole report, exit 1. A dry-run never asks
	// either: the summary is the whole preview.
	if canApply && !m.dry {
		if err := m.conf.ready(m.yes); err != nil {
			return err
		}
	}
	if err := printSummary(m.out, summ); err != nil {
		return err
	}
	for _, block := range changelogBlocks(m.s, m.installs) {
		if _, err := fmt.Fprintln(m.out); err != nil {
			return err
		}
		if err := printSections(m.out, [][]string{block}); err != nil {
			return err
		}
	}
	if m.dry || !canApply {
		for _, e := range summ.excl {
			Print(exclusionError(resultRow{key: e.key, reasons: e.reasons, category: e.category}), m.errOut)
		}
		if !summ.clean() {
			return exitFailed()
		}
		return nil
	}
	if !m.yes {
		if !m.conf.ask() {
			return m.conf.declined()
		}
	}
	oc := m.execute()
	for _, p := range m.s.stagedRemoved {
		m.notes = append(m.notes, "removed the staged file "+p+" left by an interrupted apply")
	}
	// The result stands apart from the summary by a blank line, the way
	// sections do.
	if _, err := fmt.Fprintln(m.out); err != nil {
		return err
	}
	if err := printResult(m.out, m.errOut, oc); err != nil {
		return err
	}
	if err := printNotes(m.out, m.notes); err != nil {
		return err
	}
	if oc.stateErr != nil {
		return oc.stateErr
	}
	if oc.failed > 0 {
		return exitFailed()
	}
	return nil
}

// execute performs the confirmed run: the removals first, then the
// installs — planned again from the target as the removals of the same
// run leave it — and the staged settle afterwards when the command
// consumes them. The rows build between the two, after the re-planning
// and before the install steps run: they read the plans that will
// execute and a lock no step of theirs has rewritten yet.
func (m *mutation) execute() *runOutcome {
	oc := &runOutcome{}
	m.installsRun = m.installs
	var results []apply.Result
	if len(m.removals) > 0 {
		results = append(results, apply.Remove(m.s.target, &plan.Plan{Items: m.removals}, m.s.lock, m.s.locks, nil)...)
	}
	if len(m.removals) > 0 && len(m.installs) > 0 && m.replan != nil {
		re, err := m.replan()
		if err != nil {
			oc.stateErr = err
			return oc
		}
		m.installsRun = re
	}
	rows, applied, kept, failed := m.buildRows()
	if len(m.installsRun) > 0 {
		results = append(results, apply.Apply(m.s.target, &plan.Plan{Items: m.installsRun}, m.s.lock, m.s.locks, nil)...)
	}
	errs := map[string]error{}
	for _, r := range results {
		if r.Err != nil {
			errs[r.Key] = r.Err
		}
	}
	removed := map[string]bool{}
	for _, r := range results {
		if r.Err == nil && m.wasRemoval(r.Key) {
			removed[r.Key] = true
		}
	}
	for i := range rows {
		if err, failedRun := errs[rows[i].key]; failedRun && rows[i].verb != "kept" && rows[i].verb != "excluded" {
			rows[i].verb, rows[i].note = "failed", err.Error()
			rows[i].version = ""
			// A step that failed reached the file system and lost: the
			// one category its Error line can carry is io.
			rows[i].category = CategoryIO
			applied--
			failed++
		}
	}
	oc.rows, oc.applied, oc.kept, oc.failed = rows, applied, kept, failed
	if m.after != nil {
		report, err := m.after(satisfiedKeys(rows, errs), removed)
		if err != nil {
			oc.stateErr = err
		}
		oc.deferred = report
	}
	return oc
}

// wasRemoval reports whether the run planned a removal of that key.
func (m *mutation) wasRemoval(key string) bool {
	for _, ip := range m.removals {
		if ip.Key == key && ip.Status == plan.StatusRemove {
			return true
		}
	}
	return false
}

// satisfiedKeys names the keys whose outcome satisfies their intent, the
// input the settle consumes: the rows the run applied or kept, with the
// error map excepted — a key whose step failed and one the planning
// excluded hold no outcome and keep their staged actions.
func satisfiedKeys(rows []resultRow, errs map[string]error) map[string]bool {
	satisfied := map[string]bool{}
	for _, r := range rows {
		if r.verb == "excluded" || errs[r.key] != nil {
			continue
		}
		satisfied[r.key] = true
	}
	return satisfied
}

// buildRows lays the planned items out into result rows, before the
// steps run: a removal row with the content it leaves behind, an install
// row with the version it reaches, a kept row with its verdict reason,
// an excluded row with its reasons and the category its stderr line
// carries.
func (m *mutation) buildRows() (rows []resultRow, applied, kept, failed int) {
	for _, ip := range m.removals {
		switch ip.Status {
		case plan.StatusRemove:
			row := resultRow{verb: view.PastVerb(view.VerbRemove), key: ip.Key}
			seen := map[string]bool{}
			for _, k := range ip.Kept {
				id := k.Path + "\x00" + k.Reason
				if seen[id] {
					continue
				}
				seen[id] = true
				row.keptNotes = append(row.keptNotes, k.Path+" kept: "+k.Reason)
			}
			rows = append(rows, row)
			applied++
		case plan.StatusError:
			rows = append(rows, m.excludedRow(ip))
			failed++
		}
	}
	for _, ip := range m.installsRun {
		switch ip.Status {
		case plan.StatusInstall:
			verb := view.PlanVerb(ip, m.s.lock, m.resets)
			rows = append(rows, resultRow{
				verb:    view.PastVerb(verb),
				key:     ip.Key,
				version: resultVersion(ip, m.s.lock, verb),
			})
			applied++
		case plan.StatusNoAction:
			rec := m.s.lock.Items[ip.Key]
			version := ip.Version
			if rec != nil {
				version = rec.Version
			}
			rows = append(rows, resultRow{verb: "kept", key: ip.Key, version: version, note: ip.Reason})
			kept++
		case plan.StatusError:
			rows = append(rows, m.excludedRow(ip))
			failed++
		}
	}
	return rows, applied, kept, failed
}

// excludedRow builds one excluded item's result row.
func (m *mutation) excludedRow(ip *plan.ItemPlan) resultRow {
	return resultRow{
		verb:     "excluded",
		key:      ip.Key,
		reasons:  view.ExclusionLines(ip),
		category: exclusionCategoryOf(ip),
	}
}

// hasPending reports whether the target holds staged pending actions of
// any kind — the item actions and the deferred operations both count:
// while any of them waits, the target's state is not settled.
func hasPending(s *session) bool {
	return s.pending != nil && (len(s.pending.Items) > 0 || len(s.pending.Deferred) > 0)
}
