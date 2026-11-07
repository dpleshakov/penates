package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/muesli/termenv"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// The main screen tests: the flows the old screens tested, ported to the
// new model, plus the capabilities the flip adds — the first run, the
// major-update gate, the breakpoints, and the details view. Views are
// asserted under the forced NO_COLOR profile, so they hold bare glyphs.

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdn":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
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

// resize lays the model out at a size, with the color profile forced to
// NO_COLOR so view assertions hold bare glyphs.
func resize(t *testing.T, m tea.Model, w, h int) tea.Model {
	t.Helper()
	forceVisual(t, termenv.Ascii, true)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// itemKeys lists the item keys of the built tree in row order.
func itemKeys(m *model) []string {
	var out []string
	for _, r := range m.rows {
		if r.Kind == KindItem {
			out = append(out, r.Item.Key)
		}
	}
	return out
}

// itemRow finds an item row of the built tree by its key.
func itemRow(m *model, key string) ItemRow {
	for _, r := range m.rows {
		if r.Kind == KindItem && r.Item.Key == key {
			return r.Item
		}
	}
	return ItemRow{}
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

// writeChangelogKit writes a kit with one item whose changelog holds
// enough entries to scroll the details view.
func writeChangelogKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	var entries strings.Builder
	for i := 0; i < 30; i++ {
		entries.WriteString("## 1.0." + string(rune('a'+i)) + "\n\nnote " + string(rune('a'+i)) + "\n\n")
	}
	files := map[string]string{
		"kit.yaml":                "name: ex\ntarget_hint: " + target + "\n",
		"items/long/item.yaml":    "id: long\nversion: 1.0.0\ndescription: Long changelog\ncontributes:\n  - file: l.md\n    to: skills/long/l.md\n",
		"items/long/l.md":         "long\n",
		"items/long/CHANGELOG.md": entries.String(),
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

// writeTeamKit writes a second kit for the same target, so the tree shows
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

func TestRegisterListApplyFlow(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// The kit argument opens the add-kit input with the path filled in.
	m := resize(t, app.Initial(kitDir), 100, 30)
	lm := m.(*model)
	if lm.inputKind != inputAddKit || lm.pathInput.Value() != kitDir {
		t.Fatalf("input %v, path %q; want the add-kit input prefilled", lm.inputKind, lm.pathInput.Value())
	}

	// Enter opens the first-target input of 6.4 — the kit proposes a
	// target and none is registered — and the second registers it and
	// attaches; the tree shows both items, none installed.
	m = drive(t, m, "enter", "enter")
	lm = m.(*model)
	if lm.inputKind != inputNone || lm.target != target {
		t.Fatalf("target = %q, input %v; want %q attached", lm.target, lm.inputKind, target)
	}
	if keys := itemKeys(lm); len(keys) != 2 || keys[0] != "ex/good" || keys[1] != "ex/varitem" {
		t.Fatalf("items = %v", keys)
	}
	if r := itemRow(lm, "ex/good"); r.Installed || r.Status != StatusAvailable {
		t.Fatalf("fresh good row = %+v", r)
	}

	// Select the good item, then the varitem: its selection opens the
	// variable form at selection time (7.3), with the required field
	// prefilled honestly empty.
	m = drive(t, m, "down", "space", "down", "space")
	fm := m.(*model)
	if !fm.formOpen || fm.screen != screenList {
		t.Fatalf("screen = %v, form open = %v; want the form open on the list", fm.screen, fm.formOpen)
	}
	if len(fm.formFields) != 1 || fm.formFields[0].varID != "p" || fm.formFields[0].prompt != "pick a word" || !fm.formFields[0].required {
		t.Fatalf("form fields = %+v", fm.formFields)
	}

	// The answer lands in the pending answers, and the apply goes straight
	// to the summary: nothing asks for variables at apply time. Nothing is
	// written until the summary is confirmed.
	m = drive(t, m, "answered", "enter", "a")
	sm := m.(*model)
	if sm.screen != screenSummary {
		t.Fatalf("screen after form = %v, want summary", sm.screen)
	}
	if sm.formOpen {
		t.Fatal("the form stayed open after enter accepted the answers")
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

	// Back to the list: the applied items hold their selection, and applying
	// the same selection again reuses the recorded answers — no form, straight
	// to the summary that keeps everything, then the results.
	m = drive(t, m, "enter")
	lm = m.(*model)
	good, varitem := itemRow(lm, "ex/good"), itemRow(lm, "ex/varitem")
	if !good.Installed || good.Pending == nil || *good.Pending != PendingApply || !varitem.Installed {
		t.Errorf("after apply: good %+v varitem %+v", good, varitem)
	}
	if n := lm.pendingCount(); n != 2 {
		t.Errorf("pending count = %d, want 2", n)
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

// TestAddKitInputEditing checks that the add-kit input accepts editing: clear
// the prefilled path, type another kit's path, and confirm — the typed kit
// attaches at its own target hint.
func TestAddKitInputEditing(t *testing.T) {
	dataDir := t.TempDir()
	kitDir := writeFixtureKit(t, filepath.Join(t.TempDir(), "hinted"))
	chosenHint := filepath.Join(t.TempDir(), "chosen")
	otherKit := writeFixtureKit(t, chosenHint)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(otherKit)})
	// The first enter opens the first-target input of 6.4 — the typed
	// kit's hint prefills it — the second registers and attaches.
	m = drive(t, m, "enter", "enter")

	lm := m.(*model)
	st, err := stateStore.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Kits["ex"].Dir; got != otherKit {
		t.Errorf("registered kit dir = %q, want the typed %q", got, otherKit)
	}
	if lm.target != chosenHint {
		t.Errorf("target = %q, want the typed kit's hint %q", lm.target, chosenHint)
	}
}

func TestUpdateAndResetFlow(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("first apply:\n%s", rm.lastResults)
	}

	// The kit publishes 1.1.0 with new content: the tree shows the update.
	bumpKit(t, kitDir, "1.1.0", "good skill v2\n")
	m = resize(t, app.Initial(""), 100, 30)
	lm := m.(*model)
	r := itemRow(lm, "ex/good")
	if r.InstalledVersion != "1.0.0" || r.KitVersion != "1.1.0" || r.Status != StatusUpdate {
		t.Fatalf("update row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "1.0.0 → 1.1.0") {
		t.Errorf("no update arrow:\n%s", view)
	}

	// The held selection applies the update.
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
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/good"); r.Status != StatusDowngrade {
		t.Fatalf("downgrade row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "1.1.0 → 0.9.0") {
		t.Errorf("no downgrade arrow:\n%s", view)
	}

	// The user modifies the installed file while the kit content is
	// unchanged; a fresh run applies nothing and keeps the modification.
	bumpKit(t, kitDir, "1.1.0", "good skill v2\n")
	if err := os.WriteFile(filepath.Join(target, "skills", "good", "SKILL.md"), []byte("user change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/good"); r.Status != StatusModified || r.Pending == nil {
		t.Fatalf("modified row = %+v", r)
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
	m = drive(t, m, "enter", "down", "r", "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.1.0") {
		t.Fatalf("reset apply:\n%s", rm.lastResults)
	}
	data, err = os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(data) != "good skill v2\n" {
		t.Fatalf("reset file = %q, %v; want the kit content back", data, err)
	}
}

func TestRemoveFlow(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// Back to the list: the installed row takes the removal mark, and the
	// apply removes it.
	m = drive(t, m, "enter", "down", "d")
	lm := m.(*model)
	r := itemRow(lm, "ex/good")
	if !r.Installed || r.Pending == nil || *r.Pending != PendingRemove {
		t.Fatalf("row after d = %+v, want installed and marked", r)
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

	// The tree no longer offers the item as installed, and d on it does
	// nothing.
	m = drive(t, m, "enter", "down")
	lm = m.(*model)
	r = itemRow(lm, "ex/good")
	if r.Installed || r.Pending != nil {
		t.Fatalf("row after removal = %+v", r)
	}
	m = drive(t, m, "d")
	if r := itemRow(m.(*model), "ex/good"); r.Pending != nil {
		t.Error("d marked a row that is not installed")
	}
}

func TestRemoveReportsKeptContent(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// The user edits the installed file; the removal keeps it and says so.
	if err := os.WriteFile(filepath.Join(target, "skills", "good", "SKILL.md"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = drive(t, m, "enter", "down", "d", "a", "enter")
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

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter")
	lm := m.(*model)
	if keys := itemKeys(lm); len(keys) != 3 || keys[2] != "gone/old" {
		t.Fatalf("items = %v", keys)
	}
	orphan := itemRow(lm, "gone/old")
	if !orphan.Installed || orphan.Status != StatusOrphan {
		t.Fatalf("orphan row = %+v", orphan)
	}

	// The orphan removes from the lock alone, without any kit: its row sits
	// in the orphans group at the end of the tree.
	m = drive(t, m, "down", "down", "down", "down", "d", "a", "enter")
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

func TestRemoveThenInstallSameFile(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeSectionKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/one 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// Mark the installed item for removal and select another writer of the
	// same shared file: one apply must land the target on two's block alone.
	m = drive(t, m, "enter", "down", "down", "space", "up", "d", "a", "enter")
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

func TestRemoveWithFormResume(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeSectionKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/one 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// The selection of the varitem opens the form; nothing is planned or
	// written while it waits, and the removal of the marked item waits for
	// the summary's confirmation.
	m = drive(t, m, "enter", "down", "down", "down", "space")
	fm := m.(*model)
	if !fm.formOpen {
		t.Fatal("the selection of an item with variables opened no form")
	}
	if _, err := os.Stat(filepath.Join(target, "AGENTS.md")); err != nil {
		t.Fatalf("the selection ran before the form: %v", err)
	}

	// The answer lands in the pending answers; marking the removal and
	// applying goes straight to the summary, and it lists the removal and
	// the install together.
	m = drive(t, m, "answered", "enter", "up", "up", "d", "a")
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

	// The tree drops every mark of the removed row.
	m = drive(t, m, "enter")
	lm := m.(*model)
	if r := itemRow(lm, "ex/one"); r.Installed || r.Pending != nil {
		t.Errorf("row of the removed item = %+v", r)
	}
}

func TestListFilter(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFilterKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter")
	lm := m.(*model)
	if keys := itemKeys(lm); len(keys) != 3 || keys[0] != "ex/alpha" || keys[2] != "ex/gamma" {
		t.Fatalf("items = %v", keys)
	}

	// The filter matches a description and hides the rest; the kit row stays
	// with its match counts (7.5).
	m = drive(t, m, "/", "review")
	lm = m.(*model)
	if !lm.filtering {
		t.Fatal("typing / did not open the filter")
	}
	if keys := itemKeys(lm); len(keys) != 1 || keys[0] != "ex/beta" {
		t.Fatalf("items after 'review' = %v", keys)
	}
	if g := lm.rows[0].Kit; !g.Filtering || g.Matches != 1 || g.Total != 3 {
		t.Fatalf("kit row counts = %+v", g)
	}
	if view := lm.View(); !strings.Contains(view, "beta") || strings.Contains(view, "alpha") || strings.Contains(view, "gamma") {
		t.Errorf("the tree is not narrowed to beta:\n%s", view)
	}

	// esc clears the filter and shows everything again.
	m = drive(t, m, "esc")
	lm = m.(*model)
	if lm.filtering || lm.filter.Value() != "" {
		t.Fatalf("esc left the filter open or set: %v %q", lm.filtering, lm.filter.Value())
	}
	if keys := itemKeys(lm); len(keys) != 3 {
		t.Fatalf("items after esc = %v", keys)
	}

	// A tag matches too.
	m = drive(t, m, "/", "opencode-v2")
	lm = m.(*model)
	if keys := itemKeys(lm); len(keys) != 1 || keys[0] != "ex/alpha" {
		t.Fatalf("items after 'opencode-v2' = %v", keys)
	}

	// enter closes the filter and keeps it applied.
	m = drive(t, m, "enter")
	lm = m.(*model)
	if lm.filtering || lm.filter.Value() != "opencode-v2" || len(itemKeys(lm)) != 1 {
		t.Fatalf("after enter: filtering %v, filter %q, items %v", lm.filtering, lm.filter.Value(), itemKeys(lm))
	}

	// No match: only the add-kit row remains, and acting on it changes
	// nothing.
	m = drive(t, m, "/", "zzz", "space", "down", "i")
	lm = m.(*model)
	if len(lm.rows) != 1 || lm.rows[0].Kind != KindAddKit || lm.screen != screenList {
		t.Fatalf("after no-match: rows %v, screen %v", lm.rows, lm.screen)
	}

	// With the filter applied, selection and apply still act on the row the
	// filter shows.
	m = drive(t, m, "esc", "/", "beta", "enter", "down", "space", "a", "enter")
	rm := m.(*model)
	if rm.screen != screenResults || !strings.Contains(rm.lastResults, "applied  ex/beta 1.0.0") {
		t.Fatalf("filtered apply:\n%s", rm.lastResults)
	}
	if strings.Contains(rm.lastResults, "ex/alpha") || strings.Contains(rm.lastResults, "ex/gamma") {
		t.Errorf("the filter leaked into the selection:\n%s", rm.lastResults)
	}
}

func TestItemDetailsView(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// Install both items so the answers of varitem are recorded: the form
	// asks at selection now, the apply goes straight through the summary.
	// The drive ends on the results screen.
	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "down", "space", "answered", "enter", "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/varitem") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}

	// enter opens the details view of the row under the cursor; the first
	// item carries a changelog and tags.
	m = drive(t, m, "enter", "down", "enter")
	dm := m.(*model)
	if !dm.view || dm.viewKit {
		t.Fatalf("view after enter = %v %v, want the item view open", dm.view, dm.viewKit)
	}
	view := dm.View()
	for _, want := range []string{"good · ex", "Good item", "Tags", "writing", "Contributes", "skills/good/SKILL.md", "Changelog", "first release"} {
		if !strings.Contains(view, want) {
			t.Errorf("details miss %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Variables") {
		t.Errorf("details invent variables for an item without them:\n%s", view)
	}

	// esc returns to the tree; the second item shows its templated
	// contribution and the answer the installation recorded.
	m = drive(t, m, "esc", "down", "enter")
	dm = m.(*model)
	if !dm.view {
		t.Fatalf("view after esc, down, enter = %v, want the item view open", dm.view)
	}
	view = dm.View()
	for _, want := range []string{"varitem · ex", "Var item", "skills/varitem/v.md", "Variables", "p = answered"} {
		if !strings.Contains(view, want) {
			t.Errorf("details miss %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Changelog") {
		t.Errorf("details show a changelog for an item without one:\n%s", view)
	}

	// esc brings the tree back.
	m = drive(t, m, "esc")
	if m.(*model).view {
		t.Fatal("esc did not close the view")
	}
}

func TestListStatuses(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "target")
	kitDir := writeStatusKit(t, targetDir)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// Nothing is installed: both rows show the available glyph.
	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter")
	lm := m.(*model)
	if keys := itemKeys(lm); len(keys) != 2 || keys[0] != "ex/dup" || keys[1] != "ex/plain" {
		t.Fatalf("items = %v", keys)
	}
	if r := itemRow(lm, "ex/dup"); r.Status != StatusAvailable {
		t.Fatalf("fresh dup row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "○") {
		t.Errorf("the fresh tree shows no available glyph:\n%s", view)
	}

	// Selecting sets the pending action.
	m = drive(t, m, "down", "down", "space")
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Pending == nil || *r.Pending != PendingApply {
		t.Fatalf("selected row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "✓") {
		t.Errorf("no pending mark:\n%s", view)
	}

	// Applying makes the item installed.
	m = drive(t, m, "a", "enter")
	rm := m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/plain 1.0.0") {
		t.Fatalf("install:\n%s", rm.lastResults)
	}
	m = drive(t, m, "enter")
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Status != StatusInstalled {
		t.Fatalf("installed row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "●") {
		t.Errorf("no installed glyph:\n%s", view)
	}

	// A higher kit version is an update, with the arrow in the version
	// column.
	bumpStatusKit(t, kitDir, "1.1.0", "plain v2\n")
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Status != StatusUpdate {
		t.Fatalf("update row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "1.0.0 → 1.1.0") {
		t.Errorf("no update marker or arrow:\n%s", view)
	}

	// A lower kit version is a downgrade.
	bumpStatusKit(t, kitDir, "0.9.0", "plain v2\n")
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Status != StatusDowngrade {
		t.Fatalf("downgrade row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "1.0.0 → 0.9.0") {
		t.Errorf("no downgrade marker or arrow:\n%s", view)
	}

	// A major bump is marked for review: the row carries the major flag
	// (7.2).
	bumpStatusKit(t, kitDir, "2.0.0", "plain v2\n")
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Status != StatusUpdate || !r.Major {
		t.Fatalf("major row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "1.0.0 → 2.0.0") {
		t.Errorf("no major update arrow:\n%s", view)
	}

	// The same version with changed content warns while the item is
	// selected: applying would keep everything.
	bumpStatusKit(t, kitDir, "1.0.0", "plain v2 changed\n")
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Status != StatusWarning {
		t.Fatalf("warning row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "△") {
		t.Errorf("no warning glyph:\n%s", view)
	}

	// With the kit content back to the installed one, a user edit shows as
	// modified.
	bumpStatusKit(t, kitDir, "1.0.0", "plain v1\n")
	if err := os.WriteFile(filepath.Join(targetDir, "skills", "plain", "SKILL.md"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Status != StatusModified {
		t.Fatalf("modified row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "~") {
		t.Errorf("no modified glyph:\n%s", view)
	}

	// Losing the owned file shows broken.
	if err := os.Remove(filepath.Join(targetDir, "skills", "plain", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Status != StatusBroken {
		t.Fatalf("broken row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "!") {
		t.Errorf("no broken glyph:\n%s", view)
	}

	// The held selection repairs the broken item; the tree shows installed
	// again.
	m = drive(t, m, "a", "enter")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/plain 1.0.0") {
		t.Fatalf("repair:\n%s", rm.lastResults)
	}
	m = drive(t, m, "enter")
	lm = m.(*model)
	if r := itemRow(lm, "ex/plain"); r.Status != StatusInstalled {
		t.Fatalf("after repair the row = %+v", r)
	}

	// A heading the user wrote outside managed blocks conflicts with the
	// section item as soon as it is selected, and the reason shows in the
	// row; deselecting clears the conflict. The tree re-reads the target on
	// entry, so the file lands before the selection is toggled.
	if err := os.WriteFile(filepath.Join(targetDir, "AGENTS.md"), []byte("notes\n\n## Dup rules\n\nhand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = resize(t, app.Initial(""), 100, 30)
	m = drive(t, m, "down", "space")
	lm = m.(*model)
	if r := itemRow(lm, "ex/dup"); r.Status != StatusConflict {
		t.Fatalf("conflict row = %+v", r)
	}
	view := lm.View()
	if !strings.Contains(view, "✗") || !strings.Contains(view, "already has a heading") {
		t.Errorf("no conflict glyph or reason:\n%s", view)
	}
	m = drive(t, m, "space")
	lm = m.(*model)
	if r := itemRow(lm, "ex/dup"); r.Status != StatusAvailable || r.Pending != nil {
		t.Fatalf("deselected conflict row = %+v", r)
	}

	// An installed item whose kit is not attached is an orphan, in the
	// orphans group at the end of the tree.
	locks := lock.NewStore(dataDir)
	lk, err := locks.Load(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	lk.SetItem("gone/old", &lock.Item{Version: "0.2.0"})
	if err := locks.Save(lk); err != nil {
		t.Fatal(err)
	}
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if keys := itemKeys(lm); len(keys) != 3 || keys[2] != "gone/old" {
		t.Fatalf("items = %v", keys)
	}
	if r := itemRow(lm, "gone/old"); r.Status != StatusOrphan {
		t.Fatalf("orphan row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "orphans") {
		t.Errorf("no orphans group:\n%s", view)
	}

	// When the kit stops loading, its installed items freeze as kit
	// unavailable under the kit's collapsed row; expanding it shows them
	// next to the healthy kit's items.
	if err := os.Remove(filepath.Join(kitDir, "kit.yaml")); err != nil {
		t.Fatal(err)
	}
	teamDir := writeTeamKit(t, targetDir)
	m = resize(t, app.Initial(teamDir), 100, 30)
	if m.(*model).inputKind != inputAddKit {
		t.Fatalf("input = %v, want the add-kit input for the new kit", m.(*model).inputKind)
	}
	m = drive(t, m, "enter")
	lm = m.(*model)
	// The orphans group of the previous step still holds gone/old; the
	// unreadable kit sits collapsed above it.
	if keys := itemKeys(lm); len(keys) != 2 || keys[0] != "team/titem" || keys[1] != "gone/old" {
		t.Fatalf("items of the collapsed unreadable kit = %v", keys)
	}
	m = drive(t, m, "space")
	lm = m.(*model)
	if keys := itemKeys(lm); len(keys) != 3 || keys[0] != "ex/plain" {
		t.Fatalf("items after expanding the unreadable kit = %v", keys)
	}
	if r := itemRow(lm, "ex/plain"); r.Status != StatusUnavailable {
		t.Fatalf("unavailable row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "unreadable") {
		t.Errorf("the unreadable kit lost its mark:\n%s", view)
	}
}

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

	m := resize(t, app.Initial(dirA), 100, 30)
	m = drive(t, m, "enter", "enter")
	lm := m.(*model)
	if lm.rows[0].Kind != KindKit || lm.rows[0].Kit.Name != "aaa" || lm.rows[0].Kit.Path != dirA {
		t.Fatalf("first group = %+v", lm.rows[0])
	}

	// Attaching the second kit lands on the same target: two groups.
	m = drive(t, m, "end", "enter")
	m = resize(t, m, 100, 30)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(dirB)})
	m = drive(t, m, "enter")
	lm = m.(*model)
	if lm.rows[0].Kit.Name != "aaa" || lm.rows[2].Kit.Name != "bbb" || lm.rows[2].Kit.Path != dirB {
		t.Fatalf("groups = %+v", lm.rows)
	}
	if keys := itemKeys(lm); len(keys) != 2 || keys[0] != "aaa/aone" || keys[1] != "bbb/bone" {
		t.Fatalf("items = %v", keys)
	}

	// A filter that nothing in aaa matches hides aaa's header as well.
	m = drive(t, m, "/", "bone")
	lm = m.(*model)
	if lm.rows[0].Kit.Name != "bbb" {
		t.Fatalf("a filter did not hide the empty group: %+v", lm.rows)
	}
	if keys := itemKeys(lm); len(keys) != 1 || keys[0] != "bbb/bone" {
		t.Fatalf("filtered items = %v", keys)
	}
}

func TestReconciliationFlow(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, targetDir)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a", "enter")
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
	m = resize(t, app.Initial(""), 100, 30)
	lm := m.(*model)
	if r := itemRow(lm, "ex/good"); r.Status != StatusBroken {
		t.Fatalf("row after reconciliation = %+v, want broken", r)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("the staged file survived the reconciliation: %v", err)
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

	// The user modifies the installed file: the tree shows modified, and the
	// apply keeps the content.
	if err := os.WriteFile(filepath.Join(targetDir, "skills", "good", "SKILL.md"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = resize(t, app.Initial(""), 100, 30)
	lm = m.(*model)
	if r := itemRow(lm, "ex/good"); r.Status != StatusModified {
		t.Fatalf("row after reconciliation = %+v, want modified", r)
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

func TestApplySummary(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a")
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

	// esc cancels: back to the tree with the selection held, nothing applied.
	m = drive(t, m, "esc")
	lm := m.(*model)
	if lm.screen != screenList || itemRow(lm, "ex/good").Pending == nil {
		t.Fatalf("screen after cancel = %v, pending %+v", lm.screen, itemRow(lm, "ex/good").Pending)
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
	m = drive(t, m, "enter", "down", "d", "a")
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
		t.Fatalf("screen after cancel = %v, want tree", m.(*model).screen)
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

func TestApplySummaryExcluded(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// A selection whose required answers are missing — one restored from an
	// earlier session, say — plans as excluded: nothing asks for variables
	// at apply time (7.3).
	// The target must be registered before the pending actions seed: the
	// kit attaches, then the selection without answers goes into the state.
	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter")
	if err := stateStore.Update(func(reg *state.Registry) error {
		reg.Pending[target] = &state.Pending{Items: map[string]*state.ItemPending{
			"ex/varitem": {Select: true},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m = resize(t, app.Initial(""), 100, 30)
	m = drive(t, m, "a")
	sm := m.(*model)
	if sm.screen != screenSummary {
		t.Fatalf("screen after apply = %v, want summary without a form", sm.screen)
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

func TestApplySummaryConflictRenders(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	// The target already holds the file the kit would install: the planner
	// refuses to touch it as possible user content.
	if err := os.MkdirAll(filepath.Join(target, "skills", "good"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "skills", "good", "SKILL.md"), []byte("user's own\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// The summary spells the conflict before anything is written; the test
	// runs wide so the long conflict line renders uncut.
	m := resize(t, app.Initial(kitDir), 200, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a")
	sm := m.(*model)
	if sm.screen != screenSummary {
		t.Fatalf("screen after apply = %v, want summary", sm.screen)
	}
	view := sm.View()
	want := "skills/good/SKILL.md exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user"
	for _, part := range []string{"excluded:", "ex/good", want} {
		if !strings.Contains(view, part) {
			t.Errorf("summary misses %q:\n%s", part, view)
		}
	}

	// The confirmation runs the apply: the results repeat the same line, and
	// nothing is written.
	m = drive(t, m, "enter")
	rm := m.(*model)
	if rm.screen != screenResults || !strings.Contains(rm.lastResults, want) {
		t.Fatalf("confirmed apply:\n%s", rm.lastResults)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("the user's file did not survive: %v", err)
	}
}

func TestFirstRun(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// No target, no kit: the tree holds the add-kit row alone, the body
	// explains what a kit is, the footer names enter (6.1).
	m := resize(t, app.Initial(""), 100, 30)
	lm := m.(*model)
	if len(lm.rows) != 1 || lm.rows[0].Kind != KindAddKit {
		t.Fatalf("rows = %v, want the add-kit row alone", lm.rows)
	}
	view := lm.View()
	for _, want := range []string{"No kits are registered yet.", "no target", "enter add kit"} {
		if !strings.Contains(view, want) {
			t.Errorf("first run misses %q:\n%s", want, view)
		}
	}

	// enter opens the input; the path opens the first-target input of 6.4,
	// the second enter registers and attaches, and the target becomes the
	// last active one (11).
	m = drive(t, m, "enter")
	lm = m.(*model)
	if lm.inputKind != inputAddKit || lm.pathInput.Value() != "" {
		t.Fatalf("input %v, path %q; want the empty input open", lm.inputKind, lm.pathInput.Value())
	}
	m = drive(t, m, kitDir, "enter", "enter")
	lm = m.(*model)
	if lm.inputKind != inputNone || lm.target != target {
		t.Fatalf("after attach: target %q, input %v; want %q", lm.target, lm.inputKind, target)
	}
	st, err := stateStore.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if st.LastTarget != target {
		t.Errorf("last active target = %q, want %q", st.LastTarget, target)
	}
	if keys := itemKeys(lm); len(keys) != 2 {
		t.Fatalf("items after attach = %v", keys)
	}

	// A restart opens the remembered target directly.
	m = resize(t, app.Initial(""), 100, 30)
	if m.(*model).target != target {
		t.Errorf("restart target = %q, want %q", m.(*model).target, target)
	}
}

func TestMajorGate(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "space", "a", "enter")
	if !strings.Contains(m.(*model).lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("install:\n%s", m.(*model).lastResults)
	}

	// The kit publishes 2.0.0: a major update (7.2).
	bumpKit(t, kitDir, "2.0.0", "good skill v2\n")
	m = resize(t, app.Initial(""), 100, 30)
	lm := m.(*model)
	if r := itemRow(lm, "ex/good"); r.Status != StatusUpdate || !r.Major {
		t.Fatalf("major row = %+v", r)
	}

	// The applied selection is still held; space unselects it first.
	m = drive(t, m, "down", "space")
	if n := m.(*model).pendingCount(); n != 0 {
		t.Fatalf("space did not unselect the held selection: %d pending", n)
	}

	// The first space on the row opens the details with the review note;
	// nothing is selected.
	m = drive(t, m, "space")
	lm = m.(*model)
	if !lm.view {
		t.Fatal("the first space on a major update did not open the details")
	}
	if n := lm.pendingCount(); n != 0 {
		t.Fatalf("the first space selected the update: %d pending", n)
	}
	view := lm.View()
	if !strings.Contains(view, "A major version changes behavior") || !strings.Contains(view, "space select") {
		t.Errorf("the view misses the review note or its key:\n%s", view)
	}

	// The second space — in the view — selects the update.
	m = drive(t, m, "space")
	lm = m.(*model)
	if n := lm.pendingCount(); n != 1 {
		t.Fatalf("the second space did not select: %d pending", n)
	}
	if r := itemRow(lm, "ex/good"); r.Pending == nil || *r.Pending != PendingApply {
		t.Fatalf("selected row = %+v", r)
	}

	// esc closes the view; space on the row unselects.
	m = drive(t, m, "esc", "space")
	lm = m.(*model)
	if lm.view {
		t.Fatal("esc did not close the view")
	}
	if n := lm.pendingCount(); n != 0 {
		t.Fatalf("space did not unselect: %d pending", n)
	}

	// Reviewed already: the next space on the row selects without the view.
	m = drive(t, m, "space")
	lm = m.(*model)
	if lm.view {
		t.Fatal("a reviewed major update opened the view again")
	}
	if n := lm.pendingCount(); n != 1 {
		t.Fatalf("space on the reviewed row did not select: %d pending", n)
	}
}

func TestBreakpoints(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFilterKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 120, 32)
	m = drive(t, m, "enter", "enter", "down")
	lm := m.(*model)

	// Wide: the details pane of the row under the cursor sits beside the
	// tree, behind the separator (2.2).
	view := lm.View()
	if !strings.Contains(view, "│") || !strings.Contains(view, "Contributes") || !strings.Contains(view, "alpha") {
		t.Errorf("the wide layout misses the pane or the tree:\n%s", view)
	}

	// Medium: the tree keeps the description column.
	m = resize(t, m, 90, 24)
	if view := lm.View(); !strings.Contains(view, "Marks finished sessions") {
		t.Errorf("the medium layout lost the description column:\n%s", view)
	}

	// Narrow: tags and descriptions drop out.
	m = resize(t, m, 64, 20)
	view = lm.View()
	if strings.Contains(view, "Marks finished sessions") || !strings.Contains(view, "alpha") {
		t.Errorf("the narrow layout did not drop the description column:\n%s", view)
	}

	// Below the minimum: the message states the size (4.4).
	m = resize(t, m, 50, 10)
	if view := lm.View(); !strings.Contains(view, "penates needs at least") || !strings.Contains(view, "50 × 10") {
		t.Errorf("no minimum-size message:\n%s", view)
	}
}

func TestDetailsViewScroll(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeChangelogKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = drive(t, m, "enter", "enter", "down", "enter")
	lm := m.(*model)
	if !lm.view {
		t.Fatal("enter did not open the view")
	}
	if lm.viewOffset != 0 {
		t.Fatalf("the view opened scrolled: %d", lm.viewOffset)
	}
	page := lm.viewportHeight()
	if len(lm.viewLines) <= page {
		t.Fatalf("the changelog fits: %d lines, page %d", len(lm.viewLines), page)
	}
	top := lm.View()

	// pgdn pages the view forward as far as the content goes, pgup back to
	// the top (5).
	m = drive(t, m, "pgdn")
	if lm := m.(*model); lm.viewOffset <= 0 {
		t.Fatalf("after pgdn: offset %d, want a forward window", lm.viewOffset)
	}
	if paged := m.(*model).View(); paged == top {
		t.Error("pgdn did not change the rendered view")
	}
	m = drive(t, m, "pgup")
	if lm := m.(*model); lm.viewOffset != 0 {
		t.Fatalf("after pgup: offset %d, want 0", lm.viewOffset)
	}
	if m.(*model).View() != top {
		t.Error("pgup did not return to the top of the view")
	}
}

func TestHeaderFooter(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 120, 30)
	m = drive(t, m, "enter", "enter", "down", "space")
	lm := m.(*model)

	// The header names the target, the pending count, and the help hint
	// (2.1).
	view := lm.View()
	if !strings.Contains(view, target) || !strings.Contains(view, "1 pending") || !strings.Contains(view, "? help") {
		t.Errorf("header misses target, pending count, or help hint:\n%s", view)
	}

	// The footer of the selected row names the unselect key (7.1).
	if view := lm.View(); !strings.Contains(view, "space unselect · enter details") {
		t.Errorf("the selected footer misses the unselect key:\n%s", view)
	}

	// The kit row collapses; the add-kit row adds (12.2, 12.4).
	m = drive(t, m, "up")
	if view := m.(*model).View(); !strings.Contains(view, "space collapse") {
		t.Errorf("the kit footer misses the collapse key:\n%s", view)
	}
	m = drive(t, m, "end")
	if view := m.(*model).View(); !strings.Contains(view, "enter add kit") {
		t.Errorf("the add-kit footer misses its key:\n%s", view)
	}

	// A modified installed item names the reset key (7.1, 7.6).
	m = drive(t, m, "a", "enter")
	if !strings.Contains(m.(*model).lastResults, "applied  ex/good 1.0.0") {
		t.Fatalf("apply:\n%s", m.(*model).lastResults)
	}
	if err := os.WriteFile(filepath.Join(target, "skills", "good", "SKILL.md"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = resize(t, app.Initial(""), 120, 30)
	m = drive(t, m, "down")
	lm = m.(*model)
	if r := itemRow(lm, "ex/good"); r.Status != StatusModified {
		t.Fatalf("modified row = %+v", r)
	}
	if view := lm.View(); !strings.Contains(view, "r reset") || !strings.Contains(view, "d remove") {
		t.Errorf("the modified footer misses d or r:\n%s", view)
	}
}
