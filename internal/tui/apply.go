package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dpleshakov/penates/internal/apply"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
	"github.com/dpleshakov/penates/internal/view"
)

// The apply flow of section 10 (tui-screens.md): the pending actions of the
// target collect into one apply, the summary of 10.1 confirms it — a verb
// group per item with a mark per path, and the excluded items with their
// reasons — the run drives the stepping apply of the core into the progress
// view of 10.2 through its step events, the result screen of 10.3 reports
// what landed, and the changes applied during the session accumulate into
// the record of 10.4 that the command prints to the regular output on exit.
// Nothing asks for variables here: the forms live at selection time (7.3),
// and a selection whose required answers went missing plans as excluded.

// pendingApply is one apply waiting on the summary's confirmation: the
// pending actions it was built from, and the paths its planning touches.
type pendingApply struct {
	selection   []string
	removals    []string
	resets      map[string]bool
	deferred    []state.Deferred
	paths       []string
	sharedPaths []string
}

// beginApply collects what the target's pending actions would apply and
// moves to the summary it waits on. A pending that holds no item actions
// still reaches the summary when deferred operations wait — completing
// them is what the apply is for (6.7).
func (m *model) beginApply() {
	p := m.collectApply()
	if len(p.selection) == 0 && len(p.removals) == 0 && len(p.deferred) == 0 {
		m.outcome = nil
		m.screen = screenResults
		return
	}
	m.buildApplySummary(p)
}

// collectApply gathers the target's pending actions into one pending apply.
// A removal wins over a held selection: the removed item does not join the
// installs of the same apply. A reset staged without a selection rides its
// own item into the apply, when the kit still provides the item and the
// lock still holds it — the plan never sees a reset that is not planned.
// The paths cover the selected items' owned files, the kits' shared files,
// and the removals' recorded files and shared files, so the planning sees
// everything it may touch.
func (m *model) collectApply() *pendingApply {
	p := &pendingApply{resets: map[string]bool{}}
	if pending := m.pendingActions(); pending != nil {
		p.deferred = pending.Deferred
		for key, item := range pending.Items {
			switch {
			case item.Remove:
				p.removals = append(p.removals, key)
			case item.Select:
				p.selection = append(p.selection, key)
			case item.Reset && m.kitItem(key) != nil && m.lk != nil && m.lk.Items[key] != nil:
				p.selection = append(p.selection, key)
			}
			if item.Reset {
				p.resets[key] = true
			}
		}
	}
	sort.Strings(p.selection)
	sort.Strings(p.removals)

	var paths []string
	seen := map[string]bool{}
	sharedSeen := map[string]bool{}
	var sharedPaths []string
	for _, k := range m.kits {
		for _, s := range k.Shared {
			if !sharedSeen[s] {
				sharedSeen[s] = true
				sharedPaths = append(sharedPaths, s)
			}
		}
	}
	for _, key := range p.selection {
		kitName, itemID, _ := strings.Cut(key, "/")
		for _, k := range m.kits {
			if k.Name != kitName {
				continue
			}
			for _, it := range k.Items {
				if it.ID != itemID || len(it.Problems) > 0 {
					// A broken item is never selected, and its
					// unvalidated targets must not reach the snapshot.
					continue
				}
				for _, c := range it.Contributes {
					if c.Kind == kit.KindFile && !seen[c.To] {
						seen[c.To] = true
						paths = append(paths, c.To)
					}
				}
			}
		}
	}

	// A removal is planned from the lock: its files and shared files join the
	// snapshot, for the items of kits that are gone as well.
	if m.lk != nil {
		for _, key := range p.removals {
			rec := m.lk.Items[key]
			if rec == nil {
				continue
			}
			for path := range rec.Files {
				if !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
			}
			for file := range rec.Sections {
				if !sharedSeen[file] {
					sharedSeen[file] = true
					sharedPaths = append(sharedPaths, file)
				}
			}
			for file := range rec.JSON {
				if !sharedSeen[file] {
					sharedSeen[file] = true
					sharedPaths = append(sharedPaths, file)
				}
			}
		}
	}
	sort.Strings(paths)
	sort.Strings(sharedPaths)
	p.paths = paths
	p.sharedPaths = sharedPaths
	return p
}

// summaryNoteColumn is the column the note behind a summary line starts at
// (10.1).
const summaryNoteColumn = 32

// summaryItem is one verb group of the summary (10.1): the item line with
// its verb, id, version and kit, and the path lines under it.
type summaryItem struct {
	verb    string
	id      string
	version string
	kit     string
	lines   []view.PathLine
}

// summaryExcluded is one excluded item of the summary (10.1).
type summaryExcluded struct {
	id      string
	reasons []string
}

// applySummary is the content the summary screen renders (10.1): the
// verb groups, the excluded, and the deferred operations an apply with
// no item actions completes or drops.
type applySummary struct {
	items    []summaryItem
	excluded []summaryExcluded
	deferred []string
}

// writes counts the path lines that write the target: the created, replaced
// and deleted content (10.1).
func (s applySummary) writes() int {
	n := 0
	for _, it := range s.items {
		for _, ln := range it.lines {
			switch ln.Mark {
			case view.MarkCreate, view.MarkReplace, view.MarkDelete:
				n++
			}
		}
	}
	return n
}

// applicable reports whether the summary holds anything to confirm: a
// summary whose every item is excluded cannot be applied, and its footer
// reads "nothing to apply" (10.1); a deferred-only apply is confirmable —
// completing the deferred operations is what it exists for.
func (s applySummary) applicable() bool { return len(s.items) > 0 || len(s.deferred) > 0 }

// buildApplySummary plans the pending apply without writing anything and
// shows what it would do (10.1). The apply runs only after the screen's
// confirmation, and plans again from the target of that moment.
func (m *model) buildApplySummary(p *pendingApply) {
	m.pending = p
	m.summaryOffset = 0
	snap, err := target.Read(m.target, p.paths, p.sharedPaths)
	if err != nil {
		m.outcome = &applyOutcome{err: "apply failed: " + err.Error()}
		m.screen = screenResults
		return
	}
	var installs, removals []*plan.ItemPlan
	if len(p.removals) > 0 {
		remove := map[string]bool{}
		for _, key := range p.removals {
			remove[key] = true
		}
		removals = plan.BuildRemoval(plan.Input{Lock: m.lk, Snapshot: snap, Remove: remove}).Items
	}
	if len(p.selection) > 0 {
		sources, err := kit.ReadSources(m.kits, p.selection)
		if err != nil {
			m.outcome = &applyOutcome{err: "apply failed: " + err.Error()}
			m.screen = screenResults
			return
		}
		installs = plan.Build(plan.Input{
			Kits:       m.kits,
			Selection:  p.selection,
			Lock:       m.lk,
			Snapshot:   snap,
			Sources:    sources,
			Answers:    m.currentAnswers(),
			Reset:      p.resets,
			Reconciled: m.reconcil,
		}).Items
	}
	s := summarize(installs, removals, m.lk, snap, p.resets)
	s.deferred = deferredSummaryLines(m.target, p.deferred)
	m.applySummary = &s
	m.screen = screenSummary
}

// deferredSummaryLines names the deferred operations the apply completes
// or drops (6.7), the lines the CLI's deferred-only apply confirms too.
func deferredSummaryLines(target string, ops []state.Deferred) []string {
	var lines []string
	for _, d := range ops {
		switch d.Kind {
		case state.DeferredDetachKit:
			lines = append(lines, "detach "+d.Kit+" from "+target)
		case state.DeferredForgetTarget:
			lines = append(lines, "forget "+target)
		}
	}
	return lines
}

// summarize lays the planner's verdicts out into the summary (10.1): one
// verb group per planned item, the installs first, then the updates and the
// downgrades, the removals, the resets, with a mark per path — a write the
// target has not seen marks `+`, owned content replaced marks `~`, owned
// content taken away marks `-`; content that stands as recorded and no
// action rewrites marks `=`; content a removal leaves behind as the user's
// own marks `!`. The excluded items group at the end with their reasons.
// lk and snap are the lock and the snapshot the plans were built against;
// resets are the items the apply resets to kit. The lines themselves come
// from internal/view, the one builder the CLI prints from too.
func summarize(installs, removals []*plan.ItemPlan, lk *lock.Lock, snap *target.Snapshot, resets map[string]bool) applySummary {
	s := applySummary{}
	var installItems, updateItems, removeItems, resetItems []summaryItem
	for _, ip := range installs {
		if ip.Status == plan.StatusError {
			s.excluded = append(s.excluded, summaryExcluded{id: planID(ip), reasons: view.ExclusionLines(ip)})
			continue
		}
		verb, lines := view.InstallLines(ip, lk, snap, resets)
		it := summaryItem{
			verb:    verb,
			id:      planID(ip),
			version: view.VersionText(ip, lk, verb),
			kit:     ip.KitName,
			lines:   lines,
		}
		switch verb {
		case view.VerbReset:
			resetItems = append(resetItems, it)
		case view.VerbUpdate, view.VerbDowngrade:
			updateItems = append(updateItems, it)
		default:
			installItems = append(installItems, it)
		}
	}
	for _, ip := range removals {
		if ip.Status == plan.StatusError {
			s.excluded = append(s.excluded, summaryExcluded{id: planID(ip), reasons: view.ExclusionLines(ip)})
			continue
		}
		removeItems = append(removeItems, summaryItem{
			verb:    view.VerbRemove,
			id:      planID(ip),
			version: ip.Version,
			kit:     ip.KitName,
			lines:   view.RemovalLines(ip),
		})
	}
	s.items = append(s.items, installItems...)
	s.items = append(s.items, updateItems...)
	s.items = append(s.items, removeItems...)
	s.items = append(s.items, resetItems...)
	return s
}

// planID is the id a summary line shows: the item id, or the whole key when
// it does not split.
func planID(ip *plan.ItemPlan) string {
	if ip.ItemID != "" {
		return ip.ItemID
	}
	return ip.Key
}

// renderSummary lays the summary out into body lines (10.1): one verb group
// per item — the item line with the kit flush right, the path lines under
// it — then the excluded with their reasons. A blank line separates the
// groups.
func renderSummary(s applySummary, width int, set Set) []string {
	var rows []string
	for _, it := range s.items {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		left := fmt.Sprintf("%-7s  %s", it.verb, it.id)
		if it.version != "" {
			left += " " + Dim(it.version)
		}
		rows = append(rows, JoinSides(left, Dim(it.kit), width))
		for _, ln := range it.lines {
			row := "  " + pathMarks[ln.Mark].rendered(set) + " " + ln.Path
			if ln.Note != "" {
				row = padNote(row, ln.Note)
			}
			rows = append(rows, row)
		}
	}
	if len(s.deferred) > 0 {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, "deferred")
		for _, line := range s.deferred {
			rows = append(rows, "  "+line)
		}
	}
	if len(s.excluded) > 0 {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, "excluded")
		for _, e := range s.excluded {
			left := "  " + statusMark(StatusConflict, set) + " " + e.id
			if len(e.reasons) == 0 {
				rows = append(rows, left)
				continue
			}
			rows = append(rows, padNote(left, e.reasons[0]))
			for _, r := range e.reasons[1:] {
				rows = append(rows, "    "+Dim(r))
			}
		}
	}
	return rows
}

// padNote puts a note behind a summary line, starting at the note column
// (10.1); a line that runs past the column keeps one space before it.
func padNote(line, note string) string {
	if gap := summaryNoteColumn - ansi.StringWidth(line); gap > 0 {
		return line + strings.Repeat(" ", gap) + Dim(note)
	}
	return line + " " + Dim(note)
}

// summaryCounts is the right side of the summary header (10.1): the items,
// the writes and the exclusions, the zero counts left out.
func (m *model) summaryCounts() string {
	s := m.applySummary
	if s == nil {
		return ""
	}
	var parts []string
	if n := len(s.items); n > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, "item", "items")))
	}
	if n := s.writes(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, "write", "writes")))
	}
	if n := len(s.deferred); n > 0 {
		parts = append(parts, fmt.Sprintf("%d deferred", n))
	}
	if n := len(s.excluded); n > 0 {
		parts = append(parts, fmt.Sprintf("%d excluded", n))
	}
	return strings.Join(parts, " · ")
}

// The steps of one running apply (10.2, 10.3).
type stepState int

const (
	stepWaiting stepState = iota
	stepRunning
	stepDone
	stepFailed
	// stepKept: the item ran no step — a NoAction verdict keeps it as
	// installed, a perform-time exclusion stops it before any write.
	stepKept
)

// applyStep is one item of the apply: its line in the progress view (10.2)
// and its row in the result (10.3).
type applyStep struct {
	key      string
	verb     string // install, update, downgrade, reset, remove
	id       string
	version  string // the version the line reports
	from     string // the installed version an update moves from
	state    stepState
	err      string          // the failure of a failed step
	note     string          // the reason a kept item keeps
	excluded []string        // the reasons of an item the perform-time planning excluded
	kept     []plan.KeptNote // the content a removal left behind
}

// applyOutcome is what one run of the apply produced: the row per planned
// item for the result screen (10.3), the pending actions as the run left
// them, and the deferred report of 6.7.
type applyOutcome struct {
	// err is the failure that stopped the run before any step.
	err      string
	steps    []applyStep
	removed  map[string]bool
	deferred []string
	errs     []string
	lk       *lock.Lock
	reg      *state.Registry
}

// stepOf builds the row of one planned item: the verb, the id and the
// versions its lines report (10.2, 10.3).
func stepOf(ip *plan.ItemPlan, verb string, lk *lock.Lock) applyStep {
	st := applyStep{
		key:     ip.Key,
		verb:    verb,
		id:      planID(ip),
		version: ip.Version,
	}
	if verb == view.VerbRemove {
		return st
	}
	if rec := lk.Items[ip.Key]; rec != nil {
		st.from = rec.Version
	}
	return st
}

// queuedSteps builds the progress lines of the steps the apply is about to
// run: the removals first, then the installs (10.2). Items the planning
// excluded run no step and join the result only.
func queuedSteps(removals, installs []*plan.ItemPlan, lk *lock.Lock, resets map[string]bool) []applyStep {
	var steps []applyStep
	for _, ip := range removals {
		if ip.Status == plan.StatusRemove {
			st := stepOf(ip, view.VerbRemove, lk)
			st.state = stepWaiting
			steps = append(steps, st)
		}
	}
	for _, ip := range installs {
		if ip.Status == plan.StatusInstall {
			st := stepOf(ip, view.PlanVerb(ip, lk, resets), lk)
			st.state = stepWaiting
			steps = append(steps, st)
		}
	}
	return steps
}

// The messages the run of one apply sends through the event loop: the queue
// of the steps, one boundary per step, and the outcome that settles the
// model (10.2).
type applyDoneMsg struct{ out *applyOutcome }

// progressTickMsg advances the spinner of the progress view (10.2).
type progressTickMsg time.Time

// progressTickInterval is how often the spinner of the progress view turns.
const progressTickInterval = 100 * time.Millisecond

// tickProgress arms the next spinner tick.
func tickProgress() tea.Cmd {
	return tea.Tick(progressTickInterval, func(t time.Time) tea.Msg { return progressTickMsg(t) })
}

// confirmApply leaves the summary for the progress view (10.2) and starts
// the run: the apply executes outside the update loop, its events arrive
// through the channel, and ctrl+c stays a plain quit — the next start
// reconciles the interruption. An apply whose pending holds no item
// actions completes the deferred operations directly (6.7): there are no
// steps to follow, so the report becomes the result without a progress
// view.
func (m *model) confirmApply() tea.Cmd {
	p := m.pending
	m.pending = nil
	m.applySummary = nil
	m.summaryOffset = 0
	m.progress = nil
	m.spinner = 0
	if len(p.selection) == 0 && len(p.removals) == 0 {
		m.finishApply(m.completeDeferredOnly())
		return nil
	}
	m.screen = screenProgress
	ch := make(chan any, 256)
	m.applyCh = ch
	run := func() tea.Msg {
		out := m.runApply(p, ch)
		close(ch)
		return applyDoneMsg{out}
	}
	return tea.Batch(run, m.listenApply(), tickProgress())
}

// completeDeferredOnly finishes a pending whose deferred operations wait
// alone: the lock as it stands decides — a detach whose kit lost its last
// installed item completes, one the apply cannot cover is dropped with
// the reason in the report. It is the twin of the CLI's deferred-only
// apply: nothing planned, the confirmation behind it, the report as the
// whole outcome.
func (m *model) completeDeferredOnly() *applyOutcome {
	out := &applyOutcome{removed: map[string]bool{}}
	out.lk, out.reg, out.deferred, out.errs = completeDeferred(m.app, m.target, nil)
	return out
}

// listenApply waits for the next event of the running apply.
func (m *model) listenApply() tea.Cmd {
	ch := m.applyCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return nil
		}
		return v
	}
}

// runApply performs one confirmed apply: the removals first, then the
// installs planned from the target as the removals leave it — both plan
// again from a fresh snapshot, so a removed item's content cannot rise
// again in a shared file. It runs off the update loop and touches no model
// field: the step events and the queue go through the channel, and the
// outcome returns to the loop as one message.
func (m *model) runApply(p *pendingApply, events chan<- any) *applyOutcome {
	out := &applyOutcome{removed: map[string]bool{}}
	on := func(ev apply.StepEvent) { events <- ev }
	lk := m.lk

	snap, err := target.Read(m.target, p.paths, p.sharedPaths)
	if err != nil {
		out.err = "apply failed: " + err.Error()
		return out
	}

	var removalPlans, installPlans []*plan.ItemPlan
	if len(p.removals) > 0 {
		remove := map[string]bool{}
		for _, key := range p.removals {
			remove[key] = true
		}
		removalPlans = plan.BuildRemoval(plan.Input{Lock: lk, Snapshot: snap, Remove: remove}).Items
	}
	var sources map[string][]byte
	if len(p.selection) > 0 {
		sources, err = kit.ReadSources(m.kits, p.selection)
		if err != nil {
			out.err = "apply failed: " + err.Error()
			return out
		}
		installPlans = plan.Build(plan.Input{
			Kits:       m.kits,
			Selection:  p.selection,
			Lock:       lk,
			Snapshot:   snap,
			Sources:    sources,
			Answers:    m.currentAnswers(),
			Reset:      p.resets,
			Reconciled: m.reconcil,
		}).Items
	}
	events <- queuedSteps(removalPlans, installPlans, lk, p.resets)

	if len(removalPlans) > 0 {
		out.steps = append(out.steps, runRemovals(m.target, removalPlans, lk, m.app.Locks, on, out)...)
		if loaded, err := m.app.Locks.Load(m.target); err == nil {
			lk = loaded
		}
		// The removals rewrote and deleted what the snapshot observed: the
		// installs plan from the target as the removals leave it.
		if len(p.selection) > 0 {
			snap, err = target.Read(m.target, p.paths, p.sharedPaths)
			if err != nil {
				out.err = "apply failed: " + err.Error()
				return out
			}
		}
	}
	if len(p.selection) > 0 {
		sources, err = kit.ReadSources(m.kits, p.selection)
		if err != nil {
			out.err = "apply failed: " + err.Error()
			return out
		}
		planned := plan.Build(plan.Input{
			Kits:       m.kits,
			Selection:  p.selection,
			Lock:       lk,
			Snapshot:   snap,
			Sources:    sources,
			Answers:    m.currentAnswers(),
			Reset:      p.resets,
			Reconciled: m.reconcil,
		})
		out.steps = append(out.steps, runInstalls(m.target, planned.Items, lk, m.app.Locks, on, p.resets)...)
	}

	// The settle is the state operation the CLI's apply runs too: what the
	// run satisfied is consumed — the selection with its answers, the
	// reset, the successful removal — while a failed step and an excluded
	// item keep their actions for the next apply.
	if err := m.app.State.SettlePending(m.target, satisfiedKeys(out.steps), out.removed); err != nil {
		out.errs = append(out.errs, "could not save the state: "+err.Error())
	}

	// The deferred operations the apply covered complete here: a kit whose
	// removals landed detaches from the target, one it did not cover is
	// dropped with the reason in the report (6.7).
	out.lk, out.reg, out.deferred, out.errs = completeDeferred(m.app, m.target, out.errs)

	return out
}

// satisfiedKeys names the keys whose outcome satisfies their intent, the
// input the settle consumes: a done step landed and a kept step without
// exclusion lines kept the item as it is; a failed step and a kept step
// the perform-time planning excluded hold no outcome and keep their
// staged actions.
func satisfiedKeys(steps []applyStep) map[string]bool {
	satisfied := map[string]bool{}
	for _, st := range steps {
		if st.state == stepDone || (st.state == stepKept && st.excluded == nil) {
			satisfied[st.key] = true
		}
	}
	return satisfied
}

// runRemovals executes the removal steps of one apply and builds their
// result rows (10.3). The rows are built before the steps run: a step that
// ran rewrites the lock, and the rows after it must not read it changed.
func runRemovals(target string, items []*plan.ItemPlan, lk *lock.Lock, store *lock.Store, on func(apply.StepEvent), out *applyOutcome) []applyStep {
	steps := make([]applyStep, 0, len(items))
	for _, ip := range items {
		switch ip.Status {
		case plan.StatusRemove:
			steps = append(steps, stepOf(ip, view.VerbRemove, lk))
		case plan.StatusError:
			steps = append(steps, applyStep{key: ip.Key, id: planID(ip), verb: view.VerbRemove, state: stepKept, excluded: view.ExclusionLines(ip)})
		}
	}
	results := apply.Remove(target, &plan.Plan{Items: items}, lk, store, on)
	errs := map[string]string{}
	for _, r := range results {
		if r.Err != nil {
			errs[r.Key] = r.Err.Error()
		}
	}
	for i, ip := range items {
		if steps[i].state == stepKept {
			continue
		}
		if err, failed := errs[ip.Key]; failed {
			steps[i].state, steps[i].err = stepFailed, err
			continue
		}
		steps[i].state = stepDone
		steps[i].kept = ip.Kept
		out.removed[ip.Key] = true
	}
	return steps
}

// runInstalls executes the install steps of one apply and builds their
// result rows (10.3): an applied item, a kept item with its verdict reason,
// and an item the perform-time planning excluded with its reasons. The rows
// are built before the steps run: a step that ran rewrites the lock, and
// the rows after it must not read it changed.
func runInstalls(target string, items []*plan.ItemPlan, lk *lock.Lock, store *lock.Store, on func(apply.StepEvent), resets map[string]bool) []applyStep {
	steps := make([]applyStep, 0, len(items))
	for _, ip := range items {
		switch ip.Status {
		case plan.StatusInstall, plan.StatusNoAction:
			st := stepOf(ip, view.PlanVerb(ip, lk, resets), lk)
			if ip.Status == plan.StatusNoAction {
				st.state = stepKept
				st.note = ip.Reason
			}
			steps = append(steps, st)
		case plan.StatusError:
			steps = append(steps, applyStep{key: ip.Key, id: planID(ip), verb: view.PlanVerb(ip, lk, resets), state: stepKept, excluded: view.ExclusionLines(ip)})
		}
	}
	results := apply.Apply(target, &plan.Plan{Items: items}, lk, store, on)
	errs := map[string]string{}
	for _, r := range results {
		if r.Err != nil {
			errs[r.Key] = r.Err.Error()
		}
	}
	for i := range steps {
		if steps[i].state != stepWaiting {
			continue
		}
		if err, failed := errs[steps[i].key]; failed {
			steps[i].state, steps[i].err = stepFailed, err
		} else {
			steps[i].state = stepDone
		}
	}
	return steps
}

// completeDeferred finishes the deferred operations of the target after the
// apply (6.7): the lock as the apply left it decides — a deferred detach
// whose kit lost its last installed item completes, one the apply did not
// finish is dropped with its reason. It returns the lock and the registry
// as the run left them, the report, and the errors to show.
func completeDeferred(app *App, target string, errs []string) (*lock.Lock, *state.Registry, []string, []string) {
	lk, err := app.Locks.Load(target)
	if err != nil {
		return nil, nil, nil, append(errs, "could not complete the deferred operations: "+err.Error())
	}
	installed := make([]string, 0, len(lk.Items))
	for key := range lk.Items {
		installed = append(installed, key)
	}
	sort.Strings(installed)
	report, err := app.State.CompleteDeferred(target, installed, app.Locks)
	if err != nil {
		return lk, nil, nil, append(errs, "could not complete the deferred operations: "+err.Error())
	}
	var reg *state.Registry
	if len(report) > 0 {
		if r, err := app.State.LoadRegistry(); err == nil {
			reg = r
		} else {
			errs = append(errs, "could not reload the state: "+err.Error())
		}
	}
	return lk, reg, report, errs
}

// finishApply settles the model after one run: the outcome becomes the
// result screen (10.3), the record of 10.4 takes the successful steps, and
// the lock and the registry come back as the run left them.
func (m *model) finishApply(out *applyOutcome) {
	m.applyCh = nil
	m.progress = nil
	m.outcome = out
	m.record = recordOf(m.target, out.steps, m.record)
	if out.lk != nil {
		m.lk = out.lk
	}
	if out.reg != nil {
		m.reg = out.reg
	}
	m.screen = screenResults
}

// stepEvent moves one step of the running apply along its boundaries (10.2):
// the start lights the spinner on its line, the result settles the line
// with a check or a cross.
func (m *model) stepEvent(ev apply.StepEvent) {
	for i := range m.progress {
		if m.progress[i].key != ev.Key {
			continue
		}
		switch {
		case ev.Start && m.progress[i].state == stepWaiting:
			m.progress[i].state = stepRunning
		case !ev.Start && m.progress[i].state == stepRunning:
			if ev.Err != nil {
				m.progress[i].state, m.progress[i].err = stepFailed, ev.Err.Error()
			} else {
				m.progress[i].state = stepDone
			}
		}
		return
	}
}

// renderProgress lays the running apply out into body lines (10.2): one
// line per step, the spinner on the running one, a check on a completed
// one, a cross on a failed one.
func renderProgress(steps []applyStep, spinner int, set Set) []string {
	frames := SpinnerFrames(set)
	var rows []string
	for _, st := range steps {
		var cell string
		switch st.state {
		case stepDone:
			cell = doneMark.rendered(set)
		case stepFailed:
			cell = statusMark(StatusConflict, set)
		case stepRunning:
			cell = gloss(roleAccent).Render(frames[spinner%len(frames)])
		default:
			cell = " "
		}
		row := "  " + cell + " " + fmt.Sprintf("%-7s  %s", st.verb, st.id)
		if st.version != "" {
			row += " " + Dim(st.version)
		}
		rows = append(rows, row)
	}
	return rows
}

// resultNoteColumn is the column the notes of the result lines start at —
// the kept content of a removal, the reason of a failure (10.3).
const resultNoteColumn = 14

// renderResults lays the outcome of one apply out into body lines (10.3):
// the failure that stopped the run whole, the applied items with their
// verb, the kept content of a removal under its line, every failed or
// excluded item with its reason, and the deferred report of 6.7 behind
// them.
func renderResults(err string, steps []applyStep, deferred, errs []string, set Set) []string {
	var rows []string
	if err != "" {
		rows = append(rows, err)
	}
	for _, st := range steps {
		switch {
		case st.state == stepDone:
			rows = append(rows, resultLine(st, doneMark.rendered(set), set))
			for _, note := range keptNotes(st) {
				rows = append(rows, strings.Repeat(" ", resultNoteColumn)+Dim(note))
			}
		case st.state == stepFailed:
			rows = append(rows, resultLine(st, statusMark(StatusConflict, set), set))
			rows = append(rows, strings.Repeat(" ", resultNoteColumn)+Dim(st.err))
		case st.excluded != nil:
			left := "  " + statusMark(StatusConflict, set) + fmt.Sprintf(" %-9s  %s", "excluded", st.id)
			if len(st.excluded) == 0 {
				rows = append(rows, left)
				continue
			}
			rows = append(rows, padNote(left, st.excluded[0]))
			for _, r := range st.excluded[1:] {
				rows = append(rows, "    "+Dim(r))
			}
		default: // stepKept: nothing was written for the item
			left := "  " + pathMarks[view.MarkKeep].rendered(set) + fmt.Sprintf(" %-9s  %s", "kept", st.id)
			if st.version != "" {
				left += " " + Dim(st.version)
			}
			if st.note != "" {
				left += "  " + Dim(st.note)
			}
			rows = append(rows, left)
		}
	}
	for _, line := range deferred {
		rows = append(rows, Dim(line))
	}
	for _, line := range errs {
		rows = append(rows, Dim(line))
	}
	return rows
}

// resultLine is one applied or failed item's line of the result (10.3): the
// mark, the past verb, the id and the version it reached — a removal names
// no version, what it took away is gone.
func resultLine(st applyStep, mk string, set Set) string {
	row := "  " + mk + fmt.Sprintf(" %-9s  %s", view.PastVerb(st.verb), st.id)
	if st.version != "" && st.verb != view.VerbRemove {
		row += " " + Dim(st.version)
	}
	return row
}

// keptNotes names the content a removal left behind, one note per path
// (10.3).
func keptNotes(st applyStep) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range st.kept {
		if seen[k.Path] {
			continue
		}
		seen[k.Path] = true
		out = append(out, k.Path+" kept: "+k.Reason)
	}
	return out
}

// resultCounts is the right side of the result header (10.3). A run with
// no item steps — a deferred-only completion — reports no counts: its
// report lines are the whole story.
func (m *model) resultCounts() string {
	if len(m.outcome.steps) == 0 {
		return ""
	}
	done, failed := 0, 0
	for _, st := range m.outcome.steps {
		switch {
		case st.state == stepDone:
			done++
		case st.state == stepFailed || st.excluded != nil:
			failed++
		}
	}
	return fmt.Sprintf("%d done · %d failed", done, failed)
}

// recordEntry is one applied change of the session record (10.4).
type recordEntry struct {
	verb    string // past: installed, updated, downgraded, reset, removed
	key     string // the full item key
	version string // the version text: "1.2.0 → 1.3.0" for an update
	kept    []string
}

// recordBlock is the record of one target.
type recordBlock struct {
	target  string
	entries []recordEntry
}

// recordOf appends the successful steps of one apply to the session record
// (10.4): the installs before the removals, the version an update moved
// from and to, the kept files of a removal in parentheses.
func recordOf(target string, steps []applyStep, record []recordBlock) []recordBlock {
	var installs, removals []recordEntry
	for _, st := range steps {
		if st.state != stepDone {
			continue
		}
		e := recordEntry{verb: view.PastVerb(st.verb), key: st.key}
		if st.verb == view.VerbRemove {
			seen := map[string]bool{}
			for _, k := range st.kept {
				if seen[k.Path] {
					continue
				}
				seen[k.Path] = true
				e.kept = append(e.kept, k.Path)
			}
			removals = append(removals, e)
			continue
		}
		switch {
		case st.from != "" && st.from != st.version:
			e.version = st.from + " → " + st.version
		default:
			e.version = st.version
		}
		installs = append(installs, e)
	}
	if len(installs)+len(removals) == 0 {
		return record
	}
	entries := installs
	entries = append(entries, removals...)
	for i := range record {
		if record[i].target == target {
			record[i].entries = append(record[i].entries, entries...)
			return record
		}
	}
	return append(record, recordBlock{target: target, entries: entries})
}

// recordText is the session record (10.4): one block per target, each
// applied change on its line; empty when nothing was applied.
func (m *model) recordText() string {
	var b strings.Builder
	for _, block := range m.record {
		fmt.Fprintf(&b, "penates: applied to %s\n", block.target)
		for _, e := range block.entries {
			fmt.Fprintf(&b, "  %-9s  %s", e.verb, e.key)
			if e.version != "" {
				b.WriteString(" " + e.version)
			}
			if len(e.kept) > 0 {
				fmt.Fprintf(&b, " (kept: %s)", strings.Join(e.kept, ", "))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// resultText is the result screen's rows as one plain string, bare of
// colors — the tests assert against it.
func (m *model) resultText() string {
	if m.outcome == nil {
		return "nothing selected"
	}
	return strings.Join(renderResults(m.outcome.err, m.outcome.steps, m.outcome.deferred, m.outcome.errs, m.set), "\n")
}

// ExitRecord returns the record of the changes applied during the session
// (10.4), for the command to print to the regular output after the program
// exits. Nothing was applied when it is empty.
func ExitRecord(m tea.Model) string {
	if mm, ok := m.(*model); ok {
		return mm.recordText()
	}
	return ""
}
