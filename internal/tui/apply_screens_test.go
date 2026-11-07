package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"

	"github.com/dpleshakov/penates/internal/apply"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
)

// The apply screens of section 10: the summary, the progress and the result
// against the mockups, the stepping of the core apply driving the progress
// view item by item, and the keys of the screens while the run lasts.

// summaryFixture is the mixed plan of the 10.1 mockup: an install of a file
// and a section, an update that replaces one file and keeps another, a
// removal that takes one file and releases another, and one excluded item.
var summaryFixture = applySummary{
	items: []summaryItem{
		{
			verb: verbInstall, id: "review", version: "0.3.0", kit: "team-opencode",
			lines: []summaryLine{
				{mark: markCreate, path: "agents/review.md"},
				{mark: markCreate, path: "AGENTS.md", suffix: "section team-opencode/review"},
			},
		},
		{
			verb: verbUpdate, id: "done-v2", version: "1.2.0 → 1.3.0", kit: "personal-opencode",
			lines: []summaryLine{
				{mark: markReplace, path: "plugin/done.ts"},
				{mark: markKeep, path: "plugin/README.md", suffix: "kept: already installed"},
			},
		},
		{
			verb: verbRemove, id: "old-lint", version: "0.2.0", kit: "team-opencode",
			lines: []summaryLine{
				{mark: markDelete, path: "agents/old-lint.md"},
				{mark: markRelease, path: "agents/old-lint-notes.md", suffix: "kept and released: modified by the user"},
			},
		},
	},
	excluded: []summaryExcluded{
		{id: "git-via-gh", reasons: []string{`AGENTS.md has the heading "Git usage" outside managed blocks`}},
	},
}

// summaryScreen builds the summary screen of the fixture at the size.
func summaryScreen(t *testing.T, w, h int) *model {
	t.Helper()
	forceVisual(t, termenv.Ascii, true)
	m := &model{target: "~/.config/opencode", set: UnicodeSet, screen: screenSummary, applySummary: &summaryFixture}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return mm.(*model)
}

func TestApplySummaryScreen(t *testing.T) {
	m := summaryScreen(t, 120, 30)
	view := m.View()
	if !strings.Contains(view, "Apply to ~/.config/opencode") || !strings.Contains(view, "3 items · 4 writes · 1 excluded") {
		t.Errorf("the header misses the target or the counts:\n%s", view)
	}
	for _, want := range []string{
		"install  review 0.3.0",
		"+ agents/review.md",
		"+ AGENTS.md",
		"section team-opencode/review",
		"update   done-v2 1.2.0 → 1.3.0",
		"~ plugin/done.ts",
		"= plugin/README.md",
		"remove   old-lint 0.2.0",
		"- agents/old-lint.md",
		"! agents/old-lint-notes.md",
		"kept and released: modified by the user",
		"excluded",
		"✗ git-via-gh",
		"enter apply · ↑↓ scroll · esc back",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the summary misses %q:\n%s", want, view)
		}
	}
	// The kit names sit flush to the right edge of the item lines.
	for _, want := range []string{"team-opencode", "personal-opencode"} {
		if !strings.Contains(view, want) {
			t.Errorf("the summary misses the kit %q:\n%s", want, view)
		}
	}

	// The body scrolls: down moves the offset one row, pgdn a page, pgup
	// back to the top.
	m2 := summaryScreen(t, 120, 16)
	mm, _ := m2.Update(key("down"))
	if mm.(*model).summaryOffset != 1 {
		t.Fatalf("after down: offset %d, want 1", mm.(*model).summaryOffset)
	}
	mm, _ = mm.Update(key("pgdn"))
	if mm.(*model).summaryOffset <= 1 {
		t.Fatalf("after pgdn: offset %d, want a paged window", mm.(*model).summaryOffset)
	}
	mm, _ = mm.Update(key("pgup"))
	if mm.(*model).summaryOffset != 0 {
		t.Fatalf("after pgup: offset %d, want 0", mm.(*model).summaryOffset)
	}
}

// TestApplySummaryNothingToApply pins the summary whose every pending item
// is excluded: enter is unavailable and the footer reads "nothing to apply"
// (10.1).
func TestApplySummaryNothingToApply(t *testing.T) {
	m := summaryScreen(t, 120, 30)
	m.applySummary = &applySummary{excluded: []summaryExcluded{{id: "git-via-gh", reasons: []string{"reason"}}}}
	m.pending = &pendingApply{resets: map[string]bool{}}
	view := m.View()
	if !strings.Contains(view, "nothing to apply") || !strings.Contains(view, "1 excluded") {
		t.Errorf("the all-excluded summary:\n%s", view)
	}
	mm, _ := m.Update(key("enter"))
	if mm.(*model).screen != screenSummary {
		t.Fatal("enter applied a summary with nothing to apply")
	}
	mm, _ = mm.Update(key("esc"))
	if mm.(*model).screen != screenList {
		t.Fatalf("screen after esc = %v, want the tree", mm.(*model).screen)
	}
}

// progressFixture is the running apply of the 10.2 mockup: two steps done,
// the third under the spinner.
var progressFixture = []applyStep{
	{key: "team-opencode/review", id: "review", verb: verbInstall, version: "0.3.0", state: stepDone},
	{key: "personal-opencode/done-v2", id: "done-v2", verb: verbUpdate, version: "1.3.0", state: stepDone},
	{key: "team-opencode/old-lint", id: "old-lint", verb: verbRemove, version: "0.2.0", state: stepRunning},
}

func TestApplyProgress(t *testing.T) {
	forceVisual(t, termenv.Ascii, true)
	m := &model{target: "~/.config/opencode", set: UnicodeSet, screen: screenProgress, progress: progressFixture}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	view := mm.(*model).View()
	for _, want := range []string{
		"Applying to ~/.config/opencode",
		"2 of 3",
		"✓ install  review 0.3.0",
		"✓ update   done-v2 1.3.0",
		"⠋ remove   old-lint 0.2.0",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the progress view misses %q:\n%s", want, view)
		}
	}
}

// resultsFixture is the finished apply of the 10.3 mockup.
var resultsFixture = &applyOutcome{
	steps: []applyStep{
		{key: "team-opencode/review", id: "review", verb: verbInstall, version: "0.3.0", state: stepDone},
		{key: "personal-opencode/done-v2", id: "done-v2", verb: verbUpdate, version: "1.3.0", state: stepDone},
		{key: "team-opencode/old-lint", id: "old-lint", verb: verbRemove, version: "0.2.0", state: stepDone,
			kept: []plan.KeptNote{{Path: "agents/old-lint-notes.md", Reason: "modified by the user"}}},
	},
}

func TestApplyResults(t *testing.T) {
	forceVisual(t, termenv.Ascii, true)
	m := &model{target: "~/.config/opencode", set: UnicodeSet, screen: screenResults, outcome: resultsFixture}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	view := mm.(*model).View()
	for _, want := range []string{
		"Applied to ~/.config/opencode",
		"3 done · 0 failed",
		"✓ installed  review 0.3.0",
		"✓ updated    done-v2 1.3.0",
		"✓ removed    old-lint",
		"agents/old-lint-notes.md kept: modified by the user",
		"enter back to the list",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the result view misses %q:\n%s", want, view)
		}
	}
}

// confirmAll confirms the summary and returns the model with the run's
// command still in hand, so the test executes the apply itself.
func confirmAll(t *testing.T, m tea.Model) (*model, tea.Cmd) {
	t.Helper()
	mm, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("confirming the summary returned no command")
	}
	return mm.(*model), cmd
}

// runApplyCmd executes the run's command and drains the events it sent the
// way the runtime delivers them, handing each to visit together with the
// updated model. The apply runs to completion before the first event is
// delivered — the channel buffers them — so the visits see the order of the
// steps, not their wall-clock timing.
func runApplyCmd(t *testing.T, m *model, cmd tea.Cmd, visit func(m *model, msg tea.Msg)) {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("confirming the summary returned no batch of commands")
	}
	if len(batch) != 3 {
		t.Fatalf("the batch holds %d commands, want the run, the listener and the tick", len(batch))
	}
	done := batch[0]()
	for {
		v, ok := <-m.applyCh
		if !ok {
			break
		}
		mm, _ := m.Update(v)
		visit(mm.(*model), v)
	}
	dm, ok := done.(applyDoneMsg)
	if !ok {
		t.Fatalf("the run returned %T, want the outcome", done)
	}
	mm, _ := m.Update(dm)
	visit(mm.(*model), dm)
}

// TestApplyProgressStepping pins the stepping: the queue reaches the view
// before the steps, the start of a step lights its line, its result settles
// it, and the finished run lands on the results with every item applied.
func TestApplyProgressStepping(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = driveAll(t, m, "enter", "enter", "down", "space", "down", "space", "answered", "enter", "a")
	lm, cmd := confirmAll(t, m)

	var started, settled int
	runApplyCmd(t, lm, cmd, func(m *model, msg tea.Msg) {
		switch msg := msg.(type) {
		case []applyStep:
			if len(msg) != 2 || msg[0].key != "ex/good" || msg[1].key != "ex/varitem" {
				t.Fatalf("queue = %+v, want the two steps in order", msg)
			}
			for _, st := range msg {
				if st.state != stepWaiting {
					t.Fatalf("queued step %s = %v, want waiting", st.key, st.state)
				}
			}
		case apply.StepEvent:
			if msg.Start {
				started++
				if m.progress[started-1].state != stepRunning {
					t.Fatalf("step %d = %v, want running after its start", started, m.progress[started-1].state)
				}
				return
			}
			settled++
			if m.progress[settled-1].state != stepDone {
				t.Fatalf("step %d = %v, want done after its result", settled, m.progress[settled-1].state)
			}
		case applyDoneMsg:
			if m.screen != screenResults {
				t.Fatalf("screen after the run = %v, want results", m.screen)
			}
			for _, want := range []string{"installed  good 1.0.0", "installed  varitem 0.1.0"} {
				if !strings.Contains(m.resultText(), want) {
					t.Errorf("the results miss %q:\n%s", want, m.resultText())
				}
			}
			if header := m.resultsFrame(100).Header; !strings.Contains(header, "2 done · 0 failed") {
				t.Errorf("the result header = %q, want the counts", header)
			}
		}
	})
	if started != 2 || settled != 2 {
		t.Fatalf("events = %d starts, %d results; want two of each", started, settled)
	}
}

// TestApplyExcludesAtPerformTime pins the ✗ side of the stepping: the run
// plans again from the target of that moment, an item the fresh planning
// excludes runs no step and joins the results with its reason, and the item
// that runs lands.
func TestApplyExcludesAtPerformTime(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeSectionKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = driveAll(t, m, "enter", "enter", "down", "space", "down", "down", "space", "answered", "enter", "a")
	lm, cmd := confirmAll(t, m)

	// The run re-plans from the target of that moment: the shared file now
	// holds the heading the first item's fragment would duplicate, and the
	// planning refuses it as user content. The target directory exists only
	// once something writes to it; the planted file creates it.
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "AGENTS.md"), []byte("notes\n\n## One rules\n\nhand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runApplyCmd(t, lm, cmd, func(m *model, msg tea.Msg) {
		switch msg := msg.(type) {
		case []applyStep:
			if len(msg) != 1 || msg[0].key != "ex/varitem" {
				t.Fatalf("queue = %+v, want the varitem alone", msg)
			}
		case applyDoneMsg:
			if m.screen != screenResults {
				t.Fatalf("screen after the run = %v, want results", m.screen)
			}
			text := m.resultText()
			for _, want := range []string{
				"excluded   one",
				"already has a heading",
				"installed  varitem 0.1.0",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("the results miss %q:\n%s", want, text)
				}
			}
			if header := m.resultsFrame(100).Header; !strings.Contains(header, "1 done · 1 failed") {
				t.Errorf("the result header = %q, want the counts", header)
			}
		}
	})
	// The excluded item wrote no block; the applied one landed its block.
	data, err := os.ReadFile(filepath.Join(target, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "penates:begin ex/one") || !strings.Contains(string(data), "penates:begin ex/varitem") {
		t.Errorf("AGENTS.md after the apply:\n%s", data)
	}
}

// TestApplyFailsBeforeTheSteps pins the failure that stops the run whole:
// the results show the failure instead of the counts, and nothing landed.
func TestApplyFailsBeforeTheSteps(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = driveAll(t, m, "enter", "enter", "down", "space", "a")
	lm, cmd := confirmAll(t, m)

	// The kit loses the item's source before the run: the perform-time
	// planning cannot read what the summary showed, and the run stops
	// before any step.
	if err := os.Remove(filepath.Join(kitDir, "items", "good", "skill", "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	runApplyCmd(t, lm, cmd, func(m *model, msg tea.Msg) {
		if _, queued := msg.([]applyStep); queued {
			t.Fatal("a failed run queued steps")
		}
		if _, ok := msg.(applyDoneMsg); !ok {
			return
		}
		if m.screen != screenResults {
			t.Fatalf("screen after the run = %v, want results", m.screen)
		}
		if !strings.Contains(m.resultText(), "apply failed:") {
			t.Errorf("the results miss the failure:\n%s", m.resultText())
		}
		if header := m.resultsFrame(100).Header; strings.Contains(header, "done") {
			t.Errorf("the header counts a run that never started: %q", header)
		}
	})
}

// TestStepEventFailsTheStep pins the ✗ of the progress view: the step whose
// result carries an error settles its line as failed, and the view marks it
// with the cross.
func TestStepEventFailsTheStep(t *testing.T) {
	forceVisual(t, termenv.Ascii, true)
	m := &model{target: "~/.config/opencode", set: UnicodeSet, screen: screenProgress,
		progress: []applyStep{{key: "ex/good", id: "good", verb: verbInstall, version: "1.0.0", state: stepWaiting}}}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm, _ = mm.Update(apply.StepEvent{Key: "ex/good", Start: true})
	lm := mm.(*model)
	if lm.progress[0].state != stepRunning {
		t.Fatalf("after the start: %v, want running", lm.progress[0].state)
	}
	mm, _ = lm.Update(apply.StepEvent{Key: "ex/good", Err: errors.New("write failed")})
	lm = mm.(*model)
	if lm.progress[0].state != stepFailed || lm.progress[0].err != "write failed" {
		t.Fatalf("after the result: %+v, want failed with the error", lm.progress[0])
	}
	if view := lm.View(); !strings.Contains(view, "✗ install  good 1.0.0") {
		t.Errorf("the progress view misses the cross:\n%s", view)
	}
}

// TestProgressKeys pins 10.2: the view cannot be left by navigation while
// the items are being applied, and ctrl+c stays a plain quit — the next
// start reconciles the interruption.
func TestProgressKeys(t *testing.T) {
	dataDir := t.TempDir()
	kitDir := writeFixtureKit(t, filepath.Join(t.TempDir(), "target"))

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	m := resize(t, app.Initial(kitDir), 100, 30)
	m = driveAll(t, m, "enter", "enter", "down", "space", "a")
	mm, _ := m.Update(key("enter"))
	lm := mm.(*model)
	if lm.screen != screenProgress {
		t.Fatalf("screen after confirm = %v, want progress", lm.screen)
	}
	for _, k := range []string{"down", "up", "q", "a", "t"} {
		next, _ := lm.Update(key(k))
		if next != tea.Model(lm) {
			t.Fatalf("the %s key moved the model off the progress view", k)
		}
		if next.(*model).screen != screenProgress {
			t.Fatalf("the %s key left the progress view", k)
		}
	}
	// The double ctrl+c of 12.1 stays available on the progress view: the
	// first press arms the quit, a key in between disarms it, the second
	// quits at once.
	next, _ := lm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	lm = next.(*model)
	if lm.screen != screenProgress || !lm.ctrlcArmed {
		t.Fatalf("the first ctrl+c did not arm the quit: screen %v, armed %v", lm.screen, lm.ctrlcArmed)
	}
	next, _ = lm.Update(key("down"))
	if next.(*model).ctrlcArmed {
		t.Fatal("a key in between did not disarm the quit")
	}
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next, cmd := next.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Fatalf("the second ctrl+c returned %v, want the quit", msg)
	}
}

// TestRecordOnExit pins 10.4: the changes applied during the session
// accumulate into the record the command prints on exit — installed and
// updated with the versions they moved between, a removal with the files it
// kept — and nothing is recorded for a session that applied nothing.
func TestRecordOnExit(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeFixtureKit(t, target)

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(stateStore, lock.NewStore(dataDir))

	// Nothing applied yet: nothing prints.
	m := resize(t, app.Initial(kitDir), 100, 30)
	if rec := ExitRecord(m); rec != "" {
		t.Fatalf("the empty session record = %q", rec)
	}

	// One session, three applies: install the good item, publish an update
	// and apply it, then remove the item over a user modification. The
	// record keeps every change of the session; entering the target again
	// reloads the kits without touching it.
	m = driveAll(t, m, "enter", "enter", "down", "space", "a", "enter", "enter")
	lm := m.(*model)
	if !strings.Contains(lm.resultText(), "installed  good 1.0.0") {
		t.Fatalf("install:\n%s", lm.resultText())
	}
	bumpKit(t, kitDir, "1.1.0", "good skill v2\n")
	lm.enterTarget(lm.target)
	m = driveAll(t, lm, "a", "enter", "enter")
	if !strings.Contains(lm.resultText(), "updated    good 1.1.0") {
		t.Fatalf("update:\n%s", lm.resultText())
	}
	if err := os.WriteFile(filepath.Join(target, "skills", "good", "SKILL.md"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = driveAll(t, m, "down", "d", "a", "enter")
	if !strings.Contains(lm.resultText(), "removed    good") {
		t.Fatalf("removal:\n%s", lm.resultText())
	}

	want := "penates: applied to " + target + "\n" +
		"  installed  ex/good 1.0.0\n" +
		"  updated    ex/good 1.0.0 → 1.1.0\n" +
		"  removed    ex/good (kept: skills/good/SKILL.md)\n"
	if rec := ExitRecord(m); rec != want {
		t.Errorf("record =\n%q\nwant\n%q", rec, want)
	}
}
