package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Minimum size, breakpoints and caps of the layout (tui-screens.md 2.2).
const (
	// MinWidth and MinHeight are the smallest terminal penates lays out at;
	// below it shows the minimum-size message instead of a screen.
	MinWidth  = 60
	MinHeight = 16
	// MediumBreakpoint is the width the tree gains the description column.
	MediumBreakpoint = 80
	// DetailsBreakpoint is the width the details pane appears at.
	DetailsBreakpoint = 110
	// MinDetailsWidth is the narrowest the details pane may get.
	MinDetailsWidth = 44
	// ProseLimit caps wrapped prose regardless of the available width.
	ProseLimit = 100
)

// Mode is the layout band a terminal size falls into (2.2).
type Mode int

const (
	// ModeTooSmall is below MinWidth or MinHeight: the minimum-size message
	// replaces the screen.
	ModeTooSmall Mode = iota
	// ModeNarrow is 60 to 79 columns: the tree without tags and descriptions.
	ModeNarrow
	// ModeMedium is 80 to 109 columns: the tree with a description column.
	ModeMedium
	// ModeWide is 110 columns and more: the tree and the details pane side by
	// side.
	ModeWide
)

// String names the mode for tests and messages.
func (m Mode) String() string {
	switch m {
	case ModeTooSmall:
		return "tooSmall"
	case ModeNarrow:
		return "narrow"
	case ModeMedium:
		return "medium"
	case ModeWide:
		return "wide"
	default:
		return "unknown"
	}
}

// Columns states which columns the body rows keep at the layout's width. The
// bands of 2.2 set the baseline: the tags and description columns drop below
// MediumBreakpoint, and in ModeWide the tree drops the description column in
// favor of the details pane. Columns that still do not fit drop in the order
// tags, description, then the version column shrinks to the kit version alone
// (FitVersion).
type Columns struct {
	Tags        bool
	Description bool
}

// Layout is the arrangement of one terminal size: the band of 2.2, the widths
// of the regions, and the columns the body keeps. A screen renders through a
// Layout with Render; a resize builds a new Layout and re-renders.
type Layout struct {
	Width  int
	Height int
	Mode   Mode
	// Columns are the body columns the layout keeps.
	Columns Columns
	// DetailsWidth is the width of the details pane, 0 when the band shows
	// none.
	DetailsWidth int
	// TreeWidth is the width the body rows get: the full width, or the width
	// left of the pane separator.
	TreeWidth int
}

// NewLayout lays out a terminal of the given size. The details pane takes
// about 40% of the width, at least MinDetailsWidth.
func NewLayout(width, height int) Layout {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	l := Layout{Width: width, Height: height}
	switch {
	case width < MinWidth || height < MinHeight:
		l.Mode = ModeTooSmall
	case width >= DetailsBreakpoint:
		l.Mode = ModeWide
	case width >= MediumBreakpoint:
		l.Mode = ModeMedium
	default:
		l.Mode = ModeNarrow
	}
	switch l.Mode {
	case ModeWide:
		pane := width * 2 / 5
		if pane < MinDetailsWidth {
			pane = MinDetailsWidth
		}
		l.DetailsWidth = pane
		l.TreeWidth = width - pane - 1 // the separator column
		l.Columns = Columns{Tags: true}
	case ModeMedium:
		l.TreeWidth = width
		l.Columns = Columns{Tags: true, Description: true}
	case ModeNarrow:
		l.TreeWidth = width
	}
	return l
}

// Fixed chrome around the body: the header line and the blank under it, the
// blank over the footer and the footer line (2.3).
const (
	headerLines = 2
	footerLines = 2
)

// detailsSeparator runs between the tree and the details pane.
const detailsSeparator = "│"

// Frame is the content one screen renders: fixed header and footer lines, the
// body rows, and the optional input and notice lines. Rows carry their full
// content; Render clips them to the viewport and keeps the cursor row in view.
type Frame struct {
	Header string
	Footer string
	// Rows are the body rows with their full content.
	Rows []string
	// Cursor is the row index Render keeps in view.
	Cursor int
	// Details are the details pane lines; Render lays them beside the body in
	// ModeWide, from the top of the body, and ignores them otherwise.
	Details []string
	// Input and Notice are the optional lines under the body; an empty string
	// means the line is absent (2.1: header, body, input, notice, footer).
	Input  string
	Notice string
}

// Render lays the frame out at the layout's size and returns the screen text,
// one line per terminal row.
func (l Layout) Render(f Frame) string {
	if l.Mode == ModeTooSmall {
		return l.minimumMessage()
	}
	lines := make([]string, 0, l.Height)
	lines = append(lines, cut(f.Header, l.Width), "")
	viewport := l.Height - headerLines - footerLines
	if f.Input != "" {
		viewport--
	}
	if f.Notice != "" {
		viewport--
	}
	if viewport < 1 {
		viewport = 1
	}
	start := scrollStart(f.Cursor, viewport, len(f.Rows))
	for i := range viewport {
		row := ""
		if start+i < len(f.Rows) {
			row = f.Rows[start+i]
		}
		lines = append(lines, l.bodyLine(row, i, f))
	}
	if f.Input != "" {
		lines = append(lines, cut(f.Input, l.Width))
	}
	if f.Notice != "" {
		lines = append(lines, cut(f.Notice, l.Width))
	}
	lines = append(lines, "", cut(f.Footer, l.Width))
	return strings.Join(lines, "\n")
}

// bodyLine renders one visible body row: clipped to the tree width, with the
// details pane line beside it in ModeWide.
func (l Layout) bodyLine(row string, paneRow int, f Frame) string {
	if l.Mode != ModeWide {
		return cut(row, l.TreeWidth)
	}
	pane := ""
	if paneRow < len(f.Details) {
		pane = cut(f.Details[paneRow], l.DetailsWidth)
	}
	return strings.TrimRight(pad(row, l.TreeWidth)+detailsSeparator+pane, " ")
}

// minimumMessage is the message below the minimum size; it states the minimum
// and the current size (4.4).
func (l Layout) minimumMessage() string {
	return strings.Join([]string{
		"  penates needs at least",
		fmt.Sprintf("  %d columns and %d rows.", MinWidth, MinHeight),
		"",
		fmt.Sprintf("  Current size: %d × %d.", l.Width, l.Height),
	}, "\n")
}

// scrollStart picks the first visible row so the cursor row stays in view;
// past the first page the list scrolls with the cursor, at the end it snaps
// to the last page.
func scrollStart(cursor, viewport, rows int) int {
	start := 0
	if cursor >= viewport {
		start = cursor - viewport + 1
	}
	if over := rows - viewport; start > over {
		start = over
	}
	if cursor < start {
		start = cursor
	}
	if start < 0 {
		start = 0
	}
	return start
}

// ShortenLeft shortens s from the left with an ellipsis so it fits the width:
// the head of a path gives way, and the cut lands on a path separator when
// there is one, so the ellipsis replaces whole directories (4.3).
func ShortenLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	tail := ansi.TruncateLeft(s, w-(width-1), "")
	if i := strings.Index(tail, "/"); i >= 0 && i < len(tail)-1 {
		tail = tail[i:]
	}
	return "…" + tail
}

// ProseWidth caps an available width at ProseLimit: prose in the details pane
// and the details view wraps at no more than 100 columns, regardless of the
// available width (2.2).
func ProseWidth(avail int) int {
	if avail > ProseLimit {
		return ProseLimit
	}
	if avail < 1 {
		return 1
	}
	return avail
}

// WrapProse wraps a paragraph of prose to the width, breaking words that do
// not fit on a line of their own.
func WrapProse(text string, width int) []string {
	if text == "" {
		return nil
	}
	return strings.Split(ansi.Wrap(text, ProseWidth(width), " "), "\n")
}

// FitVersion renders the version column for the width: "installed → kit" when
// the pair fits, the kit version alone when it does not (2.2). The caller
// passes a single version for an item whose installed version matches.
func FitVersion(installed, kit string, width int) string {
	full := installed + " → " + kit
	if ansi.StringWidth(full) <= width {
		return full
	}
	return cut(kit, width)
}

// JoinSides puts left and right content on one line of the width, the right
// content flush to the edge; the left content gives way first when the line
// clips.
func JoinSides(left, right string, width int) string {
	right = cut(right, width)
	left = pad(cut(left, width-ansi.StringWidth(right)), width-ansi.StringWidth(right))
	return left + right
}

// noticeTTL is how long a notice stays on the notice line before it clears
// itself (2.1: cleared by the next key or after a few seconds).
const noticeTTL = 4 * time.Second

// Notice is the transient message of the notice line. The next key clears it,
// or it clears itself after its time to live.
type Notice struct {
	text    string
	expires time.Time // zero when no notice is shown
}

// Show puts text on the notice line.
func (n *Notice) Show(text string) {
	n.text = text
	n.expires = time.Now().Add(noticeTTL)
}

// Clear removes the notice; the model calls it on the next key.
func (n *Notice) Clear() {
	n.text = ""
	n.expires = time.Time{}
}

// Tick ages the notice against now and clears it when its time is up.
func (n *Notice) Tick(now time.Time) {
	if n.Visible() && !now.Before(n.expires) {
		n.Clear()
	}
}

// Visible reports whether a notice is on the line.
func (n *Notice) Visible() bool {
	return n.text != ""
}

// Text is the notice text, empty when no notice is shown.
func (n *Notice) Text() string {
	return n.text
}

// cut clips s to the display width, without breaking ANSI escape sequences.
func cut(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "")
}

// pad clips s to the width and pads it out to the width.
func pad(s string, width int) string {
	s = cut(s, width)
	if rest := width - ansi.StringWidth(s); rest > 0 {
		return s + strings.Repeat(" ", rest)
	}
	return s
}
