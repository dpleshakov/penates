package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// The target list tests (tui-screens.md 8): the list with its counts and
// the current marker, the fuzzy search, adding and renaming, switching
// that keeps the per-target pending actions and offers the restoration of
// 11.1, and the removal dialog of 8.3 with both paths.

// ctrlKey feeds one ctrl combo to the model: the key() helper of the
// screen tests maps no ctrl keys.
func ctrlKey(t *testing.T, m tea.Model, k tea.KeyType) *model {
	t.Helper()
	m2, _ := m.Update(tea.KeyMsg{Type: k})
	return m2.(*model)
}

// targetIndex finds the row of the target in the open list.
func targetIndex(t *testing.T, m *model, path string) int {
	t.Helper()
	for i, e := range m.list.entries {
		if e.path == path {
			return i
		}
	}
	t.Fatalf("the list holds no row for %s", path)
	return 0
}

func TestTargetListShowsCountsAndMarker(t *testing.T) {
	dataDir := t.TempDir()
	targetA := filepath.Join(t.TempDir(), "alpha")
	targetB := filepath.Join(t.TempDir(), "beta")
	kitDir := writeTeamKit(t, targetA)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, targetA)
	if err := st.RegisterTarget(targetB); err != nil {
		t.Fatal(err)
	}
	// Two installed items in the lock of the first target.
	locks := lock.NewStore(dataDir)
	lk, err := locks.Load(targetA)
	if err != nil {
		t.Fatal(err)
	}
	lk.SetItem("team/titem", &lock.Item{Version: "1.0.0"})
	lk.SetItem("gone/old", &lock.Item{Version: "0.9.0"})
	if err := locks.Save(lk); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, locks)

	// The screen runs wide enough for the box to hold the long test
	// paths whole.
	m := resize(t, app.Initial(""), 160, 30)
	lm := drive(t, m, "t").(*model)
	if lm.list == nil {
		t.Fatal("t opened no target list")
	}
	ia := targetIndex(t, lm, targetA)
	ib := targetIndex(t, lm, targetB)
	if e := lm.list.entries[ia]; e.kits != 1 || e.items != 2 || !e.current {
		t.Errorf("row of the current target = %+v, want 1 kit, 2 items, current", e)
	}
	if e := lm.list.entries[ib]; e.kits != 0 || e.items != 0 || e.current {
		t.Errorf("row of the other target = %+v, want 0 kits, 0 items, not current", e)
	}
	view := lm.View()
	for _, want := range []string{
		"current · 1 kit · 2 items",
		"0 kits · 0 items",
		"+ add target",
		"enter switch · ctrl+r rename · ctrl+d remove · esc close",
		// The header arrow points up while the list shows (8.1).
		"▴",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the list misses %q:\n%s", want, view)
		}
	}

	// esc closes: the arrow points down again and the tree is back.
	lm = drive(t, lm, "esc").(*model)
	if lm.list != nil {
		t.Fatal("esc did not close the list")
	}
	if view := lm.View(); !strings.Contains(view, "▾") || !strings.Contains(view, "team") {
		t.Errorf("esc did not return to the tree:\n%s", view)
	}
}

func TestTargetListSearch(t *testing.T) {
	targetA := filepath.Join(t.TempDir(), "alpha")
	targetB := filepath.Join(t.TempDir(), "beta")
	kitDir := writeTeamKit(t, targetA)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, targetA)
	if err := st.RegisterTarget(targetB); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	lm := drive(t, resize(t, app.Initial(""), 120, 30), "t").(*model)
	// The second target carries the display name the search matches by.
	lm.list.cursor = targetIndex(t, lm, targetB)
	lm = ctrlKey(t, lm, tea.KeyCtrlR)
	lm = drive(t, lm, "wsl", "enter").(*model)
	if n := registryOf(t, st).Targets[targetB].Name; n != "wsl" {
		t.Fatalf("rename = %q, want wsl", n)
	}

	// Typing filters: the pattern matches the one target by its display
	// name, the other by its path; the path match outranks.
	lm = drive(t, lm, "wsl").(*model)
	if len(lm.list.entries) != 1 || lm.list.entries[0].path != targetB {
		t.Fatalf("entries after 'wsl' = %+v, want only the named target", lm.list.entries)
	}

	m2, _ := lm.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	lm = drive(t, m2, "alpha").(*model)
	if len(lm.list.entries) == 0 || lm.list.entries[0].path != targetA {
		t.Fatalf("entries after 'alpha' = %+v, want the path match first", lm.list.entries)
	}

	// Nothing matches: the add-target row stays under the cursor.
	lm = drive(t, lm, "zzz").(*model)
	if len(lm.list.entries) != 0 || lm.list.cursor != 0 {
		t.Fatalf("entries %d cursor %d, want none on the add-target row", len(lm.list.entries), lm.list.cursor)
	}

	// esc closes the whole list, not only the search.
	lm = drive(t, lm, "esc").(*model)
	if lm.list != nil {
		t.Fatal("esc closed the search but kept the list")
	}
}

func TestTargetListAddTarget(t *testing.T) {
	parent := t.TempDir()
	inside := filepath.Join(parent, "newdir")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// From the first run: no targets, the cursor on the add-target row.
	lm := drive(t, resize(t, app.Initial(""), 120, 30), "t").(*model)
	if len(lm.list.entries) != 0 {
		t.Fatalf("entries = %+v, want none", lm.list.entries)
	}
	lm = drive(t, lm, "enter").(*model)
	if !lm.list.adding {
		t.Fatal("enter opened no add-target input")
	}
	if view := lm.View(); !strings.Contains(view, "tab complete · enter add target · esc cancel") {
		t.Errorf("the input keeps no key line:\n%s", view)
	}

	// esc cancels the input and keeps the list.
	lm = drive(t, lm, "esc").(*model)
	if lm.list.adding || lm.list == nil {
		t.Fatalf("adding %v, list %v; want the input cancelled, the list open", lm.list.adding, lm.list != nil)
	}
	lm = drive(t, lm, "enter").(*model)

	// Typing does not read: the existence line waits for the tick (6.2).
	lm2, _ := lm.Update(key(filepath.ToSlash(parent) + "/ne"))
	lm = lm2.(*model)
	if lm.list.existsKnown {
		t.Fatal("typing read the directory at once")
	}
	tickPreviewNow(t, lm)
	if view := lm.View(); !strings.Contains(view, "The directory does not exist. It is created on the first apply.") {
		t.Errorf("no missing-directory line:\n%s", view)
	}

	// tab completes to the candidate; the existence line follows.
	lm = drive(t, lm, "tab").(*model)
	if got := lm.list.pathInput.Value(); got != filepath.ToSlash(inside)+"/" {
		t.Fatalf("tab completed to %q, want %q", got, filepath.ToSlash(inside)+"/")
	}
	tickPreviewNow(t, lm)
	if view := lm.View(); !strings.Contains(view, "The directory exists.") {
		t.Errorf("no existing-directory line:\n%s", view)
	}

	// enter registers the target: the list shows it, the notice confirms.
	lm = drive(t, lm, "enter").(*model)
	if _, ok := registryOf(t, st).Targets[inside]; !ok {
		t.Errorf("targets = %+v, want %s registered", registryOf(t, st).Targets, inside)
	}
	if len(lm.list.entries) != 1 || lm.list.entries[0].path != inside {
		t.Fatalf("entries = %+v, want the new target", lm.list.entries)
	}
	if got := lm.notice.Text(); got != "Registered target "+inside+"." {
		t.Errorf("notice = %q, want the registration", got)
	}
}

func TestTargetListRenameChangesHeader(t *testing.T) {
	dataDir := t.TempDir()
	targetA := filepath.Join(t.TempDir(), "alpha")
	targetB := filepath.Join(t.TempDir(), "beta")
	kitDir := writeTeamKit(t, targetA)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, targetA)
	if err := st.RegisterTarget(targetB); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(dataDir))

	lm := drive(t, resize(t, app.Initial(""), 120, 30), "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetA)

	// ctrl+r opens the rename input over the row, prefilled with the
	// current name.
	lm = ctrlKey(t, lm, tea.KeyCtrlR)
	if lm.list.renaming != targetA || lm.list.nameInput.Value() != "" {
		t.Fatalf("renaming %q value %q; want the empty input", lm.list.renaming, lm.list.nameInput.Value())
	}
	if view := lm.View(); !strings.Contains(view, "enter rename · esc cancel") {
		t.Errorf("the input keeps no key line:\n%s", view)
	}

	// enter saves: the registry carries the name, the header of the
	// current target shows it, and the row shows the name with the path.
	lm = drive(t, lm, "main", "enter").(*model)
	if n := registryOf(t, st).Targets[targetA].Name; n != "main" {
		t.Fatalf("name = %q, want main", n)
	}
	header := strings.Split(lm.View(), "\n")[0]
	if !strings.Contains(header, "main") {
		t.Errorf("header = %q, want the display name", header)
	}
	if view := lm.View(); !strings.Contains(view, "main  "+targetA) {
		t.Errorf("the row does not show the name with the path:\n%s", view)
	}

	// esc cancels a rename: the name stays.
	lm = drive(t, lm, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetA)
	lm = ctrlKey(t, lm, tea.KeyCtrlR)
	lm = drive(t, lm, "other", "esc").(*model)
	if n := registryOf(t, st).Targets[targetA].Name; n != "main" {
		t.Errorf("esc saved %q anyway", n)
	}
}

func TestTargetSwitchKeepsPendingAndOffersRestore(t *testing.T) {
	dataDir := t.TempDir()
	targetA := filepath.Join(t.TempDir(), "alpha")
	targetB := filepath.Join(t.TempDir(), "beta")
	kitA := writeFixtureKit(t, targetA)
	kitB := writeTeamKit(t, targetB)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitA, targetA)
	registerKitAt(t, st, "team", kitB, targetB)
	app := NewApp(st, lock.NewStore(dataDir))

	// B opens; its item takes a pending action, which saves with B.
	lm := drive(t, resize(t, app.Initial(""), 120, 30), "down", "space").(*model)
	if lm.pendingCount() != 1 {
		t.Fatalf("pending = %d, want the selection of B", lm.pendingCount())
	}

	// Switching to A asks nothing and discards nothing (8.2).
	lm = drive(t, lm, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetA)
	lm = drive(t, lm, "enter").(*model)
	if lm.target != targetA || lm.dialog != nil {
		t.Fatalf("target %q dialog %v; want A without a question", lm.target, lm.dialog)
	}
	p := registryOf(t, st).Pending[targetB]
	if p == nil || p.Items["team/titem"] == nil || !p.Items["team/titem"].Select {
		t.Fatalf("pending of B = %+v, want the selection kept", p)
	}

	// A's own selection saves with A the same way.
	lm = drive(t, lm, "down", "space").(*model)
	if lm.pendingCount() != 1 {
		t.Fatalf("pending = %d, want the selection of A", lm.pendingCount())
	}

	// Switching back to B offers the restoration of B's pending actions
	// (11.1), with esc named as starting with none.
	lm = drive(t, lm, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetB)
	lm = drive(t, lm, "enter").(*model)
	if lm.dialog == nil || lm.dialog.kind != dialogRestore {
		t.Fatalf("dialog = %+v, want the restore dialog", lm.dialog)
	}
	joined := strings.Join(lm.dialog.lines(120, lm.set), "\n")
	for _, want := range []string{
		"Previous selection",
		"The last session ended with 1 pending action in",
		"install titem",
		"▌Restore",
		"Start with no pending actions",
		"enter choose · esc start with none",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the dialog misses %q:\n%s", want, joined)
		}
	}

	// Restore keeps them: the tree shows the mark again.
	lm = drive(t, lm, "enter").(*model)
	if lm.dialog != nil || lm.pendingCount() != 1 {
		t.Fatalf("dialog %v pending %d; want the action restored", lm.dialog, lm.pendingCount())
	}
	if p := registryOf(t, st).Pending[targetA]; p == nil || !p.Items["ex/good"].Select {
		t.Errorf("pending of A = %+v, want the selection kept", p)
	}

	// Switching back to A offers again; esc starts with no pending
	// actions: A's actions go, B's stay.
	lm = drive(t, lm, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetA)
	lm = drive(t, lm, "enter", "esc").(*model)
	if p := registryOf(t, st).Pending[targetA]; p != nil {
		t.Errorf("pending of A = %+v, want none after esc", p)
	}
	if lm.pendingCount() != 0 {
		t.Errorf("pending on the screen = %d, want none", lm.pendingCount())
	}
	if p := registryOf(t, st).Pending[targetB]; p == nil || !p.Items["team/titem"].Select {
		t.Errorf("pending of B = %+v, want the selection kept", p)
	}
}

func TestTargetRestoreDialogWording(t *testing.T) {
	dataDir := t.TempDir()
	targetA := filepath.Join(t.TempDir(), "alpha")
	targetB := filepath.Join(t.TempDir(), "beta")
	kitA := writeFixtureKit(t, targetA)
	kitB := writeTeamKit(t, targetB)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitA, targetA)
	registerKitAt(t, st, "team", kitB, targetA)
	gamma := filepath.Join(t.TempDir(), "gamma")
	if err := st.RegisterTarget(gamma); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(dataDir))

	// Install the first item, then let the kit publish 1.1.0: the held
	// selection turns into the update. Selecting the other kit's item
	// adds the install: two pending actions. Both kits attach to the
	// same target, so one dialog can offer both.
	lm := driveAll(t, resize(t, app.Initial(""), 120, 30), "down", "space", "a", "enter", "enter").(*model)
	bumpKit(t, kitA, "1.1.0", "good skill v2\n")
	lm = acceptRestore(t, resize(t, app.Initial(""), 120, 30))
	if lm.pendingCount() != 1 {
		t.Fatalf("pending after the restart = %d, want the held selection", lm.pendingCount())
	}
	lm = drive(t, lm, "down", "down", "down", "down", "space").(*model)
	if lm.pendingCount() != 2 {
		t.Fatalf("pending = %d, want the update and the install", lm.pendingCount())
	}

	// Away to the bare target and back: the dialog names both actions by
	// their verbs (11.1 mockup). The body is asserted with a short label
	// so the wrap cannot split the verb list.
	lm = drive(t, lm, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, gamma)
	lm = drive(t, lm, "enter", "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetA)
	lm = drive(t, lm, "enter").(*model)
	if lm.dialog == nil {
		t.Fatal("no restore dialog")
	}
	joined := strings.Join(lm.dialog.lines(120, lm.set), "\n")
	for _, want := range []string{
		"Previous selection",
		"The last session ended with 2 pending actions in",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the dialog misses %q:\n%s", want, joined)
		}
	}
	body := strings.Join(restoreDialogBody(registryOf(t, st).Pending[targetA], lm.rows, "alpha"), " ")
	for _, want := range []string{"update good, install titem."} {
		if !strings.Contains(body, want) {
			t.Errorf("the body misses %q:\n%s", want, body)
		}
	}
}

func TestTargetRemoveThenForget(t *testing.T) {
	dataDir := t.TempDir()
	targetA := filepath.Join(t.TempDir(), "alpha")
	targetB := filepath.Join(t.TempDir(), "beta")
	kitA := writeTeamKit(t, targetA)
	kitB := writeFixtureKit(t, targetB)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitA, targetA)
	registerKitAt(t, st, "ex", kitB, targetB)
	locks := lock.NewStore(dataDir)
	app := NewApp(st, locks)

	// An installed item in the target being removed.
	m := installFirst(t, resize(t, app.Initial(""), 120, 30))
	lm := m.(*model)
	if lm.target != targetB {
		t.Fatalf("target = %q, want B", lm.target)
	}

	// ctrl+d on B's row opens the dialog of 8.3, the default on Cancel.
	lm = drive(t, lm, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetB)
	lm = ctrlKey(t, lm, tea.KeyCtrlD)
	if lm.dialog == nil || lm.dialog.kind != dialogForgetTarget {
		t.Fatalf("dialog = %+v, want the removal dialog", lm.dialog)
	}
	joined := strings.Join(lm.dialog.lines(120, lm.set), "\n")
	for _, want := range []string{
		"Remove target " + targetB,
		"1 item is installed in this target.",
		"Remove the items, then forget the target",
		"Forget the target, leave the files unmanaged",
		"▌Cancel",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the dialog misses %q:\n%s", want, joined)
		}
	}

	// Cancel changes nothing and keeps the list.
	lm = drive(t, lm, "enter").(*model)
	if _, ok := registryOf(t, st).Targets[targetB]; !ok {
		t.Fatal("cancel forgot the target anyway")
	}
	if lm.list == nil {
		t.Fatal("cancel closed the list")
	}

	// The first option: the screen switches to the target, the installed
	// item takes the removal mark, the summary opens, and the forget
	// waits behind the apply.
	lm = ctrlKey(t, lm, tea.KeyCtrlD)
	lm = drive(t, lm, "up", "up", "enter").(*model)
	if lm.screen != screenSummary || lm.target != targetB {
		t.Fatalf("screen %v target %q; want the summary of B", lm.screen, lm.target)
	}
	if r := itemRow(lm, "ex/good"); r.Pending == nil || *r.Pending != PendingRemove {
		t.Fatalf("row = %+v, want the removal mark", r)
	}
	p := registryOf(t, st).Pending[targetB]
	if p == nil || len(p.Deferred) != 1 || p.Deferred[0].Kind != state.DeferredForgetTarget {
		t.Fatalf("deferred = %+v, want the queued forget", p)
	}

	// The apply removes the item and completes the forget: the target is
	// gone with its lock file, its installed file removed.
	lm = driveAll(t, lm, "enter").(*model)
	if lm.screen != screenResults {
		t.Fatalf("screen = %v, want results", lm.screen)
	}
	if !strings.Contains(lm.resultText(), "removed    good") ||
		!strings.Contains(lm.resultText(), "completed the deferred forget of "+targetB) {
		t.Errorf("results miss the removal or the forget:\n%s", lm.resultText())
	}
	if _, ok := registryOf(t, st).Targets[targetB]; ok {
		t.Error("the target survived the forget")
	}
	if _, err := os.Stat(filepath.Join(targetB, "skills", "good", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("the installed file survived: %v", err)
	}
	if _, err := os.Stat(locks.Path(targetB)); !os.IsNotExist(err) {
		t.Errorf("the lock file survived the forget: %v", err)
	}

	// Back to the list: the remaining target opens.
	lm = drive(t, lm, "enter").(*model)
	if lm.target != targetA {
		t.Errorf("target = %q, want A", lm.target)
	}
}

func TestTargetForgetAlone(t *testing.T) {
	dataDir := t.TempDir()
	targetA := filepath.Join(t.TempDir(), "alpha")
	targetB := filepath.Join(t.TempDir(), "beta")
	kitDir := writeTeamKit(t, targetA)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, targetA)
	if err := st.RegisterTarget(targetB); err != nil {
		t.Fatal(err)
	}
	locks := lock.NewStore(dataDir)
	app := NewApp(st, locks)

	// An installed item with its file in the target being forgotten.
	m := installFirst(t, resize(t, app.Initial(""), 120, 30))

	// esc cancels the dialog without forgetting.
	lm := drive(t, m, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetA)
	lm = ctrlKey(t, lm, tea.KeyCtrlD)
	lm = drive(t, lm, "esc").(*model)
	if _, ok := registryOf(t, st).Targets[targetA]; !ok {
		t.Fatal("esc forgot the target anyway")
	}

	// The second option forgets at once: the lock is deleted, the files
	// are untouched, and the screen moves to the remaining target.
	lm = drive(t, lm, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetA)
	lm = ctrlKey(t, lm, tea.KeyCtrlD)
	lm = drive(t, lm, "up", "enter").(*model)
	if _, err := os.Stat(locks.Path(targetA)); !os.IsNotExist(err) {
		t.Errorf("the lock file survived the forget: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(targetA, "skills", "titem", "t.md"))
	if err != nil || string(data) != "team item\n" {
		t.Errorf("file after the forget = %q, %v; want it untouched", data, err)
	}
	if _, ok := registryOf(t, st).Targets[targetA]; ok {
		t.Error("the target survived the forget")
	}
	if lm.target != targetB {
		t.Errorf("target = %q, want the remaining one", lm.target)
	}
	if got := lm.notice.Text(); got != "Forgot target "+targetA+"." {
		t.Errorf("notice = %q, want the forget", got)
	}
}

func TestTargetForgettingTheOnlyTargetStartsTheFirstRun(t *testing.T) {
	dataDir := t.TempDir()
	targetA := filepath.Join(t.TempDir(), "alpha")
	kitDir := writeTeamKit(t, targetA)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, targetA)
	app := NewApp(st, lock.NewStore(dataDir))

	// The screen runs at the medium width: the first run's explanation
	// renders whole (6.1).
	m := installFirst(t, resize(t, app.Initial(""), 100, 30))
	lm := drive(t, m, "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetA)
	lm = ctrlKey(t, lm, tea.KeyCtrlD)
	lm = drive(t, lm, "up", "enter").(*model)
	if n := len(registryOf(t, st).Targets); n != 0 {
		t.Fatalf("targets = %d, want none", n)
	}
	view := lm.View()
	for _, want := range []string{"No kits are registered yet.", "no target"} {
		if !strings.Contains(view, want) {
			t.Errorf("the first run misses %q:\n%s", want, view)
		}
	}
}

func TestTargetRemoveWithNoItemsForgetsAtOnce(t *testing.T) {
	dataDir := t.TempDir()
	targetA := filepath.Join(t.TempDir(), "alpha")
	targetB := filepath.Join(t.TempDir(), "beta")
	kitDir := writeTeamKit(t, targetA)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, targetA)
	if err := st.RegisterTarget(targetB); err != nil {
		t.Fatal(err)
	}
	locks := lock.NewStore(dataDir)
	app := NewApp(st, locks)

	// The first option over a target with no installed items: the summary
	// would confirm an empty apply, so the forget goes at once.
	lm := drive(t, resize(t, app.Initial(""), 120, 30), "t").(*model)
	lm.list.cursor = targetIndex(t, lm, targetB)
	lm = ctrlKey(t, lm, tea.KeyCtrlD)
	joined := strings.Join(lm.dialog.lines(120, lm.set), "\n")
	if !strings.Contains(joined, "No items are installed in this target.") {
		t.Errorf("the dialog misses the empty body:\n%s", joined)
	}
	lm = drive(t, lm, "up", "up", "enter").(*model)
	if lm.screen != screenList {
		t.Fatalf("screen = %v, want the list, not a summary", lm.screen)
	}
	if _, ok := registryOf(t, st).Targets[targetB]; ok {
		t.Error("the target survived the forget")
	}
	if _, err := os.Stat(locks.Path(targetB)); !os.IsNotExist(err) {
		t.Errorf("a lock file was created for the forget: %v", err)
	}
	if len(lm.list.entries) != 1 || lm.list.entries[0].path != targetA {
		t.Errorf("entries = %+v, want the remaining target", lm.list.entries)
	}
}
