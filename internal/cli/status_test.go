package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// The reading commands against a real state: kits and targets registered
// the way the operations record them, locks written the way the apply
// leaves them, the output compared line for line. The target path in the
// fixtures is the test's own temp directory, so no machine path leaks.

func TestStatusEmptyState(t *testing.T) {
	e := newTestEnv(t)

	stdout, stderr, code := execute(t, testBuild, "status", "--target", filepath.Join(e.base, "harness"))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if want := noTargetRegistered + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// The full line-for-line skeleton: the target header, the kit section with
// the state word as the first token of every item line.
func TestStatusShowsTheTargetAndItsItems(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", target)
	// writing installed at the kit version; nothing recorded, so the
	// reconciliation finds nothing broken or modified.
	e.saveLock(target, map[string]*lock.Item{
		"personal/writing": {Version: "1.0.0"},
	})

	stdout, stderr, code := execute(t, testBuild, "status", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	want := strings.Join([]string{
		"target " + target,
		"",
		"kit personal  " + dir,
		"  available  personal/greeting  2.0.0",
		"  installed  personal/writing  1.0.0",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// The display name rides in the header; a version difference shows as the
// pair with the arrow.
func TestStatusHeaderNameAndVersionPair(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", target)
	e.nameTarget(target, "harness one")
	e.saveLock(target, map[string]*lock.Item{
		"personal/writing": {Version: "0.9.0"},
	})

	stdout, stderr, code := execute(t, testBuild, "status", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"target harness one (" + target + ")",
		"",
		"kit personal  " + dir,
		"  available  personal/greeting  2.0.0",
		"  update-available  personal/writing  0.9.0 → 1.0.0",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// The built-in kit's row names the kit itself with its annotation, and the
// items its kit provides show like any kit's.
func TestStatusMarksTheBuiltinKit(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	kitDir := builtinKitDir(t, e)

	// The first run registers and unpacks the built-in kit; the attach
	// needs that registration.
	_, stderr, code := execute(t, testBuild, "kit", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	e.attach(state.BuiltinKitName, target)

	stdout, stderr, code := execute(t, testBuild, "status", "--target", target)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	// The row annotates the built-in kit; the items are the embedded
	// kit's own and are not asserted here.
	if want := "target " + target + "\n"; !strings.HasPrefix(stdout, want) {
		t.Errorf("stdout = %q, want it to open with %q", stdout, want)
	}
	if !strings.Contains(stdout, kitDir+"  built-in\n") {
		t.Errorf("stdout misses the built-in kit row:\n%s", stdout)
	}
}

// A locally modified built-in kit annotates its row with the fact.
func TestStatusAnnotatesTheModifiedBuiltinKit(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	kitDir := builtinKitDir(t, e)

	_, stderr, code := execute(t, testBuild, "kit", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	e.attach(state.BuiltinKitName, target)

	// A local edit in the unpacked kit and a stamp the embedded content
	// no longer matches: the sync reports the modification and the
	// blocked update. The stamp is a 64-hex string; any other content
	// proves nothing, and the sync stands down instead.
	skill := filepath.Join(kitDir, "items", "reference", "SKILL.md")
	data, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	e.writeFiles(map[string]string{skill: string(data) + "\nA local line.\n"})
	stale := strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(e.dataDir, "builtin-kit.sha256"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := execute(t, testBuild, "status", "--target", target)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "  built-in, modified locally, update blocked by local modifications\n") {
		t.Errorf("stdout misses the modified built-in annotation:\n%s", stdout)
	}
}

// builtinKitDir is where the run's startup sync unpacks the built-in kit.
// The first status run performs it.
func builtinKitDir(t *testing.T, e *testEnv) string {
	t.Helper()
	return filepath.Join(e.dataDir, "builtin-kit")
}

// An unreadable kit shows its group with the unavailable line, and its
// installed items freeze under it.
func TestStatusShowsAnUnreadableKit(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.register("broken", filepath.Join(e.base, "kits", "broken"))
	e.attach("personal", target)
	e.attach("broken", target)
	// A kit directory without kit.yaml does not load.
	e.writeFiles(map[string]string{filepath.Join(e.base, "kits", "broken", "items", "junk", "item.yaml"): "id: junk\n"})
	e.saveLock(target, map[string]*lock.Item{
		"broken/legacy": {Version: "0.4.0"},
	})

	stdout, stderr, code := execute(t, testBuild, "status", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"target " + target,
		"",
		"kit broken  " + filepath.Join(e.base, "kits", "broken"),
		"  unavailable  (the kit directory cannot be read)",
		"  unavailable  broken/legacy  0.4.0",
		"",
		"kit personal  " + dir,
		"  available  personal/greeting  2.0.0",
		"  available  personal/writing  1.0.0",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// The installed items whose kit is not attached form the trailing orphans
// group.
func TestStatusShowsTheOrphansGroup(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", target)
	e.saveLock(target, map[string]*lock.Item{
		"personal/writing": {Version: "1.0.0"},
		"gone-kit/stray":   {Version: "1.1.0"},
	})

	stdout, stderr, code := execute(t, testBuild, "status", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"target " + target,
		"",
		"kit personal  " + dir,
		"  available  personal/greeting  2.0.0",
		"  installed  personal/writing  1.0.0",
		"",
		"orphans",
		"  orphan  gone-kit/stray  1.1.0",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// The staged pending actions of the target follow the kit sections, one
// verb per item line, the deferred operations after them.
func TestStatusShowsThePendingActions(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", target)
	e.saveLock(target, map[string]*lock.Item{
		"personal/writing": {Version: "0.9.0"},
		"personal/old":     {Version: "0.1.0"},
	})
	if err := e.states.Update(func(reg *state.Registry) error {
		reg.Pending[target] = &state.Pending{
			Items: map[string]*state.ItemPending{
				"personal/writing": {Select: true, Version: "1.0.0"},
				"personal/old":     {Remove: true},
			},
		}
		reg.Pending[target].Deferred = []state.Deferred{{Kind: state.DeferredDetachKit, Kit: "personal"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := execute(t, testBuild, "status", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"target " + target,
		"",
		"kit personal  " + dir,
		"  available  personal/greeting  2.0.0",
		"  update-available  personal/writing  0.9.0 → 1.0.0",
		"  orphan  personal/old  0.1.0",
		"    removed from the kit",
		"",
		"pending 2",
		"  remove  personal/old",
		"  update  personal/writing  0.9.0 → 1.0.0",
		"  deferred  detach personal from " + target,
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// A state file the store refuses is the state category: the message names
// the file and the way out.
func TestStateProblemsAreStateErrors(t *testing.T) {
	e := newTestEnv(t)
	e.writeFiles(map[string]string{filepath.Join(e.dataDir, state.FileName): `{"version": 1}`})

	_, stderr, code := execute(t, testBuild, "kit", "list")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, `Error [state]: state `+filepath.Join(e.dataDir, state.FileName)+`: format version 1`) {
		t.Errorf("stderr = %q, want the state category naming the file", stderr)
	}
	if !strings.Contains(stderr, "delete the file and register kits again") {
		t.Errorf("stderr = %q, want the way out named", stderr)
	}
}

// A broken state file decodes as nothing: the state category again.
func TestABrokenStateFileIsAStateError(t *testing.T) {
	e := newTestEnv(t)
	e.writeFiles(map[string]string{filepath.Join(e.dataDir, state.FileName): `{not json`})

	_, stderr, code := execute(t, testBuild, "kit", "list")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, `Error [state]: state `) {
		t.Errorf("stderr = %q, want the state category", stderr)
	}
}

// The target group without its subcommand prints its help.
func TestTargetGroupWithoutSubcommandPrintsHelp(t *testing.T) {
	stdout, _, code := execute(t, testBuild, "target")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "list") {
		t.Errorf("stdout = %q, want the target group help listing its commands", stdout)
	}
}

// A staged selection of an available item is an install; a reset is a
// reset, whatever the state.
func TestStatusPendingVerbs(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", target)
	e.saveLock(target, map[string]*lock.Item{
		"personal/writing": {Version: "1.0.0"},
	})
	if err := e.states.Update(func(reg *state.Registry) error {
		reg.Pending[target] = &state.Pending{
			Items: map[string]*state.ItemPending{
				"personal/greeting": {Select: true},
				"personal/writing":  {Reset: true},
			},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := execute(t, testBuild, "status", "--target", target)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "  install  personal/greeting  2.0.0\n") {
		t.Errorf("stdout = %q, want the staged install line", stdout)
	}
	if !strings.Contains(stdout, "  reset  personal/writing\n") {
		t.Errorf("stdout = %q, want the staged reset line", stdout)
	}
}

// The required flag: every command that acts on a target refuses without
// it — a usage error naming the flag, exit 2, nothing printed on stdout.
// The requirement is the command line's, judged before any state opens.
func TestTargetCommandsRequireTheTargetFlag(t *testing.T) {
	e := newTestEnv(t)
	dir := e.kitFixture()
	e.register("personal", dir)

	tests := []struct {
		name string
		args []string
		path string
	}{
		{"status", []string{"status"}, "penates status"},
		{"show", []string{"show", "personal/writing"}, "penates show"},
		{"install", []string{"install", "personal/writing"}, "penates install"},
		{"update", []string{"update"}, "penates update"},
		{"remove", []string{"remove", "personal/writing"}, "penates remove"},
		{"reset", []string{"reset", "personal/writing"}, "penates reset"},
		{"apply", []string{"apply"}, "penates apply"},
		{"pending clear", []string{"pending", "clear"}, "penates pending clear"},
		{"kit add", []string{"kit", "add", dir}, "penates kit add"},
		{"kit attach", []string{"kit", "attach", "personal"}, "penates kit attach"},
		{"kit detach", []string{"kit", "detach", "personal"}, "penates kit detach"},
		{"setup", []string{"setup"}, "penates setup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := fmt.Sprintf("Error [usage]: required flag(s) %q not set (run %q)\n", "target", tt.path+" --help")

			stdout, stderr, code := execute(t, testBuild, tt.args...)

			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stderr != want {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
		})
	}
}

// Shorthand flags: this client defines none, so an unknown one is a usage
// error in pflag's wording, and a malformed flag is bad flag syntax.
func TestShorthandAndMalformedFlags(t *testing.T) {
	_, stderr, code := execute(t, testBuild, "status", "-x")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if want := `Error [usage]: unknown shorthand flag: 'x' in -x (run "penates status --help")` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}

	_, stderr, code = execute(t, testBuild, "status", "--=x")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if want := `Error [usage]: bad flag syntax: --=x (run "penates status --help")` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// The target a --target names is the one the command acts on, and an
// unregistered name is a not_found. The last active target of the
// terminal UI plays no part: the CLI never reads it.
func TestStatusTargetFlag(t *testing.T) {
	e := newTestEnv(t)
	open := filepath.Join(e.base, "open")
	other := filepath.Join(e.base, "other")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", open)
	e.attach("personal", other)
	// The terminal UI last opened the other target; the command still
	// acts only on the one --target names.
	e.activate(other)

	stdout, stderr, code := execute(t, testBuild, "status", "--target", open)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "target " + open + "\n"; !strings.HasPrefix(stdout, want) {
		t.Errorf("stdout = %q, want it to open with %q", stdout, want)
	}

	_, stderr, code = execute(t, testBuild, "status", "--target", filepath.Join(e.base, "nowhere"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := `Error [not_found]: no target is registered at ` + filepath.Join(e.base, "nowhere") + ` (register it with "penates target add ` + filepath.Join(e.base, "nowhere") + `")` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// kit list lays the registered kits out globally, no target needed.
func TestKitList(t *testing.T) {
	e := newTestEnv(t)
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", filepath.Join(e.base, "harness"))

	stdout, stderr, code := execute(t, testBuild, "kit", "list")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	// The built-in kit the sync registered is listed with the rest; the
	// temp target path belongs to this test.
	want := strings.Join([]string{
		"kit penates  " + filepath.Join(e.dataDir, "builtin-kit") + "  built-in",
		"  no attachments",
		"",
		"kit personal  " + dir,
		"  attached  " + filepath.Join(e.base, "harness"),
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// target list counts the kits, the installed items, and — when the
// target holds staged pending actions — the pending actions of each.
// No target is marked active: which one the terminal UI opens is the
// TUI's own state, not the list's to tell.
func TestTargetList(t *testing.T) {
	e := newTestEnv(t)
	quiet := filepath.Join(e.base, "quiet")
	staged := filepath.Join(e.base, "staged")
	dir := e.kitFixture()
	e.register("personal", dir)
	e.attach("personal", quiet)
	e.attach("personal", staged)
	e.saveLock(quiet, map[string]*lock.Item{
		"personal/writing":  {Version: "1.0.0"},
		"personal/greeting": {Version: "2.0.0"},
	})
	stage(e, staged, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/writing": {Remove: true},
		},
		Deferred: []state.Deferred{{Kind: state.DeferredDetachKit, Kit: "personal"}},
	})

	stdout, stderr, code := execute(t, testBuild, "target", "list")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"target " + quiet + "  kits 1  installed 2",
		"target " + staged + "  kits 1  installed 0  pending 2",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// The display name of a target rides in its list line.
func TestTargetListNames(t *testing.T) {
	e := newTestEnv(t)
	named := filepath.Join(e.base, "named")
	e.register("personal", e.kitFixture())
	e.attach("personal", named)
	e.nameTarget(named, "the named one")

	stdout, _, code := execute(t, testBuild, "target", "list")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if want := "target " + named + "  name the named one  kits 1  installed 0\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}
