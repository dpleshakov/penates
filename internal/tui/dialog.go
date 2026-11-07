package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The modal dialog (tui-screens.md 6.3, 6.7): a boxed question with fixed
// options, the arrows moving over them and enter choosing. Modal dialogs
// are reserved for destructive or irreversible decisions (1.4); every
// other question happens in place.

// The kinds of dialog the screens open. The kind tells chooseDialog what
// the chosen option does.
const (
	dialogAttach = "attach"
	dialogDetach = "detach"
)

// dialog is one open modal: the question and the options, the cursor over
// the chosen one. The model holds the dialog's context — the kit it acts
// on — beside it.
type dialog struct {
	kind    string
	title   string
	body    []string
	options []string
	chosen  int
}

// dialogKeys is the key line the open dialog names (6.3, 6.7 mockups: it
// renders inside the box).
const dialogKeys = "enter choose · esc cancel"

// updateDialog feeds the keys of the open dialog: the arrows move, enter
// chooses, esc cancels. Every other key is ignored — a modal takes the
// keyboard while it is open.
func (m *model) updateDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.dialog.chosen > 0 {
			m.dialog.chosen--
		}
	case "down", "j":
		if m.dialog.chosen < len(m.dialog.options)-1 {
			m.dialog.chosen++
		}
	case "enter":
		return m, m.chooseDialog()
	case "esc":
		m.closeDialog()
	}
	return m, nil
}

// chooseDialog acts on the option the cursor of the open dialog sits on.
func (m *model) chooseDialog() tea.Cmd {
	d := m.dialog
	switch d.kind {
	case dialogAttach:
		offer := m.attach
		switch d.chosen {
		case 0:
			// Register the proposed target and attach there (6.3): an
			// already registered target only switches.
			m.closeDialog()
			return m.attachOfferAt(offer, offer.hint)
		case 1:
			// Attach to the current target anyway (6.3).
			m.closeDialog()
			return m.attachOfferAt(offer, m.target)
		}
	case dialogDetach:
		name := m.detachName
		switch d.chosen {
		case 0:
			m.closeDialog()
			return m.detachWithRemovals(name)
		case 1:
			m.closeDialog()
			return m.detachKit(name)
		}
	}
	m.closeDialog()
	return nil
}

// closeDialog tears the modal down and forgets what it acted on.
func (m *model) closeDialog() {
	m.dialog = nil
	m.attach = nil
	m.detachName = ""
}

// The box glyphs of the dialog frame; the ASCII set draws the same box
// with plain characters (3.3).
type boxGlyphs struct {
	tl, tr, bl, br, h, v string
}

var (
	unicodeBox = boxGlyphs{"╭", "╮", "╰", "╯", "─", "│"}
	asciiBox   = boxGlyphs{"+", "+", "+", "+", "-", "|"}
)

// lines renders the dialog as body lines (6.3, 6.7 mockups): the box
// centered in the width, the title in the top border, the body above the
// options, the cursor bar on the chosen option, and the key line inside
// the bottom border.
func (d *dialog) lines(width int, set Set) []string {
	g := unicodeBox
	if set == ASCIISet {
		g = asciiBox
	}
	inner := make([]string, 0, len(d.body)+len(d.options)+3)
	inner = append(inner, d.body...)
	inner = append(inner, "")
	for i, o := range d.options {
		if i == d.chosen {
			inner = append(inner, cursorMark.glyph(set)+o)
		} else {
			inner = append(inner, " "+o)
		}
	}
	inner = append(inner, "", dialogKeys)

	// One blank column of padding inside each border; the title never
	// squeezes the border fill to nothing.
	w := 0
	for _, l := range inner {
		if x := ansi.StringWidth(l); x > w {
			w = x
		}
	}
	boxW := w + 3
	if t := ansi.StringWidth(d.title); t+2 > w {
		boxW = t + 5
	}
	pad := strings.Repeat(" ", max(0, (width-boxW)/2))

	out := make([]string, 0, len(inner)+2)
	fill := max(1, boxW-ansi.StringWidth(d.title)-4)
	out = append(out, pad+g.tl+" "+d.title+" "+strings.Repeat(g.h, fill)+g.tr)
	for _, l := range inner {
		rest := boxW - 3 - ansi.StringWidth(l)
		out = append(out, pad+g.v+" "+l+strings.Repeat(" ", rest)+g.v)
	}
	out = append(out, pad+g.bl+strings.Repeat(g.h, boxW-2)+g.br)
	return out
}
