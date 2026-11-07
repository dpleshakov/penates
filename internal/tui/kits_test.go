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

// The detach flow tests (tui-screens.md 6.7): the dialog over a kit with
// installed items, the immediate detach of a kit without any, and the
// deferred detach the apply completes or drops. The kits register through
// the store — the tests exercise the detach flow, not the add-kit input.

// registerKitAt registers the kit and its target the way adding a kit
// does, through the store.
func registerKitAt(t *testing.T, st *state.Store, name, dir, target string) {
	t.Helper()
	err := st.Update(func(reg *state.Registry) error {
		reg.Kits[name] = state.Kit{Dir: dir}
		if _, ok := reg.Targets[target]; !ok {
			reg.Targets[target] = state.Target{}
		}
		reg.Attachments = append(reg.Attachments, state.Attachment{Kit: name, Target: target})
		reg.LastTarget = target
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// installFirst installs the first item row of the tree: select it, confirm
// the summary, and return to the list.
func installFirst(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	m = drive(t, m, "down", "space", "a", "enter")
	m = drive(t, m, "enter")
	return m
}

// registryOf loads the registry the store holds now.
func registryOf(t *testing.T, st *state.Store) *state.Registry {
	t.Helper()
	reg, err := st.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestDetachWithoutInstalledItems(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeTeamKit(t, target)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, target)
	app := NewApp(st, lock.NewStore(dataDir))

	m := resize(t, app.Initial(""), 120, 30)
	// d on the kit row detaches at once: no installed items to ask about.
	m = drive(t, m, "d")
	lm := m.(*model)
	if lm.dialog != nil {
		t.Fatalf("a kit without installed items opened a dialog: %+v", lm.dialog)
	}
	if got := lm.notice.Text(); got != "Detached team." {
		t.Errorf("notice = %q, want the detach confirmation", got)
	}
	if names := registryOf(t, st).KitsFor(target); len(names) != 0 {
		t.Errorf("kits attached after the detach = %v, want none", names)
	}
	if keys := itemKeys(lm); len(keys) != 0 {
		t.Errorf("item rows after the detach = %v, want none", keys)
	}
}

func TestDetachDialogAsksAboutInstalledItems(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeTeamKit(t, target)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, target)
	app := NewApp(st, lock.NewStore(dataDir))

	m := installFirst(t, resize(t, app.Initial(""), 120, 30))

	// d on the kit row opens the dialog: it names the installed items and
	// defaults to Cancel.
	m = drive(t, m, "d")
	lm := m.(*model)
	if lm.dialog == nil || lm.dialog.kind != dialogDetach {
		t.Fatalf("dialog = %+v, want the detach dialog", lm.dialog)
	}
	lines := lm.dialog.lines(120, lm.set)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"╭ Detach team ",
		"1 item of this kit is installed in " + target + ":",
		"titem 1.0.0",
		"Remove its items from the target",
		"Keep its items installed as orphans",
		"▌Cancel",
		"enter choose · esc cancel",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("dialog misses %q:\n%s", want, joined)
		}
	}

	// esc cancels: nothing changed — the kit stays attached, and the only
	// pending action is the selection the install left.
	m = drive(t, m, "esc")
	lm = m.(*model)
	if lm.dialog != nil {
		t.Fatal("the dialog stayed open after esc")
	}
	if names := registryOf(t, st).KitsFor(target); len(names) != 1 {
		t.Fatalf("kits attached after cancel = %v, want team", names)
	}
	if r := itemRow(lm, "team/titem"); r.Pending != nil && *r.Pending == PendingRemove {
		t.Errorf("cancel left a removal mark: %+v", r)
	}

	// The orphans path: the kit detaches at once, the item stays installed
	// as an orphan, and the notice offers undo.
	m = drive(t, m, "d", "up", "enter")
	lm = m.(*model)
	if got := lm.notice.Text(); got != "Detached team." {
		t.Errorf("notice = %q, want the detach confirmation", got)
	}
	if names := registryOf(t, st).KitsFor(target); len(names) != 0 {
		t.Errorf("kits attached after keep-orphans = %v, want none", names)
	}
	if r := itemRow(lm, "team/titem"); !r.Installed || r.Status != StatusOrphan {
		t.Errorf("kept item row = %+v, want installed orphan", r)
	}
	if view := lm.View(); !strings.Contains(view, "orphans") {
		t.Errorf("the tree shows no orphans group:\n%s", view)
	}
	installed, err := os.ReadFile(filepath.Join(target, "skills", "titem", "t.md"))
	if err != nil || string(installed) != "team item\n" {
		t.Errorf("kept item file = %q, %v; want it untouched", installed, err)
	}
}

func TestDetachRemoveThenDetachWaitsForTheApply(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeTeamKit(t, target)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, target)
	app := NewApp(st, lock.NewStore(dataDir))

	m := installFirst(t, resize(t, app.Initial(""), 120, 30))

	// The removals path: the item takes the removal mark, the detach is
	// queued behind the apply, and the row shows "detaching".
	m = drive(t, m, "d", "up", "up", "enter")
	lm := m.(*model)
	if r := itemRow(lm, "team/titem"); r.Pending == nil || *r.Pending != PendingRemove {
		t.Fatalf("item row after the choice = %+v, want the removal mark", r)
	}
	pending := registryOf(t, st).Pending[target]
	if pending == nil || len(pending.Deferred) != 1 || pending.Deferred[0].Kit != "team" {
		t.Fatalf("deferred operations = %+v, want the queued detach of team", pending)
	}
	if view := lm.View(); !strings.Contains(view, "detaching") {
		t.Errorf("the kit row does not show detaching:\n%s", view)
	}

	// The apply removes the items and completes the deferred detach; the
	// report names it.
	m = drive(t, m, "a", "enter")
	rm := m.(*model)
	if rm.screen != screenResults {
		t.Fatalf("screen after apply = %v, want results", rm.screen)
	}
	if !strings.Contains(rm.lastResults, "removed  team/titem") {
		t.Errorf("results miss the removal:\n%s", rm.lastResults)
	}
	if !strings.Contains(rm.lastResults, "completed the deferred detach of team from "+target) {
		t.Errorf("results miss the completed detach:\n%s", rm.lastResults)
	}

	reg := registryOf(t, st)
	if names := reg.KitsFor(target); len(names) != 0 {
		t.Errorf("kits attached after the apply = %v, want none", names)
	}
	if p := reg.Pending[target]; p != nil {
		t.Errorf("pending actions after the apply = %+v, want none", p)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "titem", "t.md")); !os.IsNotExist(err) {
		t.Errorf("the removed item's file survived: %v", err)
	}

	// Back to the list: the kit is gone from the tree.
	m = drive(t, m, "enter")
	if keys := itemKeys(m.(*model)); len(keys) != 0 {
		t.Errorf("item rows after the apply = %v, want none", keys)
	}
}

func TestDetachDeferredDroppedByTheApply(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeTeamKit(t, target)

	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "team", kitDir, target)
	app := NewApp(st, lock.NewStore(dataDir))

	m := installFirst(t, resize(t, app.Initial(""), 120, 30))

	// A deferred detach whose removals the apply does not carry is dropped
	// with its reason: the item stays installed, so the detach cannot.
	if err := st.DeferDetachKit("team", target); err != nil {
		t.Fatal(err)
	}
	m = drive(t, m, "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "dropped the deferred detach of team from "+target) ||
		!strings.Contains(rm.lastResults, "installed items remain: team/titem") {
		t.Errorf("results miss the dropped detach:\n%s", rm.lastResults)
	}
	reg := registryOf(t, st)
	if names := reg.KitsFor(target); len(names) != 1 {
		t.Errorf("kits attached after the apply = %v, want team", names)
	}
	if p := reg.Pending[target]; p != nil && len(p.Deferred) != 0 {
		t.Errorf("deferred operations survived the apply: %+v", p.Deferred)
	}
}
