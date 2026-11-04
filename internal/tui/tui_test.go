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

	// Select both items and apply: the good one installs, the one with variables is
	// excluded as not supported yet.
	m = drive(t, m, "space", "down", "space", "a")
	rm := m.(*model)
	if rm.screen != screenResults {
		t.Fatalf("screen after apply = %v, want results", rm.screen)
	}
	if !strings.Contains(rm.lastResults, "applied  ex/good 1.0.0") {
		t.Errorf("results miss the applied item:\n%s", rm.lastResults)
	}
	if !strings.Contains(rm.lastResults, "excluded ex/varitem") || !strings.Contains(rm.lastResults, "variables are not supported yet") {
		t.Errorf("results miss the excluded item:\n%s", rm.lastResults)
	}

	installed, err := os.ReadFile(filepath.Join(target, "skills", "good", "SKILL.md"))
	if err != nil || string(installed) != "good skill\n" {
		t.Errorf("installed file = %q, %v", installed, err)
	}
	lk, err := lock.NewStore(dataDir).Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if lk.Items["ex/good"] == nil {
		t.Errorf("lock items = %v", lk.Items)
	}

	// Back to the list: the applied item is now marked installed, and applying the
	// same selection again keeps it.
	m = drive(t, m, "enter")
	lm = m.(*model)
	if !lm.rows[0].installed || !lm.rows[0].selected {
		t.Errorf("after apply: %+v", lm.rows[0])
	}
	m = drive(t, m, "a")
	rm = m.(*model)
	if !strings.Contains(rm.lastResults, "kept     ex/good: already installed") {
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

// writeFixtureKit writes a kit with one installable item and one that uses
// variables, proposing target as its target hint.
func writeFixtureKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"kit.yaml":                  "name: ex\ntarget_hint: " + target + "\n",
		"items/good/item.yaml":      "id: good\nversion: 1.0.0\ndescription: Good item\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/good/SKILL.md\n",
		"items/good/skill/SKILL.md": "good skill\n",
		"items/varitem/item.yaml":   "id: varitem\nversion: 0.1.0\nvars:\n  - {id: p, required: true}\ncontributes:\n  - file: v.md\n    to: skills/varitem/v.md\n",
		"items/varitem/v.md":        "var item\n",
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
