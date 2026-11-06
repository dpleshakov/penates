package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

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

// TestRemoveExecutesThePlan walks the RFC 9.1 rows apply carries out: files
// deleted, directories penates created for the item removed while empty and
// kept while the user's files live in them, shared files rewritten, a created
// shared file dropped from the lock with its deletion, and the item's record
// dying with the removal.
func TestRemoveExecutesThePlan(t *testing.T) {
	root := t.TempDir()
	store := lock.NewStore(t.TempDir())
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	writeSource(t, root, "skills/one/SKILL.md", "skill\n")
	writeSource(t, root, "skills/one/keep.md", "user file\n")
	writeSource(t, root, "skills/two/lib.md", "lib\n")
	writeSource(t, root, "AGENTS.md", "# Notes\n\n<!-- penates:begin k/three -->\nold\n<!-- penates:end k/three -->\n\n# Own\n")

	lk.Items["k/one"] = &lock.Item{Version: "1.0.0", Files: map[string]string{
		"skills/one/SKILL.md": hashOf([]byte("skill\n")),
	}, DirsCreated: []string{"skills", "skills/one"}}
	lk.Items["k/two"] = &lock.Item{Version: "1.0.0", Files: map[string]string{
		"skills/two/lib.md": hashOf([]byte("lib\n")),
	}, DirsCreated: []string{"skills/two"}}
	lk.Items["k/three"] = &lock.Item{Version: "1.0.0", Sections: map[string]lock.Section{
		"AGENTS.md": {Block: "k/three", Hash: "sha256:xx"},
	}}
	// config.jsonc is gone from the target already: the deletion is
	// tolerated and the created list still forgets it.
	lk.Items["k/four"] = &lock.Item{Version: "1.0.0"}
	lk.CreatedSharedFiles = []string{"config.jsonc"}
	if err := store.Save(lk); err != nil {
		t.Fatal(err)
	}

	p := &plan.Plan{Items: []*plan.ItemPlan{
		{Key: "k/one", Status: plan.StatusRemove, Actions: []plan.Action{
			{Kind: plan.ActionRemove, To: "skills/one/SKILL.md", Reason: "the owned file is removed unchanged"},
		}},
		{Key: "k/two", Status: plan.StatusRemove, Actions: []plan.Action{
			{Kind: plan.ActionRemove, To: "skills/two/lib.md", Reason: "the owned file is removed unchanged"},
		}},
		{Key: "k/three", Status: plan.StatusRemove, Actions: []plan.Action{
			{Kind: plan.ActionSectionRemove, To: "AGENTS.md", Content: []byte("# Notes\n\n# Own\n")},
		}},
		// config.jsonc is gone from the target already: the deletion is
		// tolerated and the created list still forgets it.
		{Key: "k/four", Status: plan.StatusRemove, Actions: []plan.Action{
			{Kind: plan.ActionRemove, To: "config.jsonc"},
		}},
	}}
	results := Remove(root, p, lk, store)
	if len(results) != 4 {
		t.Fatalf("results = %+v", results)
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("%s failed: %v", r.Key, r.Err)
		}
	}
	// k/one: the owned file is gone, the user's file and its directory stay.
	if _, err := os.Stat(filepath.Join(root, "skills", "one", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("owned file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "one", "keep.md")); err != nil {
		t.Errorf("the user's file was removed: %v", err)
	}
	// k/two: the directory penates created is gone with its last file.
	if _, err := os.Stat(filepath.Join(root, "skills", "two")); !os.IsNotExist(err) {
		t.Errorf("the emptied directory survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); err != nil {
		t.Errorf("the shared parent directory was removed: %v", err)
	}
	// k/three: the shared file is rewritten without the block.
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || string(data) != "# Notes\n\n# Own\n" {
		t.Errorf("AGENTS.md = %q, %v", data, err)
	}
	// The lock on disk holds no removed item and no created shared file.
	reloaded, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Items) != 0 {
		t.Errorf("lock items after removal = %v", reloaded.Items)
	}
	if len(reloaded.CreatedSharedFiles) != 0 {
		t.Errorf("created shared files = %v, want empty", reloaded.CreatedSharedFiles)
	}
}

// TestRemoveRejectsForeignActions pins the guard: a removal item that carries
// an action which is not a removal fails without touching the target or the
// lock.
func TestRemoveRejectsForeignActions(t *testing.T) {
	root := t.TempDir()
	store := lock.NewStore(t.TempDir())
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	writeSource(t, root, "skills/one/SKILL.md", "skill\n")
	lk.Items["k/one"] = &lock.Item{Version: "1.0.0", Files: map[string]string{
		"skills/one/SKILL.md": hashOf([]byte("skill\n")),
	}}
	if err := store.Save(lk); err != nil {
		t.Fatal(err)
	}

	p := &plan.Plan{Items: []*plan.ItemPlan{{
		Key: "k/one", Status: plan.StatusRemove,
		Actions: []plan.Action{{Kind: plan.ActionFile, To: "skills/one/SKILL.md", Content: []byte("x")}},
	}}}
	results := Remove(root, p, lk, store)
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("results = %+v, want one failure", results)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "one", "SKILL.md")); err != nil {
		t.Errorf("the file was touched by a failed removal: %v", err)
	}
	reloaded, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Items["k/one"] == nil {
		t.Errorf("the lock lost the item to a failed removal: %v", reloaded.Items)
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

// TestApplyInstallsJSON applies an item whose json action carries the composed
// shared file: the file is written, the lock records the owned locations and
// the created shared file.
func TestApplyInstallsJSON(t *testing.T) {
	root := t.TempDir()
	store := lock.NewStore(t.TempDir())
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	content := "{\n  \"editor\": \"vim\",\n  \"permission\": {\n    \"bash\": [\"gh *\"]\n  }\n}\n"
	loc := plan.Location{Pointer: "/permission/bash", Hash: hashOf([]byte(`["gh *"]`))}
	p := &plan.Plan{Items: []*plan.ItemPlan{{
		Key: "k/one", KitName: "k", ItemID: "one", Version: "1.0.0", Status: plan.StatusInstall,
		Actions: []plan.Action{{
			Kind:    plan.ActionJSON,
			To:      "config.jsonc",
			Content: []byte(content),
			Reason:  "fragment values are written into the shared file",
		}},
		JSONLocations: map[string][]plan.Location{"config.jsonc": {loc}},
		Answers:       map[string]string{"root": "/opt/x"},
	}}}
	results := Apply(root, p, lk, store)
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v", results)
	}
	data, err := os.ReadFile(filepath.Join(root, "config.jsonc"))
	if err != nil || string(data) != content {
		t.Fatalf("config.jsonc = %q, %v", data, err)
	}
	reloaded, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rec := reloaded.Items["k/one"]
	if rec == nil {
		t.Fatalf("lock holds no k/one: %v", reloaded.Items)
	}
	locs := rec.JSON["config.jsonc"]
	if len(locs) != 1 || locs[0].Pointer != loc.Pointer || locs[0].Hash != loc.Hash {
		t.Errorf("lock locations = %v, want %v", locs, loc)
	}
	if diff := cmp.Diff(map[string]string{"root": "/opt/x"}, rec.Answers); diff != "" {
		t.Errorf("recorded answers (-want +got):\n%s", diff)
	}
	if len(reloaded.CreatedSharedFiles) != 1 || reloaded.CreatedSharedFiles[0] != "config.jsonc" {
		t.Errorf("created shared files = %v, want [config.jsonc]", reloaded.CreatedSharedFiles)
	}
}

// TestApplyRecordsCoOwnershipWithoutWrites pins the satisfied install: every
// location of the item is already present and co-owned, so the plan carries no
// action at all, the file is untouched, and the lock still records the item as
// a co-owner (RFC 7.4).
func TestApplyRecordsCoOwnershipWithoutWrites(t *testing.T) {
	root := t.TempDir()
	before := "{ // kept\n  \"theme\": \"dark\"\n}\n"
	if err := os.WriteFile(filepath.Join(root, "config.jsonc"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	store := lock.NewStore(t.TempDir())
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	loc := plan.Location{Pointer: "/theme", Hash: hashOf([]byte(`"dark"`))}
	p := &plan.Plan{Items: []*plan.ItemPlan{{
		Key: "k/two", KitName: "k", ItemID: "two", Version: "1.0.0", Status: plan.StatusInstall,
		JSONLocations: map[string][]plan.Location{"config.jsonc": {loc}},
	}}}
	results := Apply(root, p, lk, store)
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v", results)
	}
	data, err := os.ReadFile(filepath.Join(root, "config.jsonc"))
	if err != nil || string(data) != before {
		t.Fatalf("config.jsonc changed: %q, %v", data, err)
	}
	reloaded, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rec := reloaded.Items["k/two"]
	if rec == nil {
		t.Fatalf("lock holds no k/two: %v", reloaded.Items)
	}
	locs := rec.JSON["config.jsonc"]
	if len(locs) != 1 || locs[0] != (lock.Location{Pointer: loc.Pointer, Hash: loc.Hash}) {
		t.Errorf("lock locations = %v, want co-ownership at %v", locs, loc)
	}
	if len(reloaded.CreatedSharedFiles) != 0 {
		t.Errorf("created shared files = %v, want none: the file existed", reloaded.CreatedSharedFiles)
	}
}
