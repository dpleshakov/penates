package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// The variable form of the selection (tui-screens.md 7.3): selecting an
// item with variables turns the details view (any width) or the details
// pane (wide) into a form, prefilled with the stored answers; enter is
// accepted only when every required field holds a value; esc clears the
// pending action. The answers land in the state pending, and nothing asks
// for variables at apply time.

// writeFormKit writes a kit with a plain item and one whose required and
// optional variables substitute into a templated file, proposing target as
// its target hint.
func writeFormKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"kit.yaml":                "name: ex\ntarget_hint: " + target + "\n",
		"items/plain/item.yaml":   "id: plain\nversion: 1.0.0\ndescription: Plain item\ncontributes:\n  - file: s.md\n    to: skills/plain/s.md\n",
		"items/plain/s.md":        "plain\n",
		"items/varitem/item.yaml": "id: varitem\nversion: 1.0.0\ndescription: Var item\nvars:\n  - {id: p, prompt: pick a word, required: true}\n  - {id: b, prompt: name the branch, required: false}\ncontributes:\n  - file: v.md\n    to: skills/varitem/v.md\n    template: true\n",
		"items/varitem/v.md":      "var {{p}} on {{b}}\n",
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

// bumpFormKit rewrites the var item's version and variables, the way a kit
// author publishes an update; extraVar names a new required variable the
// update introduces.
func bumpFormKit(t *testing.T, kitDir, version, extraVar string) {
	t.Helper()
	vars := "vars:\n  - {id: p, prompt: pick a word, required: true}\n  - {id: b, prompt: name the branch, required: false}\n"
	if extraVar != "" {
		vars += "  - {id: " + extraVar + ", prompt: extra question, required: true}\n"
	}
	itemYaml := "id: varitem\nversion: " + version + "\ndescription: Var item\n" + vars + "contributes:\n  - file: v.md\n    to: skills/varitem/v.md\n    template: true\n"
	if err := os.WriteFile(filepath.Join(kitDir, "items", "varitem", "item.yaml"), []byte(itemYaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

// openFormKit attaches the form kit's fixture and returns the model on the
// tree, the var item row under the cursor.
func openFormKit(t *testing.T, app *App, kitDir string, w, h int) *model {
	t.Helper()
	m := resize(t, app.Initial(kitDir), w, h)
	m = drive(t, m, "enter", "enter", "down", "down")
	return m.(*model)
}

func pendingAnswers(t *testing.T, st *state.Store, target, key string) map[string]string {
	t.Helper()
	reg, err := st.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	p := reg.Pending[target]
	if p == nil || p.Items[key] == nil {
		return nil
	}
	return p.Items[key].Answers
}

func TestSelectionFormView(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFormKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// Selecting the var item opens the form in the details view (7.3): two
	// fields in declaration order, prefilled honestly empty, the footer
	// naming the form keys.
	m := openFormKit(t, app, kitDir, 100, 30)
	m = drive(t, m, "space").(*model)
	if !m.formOpen || !m.view {
		t.Fatalf("form open = %v, view = %v; want the form in the view", m.formOpen, m.view)
	}
	if len(m.formFields) != 2 || m.formFields[0].varID != "p" || !m.formFields[0].required || m.formFields[1].varID != "b" || m.formFields[1].required {
		t.Fatalf("form fields = %+v", m.formFields)
	}
	if m.formAnswers["p"] != "" || m.formAnswers["b"] != "" {
		t.Fatalf("prefill = %v; want honest empty fields", m.formAnswers)
	}
	view := m.View()
	for _, want := range []string{"varitem · ex", "Variables", "pick a word", "required", "name the branch", formFooter} {
		if !strings.Contains(view, want) {
			t.Errorf("the form misses %q:\n%s", want, view)
		}
	}

	// The gate: enter without the required value keeps the form open and
	// stores no answers, while the selection stays held.
	m = drive(t, m, "enter").(*model)
	if !m.formOpen {
		t.Fatal("enter with an empty required field accepted the form")
	}
	if a := pendingAnswers(t, stateStore, target, "ex/varitem"); len(a) != 0 {
		t.Fatalf("the gate stored answers = %v", a)
	}

	// esc clears the pending action of the item entirely, and the view the
	// form opened for closes with it.
	m = drive(t, m, "esc").(*model)
	if m.formOpen || m.view {
		t.Fatalf("esc left the form %v or the view %v open", m.formOpen, m.view)
	}
	if n := m.pendingCount(); n != 0 {
		t.Fatalf("esc kept %d pending actions", n)
	}
	if p := m.pendingActions(); p != nil && len(p.Items) != 0 {
		t.Fatalf("esc left pending items = %v", p.Items)
	}

	// Selecting again opens the form; tab moves to the optional field and
	// back, and enter accepts only once every required field holds a value.
	m = drive(t, m, "space").(*model)
	if !m.formOpen {
		t.Fatal("re-selection opened no form")
	}
	m = drive(t, m, "answered", "tab", "dev", "tab", "enter").(*model)
	if m.formOpen || m.view {
		t.Fatalf("after accept: form %v, view %v; want both closed", m.formOpen, m.view)
	}
	answers := pendingAnswers(t, stateStore, target, "ex/varitem")
	if len(answers) != 2 || answers["p"] != "answered" || answers["b"] != "dev" {
		t.Fatalf("pending answers = %v; want p and b saved", answers)
	}

	// The apply reaches the summary without asking, and the summary plans
	// the templated content with the answers substituted in.
	m = drive(t, m, "a").(*model)
	if m.screen != screenSummary {
		t.Fatalf("screen after apply = %v, want summary", m.screen)
	}
	if view := m.View(); !strings.Contains(view, "ex/varitem 1.0.0") {
		t.Errorf("the summary misses the planned var item:\n%s", view)
	}
	m = drive(t, m, "enter").(*model)
	if !strings.Contains(m.lastResults, "applied  ex/varitem 1.0.0") {
		t.Fatalf("apply:\n%s", m.lastResults)
	}
	data, err := os.ReadFile(filepath.Join(target, "skills", "varitem", "v.md"))
	if err != nil || string(data) != "var answered on dev\n" {
		t.Errorf("templated file = %q, %v; want the substituted content", data, err)
	}

	// The answers the apply wrote into the lock stay with the held selection
	// after it settles; the update flow checks the form they prefill.
	m = drive(t, m, "enter", "enter", "down", "down").(*model)
	if answers := pendingAnswers(t, stateStore, target, "ex/varitem"); len(answers) != 2 || answers["p"] != "answered" {
		t.Fatalf("settled pending answers = %v; want the answers kept", answers)
	}
}

func TestSelectionFormPane(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFormKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// On a wide terminal the form takes the details pane: the tree keeps the
	// keys until the form opens, the view stays closed.
	m := openFormKit(t, app, kitDir, 120, 32)
	m = drive(t, m, "space").(*model)
	if !m.formOpen || m.view {
		t.Fatalf("form open = %v, view = %v; want the form in the pane", m.formOpen, m.view)
	}
	view := m.View()
	for _, want := range []string{"Variables", "pick a word", "required"} {
		if !strings.Contains(view, want) {
			t.Errorf("the pane misses %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, formFooter) {
		t.Errorf("the footer names no form keys:\n%s", view)
	}

	// esc clears the selection; the pane returns to the details of the row.
	m = drive(t, m, "esc").(*model)
	if m.formOpen {
		t.Fatal("esc left the form open")
	}
	if view := m.View(); !strings.Contains(view, "Contributes") || strings.Contains(view, "pick a word") {
		t.Errorf("esc did not return the pane to the details:\n%s", view)
	}
	if n := m.pendingCount(); n != 0 {
		t.Fatalf("esc kept %d pending actions", n)
	}

	// An accepted form closes and the pane shows the pending answers in the
	// Variables section.
	m = drive(t, m, "space", "answered", "enter").(*model)
	if m.formOpen {
		t.Fatal("enter did not accept the form")
	}
	if answers := pendingAnswers(t, stateStore, target, "ex/varitem"); answers["p"] != "answered" {
		t.Fatalf("pending answers = %v", answers)
	}
	if view := m.View(); !strings.Contains(view, "p = answered") {
		t.Errorf("the pane misses the stored answer:\n%s", view)
	}
}

func TestSelectionFormOverView(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFormKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// A form opened over a details view the user had open returns to it on
	// accept, with the details showing the pending answers.
	m := openFormKit(t, app, kitDir, 100, 30)
	m = drive(t, m, "enter").(*model) // the details view of the var item
	if !m.view {
		t.Fatal("enter opened no details view")
	}
	m = drive(t, m, "space").(*model)
	if !m.formOpen || !m.view {
		t.Fatalf("form open = %v, view = %v; want the form over the view", m.formOpen, m.view)
	}
	m = drive(t, m, "answered", "enter").(*model)
	if m.formOpen || !m.view {
		t.Fatalf("after accept: form %v, view %v; want the view back", m.formOpen, m.view)
	}
	if view := m.View(); !strings.Contains(view, "Contributes") || !strings.Contains(view, "p = answered") {
		t.Errorf("the view did not return to the details:\n%s", view)
	}

	// esc over a pre-existing view cancels the selection and keeps the view.
	// The selection is held from the first accept, so space unselects before
	// the re-selection opens the form again.
	m = drive(t, m, "space", "space").(*model)
	if !m.formOpen {
		t.Fatal("re-selection opened no form")
	}
	m = drive(t, m, "esc").(*model)
	if m.formOpen || !m.view {
		t.Fatalf("after esc: form %v, view %v; want the view kept", m.formOpen, m.view)
	}
	if n := m.pendingCount(); n != 0 {
		t.Fatalf("esc kept %d pending actions", n)
	}
}

func TestUpdateIntroducesVariable(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFormKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// Install the var item with its two answers.
	m := openFormKit(t, app, kitDir, 100, 30)
	m = drive(t, m, "space", "answered", "tab", "dev", "enter", "a", "enter").(*model)
	if !strings.Contains(m.lastResults, "applied  ex/varitem 1.0.0") {
		t.Fatalf("install:\n%s", m.lastResults)
	}

	// The kit publishes 1.1.0 introducing a third, required variable.
	bumpFormKit(t, kitDir, "1.1.0", "q")
	m = resize(t, app.Initial(""), 100, 30).(*model)
	if r := itemRow(m, "ex/varitem"); r.Status != StatusUpdate {
		t.Fatalf("update row = %+v", r)
	}

	// The held selection unselects first; selecting the update opens the
	// form with the stored answers and the new field empty.
	m = drive(t, m, "down", "down", "space").(*model)
	if m.formOpen {
		t.Fatal("space unselected instead")
	}
	m = drive(t, m, "space").(*model)
	if !m.formOpen || len(m.formFields) != 3 || m.formFields[2].varID != "q" || !m.formFields[2].required {
		t.Fatalf("form fields = %+v", m.formFields)
	}
	if m.formAnswers["p"] != "answered" || m.formAnswers["b"] != "dev" || m.formAnswers["q"] != "" {
		t.Fatalf("prefill = %v; want the stored answers with the new field empty", m.formAnswers)
	}

	// The gate holds until the new variable fills; the accepted answers
	// cover every declared variable.
	m = drive(t, m, "tab", "tab", "main", "enter").(*model)
	if m.formOpen {
		t.Fatal("enter did not accept the filled form")
	}
	answers := pendingAnswers(t, stateStore, target, "ex/varitem")
	if len(answers) != 3 || answers["q"] != "main" {
		t.Fatalf("pending answers = %v; want q saved", answers)
	}

	// The update applies with the stored answers substituted.
	m = drive(t, m, "a", "enter").(*model)
	if !strings.Contains(m.lastResults, "applied  ex/varitem 1.1.0") {
		t.Fatalf("update apply:\n%s", m.lastResults)
	}
	data, err := os.ReadFile(filepath.Join(target, "skills", "varitem", "v.md"))
	if err != nil || string(data) != "var answered on dev\n" {
		t.Errorf("templated file = %q, %v; want the stored answers substituted", data, err)
	}
}
