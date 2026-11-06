package plan

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/markdown"
	"github.com/dpleshakov/penates/internal/target"
)

// item builds a kit item around file contributions, without touching any file
// system: the planner works on values.
func item(id string, tos ...string) *kit.Item {
	it := &kit.Item{ID: id, Version: "1.0.0"}
	for _, to := range tos {
		it.Contributes = append(it.Contributes, kit.Contribution{
			Kind: kit.KindFile,
			Src:  "src/" + to,
			To:   to,
		})
	}
	return it
}

func oneKit(name string, items ...*kit.Item) []*kit.Kit {
	return []*kit.Kit{{Name: name, Items: items}}
}

func snapOf(states map[string]target.FileState) *target.Snapshot {
	if states == nil {
		states = map[string]target.FileState{}
	}
	return &target.Snapshot{Root: "/target", Files: states}
}

func sharedOf(states map[string]target.SharedFileState) *target.Snapshot {
	s := snapOf(nil)
	if states != nil {
		s.Shared = states
	}
	return s
}

// planSources fills the source of every contribution of every item with content,
// so file and section planning has something to read.
func planSources(kits []*kit.Kit, content string) map[string][]byte {
	out := map[string][]byte{}
	for _, k := range kits {
		for _, it := range k.Items {
			for _, c := range it.Contributes {
				out[it.SourcePath(c)] = []byte(content)
			}
		}
	}
	return out
}

// sectionItem builds a kit item around one section contribution.
func sectionItem(id, src, to string) *kit.Item {
	return &kit.Item{
		ID:          id,
		Version:     "1.0.0",
		Contributes: []kit.Contribution{{Kind: kit.KindSection, Src: src, To: to}},
	}
}

func TestBuild(t *testing.T) {
	tests := map[string]struct {
		kits      []*kit.Kit
		selection []string
		lockItems map[string]*lock.Item
		snapshot  map[string]target.FileState
		noSources bool
		want      func(*testing.T, *Plan)
	}{
		"installs into absent files": {
			kits:      oneKit("k", item("one", "skills/one/SKILL.md", "skills/one/lib.md")),
			selection: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if ip.Version != "1.0.0" || ip.KitName != "k" || ip.ItemID != "one" {
					t.Errorf("item identity = %s %s %s", ip.KitName, ip.ItemID, ip.Version)
				}
				if len(ip.Actions) != 2 {
					t.Fatalf("actions = %v", ip.Actions)
				}
				for _, a := range ip.Actions {
					if a.Reason != "target file is absent" {
						t.Errorf("action %s reason = %q", a.To, a.Reason)
					}
				}
			},
		},
		"present and unowned file is never written": {
			kits:      oneKit("k", item("one", "skills/one/SKILL.md")),
			selection: []string{"k/one"},
			snapshot:  map[string]target.FileState{"skills/one/SKILL.md": {Exists: true, Hash: "sha256:xx"}},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError {
					t.Fatalf("status = %q, want error", ip.Status)
				}
				if !strings.Contains(strings.Join(ip.Reasons, "; "), "not recorded in the lock") {
					t.Errorf("reasons = %v", ip.Reasons)
				}
			},
		},
		"file owned by another installed item": {
			kits:      oneKit("k", item("one", "skills/one/SKILL.md")),
			selection: []string{"k/one"},
			lockItems: map[string]*lock.Item{"other/two": {Version: "1.0.0", Files: map[string]string{"skills/one/SKILL.md": "sha256:xx"}}},
			snapshot:  map[string]target.FileState{"skills/one/SKILL.md": {Exists: true, Hash: "sha256:xx"}},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "owned by installed item other/two") {
					t.Errorf("status %q, reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"already installed item needs no action": {
			kits:      oneKit("k", item("one", "skills/one/SKILL.md")),
			selection: []string{"k/one"},
			lockItems: map[string]*lock.Item{"k/one": {Version: "1.0.0", Files: map[string]string{
				"skills/one/SKILL.md": hashOf([]byte("content\n")),
			}}},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusNoAction || ip.Reason != "already installed" {
					t.Errorf("status %q reason %q", ip.Status, ip.Reason)
				}
			},
		},
		"equal version with changed content warns without an update": {
			kits:      oneKit("k", item("one", "skills/one/SKILL.md")),
			selection: []string{"k/one"},
			lockItems: map[string]*lock.Item{"k/one": {Version: "1.0.0", Files: map[string]string{
				"skills/one/SKILL.md": "sha256:bf4774b3c9cc",
			}}},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusNoAction || !strings.Contains(ip.Reason, "content changed without a version change") {
					t.Errorf("status %q reason %q", ip.Status, ip.Reason)
				}
				if !ip.Warning {
					t.Error("the warning is not marked")
				}
			},
		},
		"two items claiming the same file conflict": {
			kits:      oneKit("k", item("one", "shared.md"), item("two", "shared.md")),
			selection: []string{"k/one", "k/two"},
			want: func(t *testing.T, p *Plan) {
				for _, ip := range p.Items {
					if !ip.Conflict {
						t.Errorf("%s is not marked as a conflict", ip.Key)
					}
					if ip.Status != StatusError {
						t.Fatalf("%s: status = %q, want error", ip.Key, ip.Status)
					}
					joined := strings.Join(ip.Reasons, "; ")
					if !strings.Contains(joined, "shared.md is also contributed by") {
						t.Errorf("%s: reasons = %v", ip.Key, ip.Reasons)
					}
				}
			},
		},
		"json fragment that is not JSON is an error": {
			kits: []*kit.Kit{{Name: "k", Items: []*kit.Item{{
				ID: "one", Version: "1.0.0",
				Contributes: []kit.Contribution{
					{Kind: kit.KindJSON, Src: "p.jsonc", To: "config.jsonc"},
				},
			}}}},
			selection: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if !strings.Contains(strings.Join(ip.Reasons, "; "), "does not hold a JSON document") {
					t.Errorf("reasons %v", ip.Reasons)
				}
			},
		},
		"source content missing": {
			kits:      oneKit("k", item("one", "skills/one/SKILL.md")),
			selection: []string{"k/one"},
			noSources: true,
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "was not provided to the planner") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"unknown item key": {
			kits:      oneKit("k", item("one", "a")),
			selection: []string{"k/gone"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "no registered kit provides") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"duplicate selection entries collapse": {
			kits:      oneKit("k", item("one", "a")),
			selection: []string{"k/one", "k/one"},
			want: func(t *testing.T, p *Plan) {
				if len(p.Items) != 1 {
					t.Fatalf("items = %d, want 1", len(p.Items))
				}
			},
		},
		"a failing item does not block another": {
			kits:      oneKit("k", item("bad", "user.md"), item("good", "skills/good/SKILL.md")),
			selection: []string{"k/bad", "k/good"},
			snapshot:  map[string]target.FileState{"user.md": {Exists: true, Hash: "sha256:xx"}},
			want: func(t *testing.T, p *Plan) {
				if len(p.Items) != 2 {
					t.Fatalf("items = %d, want 2", len(p.Items))
				}
				if p.Items[0].Status != StatusError {
					t.Errorf("bad: status %q", p.Items[0].Status)
				}
				if p.Items[1].Status != StatusInstall {
					t.Errorf("good: status %q", p.Items[1].Status)
				}
				if len(p.Installable()) != 1 || p.Installable()[0].Key != "k/good" {
					t.Errorf("Installable = %v", p.Installable())
				}
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			lk := newLock(tc.lockItems)
			sources := map[string][]byte{}
			if !tc.noSources {
				sources = planSources(tc.kits, "content\n")
			}
			p := Build(Input{
				Kits:      tc.kits,
				Selection: tc.selection,
				Lock:      lk,
				Snapshot:  snapOf(tc.snapshot),
				Sources:   sources,
			})
			if len(p.Items) == 0 {
				t.Fatal("plan has no items")
			}
			tc.want(t, p)
		})
	}
}

// TestPlanSections walks the RFC 7.3 rules for section contributions on install:
// append into absent and existing shared files, unowned and foreign-owned blocks,
// the duplicate-heading check, cross-kind claims, and sequential composition when
// several items write one shared file.
func TestPlanSections(t *testing.T) {
	fragment := "## House rules\n\nbody\n"
	tests := map[string]struct {
		kits      []*kit.Kit
		selection []string
		lockItems map[string]*lock.Item
		shared    map[string]target.SharedFileState
		files     map[string]target.FileState
		sources   map[string][]byte
		want      func(*testing.T, *Plan)
	}{
		"absent shared file is created with the block": {
			kits:      oneKit("k", sectionItem("one", "note.md", "AGENTS.md")),
			selection: []string{"k/one"},
			sources:   map[string][]byte{"note.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if len(ip.Actions) != 1 {
					t.Fatalf("actions = %v", ip.Actions)
				}
				a := ip.Actions[0]
				want := markdown.BlockText("k/one", fragment) + "\n"
				if a.Kind != ActionSection || a.To != "AGENTS.md" || string(a.Content) != want {
					t.Errorf("action = %+v content %q, want section writing %q", a, a.Content, want)
				}
				if a.Block != "k/one" || a.BlockHash == "" {
					t.Errorf("block record = %q %q", a.Block, a.BlockHash)
				}
			},
		},
		"existing shared file keeps its content and gains the block": {
			kits:      oneKit("k", sectionItem("one", "note.md", "AGENTS.md")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"AGENTS.md": {Exists: true, Content: "# Notes\n"}},
			sources:   map[string][]byte{"note.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				a := p.Items[0].Actions[0]
				want := "# Notes\n\n" + markdown.BlockText("k/one", fragment) + "\n"
				if string(a.Content) != want {
					t.Errorf("content =\n%q\nwant\n%q", a.Content, want)
				}
				if a.Reason != "block is absent; it will be appended" {
					t.Errorf("reason = %q", a.Reason)
				}
			},
		},
		"unowned block in the target is never touched": {
			kits:      oneKit("k", sectionItem("one", "note.md", "AGENTS.md")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"AGENTS.md": {Exists: true, Content: markdown.BlockText("k/one", "someone else's\n")}},
			sources:   map[string][]byte{"note.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "not recorded in the lock") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"block owned by another installed item": {
			kits:      oneKit("k", sectionItem("one", "note.md", "AGENTS.md")),
			selection: []string{"k/one"},
			lockItems: map[string]*lock.Item{"other/two": {Version: "1.0.0", Sections: map[string]lock.Section{
				"AGENTS.md": {Block: "k/one", Hash: "sha256:xx"},
			}}},
			shared:  map[string]target.SharedFileState{"AGENTS.md": {Exists: true, Content: markdown.BlockText("k/one", "old\n")}},
			sources: map[string][]byte{"note.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "owned by installed item other/two") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"shared path owned as a whole file": {
			kits:      oneKit("k", sectionItem("one", "note.md", "AGENTS.md")),
			selection: []string{"k/one"},
			lockItems: map[string]*lock.Item{"other/two": {Version: "1.0.0", Files: map[string]string{"AGENTS.md": "sha256:xx"}}},
			files:     map[string]target.FileState{"AGENTS.md": {Exists: true, Hash: "sha256:xx"}},
			sources:   map[string][]byte{"note.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "owned as a whole file by installed item other/two") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"duplicate heading outside managed blocks": {
			kits:      oneKit("k", sectionItem("one", "note.md", "AGENTS.md")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"AGENTS.md": {Exists: true, Content: "# Notes\n\n## House Rules\n\nhand-written\n"}},
			sources:   map[string][]byte{"note.md": []byte("## house RULES\n\nbody\n")},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), `already has a heading "house rules"`) {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
				if !ip.Conflict {
					t.Error("the heading duplicate is not marked as a conflict")
				}
			},
		},
		"directory in place of the shared file": {
			kits:      oneKit("k", sectionItem("one", "note.md", "AGENTS.md")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"AGENTS.md": {Exists: true, IsDir: true}},
			sources:   map[string][]byte{"note.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "is a directory") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"two items append to one shared file in order": {
			kits:      oneKit("k", sectionItem("one", "note.md", "AGENTS.md"), sectionItem("two", "note.md", "AGENTS.md")),
			selection: []string{"k/one", "k/two"},
			sources:   map[string][]byte{"note.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				for _, ip := range p.Items {
					if ip.Status != StatusInstall {
						t.Fatalf("%s: status %q reasons %v", ip.Key, ip.Status, ip.Reasons)
					}
				}
				final := p.Items[1].Actions[0].Content
				oneAt := strings.Index(string(final), "penates:begin k/one")
				twoAt := strings.Index(string(final), "penates:begin k/two")
				if oneAt < 0 || twoAt < 0 || oneAt > twoAt {
					t.Errorf("final content misses both blocks in order:\n%s", final)
				}
			},
		},
		"a heading inside a planned block does not block the next item": {
			kits:      oneKit("k", sectionItem("one", "a.md", "AGENTS.md"), sectionItem("two", "b.md", "AGENTS.md")),
			selection: []string{"k/one", "k/two"},
			sources:   map[string][]byte{"a.md": []byte(fragment), "b.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				// The duplicate-heading check is coarse by design: it looks at
				// headings outside managed blocks only, so two items with the
				// same heading coexist, each inside its own block.
				for _, ip := range p.Items {
					if ip.Status != StatusInstall {
						t.Errorf("%s: status %q reasons %v", ip.Key, ip.Status, ip.Reasons)
					}
				}
			},
		},
		"one item may not write two sections into one file": {
			kits: []*kit.Kit{{Name: "k", Items: []*kit.Item{{
				ID: "one", Version: "1.0.0",
				Contributes: []kit.Contribution{
					{Kind: kit.KindSection, Src: "a.md", To: "AGENTS.md"},
					{Kind: kit.KindSection, Src: "b.md", To: "AGENTS.md"},
				},
			}}}},
			selection: []string{"k/one"},
			sources:   map[string][]byte{"a.md": []byte(fragment), "b.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "one item owns one block per shared file") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"file and section claims on one path conflict both ways": {
			kits: []*kit.Kit{
				{Name: "k1", Items: []*kit.Item{{
					ID: "one", Version: "1.0.0",
					Contributes: []kit.Contribution{{Kind: kit.KindFile, Src: "f", To: "AGENTS.md"}},
				}}},
				{Name: "k2", Items: []*kit.Item{sectionItem("two", "note.md", "AGENTS.md")}},
			},
			selection: []string{"k1/one", "k2/two"},
			sources:   map[string][]byte{"f": []byte("file\n"), "note.md": []byte(fragment)},
			want: func(t *testing.T, p *Plan) {
				if !strings.Contains(strings.Join(p.Items[0].Reasons, "; "), "is also contributed by k2/two") {
					t.Errorf("file claimant reasons %v", p.Items[0].Reasons)
				}
				if !strings.Contains(strings.Join(p.Items[1].Reasons, "; "), "is contributed as a file by k1/one") {
					t.Errorf("section claimant reasons %v", p.Items[1].Reasons)
				}
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			lk := newLock(tc.lockItems)
			snap := sharedOf(tc.shared)
			for path, state := range tc.files {
				snap.Files[path] = state
			}
			p := Build(Input{
				Kits:      tc.kits,
				Selection: tc.selection,
				Lock:      lk,
				Snapshot:  snap,
				Sources:   tc.sources,
			})
			if len(p.Items) == 0 {
				t.Fatal("plan has no items")
			}
			tc.want(t, p)
		})
	}
}

// newLock builds a lock of /target with the given items.
func newLock(items map[string]*lock.Item) *lock.Lock {
	if items == nil {
		items = map[string]*lock.Item{}
	}
	return &lock.Lock{Target: "/target", Items: items}
}

// jsonItem builds a kit item around one json contribution.
func jsonItem(id, src, to string) *kit.Item {
	return &kit.Item{
		ID:          id,
		Version:     "1.0.0",
		Contributes: []kit.Contribution{{Kind: kit.KindJSON, Src: src, To: to}},
	}
}

// TestPlanJSON walks the RFC 7.4 table for json contributions: every row of
// the per-location decision, arrays as sets, co-ownership, the conflicts of
// 7.5, and the byte preservation of the untouched file parts.
func TestPlanJSON(t *testing.T) {
	darkHash := hashOf([]byte(`"dark"`))
	aHash := hashOf([]byte(`"a"`))
	bHash := hashOf([]byte(`"b"`))
	tests := map[string]struct {
		kits      []*kit.Kit
		selection []string
		lockItems map[string]*lock.Item
		shared    map[string]target.SharedFileState
		sources   map[string][]byte
		want      func(*testing.T, *Plan)
	}{
		"absent shared file is created with the fragment": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"theme": "dark"}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if len(ip.Actions) != 1 {
					t.Fatalf("actions = %v", ip.Actions)
				}
				a := ip.Actions[0]
				if a.Kind != ActionJSON || a.To != "config.jsonc" || string(a.Content) != `{"theme": "dark"}` {
					t.Errorf("action = %+v", a)
				}
				if a.Reason != "shared file is absent; it will be created with the fragment" {
					t.Errorf("reason = %q", a.Reason)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 1 || locs[0] != (Location{Pointer: "/theme", Hash: darkHash}) {
					t.Errorf("locations = %v", locs)
				}
			},
		},
		"absent member is inserted and owned, untouched bytes survive": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: "{\n  // about the editor\n  \"editor\": \"vim\",\n}\n"}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"theme":"dark"}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				a := ip.Actions[0]
				want := "{\n  // about the editor\n  \"editor\": \"vim\",\n  \"theme\": \"dark\",\n}\n"
				if string(a.Content) != want {
					t.Errorf("content =\n%q\nwant\n%q", a.Content, want)
				}
				if a.Reason != "fragment values are written into the shared file" {
					t.Errorf("reason = %q", a.Reason)
				}
				if locs := ip.JSONLocations["config.jsonc"]; len(locs) != 1 || locs[0].Pointer != "/theme" {
					t.Errorf("locations = %v", locs)
				}
			},
		},
		"equal unowned value is satisfied without owning it": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: "{ \"theme\": \"dark\" }\n"}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"theme": "dark"}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if len(ip.Actions) != 0 {
					t.Errorf("actions = %v, want none", ip.Actions)
				}
				if len(ip.JSONLocations) != 0 {
					t.Errorf("locations = %v, want none: a satisfied value is never owned", ip.JSONLocations)
				}
			},
		},
		"equal value owned by another item makes the item a co-owner": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			lockItems: map[string]*lock.Item{"other/two": {Version: "1.0.0", JSON: map[string][]lock.Location{
				"config.jsonc": {{Pointer: "/theme", Hash: darkHash}},
			}}},
			shared:  map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"theme": "dark"}`}},
			sources: map[string][]byte{"perm.jsonc": []byte(`{"theme":"dark"}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if len(ip.Actions) != 0 {
					t.Errorf("actions = %v, want none", ip.Actions)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 1 || locs[0] != (Location{Pointer: "/theme", Hash: darkHash}) {
					t.Errorf("locations = %v, want co-ownership", locs)
				}
			},
		},
		"a different unowned value is an error": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"theme": "light"}`}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"theme":"dark"}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), `holds a different value at "/theme"`) {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"array elements are a set: missing appended and owned, equal satisfied": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"tags": ["a"]}`}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"tags":["a","b"]}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if want := `{"tags": ["a", "b"]}`; string(ip.Actions[0].Content) != want {
					t.Errorf("content = %q, want %q", ip.Actions[0].Content, want)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 1 || locs[0] != (Location{Pointer: "/tags", Hash: bHash}) {
					t.Errorf("locations = %v, want only the appended element", locs)
				}
			},
		},
		"missing array is inserted whole and its elements owned": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: "{\n  \"editor\": \"vim\"\n}\n"}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"tags":["a"]}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				content := string(ip.Actions[0].Content)
				if !strings.Contains(content, `"tags": ["a"]`) || !strings.Contains(content, `"editor": "vim"`) {
					t.Errorf("content =\n%s", content)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 1 || locs[0] != (Location{Pointer: "/tags", Hash: aHash}) {
					t.Errorf("locations = %v", locs)
				}
			},
		},
		"array element owned by another item is co-owned": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			lockItems: map[string]*lock.Item{"other/two": {Version: "1.0.0", JSON: map[string][]lock.Location{
				"config.jsonc": {{Pointer: "/tags", Hash: aHash}},
			}}},
			shared:  map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"tags": ["a"]}`}},
			sources: map[string][]byte{"perm.jsonc": []byte(`{"tags":["a"]}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall || len(ip.Actions) != 0 {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 1 || locs[0] != (Location{Pointer: "/tags", Hash: aHash}) {
					t.Errorf("locations = %v, want co-ownership of the element", locs)
				}
			},
		},
		"a nested subtree is inserted at the highest missing ancestor": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: "{\n  \"a\": {},\n}\n"}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"a":{"b":{"c":1}}}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				content := string(ip.Actions[0].Content)
				if !strings.Contains(content, `"b": {"c":1}`) {
					t.Errorf("content =\n%s", content)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 1 || locs[0] != (Location{Pointer: "/a/b/c", Hash: hashOf([]byte("1"))}) {
					t.Errorf("locations = %v, want the leaf under the inserted subtree", locs)
				}
			},
		},
		"an object cannot be placed where a scalar sits": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"a": 5}`}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"a":{"b":1}}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), `different value at "/a"`) {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"root of a different kind is a different value": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: "[1]\n"}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"a":1}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "different value at the root") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"comments inside an inserted subtree survive, hashes stay minimal": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: "{}\n"}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"a":{"b": 1 /* inner */}}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if !strings.Contains(string(ip.Actions[0].Content), "/* inner */") {
					t.Errorf("content =\n%s", ip.Actions[0].Content)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 1 || locs[0] != (Location{Pointer: "/a/b", Hash: hashOf([]byte("1"))}) {
					t.Errorf("locations = %v", locs)
				}
			},
		},
		"an empty shared file is filled with the fragment": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: "  \n"}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"a": 1}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall || len(ip.Actions) != 1 {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				a := ip.Actions[0]
				if string(a.Content) != `{"a": 1}` || a.Reason != "shared file is empty; the fragment becomes its content" {
					t.Errorf("action = %+v", a)
				}
			},
		},
		"directory in place of the shared file": {
			kits:      oneKit("k", jsonItem("one", "perm.jsonc", "config.jsonc")),
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"config.jsonc": {Exists: true, IsDir: true}},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "is a directory") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"one item may plan file, section, and json contributions together": {
			kits: []*kit.Kit{{Name: "k", Items: []*kit.Item{{
				ID: "one", Version: "1.0.0",
				Contributes: []kit.Contribution{
					{Kind: kit.KindFile, Src: "f", To: "skills/one/SKILL.md"},
					{Kind: kit.KindSection, Src: "note.md", To: "AGENTS.md"},
					{Kind: kit.KindJSON, Src: "perm.jsonc", To: "config.jsonc"},
				},
			}}}},
			selection: []string{"k/one"},
			shared:    map[string]target.SharedFileState{"AGENTS.md": {Exists: true, Content: "# Notes\n"}},
			sources:   map[string][]byte{"f": []byte("body\n"), "note.md": []byte("## Rules\n\nbody\n"), "perm.jsonc": []byte(`{"a":1}`)},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall || len(ip.Actions) != 3 {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				kinds := []ActionKind{ip.Actions[0].Kind, ip.Actions[1].Kind, ip.Actions[2].Kind}
				if kinds[0] != ActionFile || kinds[1] != ActionSection || kinds[2] != ActionJSON {
					t.Errorf("action kinds = %v", kinds)
				}
			},
		},
		"two items claiming one location with different values conflict both ways": {
			kits:      oneKit("k", jsonItem("one", "a.jsonc", "config.jsonc"), jsonItem("two", "b.jsonc", "config.jsonc")),
			selection: []string{"k/one", "k/two"},
			sources:   map[string][]byte{"a.jsonc": []byte(`{"theme":"dark"}`), "b.jsonc": []byte(`{"theme":"light"}`)},
			want: func(t *testing.T, p *Plan) {
				for _, ip := range p.Items {
					joined := strings.Join(ip.Reasons, "; ")
					if ip.Status != StatusError || !strings.Contains(joined, `is claimed at /theme with a different value by`) {
						t.Errorf("%s: status %q reasons %v", ip.Key, ip.Status, ip.Reasons)
					}
				}
			},
		},
		"two items claiming one location with equal values share it": {
			kits:      oneKit("k", jsonItem("one", "a.jsonc", "config.jsonc"), jsonItem("two", "b.jsonc", "config.jsonc")),
			selection: []string{"k/one", "k/two"},
			sources:   map[string][]byte{"a.jsonc": []byte(`{"theme":"dark"}`), "b.jsonc": []byte(`{"theme": "dark"}`)},
			want: func(t *testing.T, p *Plan) {
				for _, ip := range p.Items {
					if ip.Status != StatusInstall {
						t.Fatalf("%s: status %q reasons %v", ip.Key, ip.Status, ip.Reasons)
					}
				}
				if len(p.Items[0].Actions) != 1 {
					t.Errorf("first item actions = %v, want the file write", p.Items[0].Actions)
				}
				if len(p.Items[1].Actions) != 0 {
					t.Errorf("second item actions = %v, want none", p.Items[1].Actions)
				}
				for _, ip := range p.Items {
					locs := ip.JSONLocations["config.jsonc"]
					if len(locs) != 1 || locs[0] != (Location{Pointer: "/theme", Hash: darkHash}) {
						t.Errorf("%s: locations = %v", ip.Key, locs)
					}
				}
			},
		},
		"json and section contributions cannot share one file": {
			kits: []*kit.Kit{
				{Name: "k1", Items: []*kit.Item{jsonItem("one", "perm.jsonc", "AGENTS.md")}},
				{Name: "k2", Items: []*kit.Item{sectionItem("two", "note.md", "AGENTS.md")}},
			},
			selection: []string{"k1/one", "k2/two"},
			sources:   map[string][]byte{"perm.jsonc": []byte(`{"a":1}`), "note.md": []byte("## Rules\n\nbody\n")},
			want: func(t *testing.T, p *Plan) {
				if !strings.Contains(strings.Join(p.Items[0].Reasons, "; "), "is also contributed as a file or section by k2/two") {
					t.Errorf("json claimant reasons %v", p.Items[0].Reasons)
				}
				if !strings.Contains(strings.Join(p.Items[1].Reasons, "; "), "is also contributed as json by k1/one") {
					t.Errorf("section claimant reasons %v", p.Items[1].Reasons)
				}
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			p := Build(Input{
				Kits:      tc.kits,
				Selection: tc.selection,
				Lock:      newLock(tc.lockItems),
				Snapshot:  sharedOf(tc.shared),
				Sources:   tc.sources,
			})
			if len(p.Items) == 0 {
				t.Fatal("plan has no items")
			}
			tc.want(t, p)
		})
	}
}

// TestOwnedAction covers the table row "owned by this item", which the
// three-state rules of RFC 7.2 decide. Every contribution kind runs its owned
// content through it.
func TestOwnedAction(t *testing.T) {
	base := hashOf([]byte(`"base"`))
	tests := []struct {
		name     string
		base     string
		current  string
		incoming string
		write    bool
		conflict bool
	}{
		{"unchanged, incoming equal: no write", base, base, base, false, false},
		{"unchanged, incoming differs: replace", base, base, hashOf([]byte(`"new"`)), true, false},
		{"modified, incoming equals base: keep the user's content", base, hashOf([]byte(`"user"`)), base, false, false},
		{"modified, incoming differs: conflict", base, hashOf([]byte(`"user"`)), hashOf([]byte(`"new"`)), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			write, conflict := ownedAction(tt.base, tt.current, tt.incoming)
			if write != tt.write || (conflict != "") != tt.conflict {
				t.Errorf("ownedAction = %v, %q; want write %v, conflict %v", write, conflict, tt.write, tt.conflict)
			}
		})
	}
}

// varItem builds a kit item around one templated or plain file contribution
// from the source "s", with the declared variables.
func varItem(id string, template bool, vars ...kit.Var) *kit.Item {
	return &kit.Item{
		ID:      id,
		Version: "1.0.0",
		Vars:    vars,
		Contributes: []kit.Contribution{{
			Kind:     kit.KindFile,
			Src:      "s",
			To:       "skills/" + id + "/SKILL.md",
			Template: template,
		}},
	}
}

// TestPlanVars walks the rules of RFC 6.4: substitution only where the
// contribution opts in, unknown placeholders as errors, required and optional
// variables without values, and the answers the plan records for the lock.
func TestPlanVars(t *testing.T) {
	tests := map[string]struct {
		item    *kit.Item
		source  string
		answers map[string]string
		want    func(*testing.T, *Plan)
	}{
		"templated file content is substituted and answers recorded": {
			item:    varItem("one", true, kit.Var{ID: "root", Prompt: "root path", Required: true}),
			source:  "root: {{root}}\n",
			answers: map[string]string{"root": "/opt/x"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if got := string(ip.Actions[0].Content); got != "root: /opt/x\n" {
					t.Errorf("content = %q", got)
				}
				if diff := cmp.Diff(map[string]string{"root": "/opt/x"}, ip.Answers); diff != "" {
					t.Errorf("answers (-want +got):\n%s", diff)
				}
			},
		},
		"a plain contribution keeps its braces": {
			item:    varItem("one", false, kit.Var{ID: "root", Required: true}),
			source:  "{{root}} stays\n",
			answers: map[string]string{"root": "/x"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if got := string(ip.Actions[0].Content); got != "{{root}} stays\n" {
					t.Errorf("content = %q, want the braces verbatim", got)
				}
			},
		},
		"an unknown placeholder is an error": {
			item:    varItem("one", true, kit.Var{ID: "root", Required: true}),
			source:  "{{nope}}\n",
			answers: map[string]string{"root": "/x"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "holds the unknown placeholder {{nope}}") {
					t.Fatalf("status %q reasons %v", ip.Status, ip.Reasons)
				}
				if len(ip.Actions) != 0 {
					t.Errorf("actions = %v, want none", ip.Actions)
				}
			},
		},
		"a required variable without a value is not installable": {
			item:   varItem("one", true, kit.Var{ID: "root", Prompt: "root path", Required: true}),
			source: "{{root}}\n",
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), `required variable "root" has no value`) {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"an optional variable without a value substitutes as empty": {
			item:   varItem("one", true, kit.Var{ID: "opt"}),
			source: "a{{opt}}b\n",
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if got := string(ip.Actions[0].Content); got != "ab\n" {
					t.Errorf("content = %q, want the empty substitution", got)
				}
				if diff := cmp.Diff(map[string]string{"opt": ""}, ip.Answers); diff != "" {
					t.Errorf("answers (-want +got):\n%s", diff)
				}
			},
		},
		"an answer is substituted once, not rescanned": {
			item:    varItem("one", true, kit.Var{ID: "root", Required: true}),
			source:  "{{root}}\n",
			answers: map[string]string{"root": "see {{root}}"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusInstall {
					t.Fatalf("status = %q, reasons %v", ip.Status, ip.Reasons)
				}
				if got := string(ip.Actions[0].Content); got != "see {{root}}\n" {
					t.Errorf("content = %q, want the answer verbatim", got)
				}
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			p := Build(Input{
				Kits:      oneKit("k", tc.item),
				Selection: []string{"k/" + tc.item.ID},
				Lock:      newLock(nil),
				Snapshot:  snapOf(nil),
				Sources:   map[string][]byte{"s": []byte(tc.source)},
				Answers:   map[string]map[string]string{"k/" + tc.item.ID: tc.answers},
			})
			if len(p.Items) == 0 {
				t.Fatal("plan has no items")
			}
			tc.want(t, p)
		})
	}
}

// TestPlanVarsShared covers substitution in the shared-file contributions: the
// section block and the json fragment are templated like files, and their
// hashes are taken over the substituted content.
func TestPlanVarsShared(t *testing.T) {
	frag := []byte("## Templated\n\nfor {{root}}\n")
	section := &kit.Item{
		ID: "one", Version: "1.0.0",
		Vars: []kit.Var{{ID: "root", Required: true}},
		Contributes: []kit.Contribution{{
			Kind: kit.KindSection, Src: "note.md", To: "AGENTS.md", Template: true,
		}},
	}
	p := Build(Input{
		Kits:      oneKit("k", section),
		Selection: []string{"k/one"},
		Lock:      newLock(nil),
		Snapshot:  sharedOf(map[string]target.SharedFileState{"AGENTS.md": {Exists: true, Content: "# Notes\n"}}),
		Sources:   map[string][]byte{"note.md": frag},
		Answers:   map[string]map[string]string{"k/one": {"root": "the machine"}},
	})
	ip := p.Items[0]
	if ip.Status != StatusInstall || len(ip.Actions) != 1 {
		t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
	}
	if !strings.Contains(string(ip.Actions[0].Content), "for the machine") {
		t.Errorf("block =\n%s", ip.Actions[0].Content)
	}
	if strings.Contains(string(ip.Actions[0].Content), "{{root}}") {
		t.Errorf("block keeps the placeholder:\n%s", ip.Actions[0].Content)
	}

	json := &kit.Item{
		ID: "two", Version: "1.0.0",
		Vars: []kit.Var{{ID: "root", Required: true}},
		Contributes: []kit.Contribution{{
			Kind: kit.KindJSON, Src: "perm.jsonc", To: "config.jsonc", Template: true,
		}},
	}
	p = Build(Input{
		Kits:      oneKit("k", json),
		Selection: []string{"k/two"},
		Lock:      newLock(nil),
		Snapshot:  sharedOf(map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: "{\n  \"editor\": \"vim\"\n}\n"}}),
		Sources:   map[string][]byte{"perm.jsonc": []byte("{\"root\": \"{{root}}\"}")},
		Answers:   map[string]map[string]string{"k/two": {"root": "/opt/x"}},
	})
	ip = p.Items[0]
	if ip.Status != StatusInstall || len(ip.Actions) != 1 {
		t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
	}
	if !strings.Contains(string(ip.Actions[0].Content), `"root": "/opt/x"`) {
		t.Errorf("config =\n%s", ip.Actions[0].Content)
	}
}

// TestResolveVars pins the merge the update flow will drive through Build:
// a fresh answer wins over the recorded one, an answer of a variable the item
// no longer declares is discarded, and missing required variables are
// reported sorted.
func TestResolveVars(t *testing.T) {
	it := &kit.Item{Vars: []kit.Var{
		{ID: "b", Required: true},
		{ID: "a", Required: true},
		{ID: "opt"},
	}}
	lk := newLock(map[string]*lock.Item{"k/one": {
		Version: "1.0.0",
		Answers: map[string]string{"b": "recorded", "gone": "discarded"},
	}})

	answers, missing := resolveVars("k/one", it, lk, map[string]map[string]string{
		"k/one": {"b": "fresh"},
	})
	if diff := cmp.Diff([]string{"a"}, missing); diff != "" {
		t.Errorf("missing (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(map[string]string{"b": "fresh", "a": "", "opt": ""}, answers); diff != "" {
		t.Errorf("answers (-want +got):\n%s", diff)
	}

	// Without a fresh answer the recorded one is reused.
	answers, missing = resolveVars("k/one", it, lk, nil)
	if len(missing) != 1 || missing[0] != "a" {
		t.Errorf("missing = %v", missing)
	}
	if answers["b"] != "recorded" {
		t.Errorf("recorded answer lost: %v", answers)
	}
	if _, kept := answers["gone"]; kept {
		t.Errorf("answer of the removed variable %q survived", "gone")
	}
}

// TestPlanUpdateVersions walks the version table of RFC 8 against the lock.
func TestPlanUpdateVersions(t *testing.T) {
	const v1 = "installed body\n"
	const v2 = "kit body\n"
	base := hashOf([]byte(v1))
	tests := map[string]struct {
		kitVersion  string
		lockVersion string
		source      string
		want        func(*testing.T, *ItemPlan)
	}{
		"higher version updates": {
			kitVersion: "1.1.0", lockVersion: "1.0.0", source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || ip.Downgrade {
					t.Fatalf("status %q downgrade %v", ip.Status, ip.Downgrade)
				}
				if len(ip.Actions) != 1 || ip.Actions[0].Reason != "update replaces the owned file" {
					t.Errorf("actions = %v", ip.Actions)
				}
			},
		},
		"lower version plans a downgrade": {
			kitVersion: "0.9.0", lockVersion: "1.0.0", source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || !ip.Downgrade {
					t.Fatalf("status %q downgrade %v, want a planned downgrade", ip.Status, ip.Downgrade)
				}
				if len(ip.Actions) != 1 {
					t.Errorf("actions = %v", ip.Actions)
				}
			},
		},
		"equal version with equal content needs no action": {
			kitVersion: "1.0.0", lockVersion: "1.0.0", source: v1,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusNoAction || ip.Reason != "already installed" {
					t.Errorf("status %q reason %q", ip.Status, ip.Reason)
				}
			},
		},
		"equal version with changed content warns": {
			kitVersion: "1.0.0", lockVersion: "1.0.0", source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusNoAction || !strings.Contains(ip.Reason, "content changed without a version change") {
					t.Errorf("status %q reason %q", ip.Status, ip.Reason)
				}
				if len(ip.Actions) != 0 {
					t.Errorf("actions = %v, want none: no update is offered", ip.Actions)
				}
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			it := item("one", "skills/one/SKILL.md")
			it.Version = tc.kitVersion
			p := Build(Input{
				Kits:      oneKit("k", it),
				Selection: []string{"k/one"},
				Lock: newLock(map[string]*lock.Item{"k/one": {
					Version: tc.lockVersion,
					Files:   map[string]string{"skills/one/SKILL.md": base},
				}}),
				Snapshot: snapOf(map[string]target.FileState{"skills/one/SKILL.md": {Exists: true, Hash: base}}),
				Sources:  map[string][]byte{it.SourcePath(it.Contributes[0]): []byte(tc.source)},
			})
			if len(p.Items) != 1 {
				t.Fatal("plan has no items")
			}
			tc.want(t, p.Items[0])
		})
	}
}

// TestPlanFileUpdates walks the update rows of the 7.2 table for an owned
// file, plus the reset row of 7.6.
func TestPlanFileUpdates(t *testing.T) {
	const v1 = "installed content\n"
	const v2 = "kit content\n"
	base := hashOf([]byte(v1))
	tests := map[string]struct {
		reset   bool
		current string // the file content the target holds now
		source  string // the content the kit provides
		want    func(*testing.T, *ItemPlan)
	}{
		"unchanged file is replaced by the update": {
			current: v1, source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 1 || ip.Actions[0].Reason != "update replaces the owned file" {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				if string(ip.Actions[0].Content) != v2 {
					t.Errorf("content = %q", ip.Actions[0].Content)
				}
			},
		},
		"user modification with equal incoming is kept": {
			current: "user content\n", source: v1,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 0 {
					t.Fatalf("status %q actions %v, want nothing written", ip.Status, ip.Actions)
				}
			},
		},
		"user modification with different incoming is a conflict": {
			current: "user content\n", source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "was modified after installation") {
					t.Fatalf("status %q reasons %v", ip.Status, ip.Reasons)
				}
				if len(ip.Actions) != 0 {
					t.Errorf("actions = %v, want none", ip.Actions)
				}
			},
		},
		"reset overwrites the user's modification": {
			reset: true, current: "user content\n", source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 1 || ip.Actions[0].Reason != "reset to kit replaces the owned file" {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				if string(ip.Actions[0].Content) != v2 {
					t.Errorf("content = %q", ip.Actions[0].Content)
				}
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			it := item("one", "skills/one/SKILL.md")
			it.Version = "1.1.0"
			p := Build(Input{
				Kits:      oneKit("k", it),
				Selection: []string{"k/one"},
				Lock: newLock(map[string]*lock.Item{"k/one": {
					Version: "1.0.0",
					Files:   map[string]string{"skills/one/SKILL.md": base},
				}}),
				Snapshot: snapOf(map[string]target.FileState{"skills/one/SKILL.md": {Exists: true, Hash: hashOf([]byte(tc.current))}}),
				Sources:  map[string][]byte{it.SourcePath(it.Contributes[0]): []byte(tc.source)},
				Reset:    map[string]bool{"k/one": tc.reset},
			})
			if len(p.Items) != 1 {
				t.Fatal("plan has no items")
			}
			tc.want(t, p.Items[0])
		})
	}
}

// TestPlanSectionUpdates walks the update rows of the 7.2 table for an owned
// block, plus the reset row of 7.6.
func TestPlanSectionUpdates(t *testing.T) {
	const v1 = "installed fragment\n"
	const v2 = "kit fragment\n"
	const user = "user fragment\n"
	base := hashOf([]byte(v1))
	file := func(block string) string {
		return "# Notes\n\n" + markdown.BlockText("k/one", block)
	}
	tests := map[string]struct {
		reset   bool
		current string
		source  string
		want    func(*testing.T, *ItemPlan)
	}{
		"unchanged block is replaced by the update": {
			current: file(v1), source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 1 || ip.Actions[0].Reason != "update replaces the owned block" {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				if got := string(ip.Actions[0].Content); got != file(v2) {
					t.Errorf("content =\n%q\nwant\n%q", got, file(v2))
				}
			},
		},
		"user modification with equal incoming is kept": {
			current: file(user), source: v1,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 0 {
					t.Fatalf("status %q actions %v, want nothing written", ip.Status, ip.Actions)
				}
			},
		},
		"user modification with different incoming is a conflict": {
			current: file(user), source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "was modified after installation") {
					t.Fatalf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"reset overwrites the user's modification": {
			reset: true, current: file(user), source: v2,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 1 || ip.Actions[0].Reason != "reset to kit replaces the owned block" {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				if got := string(ip.Actions[0].Content); got != file(v2) {
					t.Errorf("content =\n%q", got)
				}
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			it := sectionItem("one", "note.md", "AGENTS.md")
			it.Version = "1.1.0"
			p := Build(Input{
				Kits:      oneKit("k", it),
				Selection: []string{"k/one"},
				Lock: newLock(map[string]*lock.Item{"k/one": {
					Version:  "1.0.0",
					Sections: map[string]lock.Section{"AGENTS.md": {Block: "k/one", Hash: base}},
				}}),
				Snapshot: sharedOf(map[string]target.SharedFileState{
					"AGENTS.md": {Exists: true, Content: tc.current, Blocks: map[string]string{"k/one": hashOf([]byte(blockContent(tc.current, "k/one")))}},
				}),
				Sources: map[string][]byte{it.SourcePath(it.Contributes[0]): []byte(tc.source)},
				Reset:   map[string]bool{"k/one": tc.reset},
			})
			if len(p.Items) != 1 {
				t.Fatal("plan has no items")
			}
			tc.want(t, p.Items[0])
		})
	}
}

// blockContent returns the content between the markers of the block id in the
// shared file text, for the snapshot's block hashes.
func blockContent(file, id string) string {
	for _, b := range markdown.Parse(file).Blocks {
		if b.ID == id {
			return b.Content
		}
	}
	return ""
}

// TestPlanJSONUpdates walks the update rows of the 7.2 table for an owned
// json location, plus the reset row of 7.6.
func TestPlanJSONUpdates(t *testing.T) {
	one := hashOf([]byte("1"))
	two := hashOf([]byte("2"))
	tests := map[string]struct {
		reset   bool
		current string // the shared file content the target holds now
		source  string // the fragment the kit provides
		want    func(*testing.T, *ItemPlan)
	}{
		"unchanged owned value is replaced by the update": {
			current: "{\n  \"a\": 1\n}\n", source: `{"a":2}`,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 1 {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				if !strings.Contains(string(ip.Actions[0].Content), `"a": 2`) {
					t.Errorf("content =\n%s", ip.Actions[0].Content)
				}
				if locs := ip.JSONLocations["config.jsonc"]; len(locs) != 1 || locs[0] != (Location{Pointer: "/a", Hash: two}) {
					t.Errorf("locations = %v", locs)
				}
			},
		},
		"user modification with equal incoming is kept": {
			current: "{\n  \"a\": 99 // user\n}\n", source: `{"a":1}`,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 0 {
					t.Fatalf("status %q actions %v, want nothing written", ip.Status, ip.Actions)
				}
				if locs := ip.JSONLocations["config.jsonc"]; len(locs) != 1 || locs[0] != (Location{Pointer: "/a", Hash: one}) {
					t.Errorf("locations = %v, want the recorded value owned", locs)
				}
			},
		},
		"user modification with different incoming is a conflict": {
			current: "{\n  \"a\": 99\n}\n", source: `{"a":2}`,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "was modified after installation") {
					t.Fatalf("status %q reasons %v", ip.Status, ip.Reasons)
				}
				if len(ip.Actions) != 0 {
					t.Errorf("actions = %v, want none", ip.Actions)
				}
			},
		},
		"reset overwrites the user's modification": {
			reset: true, current: "{\n  \"a\": 99\n}\n", source: `{"a":2}`,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 1 {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				if !strings.Contains(string(ip.Actions[0].Content), `"a": 2`) {
					t.Errorf("content =\n%s", ip.Actions[0].Content)
				}
			},
		},
		"a location the update adds is inserted": {
			current: "{\n  \"a\": 1\n}\n", source: `{"a":1,"b":[2]}`,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 1 {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				content := string(ip.Actions[0].Content)
				if !strings.Contains(content, `"a": 1`) || !strings.Contains(content, `"b": [2]`) {
					t.Errorf("content =\n%s", content)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 2 || locs[0] != (Location{Pointer: "/a", Hash: hashOf([]byte("1"))}) || locs[1] != (Location{Pointer: "/b", Hash: hashOf([]byte("2"))}) {
					t.Errorf("locations = %v, want the kept /a and the new /b", locs)
				}
			},
		},
		"a location the update drops is released with its value kept": {
			current: "{\n  \"a\": 1,\n  \"b\": [2]\n}\n", source: `{"a":3}`,
			want: func(t *testing.T, ip *ItemPlan) {
				if ip.Status != StatusInstall || len(ip.Actions) != 1 {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				// The thinner fragment releases /b: its value stays in the
				// composed file, but the item no longer owns it.
				content := string(ip.Actions[0].Content)
				if !strings.Contains(content, `"a": 3`) || !strings.Contains(content, `"b": [2]`) {
					t.Errorf("content =\n%s", content)
				}
				locs := ip.JSONLocations["config.jsonc"]
				if len(locs) != 1 || locs[0] != (Location{Pointer: "/a", Hash: hashOf([]byte("3"))}) {
					t.Errorf("locations = %v, want only the still-declared /a", locs)
				}
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			it := jsonItem("one", "perm.jsonc", "config.jsonc")
			it.Version = "1.1.0"
			p := Build(Input{
				Kits:      oneKit("k", it),
				Selection: []string{"k/one"},
				Lock: newLock(map[string]*lock.Item{"k/one": {
					Version: "1.0.0",
					JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/a", Hash: one}}},
				}}),
				Snapshot: sharedOf(map[string]target.SharedFileState{
					"config.jsonc": {Exists: true, Content: tc.current},
				}),
				Sources: map[string][]byte{"perm.jsonc": []byte(tc.source)},
				Reset:   map[string]bool{"k/one": tc.reset},
			})
			if len(p.Items) != 1 {
				t.Fatal("plan has no items")
			}
			tc.want(t, p.Items[0])
		})
	}
}

// TestBuildRemoval walks the RFC 9.1 removal table for every contribution
// kind: an owned file deleted unchanged and kept plus reported when modified,
// an owned block deleted with its markers or kept without them, a json value
// removed when its item is the last owner, the shared files penates created
// that a removal leaves without content, and the orphan flow of 9.2 — an
// item of a kit that is gone is planned from the lock and the snapshot alone.
// The directory rows of the table belong to apply, which owns the deletions.
func TestBuildRemoval(t *testing.T) {
	const frag = "## House rules\n\nbody\n"
	blockText := markdown.BlockText("k/one", frag)
	fragHash := hashOf([]byte(frag))
	darkHash := hashOf([]byte(`"dark"`))
	bHash := hashOf([]byte(`"b"`))
	fileHash := hashOf([]byte("installed\n"))
	userHash := hashOf([]byte("user changed\n"))
	tests := map[string]struct {
		lockItems map[string]*lock.Item
		created   []string
		files     map[string]target.FileState
		shared    map[string]target.SharedFileState
		remove    []string
		want      func(*testing.T, *Plan)
	}{
		"unchanged owned file is deleted": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.2.0",
				Files:   map[string]string{"skills/one/SKILL.md": fileHash},
			}},
			files:  map[string]target.FileState{"skills/one/SKILL.md": {Exists: true, Hash: fileHash}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusRemove || ip.Version != "1.2.0" || ip.ItemID != "one" {
					t.Fatalf("item = %+v", ip)
				}
				if len(ip.Actions) != 1 {
					t.Fatalf("actions = %v", ip.Actions)
				}
				a := ip.Actions[0]
				if a.Kind != ActionRemove || a.To != "skills/one/SKILL.md" || a.Reason != "the owned file is removed unchanged" {
					t.Errorf("action = %+v", a)
				}
				if len(ip.Reports) != 0 {
					t.Errorf("reports = %v, want none", ip.Reports)
				}
			},
		},
		"modified owned file is kept and reported": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				Files:   map[string]string{"skills/one/SKILL.md": fileHash},
			}},
			files:  map[string]target.FileState{"skills/one/SKILL.md": {Exists: true, Hash: userHash}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusRemove || len(ip.Actions) != 0 {
					t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
				}
				if len(ip.Reports) != 1 || !strings.Contains(ip.Reports[0], "kept skills/one/SKILL.md: modified by the user") {
					t.Errorf("reports = %v", ip.Reports)
				}
			},
		},
		"a file the user added beside the owned ones is never planned": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				Files:   map[string]string{"skills/one/SKILL.md": fileHash},
			}},
			files: map[string]target.FileState{
				"skills/one/SKILL.md":  {Exists: true, Hash: fileHash},
				"skills/one/extra.md":  {Exists: true, Hash: userHash},
				"skills/one/deeper.md": {Exists: true, Hash: userHash},
			},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 1 || ip.Actions[0].To != "skills/one/SKILL.md" {
					t.Fatalf("actions = %v, want only the owned file", ip.Actions)
				}
				for _, note := range ip.Reports {
					if strings.Contains(note, "extra.md") || strings.Contains(note, "deeper.md") {
						t.Errorf("the user's file reached the plan: %q", note)
					}
				}
			},
		},
		"owned file that is gone already dies silently": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				Files:   map[string]string{"skills/one/SKILL.md": fileHash},
			}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusRemove || len(ip.Actions) != 0 || len(ip.Reports) != 0 {
					t.Errorf("item = %+v", ip)
				}
			},
		},
		"unchanged owned block is deleted with its markers": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version:  "1.0.0",
				Sections: map[string]lock.Section{"AGENTS.md": {Block: "k/one", Hash: fragHash}},
			}},
			created: []string{"AGENTS.md"},
			shared: map[string]target.SharedFileState{"AGENTS.md": {
				Exists:  true,
				Content: "# Notes\n\n" + blockText + "\n",
				Blocks:  map[string]string{"k/one": fragHash},
			}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 1 {
					t.Fatalf("actions = %v", ip.Actions)
				}
				a := ip.Actions[0]
				// The file penates created keeps its other content, so it is
				// rewritten rather than deleted (RFC 9.1).
				if a.Kind != ActionSectionRemove || a.To != "AGENTS.md" || a.Block != "k/one" {
					t.Errorf("action = %+v", a)
				}
				if got := string(a.Content); got != "# Notes\n" {
					t.Errorf("content = %q, want the block gone with its blank line", got)
				}
			},
		},
		"modified block keeps its content without markers and is reported": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version:  "1.0.0",
				Sections: map[string]lock.Section{"AGENTS.md": {Block: "k/one", Hash: fragHash}},
			}},
			shared: map[string]target.SharedFileState{"AGENTS.md": {
				Exists:  true,
				Content: "# Notes\n\n<!-- penates:begin k/one -->\n## House rules\n\nuser body\n<!-- penates:end k/one -->\n\ntail\n",
				Blocks:  map[string]string{"k/one": hashOf([]byte("## House rules\n\nuser body"))},
			}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 1 {
					t.Fatalf("actions = %v", ip.Actions)
				}
				a := ip.Actions[0]
				if a.Kind != ActionSectionRemove {
					t.Errorf("kind = %q", a.Kind)
				}
				if got := string(a.Content); got != "# Notes\n\n## House rules\n\nuser body\n\ntail\n" {
					t.Errorf("content =\n%q\nwant the block text kept without its markers", got)
				}
				if len(ip.Reports) != 1 || !strings.Contains(ip.Reports[0], "kept the block content in AGENTS.md: modified by the user") {
					t.Errorf("reports = %v", ip.Reports)
				}
			},
		},
		"a block the user deleted already plans nothing": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version:  "1.0.0",
				Sections: map[string]lock.Section{"AGENTS.md": {Block: "k/one", Hash: fragHash}},
			}},
			shared: map[string]target.SharedFileState{"AGENTS.md": {Exists: true, Content: "# Notes\n"}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusRemove || len(ip.Actions) != 0 || len(ip.Reports) != 0 {
					t.Errorf("item = %+v", ip)
				}
			},
		},
		"a created shared file left without content is deleted": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version:  "1.0.0",
				Sections: map[string]lock.Section{"AGENTS.md": {Block: "k/one", Hash: fragHash}},
			}},
			created: []string{"AGENTS.md"},
			shared: map[string]target.SharedFileState{"AGENTS.md": {
				Exists:  true,
				Content: blockText + "\n",
				Blocks:  map[string]string{"k/one": fragHash},
			}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 1 {
					t.Fatalf("actions = %v", ip.Actions)
				}
				a := ip.Actions[0]
				if a.Kind != ActionRemove || a.To != "AGENTS.md" || a.Reason != "the shared file penates created is left without content" {
					t.Errorf("action = %+v", a)
				}
			},
		},
		"a shared file the user wrote survives empty": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version:  "1.0.0",
				Sections: map[string]lock.Section{"AGENTS.md": {Block: "k/one", Hash: fragHash}},
			}},
			shared: map[string]target.SharedFileState{"AGENTS.md": {
				Exists:  true,
				Content: blockText + "\n",
				Blocks:  map[string]string{"k/one": fragHash},
			}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				a := p.Items[0].Actions[0]
				// The file is not penates's: it is rewritten without the
				// block, not deleted, however empty that leaves it.
				if a.Kind != ActionSectionRemove || string(a.Content) != "" {
					t.Errorf("action = %+v content %q", a, a.Content)
				}
			},
		},
		"json value of the last owner is removed": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/theme", Hash: darkHash}}},
			}},
			shared: map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"theme": "dark", "editor": "vim"}`}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 1 {
					t.Fatalf("actions = %v", ip.Actions)
				}
				a := ip.Actions[0]
				if a.Kind != ActionJSONRemove || a.To != "config.jsonc" {
					t.Errorf("action = %+v", a)
				}
				if got := string(a.Content); strings.Contains(got, "theme") || !strings.Contains(got, `"editor": "vim"`) {
					t.Errorf("content =\n%s\nwant the value gone and the rest kept", got)
				}
			},
		},
		"an array element is removed by the value it holds": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/tags", Hash: bHash}}},
			}},
			shared: map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"tags": ["a", "b"]}`}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				a := p.Items[0].Actions[0]
				got := string(a.Content)
				if strings.Contains(got, `"b"`) || !strings.Contains(got, `"a"`) {
					t.Errorf("content =\n%s\nwant only the co-owned element left", got)
				}
			},
		},
		"a co-owned value stays while the record dies alone": {
			lockItems: map[string]*lock.Item{
				"k/one": {
					Version: "1.0.0",
					JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/theme", Hash: darkHash}}},
				},
				"other/two": {
					Version: "1.0.0",
					JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/theme", Hash: darkHash}}},
				},
			},
			shared: map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"theme": "dark"}`}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusRemove || len(ip.Actions) != 0 || len(ip.Reports) != 0 {
					t.Errorf("item = %+v", ip)
				}
			},
		},
		"a modified json value is kept and reported": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/theme", Hash: darkHash}}},
			}},
			shared: map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"theme": "light"}`}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 0 {
					t.Fatalf("actions = %v, want none", ip.Actions)
				}
				if len(ip.Reports) != 1 || !strings.Contains(ip.Reports[0], `kept "/theme" in config.jsonc: modified by the user`) {
					t.Errorf("reports = %v", ip.Reports)
				}
			},
		},
		"an array element the user replaced is kept and reported": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/tags", Hash: bHash}}},
			}},
			shared: map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"tags": ["a", "c"]}`}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 0 {
					t.Fatalf("actions = %v, want none: the array is the user's now", ip.Actions)
				}
				if len(ip.Reports) != 1 || !strings.Contains(ip.Reports[0], `kept "/tags" in config.jsonc: modified by the user`) {
					t.Errorf("reports = %v", ip.Reports)
				}
			},
		},
		"an array element the user removed is kept and reported": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/tags", Hash: bHash}}},
			}},
			shared: map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"tags": ["a"]}`}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 0 {
					t.Fatalf("actions = %v, want none: the array is the user's now", ip.Actions)
				}
				if len(ip.Reports) != 1 || !strings.Contains(ip.Reports[0], `kept "/tags" in config.jsonc: modified by the user`) {
					t.Errorf("reports = %v", ip.Reports)
				}
			},
		},
		"a scalar the user replaced with an array holding it loses the value": {
			// The lock records members and array elements under the same
			// (pointer, hash) shape, so an array holding the recorded value
			// reads as the item's element standing there: that element goes,
			// the rest of the user's array stays.
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/theme", Hash: darkHash}}},
			}},
			shared: map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"theme": ["dark", "light"]}`}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if len(ip.Actions) != 1 || ip.Actions[0].Kind != ActionJSONRemove {
					t.Fatalf("actions = %v", ip.Actions)
				}
				got := string(ip.Actions[0].Content)
				if strings.Contains(got, `"dark"`) || !strings.Contains(got, `"light"`) {
					t.Errorf("content =\n%s\nwant the recorded value gone and the user's element kept", got)
				}
				if len(ip.Reports) != 0 {
					t.Errorf("reports = %v, want none", ip.Reports)
				}
			},
		},
		"a json value that is gone already plans nothing": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/theme", Hash: darkHash}}},
			}},
			shared: map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"editor": "vim"}`}},
			remove: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusRemove || len(ip.Actions) != 0 || len(ip.Reports) != 0 {
					t.Errorf("item = %+v", ip)
				}
			},
		},
		"a created json file left without values is deleted": {
			lockItems: map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON:    map[string][]lock.Location{"config.jsonc": {{Pointer: "/theme", Hash: darkHash}}},
			}},
			created: []string{"config.jsonc"},
			shared:  map[string]target.SharedFileState{"config.jsonc": {Exists: true, Content: `{"theme": "dark"}`}},
			remove:  []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				a := p.Items[0].Actions[0]
				if a.Kind != ActionRemove || a.To != "config.jsonc" || a.Reason != "the shared file penates created is left without content" {
					t.Errorf("action = %+v", a)
				}
			},
		},
		"an item that is not installed is excluded": {
			remove: []string{"k/gone"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "the item is not installed") {
					t.Errorf("status %q reasons %v", ip.Status, ip.Reasons)
				}
			},
		},
		"an orphan of a kit that is gone removes from the lock alone": {
			// No kits at all: the item is planned by its lock record and the
			// snapshot, exactly like an item whose kit is available (RFC 9.2).
			lockItems: map[string]*lock.Item{"gone/old": {
				Version: "0.9.0",
				Files:   map[string]string{"skills/old/SKILL.md": fileHash},
			}},
			files:  map[string]target.FileState{"skills/old/SKILL.md": {Exists: true, Hash: fileHash}},
			remove: []string{"gone/old"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusRemove || len(ip.Actions) != 1 || ip.Actions[0].Kind != ActionRemove {
					t.Errorf("item = %+v", ip)
				}
			},
		},
		"several items remove in key order": {
			lockItems: map[string]*lock.Item{
				"k/one": {
					Version: "1.0.0",
					Files:   map[string]string{"skills/one/SKILL.md": fileHash},
				},
				"k/two": {
					Version: "1.0.0",
					Files:   map[string]string{"skills/two/SKILL.md": fileHash},
				},
			},
			files: map[string]target.FileState{
				"skills/one/SKILL.md": {Exists: true, Hash: userHash},
				"skills/two/SKILL.md": {Exists: true, Hash: fileHash},
			},
			remove: []string{"k/two", "k/one"},
			want: func(t *testing.T, p *Plan) {
				if len(p.Items) != 2 || p.Items[0].Key != "k/one" || p.Items[1].Key != "k/two" {
					t.Fatalf("items = %v", p.Items)
				}
				if len(p.Items[0].Actions) != 0 || len(p.Items[1].Actions) != 1 {
					t.Errorf("actions = %v, %v", p.Items[0].Actions, p.Items[1].Actions)
				}
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			lk := newLock(tc.lockItems)
			lk.CreatedSharedFiles = tc.created
			snap := sharedOf(tc.shared)
			for path, state := range tc.files {
				snap.Files[path] = state
			}
			remove := map[string]bool{}
			for _, key := range tc.remove {
				remove[key] = true
			}
			p := BuildRemoval(Input{Lock: lk, Snapshot: snap, Remove: remove})
			if len(p.Items) != len(tc.remove) {
				t.Fatalf("items = %d, want %d", len(p.Items), len(tc.remove))
			}
			tc.want(t, p)
		})
	}
}
