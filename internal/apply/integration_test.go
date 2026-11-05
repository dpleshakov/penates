package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// TestInstallEndToEnd walks the core path a client drives: register a kit, load the
// kits of a target, snapshot it, plan a selection, apply it, and plan again. The TUI
// is deliberately absent: this is the core contract. The item declares a required
// variable, so the client passes its form answers into planning; the templated
// file installs substituted and the lock records the answers for reuse.
func TestInstallEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "harness")
	kitDir := t.TempDir()
	writeSource(t, kitDir, "items/one/item.yaml",
		"id: one\nversion: 1.0.0\nvars:\n  - id: root\n    prompt: Root path\n    required: true\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/one/SKILL.md\n  - file: conf.md\n    to: skills/one/conf.md\n    template: true\n")
	writeSource(t, kitDir, "items/one/skill/SKILL.md", "one skill\n")
	writeSource(t, kitDir, "items/one/conf.md", "root: {{root}}\n")
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

	runApply := func(fresh map[string]map[string]string) *plan.Plan {
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
		p := plan.Build(plan.Input{Kits: kits, Selection: selection, Lock: lk, Snapshot: snap, Sources: sources, Answers: fresh})
		results := Apply(targetDir, p, lk, locks)
		for _, r := range results {
			if r.Err != nil {
				t.Fatalf("apply %s: %v", r.Key, r.Err)
			}
		}
		return p
	}

	// The form answers carry the required variable; without them the item
	// would be excluded.
	p := runApply(nil)
	if ip := p.Items[0]; ip.Status != plan.StatusError || !strings.Contains(strings.Join(ip.Reasons, "; "), `required variable "root" has no value`) {
		t.Fatalf("plan without answers = %q %v", ip.Status, ip.Reasons)
	}

	p = runApply(map[string]map[string]string{"demo/one": {"root": "/opt/x"}})
	if len(p.Installable()) != 1 {
		t.Fatalf("first plan installable = %v", p.Installable())
	}
	data, err := os.ReadFile(filepath.Join(targetDir, "skills", "one", "SKILL.md"))
	if err != nil || string(data) != "one skill\n" {
		t.Fatalf("installed file = %q, %v", data, err)
	}
	conf, err := os.ReadFile(filepath.Join(targetDir, "skills", "one", "conf.md"))
	if err != nil || string(conf) != "root: /opt/x\n" {
		t.Fatalf("templated file = %q, %v; want the substituted content", conf, err)
	}
	lk, err := locks.Load(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(map[string]string{"root": "/opt/x"}, lk.Items["demo/one"].Answers); diff != "" {
		t.Errorf("recorded answers (-want +got):\n%s", diff)
	}

	// The second pass over the same selection plans nothing, and needs no
	// answers: the item is installed.
	p = runApply(nil)
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

// TestInstallJSONEndToEnd installs the gh-note item of the fixture example kit
// into a target whose config.jsonc the user wrote by hand: the section creates
// AGENTS.md, the json values are inserted into the existing JSONC with its
// comment untouched, the reloaded lock records the owned location, and a second
// pass over the same selection plans nothing.
func TestInstallJSONEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "harness")
	kitDir := filepath.Join("..", "kit", "testdata", "example")
	userConfig := "{\n  // editor the user chose by hand\n  \"editor\": \"vim\"\n}\n"
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "config.jsonc"), []byte(userConfig), 0o644); err != nil {
		t.Fatal(err)
	}

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
	selection := []string{"example/gh-note"}

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
		var paths, sharedPaths []string
		for _, k := range kits {
			sharedPaths = append(sharedPaths, k.Shared...)
			for _, it := range k.Items {
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

	// The user's JSONC kept its bytes and gained the fragment's values.
	wantConfig := "{\n  // editor the user chose by hand\n  \"editor\": \"vim\",\n  \"permission\": {\n    \"bash\": [\"gh *\"]\n  }\n}\n"
	got, err := os.ReadFile(filepath.Join(targetDir, "config.jsonc"))
	if err != nil || string(got) != wantConfig {
		t.Fatalf("config.jsonc =\n%q\n%v\nwant\n%q", got, err, wantConfig)
	}
	fragment, err := os.ReadFile(filepath.Join(kitDir, "items", "gh-note", "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	wantAgents := "<!-- penates:begin example/gh-note -->\n" + string(fragment) + "\n<!-- penates:end example/gh-note -->\n"
	gotAgents, err := os.ReadFile(filepath.Join(targetDir, "AGENTS.md"))
	if err != nil || string(gotAgents) != wantAgents {
		t.Fatalf("AGENTS.md = %q, %v; want %q", gotAgents, err, wantAgents)
	}

	// The reloaded lock records the block, the owned location with the hash of
	// its value, and the shared file penates created; the user's config.jsonc
	// is not among them.
	fresh, err := locks.Load(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	rec := fresh.Items["example/gh-note"]
	if rec == nil {
		t.Fatalf("lock holds no example/gh-note: %v", fresh.Items)
	}
	sum := sha256.Sum256([]byte(`"gh *"`))
	wantLoc := lock.Location{Pointer: "/permission/bash", Hash: "sha256:" + hex.EncodeToString(sum[:])}
	locs := rec.JSON["config.jsonc"]
	if len(locs) != 1 || locs[0] != wantLoc {
		t.Errorf("lock json = %v, want [%v]", locs, wantLoc)
	}
	if sec, ok := rec.Sections["AGENTS.md"]; !ok || sec.Block != "example/gh-note" {
		t.Errorf("lock sections = %v", rec.Sections)
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
