package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/dpleshakov/penates/internal/apply"
	"github.com/dpleshakov/penates/internal/builtin"
	"github.com/dpleshakov/penates/internal/guard"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// The load path every state-reading command walks: open the state, bring
// the built-in kit in step (the sync writes into the penates data
// directory only, which reading commands are allowed), resolve the target,
// then load what the target's lines are built from — the lock, the
// attached kits read tolerantly, the snapshot, the reconciliation, and the
// planner's verdicts of the staged selection. All of it is values by the
// time the rendering starts, the way the TUI loads for its tree.

// session is what one command run reads.
type session struct {
	dataDir  string
	state    *state.Store
	locks    *lock.Store
	registry *state.Registry
	builtin  *builtin.Report

	target   string // the resolved target's path
	name     string // its display name, empty when none
	lock     *lock.Lock
	kits     []*kit.Kit
	snap     *target.Snapshot
	reconcil *plan.Reconciliation
	plans    map[string]*plan.ItemPlan
	pending  *state.Pending

	// stagedRemoved lists the temp files the write path's reconciliation
	// removed — the debris of an interrupted apply, reported by the
	// command that cleaned it.
	stagedRemoved []string
}

// runGuard is the guard the process holds over its data directory. A
// real run takes it once, in the first session it opens, and keeps it
// until the process ends — the process's death releases the hold. The
// only caller that finds a guard already here is a test running many
// command runs back to back in one process: the previous run's guard is
// released the way its process ending would have.
var runGuard *guard.Guard

// takeRunGuard takes the process's run guard over the data directory,
// before anything reads or writes there. A refusal means another penates
// process holds the directory — an open terminal UI, most likely — and
// says so under the busy category; every other failure is the state's:
// the directory or its lock file cannot be made.
func takeRunGuard(dataDir string) *Error {
	if runGuard != nil {
		_ = runGuard.Release()
		runGuard = nil
	}
	g, err := guard.Acquire(dataDir)
	if err != nil {
		return GuardError(err)
	}
	runGuard = g
	return nil
}

// GuardError maps what taking the run guard reported: the busy refusal
// keeps its category, every other failure — the directory or its lock
// file cannot be made — is the state's. The terminal entry maps its own
// guard through it too.
func GuardError(err error) *Error {
	var busy *guard.BusyError
	if errors.As(err, &busy) {
		return &Error{Category: CategoryBusy, Message: busy.Error(), Cause: busy}
	}
	return &Error{Category: CategoryState, Message: err.Error(), Cause: err}
}

// openSession resolves the data directory, opens the state, brings the
// built-in kit in step and loads the registry. The run guard is taken
// before the state opens, so a busy directory refuses before anything
// reads there. The state is read before the sync, so a state file the
// store refuses is the state category and nothing else runs; the sync
// registers the built-in kit while the name is free, and the registry
// the commands see is the one it left behind.
func openSession() (*session, *Error) {
	dataDir, err := state.DefaultDir()
	if err != nil {
		return nil, &Error{Category: CategoryState, Message: fmt.Sprintf("cannot resolve the penates data directory: %v", err), Cause: err}
	}
	if sessErr := takeRunGuard(dataDir); sessErr != nil {
		return nil, sessErr
	}
	st, err := state.Open(dataDir)
	if err != nil {
		return nil, &Error{Category: CategoryState, Message: err.Error(), Cause: err}
	}
	if _, err := st.LoadRegistry(); err != nil {
		return nil, &Error{Category: CategoryState, Message: err.Error(), Cause: err}
	}
	report, err := builtin.Sync(dataDir, st, builtin.Content)
	if err != nil {
		return nil, &Error{Category: CategoryIO, Message: fmt.Sprintf("cannot bring the built-in kit in step in %s: %v", dataDir, err), Cause: err}
	}
	reg, err := st.LoadRegistry()
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

// resolveTarget picks the target the command acts on: the --target flag
// every target-bound command requires. There is no fallback: which target
// the terminal UI opens is the TUI's own state, and the CLI neither reads
// it nor writes it — a command line that does not name its target is not
// self-describing. An unregistered path is refused with the way to
// register it.
func (s *session) resolveTarget(explicit string) *Error {
	path := explicit
	if path == "" {
		// Unreachable while every caller's command requires the flag;
		// kept so a command that stops requiring it fails the contract
		// loudly instead of acting on a guess.
		return &Error{
			Category: CategoryUsage,
			Message:  `required flag(s) "target" not set (run "penates --help")`,
		}
	}
	t, ok := s.registry.Targets[path]
	if !ok {
		return &Error{
			Category: CategoryNotFound,
			Message:  fmt.Sprintf("no target is registered at %s (register it with %q)", path, "penates target add "+path),
		}
	}
	s.target, s.name = path, t.Name
	s.pending = s.registry.Pending[path]
	return nil
}

// loadTarget loads everything the target's lines are built from, and
// reconciles the lock with the snapshot without writing: the reading
// commands write nothing outside the penates data directory.
func (s *session) loadTarget() *Error {
	if err := s.loadTargetBase(); err != nil {
		return err
	}
	s.reconcil = plan.Reconcile(s.lock, s.snap)
	s.planSelection()
	return nil
}

// loadTargetForWrite is the write path's load: the same values, but the
// reconciliation is the apply package's — it removes the staged temp
// files an interrupted apply left beside their destinations, a write to
// the target that only a mutating command may make.
func (s *session) loadTargetForWrite() *Error {
	if err := s.loadTargetBase(); err != nil {
		return err
	}
	rec, staged, err := apply.Reconcile(s.target, s.lock, s.snap)
	if err != nil {
		return &Error{Category: CategoryIO, Message: err.Error(), Cause: err}
	}
	s.reconcil = rec
	s.stagedRemoved = staged
	s.planSelection()
	return nil
}

// loadTargetBase loads everything the target's lines are built from. The
// kits read tolerantly, the way the tree loads them: a kit whose kit.yaml
// or items directory cannot be read stays out and the lines show it as
// unavailable.
func (s *session) loadTargetBase() *Error {
	lk, err := s.locks.Load(s.target)
	if err != nil {
		return &Error{Category: CategoryIO, Message: err.Error(), Cause: err}
	}
	s.lock = lk
	for _, name := range s.registry.KitsFor(s.target) {
		kk, ok := s.registry.Kits[name]
		if !ok {
			continue
		}
		k, _, err := kit.Read(kk.Dir)
		if err != nil {
			continue // the kit shows as unavailable
		}
		s.kits = append(s.kits, k)
	}
	files, shared := s.snapshotPaths()
	snap, err := target.Read(s.target, files, shared)
	if err != nil {
		return &Error{Category: CategoryIO, Message: err.Error(), Cause: err}
	}
	s.snap = snap
	return nil
}

// snapshotPaths assembles the paths the snapshot covers: the paths the
// lock records and every healthy contribution of the loaded kits, so the
// reconciliation and the planning both see the target as it stands. A
// broken item contributes nothing, and its unvalidated targets must not
// reach the snapshot.
func (s *session) snapshotPaths() (files, shared []string) {
	if s.lock != nil {
		files, shared = s.lock.RecordedPaths()
	}
	seenFile := map[string]bool{}
	for _, p := range files {
		seenFile[p] = true
	}
	seenShared := map[string]bool{}
	for _, p := range shared {
		seenShared[p] = true
	}
	for _, k := range s.kits {
		for _, it := range k.Items {
			if len(it.Problems) > 0 {
				continue
			}
			for _, c := range it.Contributes {
				if c.Kind != kit.KindFile {
					if !seenShared[c.To] {
						seenShared[c.To] = true
						shared = append(shared, c.To)
					}
					continue
				}
				if !seenFile[c.To] {
					seenFile[c.To] = true
					files = append(files, c.To)
				}
			}
		}
	}
	sort.Strings(files)
	sort.Strings(shared)
	return files, shared
}

// planSelection plans the target's staged selection the way an apply
// would, without writing anything: the verdicts turn the planner's
// conflicts and warnings into item states. A staged answer wins over the
// one recorded in the lock, as it does at planning. A source the kit no
// longer provides is a kit problem the apply reports; the lines simply
// show no planned states.
func (s *session) planSelection() {
	var selection []string
	if s.pending != nil {
		for key, item := range s.pending.Items {
			if item.Select && !item.Remove {
				selection = append(selection, key)
			}
		}
	}
	sort.Strings(selection)
	if len(selection) == 0 || s.snap == nil || len(s.kits) == 0 {
		return
	}
	sources, err := kit.ReadSources(s.kits, selection)
	if err != nil {
		return
	}
	p := plan.Build(plan.Input{
		Kits:       s.kits,
		Selection:  selection,
		Lock:       s.lock,
		Snapshot:   s.snap,
		Sources:    sources,
		Answers:    s.answers(),
		Reconciled: s.reconcil,
	})
	s.plans = map[string]*plan.ItemPlan{}
	for _, ip := range p.Items {
		s.plans[ip.Key] = ip
	}
}

// answers collects the answers the staged selection was given, keyed by
// item key. The caller plans only when the pending object is there.
func (s *session) answers() map[string]map[string]string {
	out := map[string]map[string]string{}
	for key, item := range s.pending.Items {
		if len(item.Answers) > 0 {
			out[key] = item.Answers
		}
	}
	return out
}

// loadedKit returns the loaded kit of that name, or nil when it did not
// load.
func (s *session) loadedKit(name string) *kit.Kit {
	for _, k := range s.kits {
		if k.Name == name {
			return k
		}
	}
	return nil
}
