package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// The detach flow tests (tui-screens.md 6.7): the dialog over a kit with
// installed items, the immediate detach of a kit without any, and the
// deferred detach the apply completes or drops. The kits register through
// the store — the tests exercise the detach flow, not the add-kit input.

// has reports whether the phrase appears in the rendered lines regardless of
// where wrapping broke them: preview panes wrap between words, so a
// multi-word phrase can straddle two lines.
func has(lines, phrase string) bool {
	return strings.Contains(strings.Join(strings.Fields(lines), " "), phrase)
}

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
	m = driveAll(t, m, "down", "space", "a", "enter", "enter")
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
	m = driveAll(t, m, "a", "enter")
	rm := m.(*model)
	if rm.screen != screenResults {
		t.Fatalf("screen after apply = %v, want results", rm.screen)
	}
	if !strings.Contains(rm.resultText(), "removed    titem") {
		t.Errorf("results miss the removal:\n%s", rm.resultText())
	}
	if !strings.Contains(rm.resultText(), "completed the deferred detach of team from "+target) {
		t.Errorf("results miss the completed detach:\n%s", rm.resultText())
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
	m = driveAll(t, m, "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.resultText(), "dropped the deferred detach of team from "+target) ||
		!strings.Contains(rm.resultText(), "installed items remain: team/titem") {
		t.Errorf("results miss the dropped detach:\n%s", rm.resultText())
	}
	reg := registryOf(t, st)
	if names := reg.KitsFor(target); len(names) != 1 {
		t.Errorf("kits attached after the apply = %v, want team", names)
	}
	if p := reg.Pending[target]; p != nil && len(p.Deferred) != 0 {
		t.Errorf("deferred operations survived the apply: %+v", p.Deferred)
	}
}

// writeBareKit writes a minimal valid kit into dir.
func writeBareKit(t *testing.T, dir, name, hint string) string {
	t.Helper()
	return writeKitFiles(t, dir, map[string]string{
		"kit.yaml":            "name: " + name + "\ntarget_hint: " + hint + "\ndescription: A bare kit\n",
		"items/one/item.yaml": "id: one\nversion: 1.0.0\ncontributes:\n  - file: f.md\n    to: skills/one/f.md\n",
		"items/one/f.md":      "one\n",
	})
}

// tickPreviewNow delivers the debounce tick the last edit scheduled: the
// tests run headless, the interval is not waited out.
func tickPreviewNow(t *testing.T, m *model) {
	t.Helper()
	m2, _ := m.Update(previewTickMsg(m.previewSeq))
	if m2.(*model) != m {
		t.Fatal("the tick returned another model")
	}
}

func TestAddKitCompletionListsDirectories(t *testing.T) {
	parent := t.TempDir()
	writeBareKit(t, filepath.Join(parent, "claude"), "claude", parent)
	writeBareKit(t, filepath.Join(parent, "claude-work"), "claude-work", parent)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	m := resize(t, app.Initial(""), 120, 30).(*model)
	m = drive(t, m, "enter").(*model)
	if m.inputKind != inputAddKit {
		t.Fatalf("input = %v, want the add-kit input", m.inputKind)
	}

	// Typing does not read: the preview waits for the interval after the
	// last keystroke (6.2).
	m2, cmd := m.Update(key(filepath.Join(parent, "cl")))
	m = m2.(*model)
	if cmd == nil {
		t.Fatal("typing scheduled no preview tick")
	}
	if m.preview != nil {
		t.Fatal("typing read the directory at once")
	}
	if len(m.completions) != 0 {
		t.Fatalf("completions = %v, want none before the tick", m.completions)
	}

	// A stale tick drops: the edit it was scheduled for is gone.
	m.Update(previewTickMsg(0))
	if m.preview != nil || len(m.completions) != 0 {
		t.Fatal("a stale tick read anyway")
	}

	tickPreviewNow(t, m)
	// The candidates sort as plain strings, and "-" sorts before "/" —
	// "claude-work/" comes first.
	want := []string{
		filepath.ToSlash(filepath.Join(parent, "claude-work")) + "/",
		filepath.ToSlash(filepath.Join(parent, "claude")) + "/",
	}
	if diff := cmp.Diff(want, m.completions); diff != "" {
		t.Errorf("completions (-want +got):\n%s", diff)
	}
	box := strings.Join(m.completionLines(), "\n")
	for _, c := range want {
		if !strings.Contains(box, c) {
			t.Errorf("the dropdown misses %q:\n%s", c, box)
		}
	}

	// The first tab takes the first candidate, the next one cycles.
	m = drive(t, m, "tab").(*model)
	if got := m.pathInput.Value(); got != want[0] {
		t.Errorf("tab completed to %q, want %q", got, want[0])
	}
	m = drive(t, m, "tab").(*model)
	if got := m.pathInput.Value(); got != want[1] {
		t.Errorf("tab cycled to %q, want %q", got, want[1])
	}
}

// openAddKit opens the add-kit input of a fresh screen and types path;
// the debounce tick is delivered, the preview is in.
func openAddKit(t *testing.T, app *App, path string) *model {
	t.Helper()
	m := resize(t, app.Initial(""), 120, 30).(*model)
	m = drive(t, m, "end", "enter").(*model)
	if m.inputKind != inputAddKit {
		t.Fatalf("input = %v, want the add-kit input", m.inputKind)
	}
	if path != "" {
		m2, _ := m.Update(key(path))
		m = m2.(*model)
		tickPreviewNow(t, m)
	}
	return m
}

func TestAddKitPreviewShowsTheKit(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// The preview pane shows the kit's facts while the input names it
	// (6.2); the screen holds no target yet, so no hint warning shows.
	m := openAddKit(t, app, kitDir)
	lines := strings.Join(m.addPreviewLines(48), "\n")
	for _, want := range []string{"New kit", "Path", "Name", "ex", "Items", "2"} {
		if !has(lines, want) {
			t.Errorf("the preview misses %q:\n%s", want, lines)
		}
	}
	if has(lines, "The kit proposes") {
		t.Errorf("the preview warns without a current target:\n%s", lines)
	}
	if view := m.View(); !strings.Contains(view, "New kit") {
		t.Errorf("the wide screen shows no preview pane:\n%s", view)
	}

	// On a target's screen, a differing hint warns (6.2).
	other := filepath.Join(t.TempDir(), "hinted")
	elsewhere := writeKitFiles(t, other, map[string]string{
		"kit.yaml":            "name: ex\ntarget_hint: " + other + "\n",
		"items/one/item.yaml": "id: one\nversion: 1.0.0\ncontributes:\n  - file: f.md\n    to: skills/one/f.md\n",
		"items/one/f.md":      "one\n",
	})
	registerKitAt(t, st, "team", writeTeamKit(t, target), target)
	m = openAddKit(t, app, elsewhere)
	lines = strings.Join(m.addPreviewLines(48), "\n")
	if !has(lines, "The kit proposes") || !has(lines, "current target is") {
		t.Errorf("the preview misses the hint warning:\n%s", lines)
	}
}

func TestAddKitPreviewShowsEveryError(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// A path that names no kit.yaml (6.5).
	m := openAddKit(t, app, t.TempDir())
	if got := strings.Join(m.addPreviewLines(48), "\n"); !strings.Contains(got, "kit.yaml not found in") {
		t.Errorf("the preview misses the not-found error:\n%s", got)
	}

	// A kit.yaml the parser rejects carries its position (6.5).
	broken := writeKitFiles(t, t.TempDir(), map[string]string{
		"kit.yaml":            "name: broken\nshared: oops\n",
		"items/one/item.yaml": "id: one\nversion: 1.0.0\ncontributes:\n  - file: f.md\n    to: skills/one/f.md\n",
		"items/one/f.md":      "one\n",
	})
	m = openAddKit(t, app, broken)
	got := strings.Join(m.addPreviewLines(48), "\n")
	if !strings.Contains(got, "The kit cannot be added.") || !strings.Contains(got, "kit.yaml:2") {
		t.Errorf("the preview misses the parse error with its position:\n%s", got)
	}

	// The same-name rule of adding: the registered kit's path names the
	// rejection (6.5).
	kitDir := writeFixtureKit(t, target)
	registerKitAt(t, st, "ex", kitDir, target)
	other := writeBareKit(t, filepath.Join(t.TempDir(), "other"), "ex", target)
	m = openAddKit(t, app, other)
	got = strings.Join(m.addPreviewLines(48), "\n")
	if !strings.Contains(got, "A kit named ex is already registered at") {
		t.Errorf("the preview misses the same-name error:\n%s", got)
	}

	// A kit already attached to this target (6.5).
	m = openAddKit(t, app, kitDir)
	got = strings.Join(m.addPreviewLines(48), "\n")
	if !strings.Contains(got, "The kit is already attached to this target") {
		t.Errorf("the preview misses the already-attached error:\n%s", got)
	}

	// Item-level problems show on their rows and do not stop the attach
	// (6.5): enter registers the kit.
	mixed := writeKitFiles(t, t.TempDir(), map[string]string{
		"kit.yaml":             "name: mixed\ntarget_hint: " + target + "\n",
		"items/good/item.yaml": "id: good\nversion: 1.0.0\ncontributes:\n  - file: f.md\n    to: skills/good/f.md\n",
		"items/good/f.md":      "good\n",
		"items/bad/item.yaml":  "id: bad\nversion: nope\n",
	})
	m = openAddKit(t, app, mixed)
	got = strings.Join(m.addPreviewLines(48), "\n")
	if !strings.Contains(got, "items/bad/item.yaml") || !strings.Contains(got, "semantic version") {
		t.Errorf("the preview misses the item-level problem:\n%s", got)
	}
	m = drive(t, m, "enter").(*model)
	reg, err := st.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if k := reg.Kits["mixed"]; k.Dir != mixed {
		t.Errorf("the kit with item problems did not attach: %+v", reg.Kits)
	}
}

func TestAddKitHintDialogThreeOutcomes(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	registered := filepath.Join(t.TempDir(), "registered")
	kitDir := writeKitFiles(t, t.TempDir(), map[string]string{
		"kit.yaml":            "name: ex\ntarget_hint: " + registered + "\n",
		"items/one/item.yaml": "id: one\nversion: 1.0.0\ncontributes:\n  - file: f.md\n    to: skills/one/f.md\n",
		"items/one/f.md":      "one\n",
	})

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// The hint is registered already; the screen opens the other target,
	// so the hint differs from the current one. The first option reads
	// "Attach to X and switch to it" (6.3).
	registerKitAt(t, st, "other", writeBareKit(t, registered, "other", target), registered)
	registerKitAt(t, st, "team", writeTeamKit(t, target), target)
	m := openAddKit(t, app, kitDir)
	m = drive(t, m, "enter").(*model)
	if m.dialog == nil || m.dialog.kind != dialogAttach {
		t.Fatalf("dialog = %+v, want the attach dialog", m.dialog)
	}
	if got := m.dialog.options[0]; got != "Attach to "+registered+" and switch to it" {
		t.Errorf("first option = %q, want the switch wording", got)
	}
	// esc cancels: the input stays editable.
	m = drive(t, m, "esc").(*model)
	if m.dialog != nil || m.inputKind != inputAddKit {
		t.Fatalf("esc: dialog %v, input %v; want the input back", m.dialog, m.inputKind)
	}

	// Outcome three: cancel changes nothing.
	m = drive(t, m, "enter", "down", "down", "enter").(*model)
	if m.dialog != nil {
		t.Fatal("cancel left the dialog open")
	}
	if _, ok := registryOf(t, st).Kits["ex"]; ok {
		t.Error("cancel registered the kit anyway")
	}
	if m.inputKind != inputAddKit {
		t.Errorf("input = %v, want the input still editable after cancel", m.inputKind)
	}

	// Outcome two: attach to the current target anyway (6.3).
	m = drive(t, m, "enter", "down", "enter").(*model)
	if m.dialog != nil {
		t.Fatalf("the dialog stayed open: %+v", m.dialog)
	}
	if m.target != target {
		t.Fatalf("target = %q, want the current target kept", m.target)
	}
	if names := registryOf(t, st).KitsFor(target); !containsString(names, "ex") {
		t.Errorf("kits at the current target = %v, want ex attached anyway", names)
	}

	// Outcome one, a fresh kit at an unregistered hint: register the hint
	// as a target and attach there. The first option reads the register
	// wording when the hint is new (6.3).
	freshHint := filepath.Join(t.TempDir(), "new-target")
	fresh := writeKitFiles(t, t.TempDir(), map[string]string{
		"kit.yaml":            "name: two\ntarget_hint: " + freshHint + "\n",
		"items/one/item.yaml": "id: one\nversion: 1.0.0\ncontributes:\n  - file: f.md\n    to: skills/one/f.md\n",
		"items/one/f.md":      "one\n",
	})
	m = openAddKit(t, app, fresh)
	m = drive(t, m, "enter").(*model)
	if m.dialog == nil {
		t.Fatal("a differing hint opened no dialog")
	}
	if got := m.dialog.options[0]; got != "Register "+freshHint+" as a target and attach there" {
		t.Errorf("first option = %q, want the register wording", got)
	}
	m = drive(t, m, "enter").(*model)
	if m.target != freshHint {
		t.Fatalf("target = %q, want the registered hint %q", m.target, freshHint)
	}
	reg := registryOf(t, st)
	if _, ok := reg.Targets[freshHint]; !ok {
		t.Error("the hint was not registered as a target")
	}
	if names := reg.KitsFor(freshHint); len(names) != 1 || names[0] != "two" {
		t.Errorf("kits at the hint = %v, want two attached", names)
	}
}

func containsString(list []string, s string) bool {
	for _, el := range list {
		if el == s {
			return true
		}
	}
	return false
}

func TestAddKitFirstTarget(t *testing.T) {
	hint := filepath.Join(t.TempDir(), "hint")
	kitDir := writeKitFiles(t, t.TempDir(), map[string]string{
		"kit.yaml":            "name: ex\ntarget_hint: " + hint + "\n",
		"items/one/item.yaml": "id: one\nversion: 1.0.0\ncontributes:\n  - file: f.md\n    to: skills/one/f.md\n",
		"items/one/f.md":      "one\n",
	})

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// With no target registered, the preview ends with the target input
	// prefilled with the hint, and the existence line names the missing
	// directory (6.4).
	m := openAddKit(t, app, kitDir)
	lines := strings.Join(m.addPreviewLines(60), "\n")
	if !has(lines, "No target is registered. Target directory:") || !has(lines, hint) {
		t.Errorf("the preview misses the target input:\n%s", lines)
	}
	if !has(lines, "The directory does not exist") || !has(lines, "created on the first") {
		t.Errorf("the preview misses the existence line:\n%s", lines)
	}

	// esc steps back to the path; the input stays editable.
	m = drive(t, m, "enter").(*model)
	if !m.targetMode {
		t.Fatal("enter opened no target input")
	}
	m = drive(t, m, "esc").(*model)
	if m.targetMode || m.inputKind != inputAddKit {
		t.Fatalf("esc: targetMode %v, input %v; want the path step back", m.targetMode, m.inputKind)
	}

	// The directory exists: the line says so, and enter registers the
	// target and attaches.
	if err := os.MkdirAll(hint, 0o755); err != nil {
		t.Fatal(err)
	}
	tickPreviewNow(t, m)
	lines = strings.Join(m.addPreviewLines(60), "\n")
	if !has(lines, "The directory exists.") {
		t.Errorf("the preview misses the existing-directory line:\n%s", lines)
	}
	m = drive(t, m, "enter", "enter").(*model)
	if m.target != hint {
		t.Fatalf("target = %q, want the hint %q", m.target, hint)
	}
	reg := registryOf(t, st)
	if _, ok := reg.Targets[hint]; !ok {
		t.Error("the hint was not registered as a target")
	}
	if names := reg.KitsFor(hint); len(names) != 1 || names[0] != "ex" {
		t.Errorf("kits = %v, want ex attached", names)
	}
	if reg.LastTarget != hint {
		t.Errorf("last target = %q, want the new target", reg.LastTarget)
	}
}

func TestAddKitFirstTargetOnNarrowOpensTheView(t *testing.T) {
	hint := filepath.Join(t.TempDir(), "hint")
	if err := os.MkdirAll(hint, 0o755); err != nil {
		t.Fatal(err)
	}
	kitDir := writeKitFiles(t, t.TempDir(), map[string]string{
		"kit.yaml":            "name: ex\ntarget_hint: " + hint + "\n",
		"items/one/item.yaml": "id: one\nversion: 1.0.0\ncontributes:\n  - file: f.md\n    to: skills/one/f.md\n",
		"items/one/f.md":      "one\n",
	})

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// On a medium terminal the preview opens as the details view after
	// enter (6.2), with the target input inside (6.4).
	m := resize(t, app.Initial(kitDir), 100, 30).(*model)
	m = drive(t, m, "enter").(*model)
	if !m.view || !m.viewPreview {
		t.Fatalf("view %v preview %v; want the preview view", m.view, m.viewPreview)
	}
	view := m.View()
	if !strings.Contains(view, "No target is registered. Target directory:") || !strings.Contains(view, "The directory exists.") {
		t.Errorf("the preview view misses the target input:\n%s", view)
	}
	m = drive(t, m, "enter").(*model)
	if m.target != hint {
		t.Fatalf("target = %q, want the hint %q", m.target, hint)
	}
	if m.view || m.viewPreview {
		t.Error("the preview view stayed open after the attach")
	}
}

func TestEditKitRelocation(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	oldDir := writeBareKit(t, filepath.Join(t.TempDir(), "old"), "ex", target)
	teamDir := writeTeamKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", oldDir, target)
	registerKitAt(t, st, "team", teamDir, target)
	locks := lock.NewStore(st.Dir)
	app := NewApp(st, locks)

	// The kit directory vanishes: e on the unreadable kit's row opens the
	// relocation input, and the pane holds the read error and the frozen
	// note (6.6).
	if err := os.RemoveAll(oldDir); err != nil {
		t.Fatal(err)
	}
	lk, err := locks.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	lk.SetItem("ex/one", &lock.Item{Version: "1.0.0"})
	if err := locks.Save(lk); err != nil {
		t.Fatal(err)
	}

	m := resize(t, app.Initial(""), 120, 30).(*model)
	if r := m.rows[m.cursor]; r.Kind != KindKit || r.Kit.Name != "ex" || !r.Kit.Unreadable {
		t.Fatalf("row under the cursor = %+v, want the unreadable ex", r)
	}
	m = drive(t, m, "e").(*model)
	if m.inputKind != inputEditKit || m.editName != "ex" {
		t.Fatalf("input %v name %q; want the relocation input of ex", m.inputKind, m.editName)
	}
	lines := strings.Join(m.editPreviewLines(60), "\n")
	if !has(lines, "The kit directory cannot be read") || !has(lines, "no such directory") {
		t.Errorf("the pane misses the read error:\n%s", lines)
	}
	if !has(lines, "Its 1 installed item is frozen") {
		t.Errorf("the pane misses the frozen note:\n%s", lines)
	}

	// Pointing at a kit of another name is a rejection; the input stays
	// editable and nothing moves (6.6).
	other := writeBareKit(t, filepath.Join(t.TempDir(), "moved-away"), "other", target)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = m2.(*model)
	m2, _ = m.Update(key(other))
	m = m2.(*model)
	tickPreviewNow(t, m)
	lines = strings.Join(m.editPreviewLines(60), "\n")
	if !has(lines, "contains the kit other") || !has(lines, "not ex") {
		t.Errorf("the pane misses the rejection:\n%s", lines)
	}
	m = drive(t, m, "enter").(*model)
	if k := registryOf(t, st).Kits["ex"]; k.Dir == other {
		t.Error("the rejection moved the kit anyway")
	}
	if m.inputKind != inputEditKit {
		t.Fatalf("input = %v, want the input still editable", m.inputKind)
	}

	// A directory holding a kit of the same name applies: enter points the
	// kit there and the notice confirms.
	newDir := writeBareKit(t, filepath.Join(t.TempDir(), "new"), "ex", target)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = m2.(*model)
	m2, _ = m.Update(key(newDir))
	m = m2.(*model)
	tickPreviewNow(t, m)
	lines = strings.Join(m.editPreviewLines(60), "\n")
	if !has(lines, "Path") || !has(lines, "ex") {
		t.Errorf("the pane misses the new kit's preview:\n%s", lines)
	}
	m = drive(t, m, "enter").(*model)
	if k := registryOf(t, st).Kits["ex"]; k.Dir != newDir {
		t.Errorf("registered dir = %q, want the new directory %q", k.Dir, newDir)
	}
	if got := m.notice.Text(); got != "Pointed ex to "+newDir+"." {
		t.Errorf("notice = %q, want the move confirmation", got)
	}
	if m.inputKind != inputNone {
		t.Errorf("input = %v, want the input closed", m.inputKind)
	}
	// The tree reads the kit again: its item row is back.
	if r := itemRow(m, "ex/one"); r.Key == "" {
		t.Errorf("the kit's items are missing from the tree: %v", itemKeys(m))
	}
}

func TestAddKitErrorKeepsTheInputEditable(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// On a narrow terminal the preview opens as the details view after
	// enter; an error shows there, esc returns to the input, and the next
	// path goes through (6.2, 6.5).
	empty := t.TempDir()
	kitDir := writeBareKit(t, filepath.Join(t.TempDir(), "kit"), "ex", target)
	m := resize(t, app.Initial(""), 100, 30).(*model)
	m = drive(t, m, "end", "enter", empty, "enter").(*model)
	if !m.view || !m.viewPreview {
		t.Fatalf("view %v preview %v; want the error in the preview view", m.view, m.viewPreview)
	}
	if view := m.View(); !strings.Contains(view, "kit.yaml not found in") {
		t.Errorf("the view misses the not-found error:\n%s", view)
	}
	m = drive(t, m, "esc").(*model)
	if m.view || m.inputKind != inputAddKit {
		t.Fatalf("view %v, input %v; want the input back, editable", m.view, m.inputKind)
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = m2.(*model)
	m = drive(t, m, kitDir).(*model)
	tickPreviewNow(t, m)
	m = drive(t, m, "enter", "enter").(*model)
	if m.target != target {
		t.Fatalf("target = %q, want the hint %q", m.target, target)
	}
}
