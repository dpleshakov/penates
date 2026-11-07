package tui

import (
	"fmt"
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
	dialogAttach       = "attach"
	dialogDetach       = "detach"
	dialogForgetTarget = "forgetTarget"
	dialogRestore      = "restore"
	dialogQuit         = "quit"
)

// dialog is one open modal: the question and the options, the cursor over
// the chosen one. The model holds the dialog's context — the kit or the
// target it acts on — beside it. keys names the key line inside the
// bottom border; empty falls back to dialogKeys.
type dialog struct {
	kind    string
	title   string
	body    []string
	options []string
	chosen  int
	keys    string
}

// dialogKeys is the key line the open dialog names (6.3, 6.7 mockups: it
// renders inside the box).
const dialogKeys = "enter choose · esc cancel"

// updateDialog feeds the keys of the open dialog: the arrows move, enter
// chooses, esc cancels. Every other key is ignored — a modal takes the
// keyboard while it is open. The restore dialog's esc starts with no
// pending actions (11.1: "esc start with none").
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
		if m.dialog.kind == dialogRestore {
			t := m.restoreTarget
			m.closeDialog()
			return m, m.discardPendingItems(t)
		}
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
	case dialogForgetTarget:
		t, n := m.forgetTarget, m.forgetItems
		switch d.chosen {
		case 0:
			m.closeDialog()
			if n == 0 {
				// Nothing installed: the summary would confirm an empty
				// apply, so the forget goes straight through the
				// operation the deferred one would complete with (8.3).
				return m.forgetTargetNow(t)
			}
			m.closeTargetList()
			return m.forgetAfterRemovals(t)
		case 1:
			m.closeDialog()
			return m.forgetTargetNow(t)
		}
	case dialogRestore:
		t := m.restoreTarget
		switch d.chosen {
		case 0:
			// Restore keeps the valid actions; the ones that no longer
			// name anything they can act on are dropped and reported on
			// the notice line (11.1).
			m.closeDialog()
			return m.restorePending(t)
		case 1:
			m.closeDialog()
			return m.discardPendingItems(t)
		}
	case dialogQuit:
		m.closeDialog()
		if d.chosen == 0 {
			// The pending actions stay saved; the next start offers them
			// for restoration (11.3).
			return tea.Quit
		}
		return nil
	}
	m.closeDialog()
	return nil
}

// openQuitDialog asks what to do about the pending actions before quitting
// (11.3): the default is Quit — leaving is what q asked for — and esc goes
// back. They are saved either way.
func (m *model) openQuitDialog() {
	n := m.pendingCount()
	verb := "have"
	if n == 1 {
		verb = "has"
	}
	text := fmt.Sprintf("%d pending %s %s not been applied. They will be offered for restoration on the next start.",
		n, plural(n, "action", "actions"), verb)
	m.dialog = &dialog{
		kind:    dialogQuit,
		title:   "Quit",
		body:    wrapIndent(text, "", "", ProseLimit),
		options: []string{"Quit", "Back"},
		chosen:  0,
	}
}

// closeDialog tears the modal down and forgets what it acted on.
func (m *model) closeDialog() {
	m.dialog = nil
	m.attach = nil
	m.detachName = ""
	m.forgetTarget = ""
	m.forgetItems = 0
	m.restoreTarget = ""
}

// The box glyphs of the dialog frame; the ASCII set draws the same box
// with plain characters (3.3). lm and rm are the left and right caps of
// a separator border inside the box.
type boxGlyphs struct {
	tl, tr, bl, br, h, v, lm, rm string
}

var (
	unicodeBox = boxGlyphs{"╭", "╮", "╰", "╯", "─", "│", "├", "┤"}
	asciiBox   = boxGlyphs{"+", "+", "+", "+", "-", "|", "|", "|"}
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
	keys := d.keys
	if keys == "" {
		keys = dialogKeys
	}
	inner = append(inner, "", keys)

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
