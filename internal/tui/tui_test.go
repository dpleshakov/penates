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

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// drive feeds keys to the model, ignoring commands: the flow under test is pure.
func drive(t *testing.T, m tea.Model, keys ...string) tea.Model {
	t.Helper()
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
}

func TestRegisterListApplyFlow(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := app.Initial(kitDir)
	if m.(*model).screen != screenRegister {
		t.Fatalf("first screen = %v, want register", m.(*model).screen)
	}

	// Enter accepts the proposed target; the list shows both items, none installed.
	m = drive(t, m, "enter")
	lm := m.(*model)
	if lm.screen != screenList {
		t.Fatalf("screen after register = %v, want list", lm.screen)
	}
	if len(lm.rows) != 2 {
		t.Fatalf("rows = %v", lm.rows)
	}
	for i, want := range []string{"ex/good", "ex/varitem"} {
		if lm.rows[i].key != want || lm.rows[i].installed {
			t.Errorf("row %d = %+v, want %s not installed", i, lm.rows[i], want)
		}
	}

	// Select both items and apply: the form asks for the varitem's required
	// variable first.
	m = drive(t, m, "space", "down", "space", "a")
	fm := m.(*model)
	if fm.screen != screenForm {
		t.Fatalf("screen after apply = %v, want form", fm.screen)
	}
	if len(fm.formFields) != 1 || fm.formFields[0].key != "ex/varitem" || fm.formFields[0].varID != "p" || fm.formFields[0].prompt != "pick a word" {
		t.Fatalf("form fields = %+v", fm.formFields)
	}

	// The answer leads to the summary first: nothing is written until the
	// summary is confirmed (RFC 10.2).
	m = drive(t, m, "answered", "enter")
	sm := m.(*model)
	if sm.screen != screenSummary {
		t.Fatalf("screen after form = %v, want summary", sm.screen)
	}
	m = drive(t, m, "enter")
	rm := m.(*model)
	if rm.screen != screenResults {
		t.Fatalf("screen after summary = %v, want results", rm.screen)
	}
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") || !strings.Contains(rm.lastResults, "applied  ex/varitem 0.1.0") {
		t.Errorf("results miss the applied items:\n%s", rm.lastResults)
	}

	installed, err := os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(installed) != "good skill\n" {
		t.Errorf("installed file = %q, %v", installed, err)
	}
	templated, err := os.ReadFile(filepath.Join(target, "skills", "varitem", "v.md"))
	if err != nil || string(templated) != "var item answered\n" {
		t.Errorf("templated file = %q, %v; want the substituted content", templated, err)
	}
	lk, err := lock.NewStore(dataDir).Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if lk.Items["ex/good"] == nil {
		t.Errorf("lock items = %v", lk.Items)
	}
	if diff := cmp.Diff(map[string]string{"p": "answered"}, lk.Items["ex/varitem"].Answers); diff != "" {
		t.Errorf("recorded answers (-want +got):\n%s", diff)
	}

	// Back to the list: the applied items are marked installed, and applying
	// the same selection again reuses the recorded answers — no form, straight
	// to the summary that keeps everything, then the results.
	m = drive(t, m, "enter")
	lm = m.(*model)
	if !lm.rows[0].installed || !lm.rows[0].selected || !lm.rows[1].installed {
		t.Errorf("after apply: %+v %+v", lm.rows[0], lm.rows[1])
	}
	m = drive(t, m, "a")
	if m.(*model).screen != screenSummary {
		t.Fatalf("re-apply screen = %v, want summary without a form", m.(*model).screen)
	}
	m = drive(t, m, "enter")
	rm = m.(*model)
	if rm.screen != screenResults {
		t.Fatalf("re-apply screen = %v, want results without a form", rm.screen)
	}
	if !strings.Contains(rm.lastResults, "kept     ex/good: already installed") || !strings.Contains(rm.lastResults, "kept     ex/varitem: already installed") {
		t.Errorf("re-apply results:\n%s", rm.lastResults)
	}
}

// TestRegisterFormTyping checks that the register form accepts editing: clear
// the prefilled hint, type another target, and confirm.
func TestRegisterFormTyping(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "chosen")
	kitDir := writeFixtureKit(t, filepath.Join(t.TempDir(), "hinted"))

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := app.Initial(kitDir)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(target)})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	lm := m.(*model)
	if lm.screen != screenList {
		t.Fatalf("screen after register = %v, want list", lm.screen)
	}
	if lm.target != target {
		t.Errorf("target = %q, want the typed %q", lm.target, target)
	}
	st, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := st.KitTarget("ex"); !ok || got != target {
		t.Errorf("registered target = %q, %v; want %q", got, ok, target)
	}
}

// writeFixtureKit writes a kit with one installable item — tagged, with a
// changelog — and one that uses a required variable in a templated file,
// proposing target as its target hint.
func writeFixtureKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"kit.yaml":                  "name: ex\ntarget_hint: " + target + "\n",
		"items/good/item.yaml":      "id: good\nversion: 1.0.0\ndescription: Good item\ntags:\n  - writing\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/good/SKILL.md\n",
		"items/good/skill/SKILL.md": "good skill\n",
		"items/good/CHANGELOG.md":   "## 1.0.0\n\nfirst release\n",
		"items/varitem/item.yaml":   "id: varitem\nversion: 0.1.0\ndescription: Var item\nvars:\n  - {id: p, prompt: pick a word, required: true}\ncontributes:\n  - file: v.md\n    to: skills/varitem/v.md\n    template: true\n",
		"items/varitem/v.md":        "var item {{p}}\n",
	}
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestUpdateAndResetFlow walks the version display of RFC 8 and the reset
// request of 7.6: an update shows `1.0.0 > 1.1.0`, a downgrade shows
// `1.1.0 < 0.9.0`, a re-apply without a reset keeps the user's modification,
// and with the reset requested the kit content replaces it.
func TestUpdateAndResetFlow(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := app.Initial(kitDir)
	m = drive(t, m, "enter", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("first apply:\n%s", rm.lastResults)
	}

	// The kit publishes 1.1.0 with new content: the list shows the update.
	bumpKit(t, kitDir, "1.1.0", "good skill v2\n")
	m = drive(t, m, "enter")
	lm := m.(*model)
	if lm.rows[0].installedVersion != "1.0.0" {
		t.Fatalf("installed version = %q", lm.rows[0].installedVersion)
	}
	if got := lm.versionLabel(lm.rows[0]); got != "1.0.0 > 1.1.0" {
		t.Errorf("version label = %q, want the update arrow", got)
	}

	// The persisted selection applies the update.
	m = drive(t, m, "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.1.0") {
		t.Fatalf("update apply:\n%s", rm.lastResults)
	}
	data, err := os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(data) != "good skill v2\n" {
		t.Fatalf("updated file = %q, %v", data, err)
	}

	// The kit publishes 0.9.0: shown as a downgrade.
	bumpKit(t, kitDir, "0.9.0", "good skill v2\n")
	m = drive(t, m, "enter")
	lm = m.(*model)
	if got := lm.versionLabel(lm.rows[0]); got != "1.1.0 < 0.9.0" {
		t.Errorf("version label = %q, want the downgrade arrow", got)
	}

	// The user modifies the installed file while the kit content is
	// unchanged; a fresh run applies nothing and keeps the modification.
	bumpKit(t, kitDir, "1.1.0", "good skill v2\n")
	if err := os.WriteFile(filepath.Join(target, "skills", "good", "SKILL.md"), []byte("user change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = app.Initial(kitDir)
	lm = m.(*model)
	if lm.screen != screenList || !lm.rows[0].selected {
		t.Fatalf("restarted screen = %v selected %v", lm.screen, lm.rows[0].selected)
	}
	m = drive(t, m, "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "kept     ex/good: already installed") {
		t.Fatalf("apply over a user modification:\n%s", rm.lastResults)
	}
	data, err = os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(data) != "user change\n" {
		t.Fatalf("user modification overwritten without a reset: %q, %v", data, err)
	}

	// With the reset requested, the kit content replaces the user's change.
	m = drive(t, m, "enter", "r", "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.1.0") {
		t.Fatalf("reset apply:\n%s", rm.lastResults)
	}
	data, err = os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(data) != "good skill v2\n" {
		t.Fatalf("reset file = %q, %v; want the kit content back", data, err)
	}
}

// TestRemoveFlow removes an installed item through the list: the item is
// marked for removal with d, the apply reports it removed, the target loses
// the owned file and the directory penates created for it, the lock record
// dies, and the list stops showing the item as installed (RFC 9.1).
func TestRemoveFlow(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := app.Initial(kitDir)
	m = drive(t, m, "enter", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// Back to the list: the installed row takes the removal mark, and the
	// apply removes it.
	m = drive(t, m, "enter", "d")
	lm := m.(*model)
	if !lm.rows[0].installed || !lm.rows[0].remove {
		t.Fatalf("row after d = %+v, want installed and marked", lm.rows[0])
	}
	m = drive(t, m, "a", "enter")
	rm = m.(*model)
	if rm.screen != screenResults || !strings.Contains(rm.lastResults, "removed  ex/good") {
		t.Fatalf("removal apply:\n%s", rm.lastResults)
	}
	if strings.Contains(rm.lastResults, "excluded") {
		t.Errorf("removal reported an exclusion:\n%s", rm.lastResults)
	}

	if _, err := os.Stat(filepath.Join(target, "skills", "good", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("the owned file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "good")); !os.IsNotExist(err) {
		t.Errorf("the emptied directory survived: %v", err)
	}
	lk, err := lock.NewStore(dataDir).Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(lk.Items) != 0 {
		t.Errorf("lock items after removal = %v", lk.Items)
	}

	// The list no longer offers the item as installed, and d on it does
	// nothing.
	m = drive(t, m, "enter")
	lm = m.(*model)
	if lm.rows[0].installed || lm.rows[0].selected || lm.rows[0].remove {
		t.Fatalf("row after removal = %+v", lm.rows[0])
	}
	m = drive(t, m, "d")
	if m.(*model).rows[0].remove {
		t.Error("d marked a row that is not installed")
	}
}

// TestRemoveReportsKeptContent pins the report of RFC 9.1: removing an item
// whose file the user modified keeps the file, names it in the results, and
// still drops the lock record.
func TestRemoveReportsKeptContent(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := drive(t, app.Initial(kitDir), "enter", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// The user edits the installed file; the removal keeps it and says so.
	if err := os.WriteFile(filepath.Join(target, "skills", "good", "SKILL.md"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = drive(t, m, "enter", "d", "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "removed  ex/good") || !strings.Contains(rm.lastResults, "kept skills/good/SKILL.md: modified by the user") {
		t.Fatalf("removal over a modification:\n%s", rm.lastResults)
	}
	data, err := os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(data) != "user edit\n" {
		t.Errorf("file after removal = %q, %v; want the modification kept", data, err)
	}
	lk, err := lock.NewStore(dataDir).Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(lk.Items) != 0 {
		t.Errorf("lock items after removal = %v", lk.Items)
	}
}

// TestOrphanRemoval walks the frozen-orphan flow of RFC 9.2: an installed
// item no kit provides anymore stays in the list as an orphan and removes
// from the lock and the target like any other item.
func TestOrphanRemoval(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	// The lock holds an item no kit provides: its kit is gone.
	locks := lock.NewStore(dataDir)
	lk, err := locks.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	lk.SetItem("gone/old", &lock.Item{Version: "0.9.0"})
	if err := locks.Save(lk); err != nil {
		t.Fatal(err)
	}

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, locks)

	m := app.Initial(kitDir)
	m = drive(t, m, "enter")
	lm := m.(*model)
	if len(lm.rows) != 3 {
		t.Fatalf("rows = %v", lm.rows)
	}
	orphan := lm.rows[2]
	if orphan.key != "gone/old" || !orphan.orphan || !orphan.installed {
		t.Fatalf("orphan row = %+v", orphan)
	}

	// The orphan removes from the lock alone, without any kit (RFC 9.2).
	m = drive(t, m, "down", "down", "d", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "removed  gone/old") {
		t.Fatalf("orphan removal:\n%s", rm.lastResults)
	}
	fresh, err := locks.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Items) != 0 {
		t.Errorf("lock items after orphan removal = %v", fresh.Items)
	}
}

// writeSectionKit writes a kit whose three items all contribute a section to
// one shared file: two plain and one with a required templated variable.
func writeSectionKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"kit.yaml":                "name: ex\ntarget_hint: " + target + "\nshared:\n  - AGENTS.md\n",
		"items/one/item.yaml":     "id: one\nversion: 1.0.0\ndescription: One rules\ncontributes:\n  - section: note.md\n    to: AGENTS.md\n",
		"items/one/note.md":       "## One rules\n\none body\n",
		"items/two/item.yaml":     "id: two\nversion: 1.0.0\ndescription: Two rules\ncontributes:\n  - section: note.md\n    to: AGENTS.md\n",
		"items/two/note.md":       "## Two rules\n\ntwo body\n",
		"items/varitem/item.yaml": "id: varitem\nversion: 0.1.0\ndescription: Var rules\nvars:\n  - {id: p, prompt: pick a word, required: true}\ncontributes:\n  - section: note.md\n    to: AGENTS.md\n    template: true\n",
		"items/varitem/note.md":   "## Var rules\n\nfor {{p}}\n",
	}
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestRemoveThenInstallSameFile pins the order of one apply: the removals run
// first and the installs plan from the target as the removals leave it, so
// removing an item and installing another into the same shared file cannot
// bring the removed content back (RFC 9).
func TestRemoveThenInstallSameFile(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeSectionKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := app.Initial(kitDir)
	m = drive(t, m, "enter", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/one 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// Mark the installed item for removal and select another writer of the
	// same shared file: one apply must land the target on two's block alone.
	m = drive(t, m, "enter", "down", "space", "up", "d", "a", "enter")
	rm = m.(*model)
	if rm.screen != screenResults {
		t.Fatalf("screen = %v, want results", rm.screen)
	}
	if !strings.Contains(rm.lastResults, "removed  ex/one") || !strings.Contains(rm.lastResults, "applied  ex/two 1.0.0") {
		t.Fatalf("removal and install:\n%s", rm.lastResults)
	}

	data, err := os.ReadFile(filepath.Join(target, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md: %v", err)
	}
	want := "<!-- penates:begin ex/two -->\n## Two rules\n\ntwo body\n\n<!-- penates:end ex/two -->\n"
	if string(data) != want {
		t.Errorf("AGENTS.md =\n%q\nwant\n%q", data, want)
	}
	lk, err := lock.NewStore(dataDir).Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if lk.Items["ex/one"] != nil || lk.Items["ex/two"] == nil {
		t.Errorf("lock items = %v", lk.Items)
	}
}

// TestRemoveWithFormResume removes an item and installs one that asks for a
// variable: the form interrupts before anything is written, the summary
// lists the removal and the install together, and the confirmed apply
// neither re-reports the removed item as excluded nor reinstalls it through
// its held selection (RFC 9, 10.2).
func TestRemoveWithFormResume(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeSectionKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := app.Initial(kitDir)
	m = drive(t, m, "enter", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/one 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// The form interrupts before anything is written: the removal of the
	// marked item waits for the summary's confirmation (RFC 10.2).
	m = drive(t, m, "enter", "down", "down", "space", "up", "up", "d", "a")
	fm := m.(*model)
	if fm.screen != screenForm {
		t.Fatalf("screen after apply = %v, want form", fm.screen)
	}
	if _, err := os.Stat(filepath.Join(target, "AGENTS.md")); err != nil {
		t.Fatalf("the removal ran before the form: %v", err)
	}

	// The answer leads to the summary; it lists the removal and the install
	// together.
	m = drive(t, m, "answered", "enter")
	sm := m.(*model)
	if sm.screen != screenSummary {
		t.Fatalf("screen after form = %v, want summary", sm.screen)
	}
	view := sm.View()
	if !strings.Contains(view, "removals:") || !strings.Contains(view, "ex/one 1.0.0") || !strings.Contains(view, "changes:") {
		t.Errorf("the summary misses the removal or the install:\n%s", view)
	}

	// The confirmation applies: the varitem installs into the target as the
	// removal left it; the removed item is reported as removed, not as
	// excluded or installed again.
	m = drive(t, m, "enter")
	rm = m.(*model)
	if rm.screen != screenResults || !strings.Contains(rm.lastResults, "applied  ex/varitem 0.1.0") {
		t.Fatalf("resumed apply:\n%s", rm.lastResults)
	}
	if strings.Contains(rm.lastResults, "the item is not installed") || strings.Contains(rm.lastResults, "applied  ex/one") {
		t.Errorf("the removed item leaked into the results:\n%s", rm.lastResults)
	}

	data, err := os.ReadFile(filepath.Join(target, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md: %v", err)
	}
	want := "<!-- penates:begin ex/varitem -->\n## Var rules\n\nfor answered\n\n<!-- penates:end ex/varitem -->\n"
	if string(data) != want {
		t.Errorf("AGENTS.md =\n%q\nwant\n%q", data, want)
	}
	lk, err := lock.NewStore(dataDir).Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if lk.Items["ex/one"] != nil || lk.Items["ex/varitem"] == nil {
		t.Errorf("lock items = %v", lk.Items)
	}

	// The list drops every mark of the removed row.
	m = drive(t, m, "enter")
	lm := m.(*model)
	if lm.rows[0].installed || lm.rows[0].selected || lm.rows[0].remove {
		t.Errorf("row of the removed item = %+v", lm.rows[0])
	}
}

// bumpKit rewrites the good item's version and skill content in the fixture
// kit, the way a kit author publishes a new version.
func bumpKit(t *testing.T, kitDir, version, skill string) {
	t.Helper()
	itemYaml := "id: good\nversion: " + version + "\ndescription: Good item\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/good/SKILL.md\n"
	if err := os.WriteFile(filepath.Join(kitDir, "items", "good", "item.yaml"), []byte(itemYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kitDir, "items", "good", "skill", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeKitFiles writes a kit directory from a path-to-content map.
func writeKitFiles(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// writeFilterKit writes a kit of three items whose descriptions and tags tell
// the filter test what to type.
func writeFilterKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	return writeKitFiles(t, dir, map[string]string{
		"kit.yaml":              "name: ex\ntarget_hint: " + target + "\n",
		"items/alpha/item.yaml": "id: alpha\nversion: 1.0.0\ndescription: Marks finished sessions\ntags:\n  - opencode-v2\ncontributes:\n  - file: a.md\n    to: skills/alpha/a.md\n",
		"items/alpha/a.md":      "alpha\n",
		"items/beta/item.yaml":  "id: beta\nversion: 1.0.0\ndescription: Review agent\ntags:\n  - review\ncontributes:\n  - file: b.md\n    to: skills/beta/b.md\n",
		"items/beta/b.md":       "beta\n",
		"items/gamma/item.yaml": "id: gamma\nversion: 1.0.0\ndescription: Stores notes\ncontributes:\n  - file: g.md\n    to: skills/gamma/g.md\n",
		"items/gamma/g.md":      "gamma\n",
	})
}

// TestListFilter drives the `/` filter of RFC 10.2: it narrows the list by
// id, tag, and description; esc clears it; enter keeps it applied; keys that
// quit and apply in the list type into the open filter instead.
func TestListFilter(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFilterKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := drive(t, app.Initial(kitDir), "enter")
	lm := m.(*model)
	if len(lm.rows) != 3 || lm.rows[0].key != "ex/alpha" || lm.rows[2].key != "ex/gamma" {
		t.Fatalf("rows = %+v", lm.rows)
	}
	if len(lm.visible()) != 3 {
		t.Fatalf("visible = %v", lm.visible())
	}

	// The filter matches a description and hides the rest.
	m = drive(t, m, "/", "review")
	lm = m.(*model)
	if !lm.filtering {
		t.Fatal("typing / did not open the filter")
	}
	if vis := lm.visible(); len(vis) != 1 || lm.rows[vis[0]].key != "ex/beta" {
		t.Fatalf("visible after 'review' = %v", vis)
	}
	if view := lm.View(); !strings.Contains(view, "ex/beta") || strings.Contains(view, "ex/alpha") || strings.Contains(view, "ex/gamma") {
		t.Errorf("the list is not narrowed to beta:\n%s", view)
	}

	// esc clears the filter and shows everything again.
	m = drive(t, m, "esc")
	lm = m.(*model)
	if lm.filtering || lm.filter.Value() != "" {
		t.Fatalf("esc left the filter open or set: %v %q", lm.filtering, lm.filter.Value())
	}
	if len(lm.visible()) != 3 {
		t.Fatalf("visible after esc = %v", lm.visible())
	}

	// A tag matches too.
	m = drive(t, m, "/", "opencode-v2")
	lm = m.(*model)
	if vis := lm.visible(); len(vis) != 1 || lm.rows[vis[0]].key != "ex/alpha" {
		t.Fatalf("visible after 'opencode-v2' = %v", vis)
	}

	// enter closes the filter and keeps it applied.
	m = drive(t, m, "enter")
	lm = m.(*model)
	if lm.filtering || len(lm.visible()) != 1 {
		t.Fatalf("after enter: filtering %v, visible %v", lm.filtering, lm.visible())
	}

	// No match: the list is empty, and acting on it changes nothing.
	m = drive(t, m, "esc", "/", "zzz", "space", "down", "i")
	lm = m.(*model)
	if len(lm.visible()) != 0 || lm.screen != screenList {
		t.Fatalf("after no-match: visible %v, screen %v", lm.visible(), lm.screen)
	}

	// With the filter applied, selection and apply still act on the row the
	// filter shows.
	m = drive(t, m, "esc", "/", "beta", "enter", "space", "a", "enter")
	rm := m.(*model)
	if rm.screen != screenResults || !strings.Contains(rm.lastResults, "applied  ex/beta 1.0.0") {
		t.Fatalf("filtered apply:\n%s", rm.lastResults)
	}
	if strings.Contains(rm.lastResults, "ex/alpha") || strings.Contains(rm.lastResults, "ex/gamma") {
		t.Errorf("the filter leaked into the selection:\n%s", rm.lastResults)
	}
}

// TestItemDetails opens the details screen with i and checks that it shows
// the description, the contributions, the changelog when the item carries
// one, the source kit, and the recorded variable answers (RFC 10.2).
func TestItemDetails(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// Install both items so the answers of varitem are recorded.
	m := drive(t, app.Initial(kitDir), "enter", "space", "down", "space", "a", "answered", "enter", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/varitem") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// The first item carries a changelog and tags.
	m = drive(t, m, "enter", "i")
	dm := m.(*model)
	if dm.screen != screenDetails {
		t.Fatalf("screen after i = %v, want details", dm.screen)
	}
	view := dm.View()
	for _, want := range []string{"ex/good", "Good item", "tags  writing", "kit   ex", "skills/good/SKILL.md", "changelog:", "first release"} {
		if !strings.Contains(view, want) {
			t.Errorf("details miss %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "answers:") {
		t.Errorf("details invent answers for an item without variables:\n%s", view)
	}

	// esc returns to the list; the second item shows its templated
	// contribution and the answer the installation recorded.
	m = drive(t, m, "esc", "down", "i")
	dm = m.(*model)
	if dm.screen != screenDetails {
		t.Fatalf("screen after esc, down, i = %v, want details", dm.screen)
	}
	view = dm.View()
	for _, want := range []string{"ex/varitem", "Var item", "skills/varitem/v.md  (template)", "p = answered"} {
		if !strings.Contains(view, want) {
			t.Errorf("details miss %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "changelog:") {
		t.Errorf("details show a changelog for an item without one:\n%s", view)
	}

	// esc brings the list back.
	m = drive(t, m, "esc")
	if m.(*model).screen != screenList {
		t.Fatal("esc did not return to the list")
	}
}

// TestListStatuses walks every status of the RFC 10.2 list through one
// target: not installed, selected, installed, update, downgrade, warning,
// modified, broken, conflict, orphan, and kit unavailable each render their
// own bracket marker, and the planner's conflict and warning verdicts reach
// the rows of the current selection.
func TestListStatuses(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "target")
	kitDir := writeStatusKit(t, targetDir)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// Nothing is installed: both rows show the empty marker.
	m := drive(t, app.Initial(kitDir), "enter")
	lm := m.(*model)
	if lm.rows[0].key != "ex/dup" || lm.rows[1].key != "ex/plain" {
		t.Fatalf("rows = %+v", lm.rows)
	}
	if lm.rows[0].statusWord() != " " || lm.rows[1].statusWord() != " " {
		t.Fatalf("fresh rows = %q %q, want empty statuses", lm.rows[0].statusWord(), lm.rows[1].statusWord())
	}
	if !strings.Contains(lm.View(), "[ ]") {
		t.Errorf("the fresh list shows no empty marker:\n%s", lm.View())
	}

	// Selecting marks the row as selected.
	m = drive(t, m, "down", "space")
	lm = m.(*model)
	if !lm.rows[1].selected || lm.rows[1].statusWord() != "selected" {
		t.Fatalf("selected row = %+v, word %q", lm.rows[1], lm.rows[1].statusWord())
	}
	if !strings.Contains(lm.View(), "[selected]") {
		t.Errorf("no selected marker:\n%s", lm.View())
	}

	// Applying makes the item installed; the bracket shows it.
	m = drive(t, m, "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/plain 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}
	m = drive(t, m, "enter")
	lm = m.(*model)
	if lm.rows[1].statusWord() != "installed" {
		t.Fatalf("installed word = %q", lm.rows[1].statusWord())
	}
	if !strings.Contains(lm.View(), "[installed]") {
		t.Errorf("no installed marker:\n%s", lm.View())
	}

	// A higher kit version is an update, with the arrow in the version
	// column.
	bumpStatusKit(t, kitDir, "1.1.0", "plain v2\n")
	m = app.Initial(kitDir)
	lm = m.(*model)
	if !lm.rows[1].update || lm.rows[1].statusWord() != "update" {
		t.Fatalf("update row = %+v, word %q", lm.rows[1], lm.rows[1].statusWord())
	}
	if view := lm.View(); !strings.Contains(view, "[update]") || !strings.Contains(view, "1.0.0 > 1.1.0") {
		t.Errorf("no update marker or arrow:\n%s", view)
	}

	// A lower kit version is a downgrade.
	bumpStatusKit(t, kitDir, "0.9.0", "plain v2\n")
	m = app.Initial(kitDir)
	lm = m.(*model)
	if !lm.rows[1].downgrade || lm.rows[1].statusWord() != "downgrade" {
		t.Fatalf("downgrade row = %+v, word %q", lm.rows[1], lm.rows[1].statusWord())
	}
	if view := lm.View(); !strings.Contains(view, "[downgrade]") || !strings.Contains(view, "1.0.0 < 0.9.0") {
		t.Errorf("no downgrade marker or arrow:\n%s", view)
	}

	// A major bump is marked for review (RFC 8): the row carries the major
	// flag and the update marker takes the error style.
	bumpStatusKit(t, kitDir, "2.0.0", "plain v2\n")
	m = app.Initial(kitDir)
	lm = m.(*model)
	if !lm.rows[1].major || lm.rows[1].statusWord() != "update" {
		t.Fatalf("major row = %+v, word %q", lm.rows[1], lm.rows[1].statusWord())
	}
	if view := lm.View(); !strings.Contains(view, "[update]") || !strings.Contains(view, "1.0.0 > 2.0.0") {
		t.Errorf("no major update marker or arrow:\n%s", view)
	}

	// The same version with changed content warns while the item is
	// selected: applying would keep everything.
	bumpStatusKit(t, kitDir, "1.0.0", "plain v2 changed\n")
	m = app.Initial(kitDir)
	lm = m.(*model)
	if !lm.rows[1].warning || lm.rows[1].statusWord() != "warning" {
		t.Fatalf("warning row = %+v, word %q", lm.rows[1], lm.rows[1].statusWord())
	}
	if !strings.Contains(lm.View(), "[warning]") {
		t.Errorf("no warning marker:\n%s", lm.View())
	}

	// With the kit content back to the installed one, a user edit shows as
	// modified.
	bumpStatusKit(t, kitDir, "1.0.0", "plain v1\n")
	if err := os.WriteFile(filepath.Join(targetDir, "skills", "plain", "SKILL.md"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = app.Initial(kitDir)
	lm = m.(*model)
	if !lm.rows[1].modified || lm.rows[1].statusWord() != "modified" {
		t.Fatalf("modified row = %+v, word %q", lm.rows[1], lm.rows[1].statusWord())
	}
	if !strings.Contains(lm.View(), "[modified]") {
		t.Errorf("no modified marker:\n%s", lm.View())
	}

	// Losing the owned file shows broken.
	if err := os.Remove(filepath.Join(targetDir, "skills", "plain", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	m = app.Initial(kitDir)
	lm = m.(*model)
	if !lm.rows[1].broken || lm.rows[1].statusWord() != "broken" {
		t.Fatalf("broken row = %+v, word %q", lm.rows[1], lm.rows[1].statusWord())
	}
	if !strings.Contains(lm.View(), "[broken]") {
		t.Errorf("no broken marker:\n%s", lm.View())
	}

	// The held selection repairs the broken item; the list shows installed
	// again.
	m = drive(t, m, "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/plain 1.0.0") {
		t.Fatalf("repair:\n%s", rm.lastResults)
	}
	m = drive(t, m, "enter")
	lm = m.(*model)
	if lm.rows[1].statusWord() != "installed" {
		t.Fatalf("after repair the word = %q", lm.rows[1].statusWord())
	}

	// A heading the user wrote outside managed blocks conflicts with the
	// section item as soon as it is selected, and the reason shows in the
	// row; deselecting clears the conflict. The list re-reads the target on
	// entry, so the file lands before the selection is toggled.
	if err := os.WriteFile(filepath.Join(targetDir, "AGENTS.md"), []byte("notes\n\n## Dup rules\n\nhand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = app.Initial(kitDir)
	lm = m.(*model)
	m = drive(t, m, "space")
	lm = m.(*model)
	if !lm.rows[0].conflict || lm.rows[0].statusWord() != "conflict" {
		t.Fatalf("conflict row = %+v, word %q", lm.rows[0], lm.rows[0].statusWord())
	}
	view := lm.View()
	if !strings.Contains(view, "[conflict]") || !strings.Contains(view, "already has a heading") {
		t.Errorf("no conflict marker or reason:\n%s", view)
	}
	m = drive(t, m, "space")
	lm = m.(*model)
	if lm.rows[0].statusWord() != " " {
		t.Fatalf("deselected conflict row word = %q", lm.rows[0].statusWord())
	}

	// An installed item whose kit is not registered is an orphan, grouped
	// under its kit's name.
	locks := lock.NewStore(dataDir)
	lk, err := locks.Load(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	lk.SetItem("gone/old", &lock.Item{Version: "0.2.0"})
	if err := locks.Save(lk); err != nil {
		t.Fatal(err)
	}
	m = app.Initial(kitDir)
	lm = m.(*model)
	if lm.rows[2].key != "gone/old" || !lm.rows[2].orphan || lm.rows[2].statusWord() != "orphan" {
		t.Fatalf("orphan row = %+v, word %q", lm.rows[2], lm.rows[2].statusWord())
	}
	view = lm.View()
	if !strings.Contains(view, "[orphan]") || !strings.Contains(view, "kit not registered") {
		t.Errorf("no orphan marker or group note:\n%s", view)
	}

	// When the kit stops loading, its installed items freeze as kit
	// unavailable under the kit's header, next to the healthy kit's items.
	if err := os.Remove(filepath.Join(kitDir, "kit.yaml")); err != nil {
		t.Fatal(err)
	}
	teamDir := writeTeamKit(t, targetDir)
	m = app.Initial(teamDir)
	if m.(*model).screen != screenRegister {
		t.Fatalf("screen = %v, want register for the new kit", m.(*model).screen)
	}
	m = drive(t, m, "enter")
	lm = m.(*model)
	if lm.screen != screenList {
		t.Fatalf("screen after register = %v", lm.screen)
	}
	if lm.rows[1].key != "ex/plain" || !lm.rows[1].unavailable || lm.rows[1].statusWord() != "unavailable" {
		t.Fatalf("unavailable row = %+v, word %q", lm.rows[1], lm.rows[1].statusWord())
	}
	view = lm.View()
	if !strings.Contains(view, "[unavailable]") || !strings.Contains(view, "kit did not load") || !strings.Contains(view, "-- ex") || !strings.Contains(view, "-- team") {
		t.Errorf("the unavailable kit lost its group:\n%s", view)
	}
}

// writeStatusKit writes a kit with a plain file item and a section item
// whose heading the status test can place outside managed blocks.
func writeStatusKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	return writeKitFiles(t, dir, map[string]string{
		"kit.yaml":                   "name: ex\ntarget_hint: " + target + "\nshared:\n  - AGENTS.md\n",
		"items/dup/item.yaml":        "id: dup\nversion: 1.0.0\ndescription: Dup item\ncontributes:\n  - section: note.md\n    to: AGENTS.md\n",
		"items/dup/note.md":          "## Dup rules\n\ndup body\n",
		"items/plain/item.yaml":      "id: plain\nversion: 1.0.0\ndescription: Plain item\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/plain/SKILL.md\n",
		"items/plain/skill/SKILL.md": "plain v1\n",
	})
}

// bumpStatusKit rewrites the plain item's version and skill content in the
// status fixture kit.
func bumpStatusKit(t *testing.T, kitDir, version, skill string) {
	t.Helper()
	itemYaml := "id: plain\nversion: " + version + "\ndescription: Plain item\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/plain/SKILL.md\n"
	if err := os.WriteFile(filepath.Join(kitDir, "items", "plain", "item.yaml"), []byte(itemYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kitDir, "items", "plain", "skill", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeTeamKit writes a second kit for the same target, so the list shows
// two kit groups.
func writeTeamKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	return writeKitFiles(t, dir, map[string]string{
		"kit.yaml":              "name: team\ntarget_hint: " + target + "\n",
		"items/titem/item.yaml": "id: titem\nversion: 1.0.0\ndescription: Team item\ncontributes:\n  - file: t.md\n    to: skills/titem/t.md\n",
		"items/titem/t.md":      "team item\n",
	})
}

// TestKitGroups checks the kit group headers of RFC 10.2: the header line
// names the registered kits, every group header carries the kit directory,
// and a filter that hides a kit's items hides its header too.
func TestKitGroups(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	dirA := writeKitFiles(t, t.TempDir(), map[string]string{
		"kit.yaml":             "name: aaa\ntarget_hint: " + target + "\n",
		"items/aone/item.yaml": "id: aone\nversion: 1.0.0\ndescription: One\ncontributes:\n  - file: a.md\n    to: skills/aone/a.md\n",
		"items/aone/a.md":      "one\n",
	})
	dirB := writeKitFiles(t, t.TempDir(), map[string]string{
		"kit.yaml":             "name: bbb\ntarget_hint: " + target + "\n",
		"items/bone/item.yaml": "id: bone\nversion: 1.0.0\ndescription: Two\ncontributes:\n  - file: b.md\n    to: skills/bone/b.md\n",
		"items/bone/b.md":      "two\n",
	})

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := drive(t, app.Initial(dirA), "enter")
	lm := m.(*model)
	if lm.screen != screenList || len(lm.groups) != 1 || lm.groups[0].kitName != "aaa" {
		t.Fatalf("first kit: screen %v, groups %+v", lm.screen, lm.groups)
	}

	// Registering the second kit lands on the same target: two groups.
	m = drive(t, app.Initial(dirB), "enter")
	lm = m.(*model)
	if len(lm.groups) != 2 || lm.groups[0].kitName != "aaa" || lm.groups[1].kitName != "bbb" {
		t.Fatalf("groups = %+v", lm.groups)
	}
	if lm.rows[0].key != "aaa/aone" || lm.rows[1].key != "bbb/bone" {
		t.Fatalf("rows = %+v", lm.rows)
	}
	if lm.groups[0].kitDir != dirA || lm.groups[1].kitDir != dirB {
		t.Errorf("group dirs = %q %q", lm.groups[0].kitDir, lm.groups[1].kitDir)
	}
	view := lm.View()
	if !strings.Contains(view, "kits: aaa, bbb") {
		t.Errorf("header misses the kit names:\n%s", view)
	}
	if i := strings.Index(view, "-- aaa"); i < 0 || i > strings.Index(view, "-- bbb") {
		t.Errorf("groups out of order:\n%s", view)
	}

	// A filter that nothing in aaa matches hides aaa's header as well.
	m = drive(t, m, "/", "bone")
	lm = m.(*model)
	view = lm.View()
	if strings.Contains(view, "-- aaa") || !strings.Contains(view, "-- bbb") {
		t.Errorf("a filter did not hide the empty group:\n%s", view)
	}
}

// TestReconciliationFlow reconciles the list with the target (RFC 7.7): a
// staged temp file of an interrupted apply is removed on entering the list,
// an item whose owned file is gone is shown broken and repaired by the next
// apply, and a modified owned file is shown modified and kept.
func TestReconciliationFlow(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, targetDir)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := app.Initial(kitDir)
	m = drive(t, m, "enter", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// The interrupted apply: a staged temp file lies beside the owned file,
	// and the owned file itself is gone from the target.
	staged := filepath.Join(targetDir, "skills", "good", ".penates-4211")
	if err := os.WriteFile(staged, []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(targetDir, "skills", "good", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	m = app.Initial(kitDir)
	lm := m.(*model)
	if lm.screen != screenList || !lm.rows[0].broken || lm.rows[0].modified {
		t.Fatalf("row after reconciliation = %+v, want broken", lm.rows[0])
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("the staged file survived the reconciliation: %v", err)
	}
	if view := lm.View(); !strings.Contains(view, "broken") {
		t.Errorf("the list shows no broken mark:\n%s", view)
	}

	// The held selection applies: the broken item is repaired at its
	// installed version.
	m = drive(t, m, "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("repair apply:\n%s", rm.lastResults)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("the repair wrote no file: %v", err)
	}

	// The user modifies the installed file: the list shows modified, and the
	// apply keeps the content.
	if err := os.WriteFile(filepath.Join(targetDir, "skills", "good", "SKILL.md"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = app.Initial(kitDir)
	lm = m.(*model)
	if !lm.rows[0].modified || lm.rows[0].broken {
		t.Fatalf("row after reconciliation = %+v, want modified", lm.rows[0])
	}
	if view := lm.View(); !strings.Contains(view, "modified") {
		t.Errorf("the list shows no modified mark:\n%s", view)
	}
	m = drive(t, m, "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "kept     ex/good: already installed; the content was modified after installation") {
		t.Fatalf("apply over a modification:\n%s", rm.lastResults)
	}
	if data, err := os.ReadFile(filepath.Join(targetDir, "skills", "good", "SKILL.md")); err != nil || string(data) != "user edit\n" {
		t.Errorf("file after apply = %q, %v; want the modification kept", data, err)
	}
}

// TestApplySummary drives the RFC 10.2 summary: applying shows what would
// change per item — and what a removal marked with d takes away — before
// anything is written; the apply runs only after the summary is confirmed,
// and esc cancels out of it without touching the target.
func TestApplySummary(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := app.Initial(kitDir)
	m = drive(t, m, "enter", "space", "a")
	sm := m.(*model)
	if sm.screen != screenSummary {
		t.Fatalf("screen after apply = %v, want summary", sm.screen)
	}
	view := sm.View()
	for _, want := range []string{"changes:", "ex/good 1.0.0", "skills/good/SKILL.md", "target file is absent"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary misses %q:\n%s", want, view)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "good", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("the summary wrote the file: %v", err)
	}

	// esc cancels: back to the list with the selection held, nothing applied.
	m = drive(t, m, "esc")
	lm := m.(*model)
	if lm.screen != screenList || !lm.rows[0].selected {
		t.Fatalf("screen after cancel = %v selected %v", lm.screen, lm.rows[0].selected)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "good", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("the canceled apply wrote the file: %v", err)
	}

	// The confirmation applies: the summary is seen before the results.
	m = drive(t, m, "a", "enter")
	rm := m.(*model)
	if rm.screen != screenResults || !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("confirmed apply:\n%s", rm.lastResults)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("the confirmed apply wrote no file: %v", err)
	}

	// The removal marked with d joins the summary too: its action and reason
	// show before anything is deleted, and esc keeps the item in place.
	m = drive(t, m, "enter", "d", "a")
	sm = m.(*model)
	if sm.screen != screenSummary {
		t.Fatalf("screen after removal apply = %v, want summary", sm.screen)
	}
	view = sm.View()
	for _, want := range []string{"removals:", "ex/good 1.0.0", "the owned file is removed unchanged"} {
		if !strings.Contains(view, want) {
			t.Errorf("removal summary misses %q:\n%s", want, view)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("the summary removed the file: %v", err)
	}
	m = drive(t, m, "esc")
	if m.(*model).screen != screenList {
		t.Fatalf("screen after cancel = %v, want list", m.(*model).screen)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("the canceled removal deleted the file: %v", err)
	}

	// Confirming the removal deletes the file.
	m = drive(t, m, "a", "enter")
	rm = m.(*model)
	if rm.screen != screenResults || !strings.Contains(rm.lastResults, "removed  ex/good") {
		t.Fatalf("confirmed removal:\n%s", rm.lastResults)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "good", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("the file survived the confirmed removal: %v", err)
	}
}

// TestApplySummaryExcluded shows an excluded item with its reason on the
// summary: a required variable answered with an empty string stays missing,
// so the planner excludes the item and the summary says why before the
// results repeat it (RFC 6.4, 10.2).
func TestApplySummaryExcluded(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// Only the varitem is selected: its required variable opens the form,
	// and the empty answer leaves the variable without a value.
	m := app.Initial(kitDir)
	m = drive(t, m, "enter", "down", "space", "a", "enter")
	sm := m.(*model)
	if sm.screen != screenSummary {
		t.Fatalf("screen after form = %v, want summary", sm.screen)
	}
	view := sm.View()
	for _, want := range []string{"excluded:", "ex/varitem", `required variable "p" has no value`} {
		if !strings.Contains(view, want) {
			t.Errorf("summary misses %q:\n%s", want, view)
		}
	}

	// The confirmation runs the apply: the results report the same exclusion
	// and nothing is written.
	m = drive(t, m, "enter")
	rm := m.(*model)
	if rm.screen != screenResults || !strings.Contains(rm.lastResults, "excluded ex/varitem") {
		t.Fatalf("confirmed apply:\n%s", rm.lastResults)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "varitem", "v.md")); !os.IsNotExist(err) {
		t.Errorf("the excluded item wrote a file: %v", err)
	}
}
