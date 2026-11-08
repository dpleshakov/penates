package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The command palette of the main screen (tui-screens.md 9): a box under
// the header with fuzzy search over the fixed command set, in the box
// style of the target list (8.1) and through the fuzzy matcher the list
// shares. A command that has a direct key shows it at the right of its
// row. Commands that act on a kit take the kit under the cursor; outside
// one they ask for it through the kit picker, the same box listing the
// kits of the current target.

// paletteCommand is one command of the palette: its name, the direct key
// the row names when the command has one, and what running it does. needsKit
// marks a command that acts on a kit; run receives the kit to act on, empty
// for the global ones.
type paletteCommand struct {
	name     string
	key      string
	needsKit bool
	run      func(m *model, kit string) tea.Cmd
}

// paletteCommands is the fixed command set of 14, in the order the palette
// lists them before a search narrows it.
func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{name: "Add kit", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			return m.startAddKit("")
		}},
		{name: "Attach kit", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			return m.startAttachKit()
		}},
		{name: "Detach kit", key: "d", needsKit: true, run: func(m *model, name string) tea.Cmd {
			m.closePalette()
			return m.detachKitFlow(name)
		}},
		{name: "Edit kit path", key: "e", needsKit: true, run: func(m *model, name string) tea.Cmd {
			m.closePalette()
			if kk, ok := m.reg.Kits[name]; ok {
				m.startEditKit(name, kk.Dir)
			}
			return nil
		}},
		{name: "Reset builtin kit", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			return m.resetBuiltinFlow()
		}},
		{name: "Switch target", key: "t", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			return m.openTargetList()
		}},
		{name: "Add target", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			cmd := m.openTargetList()
			// The add-target row's input opens at once (8.1).
			m.list.startAdd()
			return tea.Batch(cmd, textinput.Blink)
		}},
		{name: "Rename target", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			cmd := m.openTargetList()
			if m.target != "" {
				for i, e := range m.list.entries {
					if e.path == m.target {
						m.list.cursor = i
						m.list.startRename(e)
						return tea.Batch(cmd, textinput.Blink)
					}
				}
			}
			return cmd
		}},
		{name: "Remove target", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			if m.target == "" {
				return m.showNotice("No target is open.")
			}
			m.openForgetDialog(m.target)
			return nil
		}},
		{name: "Rescan kits", key: "R", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			return m.rescanKits()
		}},
		{name: "Apply", key: "a", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			m.beginApply()
			return nil
		}},
		{name: "Clear pending actions", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			return m.discardPendingItems(m.target)
		}},
		{name: "Help", key: "?", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			m.openHelp()
			return nil
		}},
		{name: "Quit", key: "q", run: func(m *model, _ string) tea.Cmd {
			m.closePalette()
			return m.quit()
		}},
	}
}

// commandPalette is the open palette: the fixed commands, the ones the search
// keeps in display order, and the cursor over them.
type commandPalette struct {
	commands []paletteCommand
	shown    []int
	cursor   int
	search   textinput.Model
}

// newCommandPalette opens the palette: the search takes the keyboard the way the
// target list's does.
func newCommandPalette() *commandPalette {
	p := &commandPalette{commands: paletteCommands()}
	p.search = textinput.New()
	p.search.Prompt = "› "
	p.search.Width = 48
	p.search.Focus()
	p.applyFilter()
	return p
}

// openPalette opens the command palette over the body (9).
func (m *model) openPalette() tea.Cmd {
	m.palette = newCommandPalette()
	return textinput.Blink
}

// closePalette tears the palette and the kit picker it may have opened
// down.
func (m *model) closePalette() {
	m.palette = nil
	m.kitPick = nil
}

// applyFilter filters the commands through the search pattern: the better
// fuzzy match first, the fixed order breaking ties (9).
func (p *commandPalette) applyFilter() {
	pattern := p.search.Value()
	type ranked struct {
		index int
		score int
	}
	var keep []ranked
	for i, c := range p.commands {
		if score, ok := FuzzyMatch(pattern, c.name); ok {
			keep = append(keep, ranked{i, score})
		}
	}
	sort.SliceStable(keep, func(i, j int) bool { return keep[i].score > keep[j].score })
	p.shown = make([]int, len(keep))
	for i, r := range keep {
		p.shown[i] = r.index
	}
	if p.cursor >= len(p.shown) {
		p.cursor = len(p.shown) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// updatePalette feeds the open palette: the navigation and the choice, then
// the search. While the palette is open it takes the keyboard, the way the
// dialogs do.
func (m *model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.palette
	switch msg.String() {
	case "esc":
		m.closePalette()
		return m, nil
	case "enter":
		return m, m.runPaletteCommand()
	case "up", "k":
		p.cursor--
		p.applyFilter()
		return m, nil
	case "down", "j":
		p.cursor++
		p.applyFilter()
		return m, nil
	}
	var cmd tea.Cmd
	p.search, cmd = p.search.Update(msg)
	p.applyFilter()
	return m, cmd
}

// runPaletteCommand runs the command the cursor sits on: a kit command
// takes the kit under the cursor, or asks for one (9).
func (m *model) runPaletteCommand() tea.Cmd {
	p := m.palette
	if p.cursor >= len(p.shown) {
		return nil
	}
	c := p.commands[p.shown[p.cursor]]
	if c.needsKit {
		if name := m.kitUnderCursor(); name != "" {
			m.closePalette()
			return c.run(m, name)
		}
		return m.openKitPicker(c)
	}
	m.closePalette()
	return c.run(m, "")
}

// kitUnderCursor names the kit the cursor sits in: the kit row itself, or
// an item row under it. The orphans group and the add-kit row are not
// inside a kit — they name none (9: the command asks for one).
func (m *model) kitUnderCursor() string {
	if len(m.rows) == 0 || m.cursor >= len(m.rows) {
		return ""
	}
	switch r := m.rows[m.cursor]; r.Kind {
	case KindAddKit:
		return ""
	case KindKit:
		if r.Kit.Orphans {
			return ""
		}
		return r.Kit.Name
	default:
		for i := m.cursor; i >= 0; i-- {
			if k := m.rows[i]; k.Kind == KindKit {
				if k.Kit.Orphans {
					return ""
				}
				return k.Kit.Name
			}
		}
		return ""
	}
}

// kitPick asks which kit a palette command acts on (9: the kit commands
// ask for one outside a kit). The box lists the kit names — the kits of
// the current target for the kit commands, the registered kits not
// attached to it for Attach kit; enter runs the command on the chosen
// one, esc cancels.
type kitPick struct {
	run    func(m *model, kit string) tea.Cmd
	kits   []string
	cursor int
}

// openKitPicker replaces the palette with the kit picker (9); a target
// with no kits has nothing to pick from, and the question answers itself.
func (m *model) openKitPicker(c paletteCommand) tea.Cmd {
	m.closePalette()
	kits := m.reg.KitsFor(m.target)
	if len(kits) == 0 {
		return m.showNotice("No kits are attached to this target.")
	}
	m.kitPick = &kitPick{run: c.run, kits: kits}
	return nil
}

// updateKitPick feeds the open picker: the arrows move, enter chooses, esc
// cancels.
func (m *model) updateKitPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.kitPick
	switch msg.String() {
	case "esc":
		m.closePalette()
		return m, nil
	case "enter":
		run, name := p.run, p.kits[p.cursor]
		m.closePalette()
		return m, run(m, name)
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
		return m, nil
	case "down", "j":
		if p.cursor < len(p.kits)-1 {
			p.cursor++
		}
		return m, nil
	}
	return m, nil
}

// rowContent is one command row without its cursor bar: the name, and the
// direct key at the right when the command has one (9 mockup).
func (p *commandPalette) rowContent(index int) string {
	c := p.commands[index]
	if c.key == "" {
		return c.name
	}
	return fmt.Sprintf("%-*s  %s", p.nameWidth(), c.name, Dim(c.key))
}

// nameWidth is the column the direct keys start at: one past the longest
// name the fixed set holds, so the keys line up across searches.
func (p *commandPalette) nameWidth() int {
	w := 0
	for _, c := range p.commands {
		if len(c.name) > w {
			w = len(c.name)
		}
	}
	return w
}

// lines renders the palette as body lines (9 mockup): the box centered in
// the width under the header, the search line at the top, the commands the
// search keeps with the cursor bar and the direct key at the right.
func (p *commandPalette) lines(width int, set Set) []string {
	g := unicodeBox
	if set == ASCIISet {
		g = asciiBox
	}

	inner := 0
	widen := func(s string) {
		if w := ansi.StringWidth(s); w > inner {
			inner = w
		}
	}
	widen(" " + p.search.View())
	for _, index := range p.shown {
		widen(" " + p.rowContent(index))
	}

	boxW := inner + 3
	pad := strings.Repeat(" ", max(0, (width-boxW)/2))

	out := make([]string, 0, len(p.shown)+3)
	out = append(out, pad+g.tl+strings.Repeat(g.h, boxW-2)+g.tr)
	line := func(s string) {
		rest := boxW - 3 - ansi.StringWidth(s)
		out = append(out, pad+g.v+" "+s+strings.Repeat(" ", max(0, rest))+g.v)
	}
	line(" " + p.search.View())
	for i, index := range p.shown {
		bar := " "
		if i == p.cursor {
			bar = cursorMark.glyph(set)
		}
		line(bar + p.rowContent(index))
	}
	out = append(out, pad+g.bl+strings.Repeat(g.h, boxW-2)+g.br)
	return out
}

// lines renders the kit picker as body lines: the box centered in the
// width, one row per kit of the target, the key line inside the bottom
// border behind a separator, the way the target list names its keys (8.1).
func (p *kitPick) lines(width int, set Set) []string {
	g := unicodeBox
	if set == ASCIISet {
		g = asciiBox
	}

	inner := 0
	widen := func(s string) {
		if w := ansi.StringWidth(s); w > inner {
			inner = w
		}
	}
	for _, name := range p.kits {
		widen(" " + name)
	}
	widen(" " + kitPickKeys)

	boxW := inner + 3
	pad := strings.Repeat(" ", max(0, (width-boxW)/2))

	out := make([]string, 0, len(p.kits)+4)
	out = append(out, pad+g.tl+strings.Repeat(g.h, boxW-2)+g.tr)
	line := func(s string) {
		rest := boxW - 3 - ansi.StringWidth(s)
		out = append(out, pad+g.v+" "+s+strings.Repeat(" ", max(0, rest))+g.v)
	}
	for i, name := range p.kits {
		bar := " "
		if i == p.cursor {
			bar = cursorMark.glyph(set)
		}
		line(bar + name)
	}
	out = append(out, pad+g.lm+strings.Repeat(g.h, boxW-2)+g.rm)
	line(kitPickKeys)
	out = append(out, pad+g.bl+strings.Repeat(g.h, boxW-2)+g.br)
	return out
}

// kitPickKeys is the key line the picker names in its bottom border.
const kitPickKeys = "enter choose · esc cancel"

// paletteFrame is the frame the palette or the picker renders through: the
// header stays, the box takes the body, the footer stays empty like the
// target list's (8.1).
func (m *model) paletteFrame(l Layout) Frame {
	f := Frame{Header: m.header(l.Width)}
	if m.kitPick != nil {
		f.Rows = m.kitPick.lines(l.Width, m.set)
		f.Cursor = m.kitPick.cursor + 1 // the rows start under the top border
		return f
	}
	f.Rows = m.palette.lines(l.Width, m.set)
	f.Cursor = m.palette.cursor + 2 // the rows start under the top border and the search line
	return f
}
