// Package tui is the terminal client: registration, item list, variable forms, and
// apply summary. It is the only package that knows about the terminal.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dpleshakov/penates/internal/apply"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// App carries the wiring the screens work through: the state store and the lock
// store of one penates data directory.
type App struct {
	State *state.Store
	Locks *lock.Store
}

// NewApp wires the stores.
func NewApp(st *state.Store, locks *lock.Store) *App {
	return &App{State: st, Locks: locks}
}

// Initial builds the first screen: registering kitPath when it is given, otherwise
// the single — or chosen — registered target.
func (a *App) Initial(kitPath string) tea.Model {
	m := &model{app: a, input: textinput.New(), filter: textinput.New()}
	m.input.CharLimit = 0

	if kitPath != "" {
		k, err := kit.Load(kitPath)
		if err != nil {
			return fatalModel(err)
		}
		st, err := a.State.Load()
		if err != nil {
			return fatalModel(err)
		}
		if t, ok := st.KitTarget(k.Name); ok {
			m.enterList(t)
			return m
		}
		m.pendingKit = k
		m.input.Placeholder = "target directory"
		m.input.SetValue(expandHome(k.TargetHint))
		m.input.Focus()
		m.screen = screenRegister
		return m
	}

	st, err := a.State.Load()
	if err != nil {
		return fatalModel(err)
	}
	switch len(st.Targets) {
	case 0:
		m.screen = screenUsage
	case 1:
		m.enterList(st.Targets[0])
	default:
		m.targets = st.Targets
		m.screen = screenPicker
	}
	return m
}

type screen int

const (
	screenUsage screen = iota
	screenRegister
	screenPicker
	screenList
	screenForm
	screenDetails
	screenSummary
	screenResults
)

// row is one item line in the list.
type row struct {
	key              string
	kitName          string
	kitDir           string
	version          string
	description      string
	tags             []string
	installed        bool
	installedVersion string
	update           bool
	downgrade        bool
	orphan           bool
	unavailable      bool
	modified         bool
	broken           bool
	conflict         bool
	conflictReason   string
	warning          bool
	// major marks an update across the major version: major updates are
	// behavior changes the user reviews, so the marker takes the error style.
	major    bool
	selected bool
	reset    bool
	remove   bool
}

// statusWord is the bracketed status the list renders for the row: the most
// specific of the states it holds. The order is the precedence — a planned
// conflict outranks all, the reconciliation and planner verdicts (broken,
// warning, modified) outrank the version markers, and the frozen statuses
// outrank selection.
func (r row) statusWord() string {
	switch {
	case r.conflict:
		return "conflict"
	case r.broken:
		return "broken"
	case r.warning:
		return "warning"
	case r.modified:
		return "modified"
	case r.downgrade:
		return "downgrade"
	case r.update:
		return "update"
	case r.orphan:
		return "orphan"
	case r.unavailable:
		return "unavailable"
	case r.installed:
		return "installed"
	case r.selected:
		return "selected"
	}
	return " "
}

// matches reports whether the row survives a filter: a case-insensitive
// substring over the id, the tags, and the description.
func (r row) matches(filter string) bool {
	if filter == "" {
		return true
	}
	f := strings.ToLower(filter)
	if strings.Contains(strings.ToLower(r.key), f) || strings.Contains(strings.ToLower(r.description), f) {
		return true
	}
	for _, tag := range r.tags {
		if strings.Contains(strings.ToLower(tag), f) {
			return true
		}
	}
	return false
}

// group is one kit block of the list: the header line and the indices of its
// rows. Kits are listed in registration order; the items of a kit that did
// not load and the installed items of a kit that is not registered keep
// their groups, so their frozen statuses stay visible.
type group struct {
	kitName string
	kitDir  string
	// note is rendered under the header: why the group holds no kit —
	// a load error, or a kit that is not registered.
	note    string
	noteErr bool
	rows    []int
}

// formField is one missing required variable the variable form asks for.
type formField struct {
	key    string
	varID  string
	prompt string
}

// pendingApply is one apply waiting on the summary's confirmation: the
// selection and removals it was built from, and the paths its planning
// touches.
type pendingApply struct {
	selection   []string
	removals    []string
	paths       []string
	sharedPaths []string
}

type model struct {
	app     *App
	screen  screen
	message string

	pendingKit *kit.Kit
	input      textinput.Model

	targets  []string
	target   string
	kits     []*kit.Kit
	kitErrs  []string
	lk       *lock.Lock
	snap     *target.Snapshot
	reconcil *plan.Reconciliation
	rows     []row
	groups   []group
	cursor   int
	resets   map[string]bool
	planned  map[string]*plan.ItemPlan

	filter      textinput.Model
	filtering   bool
	detailKey   string
	formFields  []formField
	formCursor  int
	answers     map[string]map[string]string
	lastResults string
	pending     *pendingApply
	summary     string
}

func fatalModel(err error) tea.Model {
	return &model{screen: screenUsage, message: "error: " + err.Error()}
}

func (m *model) Init() tea.Cmd {
	if m.screen == screenRegister || m.screen == screenForm {
		return textinput.Blink
	}
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch m.screen {
		case screenUsage:
			if msg.String() == "q" || msg.String() == "esc" {
				return m, tea.Quit
			}
		case screenResults:
			if msg.String() == "q" {
				return m, tea.Quit
			}
			m.enterList(m.target)
		case screenRegister:
			return m.updateRegister(msg)
		case screenPicker:
			return m.updatePicker(msg)
		case screenList:
			return m.updateList(msg)
		case screenForm:
			return m.updateForm(msg)
		case screenDetails:
			if msg.String() == "esc" || msg.String() == "i" || msg.String() == "q" {
				m.screen = screenList
			}
		case screenSummary:
			switch msg.String() {
			case "enter", "y":
				if m.pending != nil {
					m.performApply(m.pending)
				}
			case "esc", "n", "q":
				m.screen = screenList
			}
		}
	case tea.WindowSizeMsg:
		// Rendering is line-based; no size-dependent layout yet.
	}
	return m, nil
}

func (m *model) updateRegister(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.screen = screenUsage
		return m, nil
	}
	if msg.String() == "enter" {
		abs, err := filepath.Abs(expandHome(m.input.Value()))
		if err != nil {
			m.message = "error: " + err.Error()
			return m, nil
		}
		st, err := m.app.State.Load()
		if err != nil {
			m.message = "error: " + err.Error()
			return m, nil
		}
		st.RegisterKit(m.pendingKit.Name, m.pendingKit.Dir, abs)
		if err := m.app.State.Save(st); err != nil {
			m.message = "error: " + err.Error()
			return m, nil
		}
		m.enterList(abs)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.targets)-1 {
			m.cursor++
		}
	case "enter":
		m.enterList(m.targets[m.cursor])
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

// visible lists the indices of the rows the current filter lets through, in
// list order. The cursor moves over these.
func (m *model) visible() []int {
	filter := m.filter.Value()
	var out []int
	for _, g := range m.groups {
		for _, idx := range g.rows {
			if m.rows[idx].matches(filter) {
				out = append(out, idx)
			}
		}
	}
	return out
}

func (m *model) clampCursor() {
	vis := m.visible()
	if len(vis) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(vis) {
		m.cursor = len(vis) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "enter":
			m.filtering = false
			m.clampCursor()
			return m, nil
		case "esc":
			m.filtering = false
			m.filter.Reset()
			m.clampCursor()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.clampCursor()
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.visible())-1 {
			m.cursor++
		}
	case "/":
		// The filter takes over the keyboard: while it is open, q and a
		// type into it instead of quitting and applying.
		m.filtering = true
		m.filter.Reset()
		return m, m.filter.Focus()
	case " ", "enter":
		if vis := m.visible(); len(vis) > 0 {
			idx := vis[m.cursor]
			m.rows[idx].selected = !m.rows[idx].selected
			m.refreshPlanStatuses()
		}
	case "r":
		// Reset to kit is only ever a user request: it toggles on
		// the row under the cursor, applies once, and clears. It changes
		// what planning would decide, so the statuses follow.
		if vis := m.visible(); len(vis) > 0 && m.rows[vis[m.cursor]].installed {
			idx := vis[m.cursor]
			key := m.rows[idx].key
			if m.resets == nil {
				m.resets = map[string]bool{}
			}
			if m.resets[key] {
				delete(m.resets, key)
				m.rows[idx].reset = false
			} else {
				m.resets[key] = true
				m.rows[idx].reset = true
			}
			m.refreshPlanStatuses()
		}
	case "d":
		// Removal is only ever a user request: it toggles on the
		// installed row under the cursor and applies once.
		if vis := m.visible(); len(vis) > 0 && m.rows[vis[m.cursor]].installed {
			idx := vis[m.cursor]
			m.rows[idx].remove = !m.rows[idx].remove
		}
	case "i":
		if vis := m.visible(); len(vis) > 0 {
			m.detailKey = m.rows[vis[m.cursor]].key
			m.screen = screenDetails
		}
	case "a":
		m.beginApply()
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

// updateForm feeds the current field; enter records the answer and moves on,
// and the last one resumes the apply the form interrupted.
func (m *model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.screen = screenList
		return m, nil
	}
	if msg.String() == "enter" {
		f := m.formFields[m.formCursor]
		if m.answers == nil {
			m.answers = map[string]map[string]string{}
		}
		if m.answers[f.key] == nil {
			m.answers[f.key] = map[string]string{}
		}
		m.answers[f.key][f.varID] = strings.TrimSpace(m.input.Value())
		m.formCursor++
		if m.formCursor < len(m.formFields) {
			m.input.Reset()
			return m, textinput.Blink
		}
		if m.pending == nil {
			m.screen = screenList
			return m, nil
		}
		m.buildSummary(m.pending)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// enterList loads everything the item list shows: the kits registered for the
// target, the target's lock, and the previous selection. The lock is
// reconciled with the target before anything is shown or planned:
// the staged files of an interrupted apply leave the target, and the items
// whose recorded content the target holds modified or has lost take their
// statuses. Rows are grouped under their kit's header; the items of a kit
// that no longer loads and the installed items of a kit that is not
// registered keep their groups and their frozen statuses. The
// current selection is then planned against the snapshot, which turns the
// conflict and warning outcomes of the planner into row statuses.
func (m *model) enterList(dir string) {
	m.message = ""
	m.target = dir
	m.cursor = 0
	m.screen = screenList
	m.filtering = false
	m.filter.Reset()

	m.kits, m.kitErrs, m.reconcil, m.planned = nil, nil, nil, nil
	m.rows, m.groups = nil, nil
	lk, err := m.app.Locks.Load(dir)
	if err != nil {
		m.kitErrs = append(m.kitErrs, "lock: "+err.Error())
	}
	m.lk = lk

	st, err := m.app.State.Load()
	if err != nil {
		m.kitErrs = append(m.kitErrs, "state: "+err.Error())
		return
	}

	// The kits registered for the target, loaded where they still load.
	names := st.KitsForTarget(dir)
	failed := map[string]string{}
	for _, name := range names {
		k, err := kit.Load(st.Kits[name])
		if err != nil {
			failed[name] = err.Error()
			continue
		}
		m.kits = append(m.kits, k)
	}

	// The snapshot covers the paths the lock records and every loaded
	// contribution target, so the planning below and a later selection
	// toggle both see the target as it stands.
	var files, shared []string
	if lk != nil {
		files, shared = lk.RecordedPaths()
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

	var snap *target.Snapshot
	if snap, err = target.Read(dir, files, shared); err != nil {
		m.kitErrs = append(m.kitErrs, "target: "+err.Error())
	} else if lk != nil {
		if rec, err := apply.Reconcile(dir, lk, snap); err != nil {
			m.kitErrs = append(m.kitErrs, "reconcile: "+err.Error())
		} else {
			m.reconcil = rec
		}
	}
	m.snap = snap

	selected := map[string]bool{}
	for _, key := range st.SelectionFor(dir) {
		selected[key] = true
	}

	// One group per registered kit, in registration order, with a row per
	// item the kit provides.
	covered := map[string]bool{}
	next := 0
	for _, name := range names {
		if reason, ok := failed[name]; ok {
			m.groups = append(m.groups, group{kitName: name, note: "kit did not load: " + reason, noteErr: true})
			continue
		}
		k := m.kits[next]
		next++
		g := group{kitName: k.Name, kitDir: k.Dir}
		for _, it := range k.Items {
			key := kit.ItemKey(k.Name, it.ID)
			covered[key] = true
			g.rows = append(g.rows, len(m.rows))
			m.rows = append(m.rows, m.kitRow(k, it, selected))
		}
		m.groups = append(m.groups, g)
	}

	// Installed items no loaded kit provides stay managed and frozen: the
	// list shows them under their kit's name so the user can remove them.
	// An installed item of a kit that failed to load is shown as
	// kit unavailable; one whose kit is not registered at all, or that a
	// loaded kit no longer provides, is an orphan.
	if lk != nil {
		keys := make([]string, 0, len(lk.Items))
		for key := range lk.Items {
			if !covered[key] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			rec := lk.Items[key]
			kitName, _, _ := strings.Cut(key, "/")
			gi := -1
			for i := range m.groups {
				if m.groups[i].kitName == kitName {
					gi = i
					break
				}
			}
			if gi < 0 {
				gi = len(m.groups)
				m.groups = append(m.groups, group{kitName: kitName, note: "kit not registered"})
			}
			r := row{
				key:              key,
				kitName:          kitName,
				version:          rec.Version,
				installed:        true,
				installedVersion: rec.Version,
				selected:         selected[key],
				modified:         m.reconcil.ModifiedItem(key),
				broken:           m.reconcil.BrokenItem(key),
			}
			switch {
			case failed[kitName] != "":
				// The kit is registered but no longer loads: the item is
				// frozen as kit unavailable.
				r.unavailable = true
				r.description = "kit did not load"
			case m.groups[gi].kitDir != "":
				// The kit loads but no longer provides the item.
				r.orphan = true
				r.description = "removed from the kit"
			default:
				// No kit of this name is registered.
				r.orphan = true
				r.description = "kit is not registered"
			}
			m.groups[gi].rows = append(m.groups[gi].rows, len(m.rows))
			m.rows = append(m.rows, r)
		}
	}

	m.refreshPlanStatuses()
}

// kitRow builds the list row of one item a loaded kit provides: the kit
// facts, the lock record, and the reconciliation and version-table statuses
// the lock and the kit version decide.
func (m *model) kitRow(k *kit.Kit, it *kit.Item, selected map[string]bool) row {
	key := kit.ItemKey(k.Name, it.ID)
	r := row{
		key:         key,
		kitName:     k.Name,
		kitDir:      k.Dir,
		version:     it.Version,
		description: it.Description,
		tags:        it.Tags,
		selected:    selected[key],
	}
	rec := m.lk.Items[key]
	if m.lk == nil || rec == nil {
		return r
	}
	r.installed = true
	r.installedVersion = rec.Version
	r.reset = m.resets[key]
	r.modified = m.reconcil.ModifiedItem(key)
	r.broken = m.reconcil.BrokenItem(key)
	if it.Version != rec.Version && kit.MajorVersion(it.Version) != kit.MajorVersion(rec.Version) {
		r.major = true
	}
	switch kit.CompareVersions(it.Version, rec.Version) {
	case 1:
		r.update = true
	case -1:
		r.downgrade = true
	}
	return r
}

// refreshPlanStatuses plans the current selection against the snapshot the
// way an apply would, without writing anything, and lifts the planner's
// conflict and warning verdicts onto the rows. Rows whose
// selection was toggled since the last refresh take their new statuses here.
func (m *model) refreshPlanStatuses() {
	m.planned = nil
	for i := range m.rows {
		m.rows[i].conflict = false
		m.rows[i].conflictReason = ""
		m.rows[i].warning = false
	}
	var selection []string
	for i := range m.rows {
		if m.rows[i].selected {
			selection = append(selection, m.rows[i].key)
		}
	}
	if len(selection) == 0 || m.snap == nil || len(m.kits) == 0 {
		return
	}
	sources, err := kit.ReadSources(m.kits, selection)
	if err != nil {
		// A source the kit no longer provides is a kit problem the apply
		// reports; the list simply shows no planned statuses.
		return
	}
	p := plan.Build(plan.Input{
		Kits:       m.kits,
		Selection:  selection,
		Lock:       m.lk,
		Snapshot:   m.snap,
		Sources:    sources,
		Answers:    m.answers,
		Reconciled: m.reconcil,
	})
	m.planned = map[string]*plan.ItemPlan{}
	for _, ip := range p.Items {
		m.planned[ip.Key] = ip
	}
	for i := range m.rows {
		ip := m.planned[m.rows[i].key]
		if ip == nil {
			continue
		}
		if len(ip.Conflicts) > 0 {
			m.rows[i].conflict = true
			m.rows[i].conflictReason = renderConflict(ip.Conflicts[0])
		}
		if ip.Status == plan.StatusNoAction && ip.Warning {
			m.rows[i].warning = true
		}
	}
}

// beginApply collects what the list would apply and moves to the summary it
// waits on. The required variables neither the lock nor this
// session's answers provide are asked for first: the summary must show the
// templated content as substituted.
func (m *model) beginApply() {
	p := m.collectApply()
	m.pending = p
	if len(p.selection) == 0 && len(p.removals) == 0 {
		m.lastResults = "nothing selected\n"
		m.screen = screenResults
		return
	}
	if missing := plan.MissingVars(m.kits, p.selection, m.lk, m.answers); len(missing) > 0 {
		m.formFields = nil
		for _, key := range p.selection {
			for _, v := range missing[key] {
				m.formFields = append(m.formFields, formField{key: key, varID: v.ID, prompt: v.Prompt})
			}
		}
		m.formCursor = 0
		m.input = textinput.New()
		m.input.Focus()
		m.screen = screenForm
		return
	}
	m.buildSummary(p)
}

// collectApply gathers the selection and the removal marks of the list into
// one pending apply. The paths cover the selected items' owned files, the
// kits' shared files, and the removals' recorded files and shared files, so
// the planning sees everything it may touch.
func (m *model) collectApply() *pendingApply {
	var selection []string
	var removals []string
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
	for _, r := range m.rows {
		if r.remove && r.installed {
			removals = append(removals, r.key)
			// A removal wins over a held selection: the item does not join
			// the installs of the same apply.
			continue
		}
		if !r.selected {
			continue
		}
		selection = append(selection, r.key)
		kitName, itemID, _ := strings.Cut(r.key, "/")
		for _, k := range m.kits {
			if k.Name != kitName {
				continue
			}
			for _, it := range k.Items {
				if it.ID != itemID {
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
	sort.Strings(paths)

	// A removal is planned from the lock: its files and shared files join the
	// snapshot, for the items of kits that are gone as well.
	for _, key := range removals {
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
	sort.Strings(paths)
	sort.Strings(sharedPaths)
	return &pendingApply{selection: selection, removals: removals, paths: paths, sharedPaths: sharedPaths}
}

// buildSummary plans the pending apply without writing anything and shows
// what it would do: every change per item with the reason of each action,
// the removals with the content they leave behind, the items kept, and every
// excluded item with its reasons. The apply runs only after the
// screen's confirmation, and plans again from the target of that moment.
func (m *model) buildSummary(p *pendingApply) {
	m.pending = p
	snap, err := target.Read(m.target, p.paths, p.sharedPaths)
	if err != nil {
		m.lastResults = "apply failed: " + err.Error()
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
			m.lastResults = "apply failed: " + err.Error()
			m.screen = screenResults
			return
		}
		installs = plan.Build(plan.Input{
			Kits:       m.kits,
			Selection:  p.selection,
			Lock:       m.lk,
			Snapshot:   snap,
			Sources:    sources,
			Answers:    m.answers,
			Reset:      m.resets,
			Reconciled: m.reconcil,
		}).Items
	}
	m.summary = renderSummary(installs, removals)
	m.screen = screenSummary
}

// renderSummary lays out the plans of one pending apply: every
// change per item with the reason of each action, the removals with the
// content they leave behind, the items kept, and every excluded item with
// its conflicts and reasons. An install that writes no byte still takes
// ownership of its JSON locations; the summary names them.
func renderSummary(installs, removals []*plan.ItemPlan) string {
	var changes, kept, excluded, removed []*plan.ItemPlan
	for _, ip := range installs {
		switch ip.Status {
		case plan.StatusInstall:
			changes = append(changes, ip)
		case plan.StatusNoAction:
			kept = append(kept, ip)
		case plan.StatusError:
			excluded = append(excluded, ip)
		}
	}
	for _, ip := range removals {
		if ip.Status == plan.StatusError {
			excluded = append(excluded, ip)
			continue
		}
		removed = append(removed, ip)
	}

	var b strings.Builder
	writeActions := func(items []*plan.ItemPlan) {
		for _, ip := range items {
			fmt.Fprintf(&b, "  %s %s\n", ip.Key, ip.Version)
			for _, a := range ip.Actions {
				fmt.Fprintf(&b, "    %-14s %-44s %s\n", string(a.Kind), a.To, a.Reason)
			}
			if len(ip.Actions) == 0 {
				files := make([]string, 0, len(ip.JSONLocations))
				for f := range ip.JSONLocations {
					files = append(files, f)
				}
				sort.Strings(files)
				for _, f := range files {
					for _, loc := range ip.JSONLocations[f] {
						fmt.Fprintf(&b, "    %-14s %-44s takes ownership of %s\n", string(plan.ActionJSON), f, loc.Pointer)
					}
				}
			}
			for _, report := range ip.Reports {
				fmt.Fprintf(&b, "    %s\n", report)
			}
		}
		b.WriteString("\n")
	}
	if len(changes) > 0 {
		b.WriteString("changes:\n")
		writeActions(changes)
	}
	if len(removed) > 0 {
		b.WriteString("removals:\n")
		writeActions(removed)
	}
	if len(kept) > 0 {
		b.WriteString("kept:\n")
		for _, ip := range kept {
			reason := ip.Reason
			if ip.Warning {
				reason = "warning: " + reason
			}
			fmt.Fprintf(&b, "  %s %s   %s\n", ip.Key, ip.Version, reason)
		}
		b.WriteString("\n")
	}
	if len(excluded) > 0 {
		b.WriteString("excluded:\n")
		for _, ip := range excluded {
			fmt.Fprintf(&b, "  %s\n", ip.Key)
			for _, line := range exclusionLines(ip) {
				fmt.Fprintf(&b, "    %s\n", line)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// exclusionLines renders an excluded item's causes: the structured
// conflicts first, then the plain reasons.
func exclusionLines(ip *plan.ItemPlan) []string {
	lines := make([]string, 0, len(ip.Conflicts)+len(ip.Reasons))
	for _, c := range ip.Conflicts {
		lines = append(lines, renderConflict(c))
	}
	return append(lines, ip.Reasons...)
}

// renderConflict renders one structured planner conflict as the prose the
// user sees: every contribution kind and cause has its own line here.
func renderConflict(c plan.Conflict) string {
	rivals := strings.Join(c.Rivals, ", ")
	switch c.Cause {
	case plan.CauseClaimed:
		if c.Kind == kit.KindJSON {
			return fmt.Sprintf("%s is claimed at %s with a different value by %s", c.File, quotePointer(c.Pointer), rivals)
		}
		return fmt.Sprintf("%s is also contributed by %s", c.File, rivals)
	case plan.CauseClaimedAsFile:
		return fmt.Sprintf("%s is contributed as a file by %s", c.File, rivals)
	case plan.CauseClaimedAsJSON:
		return fmt.Sprintf("%s is also contributed as json by %s; one shared file is either Markdown or JSON", c.File, rivals)
	case plan.CauseClaimedAsFileOrSection:
		return fmt.Sprintf("%s is also contributed as a file or section by %s; one shared file is either Markdown or JSON", c.File, rivals)
	case plan.CauseUserContent:
		if c.Heading != "" {
			return fmt.Sprintf("%s already has a heading %q outside managed blocks; the section would duplicate it", c.File, c.Heading)
		}
		if c.Block != "" {
			return fmt.Sprintf("%s holds block %s that is not recorded in the lock; refusing to touch content that may belong to the user", c.File, c.Block)
		}
		return fmt.Sprintf("%s exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user", c.File)
	case plan.CauseOwned:
		switch {
		case c.Kind == kit.KindFile:
			return fmt.Sprintf("%s is owned by installed item %s", c.File, c.Owner)
		case c.Block != "":
			return fmt.Sprintf("block %s in %s is owned by installed item %s", c.Block, c.File, c.Owner)
		default:
			return fmt.Sprintf("%s is owned as a whole file by installed item %s", c.File, c.Owner)
		}
	case plan.CauseModified:
		switch c.Kind {
		case kit.KindJSON:
			return fmt.Sprintf("%s holds a value at %s that was modified after installation, and the kit provides different content", c.File, quotePointer(c.Pointer))
		case kit.KindSection:
			return fmt.Sprintf("block %s in %s was modified after installation, and the kit provides different content", c.Block, c.File)
		default:
			return fmt.Sprintf("%s was modified after installation, and the kit provides different content", c.File)
		}
	case plan.CauseDifferentValue:
		return fmt.Sprintf("%s holds a different value at %s", c.File, quotePointer(c.Pointer))
	case plan.CauseTwoSections:
		return fmt.Sprintf("two section contributions write %s; one item owns one block per shared file", c.File)
	}
	return ""
}

// quotePointer names a JSON location for a conflict line: the root by
// name, any other pointer quoted.
func quotePointer(ptr string) string {
	if ptr == "" {
		return "the root"
	}
	return strconv.Quote(ptr)
}

// performApply runs the apply the summary confirmed: removals first, then
// the installs. Both plan again from a fresh snapshot: the removals rewrite
// and delete what the summary's snapshot observed, so a removed item's
// content cannot rise again in a shared file.
func (m *model) performApply(p *pendingApply) {
	m.pending = nil
	snap, err := target.Read(m.target, p.paths, p.sharedPaths)
	if err != nil {
		m.lastResults = "apply failed: " + err.Error()
		m.screen = screenResults
		return
	}

	var b strings.Builder

	// The removals go first: they shrink what the installs write into.
	if len(p.removals) > 0 {
		remove := map[string]bool{}
		for _, key := range p.removals {
			remove[key] = true
		}
		rp := plan.BuildRemoval(plan.Input{Lock: m.lk, Snapshot: snap, Remove: remove})
		results := apply.Remove(m.target, rp, m.lk, m.app.Locks)
		errors := map[string]string{}
		removed := map[string]bool{}
		for _, r := range results {
			if r.Err != nil {
				errors[r.Key] = r.Err.Error()
				continue
			}
			removed[r.Key] = true
		}
		// A removed row drops both of its marks: a form-resumed apply
		// collects no removal for it again, and does not reinstall the item
		// through its held selection.
		for i := range m.rows {
			if removed[m.rows[i].key] {
				m.rows[i].remove = false
				m.rows[i].selected = false
			}
		}
		for _, ip := range rp.Items {
			switch ip.Status {
			case plan.StatusRemove:
				if err := errors[ip.Key]; err != "" {
					fmt.Fprintf(&b, "failed   %s: %s\n", ip.Key, err)
				} else {
					fmt.Fprintf(&b, "removed  %s\n", ip.Key)
				}
			case plan.StatusError:
				fmt.Fprintf(&b, "excluded %s:\n", ip.Key)
				for _, line := range exclusionLines(ip) {
					fmt.Fprintf(&b, "         %s\n", line)
				}
			}
			for _, report := range ip.Reports {
				fmt.Fprintf(&b, "         %s\n", report)
			}
		}
		if lk, err := m.app.Locks.Load(m.target); err == nil {
			m.lk = lk
		}
		// The removals rewrote and deleted what the snapshot observed: the
		// installs plan from the target as the removals leave it, so a
		// removed item's content cannot rise again in a shared file.
		if len(p.selection) > 0 {
			snap, err = target.Read(m.target, p.paths, p.sharedPaths)
			if err != nil {
				m.lastResults = "apply failed: " + err.Error()
				m.screen = screenResults
				return
			}
		}
	}

	if len(p.selection) > 0 {
		sources, err := kit.ReadSources(m.kits, p.selection)
		if err != nil {
			m.lastResults = "apply failed: " + err.Error()
			m.screen = screenResults
			return
		}
		planned := plan.Build(plan.Input{
			Kits:       m.kits,
			Selection:  p.selection,
			Lock:       m.lk,
			Snapshot:   snap,
			Sources:    sources,
			Answers:    m.answers,
			Reset:      m.resets,
			Reconciled: m.reconcil,
		})
		m.resets = nil
		results := apply.Apply(m.target, planned, m.lk, m.app.Locks)
		errors := map[string]string{}
		for _, r := range results {
			if r.Err != nil {
				errors[r.Key] = r.Err.Error()
			}
		}

		for _, ip := range planned.Items {
			switch ip.Status {
			case plan.StatusInstall:
				if err := errors[ip.Key]; err != "" {
					fmt.Fprintf(&b, "failed   %s: %s\n", ip.Key, err)
				} else {
					fmt.Fprintf(&b, "applied  %s %s\n", ip.Key, ip.Version)
				}
			case plan.StatusNoAction:
				fmt.Fprintf(&b, "kept     %s: %s\n", ip.Key, ip.Reason)
			case plan.StatusError:
				fmt.Fprintf(&b, "excluded %s:\n", ip.Key)
				for _, line := range exclusionLines(ip) {
					fmt.Fprintf(&b, "         %s\n", line)
				}
			}
		}

	}

	if b.Len() == 0 {
		b.WriteString("nothing selected\n")
	}
	m.lastResults = b.String()

	// The selection as applied — the removal rows dropped out of it —
	// replaces the remembered one, so a removed item is not offered selected
	// again.
	if st, err := m.app.State.Load(); err == nil {
		st.SetSelection(m.target, p.selection)
		if err := m.app.State.Save(st); err != nil {
			m.lastResults += "\ncould not save the selection: " + err.Error()
		}
	}
	if lk, err := m.app.Locks.Load(m.target); err == nil {
		m.lk = lk
	}
	m.screen = screenResults
}

var (
	styleTitle = lipgloss.NewStyle().Bold(true)
	styleDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	styleErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func (m *model) View() string {
	var b strings.Builder
	switch m.screen {
	case screenUsage:
		if m.message != "" {
			b.WriteString(styleErr.Render(m.message) + "\n\n")
		}
		b.WriteString("penates installs items from a kit into a harness configuration\n")
		b.WriteString("directory. Start with a kit path:\n\n")
		b.WriteString("  penates <kit-path>\n")
	case screenRegister:
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render("penates"))
		fmt.Fprintf(&b, "kit        %s (%s)\n", m.pendingKit.Name, m.pendingKit.Dir)
		fmt.Fprintf(&b, "install to %s\n", m.input.View())
		fmt.Fprintf(&b, "\n%s\n", styleDim.Render("enter  confirm    esc  back"))
	case screenPicker:
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render("penates"))
		fmt.Fprintf(&b, "registered targets:\n\n")
		for i, t := range m.targets {
			prefix := "  "
			if i == m.cursor {
				prefix = "> "
			}
			fmt.Fprintf(&b, "%s%s\n", prefix, t)
		}
		fmt.Fprintf(&b, "\n%s\n", styleDim.Render("enter  choose    q  quit"))
	case screenList:
		header := "penates  target " + m.target
		if names := m.registeredKitNames(); len(names) > 0 {
			header += "  kits: " + strings.Join(names, ", ")
		}
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render(header))
		for _, e := range m.kitErrs {
			b.WriteString(styleErr.Render(e) + "\n")
		}
		if len(m.rows) == 0 && len(m.kitErrs) == 0 {
			b.WriteString("no items\n")
		}
		pos := 0
		for _, g := range m.groups {
			head := "-- " + g.kitName
			if g.kitDir != "" {
				head += "  " + g.kitDir
			}
			shown := 0
			for _, idx := range g.rows {
				if m.rows[idx].matches(m.filter.Value()) {
					shown++
				}
			}
			if shown == 0 {
				continue
			}
			fmt.Fprintf(&b, "%s\n", styleTitle.Render(head))
			if g.note != "" {
				note := "   " + g.note
				if g.noteErr {
					b.WriteString(styleErr.Render(note) + "\n")
				} else {
					b.WriteString(styleDim.Render(note) + "\n")
				}
			}
			for _, idx := range g.rows {
				r := &m.rows[idx]
				if !r.matches(m.filter.Value()) {
					continue
				}
				cursor := " "
				if pos == m.cursor {
					cursor = ">"
				}
				pos++
				mark := fmt.Sprintf("%-12s", "["+r.statusWord()+"]")
				switch r.statusWord() {
				case "conflict", "broken", "unavailable":
					mark = styleErr.Render(mark)
				}
				if r.major {
					// A major update is a behavior change; mark it
					// for review.
					mark = styleErr.Render(mark)
				}
				idcol := r.key
				if len(r.tags) > 0 {
					idcol += "  " + strings.Join(r.tags, " ")
				}
				desc := r.description
				if r.conflict && r.conflictReason != "" {
					desc = r.conflictReason
				}
				suffix := ""
				if r.reset {
					suffix += styleDim.Render(" reset")
				}
				if r.remove {
					suffix += styleErr.Render(" remove")
				}
				fmt.Fprintf(&b, "%s%s %-30s %-16s %s%s\n", cursor, mark, idcol, m.versionLabel(*r), desc, suffix)
			}
			b.WriteString("\n")
		}
		if m.filtering {
			fmt.Fprintf(&b, "filter %s\n", m.filter.View())
			fmt.Fprintf(&b, "\n%s\n", styleDim.Render("enter  apply filter    esc  clear filter"))
		} else {
			fmt.Fprintf(&b, "\n%s\n", styleDim.Render("/  filter    space  select    i  details    r  reset    d  remove    a  apply    q  quit"))
		}
	case screenDetails:
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render("penates  "+m.target))
		r, ok := m.rowByKey(m.detailKey)
		if !ok {
			b.WriteString("no item\n")
			break
		}
		fmt.Fprintf(&b, "%s %s\n", styleTitle.Render(r.key), m.versionLabel(r))
		fmt.Fprintf(&b, "\n%s\n", r.description)
		if len(r.tags) > 0 {
			fmt.Fprintf(&b, "\ntags  %s\n", strings.Join(r.tags, ", "))
		}
		if r.kitName != "" {
			kitLine := "kit   " + r.kitName
			if r.kitDir != "" {
				kitLine += "  " + r.kitDir
			}
			fmt.Fprintf(&b, "%s\n", kitLine)
		}
		if it := m.itemByKey(r.key); it != nil {
			if len(it.Contributes) > 0 {
				b.WriteString("\ncontributes:\n")
				for _, c := range it.Contributes {
					tmpl := ""
					if c.Template {
						tmpl = "  (template)"
					}
					fmt.Fprintf(&b, "  %-8s %s%s\n", string(c.Kind), c.To, tmpl)
				}
			}
			if it.HasChangelog {
				if data, err := os.ReadFile(filepath.Join(it.Dir, "CHANGELOG.md")); err == nil {
					b.WriteString("\nchangelog:\n")
					for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
						fmt.Fprintf(&b, "  %s\n", line)
					}
				}
			}
		}
		if m.lk != nil {
			if rec := m.lk.Items[r.key]; rec != nil && len(rec.Answers) > 0 {
				b.WriteString("\nanswers:\n")
				ids := make([]string, 0, len(rec.Answers))
				for id := range rec.Answers {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				for _, id := range ids {
					fmt.Fprintf(&b, "  %s = %s\n", id, rec.Answers[id])
				}
			}
		}
		fmt.Fprintf(&b, "\n%s\n", styleDim.Render("esc  back"))
	case screenForm:
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render("penates  "+m.target))
		fmt.Fprintf(&b, "required variables:\n\n")
		for i, f := range m.formFields {
			if i < m.formCursor {
				continue
			}
			cursor := "  "
			if i == m.formCursor {
				cursor = "> "
			}
			if f.prompt != "" {
				fmt.Fprintf(&b, "%s%s\n", cursor, f.prompt)
			} else {
				fmt.Fprintf(&b, "%s%s\n", cursor, f.varID)
			}
			if i == m.formCursor {
				fmt.Fprintf(&b, "  %s\n", m.input.View())
			}
		}
		fmt.Fprintf(&b, "\n%s\n", styleDim.Render(fmt.Sprintf("variable %d of %d    enter  next    esc  cancel", m.formCursor+1, len(m.formFields))))
	case screenSummary:
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render("penates  apply summary  "+m.target))
		b.WriteString(strings.TrimRight(m.summary, "\n"))
		fmt.Fprintf(&b, "\n\n%s\n", styleDim.Render("enter  apply    esc  cancel"))
	case screenResults:
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render("apply"))
		b.WriteString(m.lastResults)
		fmt.Fprintf(&b, "\n%s\n", styleDim.Render("any key  back"))
	}
	return b.String()
}

// registeredKitNames lists the kits registered for the target: the loaded
// ones and the ones that failed to load, in list order.
func (m *model) registeredKitNames() []string {
	var out []string
	for _, g := range m.groups {
		if g.kitDir != "" || g.noteErr {
			out = append(out, g.kitName)
		}
	}
	return out
}

// rowByKey finds a row by its item key.
func (m *model) rowByKey(key string) (row, bool) {
	for i := range m.rows {
		if m.rows[i].key == key {
			return m.rows[i], true
		}
	}
	return row{}, false
}

// itemByKey finds the kit item behind a row key, or nil for the rows whose
// kit does not load or is not registered.
func (m *model) itemByKey(key string) *kit.Item {
	kitName, itemID, split := strings.Cut(key, "/")
	if !split {
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

// versionLabel renders the version column: installed and kit versions joined
// by the direction of the change when they differ.
func (m *model) versionLabel(r row) string {
	if !r.installed || r.installedVersion == r.version {
		return r.version
	}
	switch kit.CompareVersions(r.version, r.installedVersion) {
	case 1:
		return r.installedVersion + " > " + r.version
	case -1:
		return r.installedVersion + " < " + r.version
	}
	return r.version
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
