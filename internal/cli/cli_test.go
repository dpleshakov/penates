package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/builtin"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// TestMain turns off a question cobra asks on Windows before every execution:
// whether the process was started from Explorer, answered by listing every
// process of the system. At several milliseconds a call, it would slow the
// Windows leg of the CI matrix for no test benefit.
func TestMain(m *testing.M) {
	cobra.MousetrapHelpText = ""
	m.Run()
}

// testBuild is the stamp an unstamped `go build` leaves behind.
var testBuild = Build{Version: "dev", Commit: "unknown"}

// tuiCalls counts the TUI runner's invocations of the last execute call.
var tuiCalls int

// execute runs the command tree the way main does and returns what the
// process would have written and exited with. The TUI runner is a stub.
func execute(t *testing.T, b Build, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	tuiCalls = 0
	var out, errOut bytes.Buffer
	code = Run(b, Options{TUI: func(io.Writer) error { tuiCalls++; return nil }}, args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// testEnv is the surroundings of one command test: a penates data
// directory under a temp XDG_CONFIG_HOME, reached the way every run
// reaches it, and helpers that record kits, targets, and locks in it.
type testEnv struct {
	t       *testing.T
	base    string
	dataDir string
	states  *state.Store
	locks   *lock.Store
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	dataDir := filepath.Join(base, "penates")
	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{
		t:       t,
		base:    base,
		dataDir: dataDir,
		states:  st,
		locks:   lock.NewStore(dataDir),
	}
}

// writeKit writes a kit directory holding the given files and returns its
// path. The kit.yaml carries the name; the caller's files add the items.
func (e *testEnv) writeKit(name string, files map[string]string) string {
	e.t.Helper()
	dir := filepath.Join(e.base, "kits", name)
	for file, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			e.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			e.t.Fatal(err)
		}
	}
	if _, ok := files["kit.yaml"]; !ok {
		e.writeFiles(map[string]string{filepath.Join(dir, "kit.yaml"): fmt.Sprintf("name: %s\n", name)})
	}
	return dir
}

// writeFiles writes the given absolute-path files.
func (e *testEnv) writeFiles(files map[string]string) {
	e.t.Helper()
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			e.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			e.t.Fatal(err)
		}
	}
}

// register records a kit directory under its name.
func (e *testEnv) register(kitName, dir string) {
	e.t.Helper()
	if err := e.states.RegisterKit(kitName, dir); err != nil {
		e.t.Fatal(err)
	}
}

// attach attaches a registered kit to a target path, registering the
// target.
func (e *testEnv) attach(kitName, target string) {
	e.t.Helper()
	if err := e.states.RegisterTarget(target); err != nil {
		e.t.Fatal(err)
	}
	if err := e.states.AttachKit(kitName, target); err != nil {
		e.t.Fatal(err)
	}
}

// nameTarget sets the display name of a registered target.
func (e *testEnv) nameTarget(target, name string) {
	e.t.Helper()
	if err := e.states.Update(func(reg *state.Registry) error {
		t := reg.Targets[target]
		t.Name = name
		reg.Targets[target] = t
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

// activate records the target as the last active one, the way the terminal
// UI leaves it.
func (e *testEnv) activate(target string) {
	e.t.Helper()
	if err := e.states.Update(func(reg *state.Registry) error {
		reg.LastTarget = target
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

// saveLock writes a lock for the target holding the given records.
func (e *testEnv) saveLock(target string, items map[string]*lock.Item) {
	e.t.Helper()
	lk := &lock.Lock{Target: target, Items: items}
	if err := e.locks.Save(lk); err != nil {
		e.t.Fatal(err)
	}
}

// kitFixture writes the kit the status and show tests read: two items, one
// with a version and a changelog, one with a variable.
func (e *testEnv) kitFixture() string {
	return e.writeKit("personal", map[string]string{
		"kit.yaml":                     "name: personal\ndescription: Personal kit\nshared:\n  - AGENTS.md\n",
		"items/writing/item.yaml":      "id: writing\nversion: 1.0.0\ndescription: Text editing skill\ntags: [writing]\ncontributes:\n  - file: skill/SKILL.md\n    to: skills/writing/SKILL.md\n",
		"items/writing/skill/SKILL.md": "# writing\n",
		"items/writing/CHANGELOG.md":   "## 1.0.0\nAdds the writing skill.\n",
		"items/greeting/item.yaml":     "id: greeting\nversion: 2.0.0\ndescription: Greets the user\ncontributes:\n  - section: note.md\n    to: AGENTS.md\n",
		"items/greeting/note.md":       "Greets.\n",
	})
}

// TestZeroArgumentsOpenTheTUI pins the headline behavior: zero arguments
// run the interface, not the help.
func TestZeroArgumentsOpenTheTUI(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if tuiCalls != 1 {
		t.Errorf("the TUI runner ran %d times, want once", tuiCalls)
	}
}

func TestVersionDoesNotOpenTheTUI(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "penates dev (unknown)\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if tuiCalls != 0 {
		t.Errorf("the TUI runner ran %d times, want none", tuiCalls)
	}
}

func TestStampedBuildReportsItsStamp(t *testing.T) {
	stdout, _, code := execute(t, Build{Version: "1.2.3", Commit: "abc1234"}, "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if want := "penates 1.2.3 (abc1234)\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// A plain build carries no third-party set: the command lists the two fixed
// components and says where the release builds put the rest.
func TestLicensesPrintsTheComponents(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "licenses")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	for _, want := range []string{
		"only in the release builds",
		"https://github.com/dpleshakov/penates/releases",
		"The penates binary contains the following components:",
		"penates",
		"Go standard library and runtime",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%.400s", want, stdout)
		}
	}
}

// The usage errors exit 2, and every stderr line stays in the contract
// format: `Error [usage]: message`, one line, the next step named.
func TestUsageErrorsExitTwo(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "a word like no command",
			args: []string{"frobnicate"},
			want: `Error [usage]: unknown command "frobnicate" for "penates" (run "penates --help" for the available commands)`,
		},
		{
			name: "a word close to a command",
			args: []string{"versio"},
			want: `Error [usage]: unknown command "versio" for "penates" (did you mean "version"?)`,
		},
		{
			// An empty argument prefix-matches every command, so it must
			// not ask "did you mean".
			name: "an empty argument",
			args: []string{""},
			want: `Error [usage]: unknown command "" for "penates" (run "penates --help" for the available commands)`,
		},
		{
			name: "extra arguments to version",
			args: []string{"version", "extra"},
			want: `Error [usage]: unknown command "extra" for "penates version" (run "penates version --help" for the available commands)`,
		},
		{
			name: "extra arguments to licenses",
			args: []string{"licenses", "extra"},
			want: `Error [usage]: unknown command "extra" for "penates licenses" (run "penates licenses --help" for the available commands)`,
		},
		{
			name: "an unknown flag on a command",
			args: []string{"status", "--frob"},
			want: `Error [usage]: unknown flag: --frob (run "penates status --help")`,
		},
		{
			name: "an unknown flag on the root",
			args: []string{"--frob"},
			want: `Error [usage]: unknown flag: --frob (run "penates --help")`,
		},
		{
			// A flag before the command path breaks the prefix a
			// permission rule matches; the refusal names the corrected
			// command.
			name: "a flag before the command path",
			args: []string{"--target", "/x", "status"},
			want: `Error [usage]: the command "penates status" must come first, before its flags and arguments (run "penates status --help")`,
		},
		{
			name: "show without its item",
			args: []string{"show"},
			want: `Error [usage]: show takes 1 item named "kit/item", got 0 (run "penates show --help")`,
		},
		{
			name: "show with two items",
			args: []string{"show", "a/b", "c/d"},
			want: `Error [usage]: show takes 1 item named "kit/item", got 2 (run "penates show --help")`,
		},
		{
			name: "an unknown help topic",
			args: []string{"help", "frobnicate"},
			want: `Error [usage]: unknown help topic "frobnicate" (run "penates --help")`,
		},
		{
			name: "an unknown subcommand of a group",
			args: []string{"kit", "frobnicate"},
			want: `Error [usage]: unknown command "frobnicate" for "penates kit" (run "penates kit --help" for the available commands)`,
		},
		{
			// Below the root, cobra itself would print the completion
			// group's help and exit 0 for an unknown shell.
			name: "an unknown shell",
			args: []string{"completion", "bashh"},
			want: `Error [usage]: unknown command "bashh" for "penates completion" (did you mean "bash"?)`,
		},
		{
			// The shells cobra builds in refuse extra arguments with an
			// untyped error; the wrap makes it a usage error.
			name: "extra arguments to a shell",
			args: []string{"completion", "bash", "extra"},
			want: `Error [usage]: unknown command "extra" for "penates completion bash" (run "penates completion bash --help")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := execute(t, testBuild, tt.args...)

			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stderr != tt.want+"\n" {
				t.Errorf("stderr = %q, want %q", stderr, tt.want+"\n")
			}
			if lines := strings.Count(stderr, "\n"); lines != 1 {
				t.Errorf("stderr carries %d lines, want exactly one", lines)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
		})
	}
}

func TestCompletionGroupWithoutSubcommandPrintsHelp(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "completion")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if !strings.Contains(stdout, "bash") {
		t.Errorf("stdout = %q, want the completion help listing the shells", stdout)
	}
}

func TestKitGroupWithoutSubcommandPrintsHelp(t *testing.T) {
	stdout, _, code := execute(t, testBuild, "kit")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "list") {
		t.Errorf("stdout = %q, want the kit group help listing its commands", stdout)
	}
}

func TestHelpGoesToStdoutWithoutError(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "--help")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	for _, want := range []string{"version", "status", "show", "kit", "target", "Available Commands:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %s", want)
		}
	}
}

// The help of a command reached through the help command, and the help
// behind --help, both land on stdout with exit 0.
func TestHelpTopics(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"the help command reaches a command", []string{"help", "version"}, "Print the version"},
		{"a flag after the path", []string{"version", "--help"}, "Print the version"},
		{"a group", []string{"kit", "--help"}, "List the registered kits"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := execute(t, testBuild, tt.args...)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want nothing", stderr)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Errorf("help = %q, want it to mention %q", stdout, tt.want)
			}
		})
	}
}

// The reading commands open the state and run the built-in kit sync; a
// test that stubs nothing sees the unpacked built-in kit registered, and
// the loader accepts it. One command run is enough to pin the fact.
func TestEveryCommandSyncsTheBuiltinKit(t *testing.T) {
	e := newTestEnv(t)

	_, _, code := execute(t, testBuild, "kit", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	reg, err := e.states.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Kits[state.BuiltinKitName]; !ok {
		t.Errorf("the built-in kit did not register itself during the run")
	}
	if _, _, err := kit.Read(builtin.KitDir(e.dataDir)); err != nil {
		t.Errorf("the unpacked built-in kit does not read: %v", err)
	}
}
