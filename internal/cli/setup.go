package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/view"
)

// newSetupCmd builds "penates setup": the first run's CLI form, the twin
// of the terminal UI's first run. It attaches the built-in kit to the
// --target — registering the target when it is new — and installs the
// kit's items, the product's own skills. Idempotent: an attachment and
// items already in place are kept and reported, not errors, and a repeat
// asks nothing beyond the one confirmation. It refuses while the target
// holds staged pending actions, like every one-shot command, and trusts
// what it may write to recorded ownership, not to who ran first.
func newSetupCmd(ask *confirmer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup --target <path>",
		Short: "The first run's CLI form: attach the built-in kit to a target and install its skills",
		Long: "Attach the built-in kit to the target the --target flag names — registering\n" +
			"the target when it is new — and install the kit's skills, the product's own\n" +
			"documentation. The first run is usually a person's, through the terminal UI\n" +
			"a bare \"penates\" opens; this command is how an agent begins a machine\n" +
			"alone. Running it again changes nothing: the attachment and the installed\n" +
			"items are kept and reported.",
		Args: rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			explicit, _ := cmd.Flags().GetString("target")
			dry, yes := dryOf(cmd), yesOf(cmd)
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			s, err := openSession()
			if err != nil {
				return err
			}
			// The named target: a registered one carries its display
			// name and may hold staged pending actions, a new one is
			// registered by the attachment below.
			attached := false
			if t, ok := s.registry.Targets[explicit]; ok {
				s.target, s.name, s.pending = explicit, t.Name, s.registry.Pending[explicit]
				attached = isAttached(s.registry, state.BuiltinKitName, explicit)
			} else {
				s.target = explicit
			}
			// The built-in kit installs from its registered directory —
			// the startup sync has just kept it in step. A directory
			// that cannot be read is the kit's fault, like any
			// unreadable kit.
			regKit, ok := s.registry.Kits[state.BuiltinKitName]
			if !ok {
				return &Error{Category: CategoryKit, Message: "the built-in kit is not registered (its directory is not where penates manages it)"}
			}
			builtinKit, _, readErr := kit.Read(regKit.Dir)
			if readErr != nil {
				return &Error{Category: CategoryKit, Message: readErr.Error(), Cause: readErr}
			}
			// Where the kit is not attached yet it joins the session
			// before the load, so the snapshot covers the paths its
			// items will take: the planning, not the write, is what
			// refuses to touch content the target already holds.
			if !attached {
				s.kits = append(s.kits, builtinKit)
			}
			if err := s.loadTargetForWrite(); err != nil {
				return err
			}
			// The staged refusal stands before any write: a target that
			// holds staged pending actions refuses the setup before the
			// attachment lands, the way every one-shot command refuses
			// before it mutates.
			if err := stagedRefusal(s); err != nil {
				return err
			}
			// Attach, skipping the repeat an already attached kit would
			// be: a registration collision is kit attach's refusal, not
			// setup's — here the attachment existing is the good case.
			var notes []string
			if !dry {
				switch {
				case attached:
					notes = append(notes, "the built-in kit is already attached to "+explicit)
				default:
					if err := s.state.AttachKit(state.BuiltinKitName, explicit); err != nil {
						return stateOpError(err)
					}
					notes = append(notes, "attached the built-in kit to "+explicit)
				}
			}
			keys, keyErr := builtinItemKeys(builtinKit)
			if keyErr != nil {
				return keyErr
			}
			installs, err := buildInstalls(s, keys, nil, nil)
			if err != nil {
				return err
			}
			m := &mutation{s: s, out: out, errOut: errOut, conf: ask, dry: dry, yes: yes, installs: installs, notes: notes}
			return m.run()
		},
	}
	targetFlag(cmd)
	dryRunFlag(cmd)
	yesFlag(cmd)
	return cmd
}

// builtinItemKeys names every item the built-in kit provides, a broken
// declaration refused the way resolveNamedOne refuses one: the kit is to
// fix, not the command line.
func builtinItemKeys(k *kit.Kit) ([]string, *Error) {
	var keys []string
	for _, it := range k.Items {
		if len(it.Problems) > 0 {
			return nil, &Error{
				Category: CategoryKit,
				Message:  fmt.Sprintf("item %s has a broken declaration: %v", k.Name+"/"+view.RowID(it), it.Problems[0]),
			}
		}
		keys = append(keys, k.Name+"/"+view.RowID(it))
	}
	sort.Strings(keys)
	return keys, nil
}

// isAttached reports whether the attachment of kit to target is recorded
// in the registry the session holds.
func isAttached(reg *state.Registry, kitName, target string) bool {
	for _, a := range reg.Attachments {
		if a.Kit == kitName && a.Target == target {
			return true
		}
	}
	return false
}
