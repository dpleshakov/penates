package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// The visual language of the interface (tui-screens.md 3): status and
// pending glyphs with colors and ASCII fallbacks (3.1, 3.2), dim secondary
// information, the light and dark theme, and NO_COLOR (3.3). State is shown
// by a glyph and a color together, so a mark stripped of its color keeps its
// glyph and stays legible (1.2).

// Set is a glyph set of the visual language: the Unicode set, or the ASCII
// fallback set of 3.1 and 3.2. The set follows PENATES_ASCII alone;
// automatic detection of a terminal that cannot render the Unicode set is
// deferred (3.3, backlog ascii-detect).
type Set int

const (
	// UnicodeSet is the default glyph set.
	UnicodeSet Set = iota
	// ASCIISet is the fallback glyph set.
	ASCIISet
)

// SetFromEnv selects the glyph set from the environment: ASCIISet when
// PENATES_ASCII is "1", UnicodeSet otherwise (3.3). getenv resolves the
// variable; production passes os.Getenv.
func SetFromEnv(getenv func(string) string) Set {
	if getenv("PENATES_ASCII") == "1" {
		return ASCIISet
	}
	return UnicodeSet
}

// colorRole is a role in the palette of the visual language. Roles resolve
// through the theme, which follows the terminal background: light or dark
// (3.3).
type colorRole int

const (
	roleDim    colorRole = iota // secondary information (3.3)
	roleGreen                   // installed
	roleCyan                    // update available
	roleYellow                  // modified, warning, orphan
	roleRed                     // conflict, broken, removal
	roleAccent                  // a selected action (3.2)
)

// palette adapts each role to the theme: a color for the light and one for
// the dark terminal background (3.3).
var palette = [...]lipgloss.AdaptiveColor{
	roleDim:    {Light: "#59636e", Dark: "#8b949e"},
	roleGreen:  {Light: "#1a7f37", Dark: "#3fb950"},
	roleCyan:   {Light: "#1b7c83", Dark: "#76e3ea"},
	roleYellow: {Light: "#9a6700", Dark: "#d29922"},
	roleRed:    {Light: "#cf222e", Dark: "#f85149"},
	roleAccent: {Light: "#8250df", Dark: "#a371f7"},
}

// gloss styles text in the color of the role. The color profile of the
// terminal degrades or strips the color: under NO_COLOR the text renders
// plain (3.3).
func gloss(role colorRole) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(palette[role])
}

// Dim renders secondary information — paths, counts, tags — in the dim color
// (3.3); under NO_COLOR it is the text itself.
func Dim(s string) string { return gloss(roleDim).Render(s) }

// mark is one entry of the visual language: the glyph of each set and the
// color role it renders in (3.1, 3.2).
type mark struct {
	unicode string
	ascii   string
	role    colorRole
}

// glyph returns the glyph of the mark in the set.
func (m mark) glyph(set Set) string {
	if set == ASCIISet {
		return m.ascii
	}
	return m.unicode
}

// rendered returns the glyph of the mark in the set, in its color; a profile
// without color, as under NO_COLOR, keeps the bare glyph.
func (m mark) rendered(set Set) string {
	return gloss(m.role).Render(m.glyph(set))
}

// Status is the state of an item shown by a glyph and a color together
// (3.1). The tree model maps the states of the scan onto a Status.
type Status int

const (
	// StatusAvailable: not installed.
	StatusAvailable Status = iota
	// StatusInstalled: matches the kit.
	StatusInstalled
	// StatusUpdate: an update is available.
	StatusUpdate
	// StatusDowngrade: a downgrade is available.
	StatusDowngrade
	// StatusModified: modified by the user; kit content unchanged.
	StatusModified
	// StatusConflict.
	StatusConflict
	// StatusBroken: owned content is missing from the target.
	StatusBroken
	// StatusWarning: kit content changed without a version change.
	StatusWarning
	// StatusOrphan: removed from its kit.
	StatusOrphan
	// StatusUnavailable: the kit cannot be read.
	StatusUnavailable
)

// statusMarks are the status glyphs of 3.1: both sets and the colors.
var statusMarks = [...]mark{
	StatusAvailable:   {unicode: "○", ascii: "o", role: roleDim},
	StatusInstalled:   {unicode: "●", ascii: "*", role: roleGreen},
	StatusUpdate:      {unicode: "↑", ascii: "^", role: roleCyan},
	StatusDowngrade:   {unicode: "↓", ascii: "v", role: roleDim},
	StatusModified:    {unicode: "~", ascii: "~", role: roleYellow},
	StatusConflict:    {unicode: "✗", ascii: "x", role: roleRed},
	StatusBroken:      {unicode: "!", ascii: "!", role: roleRed},
	StatusWarning:     {unicode: "△", ascii: "?", role: roleYellow},
	StatusOrphan:      {unicode: "◌", ascii: ".", role: roleYellow},
	StatusUnavailable: {unicode: "⊘", ascii: "#", role: roleDim},
}

// Glyph returns the status glyph of the set (3.1), uncolored.
func (s Status) Glyph(set Set) string { return statusMarks[s].glyph(set) }

// Mark returns the status glyph of the set in its color (3.1).
func (s Status) Mark(set Set) string { return statusMarks[s].rendered(set) }

// Pending is a pending action shown in a column at the right edge of the
// tree, separate from the status (3.2). Install, update and downgrade share
// a mark; the exact verb is shown in the details pane and in the summary.
type Pending int

const (
	// PendingApply: install, update or downgrade.
	PendingApply Pending = iota
	// PendingReset: reset to kit.
	PendingReset
	// PendingRemove.
	PendingRemove
)

// pendingMarks are the pending glyphs of 3.2: both sets and the colors.
var pendingMarks = [...]mark{
	PendingApply:  {unicode: "✓", ascii: "+", role: roleAccent},
	PendingReset:  {unicode: "↺", ascii: "r", role: roleAccent},
	PendingRemove: {unicode: "−", ascii: "-", role: roleRed},
}

// Glyph returns the pending glyph of the set (3.2), uncolored.
func (p Pending) Glyph(set Set) string { return pendingMarks[p].glyph(set) }

// Mark returns the pending glyph of the set in its color (3.2).
func (p Pending) Mark(set Set) string { return pendingMarks[p].rendered(set) }

// SpinnerFrames returns the frames of the progress spinner (10.2): braille
// frames in the Unicode set, the ASCII fallback "| / - \" of 3.3 in the
// ASCII set.
func SpinnerFrames(set Set) []string {
	if set == ASCIISet {
		return []string{"|", "/", "-", "\\"}
	}
	return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
}
