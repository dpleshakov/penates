package apply

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/target"
)

// boundary names one step event the way the tests assert them: which item,
// which boundary, and whether the result carries a failure.
func boundary(ev StepEvent) string {
	switch {
	case ev.Start:
		return ev.Key + " start"
	case ev.Err != nil:
		return ev.Key + " failed"
	default:
		return ev.Key + " done"
	}
}

// TestStepReportsBoundaries pins the step reporting: every step of Apply and
// Remove reports its start and then its result, the result of a failed step
// carries the failure, and the order follows the plan.
func TestStepReportsBoundaries(t *testing.T) {
	root := t.TempDir()
	kitDir := t.TempDir()
	dataDir := t.TempDir()
	// The bad item's destination is blocked by a directory, so its step fails
	// before the second file moves; the good item installs.
	writeSource(t, kitDir, "skills/good/SKILL.md", "good skill\n")
	writeSource(t, kitDir, "skills/good/lib.md", "good lib\n")
	if err := os.MkdirAll(filepath.Join(root, "skills", "bad", "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	store := lock.NewStore(dataDir)
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	var events []StepEvent
	results := Apply(root, stagedPlan(kitDir, "k/bad", "k/good"), lk, store, func(ev StepEvent) {
		events = append(events, ev)
	})
	if len(results) != 2 || results[0].Err == nil || results[1].Err != nil {
		t.Fatalf("results = %+v, want one failure then one success", results)
	}
	want := []string{"k/bad start", "k/bad failed", "k/good start", "k/good done"}
	if got := boundaries(events); diffLines(want, got) != "" {
		t.Errorf("apply events = %v, want %v", got, want)
	}
	if events[1].Err != results[0].Err {
		t.Errorf("reported failure = %v, want the result failure %v", events[1].Err, results[0].Err)
	}

	// A removal step reports the same way.
	writeSource(t, root, "skills/one/SKILL.md", "skill\n")
	lk.SetItem("k/one", &lock.Item{Version: "1.0.0", Files: map[string]string{
		"skills/one/SKILL.md": hashOf([]byte("skill\n")),
	}})
	if err := store.Save(lk); err != nil {
		t.Fatal(err)
	}
	rp := &plan.Plan{Items: []*plan.ItemPlan{{
		Key: "k/one", Status: plan.StatusRemove, Actions: []plan.Action{
			{Kind: plan.ActionRemove, To: "skills/one/SKILL.md"},
		},
	}}}
	events = nil
	results = Remove(root, rp, lk, store, func(ev StepEvent) {
		events = append(events, ev)
	})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("removal results = %+v", results)
	}
	want = []string{"k/one start", "k/one done"}
	if got := boundaries(events); diffLines(want, got) != "" {
		t.Errorf("remove events = %v, want %v", got, want)
	}
}

func boundaries(events []StepEvent) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = boundary(ev)
	}
	return out
}

// diffLines returns "" when got equals want, and a want/got listing otherwise.
func diffLines(want, got []string) string {
	if len(want) != len(got) {
		return "want " + strings.Join(want, ", ") + "; got " + strings.Join(got, ", ")
	}
	for i := range want {
		if want[i] != got[i] {
			return "want " + strings.Join(want, ", ") + "; got " + strings.Join(got, ", ")
		}
	}
	return ""
}

// mixedFixture writes the target of a mixed run and its lock: the installed
// item k/a owns a file, a block in a shared file penates created, and the
// directories penates created for it; the removal plan takes k/a away, and the
// install plans add k/b and k/c. Everything a step of the run touches is in
// play: a file deletion, a shared-file rewrite, directory cleanup, and
// directory creation.
func mixedFixture(t *testing.T) (root string, store *lock.Store, removals *plan.Plan, installs []*plan.ItemPlan) {
	t.Helper()
	root = t.TempDir()
	store = lock.NewStore(t.TempDir())
	writeSource(t, root, "AGENTS.md", "# Notes\n\n<!-- penates:begin k/a -->\nold a\n<!-- penates:end k/a -->\n\n# Own\n")
	writeSource(t, root, "skills/a/SKILL.md", "a skill\n")

	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	lk.Items["k/a"] = &lock.Item{
		Version:     "1.0.0",
		Files:       map[string]string{"skills/a/SKILL.md": hashOf([]byte("a skill\n"))},
		DirsCreated: []string{"skills", "skills/a"},
		Sections:    map[string]lock.Section{"AGENTS.md": {Block: "k/a", Hash: hashOf([]byte("old a\n"))}},
	}
	lk.CreatedSharedFiles = []string{"AGENTS.md"}
	if err := store.Save(lk); err != nil {
		t.Fatal(err)
	}

	removals = &plan.Plan{Items: []*plan.ItemPlan{{
		Key: "k/a", Status: plan.StatusRemove, Actions: []plan.Action{
			{Kind: plan.ActionRemove, To: "skills/a/SKILL.md"},
			{Kind: plan.ActionSectionRemove, To: "AGENTS.md", Content: []byte("# Notes\n\n# Own\n")},
		},
	}}}
	installs = []*plan.ItemPlan{
		{
			Key: "k/b", KitName: "k", ItemID: "b", Version: "1.0.0", Status: plan.StatusInstall,
			Actions: []plan.Action{
				{Kind: plan.ActionFile, To: "skills/b/one.md", Content: []byte("one\n")},
				{Kind: plan.ActionFile, To: "skills/b/two.md", Content: []byte("two\n")},
			},
		},
		{
			Key: "k/c", KitName: "k", ItemID: "c", Version: "1.0.0", Status: plan.StatusInstall,
			Actions: []plan.Action{
				{Kind: plan.ActionFile, To: "skills/c/note.md", Content: []byte("note\n")},
			},
		},
	}
	return root, store, removals, installs
}

// runMonolithic runs the mixed fixture through the monolithic compositions:
// Remove first, then Apply, on a fresh copy of the fixture. It returns the
// target content tree and the reloaded lock the run leaves behind.
func runMonolithic(t *testing.T) (map[string]string, *lock.Lock) {
	t.Helper()
	root, store, removals, installs := mixedFixture(t)
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Remove(root, removals, lk, store, nil) {
		if r.Err != nil {
			t.Fatalf("remove %s: %v", r.Key, r.Err)
		}
	}
	for _, r := range Apply(root, &plan.Plan{Items: installs}, lk, store, nil) {
		if r.Err != nil {
			t.Fatalf("apply %s: %v", r.Key, r.Err)
		}
	}
	return targetTree(t, root), lockState(t, store, root)
}

// lockState reloads the lock of root from the store, blanking the target path:
// the run directories of two runs differ, their records must not.
func lockState(t *testing.T, store *lock.Store, root string) *lock.Lock {
	t.Helper()
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	lk.Target = ""
	return lk
}

// targetTree walks the regular files of root into a map of slash paths to
// content. A staged temp file a step left behind shows up here and breaks the
// comparison.
func targetTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSteppedRunMatchesMonolithic walks the mixed fixture step by step — the
// way a client that shows per-item progress drives the steps — and holds the
// run against the monolithic compositions: the same target content and the
// same lock records come out.
func TestSteppedRunMatchesMonolithic(t *testing.T) {
	wantTree, wantLock := runMonolithic(t)

	root, store, removals, installs := mixedFixture(t)
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// The steps run in the invariant order: the removals first, then the
	// installs, one item per step.
	for _, ip := range removals.Items {
		if err := RemoveItem(root, ip, lk, store); err != nil {
			t.Fatalf("remove step %s: %v", ip.Key, err)
		}
	}
	for _, ip := range installs {
		if err := Item(root, ip, lk, store); err != nil {
			t.Fatalf("apply step %s: %v", ip.Key, err)
		}
	}

	if diff := cmp.Diff(wantTree, targetTree(t, root)); diff != "" {
		t.Errorf("target content (-monolithic +stepped):\n%s", diff)
	}
	if diff := cmp.Diff(wantLock, lockState(t, store, root)); diff != "" {
		t.Errorf("lock (-monolithic +stepped):\n%s", diff)
	}
}

// TestInterruptionBetweenStepsRepairs stops a stepped run after each kind of
// step — a removal, then an install — and holds the interrupted state against
// the existing reconciliation: nothing broken, nothing modified, and the rest
// of the run brings the target and the lock to the monolithic outcome.
func TestInterruptionBetweenStepsRepairs(t *testing.T) {
	t.Run("after a removal step", func(t *testing.T) {
		wantTree, wantLock := runMonolithic(t)
		root, store, removals, installs := mixedFixture(t)
		lk, err := store.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := RemoveItem(root, removals.Items[0], lk, store); err != nil {
			t.Fatalf("remove step %s: %v", removals.Items[0].Key, err)
		}
		assertRepairable(t, root, store)
		for _, ip := range installs {
			if err := Item(root, ip, lk, store); err != nil {
				t.Fatalf("apply step %s: %v", ip.Key, err)
			}
		}
		assertSameRun(t, wantTree, wantLock, root, store)
	})

	t.Run("after an install step", func(t *testing.T) {
		wantTree, wantLock := runMonolithic(t)
		root, store, removals, installs := mixedFixture(t)
		lk, err := store.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := RemoveItem(root, removals.Items[0], lk, store); err != nil {
			t.Fatalf("remove step %s: %v", removals.Items[0].Key, err)
		}
		if err := Item(root, installs[0], lk, store); err != nil {
			t.Fatalf("apply step %s: %v", installs[0].Key, err)
		}
		assertRepairable(t, root, store)
		if err := Item(root, installs[1], lk, store); err != nil {
			t.Fatalf("apply step %s: %v", installs[1].Key, err)
		}
		assertSameRun(t, wantTree, wantLock, root, store)
	})
}

// assertRepairable reconciles the lock with the target the way the next start
// does and requires the interrupted state to come out clean: nothing broken,
// nothing modified, no staged files.
func assertRepairable(t *testing.T, root string, store *lock.Store) {
	t.Helper()
	lk, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	files, shared := lk.RecordedPaths()
	snap, err := target.Read(root, files, shared)
	if err != nil {
		t.Fatal(err)
	}
	rec, staged, err := Reconcile(root, lk, snap)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(staged) != 0 {
		t.Errorf("staged after the interruption = %v, want none", staged)
	}
	if len(rec.Broken) != 0 {
		t.Errorf("broken after the interruption = %v, want none", rec.Broken)
	}
	if len(rec.Modified) != 0 {
		t.Errorf("modified after the interruption = %v, want none", rec.Modified)
	}
}

// assertSameRun holds the finished run against the monolithic reference.
func assertSameRun(t *testing.T, wantTree map[string]string, wantLock *lock.Lock, root string, store *lock.Store) {
	t.Helper()
	if diff := cmp.Diff(wantTree, targetTree(t, root)); diff != "" {
		t.Errorf("target content (-monolithic +interrupted):\n%s", diff)
	}
	if diff := cmp.Diff(wantLock, lockState(t, store, root)); diff != "" {
		t.Errorf("lock (-monolithic +interrupted):\n%s", diff)
	}
}
