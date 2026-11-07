package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dpleshakov/penates/internal/state"
)

// The target list overlay of the main screen (tui-screens.md 8): the list
// under the header with fuzzy search over the display name and the path,
// the current target marked, the kit and item counts, adding a target
// with directory completion and the existence line of 6.4, renaming the
// display name, and the removal dialog of 8.3. Switching moves to the
// target and offers the saved pending actions for restoration (8.2,
// 11.1). The flows drive the state operations — RegisterTarget,
// ForgetTarget, DeferForgetTarget — and hold no state logic of their own.

// The marks of the target rows (8.1 mockup): the current target filled,
// the others hollow.
var (
	targetCurrentMark = mark{unicode: "●", ascii: "*", role: roleGreen}
	targetOtherMark   = mark{unicode: "○", ascii: "o", role: roleDim}
)

// The arrows of the header target: pointing down while the list is
// closed, up while it shows (8.1 mockup).
var (
	targetClosedMark = mark{unicode: "▾", ascii: "v", role: roleDim}
	targetOpenMark   = mark{unicode: "▴", ascii: "^", role: roleDim}
)

// The labels and key lines of the list and its flows (8.1 mockup).
const (
	addTargetLabel   = "+ add target"
	targetListKeys   = "enter switch · ctrl+r rename · ctrl+d remove · esc close"
	targetAddKeys    = "tab complete · enter add target · esc cancel"
	targetRenameKeys = "enter rename · esc cancel"
)

// targetEntry is one row of the list: the target with the counts the list
// read when it opened or last refreshed.
type targetEntry struct {
	path    string
	name    string
	kits    int
	items   int
	current bool
}

// label is the title of the row: the display name when the target has
// one, the path otherwise (8.1).
func (e targetEntry) label() string {
	if e.name != "" {
		return e.name
	}
	return displayPath(e.path)
}

// countsText renders the counts of the row: "N kits · M items", the items
// the lock of the target records.
func (e targetEntry) countsText() string {
	return fmt.Sprintf("%d %s · %d %s", e.kits, plural(e.kits, "kit", "kits"), e.items, plural(e.items, "item", "items"))
}

// plural picks the noun for the count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// targetList is the open overlay: every registered target with its
// counts, the entries the search lets through in display order, the
// cursor over them and the add-target row, and the inputs the row
// actions take over.
type targetList struct {
	// all is every registered target; entries is what the search keeps,
	// the better fuzzy match first.
	all     []targetEntry
	entries []targetEntry
	cursor  int

	search textinput.Model

	// The add-target input: the path with directory completion, read a
	// short interval after the last keystroke (6.2), with the existence
	// line of 6.4.
	adding      bool
	pathInput   textinput.Model
	completions []string
	completion  int
	exists      bool
	existsKnown bool

	// The rename input of the display name, over one target's row.
	renaming  string
	nameInput textinput.Model
}

// openTargetList opens the list under the header (8.1) and reads the
// counts of every target — the lock of each one, the way the kits load on
// entering a target.
func (m *model) openTargetList() tea.Cmd {
	l := &targetList{}
	l.search = textinput.New()
	l.search.Prompt = "› "
	l.search.Width = 48
	l.search.Focus()
	l.pathInput = textinput.New()
	l.pathInput.Width = 48
	l.nameInput = textinput.New()
	l.nameInput.Width = 48
	m.list = l
	m.refreshTargetList()
	return textinput.Blink
}

// closeTargetList tears the overlay down.
func (m *model) closeTargetList() {
	m.list = nil
}

// refreshTargetList rebuilds the entries from the registry and the locks:
// the kit count of the attachments and the item count of the lock, one
// read per target, as at the open.
func (m *model) refreshTargetList() {
	l := m.list
	if l == nil || m.reg == nil {
		return
	}
	l.all = nil
	for _, p := range m.reg.TargetPaths() {
		t := m.reg.Targets[p]
		items := 0
		if lk, err := m.app.Locks.Load(p); err == nil {
			items = len(lk.Items)
		}
		l.all = append(l.all, targetEntry{
			path:    p,
			name:    t.Name,
			kits:    len(m.reg.KitsFor(p)),
			items:   items,
			current: p == m.target,
		})
	}
	l.applyFilter()
}

// applyFilter filters the entries through the search pattern, over the
// display name and the path together: the better fuzzy match first, the
// registry order breaking ties (8.1).
func (l *targetList) applyFilter() {
	pattern := l.search.Value()
	type ranked struct {
		entry targetEntry
		score int
	}
	var keep []ranked
	for _, e := range l.all {
		text := e.path
		if e.name != "" {
			text = e.name + " " + e.path
		}
		if score, ok := FuzzyMatch(pattern, text); ok {
			keep = append(keep, ranked{e, score})
		}
	}
	sort.SliceStable(keep, func(i, j int) bool { return keep[i].score > keep[j].score })
	l.entries = make([]targetEntry, len(keep))
	for i, r := range keep {
		l.entries[i] = r.entry
	}
	l.clampCursor()
}

// clampCursor keeps the cursor on a row or on the add-target row, which
// sits after the entries.
func (l *targetList) clampCursor() {
	if l.cursor > len(l.entries) {
		l.cursor = len(l.entries)
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// updateTargetList feeds the open list: the inputs of the row actions
// first, then the navigation, then the search. While the list is open it
// takes the keyboard, the way the dialogs do.
func (m *model) updateTargetList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := m.list
	switch msg.String() {
	case "esc":
		switch {
		case l.adding:
			l.stopAdd()
		case l.renaming != "":
			l.stopRename()
		default:
			m.closeTargetList()
		}
		return m, nil
	case "enter":
		switch {
		case l.adding:
			return m, m.addTarget()
		case l.renaming != "":
			return m, m.applyRename()
		case l.cursor == len(l.entries):
			// The add-target row opens the path input (8.1).
			l.startAdd()
			return m, textinput.Blink
		default:
			return m, m.switchFromList()
		}
	case "tab":
		if l.adding {
			return m, m.completeListPath()
		}
	case "ctrl+r":
		if !l.adding && l.renaming == "" && l.cursor < len(l.entries) {
			l.startRename(l.entries[l.cursor])
			return m, textinput.Blink
		}
	case "ctrl+d":
		if !l.adding && l.renaming == "" && l.cursor < len(l.entries) {
			m.openForgetDialog(l.entries[l.cursor].path)
			return m, nil
		}
	case "up", "k":
		if !l.adding && l.renaming == "" {
			l.cursor--
			l.clampCursor()
		}
		return m, nil
	case "down", "j":
		if !l.adding && l.renaming == "" {
			l.cursor++
			l.clampCursor()
		}
		return m, nil
	}
	var cmd tea.Cmd
	switch {
	case l.adding:
		l.pathInput, cmd = l.pathInput.Update(msg)
		l.completion = 0
		m.previewSeq++
		cmd = tea.Batch(cmd, tickPreview(m.previewSeq))
	case l.renaming != "":
		l.nameInput, cmd = l.nameInput.Update(msg)
	default:
		l.search, cmd = l.search.Update(msg)
		l.applyFilter()
	}
	return m, cmd
}

// startAdd turns the add-target row into the path input (8.1): the
// completion and the existence line come from the debounced read, as in
// 6.2 and 6.4.
func (l *targetList) startAdd() {
	l.adding = true
	l.pathInput.Focus()
	l.search.Blur()
}

// stopAdd tears the path input down; the search takes the keyboard back.
func (l *targetList) stopAdd() {
	l.adding = false
	l.pathInput.Reset()
	l.pathInput.Blur()
	l.completions = nil
	l.completion = 0
	l.exists = false
	l.existsKnown = false
	l.search.Focus()
}

// startRename turns the row into the display-name input, prefilled with
// the current name (8.1: ctrl+r renames).
func (l *targetList) startRename(e targetEntry) {
	l.renaming = e.path
	l.nameInput.SetValue(e.name)
	l.nameInput.Focus()
	l.search.Blur()
}

// stopRename tears the rename input down.
func (l *targetList) stopRename() {
	l.renaming = ""
	l.nameInput.Reset()
	l.nameInput.Blur()
	l.search.Focus()
}

// addTarget registers the directory the input names (8.1): the target
// exists without kits until one attaches there. The list shows the new
// row and the cursor moves to it.
func (m *model) addTarget() tea.Cmd {
	l := m.list
	t := absOf(strings.TrimSpace(l.pathInput.Value()))
	if t == "" {
		return nil
	}
	if err := m.app.State.RegisterTarget(t); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	l.stopAdd()
	m.refreshTargetList()
	for i, e := range l.entries {
		if e.path == t {
			l.cursor = i
			break
		}
	}
	return m.showNotice("Registered target " + displayPath(t) + ".")
}

// completeListPath completes the add-target input to the next candidate,
// the way the add-kit input does (6.2).
func (m *model) completeListPath() tea.Cmd {
	l := m.list
	next, value := nextCompletion(&l.pathInput, l.completions, l.completion)
	if value == "" {
		return nil
	}
	l.completion = next
	l.pathInput.SetValue(value)
	m.previewSeq++
	return tickPreview(m.previewSeq)
}

// applyRename is the enter of the rename input (8.1): the display name
// saves onto the target — an empty input clears it. A renamed current
// target changes the header at once.
func (m *model) applyRename() tea.Cmd {
	l := m.list
	t, name := l.renaming, strings.TrimSpace(l.nameInput.Value())
	err := m.app.State.Update(func(reg *state.Registry) error {
		target, ok := reg.Targets[t]
		if !ok {
			return fmt.Errorf("target %s is not registered", t)
		}
		target.Name = name
		reg.Targets[t] = target
		return nil
	})
	if err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	l.stopRename()
	m.refreshTargetList()
	if name == "" {
		return m.showNotice("Cleared the display name of " + displayPath(t) + ".")
	}
	return m.showNotice("Renamed target to " + name + ".")
}

// switchFromList is the enter of a target row (8.1): the screen moves to
// the target. Switching to the current one just closes the list.
func (m *model) switchFromList() tea.Cmd {
	t := m.list.entries[m.list.cursor].path
	m.closeTargetList()
	if t == m.target {
		return nil
	}
	return m.switchTarget(t)
}

// switchTarget moves the screen to the target (8.2): the pending actions
// of the target being left are already saved — every toggle saves them —
// so switching never discards anything and asks nothing. A target that
// holds saved pending actions offers them for restoration on entering
// (11.1).
func (m *model) switchTarget(dir string) tea.Cmd {
	entered := m.enterTarget(dir)
	return tea.Batch(entered, m.offerRestore())
}

// offerRestore opens the restore dialog of 11.1 when the entered target
// holds pending actions. The deferred operations are not actions: a
// target that holds only them opens without the offer.
func (m *model) offerRestore() tea.Cmd {
	p := m.pendingActions()
	if p == nil || len(p.Items) == 0 {
		return nil
	}
	m.restoreTarget = m.target
	m.dialog = &dialog{
		kind:  dialogRestore,
		title: "Previous selection",
		body:  restoreDialogBody(p, m.rows, displayPath(m.target)),
		options: []string{
			"Restore",
			"Start with no pending actions",
		},
		chosen: 0,
		keys:   "enter choose · esc start with none",
	}
	return nil
}

// restoreDialogBody names what the dialog offers to restore (11.1): how
// many pending actions the target holds and what each would do.
func restoreDialogBody(p *state.Pending, rows []Row, target string) []string {
	keys := make([]string, 0, len(p.Items))
	for key := range p.Items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, restoreVerb(p.Items[key], key, rows)+" "+restoreItemID(key))
	}
	noun := "actions"
	if len(keys) == 1 {
		noun = "action"
	}
	text := fmt.Sprintf("The last session ended with %d pending %s in %s: %s.",
		len(keys), noun, target, strings.Join(parts, ", "))
	return wrapIndent(text, "", "", ProseLimit)
}

// restoreVerb names what one pending action would do: remove and reset
// speak for themselves; a selection is an install, an update or a
// downgrade by the status the tree planned for the item — an item the
// tree does not show installs.
func restoreVerb(item *state.ItemPending, key string, rows []Row) string {
	switch {
	case item.Remove:
		return "remove"
	case item.Reset:
		return "reset"
	}
	status := StatusAvailable
	for _, r := range rows {
		if r.Kind == KindItem && r.Item.Key == key {
			status = r.Item.Status
		}
	}
	switch status {
	case StatusUpdate:
		return "update"
	case StatusDowngrade:
		return "downgrade"
	}
	return "install"
}

// restoreItemID is the item id the dialog names: the part after the kit.
func restoreItemID(key string) string {
	if _, id, ok := strings.Cut(key, "/"); ok {
		return id
	}
	return key
}

// restorePending keeps the restored actions of the target (11.1) after
// checking they still name something they can act on: the ones that do
// not are dropped and reported on the notice line. The target is the one
// the dialog offered; only the entered one holds the loaded kits the
// check reads.
func (m *model) restorePending(t string) tea.Cmd {
	if t != m.target {
		return nil
	}
	dropped, err := m.dropInvalidPending()
	if err != nil {
		return m.showNotice(err.Error())
	}
	if dropped > 0 {
		return m.showNotice(fmt.Sprintf("Dropped %d invalid pending %s.", dropped, plural(dropped, "action", "actions")))
	}
	return nil
}

// discardPendingItems starts the target with no pending actions (11.1):
// the item actions go with their answers; the deferred operations stay —
// they are not actions but operations waiting for their apply. The notice
// reports how many went (11.1: the drops are reported).
func (m *model) discardPendingItems(t string) tea.Cmd {
	n := 0
	if p := m.pendingActionsOf(t); p != nil {
		n = len(p.Items)
	}
	err := m.app.State.Update(func(reg *state.Registry) error {
		p, ok := reg.Pending[t]
		if !ok {
			return nil
		}
		p.Items = map[string]*state.ItemPending{}
		if len(p.Deferred) == 0 {
			delete(reg.Pending, t)
		}
		return nil
	})
	if err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	m.refresh()
	if n > 0 {
		return m.showNotice(fmt.Sprintf("Discarded %d pending %s.", n, plural(n, "action", "actions")))
	}
	return nil
}

// openForgetDialog opens the removal dialog of 8.3 over the target: how
// many items are installed decides what the first option has to do. The
// default is Cancel: the question is irreversible, and the answer must
// never happen by enter alone, as in 6.7.
func (m *model) openForgetDialog(t string) {
	items := 0
	if lk, err := m.app.Locks.Load(t); err == nil {
		items = len(lk.Items)
	}
	m.forgetTarget = t
	m.forgetItems = items
	body := []string{"No items are installed in this target."}
	switch {
	case items == 1:
		body = []string{"1 item is installed in this target."}
	case items > 1:
		body = []string{fmt.Sprintf("%d items are installed in this target.", items)}
	}
	m.dialog = &dialog{
		kind:  dialogForgetTarget,
		title: "Remove target " + displayPath(t),
		body:  body,
		options: []string{
			"Remove the items, then forget the target",
			"Forget the target, leave the files unmanaged",
			"Cancel",
		},
		chosen: 2,
	}
}

// forgetAfterRemovals is the first option of 8.3: the screen switches to
// the target, every installed item takes the removal mark, and the
// summary opens; the forget waits behind the apply as a deferred
// operation and completes with it.
func (m *model) forgetAfterRemovals(t string) tea.Cmd {
	entered := m.enterTarget(t)
	var keys []string
	if m.lk != nil {
		for key := range m.lk.Items {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := m.editPending(key, func(ip *state.ItemPending) { ip.Remove = true }); err != nil {
			return tea.Batch(entered, m.showNotice(err.Error()))
		}
	}
	if err := m.app.State.DeferForgetTarget(t); err != nil {
		return tea.Batch(entered, m.showNotice(err.Error()))
	}
	if err := m.reloadRegistry(); err != nil {
		return tea.Batch(entered, m.showNotice(err.Error()))
	}
	m.refresh()
	m.beginApply()
	return entered
}

// forgetTargetNow is the second option of 8.3, and the first one when the
// target holds no installed items — the summary would confirm an empty
// apply. The lock of the target is deleted and the target is forgotten;
// its files are not touched and are no longer owned. When the forgotten
// target is the current one, the first registered path opens — the last
// active one is not recorded any more — or the first run when none is.
func (m *model) forgetTargetNow(t string) tea.Cmd {
	if err := m.app.State.ForgetTarget(t, m.app.Locks); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	var entered tea.Cmd
	if t == m.target {
		m.closeTargetList()
		paths := m.reg.TargetPaths()
		if len(paths) == 0 {
			entered = m.enterTarget("")
		} else {
			entered = m.enterTarget(paths[0])
		}
	} else if m.list != nil {
		m.refreshTargetList()
	}
	return tea.Batch(entered, m.showNotice("Forgot target "+displayPath(t)+"."))
}

// read is what a debounce tick performs while the list is open (6.2): the
// directory candidates of the add-target input and the existence of the
// directory it names.
func (l *targetList) read() {
	if !l.adding {
		return
	}
	v := strings.TrimSpace(l.pathInput.Value())
	if v == "" {
		l.completions = nil
		l.exists = false
		l.existsKnown = false
		return
	}
	l.completions = directoryCandidates(l.pathInput.Value())
	l.existsKnown = true
	l.exists = dirExists(absOf(v))
}

// existsLine is the existence line of 6.4 under the add-target input.
func (l *targetList) existsLine() string {
	if l.exists {
		return "The directory exists."
	}
	return "The directory does not exist. It is created on the first apply."
}

// keysLine is the key line the list names in its bottom border: the keys
// of the mode the list is in (8.1 mockup).
func (l *targetList) keysLine() string {
	switch {
	case l.adding:
		return targetAddKeys
	case l.renaming != "":
		return targetRenameKeys
	}
	return targetListKeys
}

// rowContent is one target row without its cursor bar: the mark, the
// display name or the path, and the dim path beside a name (8.1). The
// row being renamed shows its input in place of the name.
func (l *targetList) rowContent(e targetEntry, set Set) string {
	mk := targetOtherMark
	if e.current {
		mk = targetCurrentMark
	}
	if l.renaming == e.path {
		return mk.glyph(set) + " " + l.nameInput.View()
	}
	s := mk.glyph(set) + " " + e.label()
	if e.name != "" {
		s += "  " + Dim(displayPath(e.path))
	}
	return s
}

// rowRight is the right side of a row: the current label and the dim
// counts (8.1 mockup).
func (l *targetList) rowRight(e targetEntry) string {
	right := ""
	if e.current {
		right = "current · "
	}
	return right + Dim(e.countsText())
}

// lines renders the list as body lines (8.1 mockup): the box centered in
// the width under the header, the search line at the top, the target rows
// with the cursor bar and the counts at the right, the add-target row
// with the input, the existence line and the dropdown while it is open,
// and the key line inside the bottom border after a separator.
func (l *targetList) lines(width int, set Set) []string {
	g := unicodeBox
	if set == ASCIISet {
		g = asciiBox
	}

	// The inner width fits the widest line, plus the one column of
	// padding inside each border.
	inner := 0
	widen := func(s string) {
		if w := ansi.StringWidth(s); w > inner {
			inner = w
		}
	}
	widen(" " + l.search.View())
	for _, e := range l.entries {
		widen(" " + l.rowContent(e, set) + "  " + l.rowRight(e))
	}
	addContent := addTargetLabel
	if l.adding {
		addContent = addTargetLabel + "  " + l.pathInput.View()
	}
	widen(" " + addContent)
	var exist, drop []string
	if l.adding {
		if l.existsKnown {
			exist = []string{"  " + l.existsLine()}
			widen(exist[0])
		}
		drop = completionBox(l.completions, len(" "+addTargetLabel+"  "), set)
		for _, d := range drop {
			widen(d)
		}
	}
	widen(" " + l.keysLine())

	boxW := inner + 3
	pad := strings.Repeat(" ", max(0, (width-boxW)/2))

	out := make([]string, 0, len(l.entries)+8)
	out = append(out, pad+g.tl+strings.Repeat(g.h, boxW-2)+g.tr)
	line := func(s string) {
		rest := boxW - 3 - ansi.StringWidth(s)
		out = append(out, pad+g.v+" "+s+strings.Repeat(" ", max(0, rest))+g.v)
	}
	line(" " + l.search.View())
	for i, e := range l.entries {
		bar := " "
		if i == l.cursor {
			bar = cursorMark.glyph(set)
		}
		line(bar + l.rowContent(e, set) + "  " + l.rowRight(e))
	}
	bar := " "
	if l.cursor == len(l.entries) {
		bar = cursorMark.glyph(set)
	}
	line(bar + addContent)
	for _, e := range exist {
		line(e)
	}
	for _, d := range drop {
		line(d)
	}
	out = append(out, pad+g.lm+strings.Repeat(g.h, boxW-2)+g.rm)
	line(l.keysLine())
	out = append(out, pad+g.bl+strings.Repeat(g.h, boxW-2)+g.br)
	return out
}
