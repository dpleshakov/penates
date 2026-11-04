package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
)

// stagedPlan builds a plan with installable items whose actions carry content the
// way the planner emits them: the sources under kitDir, read at planning time.
func stagedPlan(kitDir string, keys ...string) *plan.Plan {
	p := &plan.Plan{}
	for _, key := range keys {
		_, id, _ := strings.Cut(key, "/")
		ip := &plan.ItemPlan{Key: key, KitName: "k", ItemID: id, Version: "1.0.0", Status: plan.StatusInstall}
		for _, to := range []string{"skills/" + id + "/SKILL.md", "skills/" + id + "/lib.md"} {
			content, err := os.ReadFile(filepath.Join(kitDir, filepath.FromSlash(to)))
			if err != nil {
				content = nil // the planner would have excluded the item; here it just writes nothing
			}
			ip.Actions = append(ip.Actions, plan.Action{
				Kind:    plan.ActionFile,
				To:      to,
				Content: content,
				Reason:  "target file is absent",
			})
		}
		p.Items = append(p.Items, ip)
	}
	return p
}

func TestApplyInstalls(t *testing.T) {
	root := t.TempDir()
	kitDir := t.TempDir()
	dataDir := t.TempDir()
	writeSource(t, kitDir, "skills/one/SKILL.md", "skill\n")
	writeSource(t, kitDir, "skills/one/lib.md", "lib\n")

	store := lock.NewStore(dataDir)
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	results := Apply(root, stagedPlan(kitDir, "k/one"), lk, store)
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v", results)
	}

	wantContent := map[string]string{
		"skills/one/SKILL.md": "skill\n",
		"skills/one/lib.md":   "lib\n",
	}
	for rel, want := range wantContent {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("installed file %s: %v", rel, err)
		}
		if string(data) != want {
			t.Errorf("content of %s = %q, want %q", rel, data, want)
		}
	}

	// The lock on disk records the item with hashes and created directories.
	reloaded, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	item, ok := reloaded.Items["k/one"]
	if !ok {
		t.Fatalf("lock items = %v", reloaded.Items)
	}
	if item.Version != "1.0.0" {
		t.Errorf("version = %q", item.Version)
	}
	if len(item.Files) != 2 {
		t.Errorf("files = %v", item.Files)
	}
	for to, hash := range item.Files {
		if !strings.HasPrefix(hash, "sha256:") {
			t.Errorf("hash of %s = %q", to, hash)
		}
	}
	if diff := len(item.DirsCreated); diff != 2 {
		t.Errorf("dirs created = %v, want skills and skills/one", item.DirsCreated)
	}
}

func TestApplyFailureLeavesNothing(t *testing.T) {
	root := t.TempDir()
	kitDir := t.TempDir()
	dataDir := t.TempDir()
	// The first action stages fine; the destination of its rename is blocked by a
	// directory, so the item fails before the second file moves.
	writeSource(t, kitDir, "skills/one/SKILL.md", "skill\n")
	if err := os.MkdirAll(filepath.Join(root, "skills", "one", "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	store := lock.NewStore(dataDir)
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	results := Apply(root, stagedPlan(kitDir, "k/one"), lk, store)
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("results = %+v, want one failure", results)
	}

	// No temp files, no directories beyond the blocked path, no lock entry.
	entries, err := os.ReadDir(filepath.Join(root, "skills", "one"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Errorf("leftovers after failure: %v", entries)
	}
	reloaded, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Items) != 0 {
		t.Errorf("lock records a failed item: %v", reloaded.Items)
	}
}

func TestFailureOfOneItemDoesNotBlockTheNext(t *testing.T) {
	root := t.TempDir()
	kitDir := t.TempDir()
	dataDir := t.TempDir()
	writeSource(t, kitDir, "skills/good/SKILL.md", "good skill\n")
	writeSource(t, kitDir, "skills/good/lib.md", "good lib\n")
	// The bad item's source is missing entirely.
	if err := os.MkdirAll(filepath.Join(root, "skills", "bad", "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	store := lock.NewStore(dataDir)
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	results := Apply(root, stagedPlan(kitDir, "k/bad", "k/good"), lk, store)
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Err == nil {
		t.Errorf("bad item did not fail: %+v", results[0])
	}
	if results[1].Err != nil {
		t.Errorf("good item failed: %+v", results[1])
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("good item was not installed: %v", err)
	}
	reloaded, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Items["k/good"]; !ok {
		t.Errorf("lock misses the good item: %v", reloaded.Items)
	}
	if _, ok := reloaded.Items["k/bad"]; ok {
		t.Errorf("lock records the bad item")
	}
}

func writeSource(t *testing.T, kitDir, rel, content string) {
	t.Helper()
	full := filepath.Join(kitDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
