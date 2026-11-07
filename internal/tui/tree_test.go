package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/muesli/termenv"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// Fixture values for the tree model: a registry with three kits attached to
// one target — two of which load, one of which does not — a lock with
// installed items of every frozen kind, a snapshot they reconcile against,
// and one planned conflict.

const treeTarget = "/t"

// sha hashes fixture content the way the snapshot and the lock record it.
func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// fixtureRegistry registers two readable kits and old-kit, which the tests
// leave out of the loaded kits, so it shows as unreadable (6.6).
func fixtureRegistry() *state.Registry {
	return &state.Registry{
		Kits: map[string]state.Kit{
			"personal-opencode": {Dir: "~/kits/opencode"},
			"team-opencode":     {Dir: "~/kits/team"},
			"old-kit":           {Dir: "~/kits/old"},
		},
		Targets: map[string]state.Target{treeTarget: {}},
		Attachments: []state.Attachment{
			{Kit: "personal-opencode", Target: treeTarget},
			{Kit: "team-opencode", Target: treeTarget},
			{Kit: "old-kit", Target: treeTarget},
		},
	}
}

// fixtureKits loads the two readable kits: personal-opencode with an
// installed, an update, a conflicting and an available item, team-opencode
// with one available item.
func fixtureKits() []*kit.Kit {
	return []*kit.Kit{
		{
			Name: "personal-opencode",
			Dir:  "~/kits/opencode",
			Items: []*kit.Item{
				{ID: "writing", Version: "1.0.0", Description: "Text editing skill", Tags: []string{"writing"}},
				{ID: "done-v2", Version: "1.3.0", Description: "Marks finished sessions", Tags: []string{"opencode-v2"}},
				{ID: "git-via-gh", Version: "1.2.0", Description: "Use gh instead of git", Tags: []string{"gh"}},
				{ID: "done-v1", Version: "1.0.0", Description: "Marks finished sessions", Tags: []string{"opencode-v1"}},
			},
		},
		{
			Name: "team-opencode",
			Dir:  "~/kits/team",
			Items: []*kit.Item{
				{ID: "review", Version: "0.3.0", Description: "Review agent with team checklist", Tags: []string{"review"}},
			},
		},
	}
}

// fixtureLock installs: writing at the kit version, done-v2 behind it,
// git-via-gh at the kit version, old-lint left over in its kit, old-kit's
// legacy-rules under the unreadable kit, and stray under a kit attached
// nowhere.
func fixtureLock() *lock.Lock {
	return &lock.Lock{
		Target: treeTarget,
		Items: map[string]*lock.Item{
			"personal-opencode/writing":    {Version: "1.0.0", Files: map[string]string{"skills/writing/SKILL.md": sha("written")}},
			"personal-opencode/done-v2":    {Version: "1.2.0", Files: map[string]string{"plugin/done.ts": sha("done")}},
			"personal-opencode/git-via-gh": {Version: "1.2.0"},
			"team-opencode/old-lint":       {Version: "0.2.0"},
			"old-kit/legacy-rules":         {Version: "0.4.0"},
			"gone-kit/stray":               {Version: "1.1.0"},
		},
	}
}

// fixtureSnap observes the recorded files as installed: the reconciliation
// finds nothing broken or modified.
func fixtureSnap() *target.Snapshot {
	return &target.Snapshot{
		Root: treeTarget,
		Files: map[string]target.FileState{
			"skills/writing/SKILL.md": {Exists: true, Hash: sha("written")},
			"plugin/done.ts":          {Exists: true, Hash: sha("done")},
		},
		Shared: map[string]target.SharedFileState{},
	}
}

// fixturePlans excludes git-via-gh with a conflict against user content.
func fixturePlans() map[string]*plan.ItemPlan {
	return map[string]*plan.ItemPlan{
		"personal-opencode/git-via-gh": {
			Key:    "personal-opencode/git-via-gh",
			Status: plan.StatusError,
			Conflicts: []plan.Conflict{{
				Kind:  kit.KindFile,
				File:  "AGENTS.md",
				Cause: plan.CauseUserContent,
			}},
		},
	}
}

// fixtureInput assembles the fixture tree input; pending, collapsed and
// filter vary per test.
func fixtureInput(pending *state.Pending, collapsed map[string]bool, filter string) TreeInput {
	reg := fixtureRegistry()
	if pending != nil {
		reg.Pending = map[string]*state.Pending{treeTarget: pending}
	}
	return TreeInput{
		Target:    treeTarget,
		Registry:  reg,
		Lock:      fixtureLock(),
		Snapshot:  fixtureSnap(),
		Kits:      fixtureKits(),
		Plans:     fixturePlans(),
		Collapsed: collapsed,
		Filter:    filter,
	}
}

// fixtureRows builds the fixture tree.
func treeRows(pending *state.Pending, collapsed map[string]bool, filter string) []Row {
	return BuildTree(fixtureInput(pending, collapsed, filter))
}

// findItem returns the item row of key.
func findItem(t *testing.T, rows []Row, key string) ItemRow {
	t.Helper()
	for _, r := range rows {
		if r.Kind == KindItem && r.Item.Key == key {
			return r.Item
		}
	}
	t.Fatalf("no item row %s in the tree", key)
	return ItemRow{}
}

// findKit returns the group row of name.
func findKit(t *testing.T, rows []Row, name string) KitRow {
	t.Helper()
	for _, r := range rows {
		if r.Kind == KindKit && r.Kit.Name == name {
			return r.Kit
		}
	}
	t.Fatalf("no group row %s in the tree", name)
	return KitRow{}
}

// TestBuildTreeStatuses walks the fixture tree: every item kind of 3.1
// lands on its status, and the conflict reason stands in for the
// description.
func TestBuildTreeStatuses(t *testing.T) {
	// old-kit expands: its frozen item takes part in the tree.
	rows := treeRows(nil, map[string]bool{"old-kit": false}, "")

	tests := []struct {
		name      string
		key       string
		want      Status
		installed bool
	}{
		{"installed", "personal-opencode/writing", StatusInstalled, true},
		{"update", "personal-opencode/done-v2", StatusUpdate, true},
		{"conflict", "personal-opencode/git-via-gh", StatusConflict, true},
		{"available", "personal-opencode/done-v1", StatusAvailable, false},
		{"available in team", "team-opencode/review", StatusAvailable, false},
		{"item of an unreadable kit", "old-kit/legacy-rules", StatusUnavailable, true},
		{"orphan of a detached kit", "gone-kit/stray", StatusOrphan, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := findItem(t, rows, tc.key)
			if r.Status != tc.want {
				t.Errorf("status = %v, want %v", r.Status, tc.want)
			}
			if r.Installed != tc.installed {
				t.Errorf("installed = %v, want %v", r.Installed, tc.installed)
			}
		})
	}

	t.Run("orphan under its kit", func(t *testing.T) {
		r := findItem(t, rows, "team-opencode/old-lint")
		if r.Status != StatusOrphan {
			t.Errorf("status = %v, want %v", r.Status, StatusOrphan)
		}
		if r.Description != "removed from the kit" {
			t.Errorf("description = %q, want the orphan note", r.Description)
		}
	})

	t.Run("update carries the version pair", func(t *testing.T) {
		r := findItem(t, rows, "personal-opencode/done-v2")
		if r.InstalledVersion != "1.2.0" || r.KitVersion != "1.3.0" {
			t.Errorf("versions = %q, %q; want 1.2.0, 1.3.0", r.InstalledVersion, r.KitVersion)
		}
		if r.Major {
			t.Error("a minor update is not major")
		}
	})

	t.Run("conflict carries the reason", func(t *testing.T) {
		r := findItem(t, rows, "personal-opencode/git-via-gh")
		want := "AGENTS.md exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user"
		if r.Reason != want {
			t.Errorf("reason = %q, want %q", r.Reason, want)
		}
	})

	// One item per table case, built and planned on its own.
	oneItemRows := func(kitVer, lockVer string, snap *target.Snapshot, ip *plan.ItemPlan) []Row {
		k := &kit.Kit{
			Name:  "solo",
			Dir:   "~/kits/solo",
			Items: []*kit.Item{{ID: "thing", Version: kitVer}},
		}
		var lk *lock.Lock
		if lockVer != "" {
			lk = &lock.Lock{Target: treeTarget, Items: map[string]*lock.Item{
				"solo/thing": {Version: lockVer, Files: map[string]string{"f.md": sha("installed")}},
			}}
		}
		plans := map[string]*plan.ItemPlan{}
		if ip != nil {
			ip.Key = "solo/thing"
			plans["solo/thing"] = ip
		}
		reg := &state.Registry{
			Kits:        map[string]state.Kit{"solo": {Dir: "~/kits/solo"}},
			Targets:     map[string]state.Target{treeTarget: {}},
			Attachments: []state.Attachment{{Kit: "solo", Target: treeTarget}},
		}
		return BuildTree(TreeInput{
			Target:   treeTarget,
			Registry: reg,
			Lock:     lk,
			Snapshot: snap,
			Kits:     []*kit.Kit{k},
			Plans:    plans,
		})
	}
	// snapWith observes the recorded file at hash, or misses it entirely.
	snapWith := func(hash string) *target.Snapshot {
		files := map[string]target.FileState{}
		if hash != "" {
			files["f.md"] = target.FileState{Exists: true, Hash: hash}
		}
		return &target.Snapshot{Root: treeTarget, Files: files}
	}

	t.Run("downgrade", func(t *testing.T) {
		r := findItem(t, oneItemRows("1.2.0", "1.3.0", snapWith(sha("installed")), nil), "solo/thing")
		if r.Status != StatusDowngrade {
			t.Errorf("status = %v, want %v", r.Status, StatusDowngrade)
		}
	})

	t.Run("modified", func(t *testing.T) {
		r := findItem(t, oneItemRows("1.0.0", "1.0.0", snapWith(sha("changed")), nil), "solo/thing")
		if r.Status != StatusModified {
			t.Errorf("status = %v, want %v", r.Status, StatusModified)
		}
	})

	t.Run("broken carries the reason", func(t *testing.T) {
		r := findItem(t, oneItemRows("1.0.0", "1.0.0", snapWith(""), nil), "solo/thing")
		if r.Status != StatusBroken {
			t.Errorf("status = %v, want %v", r.Status, StatusBroken)
		}
		if r.Reason != brokenReason {
			t.Errorf("reason = %q, want %q", r.Reason, brokenReason)
		}
	})

	t.Run("broken outranks the orphan state", func(t *testing.T) {
		// A kit item the kit stopped providing, whose recorded content the
		// target lost: the frozen orphan shows broken with the reason of
		// 3.1, and the description column shows it in place of the orphan
		// note.
		k := &kit.Kit{Name: "solo", Dir: "~/kits/solo"}
		reg := &state.Registry{
			Kits:        map[string]state.Kit{"solo": {Dir: "~/kits/solo"}},
			Targets:     map[string]state.Target{treeTarget: {}},
			Attachments: []state.Attachment{{Kit: "solo", Target: treeTarget}},
		}
		lk := &lock.Lock{Target: treeTarget, Items: map[string]*lock.Item{
			"solo/ghost": {Version: "1.0.0", Files: map[string]string{"f.md": sha("gone")}},
		}}
		snap := &target.Snapshot{Root: treeTarget}
		rows := BuildTree(TreeInput{
			Target: treeTarget, Registry: reg, Lock: lk, Snapshot: snap, Kits: []*kit.Kit{k},
		})
		r := findItem(t, rows, "solo/ghost")
		if r.Status != StatusBroken {
			t.Errorf("status = %v, want %v", r.Status, StatusBroken)
		}
		if r.Reason != brokenReason {
			t.Errorf("reason = %q, want %q", r.Reason, brokenReason)
		}
	})

	t.Run("warning carries the plan reason", func(t *testing.T) {
		ip := &plan.ItemPlan{Status: plan.StatusNoAction, Warning: true, Reason: "content changed without a version change; no update is offered"}
		r := findItem(t, oneItemRows("1.0.0", "1.0.0", snapWith(sha("installed")), ip), "solo/thing")
		if r.Status != StatusWarning {
			t.Errorf("status = %v, want %v", r.Status, StatusWarning)
		}
		if r.Reason != ip.Reason {
			t.Errorf("reason = %q, want %q", r.Reason, ip.Reason)
		}
	})

	t.Run("major update", func(t *testing.T) {
		r := findItem(t, oneItemRows("2.0.0", "1.2.0", snapWith(sha("installed")), nil), "solo/thing")
		if r.Status != StatusUpdate || !r.Major {
			t.Errorf("status = %v, major = %v; want %v, true", r.Status, r.Major, StatusUpdate)
		}
	})

	t.Run("conflict outranks broken", func(t *testing.T) {
		ip := &plan.ItemPlan{Status: plan.StatusError, Conflicts: []plan.Conflict{{
			Kind: kit.KindFile, File: "AGENTS.md", Cause: plan.CauseUserContent,
		}}}
		r := findItem(t, oneItemRows("1.0.0", "1.0.0", snapWith(""), ip), "solo/thing")
		if r.Status != StatusConflict {
			t.Errorf("status = %v, want %v", r.Status, StatusConflict)
		}
	})

	t.Run("a plain exclusion keeps the base status", func(t *testing.T) {
		ip := &plan.ItemPlan{Status: plan.StatusError, Reasons: []string{`required variable "branch" has no value`}}
		r := findItem(t, oneItemRows("1.3.0", "1.2.0", snapWith(sha("installed")), ip), "solo/thing")
		if r.Status != StatusUpdate {
			t.Errorf("status = %v, want %v", r.Status, StatusUpdate)
		}
		if r.Reason != "" {
			t.Errorf("reason = %q, want none", r.Reason)
		}
	})
}

// TestBuildTreeVerdictOnAvailableItem pins the planner's verdict on an
// available row: two rivals claim one file, the selection of one makes it
// conflicting, and the reason stands in for the description — the row has
// no lock record behind it, and the verdict applies all the same (3.1).
func TestBuildTreeVerdictOnAvailableItem(t *testing.T) {
	conflict := plan.Conflict{
		Kind:   kit.KindFile,
		File:   "skills/shared.md",
		Rivals: []string{"ex/two"},
		Cause:  plan.CauseClaimed,
	}
	rows := BuildTree(TreeInput{
		Target: treeTarget,
		Registry: &state.Registry{
			Kits:        map[string]state.Kit{"ex": {Dir: "~/kits/ex"}},
			Targets:     map[string]state.Target{treeTarget: {}},
			Attachments: []state.Attachment{{Kit: "ex", Target: treeTarget}},
		},
		Kits: []*kit.Kit{{
			Name: "ex",
			Dir:  "~/kits/ex",
			Items: []*kit.Item{
				{ID: "one", Version: "1.0.0", Description: "One"},
				{ID: "two", Version: "1.0.0", Description: "Two"},
			},
		}},
		Plans: map[string]*plan.ItemPlan{
			"ex/one": {Key: "ex/one", Status: plan.StatusError, Conflicts: []plan.Conflict{conflict}},
		},
	})
	r := findItem(t, rows, "ex/one")
	if r.Status != StatusConflict {
		t.Errorf("status = %v, want %v", r.Status, StatusConflict)
	}
	if want := renderConflict(conflict); r.Reason != want {
		t.Errorf("reason = %q, want %q", r.Reason, want)
	}
	if rival := findItem(t, rows, "ex/two"); rival.Status != StatusAvailable {
		t.Errorf("rival status = %v, want %v", rival.Status, StatusAvailable)
	}
}

// TestBuildTreeSummaries checks the group summaries of 3.4: the counts run
// by glyph in the order of the status table, the unreadable kit carries
// none, and the groups appear in attachment order with the orphans group
// before the add-kit row.
func TestBuildTreeSummaries(t *testing.T) {
	rows := treeRows(nil, nil, "")

	var order []string
	for _, r := range rows {
		switch r.Kind {
		case KindKit:
			order = append(order, r.Kit.Name)
		case KindItem:
			order = append(order, r.Item.Key)
		default:
			order = append(order, AddKitLabel)
		}
	}
	wantOrder := []string{
		"old-kit",
		"personal-opencode",
		"personal-opencode/writing",
		"personal-opencode/done-v2",
		"personal-opencode/git-via-gh",
		"personal-opencode/done-v1",
		"team-opencode",
		"team-opencode/review",
		"team-opencode/old-lint",
		"orphans",
		"gone-kit/stray",
		"+ add kit",
	}
	if diff := cmp.Diff(wantOrder, order); diff != "" {
		t.Errorf("row order mismatch (-want +got):\n%s", diff)
	}

	tests := []struct {
		name    string
		group   string
		summary []GlyphCount
	}{
		{"personal", "personal-opencode", []GlyphCount{
			{StatusAvailable, 1}, {StatusInstalled, 1}, {StatusUpdate, 1}, {StatusConflict, 1},
		}},
		{"team", "team-opencode", []GlyphCount{
			{StatusAvailable, 1}, {StatusOrphan, 1},
		}},
		{"orphans", "orphans", []GlyphCount{
			{StatusOrphan, 1},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := findKit(t, rows, tc.group)
			if diff := cmp.Diff(tc.summary, g.Summary); diff != "" {
				t.Errorf("summary mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("the unreadable kit carries no summary and is collapsed", func(t *testing.T) {
		g := findKit(t, rows, "old-kit")
		if !g.Unreadable || !g.Collapsed {
			t.Errorf("unreadable = %v, collapsed = %v; want true, true", g.Unreadable, g.Collapsed)
		}
		if g.Summary != nil {
			t.Errorf("summary = %v, want none", g.Summary)
		}
		if g.Total != 1 {
			t.Errorf("total = %d, want 1", g.Total)
		}
	})

	t.Run("detaching comes from the deferred detach", func(t *testing.T) {
		pending := &state.Pending{Deferred: []state.Deferred{{Kind: state.DeferredDetachKit, Kit: "team-opencode"}}}
		g := findKit(t, treeRows(pending, nil, ""), "team-opencode")
		if !g.Detaching {
			t.Error("the kit with a deferred detach does not show detaching")
		}
	})

	t.Run("the first run holds only the add-kit row", func(t *testing.T) {
		rows := BuildTree(TreeInput{Target: treeTarget})
		if len(rows) != 1 || rows[0].Kind != KindAddKit {
			t.Errorf("rows = %v, want the add-kit row alone", rows)
		}
	})
}

// TestBuildTreeCollapse checks that a collapsed group keeps its row and its
// summary, that its item rows stay out, and that the collapse state the
// caller holds wins over the defaults (3.4, 6.6).
func TestBuildTreeCollapse(t *testing.T) {
	t.Run("a collapsed kit keeps its group", func(t *testing.T) {
		rows := treeRows(nil, map[string]bool{"personal-opencode": true}, "")
		g := findKit(t, rows, "personal-opencode")
		if !g.Collapsed || len(g.Summary) != 4 {
			t.Errorf("collapsed = %v, summary entries = %d; want true, 4", g.Collapsed, len(g.Summary))
		}
		for _, r := range rows {
			if r.Kind == KindItem && strings.HasPrefix(r.Item.Key, "personal-opencode/") {
				t.Errorf("item %s shows under its collapsed kit", r.Item.Key)
			}
		}
	})

	t.Run("an unreadable kit expands on explicit state", func(t *testing.T) {
		rows := treeRows(nil, map[string]bool{"old-kit": false}, "")
		r := findItem(t, rows, "old-kit/legacy-rules")
		if r.Status != StatusUnavailable {
			t.Errorf("status = %v, want %v", r.Status, StatusUnavailable)
		}
	})

	t.Run("the orphans group collapses by its key", func(t *testing.T) {
		rows := treeRows(nil, map[string]bool{OrphansKey: true}, "")
		g := findKit(t, rows, "orphans")
		if !g.Collapsed {
			t.Error("the orphans group is not collapsed")
		}
		for _, r := range rows {
			if r.Kind == KindItem && r.Item.Key == "gone-kit/stray" {
				t.Error("the item shows under the collapsed orphans group")
			}
		}
	})
}

// TestBuildTreeFilter checks the filter of 7.5: a group survives when any
// of its items match and carries the match counts, the items that do not
// match stay out, and the counts are case-insensitive.
func TestBuildTreeFilter(t *testing.T) {
	t.Run("matches by id", func(t *testing.T) {
		rows := treeRows(nil, nil, "done")
		g := findKit(t, rows, "personal-opencode")
		if !g.Filtering || g.Matches != 2 || g.Total != 4 {
			t.Errorf("filtering = %v, counts = %d of %d; want true, 2 of 4", g.Filtering, g.Matches, g.Total)
		}
		var ids []string
		for _, r := range rows {
			if r.Kind == KindItem {
				ids = append(ids, r.Item.ID)
			}
		}
		if diff := cmp.Diff([]string{"done-v2", "done-v1"}, ids); diff != "" {
			t.Errorf("visible items mismatch (-want +got):\n%s", diff)
		}
		for _, name := range []string{"team-opencode", "old-kit", "orphans"} {
			for _, r := range rows {
				if r.Kind == KindKit && r.Kit.Name == name {
					t.Errorf("group %s shows without matching items", name)
				}
			}
		}
	})

	t.Run("matches by tag", func(t *testing.T) {
		g := findKit(t, treeRows(nil, nil, "gh"), "personal-opencode")
		if g.Matches != 1 {
			t.Errorf("matches = %d, want 1", g.Matches)
		}
	})

	t.Run("matches by description", func(t *testing.T) {
		g := findKit(t, treeRows(nil, nil, "finished"), "personal-opencode")
		if g.Matches != 2 {
			t.Errorf("matches = %d, want 2", g.Matches)
		}
	})

	t.Run("the match is case-insensitive", func(t *testing.T) {
		g := findKit(t, treeRows(nil, nil, "DONE"), "personal-opencode")
		if g.Matches != 2 {
			t.Errorf("matches = %d, want 2", g.Matches)
		}
	})

	t.Run("the orphans group is visible when its item matches", func(t *testing.T) {
		rows := treeRows(nil, nil, "stray")
		g := findKit(t, rows, "orphans")
		if g.Matches != 1 || g.Total != 1 {
			t.Errorf("counts = %d of %d, want 1 of 1", g.Matches, g.Total)
		}
		findItem(t, rows, "gone-kit/stray")
	})

	t.Run("no filter marks no group", func(t *testing.T) {
		for _, r := range treeRows(nil, nil, "") {
			if r.Kind == KindKit && r.Kit.Filtering {
				t.Errorf("group %s is marked as filtered without a filter", r.Kit.Name)
			}
		}
	})
}

// TestBuildTreePending checks the pending marks out of the target's pending
// object, and that a removal outranks a reset, a reset outranks a
// selection.
func TestBuildTreePending(t *testing.T) {
	pending := &state.Pending{Items: map[string]*state.ItemPending{
		"personal-opencode/writing": {Select: true},
		"personal-opencode/done-v2": {Reset: true},
		"team-opencode/old-lint":    {Remove: true},
		"gone-kit/stray":            {Select: true, Remove: true},
	}}
	rows := treeRows(pending, nil, "")

	tests := []struct {
		key  string
		want Pending
	}{
		{"personal-opencode/writing", PendingApply},
		{"personal-opencode/done-v2", PendingReset},
		{"team-opencode/old-lint", PendingRemove},
		{"gone-kit/stray", PendingRemove},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			if got := findItem(t, rows, tc.key).Pending; got == nil || *got != tc.want {
				t.Errorf("pending = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("no pending action is nil", func(t *testing.T) {
		if got := findItem(t, rows, "personal-opencode/git-via-gh").Pending; got != nil {
			t.Errorf("pending = %v, want none", *got)
		}
	})
}

// TestDefaultActions checks the 7.1 table: the default action of space per
// status, removal on any installed item, and reset on the modified,
// conflict and broken states only.
func TestDefaultActions(t *testing.T) {
	installed := ItemRow{Status: StatusInstalled, Installed: true}
	tests := []struct {
		name           string
		row            ItemRow
		wantAction     Pending
		wantOK         bool
		wantRemovable  bool
		wantResettable bool
	}{
		{"available", ItemRow{Status: StatusAvailable}, PendingApply, true, false, false},
		{"installed", installed, 0, false, true, false},
		{"update", ItemRow{Status: StatusUpdate, Installed: true}, PendingApply, true, true, false},
		{"downgrade", ItemRow{Status: StatusDowngrade, Installed: true}, PendingApply, true, true, false},
		{"modified", ItemRow{Status: StatusModified, Installed: true}, 0, false, true, true},
		{"conflict", ItemRow{Status: StatusConflict, Installed: true}, 0, false, true, true},
		{"broken", ItemRow{Status: StatusBroken, Installed: true}, PendingReset, true, true, true},
		{"warning", ItemRow{Status: StatusWarning, Installed: true}, 0, false, true, false},
		{"orphan", ItemRow{Status: StatusOrphan, Installed: true}, 0, false, true, false},
		{"unavailable", ItemRow{Status: StatusUnavailable, Installed: true}, 0, false, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DefaultAction(tc.row.Status)
			if got != tc.wantAction || ok != tc.wantOK {
				t.Errorf("DefaultAction = %v, %v; want %v, %v", got, ok, tc.wantAction, tc.wantOK)
			}
			if got := Removable(tc.row); got != tc.wantRemovable {
				t.Errorf("Removable = %v, want %v", got, tc.wantRemovable)
			}
			if got := Resettable(tc.row); got != tc.wantResettable {
				t.Errorf("Resettable = %v, want %v", got, tc.wantResettable)
			}
		})
	}
}

// TestBuildTreeEmpty checks that a tree of empty values is just the
// add-kit row, and that nil inputs do not reach the model.
func TestBuildTreeEmpty(t *testing.T) {
	t.Run("nothing at all", func(t *testing.T) {
		rows := BuildTree(TreeInput{Target: treeTarget})
		if len(rows) != 1 || rows[0].Kind != KindAddKit {
			t.Errorf("rows = %v, want the add-kit row alone", rows)
		}
	})

	t.Run("an empty registry", func(t *testing.T) {
		rows := BuildTree(TreeInput{Target: treeTarget, Registry: &state.Registry{}})
		if len(rows) != 1 || rows[0].Kind != KindAddKit {
			t.Errorf("rows = %v, want the add-kit row alone", rows)
		}
	})

	t.Run("kits without a lock", func(t *testing.T) {
		rows := BuildTree(TreeInput{
			Target:   treeTarget,
			Registry: fixtureRegistry(),
			Kits:     fixtureKits(),
			Snapshot: fixtureSnap(),
		})
		// Without a lock nothing is installed: every kit item shows
		// available.
		r := findItem(t, rows, "personal-opencode/writing")
		if r.Status != StatusAvailable || r.Installed {
			t.Errorf("status = %v, installed = %v; want %v, false", r.Status, r.Installed, StatusAvailable)
		}
	})

	t.Run("a lock without a snapshot skips the reconciliation", func(t *testing.T) {
		rows := BuildTree(TreeInput{
			Target:   treeTarget,
			Registry: fixtureRegistry(),
			Lock:     fixtureLock(),
			Kits:     fixtureKits(),
			Plans:    fixturePlans(),
		})
		if r := findItem(t, rows, "personal-opencode/writing"); r.Status != StatusInstalled {
			t.Errorf("status = %v, want %v", r.Status, StatusInstalled)
		}
	})
}

// TestRenderTree checks the lines the rows render into through the layout
// and the glyph set: the columns of each band (2.2), the pending mark at
// the right edge of the tree (3.2), the unreadable kit (6.6), the version
// pair shrinking to the kit version, and the ASCII fallbacks (3.3).
func TestRenderTree(t *testing.T) {
	// Colors off: the lines compare plain.
	forceVisual(t, termenv.Ascii, true)

	pending := &state.Pending{Items: map[string]*state.ItemPending{
		"personal-opencode/done-v2": {Select: true},
	}}

	t.Run("wide keeps the tags and drops the description", func(t *testing.T) {
		lines := RenderTree(treeRows(pending, nil, ""), NewLayout(120, 30), UnicodeSet)
		// The groups follow the attachment order of the registry: old-kit,
		// collapsed by default, first; personal-opencode under it.
		if lines[0] != "  ▸ old-kit  ⊘ unreadable  ~/kits/old" {
			t.Errorf("first row = %q", lines[0])
		}
		if lines[1] != "  ▾ personal-opencode  ○1 ●1 ↑1 ✗1  ~/kits/opencode" {
			t.Errorf("kit row = %q", lines[1])
		}
		want := "  ↑ done-v2  opencode-v2  1.2.0 → 1.3.0" + strings.Repeat(" ", 31) + "✓"
		if lines[3] != want {
			t.Errorf("item row = %q, want %q", lines[3], want)
		}
		for _, line := range lines {
			if strings.Contains(line, "AGENTS.md exists") {
				t.Error("the conflict reason shows in the band without a description column")
			}
		}
		if last := lines[len(lines)-1]; last != "  + add kit" {
			t.Errorf("last row = %q, want the add-kit row", last)
		}
	})

	t.Run("medium keeps the description", func(t *testing.T) {
		lines := RenderTree(treeRows(nil, nil, ""), NewLayout(90, 24), UnicodeSet)
		found := false
		for _, line := range lines {
			if strings.Contains(line, "AGENTS.md exists in the target") {
				found = true
			}
		}
		if !found {
			t.Error("the conflict reason does not show in the description column")
		}
	})

	t.Run("narrow drops the tags and the description", func(t *testing.T) {
		lines := RenderTree(treeRows(nil, nil, ""), NewLayout(64, 20), UnicodeSet)
		var row string
		for _, line := range lines {
			if strings.Contains(line, "done-v2") {
				row = line
			}
		}
		if !strings.Contains(row, "1.2.0 → 1.3.0") {
			t.Errorf("row = %q, want the version pair", row)
		}
		if strings.Contains(row, "opencode-v2") || strings.Contains(row, "Marks finished sessions") {
			t.Errorf("row = %q, want no tags or description", row)
		}
	})

	t.Run("the version pair shrinks to the kit version", func(t *testing.T) {
		l := Layout{Mode: ModeNarrow, Width: 22, Height: 20, TreeWidth: 22}
		lines := RenderTree(treeRows(nil, nil, ""), l, UnicodeSet)
		var row string
		for _, line := range lines {
			if strings.Contains(line, "done-v2") {
				row = line
			}
		}
		if strings.Contains(row, "→") {
			t.Errorf("row = %q, want the kit version alone", row)
		}
		if !strings.Contains(row, "1.3.0") {
			t.Errorf("row = %q, want the kit version", row)
		}
	})

	t.Run("the ASCII set spells the marks", func(t *testing.T) {
		lines := RenderTree(treeRows(pending, nil, ""), NewLayout(120, 30), ASCIISet)
		joined := strings.Join(lines, "\n")
		for _, want := range []string{"  v personal-opencode", "  > old-kit", "# unreadable", "^ done-v2", "* writing", "o done-v1", ". stray", "+"} {
			if !strings.Contains(joined, want) {
				t.Errorf("ascii render misses %q", want)
			}
		}
		for _, no := range []string{"▾", "▸", "⊘", "↑", "●", "○", "◌", "✓"} {
			if strings.Contains(joined, no) {
				t.Errorf("ascii render holds %q", no)
			}
		}
	})

	t.Run("the filter shows the match counts", func(t *testing.T) {
		lines := RenderTree(treeRows(nil, nil, "done"), NewLayout(120, 30), UnicodeSet)
		if lines[0] != "  ▾ personal-opencode  2 of 4  ~/kits/opencode" {
			t.Errorf("kit row = %q, want the match counts", lines[0])
		}
	})

	t.Run("detaching stands in the summary slot", func(t *testing.T) {
		pending := &state.Pending{Deferred: []state.Deferred{{Kind: state.DeferredDetachKit, Kit: "team-opencode"}}}
		lines := RenderTree(treeRows(pending, nil, ""), NewLayout(120, 30), UnicodeSet)
		want := "  ▾ team-opencode  detaching  ~/kits/team"
		var found bool
		for _, line := range lines {
			if line == want {
				found = true
			}
		}
		if !found {
			t.Errorf("no kit row %q", want)
		}
	})
}
