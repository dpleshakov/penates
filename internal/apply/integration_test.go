package apply

import (
	"crypto/sha256"
	"encoding/hex"
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
		snap, err := target.Read(targetDir, paths, nil)
		if err != nil {
			t.Fatal(err)
		}
		sources, err := kit.ReadSources(kits, selection)
		if err != nil {
			t.Fatal(err)
		}
		p := plan.Build(plan.Input{Kits: kits, Selection: selection, Lock: lk, Snapshot: snap, Sources: sources})
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

// TestInstallSectionEndToEnd installs the agenda item of the fixture example kit
// into an empty target: the section contribution must create the shared AGENTS.md
// with the marked block, the lock must record the block and the created shared file,
// and a second pass over the same selection must plan nothing. The agenda item is
// section-only, so the item is plannable before json contributions are supported;
// gh-note keeps the fixture's json contribution for the json slices.
func TestInstallSectionEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "harness")
	kitDir := filepath.Join("..", "kit", "testdata", "example")
	fragment, err := os.ReadFile(filepath.Join(kitDir, "items", "agenda", "agenda.md"))
	if err != nil {
		t.Fatal(err)
	}

	// Register the fixture kit for the target, the way a client does.
	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.RegisterKit("example", kitDir, targetDir)
	if err := stateStore.Save(st); err != nil {
		t.Fatal(err)
	}

	locks := lock.NewStore(dataDir)
	selection := []string{"example/agenda"}
	selected := map[string]bool{}
	for _, key := range selection {
		selected[key] = true
	}

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

		// The snapshot covers the file paths of the selection's file
		// contributions and the shared files of the loaded kits.
		var paths, sharedPaths []string
		for _, k := range kits {
			sharedPaths = append(sharedPaths, k.Shared...)
			for _, it := range k.Items {
				if !selected[kit.ItemKey(k.Name, it.ID)] {
					continue
				}
				for _, c := range it.Contributes {
					if c.Kind == kit.KindFile {
						paths = append(paths, c.To)
					}
				}
			}
		}
		snap, err := target.Read(targetDir, paths, sharedPaths)
		if err != nil {
			t.Fatal(err)
		}
		sources, err := kit.ReadSources(kits, selection)
		if err != nil {
			t.Fatal(err)
		}
		p := plan.Build(plan.Input{Kits: kits, Selection: selection, Lock: lk, Snapshot: snap, Sources: sources})
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
		t.Fatalf("plan installable = %v", p.Installable())
	}

	// The shared file did not exist: it now holds the marked block alone.
	want := "<!-- penates:begin example/agenda -->\n" + string(fragment) + "\n<!-- penates:end example/agenda -->\n"
	got, err := os.ReadFile(filepath.Join(targetDir, "AGENTS.md"))
	if err != nil || string(got) != want {
		t.Fatalf("AGENTS.md = %q, %v; want %q", got, err, want)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "config.jsonc")); !os.IsNotExist(err) {
		t.Errorf("config.jsonc was written without a json action: %v", err)
	}

	// The lock records the block, the hash of the fragment, and the shared file
	// penates created; the untouched config.jsonc is not among them.
	fresh, err := locks.Load(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	rec := fresh.Items["example/agenda"]
	if rec == nil {
		t.Fatalf("lock holds no example/agenda: %v", fresh.Items)
	}
	if rec.Version != "0.1.0" {
		t.Errorf("lock version = %q, want 0.1.0", rec.Version)
	}
	sum := sha256.Sum256(fragment)
	wantHash := "sha256:" + hex.EncodeToString(sum[:])
	sec, ok := rec.Sections["AGENTS.md"]
	if !ok {
		t.Fatalf("lock sections = %v, want AGENTS.md", rec.Sections)
	}
	if sec.Block != "example/agenda" || sec.Hash != wantHash {
		t.Errorf("lock section = %q %q, want block example/agenda hash %q", sec.Block, sec.Hash, wantHash)
	}
	if len(fresh.CreatedSharedFiles) != 1 || fresh.CreatedSharedFiles[0] != "AGENTS.md" {
		t.Errorf("created shared files = %v, want [AGENTS.md]", fresh.CreatedSharedFiles)
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
