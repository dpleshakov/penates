package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
	"github.com/muesli/termenv"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
)

// Fixtures for the details renderers: the conflict, modified and major
// examples of 5.1 and the kit of 5.2, built from the values the tree input
// carries. The tests force NO_COLOR, so the goldens hold bare glyphs.

// conflictItem is the git-via-gh of 5.1: an installed item whose section
// contribution refuses to write over user content, with variables answered
// in the lock and at selection, and a changelog.
func conflictItem() ItemDetails {
	confl := plan.Conflict{
		Kind:    kit.KindSection,
		File:    "AGENTS.md",
		Heading: "Git usage",
		Cause:   plan.CauseUserContent,
	}
	return ItemDetails{
		Row: ItemRow{
			Key:              "personal-opencode/git-via-gh",
			KitName:          "personal-opencode",
			ID:               "git-via-gh",
			Status:           StatusConflict,
			Reason:           renderConflict(confl),
			Tags:             []string{"gh"},
			Installed:        true,
			InstalledVersion: "1.2.0",
			KitVersion:       "1.2.0",
			Description:      "Use gh instead of git where git is blocked",
		},
		Item: &kit.Item{
			ID:          "git-via-gh",
			Version:     "1.2.0",
			Description: "Use gh instead of git where git is blocked",
			Tags:        []string{"gh"},
			Vars: []kit.Var{
				{ID: "mode", Prompt: "Permission mode", Required: true},
				{ID: "scope", Prompt: "Scope of the permission"},
			},
			Contributes: []kit.Contribution{
				{Kind: kit.KindFile, Src: "items/git-via-gh/skills/git-via-gh/SKILL.md", To: "skills/git-via-gh/SKILL.md"},
				{Kind: kit.KindSection, Src: "items/git-via-gh/AGENTS.md", To: "AGENTS.md"},
				{Kind: kit.KindJSON, Src: "items/git-via-gh/opencode.json", To: "opencode.json"},
			},
			HasChangelog: true,
		},
		Plan: &plan.ItemPlan{
			Key:       "personal-opencode/git-via-gh",
			Status:    plan.StatusError,
			Conflicts: []plan.Conflict{confl},
		},
		Lock: &lock.Lock{
			Target: treeTarget,
			Items: map[string]*lock.Item{
				"personal-opencode/git-via-gh": {
					Version:  "1.2.0",
					Answers:  map[string]string{"mode": "allow"},
					Files:    map[string]string{"skills/git-via-gh/SKILL.md": sha("skill")},
					Sections: map[string]lock.Section{"AGENTS.md": {Block: "personal-opencode/git-via-gh", Hash: sha("section")}},
					JSON:     map[string][]lock.Location{"opencode.json": {{Pointer: "/permission/bash/gh *", Hash: sha("perm")}}},
				},
			},
		},
		Answers:   map[string]string{"scope": "repo"},
		Changelog: []ChangelogEntry{{Version: "1.2.0", Note: "Adds the gh permission for blocked git remotes"}},
	}
}

// modifiedItem is the done-v1 of 5.1: installed content the user changed,
// with a reset pending.
func modifiedItem() ItemDetails {
	return ItemDetails{
		Row: ItemRow{
			Key:              "personal-opencode/done-v1",
			KitName:          "personal-opencode",
			ID:               "done-v1",
			Status:           StatusModified,
			Installed:        true,
			InstalledVersion: "1.0.0",
			KitVersion:       "1.0.0",
			Description:      "Marks finished sessions",
			Pending:          pend(PendingReset),
		},
		Lock: &lock.Lock{
			Target: treeTarget,
			Items: map[string]*lock.Item{
				"personal-opencode/done-v1": {Version: "1.0.0"},
			},
		},
	}
}

// warningItem is a warning item of 7.1: the kit changed its content
// without a version change, no update is offered. The reason is the plan's;
// warning has no acting key (7.1), so the reason stands without advice.
func warningItem() ItemDetails {
	const reason = "content changed without a version change; no update is offered"
	return ItemDetails{
		Row: ItemRow{
			Key:              "team-opencode/old-lint",
			KitName:          "team-opencode",
			ID:               "old-lint",
			Status:           StatusWarning,
			Reason:           reason,
			Installed:        true,
			InstalledVersion: "0.2.0",
			KitVersion:       "0.2.0",
			Tags:             []string{"lint"},
			Description:      "Lint rules removed from the kit",
		},
		Plan: &plan.ItemPlan{
			Key:     "team-opencode/old-lint",
			Status:  plan.StatusNoAction,
			Warning: true,
			Reason:  reason,
		},
		Changelog: []ChangelogEntry{{Version: "0.2.0", Note: "Rules removed"}},
	}
}

// majorItem is the review of 7.2: a major update under review, selected
// already, with a changelog.
func majorItem() ItemDetails {
	return ItemDetails{
		Row: ItemRow{
			Key:              "team-opencode/review",
			KitName:          "team-opencode",
			ID:               "review",
			Status:           StatusUpdate,
			Installed:        true,
			InstalledVersion: "0.3.0",
			KitVersion:       "1.0.0",
			Major:            true,
			Description:      "Review agent with team checklist",
			Pending:          pend(PendingApply),
		},
		Item: &kit.Item{
			ID:          "review",
			Version:     "1.0.0",
			Description: "Review agent with team checklist",
		},
		Changelog: []ChangelogEntry{
			{Version: "1.0.0", Note: "Checklist rewritten; steps 4-6 removed"},
			{Version: "0.4.0", Note: "Adds security section"},
		},
	}
}

// fixtureKitDetails is the personal-opencode of 5.2, attached to one other
// target besides the current one.
func fixtureKitDetails() KitDetails {
	return KitDetails{
		Row: KitRow{
			Name:  "personal-opencode",
			Path:  "~/kits/opencode",
			Total: 4,
			Summary: []GlyphCount{
				{Status: StatusInstalled, Count: 2},
				{Status: StatusUpdate, Count: 1},
				{Status: StatusConflict, Count: 1},
			},
		},
		Kit: &kit.Kit{
			Name:        "personal-opencode",
			Description: "Personal OpenCode customization",
			TargetHint:  "~/.config/opencode",
			Shared:      []string{"AGENTS.md", "opencode.json"},
		},
		Registry: &state.Registry{
			Attachments: []state.Attachment{
				{Kit: "personal-opencode", Target: treeTarget},
				{Kit: "personal-opencode", Target: "~/wsl/opencode"},
				{Kit: "team-opencode", Target: treeTarget},
			},
		},
		Target: treeTarget,
	}
}

// pend is the pointer a pending action sits behind in a row.
func pend(p Pending) *Pending { return &p }

// noColor pins the plain profile for the duration of one test, so marks
// render as bare glyphs and the goldens hold plain text (3.3).
func noColor(t *testing.T) {
	t.Helper()
	forceVisual(t, termenv.Ascii, true)
}

// TestRenderItemDetailsConflict renders the conflict example of 5.1: the
// sections in order, the reason of the structured cause with the next step
// flowed into it, the stored answers, the contributions with block and
// pointer, and the changelog.
func TestRenderItemDetailsConflict(t *testing.T) {
	noColor(t)
	want := []string{
		"git-via-gh · personal-opencode",
		"",
		"Use gh instead of git where git is blocked",
		"",
		"Status    ✗ conflict",
		`          AGENTS.md already has a heading "Git usage" outside`,
		"          managed blocks; the section would duplicate it. Remove",
		"          or rename it, then rescan with R.",
		"",
		"Tags      gh",
		"",
		"Variables",
		"  mode = allow",
		"  scope = repo",
		"",
		"Contributes",
		"  file     skills/git-via-gh/SKILL.md",
		"  section  AGENTS.md  personal-opencode/git-via-gh",
		"  json     opencode.json  /permission/bash/gh *",
		"",
		"Changelog",
		"  1.2.0  Adds the gh permission for blocked git remotes",
	}
	got := RenderItemDetails(conflictItem(), 64, UnicodeSet)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("item details mismatch (-want +got):\n%s", diff)
	}
}

// TestRenderItemDetailsModified renders the modified example of 5.1: the
// reason of the state, the next step in a paragraph of its own, and the
// pending reset with its verb.
func TestRenderItemDetailsModified(t *testing.T) {
	noColor(t)
	want := []string{
		"done-v1 · personal-opencode",
		"",
		"Marks finished sessions",
		"",
		"Status    ~ modified",
		"          The installed content was changed after",
		"          installation. The kit content is unchanged; your",
		"          version is kept on updates.",
		"",
		"          r resets the file to the kit version.",
		"",
		"Pending   ↺ reset to kit",
	}
	got := RenderItemDetails(modifiedItem(), 60, UnicodeSet)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("item details mismatch (-want +got):\n%s", diff)
	}
}

// TestRenderItemDetailsWarning renders a warning item: the plan's reason in
// the Status section, no advice after it — warning has no acting key
// (7.1) — and the sections after it in order.
func TestRenderItemDetailsWarning(t *testing.T) {
	noColor(t)
	want := []string{
		"old-lint · team-opencode",
		"",
		"Lint rules removed from the kit",
		"",
		"Status    △ warning",
		"          content changed without a version change; no",
		"          update is offered",
		"",
		"Tags      lint",
		"",
		"Changelog",
		"  0.2.0  Rules removed",
	}
	got := RenderItemDetails(warningItem(), 60, UnicodeSet)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("warning item details mismatch (-want +got):\n%s", diff)
	}
	for _, line := range got {
		if strings.Contains(line, "resets") {
			t.Errorf("warning carries an advice line it has no key for: %q", line)
		}
	}
}

// TestRenderItemDetailsMajor renders the major update of 7.2: the version
// pair with its kind in the status line, and the review note above the
// changelog.
func TestRenderItemDetailsMajor(t *testing.T) {
	noColor(t)
	want := []string{
		"review · team-opencode",
		"",
		"Review agent with team checklist",
		"",
		"Status    ↑ update, 0.3.0 → 1.0.0 (major)",
		"",
		"Pending   ✓ update to 1.0.0",
		"",
		"△ A major version changes behavior. Review the changelog",
		"  below, then press space again to select.",
		"",
		"Changelog",
		"  1.0.0  Checklist rewritten; steps 4-6 removed",
		"  0.4.0  Adds security section",
	}
	got := RenderItemDetails(majorItem(), 60, UnicodeSet)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("item details mismatch (-want +got):\n%s", diff)
	}
}

// TestRenderItemDetailsFrozen renders the details of a frozen row: the
// sections without facts drop out, the Status line stays.
func TestRenderItemDetailsFrozen(t *testing.T) {
	noColor(t)
	want := []string{
		"stray · gone-kit",
		"",
		"Status    ◌ orphan",
	}
	d := ItemDetails{Row: ItemRow{
		Key:              "gone-kit/stray",
		KitName:          "gone-kit",
		ID:               "stray",
		Status:           StatusOrphan,
		Installed:        true,
		InstalledVersion: "1.1.0",
	}}
	got := RenderItemDetails(d, 60, UnicodeSet)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("frozen item details mismatch (-want +got):\n%s", diff)
	}
}

// TestRenderItemDetailsFreshAnswers checks the answer precedence: the
// answers of the selection win over the ones recorded in the lock, as they
// do at planning.
func TestRenderItemDetailsFreshAnswers(t *testing.T) {
	noColor(t)
	d := conflictItem()
	d.Answers = map[string]string{"mode": "repo"}
	got := RenderItemDetails(d, 60, UnicodeSet)
	if line := "  mode = repo"; !contains(got, line) {
		t.Errorf("no %q in the variables:\n%s", line, strings.Join(got, "\n"))
	}
}

// TestRenderItemDetailsUnanswered checks the marks of a variable without a
// stored answer: required ones say so, optional ones too.
func TestRenderItemDetailsUnanswered(t *testing.T) {
	noColor(t)
	d := ItemDetails{
		Row: ItemRow{Key: "k/i", KitName: "k", ID: "i", Status: StatusAvailable},
		Item: &kit.Item{ID: "i", Vars: []kit.Var{
			{ID: "branch", Prompt: "Branch", Required: true},
			{ID: "flavor"},
		}},
	}
	got := RenderItemDetails(d, 60, UnicodeSet)
	for _, line := range []string{"  branch (required, no answer)", "  flavor (optional)"} {
		if !contains(got, line) {
			t.Errorf("no %q in the variables:\n%s", line, strings.Join(got, "\n"))
		}
	}
}

// TestRenderItemDetailsASCIISet renders the conflict example in the ASCII
// set: the mark glyphs fall back, the content stays.
func TestRenderItemDetailsASCIISet(t *testing.T) {
	noColor(t)
	got := RenderItemDetails(conflictItem(), 64, ASCIISet)
	if !contains(got, "Status    x conflict") {
		t.Errorf("no ASCII conflict status:\n%s", strings.Join(got, "\n"))
	}
	for _, glyph := range []string{"✗", "△"} {
		for _, line := range got {
			if strings.Contains(line, glyph) {
				t.Errorf("line %q holds the Unicode glyph %q in the ASCII set", line, glyph)
			}
		}
	}
}

// TestRenderItemDetailsProseLimit wraps the details at a width far over the
// prose limit: no line grows past 100 columns, whatever the width (2.2),
// and a long description wraps into several of them.
func TestRenderItemDetailsProseLimit(t *testing.T) {
	noColor(t)
	const long = "The installed content was changed after installation and the " +
		"sentence keeps going so that its wrap at a wide terminal would run " +
		"far past the readable line length the spec prescribes for prose."
	d := modifiedItem()
	d.Row.Description = long
	got := RenderItemDetails(d, 300, UnicodeSet)
	for _, line := range got {
		if w := ansi.StringWidth(line); w > ProseLimit {
			t.Errorf("line of %d columns over the prose limit: %q", w, line)
		}
	}
	if !wrapped(got, long) {
		t.Errorf("the long description did not wrap at the limit:\n%s", strings.Join(got, "\n"))
	}
}

// contains reports whether lines hold one exact line.
func contains(lines []string, line string) bool {
	for _, l := range lines {
		if l == line {
			return true
		}
	}
	return false
}

// wrapped reports whether the wrap of one paragraph at the prose limit
// appears among the lines, whole and in order.
func wrapped(lines []string, text string) bool {
	want := WrapProse(text, ProseLimit)
	for i := 0; i+len(want) <= len(lines); i++ {
		found := true
		for j, w := range want {
			if lines[i+j] != w {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

// TestRenderKitDetails renders the kit of 5.2: the fact rows in order,
// the counts by status, the other attachment.
func TestRenderKitDetails(t *testing.T) {
	noColor(t)
	want := []string{
		"personal-opencode",
		"",
		"Personal OpenCode customization",
		"",
		"Path           ~/kits/opencode",
		"Target hint    ~/.config/opencode",
		"Shared files   AGENTS.md, opencode.json",
		"Items          4: 2 installed, 1 update, 1 conflict",
		"Also attached  ~/wsl/opencode",
	}
	got := RenderKitDetails(fixtureKitDetails(), 50, UnicodeSet)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("kit details mismatch (-want +got):\n%s", diff)
	}
}

// TestRenderKitDetailsSeveralAttachments lists every other target under the
// label, aligned with the first.
func TestRenderKitDetailsSeveralAttachments(t *testing.T) {
	noColor(t)
	k := fixtureKitDetails()
	k.Registry = &state.Registry{
		Attachments: []state.Attachment{
			{Kit: "personal-opencode", Target: treeTarget},
			{Kit: "personal-opencode", Target: "~/wsl/opencode"},
			{Kit: "personal-opencode", Target: "~/wsl/team"},
			{Kit: "team-opencode", Target: treeTarget},
		},
	}
	got := RenderKitDetails(k, 50, UnicodeSet)
	want := []string{
		"Also attached  ~/wsl/opencode",
		"               ~/wsl/team",
	}
	found := false
	for i, line := range got {
		if line != want[0] {
			continue
		}
		found = true
		if i+1 >= len(got) || got[i+1] != want[1] {
			t.Errorf("second attachment is not aligned under the first:\n%s", strings.Join(got, "\n"))
		}
	}
	if !found {
		t.Errorf("no attachments block in:\n%s", strings.Join(got, "\n"))
	}
}

// TestRenderKitDetailsUnreadable renders the kit that did not load: its
// path alone, no facts (6.6).
func TestRenderKitDetailsUnreadable(t *testing.T) {
	noColor(t)
	want := []string{
		"old-kit",
		"",
		"Path  ~/kits/old",
	}
	got := RenderKitDetails(KitDetails{
		Row:    KitRow{Name: "old-kit", Path: "~/kits/old", Unreadable: true},
		Target: treeTarget,
	}, 50, UnicodeSet)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("unreadable kit details mismatch (-want +got):\n%s", diff)
	}
}

// TestRenderKitDetailsOrphans renders the orphans group: its name and its
// item counts, no kit facts.
func TestRenderKitDetailsOrphans(t *testing.T) {
	noColor(t)
	want := []string{
		"orphans",
		"",
		"Items  2: 2 orphan",
	}
	got := RenderKitDetails(KitDetails{
		Row: KitRow{
			Name:    "orphans",
			Orphans: true,
			Total:   2,
			Summary: []GlyphCount{{Status: StatusOrphan, Count: 2}},
		},
		Target: treeTarget,
	}, 50, UnicodeSet)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("orphans details mismatch (-want +got):\n%s", diff)
	}
}

// TestVersionChange names the increment of a version pair: the parenthesized
// kind of the update status (4.1, 7.2).
func TestVersionChange(t *testing.T) {
	for _, tc := range []struct {
		from, to, want string
	}{
		{"1.2.0", "1.3.0", "minor"},
		{"0.3.0", "1.0.0", "major"},
		{"1.2.0", "1.2.1", "patch"},
		{"2.0.0", "2.1.0", "minor"},
		{"1.0.0", "1.0.0", "patch"},
	} {
		if got := versionChange(tc.from, tc.to); got != tc.want {
			t.Errorf("versionChange(%s, %s) = %s, want %s", tc.from, tc.to, got, tc.want)
		}
	}
}

// TestScrollWindow cuts the window of a fixed height out of fixed content
// at fixed offsets: clamped from both sides, whole content when it fits,
// nothing without height (5: the view scrolls).
func TestScrollWindow(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	for _, tc := range []struct {
		name   string
		offset int
		height int
		want   []string
	}{
		{"top", 0, 4, []string{"a", "b", "c", "d"}},
		{"middle", 5, 4, []string{"f", "g", "h", "i"}},
		{"past the end", 99, 4, []string{"g", "h", "i", "j"}},
		{"before the start", -3, 4, []string{"a", "b", "c", "d"}},
		{"window over content", 0, 99, lines},
		{"last page", 6, 4, []string{"g", "h", "i", "j"}},
		{"no height", 2, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ScrollWindow(lines, tc.offset, tc.height)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("window mismatch (-want +got):\n%s", diff)
			}
		})
	}
	if got := ScrollWindow(nil, 2, 4); got != nil {
		t.Errorf("window over no content = %v, want nil", got)
	}
}
