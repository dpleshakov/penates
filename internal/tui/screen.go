package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dpleshakov/penates/internal/apply"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// The main screen (tui-screens.md 4): the tree of kits and items rendered
// through a Layout and a glyph Set, the details pane beside it on wide
// terminals and the details view on enter, the filter input, and the
// first-run state with the add-kit input. The screen keeps no selection of
// its own: the pending actions of the target in the registry are the
// selection, saved on every toggle. Selecting an item with variables opens
// the variable form in the details (7.3). The target list, the command
// palette, the help screen, and the kit management dialogs overlay it.

// App carries the wiring the screens work through: the state store and the
// lock store of one penates data directory.
type App struct {
	State *state.Store
	Locks *lock.Store
}

// NewApp wires the stores.
func NewApp(st *state.Store, locks *lock.Store) *App {
	return &App{State: st, Locks: locks}
}

// Initial builds the first screen: the last active target (11), or the
// first-run state — the add-kit row (6.1) — when nothing is registered.
func (a *App) Initial() tea.Model {
	m := &model{
		app:            a,
		set:            SetFromEnv(os.Getenv),
		collapsed:      map[string]bool{},
		reviewed:       map[string]bool{},
		changelogCache: map[string][]ChangelogEntry{},
	}
	m.filter = textinput.New()
	m.pathInput = textinput.New()
	m.targetInput = textinput.New()
	m.formInput = textinput.New()

	reg, err := a.State.LoadRegistry()
	if err != nil {
		m.errLine = err.Error()
		return m
	}
	m.reg = reg

	paths := reg.TargetPaths()
	switch {
	case len(paths) == 0:
		m.refresh()
	case reg.LastTarget != "":
		m.enterTarget(reg.LastTarget)
	default:
		m.enterTarget(paths[0])
	}
	// A target that opens with saved pending actions offers their
	// restoration at startup, the way switching to it does (11.1).
	m.offerRestore()
	return m
}

type screen int

const (
	screenList screen = iota
	screenSummary
	screenProgress
	screenResults
)

type model struct {
	app    *App
	screen screen
	set    Set

	width  int
	height int

	// The target the screen shows, with everything the tree is built from:
	// the registry, the loaded kits, the lock, the snapshot and its
	// reconciliation, and the planner verdicts of the current selection.
	target   string
	reg      *state.Registry
	kits     []*kit.Kit
	lk       *lock.Lock
	snap     *target.Snapshot
	reconcil *plan.Reconciliation
	plans    map[string]*plan.ItemPlan

	rows      []Row
	cursor    int
	collapsed map[string]bool

	filter    textinput.Model
	filtering bool

	// reviewed marks the major updates the user has seen the review note
	// of (7.2): the first space opens the details, the second selects.
	reviewed map[string]bool

	// The details view (5): open over any width, esc closes it, the
	// arrows scroll the full lines through a ScrollWindow. viewPreview
	// marks the preview view the add-kit flow opens on the narrower
	// terminals (6.2); its lines render live from the draft.
	view        bool
	viewKit     bool
	viewKey     string
	viewLines   []string
	viewOffset  int
	viewPreview bool

	// The path input of 6.2 and 6.6: adding a kit on the add-kit row, or
	// pointing a kit to a new directory. The input edits with directory
	// completion; the debounced preview reads what the path names.
	inputKind inputKind
	pathInput textinput.Model
	// editName is the kit being relocated (6.6); editReadable and editErr
	// hold its state, read once when the input opened, for the pane to
	// render while the input names no kit directory.
	editName     string
	editReadable bool
	editErr      string
	// The first-target step of 6.4: the target input the preview ends
	// with when no target is registered, and the existence of the
	// directory it names.
	targetMode   bool
	targetInput  textinput.Model
	targetExists bool
	// completions are the directory candidates of the path input;
	// completion is the index the next tab completes.
	completions []string
	completion  int
	// preview is the read the last debounce tick performed over the path
	// input; previewSeq tags the pending tick, so a stale one drops.
	preview    *kitPreview
	previewSeq uint
	// attach is the kit the attach dialog of 6.3 acts on.
	attach *attachOffer

	// The open modal dialog (6.3, 6.7) and the kit it acts on: the dialog
	// takes the body while it is open, and the flows keep their context
	// beside it.
	dialog     *dialog
	detachName string
	// The target list overlay of 8.1 and the targets its dialogs act on:
	// the forget dialog keeps the path and the installed count it opened
	// with, the restore dialog the target it offers (11.1).
	list          *targetList
	forgetTarget  string
	forgetItems   int
	restoreTarget string

	// The command palette of 9 and the kit picker it opens when a kit
	// command runs outside a kit (9: they ask for one).
	palette *commandPalette
	kitPick *kitPick

	// The full help of 12.5: the bindings and the glyph legends, scrolled
	// like the details view.
	helpOpen   bool
	helpLines  []string
	helpOffset int

	notice Notice
	// noticeUndo is the command u runs while the notice names an immediate
	// configuration change (11.2): one press performs the reverse
	// operation. Any other key or the age-out drops the binding.
	noticeUndo func() tea.Cmd
	// ctrlcArmed marks the quit the first ctrl+c armed (12.1): the second
	// quits at once, any other key disarms.
	ctrlcArmed bool
	// errLine holds a load problem until the next successful reload; it
	// shares the notice line (2.1) but does not age out.
	errLine string

	changelogCache map[string][]ChangelogEntry

	// The variable form of the selection (7.3): open on selecting an item
	// with variables, it takes the details pane or the details view until
	// enter accepts the answers into the pending actions or esc clears the
	// selection. formFromView records a view that was open before the form,
	// so closing returns to it instead of the tree.
	formOpen     bool
	formKey      string
	formFromView bool
	formFields   []formField
	formCursor   int
	formAnswers  map[string]string
	formInput    textinput.Model
	pending      *pendingApply
	// The summary of the pending apply (10.1), the offset its body scrolls
	// with, the steps of the running apply and the spinner frame (10.2), the
	// outcome the result screen renders (10.3), the channel the run's events
	// arrive through, and the record of the session's applied changes that
	// the command prints on exit (10.4).
	applySummary  *applySummary
	summaryOffset int
	progress      []applyStep
	spinner       int
	applyCh       chan any
	outcome       *applyOutcome
	record        []recordBlock
}

// noticeTickMsg ages the notice line; the tick re-arms while a notice is
// visible.
type noticeTickMsg time.Time

func tickNotice() tea.Cmd {
	return tea.Tick(noticeTTL, func(t time.Time) tea.Msg { return noticeTickMsg(t) })
}

// showNotice puts text on the notice line and arms its timer.
func (m *model) showNotice(text string) tea.Cmd {
	m.notice.Show(text)
	return tickNotice()
}

// showUndoable puts a notice on the notice line and binds u to undo while
// the notice lives (11.2): one press performs the reverse operation of the
// immediate configuration change the notice names.
func (m *model) showUndoable(text string, undo func() tea.Cmd) tea.Cmd {
	m.noticeUndo = undo
	return m.showNotice(text)
}

// clearNotice clears the notice line, and the undo offer it carried, if
// any (11.2).
func (m *model) clearNotice() {
	m.notice.Clear()
	m.noticeUndo = nil
}

// Init schedules the commands the opening view needs: the input blink, the
// first preview read, and the aging of a visible notice.
func (m *model) Init() tea.Cmd {
	var cmds []tea.Cmd
	switch {
	case m.inputKind != inputNone:
		// The path input's initial preview read (6.2): an input that
		// already names a kit — the prefilled one of 6.1 — shows it.
		cmds = append(cmds, textinput.Blink, tickPreview(m.previewSeq))
	case m.formOpen:
		cmds = append(cmds, textinput.Blink)
	}
	// A notice the startup showed — the reconciliation of an interrupted
	// apply — ages out on its own.
	if m.notice.Visible() {
		cmds = append(cmds, tickNotice())
	}
	return tea.Batch(cmds...)
}

// Update applies one bubbletea message and returns the model and the
// commands it produced.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case noticeTickMsg:
		m.notice.Tick(time.Time(msg))
		if m.notice.Visible() {
			return m, tickNotice()
		}
		// The notice aged out; the undo it offered goes with it (11.2).
		m.noticeUndo = nil
		return m, nil
	case previewTickMsg:
		// A stale tick drops: the edit it was scheduled for is gone
		// (6.2: the read happens after the last keystroke only).
		if uint(msg) == m.previewSeq {
			m.readPreview()
		}
		return m, nil
	case []applyStep:
		// The queue of the running apply (10.2); a stale one drops — the
		// run that sent it is already settled.
		if m.applyCh == nil {
			return m, nil
		}
		m.progress = msg
		return m, m.listenApply()
	case apply.StepEvent:
		m.stepEvent(msg)
		return m, m.listenApply()
	case applyDoneMsg:
		m.finishApply(msg.out)
		return m, nil
	case progressTickMsg:
		if m.screen == screenProgress {
			m.spinner++
			return m, tickProgress()
		}
		return m, nil
	case tea.KeyMsg:
		// The double ctrl+c of 12.1: the first press arms the quit and
		// shows the notice, any other key disarms, the second quits at
		// once, without dialogs — the pending actions are saved on every
		// mutation, and an interrupted apply is repaired by
		// reconciliation (11.2). The progress view is not left by
		// navigation, yet the double press reaches it the same way
		// (10.2).
		if msg.Type == tea.KeyCtrlC {
			if m.ctrlcArmed {
				return m, tea.Quit
			}
			m.ctrlcArmed = true
			return m, m.showNotice("press ctrl+c again to quit")
		}
		m.ctrlcArmed = false
		switch m.screen {
		case screenList:
			return m.updateList(msg)
		case screenSummary:
			return m.updateSummary(msg)
		case screenProgress:
			// The view cannot be left by navigation while the items are
			// being applied (10.2); the double ctrl+c above stays the one
			// way out — the next start reconciles the interruption.
			return m, nil
		case screenResults:
			return m.updateResults(msg)
		}
	}
	return m, nil
}

func (m *model) updateSummary(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := m.viewportHeight()
	if page < 1 {
		page = 1
	}
	total := 0
	if m.applySummary != nil {
		total = len(renderSummary(*m.applySummary, m.width, m.set))
	}
	switch msg.String() {
	case "enter", "y":
		if m.pending == nil {
			m.screen = screenList
			return m, nil
		}
		if !m.applySummary.applicable() {
			// Every pending item is excluded: the footer reads "nothing to
			// apply" and enter does nothing (10.1).
			return m, nil
		}
		return m, m.confirmApply()
	case "esc", "n", "q":
		m.pending = nil
		m.applySummary = nil
		m.screen = screenList
	case "up":
		m.summaryOffset = ScrollOffset(m.summaryOffset-1, total, page)
	case "down":
		m.summaryOffset = ScrollOffset(m.summaryOffset+1, total, page)
	case "pgup":
		m.summaryOffset = ScrollOffset(m.summaryOffset-page, total, page)
	case "pgdown":
		m.summaryOffset = ScrollOffset(m.summaryOffset+page, total, page)
	}
	return m, nil
}

func (m *model) updateResults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "q" {
		return m, tea.Quit
	}
	// An apply may have completed the forget of the current target: the
	// screen cannot return to it, so the first registered path opens — or
	// the first run, when none is.
	if m.reg != nil {
		if _, ok := m.reg.Targets[m.target]; !ok {
			paths := m.reg.TargetPaths()
			if len(paths) == 0 {
				return m, m.enterTarget("")
			}
			return m, m.enterTarget(paths[0])
		}
	}
	return m, m.enterTarget(m.target)
}

// updateList dispatches the keys of the main screen: the modal dialog
// first, then the target list overlay, the command palette and the kit
// picker, the variable form, the path input of the kit flows, the details
// view, the help screen, the filter, and the tree. The input sits over the
// view: the preview view of 6.2 keeps the flow's keys. A key press clears
// the notice — except u acting on the undo the notice offers (11.2).
func (m *model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "u" || !m.notice.Visible() || m.noticeUndo == nil {
		m.clearNotice()
	}
	if m.dialog != nil {
		return m.updateDialog(msg)
	}
	if m.list != nil {
		return m.updateTargetList(msg)
	}
	if m.palette != nil {
		return m.updatePalette(msg)
	}
	if m.kitPick != nil {
		return m.updateKitPick(msg)
	}
	if m.formOpen {
		return m.updateForm(msg)
	}
	if m.inputKind != inputNone {
		return m.updateInput(msg)
	}
	if m.view {
		return m.updateView(msg)
	}
	if m.helpOpen {
		return m.updateHelp(msg)
	}
	if m.filtering {
		return m.updateFilter(msg)
	}
	return m.updateTree(msg)
}

// updateView feeds the details view: esc closes it, the arrows and page
// keys scroll, and space selects a major update under review (5, 7.2).
func (m *model) updateView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := m.viewportHeight()
	if page < 1 {
		page = 1
	}
	switch msg.String() {
	case "esc":
		m.view = false
		m.viewLines = nil
		m.viewOffset = 0
	case "up":
		m.viewOffset = ScrollOffset(m.viewOffset-1, len(m.viewLines), page)
	case "down":
		m.viewOffset = ScrollOffset(m.viewOffset+1, len(m.viewLines), page)
	case "pgup":
		m.viewOffset = ScrollOffset(m.viewOffset-page, len(m.viewLines), page)
	case "pgdown":
		m.viewOffset = ScrollOffset(m.viewOffset+page, len(m.viewLines), page)
	case " ":
		if !m.viewKit {
			if r, ok := m.itemRowByKey(m.viewKey); ok {
				cmd := m.spaceOnItem(r)
				m.openViewLines()
				return m, cmd
			}
		}
	}
	return m, nil
}

// updateFilter feeds the filter input: the tree filters as the user types,
// enter keeps the filter, esc clears it (7.5).
func (m *model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		return m, nil
	case "esc":
		m.filtering = false
		m.filter.Reset()
		m.refresh()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.refresh()
	return m, cmd
}

// updateTree moves the cursor and toggles the pending actions (12.1-12.4).
func (m *model) updateTree(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := m.viewportHeight()
	if page < 1 {
		page = 1
	}
	var cmd tea.Cmd
	switch msg.String() {
	case "up", "k":
		m.cursor--
	case "down", "j":
		m.cursor++
	case "pgup":
		m.cursor -= page
	case "pgdown":
		m.cursor += page
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = len(m.rows) - 1
	case "left":
		if len(m.rows) > 0 && m.rows[m.cursor].Kind == KindKit {
			m.collapseUnderCursor(true)
		} else {
			m.toKitRow()
		}
	case "right":
		if len(m.rows) > 0 && m.rows[m.cursor].Kind == KindKit {
			m.collapseUnderCursor(false)
		}
	case " ":
		cmd = m.spaceUnderCursor()
	case "d":
		if len(m.rows) > 0 && m.rows[m.cursor].Kind == KindKit {
			cmd = m.detachUnderCursor()
		} else {
			cmd = m.toggleRemove()
		}
	case "r":
		cmd = m.toggleReset()
	case "R":
		return m, m.rescanKits()
	case "e":
		// 12.2: the relocation input of 6.6 opens on the kit row, a
		// readable kit included.
		if len(m.rows) > 0 && m.rows[m.cursor].Kind == KindKit && !m.rows[m.cursor].Kit.Orphans {
			r := m.rows[m.cursor]
			m.startEditKit(r.Kit.Name, r.Kit.Path)
			return m, nil
		}
	case "enter":
		cmd = m.openDetails()
	case "/":
		// The filter takes over the keyboard: while it is open, q and a
		// type into it instead of quitting and applying.
		m.filtering = true
		m.filter.Reset()
		return m, m.filter.Focus()
	case "a":
		m.beginApply()
	case "t":
		// 12.1: the target list of 8.1 opens under the header.
		return m, m.openTargetList()
	case "ctrl+p":
		// 12.1: the command palette of 9 opens over the body.
		return m, m.openPalette()
	case "?":
		// 12.5: the full help opens over the body.
		m.openHelp()
		return m, nil
	case "u":
		// 11.2: the undo the notice offers, while it lives.
		if m.notice.Visible() && m.noticeUndo != nil {
			undo := m.noticeUndo
			m.noticeUndo = nil
			return m, undo()
		}
	case "q":
		return m, m.quit()
	}
	m.clampCursor()
	return m, cmd
}

// quit quits, asking about the pending actions first (11.3): a target
// that holds pending item actions opens the dialog — they are saved
// either way and offered for restoration on the next start — an empty one
// quits at once.
func (m *model) quit() tea.Cmd {
	if m.pendingCount() > 0 {
		m.openQuitDialog()
		return nil
	}
	return tea.Quit
}

// rescanKits re-reads the kits of the target (12.1, R) and reports what
// the rescan changed (11.2): the new item rows, the ones that became
// updates, the ones that became orphans — the diff of the tree before and
// after — and the pending actions the rescan invalidated, dropped and
// counted in the same message.
func (m *model) rescanKits() tea.Cmd {
	before := rowStatuses(m.rows)
	cmd := m.enterTarget(m.target)
	after := rowStatuses(m.rows)
	n := 0
	if m.reg != nil {
		n = len(m.reg.KitsFor(m.target))
	}
	newItems, updates, orphans := 0, 0, 0
	for key, status := range after {
		was, seen := before[key]
		switch {
		case !seen:
			newItems++
		case status == StatusUpdate && was != StatusUpdate:
			updates++
		case status == StatusOrphan && was != StatusOrphan:
			orphans++
		}
	}
	var parts []string
	if newItems > 0 {
		parts = append(parts, fmt.Sprintf("%d new %s", newItems, plural(newItems, "item", "items")))
	}
	if updates > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", updates, plural(updates, "update", "updates")))
	}
	if orphans > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", orphans, plural(orphans, "orphan", "orphans")))
	}
	dropped, err := m.dropInvalidPending()
	if err != nil {
		return tea.Batch(cmd, m.showNotice(err.Error()))
	}
	if dropped > 0 {
		parts = append(parts, fmt.Sprintf("%d pending %s dropped", dropped, plural(dropped, "action", "actions")))
	}
	text := fmt.Sprintf("Rescanned %d %s.", n, plural(n, "kit", "kits"))
	if len(parts) > 0 {
		text = fmt.Sprintf("Rescanned %d %s: %s.", n, plural(n, "kit", "kits"), strings.Join(parts, ", "))
	}
	return tea.Batch(cmd, m.showNotice(text))
}

// rowStatuses maps the item rows of a built tree by key to their statuses;
// the rescan diff compares two of these maps.
func rowStatuses(rows []Row) map[string]Status {
	out := make(map[string]Status, len(rows))
	for _, r := range rows {
		if r.Kind == KindItem {
			out[r.Item.Key] = r.Item.Status
		}
	}
	return out
}

// dropInvalidPending drops the saved item actions that no longer name
// anything they can act on (11.1, 11.2) and returns how many went.
func (m *model) dropInvalidPending() (int, error) {
	p := m.pendingActions()
	if p == nil || len(p.Items) == 0 {
		return 0, nil
	}
	var gone []string
	for key, item := range p.Items {
		if !m.validPendingAction(key, item) {
			gone = append(gone, key)
		}
	}
	if len(gone) == 0 {
		return 0, nil
	}
	sort.Strings(gone)
	err := m.app.State.Update(func(reg *state.Registry) error {
		p := reg.Pending[m.target]
		if p == nil {
			return nil
		}
		for _, key := range gone {
			delete(p.Items, key)
		}
		if len(p.Items) == 0 && len(p.Deferred) == 0 {
			delete(reg.Pending, m.target)
		} else {
			reg.Pending[m.target] = p
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := m.reloadRegistry(); err != nil {
		return len(gone), err
	}
	m.refresh()
	return len(gone), nil
}

// validPendingAction reports whether one saved action still names
// something it can act on (11.1): a removal and a reset need the
// installed record, a selection needs its kit item — a kit that stopped
// providing the item, or one that no longer reads, leaves the selection
// nothing to plan for. Whether the item still holds the version it held
// when the action was saved the state does not record, so a version
// change alone keeps the action.
func (m *model) validPendingAction(key string, item *state.ItemPending) bool {
	installed := m.lk != nil && m.lk.Items[key] != nil
	if (item.Remove || item.Reset) && installed {
		return true
	}
	return item.Select && m.kitItem(key) != nil
}

// toKitRow moves the cursor to the kit row above the cursor (12.3).
func (m *model) toKitRow() {
	for i := m.cursor; i >= 0; i-- {
		if m.rows[i].Kind == KindKit {
			m.cursor = i
			return
		}
	}
}

// spaceUnderCursor toggles what space means on the row under the cursor:
// the collapse of a kit group, the default action of an item, nothing on
// the add-kit row (12.2-12.4).
func (m *model) spaceUnderCursor() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	switch r := m.rows[m.cursor]; r.Kind {
	case KindKit:
		m.collapseKit(r.Kit.Name, !r.Kit.Collapsed)
	case KindItem:
		item := r.Item
		if item.Pending != nil {
			return m.setPending(item.Key, func(it *state.ItemPending) { *it = state.ItemPending{} })
		}
		// 7.2: a major update cannot be selected without reviewing it.
		// The first space opens the details with the note, the second —
		// here or in the view — selects.
		if item.Major && !m.reviewed[item.Key] {
			m.reviewed[item.Key] = true
			m.openDetails()
			return nil
		}
		return m.spaceOnItem(item)
	}
	return nil
}

// spaceOnItem toggles the default action of the item's status (7.1); a set
// action clears (the footer names it "space unselect"). An item whose
// status has no default action stays untouched. A selection of an item
// with variables opens the form (7.3) — an update introducing a new one
// included; a reset selects nothing and opens none.
func (m *model) spaceOnItem(item ItemRow) tea.Cmd {
	if item.Pending != nil {
		return m.setPending(item.Key, func(it *state.ItemPending) { *it = state.ItemPending{} })
	}
	def, ok := DefaultAction(item.Status)
	if !ok {
		return nil
	}
	cmd := m.setPending(item.Key, func(it *state.ItemPending) {
		if def == PendingReset {
			it.Reset = true
		} else {
			it.Select = true
		}
	})
	if def != PendingReset {
		if c := m.openForm(item.Key); c != nil {
			cmd = c
		}
	}
	return cmd
}

// toggleRemove toggles removal on the row under the cursor (7.1): any
// installed item, including orphans and the items of unreadable kits.
func (m *model) toggleRemove() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	r := m.rows[m.cursor]
	if r.Kind != KindItem || !Removable(r.Item) {
		return nil
	}
	return m.setPending(r.Item.Key, func(it *state.ItemPending) { it.Remove = !it.Remove })
}

// toggleReset toggles reset on the row under the cursor (7.1): the states
// modified, conflict and broken name the key; the model keeps reset for
// any installed item (concept 7.6).
func (m *model) toggleReset() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	r := m.rows[m.cursor]
	if r.Kind != KindItem || !Resettable(r.Item) {
		return nil
	}
	return m.setPending(r.Item.Key, func(it *state.ItemPending) { it.Reset = !it.Reset })
}

// collapseKit sets one group's collapse state and rebuilds the tree.
func (m *model) collapseKit(name string, collapsed bool) {
	m.collapsed[name] = collapsed
	m.rebuildTree()
}

// collapseUnderCursor collapses or expands the kit row under the cursor
// (12.2).
func (m *model) collapseUnderCursor(collapsed bool) {
	if len(m.rows) == 0 {
		return
	}
	if r := m.rows[m.cursor]; r.Kind == KindKit {
		m.collapseKit(r.Kit.Name, collapsed)
	}
}

// setPending applies one pending-action mutation, saves it, and rebuilds
// what the mutation changes: the plans and the tree.
func (m *model) setPending(key string, fn func(*state.ItemPending)) tea.Cmd {
	if err := m.editPending(key, fn); err != nil {
		return m.showNotice(err.Error())
	}
	m.refresh()
	return nil
}

// editPending mutates one item's pending actions through a store update —
// the registry saves on every mutation — and reloads the in-memory copy.
func (m *model) editPending(key string, fn func(*state.ItemPending)) error {
	err := m.app.State.Update(func(reg *state.Registry) error {
		p := reg.Pending[m.target]
		if p == nil {
			p = &state.Pending{}
		}
		if p.Items == nil {
			p.Items = map[string]*state.ItemPending{}
		}
		it := p.Items[key]
		if it == nil {
			it = &state.ItemPending{}
		}
		fn(it)
		if !it.Select && !it.Reset && !it.Remove && len(it.Answers) == 0 {
			delete(p.Items, key)
		} else {
			p.Items[key] = it
		}
		if len(p.Items) == 0 && len(p.Deferred) == 0 {
			delete(reg.Pending, m.target)
		} else {
			reg.Pending[m.target] = p
		}
		return nil
	})
	if err != nil {
		return err
	}
	return m.reloadRegistry()
}

// openDetails opens the details view for the row under the cursor (5).
func (m *model) openDetails() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	switch r := m.rows[m.cursor]; r.Kind {
	case KindItem:
		m.view = true
		m.viewKit = false
		m.viewKey = r.Item.Key
	case KindKit:
		m.view = true
		m.viewKit = true
		m.viewKey = r.Kit.Name
	case KindAddKit:
		// 12.4: enter on the add-kit row opens the path input.
		return m.startAddKit("")
	default:
		return nil
	}
	m.viewOffset = 0
	m.openViewLines()
	return nil
}

// openViewLines renders the view content of the row the view follows.
func (m *model) openViewLines() {
	if m.viewKit {
		m.viewLines = RenderKitDetails(m.kitDetails(m.viewKey), m.width, m.set)
		return
	}
	if r, ok := m.itemRowByKey(m.viewKey); ok {
		m.viewLines = RenderItemDetails(m.itemDetails(r), m.width, m.set)
	}
}

// attachInput is gone: the add flow of 6.2-6.5 lives in kits.go, on the
// state operations.

// attachedTo reports whether the attachment of kit to target is recorded.
func attachedTo(reg *state.Registry, kitName, target string) bool {
	for _, a := range reg.Attachments {
		if a.Kit == kitName && a.Target == target {
			return true
		}
	}
	return false
}

// enterTarget loads everything the tree of one target shows: the registry,
// the kits attached to it, the lock, and the snapshot. The lock is
// reconciled with the target before anything is shown or planned: the
// staged files of an interrupted apply leave the target — their removal is
// reported on the notice line with the items the interruption touched
// (11.2) — and the items whose recorded content the target holds modified
// or has lost take their statuses. The selection is then planned against
// the snapshot, which turns the conflict and warning outcomes of the
// planner into row statuses. Opening a target remembers it as the last
// active one (11). An empty directory is the first-run state: nothing
// loads. The command returned arms the notice's timer when one showed.
func (m *model) enterTarget(dir string) tea.Cmd {
	m.screen = screenList
	m.target = dir
	m.cursor = 0
	m.filtering = false
	m.filter.Reset()
	m.view = false
	m.viewPreview = false
	m.viewLines = nil
	m.viewOffset = 0
	m.inputKind = inputNone
	m.editName = ""
	m.editReadable = false
	m.editErr = ""
	m.targetMode = false
	m.pathInput.Reset()
	m.pathInput.Blur()
	m.targetInput.Reset()
	m.completions = nil
	m.preview = nil
	m.attach = nil
	m.dialog = nil
	m.detachName = ""
	m.list = nil
	m.reviewed = map[string]bool{}
	m.changelogCache = map[string][]ChangelogEntry{}
	m.errLine = ""
	m.clearNotice()

	if err := m.rememberTarget(dir); err != nil {
		m.errLine = err.Error()
	}
	reg, err := m.app.State.LoadRegistry()
	if err != nil {
		m.errLine = err.Error()
		return nil
	}
	m.reg = reg

	m.lk = nil
	m.snap = nil
	m.reconcil = nil
	m.kits = nil
	if dir == "" {
		m.refresh()
		return nil
	}

	m.lk, err = m.app.Locks.Load(dir)
	if err != nil {
		m.errLine = "lock: " + err.Error()
	}

	// The kits attached to the target, loaded where they still load; a kit
	// that fails stays out and the tree shows its group as unreadable
	// (6.6).
	for _, name := range reg.KitsFor(dir) {
		kk, ok := reg.Kits[name]
		if !ok {
			continue
		}
		k, err := kit.Load(kk.Dir)
		if err != nil {
			continue
		}
		m.kits = append(m.kits, k)
	}

	files, shared := m.snapshotPaths()
	m.snap, err = target.Read(dir, files, shared)
	if err != nil {
		m.errLine = "target: " + err.Error()
		m.snap = nil
	}
	m.reconcil = nil
	var notice tea.Cmd
	if m.snap != nil && m.lk != nil {
		rec, staged, err := apply.Reconcile(dir, m.lk, m.snap)
		if err != nil {
			m.errLine = "reconcile: " + err.Error()
		} else {
			m.reconcil = rec
			// Staged files found means an apply was interrupted here
			// (11.2); the notice names the items it touched.
			if len(staged) > 0 {
				notice = m.showNotice(reconcileNotice(rec, m.lk))
			}
		}
	}
	m.refresh()
	return notice
}

// reconcileNotice names what the reconciliation of an interrupted apply
// found (11.2): the items whose recorded content the interruption left
// modified or lost, each with the version its record holds.
func reconcileNotice(rec *plan.Reconciliation, lk *lock.Lock) string {
	seen := map[string]bool{}
	var keys []string
	for key := range rec.Modified {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for key := range rec.Broken {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "Reconciled after an interrupted apply."
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		version := ""
		if rec := lk.Items[key]; rec != nil {
			version = rec.Version
		}
		_, id, _ := strings.Cut(key, "/")
		if id == "" {
			id = key
		}
		parts = append(parts, fmt.Sprintf("%s recorded as %s", id, version))
	}
	return "Reconciled after an interrupted apply: " + strings.Join(parts, ", ") + "."
}

// rememberTarget saves the target as the last active one (11).
func (m *model) rememberTarget(dir string) error {
	if m.reg != nil && m.reg.LastTarget == dir {
		return nil
	}
	return m.app.State.Update(func(reg *state.Registry) error {
		if _, ok := reg.Targets[dir]; !ok {
			return nil
		}
		reg.LastTarget = dir
		return nil
	})
}

// snapshotPaths assembles the paths the snapshot covers: the paths the
// lock records and every loaded contribution target, so the planning and a
// later selection toggle both see the target as it stands.
func (m *model) snapshotPaths() (files, shared []string) {
	if m.lk != nil {
		files, shared = m.lk.RecordedPaths()
	}
	seenFile := map[string]bool{}
	for _, p := range files {
		seenFile[p] = true
	}
	seenShared := map[string]bool{}
	for _, p := range shared {
		seenShared[p] = true
	}
	for _, k := range m.kits {
		for _, it := range k.Items {
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

// refresh re-plans the selection and rebuilds the tree; every pending
// mutation goes through it.
func (m *model) refresh() {
	m.rebuildPlans()
	m.rebuildTree()
}

// rebuildTree builds the rows of the body from the values the target load
// and the planning produced.
func (m *model) rebuildTree() {
	m.rows = BuildTree(TreeInput{
		Target:    m.target,
		Registry:  m.reg,
		Lock:      m.lk,
		Snapshot:  m.snap,
		Kits:      m.kits,
		Plans:     m.plans,
		Collapsed: m.collapsed,
		Filter:    m.filter.Value(),
	})
	m.clampCursor()
}

func (m *model) clampCursor() {
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// rebuildPlans plans the pending selection the way an apply would, without
// writing anything, so the planner's conflict and warning verdicts become
// row statuses.
func (m *model) rebuildPlans() {
	m.plans = nil
	var selection []string
	if pending := m.pendingActions(); pending != nil {
		for key, item := range pending.Items {
			if item.Select && !item.Remove {
				selection = append(selection, key)
			}
		}
	}
	sort.Strings(selection)
	if len(selection) == 0 || m.snap == nil || len(m.kits) == 0 {
		return
	}
	sources, err := kit.ReadSources(m.kits, selection)
	if err != nil {
		// A source the kit no longer provides is a kit problem the apply
		// reports; the tree simply shows no planned statuses.
		return
	}
	p := plan.Build(plan.Input{
		Kits:       m.kits,
		Selection:  selection,
		Lock:       m.lk,
		Snapshot:   m.snap,
		Sources:    sources,
		Answers:    m.currentAnswers(),
		Reconciled: m.reconcil,
	})
	m.plans = map[string]*plan.ItemPlan{}
	for _, ip := range p.Items {
		m.plans[ip.Key] = ip
	}
}

// pendingActions is the pending object of the current target, nil when the
// target holds none.
func (m *model) pendingActions() *state.Pending {
	return m.pendingActionsOf(m.target)
}

// pendingActionsOf is the pending object of one target, nil when it holds
// none.
func (m *model) pendingActionsOf(t string) *state.Pending {
	if m.reg == nil {
		return nil
	}
	return m.reg.Pending[t]
}

// currentAnswers is the answer set the planning uses: the answers given at
// selection time, held in the pending actions. They win over the ones
// recorded in the lock, as plan answers do.
func (m *model) currentAnswers() map[string]map[string]string {
	out := map[string]map[string]string{}
	if pending := m.pendingActions(); pending != nil {
		for key, item := range pending.Items {
			if len(item.Answers) > 0 {
				out[key] = item.Answers
			}
		}
	}
	return out
}

// answersFor is the answer set of one item: the answers held in the
// pending actions; the Variables section falls back to the lock's record
// per variable.
func (m *model) answersFor(key string) map[string]string {
	return m.currentAnswers()[key]
}

func (m *model) reloadRegistry() error {
	reg, err := m.app.State.LoadRegistry()
	if err != nil {
		return err
	}
	m.reg = reg
	return nil
}

// itemRowByKey finds an item row among the built rows by its key.
func (m *model) itemRowByKey(key string) (ItemRow, bool) {
	for _, r := range m.rows {
		if r.Kind == KindItem && r.Item.Key == key {
			return r.Item, true
		}
	}
	return ItemRow{}, false
}

// kitItem finds the kit item behind an item key, or nil for the rows
// whose kit does not load or is not attached.
func (m *model) kitItem(key string) *kit.Item {
	kitName, itemID, ok := strings.Cut(key, "/")
	if !ok {
		return nil
	}
	for _, k := range m.kits {
		if k.Name != kitName {
			continue
		}
		for _, it := range k.Items {
			if it.ID == itemID {
				return it
			}
		}
	}
	return nil
}

// itemDetails gathers the values one item's details render from (5.1).
func (m *model) itemDetails(r ItemRow) ItemDetails {
	d := ItemDetails{
		Row:     r,
		Item:    m.kitItem(r.Key),
		Plan:    m.plans[r.Key],
		Lock:    m.lk,
		Answers: m.answersFor(r.Key),
	}
	if d.Item != nil {
		d.Changelog = m.changelogOf(d.Item)
	}
	return d
}

// kitDetails gathers the values one kit group's details render from (5.2).
func (m *model) kitDetails(name string) KitDetails {
	for _, r := range m.rows {
		if r.Kind == KindKit && r.Kit.Name == name {
			var k *kit.Kit
			for _, kk := range m.kits {
				if kk.Name == name {
					k = kk
					break
				}
			}
			return KitDetails{Row: r.Kit, Kit: k, Registry: m.reg, Target: m.target}
		}
	}
	return KitDetails{Row: KitRow{Name: name}, Registry: m.reg, Target: m.target}
}

// changelogOf reads an item's CHANGELOG.md, cached per target entry; the
// renderers perform no I/O, so the screen reads it (5.1).
func (m *model) changelogOf(it *kit.Item) []ChangelogEntry {
	if !it.HasChangelog {
		return nil
	}
	if e, ok := m.changelogCache[it.Dir]; ok {
		return e
	}
	e := readChangelog(filepath.Join(it.Dir, "CHANGELOG.md"))
	m.changelogCache[it.Dir] = e
	return e
}

// readChangelog parses an item changelog: a `## version` heading starts an
// entry, the lines under it join into its note.
func readChangelog(path string) []ChangelogEntry {
	data, err := os.ReadFile(path) //nolint:gosec // the path is built from the registered kit directory
	if err != nil {
		return nil
	}
	var out []ChangelogEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			out = append(out, ChangelogEntry{Version: strings.TrimSpace(line[3:])})
			continue
		}
		if len(out) == 0 {
			continue
		}
		e := &out[len(out)-1]
		if e.Note == "" {
			e.Note = line
		} else {
			e.Note += " " + line
		}
	}
	return out
}

// View renders the screen of the model's state through the layout of the
// current size.
func (m *model) View() string {
	l := NewLayout(m.width, m.height)
	if l.Mode == ModeTooSmall {
		return l.Render(Frame{})
	}
	fl := fullWidthLayout(l)
	switch m.screen {
	case screenSummary:
		return fl.Render(m.summaryFrame(fl.Width))
	case screenProgress:
		return fl.Render(m.progressFrame(fl.Width))
	case screenResults:
		return fl.Render(m.resultsFrame(fl.Width))
	}
	// The target list spans the width like the summary does (8.1): the
	// box sits under the header, no details pane takes its share.
	if m.list != nil {
		return fl.Render(m.listFrame(fl))
	}
	// The command palette spans the width the same way (9): its box sits
	// under the header, centered like the target list's.
	if m.palette != nil || m.kitPick != nil {
		return fl.Render(m.paletteFrame(fl))
	}
	return l.Render(m.listFrame(l))
}

// fullWidthLayout lays a non-tree screen out: the rows of the summary and
// the results are prose across the whole width, not the tree, so no
// details pane takes its share (2.1).
func fullWidthLayout(l Layout) Layout {
	return Layout{Width: l.Width, Height: l.Height, Mode: ModeMedium, TreeWidth: l.Width, Columns: Columns{Tags: true, Description: true}}
}

// listFrame assembles the main screen: header, tree rows with the cursor
// row marked, the details pane beside them on wide terminals, the filter
// input and notice lines, and the footer of the row under the cursor.
func (m *model) listFrame(l Layout) Frame {
	f := Frame{Header: m.header(l.Width)}
	// The modal dialog replaces the body while it is open: the box
	// centered in the width, its keys in the footer (6.3, 6.7).
	if m.dialog != nil {
		f.Rows = m.dialog.lines(l.Width, m.set)
		f.Footer = dialogKeys
		return f
	}
	// The target list takes the body the same way (8.1): its keys render
	// inside the box, the footer stays empty.
	if m.list != nil {
		f.Rows = m.list.lines(l.Width, m.set)
		f.Cursor = m.list.cursor + 2 // the rows start under the top border and the search line
		return f
	}
	lines := RenderTree(m.rows, l, m.set)
	// The path input takes its row over: the add-kit label or the kit's
	// mark and name, then the input (6.2, 6.6 mockups).
	switch m.inputKind {
	case inputAddKit:
		if len(lines) > 0 {
			lines[len(lines)-1] = "  " + AddKitLabel + "  " + m.pathInput.View()
		}
	case inputEditKit:
		if len(lines) > 0 && m.cursor < len(lines) {
			mk := expandedMark
			if m.rows[m.cursor].Kit.Collapsed {
				mk = collapsedMark
			}
			lines[m.cursor] = "  " + mk.glyph(m.set) + " " + m.editName + "  " + m.pathInput.View()
		}
	}
	if m.firstRun() {
		lines = append(m.firstRunLines(l.Width), lines...)
	}
	cursor := m.cursor
	if m.firstRun() {
		cursor += len(m.firstRunLines(l.Width))
	}
	// The completion dropdown sits under the input row (6.2 mockup).
	if drop := m.completionLines(); len(drop) > 0 && cursor+1 <= len(lines) {
		lines = append(lines, make([]string, len(drop))...)
		copy(lines[cursor+1+len(drop):], lines[cursor+1:])
		copy(lines[cursor+1:], drop)
	}
	for i := range lines {
		if i == cursor {
			lines[i] = cursorLine(lines[i], m.set)
		}
	}
	f.Rows = lines
	f.Cursor = cursor
	// The form shows in the details view, and over any other width when no
	// pane carries it: the lines render live from the draft, the window
	// clips them (5, 7.3). The preview view of 6.2 renders the same way.
	if m.view || (m.formOpen && l.Mode != ModeWide) {
		h := l.Height - headerLines - footerLines
		if m.noticeLine() != "" {
			h--
		}
		lines := m.viewLines
		switch {
		case m.viewPreview:
			lines = m.previewLines(m.width)
		case m.formOpen:
			lines = RenderItemForm(m.formDetails(), m.width, m.set)
		}
		f.Rows = ScrollWindow(lines, m.viewOffset, h)
		f.Cursor = 0
	}
	// The full help of 12.5 takes the body the way the details view does:
	// its lines scroll, the footer names its keys.
	if m.helpOpen {
		h := l.Height - headerLines - footerLines
		if m.noticeLine() != "" {
			h--
		}
		f.Rows = ScrollWindow(m.helpLines, m.helpOffset, h)
		f.Cursor = 0
	}
	f.Details = m.detailsPane(l)
	f.Input = m.inputLine()
	f.Notice = m.noticeLine()
	f.Footer = m.footer(l.Width)
	return f
}

// firstRun reports the state of 6.1: the tree holds nothing but the
// add-kit row, and the body explains what a kit is.
func (m *model) firstRun() bool {
	return len(m.rows) == 1 && m.rows[0].Kind == KindAddKit
}

// firstRunLines is the explanation the body shows on the first run (6.1).
func (m *model) firstRunLines(width int) []string {
	return []string{
		"",
		center("No kits are registered yet.", width),
		"",
		center("A kit is a directory with kit.yaml. Add one to start managing", width),
		center("items in a harness configuration directory.", width),
		"",
	}
}

// center pads a line of prose to the middle of the width.
func center(s string, width int) string {
	pad := (width - len(s)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + s
}

// cursorLine marks the cursor row: the first cell of the line becomes the
// cursor bar (2.3).
func cursorLine(line string, set Set) string {
	bar := cursorMark.glyph(set)
	r := []rune(line)
	if len(r) == 0 {
		return bar
	}
	return bar + string(r[1:])
}

// cursorMark is the bar of the cursor row (2.3); the ASCII set spells it
// in a chevron, as it spells the expansion marks.
var cursorMark = mark{unicode: "▌", ascii: ">", role: roleDim}

// header is the fixed header line (2.1): the application name and target
// on the left, the pending count and the help hint on the right. The
// target carries the arrow that opens the target list — down while the
// list is closed, up while it shows (8.1) — and its path shortens from
// the left when it does not fit (4.3).
func (m *model) header(width int) string {
	right := ""
	if n := m.pendingCount(); n > 0 {
		right = fmt.Sprintf("%d pending · ", n)
	}
	right += "? help"
	label := "no target"
	arrow := ""
	if m.target != "" {
		label = m.targetLabel()
		arrow = " " + m.targetArrowMark().glyph(m.set)
	}
	avail := width - ansi.StringWidth(right) - len(headerName) - ansi.StringWidth(arrow)
	if w := ansi.StringWidth(label); w > avail {
		label = ShortenLeft(label, avail)
	}
	return JoinSides(headerName+label+arrow, right, width)
}

// targetArrowMark is the mark after the target in the header (8.1).
func (m *model) targetArrowMark() mark {
	if m.list != nil {
		return targetOpenMark
	}
	return targetClosedMark
}

// headerName is the application name and the gap before the target.
const headerName = "penates  "

// targetLabel is the display name of the target when it has one, the path
// otherwise.
func (m *model) targetLabel() string {
	if m.reg != nil {
		if t, ok := m.reg.Targets[m.target]; ok && t.Name != "" {
			return t.Name
		}
	}
	return m.target
}

// pendingCount counts the item actions waiting for the apply.
func (m *model) pendingCount() int {
	p := m.pendingActions()
	if p == nil {
		return 0
	}
	n := 0
	for _, item := range p.Items {
		if item.Select || item.Reset || item.Remove {
			n++
		}
	}
	return n
}

// noticeLine is the text of the notice line: the transient notice, or the
// load problem that stays until the next successful reload.
func (m *model) noticeLine() string {
	if m.notice.Visible() {
		return m.notice.Text()
	}
	return m.errLine
}

// inputLine is the filter input line while it is open (7.5).
func (m *model) inputLine() string {
	if m.filtering {
		return " / " + m.filter.View()
	}
	return ""
}

// footer names the keys of the row under the cursor, then the global keys
// (2.1, 12); the open form names its own.
func (m *model) footer(width int) string {
	if m.formOpen {
		return formFooter
	}
	if m.inputKind == inputAddKit {
		if m.targetMode {
			return targetKeys
		}
		return addKitKeys
	}
	if m.inputKind == inputEditKit {
		return editKitKeys
	}
	if m.view {
		keys := "esc close · ↑↓ scroll"
		if !m.viewKit && !m.viewPreview {
			if r, ok := m.itemRowByKey(m.viewKey); ok && r.Major {
				keys += " · space select"
			}
		}
		return keys
	}
	if m.helpOpen {
		return "esc close · ↑↓ scroll"
	}
	if m.filtering {
		return "enter keep filter · esc clear"
	}
	return JoinSides(m.rowKeys(), "q quit", width)
}

// rowKeys names the keys the row under the cursor answers (12.2-12.4),
// followed by the global keys of the tree.
func (m *model) rowKeys() string {
	var keys []string
	if len(m.rows) > 0 {
		switch r := m.rows[m.cursor]; r.Kind {
		case KindKit:
			if r.Kit.Collapsed {
				keys = append(keys, "space expand")
			} else {
				keys = append(keys, "space collapse")
			}
			keys = append(keys, "enter details")
			if !r.Kit.Orphans {
				keys = append(keys, "e edit path")
			}
			if !r.Kit.Orphans && !r.Kit.Detaching {
				keys = append(keys, "d detach")
			}
		case KindItem:
			item := r.Item
			if item.Pending != nil {
				keys = append(keys, "space unselect")
			} else if def, ok := DefaultAction(item.Status); ok {
				keys = append(keys, "space "+defaultVerb(def, item.Status))
			}
			if Removable(item) {
				keys = append(keys, "d remove")
			}
			if Resettable(item) {
				keys = append(keys, "r reset")
			}
			keys = append(keys, "enter details")
		case KindAddKit:
			keys = append(keys, "enter add kit")
		}
	}
	keys = append(keys, "/ filter", "a apply", "t targets")
	return strings.Join(keys, " · ")
}

// defaultVerb names what the space key would do for the status (7.1).
func defaultVerb(p Pending, s Status) string {
	if p == PendingReset {
		return "reset"
	}
	switch s {
	case StatusUpdate:
		return "update"
	case StatusDowngrade:
		return "downgrade"
	}
	return "install"
}

// detailsPane is the details content of the row under the cursor on wide
// terminals (2.2); the view, the help and the form leave the pane out, and
// the open form or the preview takes the pane's place (7.3, 6.2).
func (m *model) detailsPane(l Layout) []string {
	if l.Mode != ModeWide || m.view || m.helpOpen || len(m.rows) == 0 {
		return nil
	}
	if m.formOpen {
		return RenderItemForm(m.formDetails(), l.DetailsWidth, m.set)
	}
	switch m.inputKind {
	case inputAddKit:
		return m.addPreviewLines(l.DetailsWidth)
	case inputEditKit:
		return m.editPreviewLines(l.DetailsWidth)
	}
	switch r := m.rows[m.cursor]; r.Kind {
	case KindItem:
		return RenderItemDetails(m.itemDetails(r.Item), l.DetailsWidth, m.set)
	case KindKit:
		return RenderKitDetails(m.kitDetails(r.Kit.Name), l.DetailsWidth, m.set)
	}
	return nil
}

// viewportHeight is the height the body rows get at the current size: the
// height under the fixed chrome, minus the lines the notice and the input
// take (2.1).
func (m *model) viewportHeight() int {
	v := m.height - headerLines - footerLines
	if m.noticeLine() != "" {
		v--
	}
	if m.inputLine() != "" {
		v--
	}
	if v < 1 {
		v = 1
	}
	return v
}

// summaryFrame is the apply summary the enter key confirms (10.1): the
// header with the item, write and exclusion counts, the verb groups with
// their path marks, and the footer that names the keys — or reads "nothing
// to apply" when every pending item is excluded.
func (m *model) summaryFrame(width int) Frame {
	f := Frame{
		Header: JoinSides("penates  Apply to "+m.targetLabel(), m.summaryCounts(), width),
		Cursor: m.summaryOffset,
	}
	if m.applySummary == nil {
		return f
	}
	f.Rows = renderSummary(*m.applySummary, width, m.set)
	f.Notice = m.noticeLine()
	if m.applySummary.applicable() {
		f.Footer = "enter apply · ↑↓ scroll · esc back"
	} else {
		f.Footer = "nothing to apply"
	}
	return f
}

// progressFrame is the running apply (10.2): the header counts the steps
// that finished, the body holds one line per step with the spinner on the
// one that runs.
func (m *model) progressFrame(width int) Frame {
	done := 0
	for _, st := range m.progress {
		if st.state == stepDone || st.state == stepFailed {
			done++
		}
	}
	return Frame{
		Header: JoinSides("penates  Applying to "+m.targetLabel(), fmt.Sprintf("%d of %d", done, len(m.progress)), width),
		Rows:   renderProgress(m.progress, m.spinner, m.set),
		// The notice line carries the armed ctrl+c of 12.1 while the
		// items apply.
		Notice: m.noticeLine(),
	}
}

// resultsFrame is the report of the last apply (10.3). A run that stopped
// before any step shows its failure instead of the counts.
func (m *model) resultsFrame(width int) Frame {
	f := Frame{
		Header: "penates  Applied to " + m.targetLabel(),
		Footer: "enter back to the list",
	}
	if m.outcome == nil {
		f.Rows = []string{"nothing selected"}
		return f
	}
	if m.outcome.err == "" {
		f.Header = JoinSides(f.Header, m.resultCounts(), width)
	}
	f.Rows = renderResults(m.outcome.err, m.outcome.steps, m.outcome.deferred, m.outcome.errs, m.set)
	f.Notice = m.noticeLine()
	return f
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if len(p) == 1 {
		return home
	}
	return filepath.Join(home, p[1:])
}
