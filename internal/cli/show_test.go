package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// show <kit/item>: the item's details, one line per fact, the sections of
// the mockup contiguous — identity, state with the pending action, the
// declared facts, the changelog verbatim.

func TestShowAProvidedItem(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", target)
	e.activate(target)
	e.saveLock(target, map[string]*lock.Item{
		"personal/writing": {Version: "0.9.0"},
	})

	stdout, stderr, code := execute(t, testBuild, "show", "personal/writing", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	want := strings.Join([]string{
		"personal/writing",
		"  status  update-available  0.9.0 → 1.0.0  major",
		"  description  Text editing skill",
		"  tags  writing",
		"  contributions",
		"    file  skill/SKILL.md → skills/writing/SKILL.md",
		"  changelog",
		"## 1.0.0",
		"Adds the writing skill.",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// A staged pending action shows as the verb the apply would run.
func TestShowThePendingAction(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", target)
	e.activate(target)
	e.saveLock(target, map[string]*lock.Item{
		"personal/writing": {Version: "0.9.0"},
	})
	if err := e.states.Update(func(reg *state.Registry) error {
		reg.Pending[target] = &state.Pending{
			Items: map[string]*state.ItemPending{
				"personal/writing": {Select: true},
			},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := execute(t, testBuild, "show", "personal/writing", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "  pending  update\n") {
		t.Errorf("stdout = %q, want the pending update line", stdout)
	}
}

// The saved answers of the lock show beside the variables, the required
// mark with them, a dash without an answer.
func TestShowTheVariables(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.writeKit("vars", map[string]string{
		"kit.yaml":               "name: vars\n",
		"items/needed/item.yaml": "id: needed\nversion: 1.0.0\ndescription: Needs a path\nvars:\n  - id: source_path\n    prompt: Where the source lives\n    required: true\n  - id: flavor\n    prompt: Which flavor\ncontributes:\n  - file: body.md\n    to: docs/needed.md\n    template: true\n",
		"items/needed/body.md":   "source at {{source_path}}, flavor {{flavor}}\n",
	})
	e.register("vars", dir)
	e.attach("vars", target)
	e.activate(target)
	e.saveLock(target, map[string]*lock.Item{
		"vars/needed": {Version: "1.0.0", Answers: map[string]string{"source_path": "/src"}},
	})

	stdout, _, code := execute(t, testBuild, "show", "vars/needed", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := strings.Join([]string{
		"vars/needed",
		"  status  installed  1.0.0",
		"  description  Needs a path",
		"  variables",
		"    source_path  /src  required",
		"    flavor  —",
		"  contributions",
		"    file  body.md → docs/needed.md",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// An item the lock alone holds shows the state line and nothing else: an
// orphan from a kit gone, an unavailable item from a kit that did not
// load.
func TestShowTheFrozenItems(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.register("broken", filepath.Join(e.base, "kits", "broken"))
	e.attach("personal", target)
	e.attach("broken", target)
	e.activate(target)
	e.saveLock(target, map[string]*lock.Item{
		"gone-kit/stray": {Version: "1.1.0"},
		"broken/legacy":  {Version: "0.4.0"},
	})

	stdout, _, code := execute(t, testBuild, "show", "gone-kit/stray", "--target", target)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if want := "gone-kit/stray\n  status  orphan  1.1.0\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	stdout, _, code = execute(t, testBuild, "show", "broken/legacy", "--target", target)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if want := "broken/legacy\n  status  unavailable  0.4.0\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// An item neither kit nor lock holds: not_found, the next step named.
func TestShowAnUnknownItem(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", target)
	e.activate(target)

	_, stderr, code := execute(t, testBuild, "show", "personal/nothing", "--target", target)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "Error [not_found]: no kit attached to " + target + ` provides "personal/nothing" and it is not installed (run "penates status" to see what is)` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// An argument that does not split is not an identity: usage.
func TestShowAMalformedArgument(t *testing.T) {
	newTestEnv(t)

	_, stderr, code := execute(t, testBuild, "show", "justanid")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if want := `Error [usage]: "justanid" does not name an item as "kit/item" (run "penates show --help")` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
