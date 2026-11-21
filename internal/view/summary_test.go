package view

import (
	"reflect"
	"testing"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/target"
)

// The summary decisions both clients render: the verbs and their past
// forms, the version texts, and the path lines of install and removal
// plans — the marks, the notes, and the JSON locations taken without a
// write.

// snapOf builds a snapshot holding the given file and shared-file states.
func snapOf(files map[string]target.FileState, shared map[string]target.SharedFileState) *target.Snapshot {
	if files == nil {
		files = map[string]target.FileState{}
	}
	if shared == nil {
		shared = map[string]target.SharedFileState{}
	}
	return &target.Snapshot{Files: files, Shared: shared}
}

func TestPlanVerb(t *testing.T) {
	ip := &plan.ItemPlan{Key: key}
	lk := &lock.Lock{Items: map[string]*lock.Item{}}
	if got := PlanVerb(ip, lk, nil); got != VerbInstall {
		t.Errorf("PlanVerb without a record = %q, want install", got)
	}
	lk.Items[key] = &lock.Item{Version: "1.0.0"}
	if got := PlanVerb(ip, lk, nil); got != VerbUpdate {
		t.Errorf("PlanVerb at a record = %q, want update", got)
	}
	ip.Downgrade = true
	if got := PlanVerb(ip, lk, nil); got != VerbDowngrade {
		t.Errorf("PlanVerb below the record = %q, want downgrade", got)
	}
	if got := PlanVerb(ip, lk, map[string]bool{key: true}); got != VerbReset {
		t.Errorf("PlanVerb of a reset = %q, want reset", got)
	}
}

func TestPastVerbAndGlyph(t *testing.T) {
	for verb, past := range map[string]string{
		VerbInstall:   "installed",
		VerbUpdate:    "updated",
		VerbDowngrade: "downgraded",
		VerbReset:     "reset",
		VerbRemove:    "removed",
		"kept":        "kept",
	} {
		if got := PastVerb(verb); got != past {
			t.Errorf("PastVerb(%q) = %q, want %q", verb, got, past)
		}
	}
	for m, glyph := range map[Mark]string{
		MarkCreate:  "+",
		MarkReplace: "~",
		MarkDelete:  "-",
		MarkKeep:    "=",
		MarkRelease: "!",
	} {
		if got := m.Glyph(); got != glyph {
			t.Errorf("Glyph of %d = %q, want %q", m, got, glyph)
		}
	}
}

func TestVersionText(t *testing.T) {
	lk := &lock.Lock{Items: map[string]*lock.Item{key: {Version: "1.2.0"}}}
	cases := []struct {
		verb string
		down bool
		want string
	}{
		{VerbInstall, false, "1.3.0"},
		{VerbRemove, false, "1.3.0"},
		{VerbUpdate, false, "1.2.0 → 1.3.0"},
		{VerbReset, false, "1.2.0 → 1.3.0"},
	}
	for _, tc := range cases {
		ip := &plan.ItemPlan{Key: key, Version: "1.3.0", Downgrade: tc.down}
		if got := VersionText(ip, lk, tc.verb); got != tc.want {
			t.Errorf("VersionText of %s = %q, want %q", tc.verb, got, tc.want)
		}
	}
	ip := &plan.ItemPlan{Key: keyAlt, Version: "1.0.0"}
	if got := VersionText(ip, lk, VerbUpdate); got != "1.0.0" {
		t.Errorf("VersionText without a record = %q, want the version alone", got)
	}
}

// TestInstallLines walks one install plan through every mark: a file the
// target has not seen, a replaced section, a json file, the kept content
// no action rewrites, and the JSON locations owned without a write.
func TestInstallLines(t *testing.T) {
	ip := &plan.ItemPlan{
		Key:     key,
		Version: "1.1.0",
		Status:  plan.StatusInstall,
		Actions: []plan.Action{
			{Kind: plan.ActionFile, To: "skills/writing/SKILL.md"},
			{Kind: plan.ActionSection, To: "AGENTS.md", Block: "personal/writing"},
			{Kind: plan.ActionJSON, To: "opencode.json"},
		},
		JSONLocations: map[string][]plan.Location{
			"settings.json": {{Pointer: "/theme"}},
		},
	}
	lk := &lock.Lock{Items: map[string]*lock.Item{
		key: {
			Version: "1.0.0",
			Files:   map[string]string{"skills/writing/README.md": "sha256:x"},
			Sections: map[string]lock.Section{
				"AGENTS.md": {Block: "personal/writing", Hash: "sha256:y"},
			},
		},
	}}
	snap := snapOf(
		map[string]target.FileState{
			"skills/writing/SKILL.md": {Exists: true, Hash: "sha256:old"},
		},
		map[string]target.SharedFileState{
			"AGENTS.md": {Exists: true, Blocks: map[string]string{
				"personal/writing": "sha256:y",
				"other/note":       "sha256:z",
			}},
		},
	)

	verb, lines := InstallLines(ip, lk, snap, nil)

	if verb != VerbUpdate {
		t.Errorf("verb = %q, want update", verb)
	}
	want := []PathLine{
		{Mark: MarkReplace, Path: "skills/writing/SKILL.md"},
		{Mark: MarkReplace, Path: "AGENTS.md", Note: "section personal/writing"},
		{Mark: MarkCreate, Path: "opencode.json"},
		{Mark: MarkKeep, Path: "skills/writing/README.md"},
		{Mark: MarkKeep, Path: "settings.json", Note: "/theme"},
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines =\n%+v\nwant\n%+v", lines, want)
	}
}

// TestInstallLinesNoActionExplainsItsKeptLine pins the verdict reason a
// NoAction item reports on its first line.
func TestInstallLinesNoActionExplainsItsKeptLine(t *testing.T) {
	ip := &plan.ItemPlan{
		Key:     key,
		Version: "1.0.0",
		Status:  plan.StatusNoAction,
		Reason:  "already installed at this version",
	}
	lk := &lock.Lock{Items: map[string]*lock.Item{
		key: {Version: "1.0.0", Files: map[string]string{"skills/writing/SKILL.md": "sha256:x"}},
	}}
	_, lines := InstallLines(ip, lk, snapOf(nil, nil), nil)
	want := []PathLine{{Mark: MarkKeep, Path: "skills/writing/SKILL.md", Note: "kept: already installed at this version"}}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %+v, want %+v", lines, want)
	}
}

// TestInstallLinesOfAReset keeps the reset verb ahead of the record's
// presence.
func TestInstallLinesOfAReset(t *testing.T) {
	ip := &plan.ItemPlan{Key: key, Version: "1.0.0", Status: plan.StatusInstall}
	lk := &lock.Lock{Items: map[string]*lock.Item{key: {Version: "1.0.0"}}}
	verb, _ := InstallLines(ip, lk, snapOf(nil, nil), map[string]bool{key: true})
	if verb != VerbReset {
		t.Errorf("verb = %q, want reset", verb)
	}
}

// TestRemovalLines walks a removal plan: the deletions, and the kept
// content released with its reason — a reason repeated on the same path
// shows once.
func TestRemovalLines(t *testing.T) {
	ip := &plan.ItemPlan{
		Key: key,
		Actions: []plan.Action{
			{Kind: plan.ActionRemove, To: "skills/old/SKILL.md"},
			{Kind: plan.ActionSectionRemove, To: "AGENTS.md", Block: "personal/old"},
		},
		Kept: []plan.KeptNote{
			{Path: "agents/notes.md", Reason: "modified by the user"},
			{Path: "agents/notes.md", Reason: "modified by the user"},
		},
	}
	want := []PathLine{
		{Mark: MarkDelete, Path: "skills/old/SKILL.md"},
		{Mark: MarkDelete, Path: "AGENTS.md", Note: "section personal/old"},
		{Mark: MarkRelease, Path: "agents/notes.md", Note: "kept and released: modified by the user"},
	}
	if got := RemovalLines(ip); !reflect.DeepEqual(got, want) {
		t.Errorf("RemovalLines =\n%+v\nwant\n%+v", got, want)
	}
}

func TestSortedKeys(t *testing.T) {
	got := SortedKeys(map[string]int{"b": 1, "a": 2, "c": 3})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SortedKeys = %v, want %v", got, want)
	}
}
