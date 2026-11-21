package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/state"
)

// executeOptions runs the command tree with the given options and
// returns what the process would have written and exited with.
func executeOptions(t *testing.T, opts Options, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(testBuild, opts, args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// stubTUI is the TUI runner the confirmation tests pin.
func stubTUI(io.Writer) error { return nil }

// The parity scenarios of the mutating item commands: install, update,
// remove, reset, apply, pending clear — the summary, the confirmation,
// the run, the result, the refusals, the exit codes. Every fixture is a
// temp kit and a temp target under the test's XDG_CONFIG_HOME.

// executeAsk runs the command tree with the given stdin and the answer
// to "does stdin hold a terminal" — the confirmation tests' harness.
func executeAsk(t *testing.T, stdin string, terminal bool, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return executeOptions(t, Options{
		TUI:        stubTUI,
		Stdin:      strings.NewReader(stdin),
		IsTerminal: func() bool { return terminal },
	}, args...)
}

// mutateKit writes the kit the mutating tests read: a file item, a
// section item into a shared file, and — when asked — a templated item
// with a variable.
func (e *testEnv) mutateKit(name string) string {
	e.t.Helper()
	return e.writeKit(name, map[string]string{
		"kit.yaml":                     fmt.Sprintf("name: %s\ndescription: Test kit\nshared:\n  - AGENTS.md\n", name),
		"items/writing/item.yaml":      "id: writing\nversion: 1.0.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
		"items/writing/skill/SKILL.md": "# writing\n",
		"items/greeting/item.yaml":     "id: greeting\nversion: 1.0.0\ncontributes:\n  - section: note.md\n    to: AGENTS.md\n",
		"items/greeting/note.md":       "Greets.\n",
	})
}

// readyKit registers the kit of that name, attaches it to a fresh target
// and hands both back — the surroundings every mutating command assumes.
func (e *testEnv) readyKit(name string) (dir, target string) {
	e.t.Helper()
	dir = e.mutateKit(name)
	target = filepath.Join(e.base, "harness")
	e.register(name, dir)
	e.attach(name, target)
	return dir, target
}

// TestInstallAppliesOneItem pins parity scenario 4 in its simplest form:
// the summary with the target line first, the confirmation answered by
// --yes, the file in the target, the lock recording it, the result with
// the past verb and the counts.
func TestInstallAppliesOneItem(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")

	stdout, stderr, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"target " + target,
		"",
		"install  personal/writing  1.0.0",
		"  + skills/writing/SKILL.md",
		"",
		"1 write",
		"",
		"installed  personal/writing  1.0.0",
		"",
		"1 applied",
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	data, err := os.ReadFile(filepath.Join(target, "skills", "writing", "SKILL.md"))
	if err != nil {
		t.Fatalf("the item file did not land: %v", err)
	}
	if string(data) != "# writing\n" {
		t.Errorf("installed content = %q", data)
	}
	lk, err := e.locks.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	rec := lk.Items["personal/writing"]
	if rec == nil || rec.Version != "1.0.0" {
		t.Errorf("lock record = %+v, want personal/writing at 1.0.0", rec)
	}
}

// TestInstallSeveralItems installs both items of the kit in one run.
func TestInstallSeveralItems(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")

	stdout, stderr, code := execute(t, testBuild, "install", "personal/writing", "personal/greeting", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"install  personal/writing  1.0.0",
		"install  personal/greeting  1.0.0",
		"  + AGENTS.md  section personal/greeting",
		"2 writes",
		"installed  personal/writing  1.0.0",
		"installed  personal/greeting  1.0.0",
		"2 applied",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
}

// TestInstallAlreadyInstalledIsKept pins the empty success: an item
// installed at an equal version is kept with the planner's reason, exit
// 0, nothing written.
func TestInstallAlreadyInstalledIsKept(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the first install exited %d", code)
	}

	stdout, stderr, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"update  personal/writing  1.0.0",
		"  = skills/writing/SKILL.md  kept: already installed",
		"0 writes",
		"kept  personal/writing  1.0.0  already installed",
		"1 kept",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

// TestInstallDowngrade pins the CLI's way to downgrade: install of a
// version below the installed one applies, named as a downgrade in the
// summary and the result.
func TestInstallDowngrade(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/writing/item.yaml"):      "id: writing\nversion: 0.9.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
		filepath.Join(dir, "items/writing/skill/SKILL.md"): "# writing, older\n",
	})

	stdout, stderr, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"downgrade  personal/writing  1.0.0 → 0.9.0",
		"  ~ skills/writing/SKILL.md",
		"downgraded  personal/writing  1.0.0 → 0.9.0",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	data, _ := os.ReadFile(filepath.Join(target, "skills", "writing", "SKILL.md"))
	if string(data) != "# writing, older\n" {
		t.Errorf("the downgrade did not write the older content: %q", data)
	}
}

// TestUpdateAllAvailable pins scenario 5: without arguments the command
// updates every item whose kit offers a higher version.
func TestUpdateAllAvailable(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/writing/item.yaml"):      "id: writing\nversion: 1.1.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
		filepath.Join(dir, "items/writing/skill/SKILL.md"): "# writing, newer\n",
	})

	stdout, stderr, code := execute(t, testBuild, "update", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"update  personal/writing  1.0.0 → 1.1.0",
		"updated  personal/writing  1.0.0 → 1.1.0",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
}

// TestUpdateNothingAvailable pins the empty success of an update with
// nothing to update.
func TestUpdateNothingAvailable(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}

	stdout, stderr, code := execute(t, testBuild, "update", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "nothing to update\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
}

// TestUpdateNamedWithoutUpdateIsKept: a named item whose version is
// current plans no action and is kept, exit 0.
func TestUpdateNamedWithoutUpdateIsKept(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}

	stdout, _, code := execute(t, testBuild, "update", "personal/writing", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "kept  personal/writing  1.0.0  already installed") {
		t.Errorf("stdout lacks the kept line:\n%s", stdout)
	}
}

// TestUpdateNamedDowngradeRefused pins the never-downgrades rule: a
// named item whose kit version is below the installed one is a wrong
// command line, and install is named as the way.
func TestUpdateNamedDowngradeRefused(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/writing/item.yaml"): "id: writing\nversion: 0.9.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
	})

	_, stderr, code := execute(t, testBuild, "update", "personal/writing", "--target", target, "--yes")

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	want := "Error [usage]: personal/writing 0.9.0 is below the installed 1.0.0; downgrade with \"penates install personal/writing\"\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestUpdateNamedNotInstalledRefused: an update names an installed item.
func TestUpdateNamedNotInstalledRefused(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")

	_, stderr, code := execute(t, testBuild, "update", "personal/writing", "--target", target, "--yes")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	want := "Error [not_found]: personal/writing is not installed (install it with \"penates install personal/writing\")\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestUpdateMajorMarksAndPrintsChangelog pins the major update: the
// summary carries the major suffix and the item's CHANGELOG.md under the
// `changelog <kit/item> <version>` heading — on the run and on the
// dry-run both.
func TestUpdateMajorMarksAndPrintsChangelog(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/writing/item.yaml"):      "id: writing\nversion: 2.0.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
		filepath.Join(dir, "items/writing/skill/SKILL.md"): "# writing, rewritten\n",
		filepath.Join(dir, "items/writing/CHANGELOG.md"):   "## 2.0.0\nBig rewrite.\n",
	})

	// The dry-run previews the changelog as it would print; then the run
	// applies the same plan.
	for _, dry := range []bool{true, false} {
		args := []string{"update", "personal/writing", "--target", target, "--yes"}
		if dry {
			args = append(args, "--dry-run")
		}
		stdout, stderr, code := execute(t, testBuild, args...)
		if code != 0 {
			t.Fatalf("dry=%v: exit code = %d, want 0 (stderr: %s)", dry, code, stderr)
		}
		for _, want := range []string{
			"update  personal/writing  1.0.0 → 2.0.0  major",
			"changelog personal/writing 2.0.0",
			"## 2.0.0",
			"Big rewrite.",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("dry=%v: stdout lacks %q:\n%s", dry, want, stdout)
			}
		}
	}
}

// TestResetModifiedItem pins scenario 6: reset to kit overwrites the
// content the user modified, on an explicitly named item.
func TestResetModifiedItem(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	e.writeFiles(map[string]string{filepath.Join(target, "skills/writing/SKILL.md"): "# my own\n"})

	stdout, stderr, code := execute(t, testBuild, "reset", "personal/writing", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"reset  personal/writing  1.0.0",
		"  ~ skills/writing/SKILL.md",
		"reset  personal/writing  1.0.0",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	data, _ := os.ReadFile(filepath.Join(target, "skills", "writing", "SKILL.md"))
	if string(data) != "# writing\n" {
		t.Errorf("the reset did not restore the kit content: %q", data)
	}
}

// TestResetHealthyItemIsAllowed pins the CLI review rule: any installed
// item may be named, the explicit name is the review.
func TestResetHealthyItemIsAllowed(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}

	stdout, stderr, code := execute(t, testBuild, "reset", "personal/writing", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "reset  personal/writing  1.0.0") {
		t.Errorf("stdout lacks the reset line:\n%s", stdout)
	}
}

// TestResetUninstalledRefused pins the not_found of a reset naming an
// item that is not installed.
func TestResetUninstalledRefused(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")

	_, stderr, code := execute(t, testBuild, "reset", "personal/writing", "--target", target, "--yes")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	want := "Error [not_found]: personal/writing is not installed (run \"penates status\" to see what is)\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestResetBuiltin pins the built-in reset: an in-step directory is the
// empty success `nothing to reset`, a modified one is reset to the
// embedded content — through the summary, the confirmation, the result.
func TestResetBuiltin(t *testing.T) {
	e := newTestEnv(t)
	// The startup sync runs with the first command and unpacks the
	// built-in kit in step.
	if _, _, code := execute(t, testBuild, "kit", "list"); code != 0 {
		t.Fatalf("kit list exited %d", code)
	}
	kitYaml := filepath.Join(e.dataDir, "builtin-kit", "kit.yaml")

	if _, stderr, code := execute(t, testBuild, "reset", "--builtin", "--yes"); code != 0 {
		t.Fatalf("nothing-to-reset exit = %d, want 0 (stderr: %s)", code, stderr)
	} else if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}

	// The user edits the managed directory: the sync reports it modified,
	// and the reset takes the edit back.
	edit := "name: penates\nedited locally\n"
	if err := os.WriteFile(kitYaml, []byte(edit), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := execute(t, testBuild, "reset", "--builtin", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "reset  penates  "+filepath.Join(e.dataDir, "builtin-kit")) {
		t.Errorf("the dry-run summary lacks the reset line:\n%s", stdout)
	}
	if _, _, code = execute(t, testBuild, "reset", "--builtin", "--yes"); code != 0 {
		t.Fatalf("reset exit = %d, want 0", code)
	}
	data, err := os.ReadFile(kitYaml)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "edited locally") {
		t.Errorf("the reset left the local edit in place:\n%s", data)
	}
}

// TestRemoveInstalledOrphanAndUnavailable pins scenario 7: an installed
// item, an orphan the kit stopped providing, and an item of a kit that
// cannot be read all remove; a name the lock does not hold is a
// not_found.
func TestRemoveInstalledOrphanAndUnavailable(t *testing.T) {
	t.Run("installed", func(t *testing.T) {
		e := newTestEnv(t)
		_, target := e.readyKit("personal")
		if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
			t.Fatalf("the install exited %d", code)
		}

		stdout, stderr, code := execute(t, testBuild, "remove", "personal/writing", "--target", target, "--yes")

		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		for _, want := range []string{
			"remove  personal/writing  1.0.0",
			"  - skills/writing/SKILL.md",
			"removed  personal/writing",
			"1 applied",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout)
			}
		}
		if _, err := os.Stat(filepath.Join(target, "skills/writing/SKILL.md")); !os.IsNotExist(err) {
			t.Errorf("the file survived the removal: %v", err)
		}
		lk, _ := e.locks.Load(target)
		if _, held := lk.Items["personal/writing"]; held {
			t.Error("the lock still records the removed item")
		}
	})

	t.Run("orphan", func(t *testing.T) {
		e := newTestEnv(t)
		dir, target := e.readyKit("personal")
		if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
			t.Fatalf("the install exited %d", code)
		}
		// The kit stops providing the item: the directory goes.
		if err := os.RemoveAll(filepath.Join(dir, "items", "writing")); err != nil {
			t.Fatal(err)
		}

		stdout, stderr, code := execute(t, testBuild, "remove", "personal/writing", "--target", target, "--yes")

		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stdout, "removed  personal/writing") {
			t.Errorf("stdout lacks the removed line:\n%s", stdout)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want nothing", stderr)
		}
	})

	t.Run("a kit that cannot be read", func(t *testing.T) {
		e := newTestEnv(t)
		dir, target := e.readyKit("personal")
		if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
			t.Fatalf("the install exited %d", code)
		}
		if err := os.Remove(filepath.Join(dir, "kit.yaml")); err != nil {
			t.Fatal(err)
		}

		stdout, stderr, code := execute(t, testBuild, "remove", "personal/writing", "--target", target, "--yes")

		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stdout, "removed  personal/writing") {
			t.Errorf("stdout lacks the removed line:\n%s", stdout)
		}
	})

	t.Run("uninstalled is not_found", func(t *testing.T) {
		e := newTestEnv(t)
		_, target := e.readyKit("personal")

		_, stderr, code := execute(t, testBuild, "remove", "personal/writing", "--target", target, "--yes")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		want := "Error [not_found]: personal/writing is not installed (run \"penates status\" to see what is)\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

// TestRemoveKeepsModifiedContent pins the removal's kept report: a file
// the user modified stays in the target, is released from the lock, and
// the summary and the result both say so.
func TestRemoveKeepsModifiedContent(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	e.writeFiles(map[string]string{filepath.Join(target, "skills/writing/SKILL.md"): "# my own\n"})

	stdout, stderr, code := execute(t, testBuild, "remove", "personal/writing", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"  ! skills/writing/SKILL.md  kept and released: modified by the user",
		"  skills/writing/SKILL.md kept: modified by the user",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	data, _ := os.ReadFile(filepath.Join(target, "skills", "writing", "SKILL.md"))
	if string(data) != "# my own\n" {
		t.Errorf("the modified content did not stay: %q", data)
	}
}

// TestOwnershipConflictsAreConflictErrors pins scenario 8: every cause
// of an ownership refusal excludes the item as a conflict, the stderr
// line carries the category, the prose names the place, and the exit is
// 1 — with the report on stdout, not an Error line for the command.
func TestOwnershipConflictsAreConflictErrors(t *testing.T) {
	t.Run("user content in the path", func(t *testing.T) {
		e := newTestEnv(t)
		_, target := e.readyKit("personal")
		e.writeFiles(map[string]string{filepath.Join(target, "skills/writing/SKILL.md"): "# mine\n"})

		stdout, stderr, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stdout, "excluded  personal/writing") || !strings.Contains(stdout, "0 writes") {
			t.Errorf("stdout lacks the exclusion:\n%s", stdout)
		}
		want := "Error [conflict]: personal/writing: skills/writing/SKILL.md exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("owned by another installed item", func(t *testing.T) {
		e := newTestEnv(t)
		dir, target := e.readyKit("personal")
		e.writeFiles(map[string]string{
			filepath.Join(dir, "items/second/item.yaml"): "id: second\nversion: 1.0.0\ncontributes:\n  - file: s.md\n    to: skills/writing/SKILL.md\n",
			filepath.Join(dir, "items/second/s.md"):      "# second\n",
		})
		if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
			t.Fatalf("the install exited %d", code)
		}

		stdout, stderr, code := execute(t, testBuild, "install", "personal/second", "--target", target, "--yes")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stdout, "excluded  personal/second") {
			t.Errorf("stdout lacks the exclusion:\n%s", stdout)
		}
		want := "Error [conflict]: personal/second: skills/writing/SKILL.md is owned by installed item personal/writing\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("modified content against changed kit content", func(t *testing.T) {
		e := newTestEnv(t)
		dir, target := e.readyKit("personal")
		if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
			t.Fatalf("the install exited %d", code)
		}
		e.writeFiles(map[string]string{filepath.Join(target, "skills/writing/SKILL.md"): "# my own\n"})
		e.writeFiles(map[string]string{
			filepath.Join(dir, "items/writing/item.yaml"):      "id: writing\nversion: 1.1.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
			filepath.Join(dir, "items/writing/skill/SKILL.md"): "# kit moved on\n",
		})

		stdout, stderr, code := execute(t, testBuild, "update", "--target", target, "--yes")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stdout, "excluded  personal/writing") {
			t.Errorf("stdout lacks the exclusion:\n%s", stdout)
		}
		want := "Error [conflict]: personal/writing: skills/writing/SKILL.md was modified after installation, and the kit provides different content\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("a duplicate heading", func(t *testing.T) {
		e := newTestEnv(t)
		dir, target := e.readyKit("personal")
		e.writeFiles(map[string]string{
			filepath.Join(dir, "items/greeting/note.md"): "# Greets\n\nGreets twice.\n",
			filepath.Join(target, "AGENTS.md"):           "# Greets\n\nThe user wrote it first.\n",
		})

		stdout, stderr, code := execute(t, testBuild, "install", "personal/greeting", "--target", target, "--yes")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stdout, "excluded  personal/greeting") {
			t.Errorf("stdout lacks the exclusion:\n%s", stdout)
		}
		want := "Error [conflict]: personal/greeting: AGENTS.md already has a heading \"greets\" outside managed blocks; the section would duplicate it\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

// TestStagedRefusal pins the refusal half of scenario 9: a one-shot
// command refuses while staged pending actions wait, naming them and the
// three ways out, with the same list one action per line under the Error
// line.
func TestStagedRefusal(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/greeting": {Select: true, Version: "1.0.0"},
		},
		Deferred: []state.Deferred{{Kind: state.DeferredDetachKit, Kit: "personal"}},
	})

	_, stderr, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes")

	if code != 1 {
		t.Fatalf("refusal exit code = %d, want 1", code)
	}
	want := fmt.Sprintf("Error [staged]: 2 pending actions are staged for %s: install personal/greeting, the deferred detach of personal (apply them with \"penates apply\", clear them with \"penates pending clear\", or ask the person who staged them)\n", target)
	if !strings.HasPrefix(stderr, want) {
		t.Errorf("stderr = %q, want prefix %q", stderr, want)
	}
	if !strings.Contains(stderr, "  install  personal/greeting  1.0.0\n  deferred  detach personal from "+target) {
		t.Errorf("stderr lacks the detail lines:\n%s", stderr)
	}
}

// TestApplyConsumesStaged pins the apply half of scenario 9: the staged
// selections apply with the answers they were given, nothing is asked
// beyond the one confirmation, and the deferred operations complete with
// the report — a detach whose kit lost its last installed item lands.
func TestApplyConsumesStaged(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/writing": {Remove: true},
		},
		Deferred: []state.Deferred{{Kind: state.DeferredDetachKit, Kit: "personal"}},
	})

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"remove  personal/writing  1.0.0",
		"  - skills/writing/SKILL.md",
		"removed  personal/writing",
		"1 applied",
		"completed the deferred detach of personal from " + target,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	lk, _ := e.locks.Load(target)
	if len(lk.Items) != 0 {
		t.Errorf("the staged removal did not land: %+v", lk.Items)
	}
	if _, attached := personalAttachment(e, target); attached {
		t.Error("the deferred detach did not complete")
	}
}

// TestApplyInstallsStagedSelection pins the selection half of the apply:
// a staged selection of an uninstalled item installs it.
func TestApplyInstallsStagedSelection(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/greeting": {Select: true, Version: "1.0.0"},
		},
	})

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "installed  personal/greeting  1.0.0") {
		t.Errorf("stdout lacks the applied item:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	lk, _ := e.locks.Load(target)
	if _, held := lk.Items["personal/greeting"]; !held {
		t.Error("the staged selection did not land")
	}
}

// TestApplyDropsUncoverableDeferred pins the deferred report's other
// half: a deferred detach whose kit still holds installed items after
// the apply is dropped, with the reason in the report.
func TestApplyDropsUncoverableDeferred(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/greeting": {Select: true, Version: "1.0.0"},
		},
		Deferred: []state.Deferred{{Kind: state.DeferredDetachKit, Kit: "personal"}},
	})

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := "dropped the deferred detach of personal from " + target + ": installed items remain: personal/greeting, personal/writing"
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks the drop report:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if _, attached := personalAttachment(e, target); !attached {
		t.Error("the kit was detached although its items remain installed")
	}
}

// TestPendingClearDropsStaged pins the third way out: pending clear
// drops the staged actions without applying them, asked once like every
// mutating command.
func TestPendingClearDropsStaged(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/writing": {Remove: true},
		},
		Deferred: []state.Deferred{{Kind: state.DeferredForgetTarget}},
	})

	stdout, stderr, code := execute(t, testBuild, "pending", "clear", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"target " + target,
		"pending 1",
		"  remove  personal/writing",
		"  deferred  forget " + target,
		"cleared  2 pending actions for " + target,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	reg := registryOf(e)
	if _, held := reg.Pending[target]; held {
		t.Errorf("the pending actions survived the clear: %+v", reg.Pending[target])
	}
}

// TestApplyCompletesDeferredOnly pins the bridge rule for a pending
// that holds deferred operations alone: the deferred lines are the
// summary, one confirmation stands between them and the completion, and
// the report is the whole output — exit 0 when everything completed.
// The refusal of a one-shot names apply as the way out, so apply has to
// work here; a bare "nothing staged" would leave the agent in the
// deadlock the refusal itself points out of.
func TestApplyCompletesDeferredOnly(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	stage(e, target, &state.Pending{
		Deferred: []state.Deferred{{Kind: state.DeferredDetachKit, Kit: "personal"}},
	})

	// The dry-run previews and touches nothing.
	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"target " + target,
		"",
		"  deferred  detach personal from " + target,
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("dry-run stdout =\n%s\nwant\n%s", stdout, want)
	}
	if _, attached := personalAttachment(e, target); !attached {
		t.Error("the dry-run completed the detach")
	}

	stdout, stderr, code = execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"target " + target,
		"  deferred  detach personal from " + target,
		"completed the deferred detach of personal from " + target,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if _, attached := personalAttachment(e, target); attached {
		t.Error("the deferred detach did not complete")
	}
	if p := registryOf(e).Pending[target]; p != nil && len(p.Deferred) > 0 {
		t.Errorf("the deferred operation survived the completion: %+v", p.Deferred)
	}
}

// TestApplyDropsDeferredOnly pins the dropped half: a deferred forget
// over a target that still holds installed items is dropped with the
// reason, and the exit is 1 without an Error line — the report on
// stdout is the whole story.
func TestApplyDropsDeferredOnly(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	stage(e, target, &state.Pending{
		Deferred: []state.Deferred{{Kind: state.DeferredForgetTarget}},
	})

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	want := "dropped the deferred forget of " + target + ": installed items remain: personal/writing"
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks the drop report:\n%s", stdout)
	}
	if strings.Contains(stderr, "Error") {
		t.Errorf("stderr carries an Error line: %q", stderr)
	}
	if _, ok := registryOf(e).Targets[target]; !ok {
		t.Error("the target was forgotten although its items remain")
	}
}

// TestApplyCleansDroppedStagedActions pins the cleanup of a pending the
// restore-time check emptied: every staged action too stale to apply
// leaves the registry with its note, the run is the empty success it is,
// and a follow-up one-shot command works without pending clear.
func TestApplyCleansDroppedStagedActions(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/writing": {Select: true, Version: "0.9.0"},
		},
	})
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/writing/item.yaml"): "id: writing\nversion: 1.1.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
	})

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "nothing staged") {
		t.Errorf("stdout lacks the empty success:\n%s", stdout)
	}
	if !strings.Contains(stdout, "note  dropped the staged selection of personal/writing") {
		t.Errorf("stdout lacks the drop note:\n%s", stdout)
	}
	if p := registryOf(e).Pending[target]; p != nil {
		t.Errorf("the dropped actions stayed in the registry: %+v", p.Items)
	}

	// The one-shot command works without pending clear.
	_, stderr, code = execute(t, testBuild, "install", "personal/greeting", "--target", target, "--yes")
	if code != 0 {
		t.Fatalf("the follow-up install exited %d (stderr: %s)", code, stderr)
	}
}

// TestApplyDropsStaleStagedActions pins the restore-time check: a staged
// selection whose item moved to another version since drops with a note,
// and the rest applies.
func TestApplyDropsStaleStagedActions(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/writing":  {Select: true, Version: "0.9.0"},
			"personal/greeting": {Select: true, Version: "1.0.0"},
		},
	})
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/writing/item.yaml"): "id: writing\nversion: 1.1.0\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
	})

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "note  dropped the staged selection of personal/writing: the item moved to 1.1.0 since it was staged") {
		t.Errorf("stdout lacks the drop note:\n%s", stdout)
	}
	if !strings.Contains(stdout, "installed  personal/greeting  1.0.0") {
		t.Errorf("stdout lacks the applied item:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
}

// TestApplyNothingStaged pins the empty success of the bridge command.
func TestApplyNothingStaged(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "nothing staged\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
}

// TestPendingClearNothingStaged pins the empty success of the clear.
func TestPendingClearNothingStaged(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")

	stdout, _, code := execute(t, testBuild, "pending", "clear", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if want := "nothing staged\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestVariables pins scenario 10: a required variable without a value
// fails the command as usage naming the id, --set answers it, the lock's
// answer is reused, and an id no item declares is usage too.
func TestVariables(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/templated/item.yaml"): "id: templated\nversion: 1.0.0\nvars:\n  - id: name\n    prompt: Name\n    required: true\ncontributes:\n  - file: t.md\n    to: skills/templated/t.md\n    template: true\n",
		filepath.Join(dir, "items/templated/t.md"):      "Hello {{name}}.\n",
	})

	// Missing: the refusal names the id and the flag.
	_, stderr, code := execute(t, testBuild, "install", "personal/templated", "--target", target, "--yes")
	if code != 2 {
		t.Fatalf("missing-variable exit code = %d, want 2", code)
	}
	want := "Error [usage]: item personal/templated requires a value for name (rerun with --set name=<value>)\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}

	// --set answers it; the value lands in the file and the lock.
	_, stderr, code = execute(t, testBuild, "install", "personal/templated", "--set", "name=World", "--target", target, "--yes")
	if code != 0 {
		t.Fatalf("--set exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	data, _ := os.ReadFile(filepath.Join(target, "skills", "templated", "t.md"))
	if string(data) != "Hello World.\n" {
		t.Errorf("the substitution did not land: %q", data)
	}
	lk, _ := e.locks.Load(target)
	if got := lk.Items["personal/templated"].Answers["name"]; got != "World" {
		t.Errorf("lock answer = %q, want World", got)
	}

	// The recorded answer is reused: the update runs without --set.
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/templated/item.yaml"): "id: templated\nversion: 1.1.0\nvars:\n  - id: name\n    prompt: Name\n    required: true\ncontributes:\n  - file: t.md\n    to: skills/templated/t.md\n    template: true\n",
		filepath.Join(dir, "items/templated/t.md"):      "Hello again, {{name}}.\n",
	})
	if _, stderr, code = execute(t, testBuild, "update", "personal/templated", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the update re-asked a recorded variable (stderr: %s)", stderr)
	}
	if data, _ = os.ReadFile(filepath.Join(target, "skills", "templated", "t.md")); string(data) != "Hello again, World.\n" {
		t.Errorf("the reused answer did not land: %q", data)
	}

	// An id no selected item declares is a wrong command line.
	_, stderr, code = execute(t, testBuild, "install", "personal/greeting", "--set", "name=World", "--target", target, "--yes")
	if code != 2 {
		t.Fatalf("unknown-id exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, `Error [usage]: unknown variable "name"`) {
		t.Errorf("stderr = %q, want the unknown-variable refusal", stderr)
	}

	// A malformed --set is usage.
	_, stderr, code = execute(t, testBuild, "install", "personal/templated", "--set", "name", "--target", target, "--yes")
	if code != 2 {
		t.Fatalf("malformed --set exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, `Error [usage]: malformed --set "name"`) {
		t.Errorf("stderr = %q, want the malformed refusal", stderr)
	}

	// An unknown placeholder in the template is the kit's fault, not the
	// command line's: the item is excluded, the category is kit. The
	// version moves too, so the changed content is planned at all — an
	// equal-version change would only be kept with a warning.
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/templated/item.yaml"): "id: templated\nversion: 1.2.0\nvars:\n  - id: name\n    prompt: Name\n    required: true\ncontributes:\n  - file: t.md\n    to: skills/templated/t.md\n    template: true\n",
		filepath.Join(dir, "items/templated/t.md"):      "Hello {{name}} and {{nope}}.\n",
	})
	_, stderr, code = execute(t, testBuild, "install", "personal/templated", "--set", "name=World", "--target", target, "--yes")
	if code != 1 {
		t.Fatalf("unknown-placeholder exit code = %d, want 1", code)
	}
	want = "Error [kit]: personal/templated: t.md holds the unknown placeholder {{nope}}\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestApplyRunsRemovalsResetsAndSelection pins the mixed apply: the
// removals run first, the installs — a staged reset among them — plan
// again from the target as the removals leave it, and the pending
// actions settle.
func TestApplyRunsRemovalsResetsAndSelection(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "personal/greeting", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	e.writeFiles(map[string]string{filepath.Join(target, "skills/writing/SKILL.md"): "# my own\n"})
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/greeting": {Remove: true},
			"personal/writing":  {Reset: true, Version: "1.0.0"},
		},
	})

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"remove  personal/greeting  1.0.0",
		"  - AGENTS.md",
		"reset  personal/writing  1.0.0",
		"  ~ skills/writing/SKILL.md",
		"2 writes",
		"removed  personal/greeting",
		"reset  personal/writing  1.0.0",
		"2 applied",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	data, _ := os.ReadFile(filepath.Join(target, "skills", "writing", "SKILL.md"))
	if string(data) != "# writing\n" {
		t.Errorf("the staged reset did not restore the kit content: %q", data)
	}
	lk, _ := e.locks.Load(target)
	if _, held := lk.Items["personal/greeting"]; held {
		t.Error("the staged removal did not land")
	}
	reg := registryOf(e)
	if p := reg.Pending[target]; p != nil && len(p.Items) > 0 {
		t.Errorf("the pending actions did not settle: %+v", p.Items)
	}
}

// TestApplyMixedRemovalAndSelectionShareOneFile pins the execution order
// of the mixed apply: the removals run first, and the installs plan again
// from the target as the removals leave it — a removed item's block must
// not rise again in the shared file the selection writes into.
func TestApplyMixedRemovalAndSelectionShareOneFile(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	// The signoff item writes a section into the same shared AGENTS.md
	// the greeting item's block lives in.
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/signoff/item.yaml"):  "id: signoff\nversion: 1.0.0\ncontributes:\n  - section: signoff.md\n    to: AGENTS.md\n",
		filepath.Join(dir, "items/signoff/signoff.md"): "# signoff\n",
	})
	if _, _, code := execute(t, testBuild, "install", "personal/greeting", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/greeting": {Remove: true},
			"personal/signoff":  {Select: true, Version: "1.0.0"},
		},
	})

	stdout, stderr, code := execute(t, testBuild, "apply", "--target", target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"removed  personal/greeting",
		"installed  personal/signoff  1.0.0",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	data, err := os.ReadFile(filepath.Join(target, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if strings.Contains(string(data), "Greets.") {
		t.Errorf("the removed item's block rose again in the shared file:\n%s", data)
	}
	if !strings.Contains(string(data), "# signoff") {
		t.Errorf("the installed item's block is missing from the shared file:\n%s", data)
	}
	lk, _ := e.locks.Load(target)
	if _, held := lk.Items["personal/greeting"]; held {
		t.Error("the lock still owns the removed item")
	}
	rec, held := lk.Items["personal/signoff"]
	if !held {
		t.Fatal("the lock does not own the installed item")
	}
	if _, owned := rec.Sections["AGENTS.md"]; !owned {
		t.Errorf("the lock does not own the installed section: %+v", rec.Sections)
	}
	reg := registryOf(e)
	if p := reg.Pending[target]; p != nil && p.Items["personal/greeting"] != nil {
		t.Errorf("the staged removal did not settle: %+v", p.Items)
	}
}

// TestFailedStepPrintsIOCategory pins the Error line of a step that
// failed at apply time: the skeleton holds its category, and a write
// that failed carries io. A read-only target directory leaves the
// planning untouched — the snapshot only reads — and fails the step's
// directory creation.
func TestFailedStepPrintsIOCategory(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o700) })

	stdout, stderr, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "failed  personal/writing") {
		t.Errorf("stdout lacks the failed row:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Error [io]: personal/writing: ") {
		t.Errorf("stderr lacks the io Error line:\n%s", stderr)
	}
}

// TestInstallMixedResult pins the partially failed run: one item
// installs, another is excluded with the conflict prose, the exit is 1,
// and the stderr carries the one Error line for the failed item.
func TestInstallMixedResult(t *testing.T) {
	e := newTestEnv(t)
	dir, target := e.readyKit("personal")
	e.writeFiles(map[string]string{
		filepath.Join(dir, "items/second/item.yaml"): "id: second\nversion: 1.0.0\ncontributes:\n  - file: s.md\n    to: skills/writing/SKILL.md\n",
		filepath.Join(dir, "items/second/s.md"):      "# second\n",
	})
	// The owner of the contested path installs first: the rival of the
	// next run meets an installed owner, not a run rival.
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}

	stdout, stderr, code := execute(t, testBuild, "install", "personal/greeting", "personal/second", "--target", target, "--yes")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	for _, want := range []string{
		"installed  personal/greeting  1.0.0",
		"excluded  personal/second",
		"1 applied · 1 failed",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	want := "Error [conflict]: personal/second: skills/writing/SKILL.md is owned by installed item personal/writing\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestConfirmation pins scenario 13: the prompt on stderr with the
// answer from stdin applies; a decline aborts with exit 1 and no Error
// line; a headless run without --yes is refused before anything prints;
// --yes never prompts.
func TestConfirmation(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")

	t.Run("yes applies", func(t *testing.T) {
		stdout, stderr, code := executeAsk(t, "y\n", true, "install", "personal/writing", "--target", target)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stderr, "Apply? [y/N] ") {
			t.Errorf("stderr lacks the prompt: %q", stderr)
		}
		if !strings.Contains(stdout, "1 applied") {
			t.Errorf("stdout lacks the applied result:\n%s", stdout)
		}
	})

	t.Run("no aborts", func(t *testing.T) {
		stdout, stderr, code := executeAsk(t, "n\n", true, "install", "personal/greeting", "--target", target)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.HasSuffix(stdout, "aborted (nothing was applied)\n") {
			t.Errorf("stdout = %q, want the aborted line at the end", stdout)
		}
		if strings.Contains(stderr, "Error") {
			t.Errorf("stderr carries an Error line: %q", stderr)
		}
		if !strings.Contains(stderr, "Apply? [y/N] ") {
			t.Errorf("stderr lacks the prompt: %q", stderr)
		}
		if _, err := os.Stat(filepath.Join(target, "AGENTS.md")); !os.IsNotExist(err) {
			t.Errorf("the declined apply wrote anyway: %v", err)
		}
	})

	t.Run("headless without --yes is refused before anything prints", func(t *testing.T) {
		stdout, stderr, code := executeAsk(t, "", false, "install", "personal/greeting", "--target", target)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want nothing", stdout)
		}
		want := "Error [confirmation]: cannot ask for confirmation without a terminal (rerun with --yes)\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("--yes never prompts", func(t *testing.T) {
		_, stderr, code := executeAsk(t, "", false, "install", "personal/greeting", "--target", target, "--yes")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if strings.Contains(stderr, "Apply?") {
			t.Errorf("stderr carries a prompt: %q", stderr)
		}
	})

	t.Run("an empty answer declines", func(t *testing.T) {
		_, _, code := executeAsk(t, "\n", true, "install", "personal/greeting", "--target", target)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})
}

// TestDryRun pins scenario 14: --dry-run prints the summary and writes
// nothing — not the target, not the pending actions; a plan with an
// excluded item exits 1.
func TestDryRun(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")

	stdout, stderr, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--dry-run")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "install  personal/writing  1.0.0") {
		t.Errorf("the dry-run printed no summary:\n%s", stdout)
	}
	if strings.Contains(stdout, "installed") {
		t.Errorf("the dry-run printed a result:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(target, "skills/writing/SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("the dry-run wrote the target: %v", err)
	}
	lk, _ := e.locks.Load(target)
	if len(lk.Items) != 0 {
		t.Errorf("the dry-run wrote the lock: %+v", lk.Items)
	}

	// The staged pending actions are untouched by a dry-run clear.
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{"personal/greeting": {Remove: true}},
	})
	stdout, _, code = execute(t, testBuild, "pending", "clear", "--target", target, "--dry-run")
	if code != 0 {
		t.Fatalf("clear dry-run exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "  remove  personal/greeting") {
		t.Errorf("the clear dry-run printed no summary:\n%s", stdout)
	}
	reg := registryOf(e)
	if _, held := reg.Pending[target]; !held {
		t.Error("the dry-run clear dropped the pending actions")
	}
	// The staged actions go, so the one-shot below is not refused.
	if _, _, code = execute(t, testBuild, "pending", "clear", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the clear exited %d", code)
	}

	// A plan with an exclusion is not a full success.
	e.writeFiles(map[string]string{filepath.Join(target, "skills/writing/SKILL.md"): "# mine\n"})
	_, stderr, code = execute(t, testBuild, "install", "personal/writing", "--target", target, "--dry-run")
	if code != 1 {
		t.Fatalf("excluded dry-run exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Error [conflict]:") {
		t.Errorf("stderr = %q, want the conflict line", stderr)
	}
}

// TestSetFlagIsUndefinedOnCommandsWithoutVariables pins the flat --set
// scope: the commands whose items carry no variables do not define the
// flag, and passing it is a wrong command line.
func TestSetFlagIsUndefinedOnCommandsWithoutVariables(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	for _, args := range [][]string{
		{"remove", "personal/writing", "--set", "x=1", "--target", target},
		{"apply", "--set", "x=1", "--target", target},
		{"reset", "personal/writing", "--set", "x=1", "--target", target},
		{"pending", "clear", "--set", "x=1", "--target", target},
	} {
		_, stderr, code := execute(t, testBuild, args...)
		if code != 2 {
			t.Errorf("%v: exit code = %d, want 2 (stderr: %s)", args, code, stderr)
		}
		if !strings.Contains(stderr, "Error [usage]: unknown flag: --set") {
			t.Errorf("%v: stderr = %q, want the unknown-flag refusal", args, stderr)
		}
	}
}

// stage writes pending actions for the target the way the terminal UI
// saves them.
func stage(e *testEnv, target string, p *state.Pending) {
	e.t.Helper()
	if err := e.states.Update(func(reg *state.Registry) error {
		reg.Pending[target] = p
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

// registryOf reloads the registry the run left behind.
func registryOf(e *testEnv) *state.Registry {
	e.t.Helper()
	reg, err := e.states.LoadRegistry()
	if err != nil {
		e.t.Fatal(err)
	}
	return reg
}

// personalAttachment reports whether the personal kit is attached to the
// target.
func personalAttachment(e *testEnv, target string) (string, bool) {
	e.t.Helper()
	for _, a := range registryOf(e).Attachments {
		if a.Kit == "personal" && a.Target == target {
			return a.Target, true
		}
	}
	return "", false
}
