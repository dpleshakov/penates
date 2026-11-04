package plan

import (
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
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

func TestBuild(t *testing.T) {
	tests := map[string]struct {
		kits      []*kit.Kit
		selection []string
		lockItems map[string]*lock.Item
		snapshot  map[string]target.FileState
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
		"unsupported contribution kinds": {
			kits: []*kit.Kit{{Name: "k", Items: []*kit.Item{{
				ID: "one", Version: "1.0.0",
				Contributes: []kit.Contribution{
					{Kind: kit.KindSection, Src: "note.md", To: "AGENTS.md"},
					{Kind: kit.KindJSON, Src: "p.jsonc", To: "config.jsonc"},
				},
			}}}},
			selection: []string{"k/one"},
			want: func(t *testing.T, p *Plan) {
				ip := p.Items[0]
				joined := strings.Join(ip.Reasons, "; ")
				for _, want := range []string{"section contributions are not supported yet", "json contributions are not supported yet"} {
					if !strings.Contains(joined, want) {
						t.Errorf("reasons %q miss %q", joined, want)
					}
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
			p := Build(Input{
				Kits:      tc.kits,
				Selection: tc.selection,
				Lock:      lk,
				Snapshot:  snapOf(tc.snapshot),
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
