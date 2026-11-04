package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// TestInstallEndToEnd walks the core path a client drives: register a kit, load the
// kits of a target, snapshot it, plan a selection, apply it, and plan again. The TUI
// is deliberately absent: this is the core contract.
func TestInstallEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "harness")
	kitDir := t.TempDir()
	writeSource(t, kitDir, "items/one/item.yaml",
		"id: one\nversion: 1.0.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/one/SKILL.md\n")
	writeSource(t, kitDir, "items/one/skill/SKILL.md", "one skill\n")
	writeSource(t, kitDir, "kit.yaml", "name: demo\n")

	// Register the kit for the target, the way a client does.
	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.RegisterKit("demo", kitDir, targetDir)
	if err := stateStore.Save(st); err != nil {
		t.Fatal(err)
	}

	locks := lock.NewStore(dataDir)
	selection := []string{"demo/one"}

	runApply := func() *plan.Plan {
		t.Helper()
		reloaded, err := stateStore.Load()
		if err != nil {
			t.Fatal(err)
		}
		var kits []*kit.Kit
		for _, name := range reloaded.KitsForTarget(targetDir) {
			k, err := kit.Load(reloaded.Kits[name])
			if err != nil {
				t.Fatal(err)
			}
			kits = append(kits, k)
		}
		lk, err := locks.Load(targetDir)
		if err != nil {
			t.Fatal(err)
		}

		var paths []string
		for _, k := range kits {
			for _, it := range k.Items {
				for _, c := range it.Contributes {
					if c.Kind == kit.KindFile {
						paths = append(paths, c.To)
					}
				}
			}
		}
		snap, err := target.SnapshotFiles(targetDir, paths)
		if err != nil {
			t.Fatal(err)
		}
		p := plan.Build(plan.Input{Kits: kits, Selection: selection, Lock: lk, Snapshot: snap})
		results := Apply(targetDir, p, lk, locks)
		for _, r := range results {
			if r.Err != nil {
				t.Fatalf("apply %s: %v", r.Key, r.Err)
			}
		}
		return p
	}

	p := runApply()
	if len(p.Installable()) != 1 {
		t.Fatalf("first plan installable = %v", p.Installable())
	}
	data, err := os.ReadFile(filepath.Join(targetDir, "skills", "one", "SKILL.md"))
	if err != nil || string(data) != "one skill\n" {
		t.Fatalf("installed file = %q, %v", data, err)
	}

	// The second pass over the same selection plans nothing.
	p = runApply()
	if len(p.Installable()) != 0 {
		t.Errorf("re-plan still installable: %v", p.Installable())
	}
	if p.Items[0].Status != plan.StatusNoAction || !strings.Contains(p.Items[0].Reason, "already installed") {
		t.Errorf("re-plan verdict = %q %q", p.Items[0].Status, p.Items[0].Reason)
	}
}
