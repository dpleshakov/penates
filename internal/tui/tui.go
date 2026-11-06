// Package tui is the terminal client: registration, item list, variable forms, and
// apply summary. It is the only package that knows about the terminal.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	m := &model{app: a, input: textinput.New()}
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
	screenResults
)

// row is one item line in the list.
type row struct {
	key              string
	version          string
	description      string
	installed        bool
	installedVersion string
	orphan           bool
	modified         bool
	broken           bool
	selected         bool
	reset            bool
	remove           bool
}

// formField is one missing required variable the variable form asks for.
type formField struct {
	key    string
	varID  string
	prompt string
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
	rows     []row
	cursor   int
	resets   map[string]bool
	reconcil *plan.Reconciliation

	formFields  []formField
	formCursor  int
	answers     map[string]map[string]string
	lastResults string
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
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			if msg.String() == "esc" {
				switch m.screen {
				case screenRegister:
					m.screen = screenUsage
					return m, nil
				case screenForm:
					m.screen = screenList
					return m, nil
				}
			}
			return m, tea.Quit
		}

		switch m.screen {
		case screenUsage:
			// Nothing to do beyond quitting.
		case screenRegister:
			return m.updateRegister(msg)
		case screenPicker:
			return m.updatePicker(msg)
		case screenList:
			return m.updateList(msg)
		case screenForm:
			return m.updateForm(msg)
		case screenResults:
			m.enterList(m.target)
		}
	case tea.WindowSizeMsg:
		// Rendering is line-based; no size-dependent layout yet.
	}
	return m, nil
}

func (m *model) updateRegister(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	}
	return m, nil
}

func (m *model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case " ", "enter":
		if len(m.rows) > 0 {
			m.rows[m.cursor].selected = !m.rows[m.cursor].selected
		}
	case "r":
		// Reset to kit is only ever a user request (RFC 7.6): it toggles on
		// the row under the cursor, applies once, and clears.
		if len(m.rows) > 0 && m.rows[m.cursor].installed {
			key := m.rows[m.cursor].key
			if m.resets == nil {
				m.resets = map[string]bool{}
			}
			if m.resets[key] {
				delete(m.resets, key)
				m.rows[m.cursor].reset = false
			} else {
				m.resets[key] = true
				m.rows[m.cursor].reset = true
			}
		}
	case "d":
		// Removal is only ever a user request (RFC 9): it toggles on the
		// installed row under the cursor and applies once.
		if len(m.rows) > 0 && m.rows[m.cursor].installed {
			m.rows[m.cursor].remove = !m.rows[m.cursor].remove
		}
	case "a":
		m.apply()
	}
	return m, nil
}

// updateForm feeds the current field; enter records the answer and moves on,
// and the last one resumes the apply the form interrupted.
func (m *model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.apply()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// enterList loads everything the item list shows: the kits registered for the
// target, the target's lock, and the previous selection. The lock is
// reconciled with the target before anything is shown or planned (RFC 7.7):
// the staged files of an interrupted apply leave the target, and the items
// whose recorded content the target holds modified or has lost take their
// statuses.
func (m *model) enterList(dir string) {
	m.message = ""
	m.target = dir
	m.cursor = 0
	m.screen = screenList

	m.kits, m.kitErrs, m.reconcil = nil, nil, nil
	lk, err := m.app.Locks.Load(dir)
	if err != nil {
		m.kitErrs = append(m.kitErrs, "lock: "+err.Error())
	}
	m.lk = lk

	if lk != nil {
		files, shared := lk.RecordedPaths()
		snap, err := target.Read(dir, files, shared)
		if err != nil {
			m.kitErrs = append(m.kitErrs, "target: "+err.Error())
		} else if rec, err := apply.Reconcile(dir, lk, snap); err != nil {
			m.kitErrs = append(m.kitErrs, "reconcile: "+err.Error())
		} else {
			m.reconcil = rec
		}
	}

	st, err := m.app.State.Load()
	if err != nil {
		m.kitErrs = append(m.kitErrs, "state: "+err.Error())
		return
	}
	selected := map[string]bool{}
	for _, key := range st.SelectionFor(dir) {
		selected[key] = true
	}

	m.rows = nil
	covered := map[string]bool{}
	for _, name := range st.KitsForTarget(dir) {
		k, err := kit.Load(st.Kits[name])
		if err != nil {
			m.kitErrs = append(m.kitErrs, "kit "+name+": "+err.Error())
			continue
		}
		m.kits = append(m.kits, k)
		for _, it := range k.Items {
			key := kit.ItemKey(k.Name, it.ID)
			covered[key] = true
			r := row{
				key:         key,
				version:     it.Version,
				description: it.Description,
				selected:    selected[key],
			}
			if rec := lk.Items[key]; rec != nil {
				r.installed = true
				r.installedVersion = rec.Version
				r.reset = m.resets[key]
				r.modified = m.reconcil.ModifiedItem(key)
				r.broken = m.reconcil.BrokenItem(key)
			}
			m.rows = append(m.rows, r)
		}
	}
	// Installed items no kit provides anymore stay managed and frozen: the
	// list shows them so the user can remove them (RFC 9.2).
	keys := make([]string, 0, len(lk.Items))
	for key := range lk.Items {
		if !covered[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		rec := lk.Items[key]
		r := row{
			key:              key,
			version:          rec.Version,
			description:      "no longer provided by any registered kit",
			installed:        true,
			installedVersion: rec.Version,
			orphan:           true,
			selected:         selected[key],
		}
		r.modified = m.reconcil.ModifiedItem(key)
		r.broken = m.reconcil.BrokenItem(key)
		m.rows = append(m.rows, r)
	}
}

// apply plans and installs the current selection, removes the items marked
// for removal, records the selection in the state, and shows the results.
func (m *model) apply() {
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
			// the installs of the same apply (RFC 9).
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
	// snapshot, for the items of kits that are gone as well (RFC 9.2).
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

	if len(selection) == 0 && len(removals) == 0 {
		m.lastResults = "nothing selected\n"
		m.screen = screenResults
		return
	}

	snap, err := target.Read(m.target, paths, sharedPaths)
	if err != nil {
		m.lastResults = "apply failed: " + err.Error()
		m.screen = screenResults
		return
	}

	var b strings.Builder

	// The removals go first: they shrink what the installs write into.
	if len(removals) > 0 {
		remove := map[string]bool{}
		for _, key := range removals {
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
		// through its held selection (RFC 9).
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
				for _, reason := range ip.Reasons {
					fmt.Fprintf(&b, "         %s\n", reason)
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
		// removed item's content cannot rise again in a shared file (RFC 9).
		if len(selection) > 0 {
			snap, err = target.Read(m.target, paths, sharedPaths)
			if err != nil {
				m.lastResults = "apply failed: " + err.Error()
				m.screen = screenResults
				return
			}
		}
	}

	if len(selection) > 0 {
		// The form collects the required variables neither the lock nor this
		// session's answers provide; apply resumes once they are in (RFC 6.4,
		// 10.2). A required variable answered with an empty string stays
		// missing, so the form asks for it again.
		if missing := plan.MissingVars(m.kits, selection, m.lk, m.answers); len(missing) > 0 {
			m.formFields = nil
			for _, key := range selection {
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

		sources, err := kit.ReadSources(m.kits, selection)
		if err != nil {
			m.lastResults = "apply failed: " + err.Error()
			m.screen = screenResults
			return
		}
		p := plan.Build(plan.Input{
			Kits:       m.kits,
			Selection:  selection,
			Lock:       m.lk,
			Snapshot:   snap,
			Sources:    sources,
			Answers:    m.answers,
			Reset:      m.resets,
			Reconciled: m.reconcil,
		})
		m.resets = nil
		results := apply.Apply(m.target, p, m.lk, m.app.Locks)
		errors := map[string]string{}
		for _, r := range results {
			if r.Err != nil {
				errors[r.Key] = r.Err.Error()
			}
		}

		for _, ip := range p.Items {
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
				for _, reason := range ip.Reasons {
					fmt.Fprintf(&b, "         %s\n", reason)
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
	// again (RFC 9).
	if st, err := m.app.State.Load(); err == nil {
		st.SetSelection(m.target, selection)
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
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render("penates  "+m.target))
		for _, e := range m.kitErrs {
			b.WriteString(styleErr.Render(e) + "\n")
		}
		if len(m.rows) == 0 && len(m.kitErrs) == 0 {
			b.WriteString("no items\n")
		}
		for i, r := range m.rows {
			mark := " "
			switch {
			case r.selected:
				mark = "x"
			case r.installed:
				mark = "i"
			}
			cursor := " "
			if i == m.cursor {
				cursor = ">"
			}
			installed := ""
			if r.orphan {
				installed += styleDim.Render(" orphan")
			} else if r.installed {
				installed = styleDim.Render(" installed")
			}
			if r.broken {
				installed += styleErr.Render(" broken")
			}
			if r.modified {
				installed += styleDim.Render(" modified")
			}
			if r.reset {
				installed += styleDim.Render(" reset")
			}
			if r.remove {
				installed += styleErr.Render(" remove")
			}
			fmt.Fprintf(&b, "%s[%s] %-24s %-16s %s%s\n", cursor, mark, r.key, m.versionLabel(r), r.description, installed)
		}
		fmt.Fprintf(&b, "\n%s\n", styleDim.Render("space  select    r  reset    d  remove    a  apply    q  quit"))
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
	case screenResults:
		fmt.Fprintf(&b, "%s\n\n", styleTitle.Render("apply"))
		b.WriteString(m.lastResults)
		fmt.Fprintf(&b, "\n%s\n", styleDim.Render("any key  back"))
	}
	return b.String()
}

// versionLabel renders the version column: installed and kit versions joined
// by the direction of the change when they differ (RFC 8).
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
