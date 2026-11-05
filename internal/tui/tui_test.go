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

	// The answer resumes the apply: both items install, the templated file
	// carries the substitution, and the lock records the answers.
	m = drive(t, m, "answered", "enter")
	rm := m.(*model)
	if rm.screen != screenResults {
		t.Fatalf("screen after form = %v, want results", rm.screen)
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
	// to the results that keep everything.
	m = drive(t, m, "enter")
	lm = m.(*model)
	if !lm.rows[0].installed || !lm.rows[0].selected || !lm.rows[1].installed {
		t.Errorf("after apply: %+v %+v", lm.rows[0], lm.rows[1])
	}
	m = drive(t, m, "a")
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

// writeFixtureKit writes a kit with one installable item and one that uses a
// required variable in a templated file, proposing target as its target hint.
func writeFixtureKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"kit.yaml":                  "name: ex\ntarget_hint: " + target + "\n",
		"items/good/item.yaml":      "id: good\nversion: 1.0.0\ndescription: Good item\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/good/SKILL.md\n",
		"items/good/skill/SKILL.md": "good skill\n",
		"items/varitem/item.yaml":   "id: varitem\nversion: 0.1.0\nvars:\n  - {id: p, prompt: pick a word, required: true}\ncontributes:\n  - file: v.md\n    to: skills/varitem/v.md\n    template: true\n",
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
	m = drive(t, m, "enter", "space", "a")
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
	m = drive(t, m, "a")
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
	m = drive(t, m, "a")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "kept     ex/good: already installed") {
		t.Fatalf("apply over a user modification:\n%s", rm.lastResults)
	}
	data, err = os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(data) != "user change\n" {
		t.Fatalf("user modification overwritten without a reset: %q, %v", data, err)
	}

	// With the reset requested, the kit content replaces the user's change.
	m = drive(t, m, "enter", "r", "a")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.1.0") {
		t.Fatalf("reset apply:\n%s", rm.lastResults)
	}
	data, err = os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(data) != "good skill v2\n" {
		t.Fatalf("reset file = %q, %v; want the kit content back", data, err)
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
