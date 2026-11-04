package plan

import (
	"strings"
	"testing"

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
			lockItems: map[string]*lock.Item{"k/one": {Version: "1.0.0"}},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusNoAction || ip.Reason != "already installed" {
					t.Errorf("status %q reason %q", ip.Status, ip.Reason)
				}
			},
		},
		"two items claiming the same file conflict": {
			kits:      oneKit("k", item("one", "shared.md"), item("two", "shared.md")),
			selection: []string{"k/one", "k/two"},
			want: func(t *testing.T, p *Plan) {
				for _, ip := range p.Items {
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
		"unsupported contribution kind: json": {
			kits: []*kit.Kit{{Name: "k", Items: []*kit.Item{{
				ID: "one", Version: "1.0.0",
				Contributes: []kit.Contribution{
					{Kind: kit.KindJSON, Src: "p.jsonc", To: "config.jsonc"},
				},
			}}}},
			selection: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if !strings.Contains(strings.Join(ip.Reasons, "; "), "json contributions are not supported yet") {
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
		"variables are not supported yet": {
			kits: []*kit.Kit{{Name: "k", Items: []*kit.Item{{
				ID: "one", Version: "1.0.0",
				Vars:        []kit.Var{{ID: "path", Required: true}},
				Contributes: []kit.Contribution{{Kind: kit.KindFile, Src: "s", To: "skills/one/SKILL.md"}},
			}}}},
			selection: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				if ip.Status != StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), "variables are not supported yet") {
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
