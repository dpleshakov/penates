package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
)

// fixtureTarget is the current target of the fixture screens: long enough
// that the narrow layout must shorten it in the header.
const fixtureTarget = "~/projects/config-collection/personal-main/opencode-target"

// fixtureHeaderChrome is the header content around the target path: the
// leading " penates  " and the trailing " ▾".
const fixtureHeaderChrome = len(" penates  ") + len(" ▾")

// fixtureHeader composes the header line the screens render: the name, the
// target path shortened from the left when it does not fit, and the right
// side of the band (4.1–4.3).
func fixtureHeader(l Layout) string {
	right := "3 pending · ? help"
	if l.Mode == ModeNarrow {
		right = "3 pending"
	}
	pathBudget := l.Width - ansi.StringWidth(right) - fixtureHeaderChrome
	left := " penates  " + ShortenLeft(fixtureTarget, pathBudget) + " ▾"
	return JoinSides(left, right, l.Width)
}

// fixtureFooter composes the footer line of the band (4.1–4.3).
func fixtureFooter(l Layout) string {
	left := "space unselect · d remove · enter details · / filter · a apply"
	right := "q quit"
	if l.Mode == ModeWide {
		left += " · ctrl+p commands"
	}
	if l.Mode == ModeNarrow {
		left = "space · d · enter · / · a apply"
		right = "? help"
	}
	return JoinSides(left, right, l.Width)
}

// fixtureKitRow composes a kit row: mark, name, summary, path (3.4).
func fixtureKitRow(mark, name, summary, path string) string {
	return "  " + mark + " " + name + "  " + summary + "  " + path
}

// fixtureItemRow composes an item row per the layout's columns: status, id,
// tags, version, description and the pending mark at the right edge of the
// tree (3.4). The columns that the band drops stay out.
func fixtureItemRow(l Layout, status, id, tags, installed, kitVer, description, pending string) string {
	s := "  " + status + " " + id
	if l.Columns.Tags && tags != "" {
		s += "  " + tags
	}
	version := kitVer
	if installed != kitVer {
		version = FitVersion(installed, kitVer, l.TreeWidth-ansi.StringWidth(s))
	}
	s += "  " + version
	if l.Columns.Description && description != "" {
		s += "  " + description
	}
	if pending != "" {
		s = pad(s, l.TreeWidth-ansi.StringWidth(pending)) + pending
	}
	return s
}

// fixtureRows composes the synthetic body of the fixture screens: the shape
// the tree model of the tree task will produce.
func fixtureRows(l Layout) []string {
	return []string{
		fixtureKitRow("▾", "personal-opencode", "●2 ↑1 ✗1", "~/kits/opencode"),
		fixtureItemRow(l, "●", "writing", "", "1.0.0", "1.0.0", "Text editing skill", ""),
		fixtureItemRow(l, "↑", "done-v2", "opencode-v2", "1.2.0", "1.3.0", "Marks finished sessions", "✓"),
		fixtureItemRow(l, "✗", "git-via-gh", "gh", "1.2.0", "1.2.0", "AGENTS.md heading Git usage exists", ""),
		fixtureItemRow(l, "○", "done-v1", "opencode-v1", "1.0.0", "1.0.0", "Marks finished sessions", ""),
		fixtureKitRow("▾", "team-opencode", "○1 ◌1", "~/kits/team"),
		fixtureItemRow(l, "○", "review", "", "0.3.0", "0.3.0", "Review agent with team checklist", "✓"),
		fixtureItemRow(l, "◌", "old-lint", "", "0.2.0", "0.2.0", "Removed from kit", "−"),
		fixtureKitRow("▸", "old-kit", "⊘ unreadable", "~/kits/old"),
		"  + add kit",
	}
}

// fixtureDetails composes the pane content for the row under the cursor, as
// the details renderer will; the prose wraps at the pane width.
func fixtureDetails(l Layout) []string {
	lines := []string{
		"done-v2 · personal-opencode",
		"",
	}
	lines = append(lines, WrapProse("Marks the session name with [DONE] on the /done command.", l.DetailsWidth)...)
	return append(lines,
		"",
		"Status    ↑ update, 1.2.0 → 1.3.0 (minor)",
		"",
		"Pending   ✓ update to 1.3.0",
		"",
		"Tags      opencode-v2",
		"",
		"Contributes",
		"  file  plugin/done.ts",
		"  json  opencode.json  /plugin",
		"",
		"Changelog",
		"  1.3.0  Supports custom marker text",
		"  1.2.0  Initial release for OpenCode v2",
	)
}

// fixtureFrame composes the synthetic main screen the golden tests render,
// after the mockups of 4.1–4.3; the cursor sits on the done-v2 row.
func fixtureFrame(l Layout) Frame {
	f := Frame{
		Header: fixtureHeader(l),
		Footer: fixtureFooter(l),
		Rows:   fixtureRows(l),
		Cursor: 2,
	}
	if l.Mode == ModeWide {
		f.Details = fixtureDetails(l)
	}
	return f
}

// assertRegions checks the chrome the engine fixes: the total height, the
// header line, the blanks around the body, and the footer line.
func assertRegions(t *testing.T, l Layout, screen string) {
	t.Helper()
	lines := strings.Split(screen, "\n")
	if len(lines) != l.Height {
		t.Fatalf("lines = %d, want %d", len(lines), l.Height)
	}
	if !strings.HasPrefix(lines[0], " penates") {
		t.Errorf("header line = %q, want the penates header", lines[0])
	}
	if lines[1] != "" {
		t.Errorf("no blank under the header: %q", lines[1])
	}
	if lines[l.Height-2] != "" {
		t.Errorf("no blank over the footer: %q", lines[l.Height-2])
	}
	if !strings.Contains(lines[l.Height-1], "apply") {
		t.Errorf("footer line = %q", lines[l.Height-1])
	}
}

func TestLayoutBreakpoints(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		mode          Mode
		tags          bool
		description   bool
		details       int
		treeWidth     int
	}{
		{"wide", 120, 24, ModeWide, true, false, 48, 71},
		{"wide at the pane minimum", 110, 16, ModeWide, true, false, MinDetailsWidth, 110 - MinDetailsWidth - 1},
		{"wide shares the width", 150, 20, ModeWide, true, false, 60, 89},
		{"medium", 109, 16, ModeMedium, true, true, 0, 109},
		{"medium lower edge", 80, 16, ModeMedium, true, true, 0, 80},
		{"narrow", 79, 16, ModeNarrow, false, false, 0, 79},
		{"narrow lower edge", 60, 16, ModeNarrow, false, false, 0, 60},
		{"below width", 59, 16, ModeTooSmall, false, false, 0, 0},
		{"below rows", 60, 15, ModeTooSmall, false, false, 0, 0},
		{"below both", 48, 20, ModeTooSmall, false, false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewLayout(tt.width, tt.height)
			if l.Mode != tt.mode || l.Mode.String() == "unknown" {
				t.Errorf("mode = %v, want %v", l.Mode, tt.mode)
			}
			if l.Columns.Tags != tt.tags || l.Columns.Description != tt.description {
				t.Errorf("columns = %+v, want tags %v, description %v", l.Columns, tt.tags, tt.description)
			}
			if l.DetailsWidth != tt.details {
				t.Errorf("details width = %d, want %d", l.DetailsWidth, tt.details)
			}
			if l.TreeWidth != tt.treeWidth {
				t.Errorf("tree width = %d, want %d", l.TreeWidth, tt.treeWidth)
			}
		})
	}
}

func TestRenderRegions(t *testing.T) {
	l := NewLayout(90, 20)
	f := Frame{
		Header: "header line",
		Footer: "footer line",
		Rows:   []string{"row one", "row two", "row three"},
		Input:  "/ filter",
		Notice: "notice line",
	}
	lines := strings.Split(l.Render(f), "\n")
	if len(lines) != l.Height {
		t.Fatalf("lines = %d, want %d", len(lines), l.Height)
	}
	if lines[0] != "header line" || lines[1] != "" {
		t.Errorf("header region = %q, %q", lines[0], lines[1])
	}
	// The viewport is 20 - 4 - 2 = 14 rows; the three body rows come first.
	if lines[2] != "row one" || lines[3] != "row two" || lines[4] != "row three" || lines[5] != "" {
		t.Errorf("body region starts with %q, %q, %q, %q", lines[2], lines[3], lines[4], lines[5])
	}
	if lines[16] != "/ filter" {
		t.Errorf("input line = %q, want it under the body", lines[16])
	}
	if lines[17] != "notice line" {
		t.Errorf("notice line = %q, want it under the input", lines[17])
	}
	if lines[18] != "" || lines[19] != "footer line" {
		t.Errorf("footer region = %q, %q", lines[18], lines[19])
	}

	// Without input and notice the lines return to the body.
	f.Input, f.Notice = "", ""
	lines = strings.Split(l.Render(f), "\n")
	if len(lines) != l.Height {
		t.Fatalf("lines = %d, want %d", len(lines), l.Height)
	}
	if lines[16] != "" || lines[17] != "" {
		t.Errorf("lines under the body = %q, %q, want empty", lines[16], lines[17])
	}
	if lines[19] != "footer line" {
		t.Errorf("footer line = %q", lines[19])
	}
}

func TestRenderGoldenWide(t *testing.T) {
	l := NewLayout(120, 24)
	got := l.Render(fixtureFrame(l))
	assertRegions(t, l, got)
	if diff := cmp.Diff(wantGoldenWide, got); diff != "" {
		t.Errorf("wide screen (-want +got):\n%s", diff)
	}
	lines := strings.Split(got, "\n")
	// The cursor row is the third body row.
	if !strings.Contains(lines[4], "done-v2") {
		t.Errorf("cursor row out of view: %q", lines[4])
	}
	// The separator runs down the body at the tree width.
	for i := 2; i < l.Height-2; i++ {
		runes := []rune(lines[i])
		if len(runes) < l.TreeWidth+1 || runes[l.TreeWidth] != '│' {
			t.Errorf("body line %d has no separator at column %d: %q", i, l.TreeWidth, lines[i])
		}
	}
	// The tree drops the description column in favor of the pane.
	if strings.Contains(got, "Marks finished sessions") {
		t.Errorf("the tree carries the description column in the wide layout")
	}
	// The pane carries the description, wrapped at the pane width: the first
	// pane line ends with the wrap, the next pane line has the rest.
	if !strings.Contains(got, "Marks the session name with [DONE] on the /done\n") ||
		!strings.Contains(got, "│command.") {
		t.Errorf("pane prose is not wrapped at the pane width")
	}
}

func TestRenderGoldenMedium(t *testing.T) {
	l := NewLayout(90, 20)
	got := l.Render(fixtureFrame(l))
	assertRegions(t, l, got)
	if diff := cmp.Diff(wantGoldenMedium, got); diff != "" {
		t.Errorf("medium screen (-want +got):\n%s", diff)
	}
	if strings.ContainsRune(got, '│') {
		t.Errorf("the medium layout shows no details pane")
	}
	// The description column and the tags column are present, the version
	// column shows the full pair.
	for _, want := range []string{"opencode-v2", "Marks finished sessions", "1.2.0 → 1.3.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("medium screen misses %q", want)
		}
	}
	// The target path fits and is not shortened.
	if strings.ContainsRune(got, '…') {
		t.Errorf("medium screen shortens the target path although it fits")
	}
}

func TestRenderGoldenNarrow(t *testing.T) {
	l := NewLayout(64, 16)
	got := l.Render(fixtureFrame(l))
	assertRegions(t, l, got)
	if diff := cmp.Diff(wantGoldenNarrow, got); diff != "" {
		t.Errorf("narrow screen (-want +got):\n%s", diff)
	}
	if strings.ContainsRune(got, '│') {
		t.Errorf("the narrow layout shows no details pane")
	}
	// The header shortens the target path from the left, keeping the deepest
	// directories.
	if !strings.Contains(got, "…/personal-main/opencode-target") {
		t.Errorf("narrow header does not shorten the target path")
	}
	if strings.Contains(got, fixtureTarget) {
		t.Errorf("narrow header carries the full target path")
	}
	// Tags and descriptions drop.
	for _, absent := range []string{"opencode-v2", "Marks finished sessions", "Removed from kit"} {
		if strings.Contains(got, absent) {
			t.Errorf("narrow screen carries %q", absent)
		}
	}
	// The version column keeps the full pair when it fits (4.3).
	if !strings.Contains(got, "1.2.0 → 1.3.0") {
		t.Errorf("narrow screen misses the full version pair although it fits")
	}
}

func TestRenderTooSmall(t *testing.T) {
	for _, tt := range []struct {
		width, height int
		current       string
	}{
		{48, 20, "48 × 20"},
		{70, 9, "70 × 9"},
	} {
		t.Run(fmt.Sprintf("%dx%d", tt.width, tt.height), func(t *testing.T) {
			l := NewLayout(tt.width, tt.height)
			if l.Mode != ModeTooSmall {
				t.Fatalf("mode = %v, want tooSmall", l.Mode)
			}
			got := l.Render(Frame{Header: "header", Footer: "footer", Rows: []string{"row"}})
			want := "  penates needs at least\n  60 columns and 16 rows.\n\n  Current size: " + tt.current + "."
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("minimum-size message (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBodyScrolling(t *testing.T) {
	l := NewLayout(90, 20) // the viewport is 16 rows
	rows := make([]string, 40)
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%02d", i)
	}
	render := func(cursor int) []string {
		t.Helper()
		lines := strings.Split(l.Render(Frame{Header: "h", Footer: "f", Rows: rows, Cursor: cursor}), "\n")
		if len(lines) != l.Height {
			t.Fatalf("lines = %d, want %d", len(lines), l.Height)
		}
		return lines[2 : len(lines)-2]
	}
	assertWindow := func(cursor int, first, last, absent string) {
		t.Run(fmt.Sprintf("cursor %d", cursor), func(t *testing.T) {
			body := render(cursor)
			if body[0] != first || body[len(body)-1] != last {
				t.Errorf("window = %q … %q, want %q … %q", body[0], body[len(body)-1], first, last)
			}
			if absent != "" && strings.Contains(strings.Join(body, "\n"), absent) {
				t.Errorf("window shows %q", absent)
			}
		})
	}
	// The cursor on the first row opens the window at the top.
	assertWindow(0, "row-00", "row-15", "row-16")
	// One row past the window: the window scrolls with the cursor.
	assertWindow(16, "row-01", "row-16", "row-00")
	// The cursor within the window does not scroll.
	assertWindow(5, "row-00", "row-15", "")
	// The cursor at the end snaps the window to the last page.
	assertWindow(39, "row-24", "row-39", "row-23")
	// Moving up within the last page keeps the page.
	assertWindow(30, "row-15", "row-30", "")
}

func TestResizeRelayout(t *testing.T) {
	rows := make([]string, 30)
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%02d", i)
	}
	const cursor = 20
	for _, size := range []struct {
		width, height int
		mode          Mode
	}{
		{90, 20, ModeMedium},
		{120, 24, ModeWide},
		{64, 16, ModeNarrow},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			l := NewLayout(size.width, size.height)
			if l.Mode != size.mode {
				t.Fatalf("mode = %v, want %v", l.Mode, size.mode)
			}
			f := fixtureFrame(l)
			f.Rows, f.Cursor = rows, cursor
			got := l.Render(f)
			assertRegions(t, l, got)
			lines := strings.Split(got, "\n")
			body := strings.Join(lines[2:len(lines)-2], "\n")
			if !strings.Contains(body, "row-20") {
				t.Errorf("the cursor row is out of view after the resize")
			}
			if strings.Contains(body, "row-00") {
				t.Errorf("the window did not scroll with the cursor")
			}
			// The layout re-computed: the pane appears and disappears with
			// the width.
			if has := strings.ContainsRune(got, '│'); has != (l.Mode == ModeWide) {
				t.Errorf("separator present = %v, want %v", has, l.Mode == ModeWide)
			}
		})
	}
}

func TestNoticeKeyAndTimeout(t *testing.T) {
	var n Notice
	if n.Visible() {
		t.Fatalf("fresh notice = %q, want none", n.Text())
	}
	l := NewLayout(90, 20)
	n.Show("Detached personal-claude.")
	if !n.Visible() || n.Text() != "Detached personal-claude." {
		t.Fatalf("shown notice = %q, visible = %v", n.Text(), n.Visible())
	}
	with := l.Render(Frame{Header: "h", Footer: "f", Notice: n.Text()})
	if !strings.Contains(with, "Detached personal-claude.") {
		t.Errorf("screen misses the notice:\n%s", with)
	}

	// The next key clears it; the line returns to the body.
	n.Clear()
	if n.Visible() {
		t.Fatalf("cleared notice = %q, want none", n.Text())
	}
	without := l.Render(Frame{Header: "h", Footer: "f"})
	if strings.Contains(without, "Detached personal-claude.") {
		t.Errorf("screen keeps the notice after the clear")
	}

	// The timeout clears it: still visible shortly before, gone at the time.
	n.Show("Rescanned 2 kits.")
	n.Tick(time.Now().Add(noticeTTL - time.Second))
	if !n.Visible() {
		t.Errorf("notice cleared before its time was up")
	}
	n.Tick(time.Now().Add(noticeTTL + time.Second))
	if n.Visible() {
		t.Errorf("notice survived its time to live")
	}
}

func TestShortenLeft(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"~/.config/opencode", 30, "~/.config/opencode"},
		{"~/.config/opencode", 18, "~/.config/opencode"},
		{"~/.config/opencode", 10, "…/opencode"},
		{"~/projects/config-collection/personal-main/opencode-target", 43, "…/personal-main/opencode-target"},
		{"~/a/b/c/d/e", 6, "…/d/e"},
		{"~/a/b/c/d/e", 2, "…e"},
		{"~/some/dir", 1, "…"},
		{"~/some/dir", 0, ""},
	}
	for _, tt := range tests {
		if got := ShortenLeft(tt.in, tt.width); got != tt.want {
			t.Errorf("ShortenLeft(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

func TestWrapProse(t *testing.T) {
	t.Run("caps at 100 columns regardless of the width", func(t *testing.T) {
		text := strings.Repeat("word ", 30) // 150 columns unwrapped
		for _, avail := range []int{130, 200, 1000} {
			for _, line := range WrapProse(text, avail) {
				if ansi.StringWidth(line) > ProseLimit {
					t.Errorf("WrapProse(_, %d) wrapped a line of %d columns", avail, ansi.StringWidth(line))
				}
			}
		}
	})
	t.Run("fits on one line", func(t *testing.T) {
		got := WrapProse("one two three", 100)
		if len(got) != 1 || got[0] != "one two three" {
			t.Errorf("WrapProse = %q, want one unchanged line", got)
		}
	})
	t.Run("breaks words that do not fit", func(t *testing.T) {
		got := WrapProse(strings.Repeat("a", 120), 100)
		if len(got) != 2 || got[0] != strings.Repeat("a", 100) || got[1] != strings.Repeat("a", 20) {
			t.Errorf("WrapProse = %d lines, want a hard break at the width", len(got))
		}
	})
	t.Run("wraps at the pane width", func(t *testing.T) {
		got := WrapProse("Marks the session name with [DONE] on the /done command.", 48)
		want := []string{"Marks the session name with [DONE] on the /done", "command."}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("WrapProse (-want +got):\n%s", diff)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := WrapProse("", 80); got != nil {
			t.Errorf("WrapProse(\"\", 80) = %q, want no lines", got)
		}
	})
}

func TestFitVersion(t *testing.T) {
	tests := []struct {
		installed, kit string
		width          int
		want           string
	}{
		{"1.2.0", "1.3.0", 13, "1.2.0 → 1.3.0"},
		{"1.2.0", "1.3.0", 12, "1.3.0"},
		{"0.10.0", "0.11.0", 15, "0.10.0 → 0.11.0"},
		{"0.10.0", "0.11.0", 14, "0.11.0"},
	}
	for _, tt := range tests {
		if got := FitVersion(tt.installed, tt.kit, tt.width); got != tt.want {
			t.Errorf("FitVersion(%q, %q, %d) = %q, want %q", tt.installed, tt.kit, tt.width, got, tt.want)
		}
	}
}

func TestJoinSides(t *testing.T) {
	tests := []struct {
		left, right string
		width       int
		want        string
	}{
		{"left", "right", 10, "left right"},
		{"left", "right", 12, "left   right"},
		{"a-loooong-left-side", "right", 10, "a-looright"},
		{"a", "a-way-too-long-right", 3, "a-w"},
	}
	for _, tt := range tests {
		if got := JoinSides(tt.left, tt.right, tt.width); got != tt.want {
			t.Errorf("JoinSides(%q, %q, %d) = %q, want %q", tt.left, tt.right, tt.width, got, tt.want)
		}
	}
}
