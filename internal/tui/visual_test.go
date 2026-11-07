package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
	"github.com/muesli/termenv"
)

// forceVisual pins the color profile and the background of the default
// renderer for the test and restores both afterwards. Tests render without a
// TTY, so the profile is forced on purpose: colors through TrueColor,
// NO_COLOR through the empty Ascii profile, and each theme through its
// background (3.3).
func forceVisual(t *testing.T, profile termenv.Profile, darkBackground bool) {
	t.Helper()
	r := lipgloss.DefaultRenderer()
	oldProfile, oldDark := r.ColorProfile(), r.HasDarkBackground()
	t.Cleanup(func() {
		r.SetColorProfile(oldProfile)
		r.SetHasDarkBackground(oldDark)
	})
	r.SetColorProfile(profile)
	r.SetHasDarkBackground(darkBackground)
}

// marked is a glyph of the visual language: its bare glyph per set and its
// colored form.
type marked interface {
	Glyph(Set) string
	Mark(Set) string
}

// markSpec transcribes one row of the tables of 3.1 or 3.2: the glyph of
// each set and the color role the mark renders in.
type markSpec struct {
	name         string
	mark         marked
	unicodeGlyph string
	asciiGlyph   string
	role         colorRole
}

// statusSpec is the status table of 3.1.
var statusSpec = []markSpec{
	{"available", StatusAvailable, "○", "o", roleDim},
	{"installed", StatusInstalled, "●", "*", roleGreen},
	{"update", StatusUpdate, "↑", "^", roleCyan},
	{"downgrade", StatusDowngrade, "↓", "v", roleDim},
	{"modified", StatusModified, "~", "~", roleYellow},
	{"conflict", StatusConflict, "✗", "x", roleRed},
	{"broken", StatusBroken, "!", "!", roleRed},
	{"warning", StatusWarning, "△", "?", roleYellow},
	{"orphan", StatusOrphan, "◌", ".", roleYellow},
	{"unavailable", StatusUnavailable, "⊘", "#", roleDim},
}

// pendingSpec is the pending-action table of 3.2.
var pendingSpec = []markSpec{
	{"apply", PendingApply, "✓", "+", roleAccent},
	{"reset", PendingReset, "↺", "r", roleAccent},
	{"remove", PendingRemove, "−", "-", roleRed},
}

// sets are both glyph sets, in table order.
var sets = []Set{UnicodeSet, ASCIISet}

// runMarkTable covers one mark table in both glyph sets: the bare glyphs of
// the spec row, the colored form through the forced TrueColor profile (tests
// have no TTY), and the form under NO_COLOR through the empty profile.
func runMarkTable(t *testing.T, table []markSpec) {
	t.Helper()
	for _, tc := range table {
		t.Run(tc.name+"/glyph", func(t *testing.T) {
			want := map[Set]string{UnicodeSet: tc.unicodeGlyph, ASCIISet: tc.asciiGlyph}
			for _, set := range sets {
				if diff := cmp.Diff(want[set], tc.mark.Glyph(set)); diff != "" {
					t.Errorf("Glyph(%v) mismatch (-want +got):\n%s", set, diff)
				}
			}
		})
		t.Run(tc.name+"/colored", func(t *testing.T) {
			forceVisual(t, termenv.TrueColor, true)
			for _, set := range sets {
				glyph := tc.mark.Glyph(set)
				want := lipgloss.NewStyle().Foreground(palette[tc.role]).Render(glyph)
				if diff := cmp.Diff(want, tc.mark.Mark(set)); diff != "" {
					t.Errorf("Mark(%v) mismatch (-want +got):\n%s", set, diff)
				}
			}
		})
		t.Run(tc.name+"/no-color", func(t *testing.T) {
			forceVisual(t, termenv.Ascii, true)
			for _, set := range sets {
				if diff := cmp.Diff(tc.mark.Glyph(set), tc.mark.Mark(set)); diff != "" {
					t.Errorf("Mark(%v) under NO_COLOR mismatch (-want +got):\n%s", set, diff)
				}
			}
		})
	}
}

// TestStatusMarks covers every status mark of 3.1 in both sets.
func TestStatusMarks(t *testing.T) {
	runMarkTable(t, statusSpec)
}

// TestPendingMarks covers every pending mark of 3.2 in both sets.
func TestPendingMarks(t *testing.T) {
	runMarkTable(t, pendingSpec)
}

// pathMark adapts one mark of the apply screens to the table.
type pathMark struct{ m mark }

func (p pathMark) Glyph(set Set) string { return p.m.glyph(set) }
func (p pathMark) Mark(set Set) string  { return p.m.rendered(set) }

// pathSpec is the per-path mark table of 10.1, with the done mark of 10.2
// and 10.3.
var pathSpec = []markSpec{
	{"create", pathMark{markCreate}, "+", "+", roleGreen},
	{"replace", pathMark{markReplace}, "~", "~", roleYellow},
	{"delete", pathMark{markDelete}, "-", "-", roleRed},
	{"keep", pathMark{markKeep}, "=", "=", roleDim},
	{"release", pathMark{markRelease}, "!", "!", roleYellow},
	{"done", pathMark{doneMark}, "✓", "+", roleGreen},
}

// TestPathMarks covers every mark of the apply screens in both sets.
func TestPathMarks(t *testing.T) {
	runMarkTable(t, pathSpec)
}

// TestSetFromEnv selects the ASCII set only on PENATES_ASCII=1 (3.3); there
// is no automatic detection (backlog ascii-detect).
func TestSetFromEnv(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  Set
	}{
		{"", UnicodeSet},
		{"0", UnicodeSet},
		{"true", UnicodeSet},
		{"1", ASCIISet},
	} {
		t.Run("PENATES_ASCII="+tc.value, func(t *testing.T) {
			getenv := func(string) string { return tc.value }
			if diff := cmp.Diff(tc.want, SetFromEnv(getenv)); diff != "" {
				t.Errorf("SetFromEnv mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestThemeFollowsBackground asserts each palette role renders its dark
// color on a dark background and its light color on a light one; both
// themes are forced (3.3).
func TestThemeFollowsBackground(t *testing.T) {
	for _, tc := range []struct {
		name string
		role colorRole
	}{
		{"dim", roleDim},
		{"green", roleGreen},
		{"cyan", roleCyan},
		{"yellow", roleYellow},
		{"red", roleRed},
		{"accent", roleAccent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adaptive := palette[tc.role]
			forceVisual(t, termenv.TrueColor, true)
			dark := gloss(tc.role).Render("x")
			wantDark := lipgloss.NewStyle().Foreground(lipgloss.Color(adaptive.Dark)).Render("x")
			if diff := cmp.Diff(wantDark, dark); diff != "" {
				t.Errorf("dark theme mismatch (-want +got):\n%s", diff)
			}
			forceVisual(t, termenv.TrueColor, false)
			light := gloss(tc.role).Render("x")
			wantLight := lipgloss.NewStyle().Foreground(lipgloss.Color(adaptive.Light)).Render("x")
			if diff := cmp.Diff(wantLight, light); diff != "" {
				t.Errorf("light theme mismatch (-want +got):\n%s", diff)
			}
			if dark == light {
				t.Error("dark and light renders are identical; the theme does not follow the background")
			}
		})
	}
}

// TestDim renders secondary information dim when color is available, and
// plain under NO_COLOR (3.3).
func TestDim(t *testing.T) {
	const text = "~/kits/opencode"
	forceVisual(t, termenv.TrueColor, true)
	if Dim(text) == text {
		t.Error("Dim renders plain although color is forced")
	}
	forceVisual(t, termenv.Ascii, true)
	if diff := cmp.Diff(text, Dim(text)); diff != "" {
		t.Errorf("Dim under NO_COLOR mismatch (-want +got):\n%s", diff)
	}
}

// TestSpinnerFrames covers the progress spinner frames of 10.2: braille
// frames in the Unicode set, "| / - \" in the ASCII set (3.3). Every frame
// is one column wide so the progress line keeps its layout.
func TestSpinnerFrames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		set    Set
		frames []string
	}{
		{"unicode", UnicodeSet, []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}},
		{"ascii", ASCIISet, []string{"|", "/", "-", "\\"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := SpinnerFrames(tc.set)
			if diff := cmp.Diff(tc.frames, frames); diff != "" {
				t.Errorf("SpinnerFrames mismatch (-want +got):\n%s", diff)
			}
			for _, frame := range frames {
				if w := ansi.StringWidth(frame); w != 1 {
					t.Errorf("spinner frame %q is %d columns wide, want 1", frame, w)
				}
			}
		})
	}
}
