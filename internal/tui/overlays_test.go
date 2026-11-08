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

// The overlays and startup flows of task 10 (tui-screens.md): the command
// palette of 9, the full help of 12.5, the notices with u undo of 11.2,
// the restore of 11.1 with its invalid drops, the quit dialog of 11.3 and
// the double ctrl+c of 12.1.

// writePlainKit writes a kit of two plain items — alpha and beta —
// proposing target as its target hint.
func writePlainKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	return writeKitFiles(t, dir, map[string]string{
		"kit.yaml":              "name: ex\ntarget_hint: " + target + "\n",
		"items/alpha/item.yaml": "id: alpha\nversion: 1.0.0\ndescription: Alpha item\ncontributes:\n  - file: a.md\n    to: skills/alpha/a.md\n",
		"items/alpha/a.md":      "alpha\n",
		"items/beta/item.yaml":  "id: beta\nversion: 1.0.0\ndescription: Beta item\ncontributes:\n  - file: b.md\n    to: skills/beta/b.md\n",
		"items/beta/b.md":       "beta\n",
	})
}

// rewritePlainKit rewrites the plain kit: alpha publishes 2.0.0, beta is
// gone from the kit, gamma joins it. The installed beta turns into an
// orphan, the installed alpha into an update.
func rewritePlainKit(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(dir, "items", "beta")); err != nil {
		t.Fatal(err)
	}
	writeKitFiles(t, dir, map[string]string{
		"items/alpha/item.yaml": "id: alpha\nversion: 2.0.0\ndescription: Alpha item\ncontributes:\n  - file: a.md\n    to: skills/alpha/a.md\n",
		"items/alpha/a.md":      "alpha v2\n",
		"items/gamma/item.yaml": "id: gamma\nversion: 0.1.0\ndescription: Gamma item\ncontributes:\n  - file: g.md\n    to: skills/gamma/g.md\n",
		"items/gamma/g.md":      "gamma\n",
	})
}

// runPalette opens the palette and runs the command at the fixed index,
// walking the cursor down to it: the fixed order of 9 keeps the walk
// deterministic without typing a search.
func runPalette(t *testing.T, m tea.Model, index int) tea.Model {
	t.Helper()
	m = drive(t, m, "ctrl+p")
	lm := m.(*model)
	if lm.palette == nil {
		t.Fatal("ctrl+p opened no palette")
	}
	keys := make([]string, index)
	for i := range keys {
		keys[i] = "down"
	}
	keys = append(keys, "enter")
	return drive(t, m, keys...)
}

func TestPaletteOpensAndFilters(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	app := NewApp(st, lock.NewStore(t.TempDir()))

	lm := resize(t, app.Initial(), 120, 30).(*model)
	lm = drive(t, lm, "ctrl+p").(*model)
	if lm.palette == nil {
		t.Fatal("ctrl+p opened no palette")
	}
	view := lm.View()
	for _, want := range []string{
		"› ", "Add kit", "Detach kit", "Edit kit path", "Switch target",
		"Add target", "Rename target", "Remove target", "Rescan kits",
		"Apply", "Clear pending actions", "Help", "Quit",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the palette misses %q:\n%s", want, view)
		}
	}
	// The commands with direct keys show them (9 mockup).
	for _, want := range []string{"t", "d", "e", "R", "a", "?", "q"} {
		if !strings.Contains(view, want) {
			t.Errorf("the palette misses the key %q:\n%s", want, view)
		}
	}

	// The search narrows the set through the fuzzy matcher; esc closes.
	lm = drive(t, lm, "s", "w").(*model)
	if len(lm.palette.shown) == 0 {
		t.Fatal("sw matched nothing")
	}
	if got := lm.palette.commands[lm.palette.shown[0]].name; got != "Switch target" {
		t.Errorf("sw ranked %q first, want Switch target", got)
	}
	for _, index := range lm.palette.shown {
		if name := lm.palette.commands[index].name; !strings.Contains(strings.ToLower(name), "t") {
			t.Errorf("sw kept %q", name)
		}
	}
	lm = drive(t, lm, "esc").(*model)
	if lm.palette != nil {
		t.Fatal("esc did not close the palette")
	}
}

func TestPaletteTargetCommands(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	other := filepath.Join(t.TempDir(), "other")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	if err := st.RegisterTarget(other); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	lm := resize(t, app.Initial(), 120, 30).(*model)

	// Switch target opens the list (8.1).
	lm = runPalette(t, lm, 3).(*model)
	if lm.list == nil {
		t.Fatal("Switch target opened no list")
	}
	lm = drive(t, lm, "esc").(*model)

	// Add target opens the list with the input of 8.1.
	lm = runPalette(t, lm, 4).(*model)
	if lm.list == nil || !lm.list.adding {
		t.Fatal("Add target opened no add-target input")
	}
	// esc cancels the input, esc again closes the list (8.1).
	lm = drive(t, lm, "esc", "esc").(*model)

	// Rename target opens the list renaming the current one.
	lm = runPalette(t, lm, 5).(*model)
	if lm.list == nil || lm.list.renaming != target {
		t.Fatalf("Rename target renames %q", lm.list.renaming)
	}
	lm = drive(t, lm, "esc", "esc").(*model)

	// Remove target opens the removal dialog of 8.3 over the current one.
	lm = runPalette(t, lm, 6).(*model)
	if lm.dialog == nil || lm.dialog.kind != dialogForgetTarget {
		t.Fatal("Remove target opened no removal dialog")
	}
	lm = drive(t, lm, "esc").(*model)

	// Rescan kits reports the plain count (11.2).
	lm = runPalette(t, lm, 7).(*model)
	if got := lm.notice.Text(); got != "Rescanned 1 kit." {
		t.Errorf("rescan notice = %q", got)
	}
}

func TestPaletteKitCommandsAndPicker(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	app := NewApp(st, lock.NewStore(t.TempDir()))

	lm := resize(t, app.Initial(), 120, 30).(*model)

	// Edit kit path on the kit row opens the relocation input (6.6).
	lm = runPalette(t, lm, 2).(*model)
	if lm.inputKind != inputEditKit || lm.editName != "ex" {
		t.Fatalf("input %v, edit %q; want the relocation input of ex", lm.inputKind, lm.editName)
	}
	lm = drive(t, lm, "esc").(*model)

	// Detach kit outside a kit — the cursor sits on the add-kit row —
	// asks for one (9): the picker lists the kits of the target, and
	// enter runs the command on the chosen one.
	lm = drive(t, lm, "end", "ctrl+p", "down", "enter").(*model)
	if lm.kitPick == nil || len(lm.kitPick.kits) != 1 || lm.kitPick.kits[0] != "ex" {
		t.Fatalf("picker = %+v, want the kit ex", lm.kitPick)
	}
	lm = drive(t, lm, "enter").(*model)
	if got := lm.notice.Text(); got != "Detached ex." {
		t.Fatalf("picker detach notice = %q", got)
	}
	if n := len(registryOf(t, st).KitsFor(target)); n != 0 {
		t.Fatalf("attachments after the picker detach = %d", n)
	}

	// With no kit attached the question answers itself: the notice says
	// so, no picker opens.
	lm = drive(t, lm, "ctrl+p", "down", "enter").(*model)
	if lm.kitPick != nil {
		t.Fatal("a target without kits opened a picker")
	}
	if got := lm.notice.Text(); got != "No kits are attached to this target." {
		t.Fatalf("empty picker notice = %q", got)
	}

	// Add kit through the palette attaches again (6.2).
	lm = drive(t, lm, "ctrl+p", "enter").(*model)
	if lm.inputKind != inputAddKit {
		t.Fatal("Add kit opened no input")
	}
	lm.pathInput.SetValue(kitDir)
	lm = drive(t, lm, "enter").(*model)
	if got := lm.notice.Text(); got != "Attached ex at "+displayPath(target)+"." {
		t.Fatalf("attach notice = %q", got)
	}
	if n := len(registryOf(t, st).KitsFor(target)); n != 1 {
		t.Fatalf("attachments after the re-attach = %d", n)
	}
}

func TestPaletteApplyClearHelpQuit(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	app := NewApp(st, lock.NewStore(t.TempDir()))

	lm := resize(t, app.Initial(), 120, 30).(*model)

	// Select the first item, then Apply runs the summary (10.1).
	lm = drive(t, lm, "down", "space").(*model)
	lm = runPalette(t, lm, 8).(*model)
	if lm.screen != screenSummary {
		t.Fatalf("Apply landed on %v, want the summary", lm.screen)
	}
	lm = drive(t, lm, "esc").(*model)

	// Clear pending actions drops the item actions with a notice.
	lm = runPalette(t, lm, 9).(*model)
	if lm.pendingCount() != 0 {
		t.Fatalf("pending after the clear = %d", lm.pendingCount())
	}
	if got := lm.notice.Text(); got != "Discarded 1 pending action." {
		t.Errorf("clear notice = %q", got)
	}

	// Help opens the full help of 12.5.
	lm = runPalette(t, lm, 10).(*model)
	if !lm.helpOpen {
		t.Fatal("Help opened no help")
	}
	lm = drive(t, lm, "esc").(*model)

	// Quit without pending actions quits at once (11.3).
	next, cmd := lm.Update(key("q"))
	if cmd == nil {
		m := next.(*model)
		if m.dialog != nil {
			t.Fatalf("q opened %v with nothing pending", m.dialog.kind)
		}
		t.Fatal("q without pending returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q without pending did not quit")
	}
}

func TestHelpScreen(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	app := NewApp(st, lock.NewStore(t.TempDir()))

	lm := resize(t, app.Initial(), 100, 30).(*model)
	lm = drive(t, lm, "?").(*model)
	if !lm.helpOpen {
		t.Fatal("? opened no help")
	}
	joined := strings.Join(lm.helpLines, "\n")
	for _, want := range []string{
		"Keys", "Global", "Kit row", "Item row", "+ add kit row",
		"Move the cursor", "Command palette", "Force quit without dialogs",
		"Status glyphs", "Pending actions",
		"Available (not installed)", "Broken: owned content is missing from the target",
		"Install, update or downgrade", "Reset to kit",
		// The legends come from the visual tables (3.1, 3.2).
		"○", "✗", "◌", "⊘", "✓", "↺", "−",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the help misses %q", want)
		}
	}

	// The arrows scroll the lines like the details view.
	lm = drive(t, lm, "down", "down", "down").(*model)
	if lm.helpOffset == 0 {
		t.Fatal("down did not scroll the help")
	}
	lm = drive(t, lm, "esc").(*model)
	if lm.helpOpen {
		t.Fatal("esc did not close the help")
	}
}

func TestRescanNoticeDiff(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// Install both items, then unselect beta: the held selection of alpha
	// stays, the rescan must not drop it.
	lm := driveAll(t, resize(t, app.Initial(), 100, 30),
		"down", "space", "a", "enter", "enter",
		"down", "down", "space", "a", "enter", "enter",
		"down", "down", "space").(*model)
	if lm.pendingCount() != 1 {
		t.Fatalf("pending = %d, want the held selection of alpha alone", lm.pendingCount())
	}

	// A selection of an item the kit does not provide is stale; the
	// rescan drops it and counts it in the same message.
	if err := st.Update(func(reg *state.Registry) error {
		p := reg.Pending[target]
		if p == nil {
			p = &state.Pending{}
			reg.Pending[target] = p
		}
		if p.Items == nil {
			p.Items = map[string]*state.ItemPending{}
		}
		p.Items["ex/gone"] = &state.ItemPending{Select: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The rescan: alpha publishes 2.0.0, beta leaves the kit — the
	// installed one turns into an orphan — and gamma joins it.
	rewritePlainKit(t, kitDir)
	lm = drive(t, lm, "R").(*model)
	want := "Rescanned 1 kit: 1 new item, 1 update, 1 orphan, 1 pending action dropped."
	if got := lm.notice.Text(); got != want {
		t.Fatalf("rescan notice =\n%q\nwant\n%q", got, want)
	}
	pending := registryOf(t, st).Pending[target]
	if _, ok := pending.Items["ex/gone"]; ok {
		t.Error("the stale selection survived the rescan")
	}
	if _, ok := pending.Items["ex/alpha"]; !ok {
		t.Error("the held selection of alpha did not survive the rescan")
	}
}

func TestAttachUndoNotice(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterTarget(target); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// The attach of 6.2 through the palette's Add kit: the notice offers
	// the undo (11.2).
	lm := resize(t, app.Initial(), 120, 30).(*model)
	lm = runPalette(t, lm, 0).(*model)
	lm.pathInput.SetValue(kitDir)
	lm = drive(t, lm, "enter").(*model)
	want := "Attached ex at " + displayPath(target) + "."
	if got := lm.notice.Text(); got != want {
		t.Fatalf("attach notice = %q, want %q", got, want)
	}
	if lm.noticeUndo == nil {
		t.Fatal("the attach notice offers no undo")
	}

	// u detaches back — one operation per press — and the detach notice
	// offers the way back in turn.
	lm = drive(t, lm, "u").(*model)
	if got := lm.notice.Text(); got != "Detached ex." {
		t.Fatalf("undo notice = %q", got)
	}
	if n := len(registryOf(t, st).KitsFor(target)); n != 0 {
		t.Fatalf("attachments after the undo = %d", n)
	}

	// The second u attaches again.
	lm = drive(t, lm, "u").(*model)
	if got := lm.notice.Text(); got != want {
		t.Fatalf("second undo notice = %q, want %q", got, want)
	}
	if n := len(registryOf(t, st).KitsFor(target)); n != 1 {
		t.Fatalf("attachments after the second undo = %d", n)
	}

	// Any other key drops the offer.
	lm = drive(t, lm, "j").(*model)
	if lm.noticeUndo != nil {
		t.Fatal("a key in between kept the undo offer")
	}
}

func TestDetachUndoNotice(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// The detach of a kit without installed items is immediate (6.7) and
	// its notice offers the undo (11.2).
	lm := resize(t, app.Initial(), 120, 30).(*model)
	lm = drive(t, lm, "d").(*model)
	if got := lm.notice.Text(); got != "Detached ex." {
		t.Fatalf("detach notice = %q", got)
	}
	if lm.noticeUndo == nil {
		t.Fatal("the detach notice offers no undo")
	}
	if n := len(registryOf(t, st).KitsFor(target)); n != 0 {
		t.Fatalf("attachments after the detach = %d", n)
	}

	// u attaches the kit back.
	lm = drive(t, lm, "u").(*model)
	want := "Attached ex at " + displayPath(target) + "."
	if got := lm.notice.Text(); got != want {
		t.Fatalf("undo notice = %q, want %q", got, want)
	}
	if n := len(registryOf(t, st).KitsFor(target)); n != 1 {
		t.Fatalf("attachments after the undo = %d", n)
	}
}

func TestRestoreStartupDialog(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	// Two of the three saved actions name nothing they can act on: the
	// selection of an item the kit does not provide, and the removal of
	// an item nothing installed.
	if err := st.Update(func(reg *state.Registry) error {
		reg.Pending[target] = &state.Pending{Items: map[string]*state.ItemPending{
			"ex/alpha": {Select: true},
			"ex/gone":  {Select: true},
			"ex/ghost": {Remove: true},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// The startup opens the same offer switching opens (11.1).
	lm := resize(t, app.Initial(), 120, 30).(*model)
	if lm.dialog == nil || lm.dialog.kind != dialogRestore {
		t.Fatal("the startup opened no restore dialog")
	}
	joined := strings.Join(lm.dialog.lines(120, lm.set), "\n")
	if !strings.Contains(joined, "The last session ended with 3 pending actions") {
		t.Errorf("the dialog misses the count:\n%s", joined)
	}

	// Restore keeps the valid action and drops the invalid ones with a
	// notice (11.1).
	lm = drive(t, lm, "enter").(*model)
	if lm.dialog != nil {
		t.Fatal("enter did not close the restore dialog")
	}
	if got := lm.notice.Text(); got != "Dropped 2 invalid pending actions." {
		t.Fatalf("restore notice = %q", got)
	}
	pending := registryOf(t, st).Pending[target]
	if len(pending.Items) != 1 {
		t.Fatalf("pending after the restore = %v, want alpha alone", pending.Items)
	}
	if _, ok := pending.Items["ex/alpha"]; !ok {
		t.Error("the valid selection did not survive the restore")
	}
}

func TestRestoreEscDiscards(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	if err := st.Update(func(reg *state.Registry) error {
		reg.Pending[target] = &state.Pending{Items: map[string]*state.ItemPending{
			"ex/alpha": {Select: true},
			"ex/beta":  {Select: true},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// esc starts with none (11.1) and reports the discard on the notice
	// line.
	lm := resize(t, app.Initial(), 120, 30).(*model)
	lm = drive(t, lm, "esc").(*model)
	if lm.dialog != nil {
		t.Fatal("esc did not close the restore dialog")
	}
	if got := lm.notice.Text(); got != "Discarded 2 pending actions." {
		t.Fatalf("discard notice = %q", got)
	}
	if p := registryOf(t, st).Pending[target]; p != nil && len(p.Items) != 0 {
		t.Fatalf("pending after esc = %v", p.Items)
	}
}

func TestQuitDialog(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	if err := st.Update(func(reg *state.Registry) error {
		reg.Pending[target] = &state.Pending{Items: map[string]*state.ItemPending{
			"ex/alpha": {Select: true},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// The startup opens the restore offer with the pending actions; the
	// test takes it before driving the tree.
	lm := acceptRestore(t, resize(t, app.Initial(), 120, 30))

	// q with pending item actions opens the dialog of 11.3, Quit chosen.
	lm = drive(t, lm, "q").(*model)
	if lm.dialog == nil || lm.dialog.kind != dialogQuit {
		t.Fatal("q with pending opened no quit dialog")
	}
	if lm.dialog.chosen != 0 || strings.Join(lm.dialog.options, "|") != "Quit|Back" {
		t.Fatalf("options = %v at %d, want Quit|Back at Quit", lm.dialog.options, lm.dialog.chosen)
	}
	joined := strings.Join(lm.dialog.lines(120, lm.set), "\n")
	for _, want := range []string{"1 pending action has not been applied.", "offered for restoration on the next start"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the dialog misses %q:\n%s", want, joined)
		}
	}

	// esc goes back; the second q chooses Quit and quits.
	lm = drive(t, lm, "esc").(*model)
	if lm.dialog != nil {
		t.Fatal("esc did not close the quit dialog")
	}
	next, _ := lm.Update(key("q"))
	next, cmd := next.Update(key("enter"))
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("enter on Quit returned %v, want the quit", cmd())
	}
	_ = next
}

func TestDoubleCtrlC(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	if err := st.Update(func(reg *state.Registry) error {
		reg.Pending[target] = &state.Pending{Items: map[string]*state.ItemPending{
			"ex/alpha": {Select: true},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	// The startup opens the restore offer; the test takes it first.
	lm := acceptRestore(t, resize(t, app.Initial(), 120, 30))

	// The first ctrl+c arms the quit and shows the notice (12.1); it does
	// not quit and opens no dialog.
	next, _ := lm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	lm = next.(*model)
	if !lm.ctrlcArmed {
		t.Fatal("the first ctrl+c armed nothing")
	}
	if got := lm.notice.Text(); got != "press ctrl+c again to quit" {
		t.Fatalf("the armed notice = %q", got)
	}
	if lm.dialog != nil {
		t.Fatal("the first ctrl+c opened a dialog")
	}

	// Any other key disarms it.
	next, _ = lm.Update(key("j"))
	lm = next.(*model)
	if lm.ctrlcArmed {
		t.Fatal("a key in between did not disarm the quit")
	}

	// The second of two presses quits at once, without the dialog (11.3
	// is skipped).
	next, _ = lm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next, cmd := next.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("the second ctrl+c returned %v, want the quit", cmd())
	}
	_ = next
}

func TestReconcileNotice(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, "ex", kitDir, target)
	app := NewApp(st, lock.NewStore(t.TempDir()))

	driveAll(t, resize(t, app.Initial(), 100, 30), "down", "space", "a", "enter", "enter")

	// The interruption: the update of alpha moved the content in and left
	// a staged temp file, the record still holds 1.0.0.
	if err := os.WriteFile(filepath.Join(target, "skills", "alpha", "a.md"), []byte("alpha v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(target, ".penates-99")
	if err := os.WriteFile(staged, []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The next start reconciles and reports what it found (11.2).
	lm := resize(t, app.Initial(), 100, 30).(*model)
	want := "Reconciled after an interrupted apply: alpha recorded as 1.0.0."
	if got := lm.notice.Text(); got != want {
		t.Fatalf("reconcile notice =\n%q\nwant\n%q", got, want)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("the staged file survived: %v", err)
	}
}
