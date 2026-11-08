package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The rendering of the tree rows into the body lines of one screen: the
// model gives the structure, the layout's columns and the glyph set give
// the line — render is a pure function of (rows, Layout, Set).

// The expansion marks of the kit rows (3.4): ▾ expanded, ▸ collapsed. The
// mark tables of 3.1 and 3.2 carry no fallback for them, so the ASCII set
// spells them in letters, as it spells the spinner (3.3). They render
// uncolored: the mark carries the structure, not a state.
var (
	expandedMark  = mark{unicode: "▾", ascii: "v", role: roleDim}
	collapsedMark = mark{unicode: "▸", ascii: ">", role: roleDim}
)

// AttachBuiltinLabel is the label of the first-run offer row of the
// built-in kit (6.1).
const AttachBuiltinLabel = "+ attach the built-in kit penates"

// RenderTree renders the tree rows into body lines, one line per row,
// clipped to the layout's tree width. The columns the layout drops stay
// out (2.2); the version column shrinks to the kit version when the pair
// does not fit; the pending mark sits flush to the right edge of the tree
// (3.2). Glyphs and colors come from the set.
func RenderTree(rows []Row, l Layout, set Set) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		var line string
		switch r.Kind {
		case KindKit:
			line = renderKitRow(r.Kit, set)
		case KindItem:
			line = renderItemRow(r.Item, l, set)
		case KindAttachBuiltin:
			line = "  " + AttachBuiltinLabel
		default:
			line = "  " + AddKitLabel
		}
		out = append(out, cut(line, l.TreeWidth))
	}
	return out
}

// renderKitRow renders one group header: expansion mark, name, the summary
// slot, and the path (3.4). The slot holds the glyph summary, the match
// counts of an active filter (7.5), "unreadable" for a kit that did not
// load (6.6), or "detaching" for a kit waiting for its removals to apply
// (6.7).
func renderKitRow(g KitRow, set Set) string {
	m := expandedMark
	if g.Collapsed {
		m = collapsedMark
	}
	s := "  " + m.glyph(set) + " " + g.Name
	switch {
	case g.Unreadable:
		s += "  " + StatusUnavailable.Mark(set) + " unreadable"
	case g.Detaching:
		s += "  " + Dim("detaching")
	case g.Modified:
		if g.UpdateBlocked {
			s += "  " + StatusModified.Mark(set) + " " + Dim("modified locally — update blocked")
		} else {
			s += "  " + StatusModified.Mark(set) + " " + Dim("modified locally")
		}
	case g.Filtering:
		s += "  " + Dim(fmt.Sprintf("%d of %d", g.Matches, g.Total))
	default:
		s += "  " + summaryGlyphs(g.Summary, set)
	}
	if g.Path != "" {
		s += "  " + Dim(g.Path)
	}
	return s
}

// summaryGlyphs renders the summary of 3.4: each status glyph with its
// count, the glyph in its color, the entries in the order of 3.1.
func summaryGlyphs(summary []GlyphCount, set Set) string {
	parts := make([]string, 0, len(summary))
	for _, gc := range summary {
		parts = append(parts, gc.Status.Mark(set)+strconv.Itoa(gc.Count))
	}
	return strings.Join(parts, " ")
}

// renderItemRow renders one item line: status glyph, id, tags, version,
// description — or the reason of a conflict, broken or warning item — and
// the pending mark flush to the right edge of the tree (3.2, 3.4).
func renderItemRow(r ItemRow, l Layout, set Set) string {
	s := "  " + r.Status.Mark(set) + " " + r.ID
	if tags := strings.Join(r.Tags, " "); l.Columns.Tags && tags != "" {
		s += "  " + Dim(tags)
	}
	if v := versionCell(r, l.TreeWidth-ansi.StringWidth(s)); v != "" {
		s += "  " + v
	}
	if d := rowDescription(r); l.Columns.Description && d != "" {
		s += "  " + d
	}
	if r.Pending == nil {
		return cut(s, l.TreeWidth)
	}
	return JoinSides(s, r.Pending.Mark(set), l.TreeWidth)
}

// versionCell renders the version column for the width: "installed → kit"
// when they differ, shrunk to the kit version alone when the pair does not
// fit (2.2, 3.4).
func versionCell(r ItemRow, width int) string {
	switch {
	case !r.Installed:
		return cut(r.KitVersion, width)
	case r.KitVersion == "" || r.KitVersion == r.InstalledVersion:
		return cut(r.InstalledVersion, width)
	}
	return FitVersion(r.InstalledVersion, r.KitVersion, width)
}

// rowDescription is the text of the description column: the reason of a
// conflict, broken or warning item stands in for the description (3.4).
func rowDescription(r ItemRow) string {
	switch r.Status {
	case StatusConflict, StatusBroken, StatusWarning:
		if r.Reason != "" {
			return r.Reason
		}
	}
	return r.Description
}
