package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/state"
)

// The kit management flows of the main screen (tui-screens.md 6): adding a
// kit with directory completion and the debounced preview (6.2), the
// target-hint dialog and the first-target input (6.3, 6.4), the preview
// errors with the input staying editable (6.5), the relocation of 6.6, and
// the detach dialog (6.7). The flows drive the state operations of the
// core — RegisterKit, AttachKit, DetachKit, DeferDetachKit, EditKitPath —
// and hold no state logic of their own.

// inputKind names the row the path input took over, if any.
type inputKind int

const (
	// inputNone: the tree answers the keys.
	inputNone inputKind = iota
	// inputAddKit: the path input of 6.2 on the add-kit row.
	inputAddKit
	// inputEditKit: the path input of 6.6 on a kit row.
	inputEditKit
)

// The key lines the flows name in the footer (6.2, 6.4, 6.6).
const (
	addKitKeys   = "tab complete · enter attach · esc cancel"
	editKitKeys  = "tab complete · enter apply path · esc cancel"
	targetKeys   = "enter register target and attach · esc cancel"
	previewTitle = "New kit"
)

// previewDelay is how long the path input waits after the last keystroke
// before it reads the directory (6.2: the directory is read a short
// interval after the last keystroke, not on every key).
const previewDelay = 200 * time.Millisecond

// previewTickMsg carries the edit the tick was scheduled for: every edit
// replaces the sequence, so only the last edit's tick reads.
type previewTickMsg uint

func tickPreview(seq uint) tea.Cmd {
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return previewTickMsg(seq) })
}

// previewProblem names what stands between the path and a kit (6.5, 6.6).
type previewProblem int

const (
	previewOK previewProblem = iota
	// previewNoDir: the path names no directory.
	previewNoDir
	// previewNoKitYaml: the directory holds no kit.yaml.
	previewNoKitYaml
	// previewInvalid: kit.yaml or the items do not read.
	previewInvalid
)

// kitPreview is what a read of the path input found: the kit when it read,
// the problems of the items that did not (6.5), and the problem that stops
// the attach when there is one.
type kitPreview struct {
	// dir is the absolute directory the read covered.
	dir string
	// kit is the kit when it read.
	kit *kit.Kit
	// problems are the item-level problems (6.5): they show on the
	// affected item rows and do not stop the attach.
	problems []error
	// problem names the kit-level problem, previewOK when the kit read.
	problem previewProblem
	// err is the problem of a kit.yaml that does not read (previewInvalid).
	err error
}

// attachOffer is the kit the attach dialog of 6.3 acts on, with the target
// its hint proposes.
type attachOffer struct {
	kit  *kit.Kit
	hint string
}

// startAddKit opens the path input on the add-kit row (6.2), the path
// filled in. The initial preview read is scheduled with the input: an
// input that already names a kit shows it at once.
func (m *model) startAddKit(path string) tea.Cmd {
	m.inputKind = inputAddKit
	m.editName = ""
	m.pathInput.SetValue(path)
	m.pathInput.Focus()
	m.refresh()
	m.cursor = len(m.rows) - 1
	m.previewSeq++
	return tickPreview(m.previewSeq)
}

// startEditKit opens the relocation input of 6.6 on the kit row: the path
// prefilled with the kit's directory, the pane holding the kit's state —
// an unreadable kit its read error and the frozen note.
func (m *model) startEditKit(name, dir string) {
	m.inputKind = inputEditKit
	m.editName = name
	m.pathInput.SetValue(displayPath(dir))
	m.pathInput.Focus()
	p := readKitPreview(dir)
	m.editReadable = p.problem == previewOK
	switch p.problem {
	case previewNoDir:
		m.editErr = displayPath(dir) + ": no such directory"
	case previewNoKitYaml:
		m.editErr = "kit.yaml not found in " + displayPath(dir)
	case previewInvalid:
		m.editErr = p.err.Error()
	default:
		m.editErr = ""
	}
	m.refresh()
}

// closeInput tears the path input down: the row returns, the draft, the
// preview and the first-target step go.
func (m *model) closeInput() {
	m.inputKind = inputNone
	m.editName = ""
	m.pathInput.Reset()
	m.pathInput.Blur()
	m.targetMode = false
	m.targetInput.Reset()
	m.targetInput.Blur()
	m.completions = nil
	m.completion = 0
	m.preview = nil
	m.attach = nil
	m.editReadable = false
	m.editErr = ""
	m.view = false
	m.viewPreview = false
	m.viewLines = nil
	m.viewOffset = 0
}

// updateInput feeds the active path input: the add-kit flow of 6.2-6.5 or
// the relocation of 6.6.
func (m *model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.inputKind == inputEditKit {
		return m.updateEditKit(msg)
	}
	return m.updateAddKit(msg)
}

// updateAddKit feeds the add-kit input of 6.2: tab completes the
// directory, the preview reads after the last keystroke, and enter
// proceeds — the attach, the target-hint dialog, or the first-target
// input. The preview view of the narrower terminals scrolls with the
// arrows and keeps the flow's keys.
func (m *model) updateAddKit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m, m.proceedAdd()
	case "esc":
		if m.targetMode {
			// The first-target step steps back to the path (6.4); the
			// preview view of the narrower terminals closes with it.
			m.targetMode = false
			m.targetInput.Reset()
			m.targetInput.Blur()
			m.pathInput.Focus()
			m.view = false
			m.viewPreview = false
			m.viewLines = nil
			m.viewOffset = 0
			return m, textinput.Blink
		}
		if m.view {
			// The preview view closes back to the input row (6.2); the
			// errors of 6.5 keep the input editable.
			m.view = false
			m.viewPreview = false
			m.viewLines = nil
			m.viewOffset = 0
			return m, nil
		}
		m.closeInput()
		return m, nil
	case "tab":
		if !m.targetMode {
			return m, m.completePath()
		}
	case "up", "down", "pgup", "pgdown":
		if m.view {
			m.scrollPreviewView(msg.String())
			return m, nil
		}
	}
	var cmd tea.Cmd
	if m.targetMode {
		m.targetInput, cmd = m.targetInput.Update(msg)
	} else {
		m.pathInput, cmd = m.pathInput.Update(msg)
		m.completion = 0
	}
	m.previewSeq++
	return m, tea.Batch(cmd, tickPreview(m.previewSeq))
}

// updateEditKit feeds the relocation input of 6.6: tab completes, enter
// applies the path when it holds a kit of the same name, and the preview
// keeps the input editable on a rejection.
func (m *model) updateEditKit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m, m.applyEditKit()
	case "esc":
		m.closeInput()
		return m, nil
	case "tab":
		return m, m.completePath()
	}
	var cmd tea.Cmd
	m.pathInput, cmd = m.pathInput.Update(msg)
	m.completion = 0
	m.previewSeq++
	return m, tea.Batch(cmd, tickPreview(m.previewSeq))
}

// scrollPreviewView scrolls the preview view the narrower terminals open
// (6.2); the keys of the flow keep working beside it.
func (m *model) scrollPreviewView(key string) {
	page := m.viewportHeight()
	if page < 1 {
		page = 1
	}
	total := len(m.previewLines(m.width))
	switch key {
	case "up":
		m.viewOffset = ScrollOffset(m.viewOffset-1, total, page)
	case "down":
		m.viewOffset = ScrollOffset(m.viewOffset+1, total, page)
	case "pgup":
		m.viewOffset = ScrollOffset(m.viewOffset-page, total, page)
	case "pgdown":
		m.viewOffset = ScrollOffset(m.viewOffset+page, total, page)
	}
}

// readPreview is what a debounce tick performs (6.2): the directory
// candidates of the path input, the existence of the first-target
// directory, and the kit the path names — or, while the target list is
// open, the candidates and the existence of its add-target input.
func (m *model) readPreview() {
	if m.list != nil {
		m.list.read()
		return
	}
	if m.inputKind == inputNone {
		return
	}
	path := m.inputPath()
	if path == "" {
		m.preview = nil
		m.completions = nil
		return
	}
	m.completions = directoryCandidates(m.pathInput.Value())
	if m.targetMode {
		m.targetExists = dirExists(m.targetInputPath())
	}
	m.preview = readKitPreview(path)
	// The first-target input of 6.4 prefills with the hint the preview
	// shows, and its existence line follows the value.
	if m.preview.problem == previewOK && m.noTargets() {
		if m.targetInput.Value() == "" {
			m.targetInput.SetValue(m.preview.kit.TargetHint)
		}
		m.targetExists = dirExists(m.targetInputPath())
	}
}

// noTargets reports whether the registry holds no registered targets: the
// first-target step of 6.4 applies.
func (m *model) noTargets() bool {
	return m.reg == nil || len(m.reg.Targets) == 0
}

// proceedAdd is the enter of the add-kit input (6.2): a fresh read of the
// path decides — the errors of 6.5 keep the input editable, a kit whose
// hint matches the current target attaches, a differing hint opens the
// dialog of 6.3, and without registered targets the preview gains the
// target input of 6.4.
func (m *model) proceedAdd() tea.Cmd {
	if m.targetMode {
		return m.registerTarget()
	}
	path := m.inputPath()
	if path == "" {
		return nil
	}
	p := readKitPreview(path)
	m.preview = p
	if p.problem != previewOK {
		return m.showPreviewView()
	}
	if block := m.addBlock(p.kit); block != "" {
		return m.showPreviewView()
	}
	if m.noTargets() {
		// The first-target step of 6.4: the target input prefills with the
		// hint the kit proposes — a draft from the preview stays.
		m.targetMode = true
		if m.targetInput.Value() == "" {
			m.targetInput.SetValue(p.kit.TargetHint)
		}
		m.targetInput.Focus()
		m.pathInput.Blur()
		m.targetExists = dirExists(m.targetInputPath())
		return m.showPreviewView()
	}
	// The comparison of 6.2 is between two paths: the hint and the
	// current target. A screen that opened no target yet — the kit-path
	// argument of 6.1 — attaches at the hint and moves to it.
	hint, ok := absPath(p.kit.TargetHint)
	switch {
	case ok && m.target == "":
		return m.attachOfferAt(&attachOffer{kit: p.kit, hint: hint}, hint)
	case ok && hint != m.target:
		return m.openAttachDialog(p.kit)
	case ok:
		return m.attachOfferAt(&attachOffer{kit: p.kit, hint: hint}, m.target)
	}
	return m.showNotice("the kit declares no target_hint")
}

// registerTarget is the enter of the first-target input (6.4): the target
// registers, the kit attaches to it, and the screen moves there.
func (m *model) registerTarget() tea.Cmd {
	t := m.targetInputPath()
	if t == "" {
		return nil
	}
	return m.attachOfferAt(&attachOffer{kit: m.preview.kit, hint: t}, t)
}

// addBlock is the registry condition that stops an attach (6.5): a name
// already registered at another directory, or the kit already attached to
// this target. Empty when nothing blocks.
func (m *model) addBlock(k *kit.Kit) string {
	if m.reg == nil {
		return ""
	}
	if kk, ok := m.reg.Kits[k.Name]; ok && kk.Dir != k.Dir {
		return fmt.Sprintf("A kit named %s is already registered at %s", k.Name, displayPath(kk.Dir))
	}
	if m.target != "" && attachedTo(m.reg, k.Name, m.target) {
		return "The kit is already attached to this target"
	}
	return ""
}

// showPreviewView opens the preview as the details view where no pane
// carries it (6.2: on medium and narrow terminals the preview opens as
// the details view after enter).
func (m *model) showPreviewView() tea.Cmd {
	if NewLayout(m.width, m.height).Mode != ModeWide {
		m.view = true
		m.viewPreview = true
		m.viewOffset = 0
	}
	return nil
}

// openAttachDialog opens the dialog of 6.3: the kit proposes another
// target than the current one. The first option reads "Attach to X and
// switch to it" when that target is already registered.
func (m *model) openAttachDialog(k *kit.Kit) tea.Cmd {
	hint, ok := absPath(k.TargetHint)
	if !ok {
		return m.showNotice("the kit declares no usable target_hint")
	}
	m.attach = &attachOffer{kit: k, hint: hint}
	d := &dialog{
		kind:  dialogAttach,
		title: "Attach " + k.Name,
		body: []string{
			fmt.Sprintf("The kit proposes %s.", k.TargetHint),
			fmt.Sprintf("The current target is %s.", displayPath(m.target)),
		},
		options: []string{
			fmt.Sprintf("Register %s as a target and attach there", k.TargetHint),
			fmt.Sprintf("Attach to %s anyway", displayPath(m.target)),
			"Cancel",
		},
		chosen: 0,
	}
	if _, ok := m.reg.Targets[hint]; ok {
		d.options[0] = fmt.Sprintf("Attach to %s and switch to it", k.TargetHint)
	}
	m.dialog = d
	return nil
}

// attachOfferAt registers the kit and attaches it to target — RegisterKit
// and AttachKit do the work, and AttachKit registers a new target — then
// moves to the target when it differs from the current one. The notice
// confirms the change and offers the undo of 11.2: u detaches the kit
// again while the notice lives.
func (m *model) attachOfferAt(o *attachOffer, target string) tea.Cmd {
	if err := m.app.State.RegisterKit(o.kit.Name, o.kit.Dir); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.app.State.AttachKit(o.kit.Name, target); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	m.closeInput()
	// The target the kit attached to opens — a full reload: the kit joins
	// the target's tree only when its kits load again.
	entered := m.enterTarget(target)
	name := o.kit.Name
	undo := func() tea.Cmd { return m.detachKit(name) }
	return tea.Batch(entered, m.showUndoable(fmt.Sprintf("Attached %s at %s.", name, displayPath(target)), undo))
}

// applyEditKit points the kit to the directory the input names (6.6): the
// directory must hold a kit of the same name — a mismatch stays a
// rejection in the preview, the input editable.
func (m *model) applyEditKit() tea.Cmd {
	path := m.inputPath()
	if path == "" {
		return nil
	}
	p := readKitPreview(path)
	m.preview = p
	if p.problem != previewOK || p.kit.Name != m.editName {
		return nil
	}
	if err := m.app.State.EditKitPath(m.editName, p.dir, p.kit.Name); err != nil {
		return m.showNotice(err.Error())
	}
	name := m.editName
	dir := displayPath(p.dir)
	m.closeInput()
	entered := m.enterTarget(m.target)
	return tea.Batch(entered, m.showNotice(fmt.Sprintf("Pointed %s to %s.", name, dir)))
}

// completePath completes the path input to the next candidate (6.2): the
// first tab takes the first candidate, the next ones cycle through the
// list.
func (m *model) completePath() tea.Cmd {
	next, value := nextCompletion(&m.pathInput, m.completions, m.completion)
	if value == "" {
		return nil
	}
	m.completion = next
	m.pathInput.SetValue(value)
	m.previewSeq++
	return tickPreview(m.previewSeq)
}

// nextCompletion cycles an input over the directory candidates: the first
// tab takes the first candidate, the next ones cycle through the list. It
// returns the index taken and the value set, empty when there are no
// candidates.
func nextCompletion(input *textinput.Model, completions []string, completion int) (int, string) {
	if len(completions) == 0 {
		return completion, ""
	}
	if input.Value() == completions[completion] {
		completion = (completion + 1) % len(completions)
	} else {
		completion = 0
	}
	return completion, completions[completion]
}

// inputPath is the path the path input names: trimmed and absolute. Empty
// while the input holds nothing.
func (m *model) inputPath() string {
	return absOf(strings.TrimSpace(m.pathInput.Value()))
}

// targetInputPath is the target directory the first-target input names.
func (m *model) targetInputPath() string {
	return absOf(strings.TrimSpace(m.targetInput.Value()))
}

// absOf expands ~ and makes the path absolute; an unusable path comes
// back expanded only. Empty stays empty.
func absOf(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(expandHome(path)); err == nil {
		return abs
	}
	return expandHome(path)
}

// absPath expands a kit-declared hint into the absolute path the
// comparisons use (6.2: the comparison is between two paths); false when
// the hint is empty.
func absPath(hint string) (string, bool) {
	if strings.TrimSpace(hint) == "" {
		return "", false
	}
	return absOf(hint), true
}

// displayPath shortens an absolute path for the texts: the home directory
// renders as ~ (the mockups of 4 and 6).
func displayPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	for _, sep := range []string{string(filepath.Separator), "/"} {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+sep) {
			return "~" + p[len(home):]
		}
	}
	return p
}

// dirExists reports whether the path names an existing directory.
func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// readKitPreview reads the kit the path names (6.2, 6.5): a missing
// directory, a missing kit.yaml and a kit.yaml that does not read each
// find their way into the preview.
func readKitPreview(path string) *kitPreview {
	abs := absOf(path)
	if !dirExists(abs) {
		return &kitPreview{problem: previewNoDir}
	}
	if _, err := os.Stat(filepath.Join(abs, "kit.yaml")); err != nil {
		return &kitPreview{dir: abs, problem: previewNoKitYaml}
	}
	k, problems, err := kit.Read(abs)
	if err != nil {
		return &kitPreview{dir: abs, problem: previewInvalid, err: err}
	}
	return &kitPreview{dir: abs, kit: k, problems: problems}
}

// directoryCandidates lists the directories the typed path could become:
// the entries of its parent directory that are directories and match the
// element the cursor sits on, as the mockup of 6.2 lists them — with the
// trailing slash. Hidden entries stay out unless the prefix asks for them.
func directoryCandidates(path string) []string {
	expanded := expandHome(path)
	if expanded == "" {
		return nil
	}
	dir, prefix := expanded, ""
	if !strings.HasSuffix(expanded, "/") && !strings.HasSuffix(expanded, string(filepath.Separator)) {
		dir, prefix = filepath.Dir(expanded), filepath.Base(expanded)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	lower := strings.ToLower(prefix)
	hidden := strings.HasPrefix(prefix, ".")
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), lower) {
			continue
		}
		// The candidates render the way the paths do: the home directory
		// as ~ (6.2 mockup).
		out = append(out, displayPath(filepath.ToSlash(filepath.Join(dir, name)))+"/")
	}
	sort.Strings(out)
	return out
}

// previewLines render the preview the pane or the view shows for the
// input mode: the new-kit block of 6.2-6.5 while adding, the relocation
// block of 6.6 while editing. Pure over the model: the reads happen in
// the debounced tick, never here.
func (m *model) previewLines(width int) []string {
	if m.inputKind == inputEditKit {
		return m.editPreviewLines(width)
	}
	return m.addPreviewLines(width)
}

// addPreviewLines render the new-kit preview (6.2): the kit's facts, the
// warning of a differing target hint, the errors of 6.5, and — without
// registered targets — the target input of 6.4.
func (m *model) addPreviewLines(width int) []string {
	lines := []string{previewTitle, ""}
	p := m.preview
	if p == nil {
		return lines
	}
	pathDisplay := displayPath(m.inputPath())
	if p.dir != "" {
		pathDisplay = displayPath(p.dir)
	}
	switch p.problem {
	case previewNoDir, previewNoKitYaml:
		lines = append(lines, renderFields([]detailField{{"Path", []string{pathDisplay}}})...)
		lines = append(lines, "")
		return append(lines, errorLines(width, m.set, "kit.yaml not found in "+pathDisplay)...)
	case previewInvalid:
		lines = append(lines, renderFields([]detailField{{"Path", []string{pathDisplay}}})...)
		lines = append(lines, "")
		lines = append(lines, errorLines(width, m.set, "The kit cannot be added.")...)
		lines = append(lines, "")
		return append(lines, detailLines(width, p.err.Error())...)
	}

	k := p.kit
	fields := []detailField{
		{label: "Path", values: []string{pathDisplay}},
		{label: "Name", values: []string{k.Name}},
	}
	if k.Description != "" {
		fields = append(fields, detailField{label: "Description", values: WrapProse(k.Description, width)})
	}
	fields = append(fields, detailField{label: "Items", values: []string{strconv.Itoa(len(k.Items))}})
	if len(k.Shared) > 0 {
		fields = append(fields, detailField{label: "Shared files", values: []string{strings.Join(k.Shared, ", ")}})
	}
	if m.reg != nil && !m.noTargets() && k.TargetHint != "" {
		fields = append(fields, detailField{label: "Target hint", values: []string{k.TargetHint}})
	}
	lines = append(lines, renderFields(fields)...)

	// The item-level problems do not stop the attach; their rows show
	// them (6.5).
	for _, pr := range p.problems {
		lines = append(lines, "")
		lines = append(lines, errorLines(width, m.set, pr.Error())...)
	}

	if block := m.addBlock(k); block != "" {
		lines = append(lines, "")
		return append(lines, errorLines(width, m.set, block)...)
	}
	if m.noTargets() {
		// The first-target input of 6.4 ends the preview, with its key
		// line and the existence of the named directory.
		lines = append(lines, "")
		lines = append(lines, "No target is registered. Target directory:")
		lines = append(lines, m.targetLine())
		lines = append(lines, wrapIndent(m.targetExistsLine(), "  ", "  ", width)...)
		lines = append(lines, "")
		lines = append(lines, Dim(targetKeys))
		return lines
	}
	if hint, ok := absPath(k.TargetHint); ok && hint != m.target {
		// The warning of 6.2: the hint proposes another target than the
		// one the screen shows.
		warn := fmt.Sprintf("The kit proposes %s. The current target is %s.", k.TargetHint, displayPath(m.target))
		lines = append(lines, "")
		lines = append(lines, wrapIndent(warn, StatusWarning.Mark(m.set)+" ", "  ", width)...)
	}
	return lines
}

// editPreviewLines render the pane of the relocation input (6.6): the new
// kit when the input names one, the rejection when it has another name,
// the kit's own state while it names no kit directory.
func (m *model) editPreviewLines(width int) []string {
	p := m.preview
	if p != nil && p.problem == previewOK {
		if p.kit.Name != m.editName {
			lines := []string{m.editName, ""}
			text := fmt.Sprintf("%s contains the kit %s, not %s. Add it as a new kit instead.",
				displayPath(p.dir), p.kit.Name, m.editName)
			return append(lines, errorLines(width, m.set, text)...)
		}
		k := p.kit
		fields := []detailField{
			{label: "Path", values: []string{displayPath(p.dir)}},
			{label: "Name", values: []string{k.Name}},
		}
		if k.Description != "" {
			fields = append(fields, detailField{label: "Description", values: WrapProse(k.Description, width)})
		}
		fields = append(fields, detailField{label: "Items", values: []string{strconv.Itoa(len(k.Items))}})
		if len(k.Shared) > 0 {
			fields = append(fields, detailField{label: "Shared files", values: []string{strings.Join(k.Shared, ", ")}})
		}
		return append([]string{m.editName, ""}, renderFields(fields)...)
	}
	if p != nil && p.problem == previewNoKitYaml {
		lines := append([]string{m.editName, ""}, errorLines(width, m.set, "The kit directory cannot be read:")...)
		return append(lines, detailLines(width, "kit.yaml not found in "+displayPath(p.dir))...)
	}
	if p != nil && p.problem == previewInvalid {
		lines := append([]string{m.editName, ""}, errorLines(width, m.set, "The kit directory cannot be read:")...)
		return append(lines, detailLines(width, p.err.Error())...)
	}
	return m.editCurrentLines(width)
}

// editCurrentLines show the kit the input edits while the input names no
// kit directory (6.6): an unreadable kit its read error and the frozen
// note, a readable one its details.
func (m *model) editCurrentLines(width int) []string {
	if !m.editReadable {
		lines := append([]string{m.editName, ""}, errorLines(width, m.set, "The kit directory cannot be read:")...)
		if m.editErr != "" {
			lines = append(lines, detailLines(width, m.editErr)...)
		}
		lines = append(lines, "")
		n := len(m.installedItems(m.editName))
		note := fmt.Sprintf("Its %d installed items are frozen: no updates, removal only. Point the kit to its new directory to resume.", n)
		if n == 1 {
			note = "Its 1 installed item is frozen: no updates, removal only. Point the kit to its new directory to resume."
		}
		return append(lines, WrapProse(note, width)...)
	}
	return RenderKitDetails(m.kitDetails(m.editName), width, m.set)
}

// targetLine renders the first-target input line (6.4): the cursor bar
// while the step is active.
func (m *model) targetLine() string {
	if m.targetMode {
		return cursorMark.glyph(m.set) + m.targetInput.View()
	}
	return m.targetInput.View()
}

// targetExistsLine is the existence line of 6.4 under the target input.
func (m *model) targetExistsLine() string {
	if m.targetExists {
		return "The directory exists."
	}
	return "The directory does not exist. It is created on the first apply."
}

// errorLines renders one preview error (6.5): the ✗ glyph, the first
// line, the wrapped rest.
func errorLines(width int, set Set, text string) []string {
	return wrapIndent(text, StatusConflict.Mark(set)+" ", "  ", width)
}

// detailLines render the indented detail lines under a preview error: the
// problem the kit carries, one line per joined error (6.5).
func detailLines(width int, text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		out = append(out, wrapIndent(l, "  ", "  ", width)...)
	}
	return out
}

// completionLines render the dropdown of the directory candidates under
// the input row (6.2 mockup), aligned under the input text.
func (m *model) completionLines() []string {
	if m.inputKind == inputNone || m.view {
		return nil
	}
	return completionBox(m.completions, m.inputIndent(), m.set)
}

// inputIndent is the column the input text starts at, so the dropdown
// lines up under it (6.2 mockup).
func (m *model) inputIndent() int {
	switch m.inputKind {
	case inputAddKit:
		return len("  " + AddKitLabel + "  ")
	case inputEditKit:
		if len(m.rows) > 0 && m.cursor < len(m.rows) && m.rows[m.cursor].Kind == KindKit {
			mk := expandedMark
			if m.rows[m.cursor].Kit.Collapsed {
				mk = collapsedMark
			}
			return ansi.StringWidth("  " + mk.glyph(m.set) + " " + m.editName + "  ")
		}
	}
	return 0
}

// completionBox renders the dropdown of the directory candidates under an
// input whose text starts at the given column (6.2 mockup): the box of
// the candidates, one per line. Empty candidates render nothing.
func completionBox(candidates []string, indent int, set Set) []string {
	if len(candidates) == 0 {
		return nil
	}
	g := unicodeBox
	if set == ASCIISet {
		g = asciiBox
	}
	w := 0
	for _, c := range candidates {
		if len(c) > w {
			w = len(c)
		}
	}
	pad := strings.Repeat(" ", indent)
	inner := w + 2
	lines := make([]string, 0, len(candidates)+2)
	lines = append(lines, pad+g.tl+strings.Repeat(g.h, inner)+g.tr)
	for _, c := range candidates {
		lines = append(lines, pad+g.v+" "+fmt.Sprintf("%-*s", w, c)+" "+g.v)
	}
	return append(lines, pad+g.bl+strings.Repeat(g.h, inner)+g.br)
}

// detachUnderCursor opens the detach flow of the kit row under the cursor
// (6.7): a kit with installed items in the target asks what happens to
// them, a kit without any detaches at once with a notice. The orphans
// group is not a kit, and a kit already detaching waits for its apply.
func (m *model) detachUnderCursor() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	r := m.rows[m.cursor]
	if r.Kind != KindKit || r.Kit.Orphans || r.Kit.Detaching {
		return nil
	}
	return m.detachKitFlow(r.Kit.Name)
}

// detachKitFlow opens the detach flow of one kit of the target (6.7): the
// path the palette's Detach kit takes as well, for the kit it was given.
func (m *model) detachKitFlow(name string) tea.Cmd {
	installed := m.installedItems(name)
	if len(installed) == 0 {
		return m.detachKit(name)
	}
	m.detachName = name
	m.dialog = &dialog{
		kind:  dialogDetach,
		title: "Detach " + name,
		body:  detachDialogBody(installed, m.target),
		options: []string{
			"Remove its items from the target",
			"Keep its items installed as orphans",
			"Cancel",
		},
		// The default is Cancel: the question is destructive, and the
		// answer must never happen by enter alone (6.7).
		chosen: 2,
	}
	return nil
}

// installedItem is one installed item of a kit in the current target: the
// item key and the version the lock records.
type installedItem struct {
	key     string
	id      string
	version string
}

// installedItems lists the kit's installed items in the target, in
// ascending key order, the way the lock records them.
func (m *model) installedItems(kitName string) []installedItem {
	if m.lk == nil {
		return nil
	}
	prefix := kitName + "/"
	var out []installedItem
	for key, rec := range m.lk.Items {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		id, _, _ := strings.Cut(strings.TrimPrefix(key, prefix), "/")
		v := ""
		if rec != nil {
			v = rec.Version
		}
		out = append(out, installedItem{key: key, id: id, version: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// detachDialogBody names what the detach dialog asks about (6.7): how
// many items of the kit are installed in the target, then the items with
// their versions.
func detachDialogBody(items []installedItem, target string) []string {
	noun, verb := "items", "are"
	if len(items) == 1 {
		noun, verb = "item", "is"
	}
	var list []string
	for _, it := range items {
		list = append(list, strings.TrimSpace(it.id+" "+it.version))
	}
	return append(
		[]string{fmt.Sprintf("%d %s of this kit %s installed in %s:", len(items), noun, verb, target)},
		wrapIndent(strings.Join(list, ", "), "  ", "  ", ProseLimit)...,
	)
}

// detachWithRemovals marks the kit's installed items for removal and
// queues the detach behind the apply that removes them (6.7): the row
// shows "detaching" until then, and CompleteDeferred finishes the detach
// after the apply.
func (m *model) detachWithRemovals(kitName string) tea.Cmd {
	for _, it := range m.installedItems(kitName) {
		if err := m.editPending(it.key, func(ip *state.ItemPending) { ip.Remove = true }); err != nil {
			return m.showNotice(err.Error())
		}
	}
	if err := m.app.State.DeferDetachKit(kitName, m.target); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	m.refresh()
	return nil
}

// detachKit detaches the kit from the target at once (6.7): the files in
// the target are not touched, its installed items move to the orphans
// group, and the notice confirms the change and offers the undo of 11.2 —
// u attaches the kit back while the notice lives.
func (m *model) detachKit(kitName string) tea.Cmd {
	if err := m.app.State.DetachKit(kitName, m.target); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	m.refresh()
	t := m.target
	undo := func() tea.Cmd { return m.attachKitBack(kitName, t) }
	return m.showUndoable(fmt.Sprintf("Detached %s.", kitName), undo)
}

// attachKitBack is the undo of a detach (11.2): the kit attaches to the
// target again — the detach never unregistered it — and the target
// reopens with the kit's tree.
func (m *model) attachKitBack(kitName, target string) tea.Cmd {
	if err := m.app.State.AttachKit(kitName, target); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	entered := m.enterTarget(target)
	return tea.Batch(entered, m.showNotice(fmt.Sprintf("Attached %s at %s.", kitName, displayPath(target))))
}
