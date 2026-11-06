package plan

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/target"
)

// TestReconcile compares a lock with snapshots (RFC 7.7): content that
// differs marks its item modified, content that is missing marks it broken,
// and content that stands as recorded marks nothing.
func TestReconcile(t *testing.T) {
	const frag = "installed fragment\n"
	base := hashOf([]byte(frag))
	xHash := hashOf([]byte(`"x"`))
	yHash := hashOf([]byte(`"y"`))
	lockItem := func() *lock.Item {
		return &lock.Item{
			Version:  "1.0.0",
			Files:    map[string]string{"skills/one/SKILL.md": base},
			Sections: map[string]lock.Section{"AGENTS.md": {Block: "k/one", Hash: base}},
			JSON: map[string][]lock.Location{"config.jsonc": {
				{Pointer: "/name", Hash: xHash},
				{Pointer: "/list", Hash: yHash},
			}},
		}
	}
	intactFiles := func() map[string]target.FileState {
		return map[string]target.FileState{
			"skills/one/SKILL.md": {Exists: true, Hash: base},
		}
	}
	intactShared := func() map[string]target.SharedFileState {
		return map[string]target.SharedFileState{
			"AGENTS.md":    {Exists: true, Blocks: map[string]string{"k/one": base}},
			"config.jsonc": {Exists: true, Content: `{"name": "x", "list": ["y"]}`},
		}
	}
	tests := map[string]struct {
		files  map[string]target.FileState
		shared map[string]target.SharedFileState
		want   Reconciliation
	}{
		"everything stands as recorded": {
			files:  intactFiles(),
			shared: intactShared(),
			want:   Reconciliation{},
		},
		"a changed file marks the item modified": {
			files: func() map[string]target.FileState {
				m := intactFiles()
				m["skills/one/SKILL.md"] = target.FileState{Exists: true, Hash: hashOf([]byte("user edit\n"))}
				return m
			}(),
			shared: intactShared(),
			want:   Reconciliation{Modified: map[string]bool{"k/one": true}},
		},
		"a missing file breaks the item": {
			files: func() map[string]target.FileState {
				m := intactFiles()
				m["skills/one/SKILL.md"] = target.FileState{}
				return m
			}(),
			shared: intactShared(),
			want:   Reconciliation{Broken: map[string]bool{"k/one": true}},
		},
		"a directory in place of the file breaks the item": {
			files: func() map[string]target.FileState {
				m := intactFiles()
				m["skills/one/SKILL.md"] = target.FileState{Exists: true}
				return m
			}(),
			shared: intactShared(),
			want:   Reconciliation{Broken: map[string]bool{"k/one": true}},
		},
		"a changed block marks the item modified": {
			files: intactFiles(),
			shared: func() map[string]target.SharedFileState {
				m := intactShared()
				m["AGENTS.md"] = target.SharedFileState{Exists: true, Blocks: map[string]string{"k/one": hashOf([]byte("user fragment\n"))}}
				return m
			}(),
			want: Reconciliation{Modified: map[string]bool{"k/one": true}},
		},
		"a removed block breaks the item": {
			files: intactFiles(),
			shared: func() map[string]target.SharedFileState {
				m := intactShared()
				m["AGENTS.md"] = target.SharedFileState{Exists: true, Blocks: map[string]string{}}
				return m
			}(),
			want: Reconciliation{Broken: map[string]bool{"k/one": true}},
		},
		"a missing shared file breaks the item": {
			files: intactFiles(),
			shared: func() map[string]target.SharedFileState {
				m := intactShared()
				m["AGENTS.md"] = target.SharedFileState{}
				return m
			}(),
			want: Reconciliation{Broken: map[string]bool{"k/one": true}},
		},
		"a changed json value marks the item modified": {
			files: intactFiles(),
			shared: func() map[string]target.SharedFileState {
				m := intactShared()
				m["config.jsonc"] = target.SharedFileState{Exists: true, Content: `{"name": "z", "list": ["y"]}`}
				return m
			}(),
			want: Reconciliation{Modified: map[string]bool{"k/one": true}},
		},
		"a gone json value breaks the item": {
			files: intactFiles(),
			shared: func() map[string]target.SharedFileState {
				m := intactShared()
				m["config.jsonc"] = target.SharedFileState{Exists: true, Content: `{"list": ["y"]}`}
				return m
			}(),
			want: Reconciliation{Broken: map[string]bool{"k/one": true}},
		},
		"a gone array element breaks the item": {
			files: intactFiles(),
			shared: func() map[string]target.SharedFileState {
				m := intactShared()
				m["config.jsonc"] = target.SharedFileState{Exists: true, Content: `{"name": "x", "list": []}`}
				return m
			}(),
			want: Reconciliation{Broken: map[string]bool{"k/one": true}},
		},
		"a shared file without a document breaks the item": {
			files: intactFiles(),
			shared: func() map[string]target.SharedFileState {
				m := intactShared()
				m["config.jsonc"] = target.SharedFileState{Exists: true, Content: "not json at all\n"}
				return m
			}(),
			want: Reconciliation{Broken: map[string]bool{"k/one": true}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := Reconcile(newLock(map[string]*lock.Item{"k/one": lockItem()}), &target.Snapshot{Root: "/target", Files: tc.files, Shared: tc.shared})
			if diff := cmp.Diff(tc.want, *got); diff != "" {
				t.Errorf("reconciliation (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPlanRepairsBrokenItems plans an item the reconciliation marked broken
// at its installed version: the lost content is installed again through the
// ordinary absent-content rules (RFC 7.7).
func TestPlanRepairsBrokenItems(t *testing.T) {
	const frag = "installed fragment\n"
	base := hashOf([]byte(frag))

	t.Run("file", func(t *testing.T) {
		it := item("one", "skills/one/SKILL.md")
		p := Build(Input{
			Kits:      oneKit("k", it),
			Selection: []string{"k/one"},
			Lock: newLock(map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				Files:   map[string]string{"skills/one/SKILL.md": base},
			}}),
			Snapshot:   snapOf(nil),
			Sources:    planSources(oneKit("k", it), frag),
			Reconciled: &Reconciliation{Broken: map[string]bool{"k/one": true}},
		})
		ip := p.Items[0]
		if ip.Status != StatusInstall || len(ip.Actions) != 1 || ip.Actions[0].Reason != "target file is absent" {
			t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
		}
		if string(ip.Actions[0].Content) != frag {
			t.Errorf("content = %q, want the kit fragment", ip.Actions[0].Content)
		}
	})

	t.Run("section", func(t *testing.T) {
		it := sectionItem("one", "note.md", "AGENTS.md")
		p := Build(Input{
			Kits:      oneKit("k", it),
			Selection: []string{"k/one"},
			Lock: newLock(map[string]*lock.Item{"k/one": {
				Version:  "1.0.0",
				Sections: map[string]lock.Section{"AGENTS.md": {Block: "k/one", Hash: base}},
			}}),
			Snapshot: sharedOf(map[string]target.SharedFileState{
				"AGENTS.md": {Exists: true, Content: "# Notes\n\n", Blocks: map[string]string{}},
			}),
			Sources:    planSources(oneKit("k", it), frag),
			Reconciled: &Reconciliation{Broken: map[string]bool{"k/one": true}},
		})
		ip := p.Items[0]
		if ip.Status != StatusInstall || len(ip.Actions) != 1 || ip.Actions[0].Reason != "block is absent; it will be appended" {
			t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
		}
	})

	t.Run("json", func(t *testing.T) {
		it := jsonItem("one", "perm.jsonc", "config.jsonc")
		p := Build(Input{
			Kits:      oneKit("k", it),
			Selection: []string{"k/one"},
			Lock: newLock(map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				JSON: map[string][]lock.Location{"config.jsonc": {
					{Pointer: "/theme", Hash: hashOf([]byte(`"dark"`))},
				}},
			}}),
			Snapshot: sharedOf(map[string]target.SharedFileState{
				"config.jsonc": {Exists: true, Content: "{\n  // editor the user chose by hand\n  \"editor\": \"vim\"\n}\n"},
			}),
			Sources:    map[string][]byte{"perm.jsonc": []byte(`{"theme": "dark"}`)},
			Reconciled: &Reconciliation{Broken: map[string]bool{"k/one": true}},
		})
		ip := p.Items[0]
		if ip.Status != StatusInstall || len(ip.Actions) != 1 || ip.Actions[0].Reason != "fragment values are written into the shared file" {
			t.Fatalf("status %q actions %v", ip.Status, ip.Actions)
		}
	})

	t.Run("without the outcome an equal version stays put", func(t *testing.T) {
		it := item("one", "skills/one/SKILL.md")
		p := Build(Input{
			Kits:      oneKit("k", it),
			Selection: []string{"k/one"},
			Lock: newLock(map[string]*lock.Item{"k/one": {
				Version: "1.0.0",
				Files:   map[string]string{"skills/one/SKILL.md": base},
			}}),
			Snapshot: snapOf(nil),
			Sources:  planSources(oneKit("k", it), frag),
		})
		ip := p.Items[0]
		if ip.Status != StatusNoAction || ip.Reason != "already installed" {
			t.Errorf("status %q reason %q, want no repair without a reconciliation", ip.Status, ip.Reason)
		}
	})
}

// TestPlanModifiedNoAction keeps a modified item at its installed version and
// names the modification in the no-action reason (RFC 7.7).
func TestPlanModifiedNoAction(t *testing.T) {
	const frag = "installed body\n"
	it := item("one", "skills/one/SKILL.md")
	p := Build(Input{
		Kits:      oneKit("k", it),
		Selection: []string{"k/one"},
		Lock: newLock(map[string]*lock.Item{"k/one": {
			Version: "1.0.0",
			Files:   map[string]string{"skills/one/SKILL.md": hashOf([]byte(frag))},
		}}),
		Snapshot: snapOf(map[string]target.FileState{
			"skills/one/SKILL.md": {Exists: true, Hash: hashOf([]byte("user body\n"))},
		}),
		Sources:    planSources(oneKit("k", it), frag),
		Reconciled: &Reconciliation{Modified: map[string]bool{"k/one": true}},
	})
	ip := p.Items[0]
	if ip.Status != StatusNoAction || ip.Reason != "already installed; the content was modified after installation" {
		t.Errorf("status %q reason %q", ip.Status, ip.Reason)
	}
	if len(ip.Actions) != 0 {
		t.Errorf("actions = %v, want none: the modification is kept", ip.Actions)
	}
}
