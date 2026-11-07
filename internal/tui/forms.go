package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dpleshakov/penates/internal/state"
)

// The variable form of the selection (tui-screens.md 7.3): selecting an
// item with variables — or an update that introduces a new one — turns the
// details pane or the details view into a form. The fields prefill with
// the stored answers, tab moves between them, enter is accepted only when
// every required field holds a value, and esc clears the pending action of
// the item. The accepted answers go into the pending object of the state
// like every other toggle and reach the lock through the apply; nothing
// asks for variables at apply time.

// formField is one declared variable of the form: the identifier the
// answers key by, the prompt the kit gives, and whether the enter gate
// requires a value.
type formField struct {
	varID    string
	prompt   string
	required bool
}

// formFooter names the keys the form answers (7.3).
const formFooter = "tab next · enter confirm · esc cancel selection"

// FormDetails carries the values one variable form renders from (7.3): the
// item line the form belongs to and the fields in declaration order.
type FormDetails struct {
	// Title is the item line the details show: "id · kit".
	Title string
	// Fields are the form fields in declaration order.
	Fields []FormField
}

// FormField is one field of the variable form: the prompt of the declared
// variable, whether it is required, and the value the field holds — the
// text input's view on the active field, the draft value on the others.
type FormField struct {
	ID       string
	Prompt   string
	Required bool
	Value    string
	// Active marks the field the input and the cursor sit on.
	Active bool
}

// RenderItemForm renders the variable form of one item (7.3): the item
// line, then one block per declared variable — the prompt with the
// required mark, and the input of the active field behind the cursor bar.
// The lines are full-length; the pane and the view clip or scroll them.
// Prose wraps at no more than ProseLimit columns regardless of width.
func RenderItemForm(d FormDetails, width int, set Set) []string {
	lines := []string{d.Title, "", "Variables"}
	for _, f := range d.Fields {
		prompt := f.Prompt
		if prompt == "" {
			prompt = f.ID
		}
		wrapped := WrapProse(prompt, width)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		if f.Required {
			wrapped[len(wrapped)-1] += "   " + Dim("required")
		}
		lines = append(lines, "")
		lines = append(lines, wrapped...)
		switch {
		case f.Active:
			lines = append(lines, cursorMark.glyph(set)+f.Value)
		case f.Value != "":
			lines = append(lines, "  "+f.Value)
		}
	}
	return lines
}

// openForm opens the variable form of the freshly selected item: the
// fields prefill with the stored answers — the pending ones over the ones
// recorded in the lock. On a wide terminal the form takes the details
// pane; over any other width it opens the details view.
func (m *model) openForm(key string) tea.Cmd {
	it := m.kitItem(key)
	if it == nil || len(it.Vars) == 0 {
		return nil
	}
	stored := m.storedAnswers(key)
	m.formOpen = true
	m.formKey = key
	m.formFromView = m.view
	m.formFields = make([]formField, 0, len(it.Vars))
	m.formAnswers = make(map[string]string, len(it.Vars))
	for _, v := range it.Vars {
		m.formFields = append(m.formFields, formField{varID: v.ID, prompt: v.Prompt, required: v.Required})
		// A stored answer may be empty: the prefill is honestly empty, and
		// the enter gate keeps the form open until a required field fills.
		m.formAnswers[v.ID] = stored[v.ID]
	}
	m.formCursor = 0
	m.formInput.Reset()
	m.formInput.SetValue(m.formAnswers[it.Vars[0].ID])
	m.formInput.Focus()
	if !m.view && NewLayout(m.width, m.height).Mode != ModeWide {
		m.view = true
	}
	m.viewOffset = 0
	return textinput.Blink
}

// updateForm feeds the keys of the form: the input takes the typing, tab
// moves to the next field, enter accepts when every required field holds
// a value, esc cancels the selection (7.3).
func (m *model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, m.cancelForm()
	case "enter":
		return m, m.acceptForm()
	case "tab":
		m.commitField()
		m.formCursor = (m.formCursor + 1) % len(m.formFields)
		m.formInput.Reset()
		m.formInput.SetValue(m.formAnswers[m.formFields[m.formCursor].varID])
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	m.formInput, cmd = m.formInput.Update(msg)
	return m, cmd
}

// acceptForm records the draft into the pending answers of the item — the
// same store every toggle saves through — and closes the form. The gate:
// enter is accepted only when every required field holds a value, so the
// selection the answers belong to never plans without them.
func (m *model) acceptForm() tea.Cmd {
	m.commitField()
	for _, f := range m.formFields {
		if f.required && m.formAnswers[f.varID] == "" {
			return nil
		}
	}
	answers := make(map[string]string, len(m.formFields))
	for _, f := range m.formFields {
		answers[f.varID] = m.formAnswers[f.varID]
	}
	key := m.formKey
	if err := m.editPending(key, func(it *state.ItemPending) { it.Answers = answers }); err != nil {
		m.closeForm()
		return m.showNotice(err.Error())
	}
	m.refresh()
	m.closeForm()
	return nil
}

// cancelForm clears the pending action of the item the form belongs to —
// the answers with the selection — and closes the form (7.3).
func (m *model) cancelForm() tea.Cmd {
	key := m.formKey
	m.closeForm()
	return m.setPending(key, func(it *state.ItemPending) { *it = state.ItemPending{} })
}

// closeForm closes the form: a view the form opened for closes with it, a
// view open before it returns to the details.
func (m *model) closeForm() {
	m.formOpen = false
	m.formKey = ""
	m.formFields = nil
	m.formAnswers = nil
	m.formCursor = 0
	m.formInput.Reset()
	if m.view {
		if m.formFromView {
			m.viewOffset = 0
			m.openViewLines()
		} else {
			m.view = false
			m.viewLines = nil
			m.viewOffset = 0
		}
	}
	m.formFromView = false
}

// commitField stashes the input's value into the draft of the active field.
func (m *model) commitField() {
	if len(m.formFields) == 0 {
		return
	}
	m.formAnswers[m.formFields[m.formCursor].varID] = strings.TrimSpace(m.formInput.Value())
}

// formDetails gathers the values the variable form of the open form
// renders from (7.3).
func (m *model) formDetails() FormDetails {
	fields := make([]FormField, len(m.formFields))
	for i, f := range m.formFields {
		active := i == m.formCursor
		value := m.formAnswers[f.varID]
		if active {
			value = m.formInput.View()
		}
		fields[i] = FormField{ID: f.varID, Prompt: f.prompt, Required: f.required, Value: value, Active: active}
	}
	title := m.formKey
	if r, ok := m.itemRowByKey(m.formKey); ok {
		title = r.ID + " · " + r.KitName
	}
	return FormDetails{Title: title, Fields: fields}
}

// storedAnswers is the answer set one form prefills from: the answers held
// in the pending actions over the ones recorded in the lock.
func (m *model) storedAnswers(key string) map[string]string {
	out := map[string]string{}
	if m.lk != nil {
		if rec := m.lk.Items[key]; rec != nil {
			for id, a := range rec.Answers {
				out[id] = a
			}
		}
	}
	if pending := m.pendingActions(); pending != nil {
		if item := pending.Items[key]; item != nil {
			for id, a := range item.Answers {
				out[id] = a
			}
		}
	}
	return out
}
