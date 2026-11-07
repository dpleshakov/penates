package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The full help of the main screen (tui-screens.md 12.5): the bindings of
// 12.1–12.4 grouped by context, then the status and pending glyph legends
// of 3.1 and 3.2 — the tables of the visual language are their source. ?
// and the palette's Help command open it; esc closes it; the arrows scroll
// the lines the way the details view scrolls.

// openHelp opens the full help over the body.
func (m *model) openHelp() {
	m.helpOpen = true
	m.helpOffset = 0
	m.helpLines = renderHelp(m.set)
}

// closeHelp tears the help down.
func (m *model) closeHelp() {
	m.helpOpen = false
	m.helpLines = nil
	m.helpOffset = 0
}

// updateHelp feeds the open help: esc closes it, the arrows and page keys
// scroll (12.5). Every other key is ignored — the help takes the keyboard
// while it is open.
func (m *model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := m.viewportHeight()
	if page < 1 {
		page = 1
	}
	switch msg.String() {
	case "esc":
		m.closeHelp()
	case "up":
		m.helpOffset = ScrollOffset(m.helpOffset-1, len(m.helpLines), page)
	case "down":
		m.helpOffset = ScrollOffset(m.helpOffset+1, len(m.helpLines), page)
	case "pgup":
		m.helpOffset = ScrollOffset(m.helpOffset-page, len(m.helpLines), page)
	case "pgdown":
		m.helpOffset = ScrollOffset(m.helpOffset+page, len(m.helpLines), page)
	}
	return m, nil
}

// helpRow is one binding or legend line: the key or glyph at the left, the
// action or meaning beside it.
type helpRow struct {
	left string
	text string
}

// renderHelp lays the help out into body lines (12.5): the bindings of
// 12.1–12.4 grouped by context, then the two legends. The left column
// lines up across the whole help, the way a table reads.
func renderHelp(set Set) []string {
	keys := [][]helpRow{
		{
			{"↑ ↓, j k", "Move the cursor"},
			{"pgup pgdn, home end", "Move by page, to the first or last row"},
			{"/", "Filter"},
			{"a", "Summary"},
			{"t", "Target list"},
			{"R", "Rescan kits"},
			{"ctrl+p", "Command palette"},
			{"?", "Full help"},
			{"u", "Undo the change the notice names"},
			{"q", "Quit"},
			{"ctrl+c twice", "Force quit without dialogs"},
			{"esc", "Close the overlay or view, cancel the input, or clear the filter"},
		},
		{
			{"space, → ←", "Expand or collapse"},
			{"enter", "Details view"},
			{"e", "Edit path"},
			{"d", "Detach"},
		},
		{
			{"space", "Toggle the default action"},
			{"d", "Toggle removal"},
			{"r", "Toggle reset"},
			{"enter", "Details view"},
			{"←", "Move to the kit row"},
		},
		{
			{"enter", "Add a kit"},
		},
	}
	status := make([]helpRow, 0, len(statusMarks))
	for s := StatusAvailable; s <= StatusUnavailable; s++ {
		status = append(status, helpRow{left: statusMarks[s].rendered(set), text: statusHelp[s]})
	}
	pending := make([]helpRow, 0, len(pendingMarks))
	for p := PendingApply; p <= PendingRemove; p++ {
		pending = append(pending, helpRow{left: pendingMarks[p].rendered(set), text: pendingHelp[p]})
	}

	width := 0
	for _, group := range append(keys, status, pending) {
		for _, r := range group {
			if w := ansi.StringWidth(r.left); w > width {
				width = w
			}
		}
	}

	lines := []string{"Keys", ""}
	titles := []string{"Global", "Kit row", "Item row", "+ add kit row"}
	for i, group := range keys {
		lines = append(lines, titles[i])
		for _, r := range group {
			lines = append(lines, helpColumn(r, width))
		}
		lines = append(lines, "")
	}
	lines = append(lines, "Status glyphs")
	for _, r := range status {
		lines = append(lines, helpColumn(r, width))
	}
	lines = append(lines, "", "Pending actions")
	for _, r := range pending {
		lines = append(lines, helpColumn(r, width))
	}
	return lines
}

// helpColumn is one help line: the left column padded to the table width,
// the text after two spaces.
func helpColumn(r helpRow, width int) string {
	pad := width - ansi.StringWidth(r.left)
	if pad < 0 {
		pad = 0
	}
	return "  " + r.left + strings.Repeat(" ", pad) + "  " + r.text
}

// statusHelp names what each status glyph of 3.1 means.
var statusHelp = [...]string{
	StatusAvailable:   "Available (not installed)",
	StatusInstalled:   "Installed, matches the kit",
	StatusUpdate:      "Update available",
	StatusDowngrade:   "Downgrade available",
	StatusModified:    "Modified by the user; kit content unchanged",
	StatusConflict:    "Conflict",
	StatusBroken:      "Broken: owned content is missing from the target",
	StatusWarning:     "Warning: kit content changed without a version change",
	StatusOrphan:      "Orphan: removed from its kit",
	StatusUnavailable: "Unavailable: the kit cannot be read",
}

// pendingHelp names what each pending glyph of 3.2 means; the exact verb
// shows in the details pane and in the summary.
var pendingHelp = [...]string{
	PendingApply:  "Install, update or downgrade",
	PendingReset:  "Reset to kit",
	PendingRemove: "Remove",
}
