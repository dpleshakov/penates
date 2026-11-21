package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/guard"
)

// heldRunGuard holds the run guard over the env's data directory the way
// another penates process would — an open terminal UI, most often. It is
// released when the test ends.
func heldRunGuard(t *testing.T, e *testEnv) *guard.Guard {
	t.Helper()
	g, err := guard.Acquire(e.dataDir)
	if err != nil {
		t.Fatalf("the test's own guard: %v", err)
	}
	t.Cleanup(func() { _ = g.Release() })
	return g
}

// Every command that opens the state refuses while another penates
// process holds the run guard, whatever the command would have found
// after the state opened — the guard is taken before the state is read.
func TestHeldGuardRefusesEveryStateCommand(t *testing.T) {
	e := newTestEnv(t)
	kitDir := e.kitFixture()
	e.register("personal", kitDir)
	target := filepath.Join(e.base, "target")
	e.attach("personal", target)
	e.activate(target)
	heldRunGuard(t, e)

	want := "Error [busy]: another penates run holds " + e.dataDir + " (close it or retry)\n"
	tests := []struct {
		name string
		args []string
	}{
		{"status", []string{"status", "--target", target}},
		{"show", []string{"show", "personal/writing", "--target", target}},
		{"kit list", []string{"kit", "list"}},
		{"kit add", []string{"kit", "add", kitDir, "--target", target}},
		{"kit attach", []string{"kit", "attach", "personal", "--target", target}},
		{"kit detach", []string{"kit", "detach", "personal", "--target", target}},
		{"kit path", []string{"kit", "path", "personal"}},
		{"target list", []string{"target", "list"}},
		{"target add", []string{"target", "add", target}},
		{"target rename", []string{"target", "rename", target, "name"}},
		{"target forget", []string{"target", "forget", target}},
		{"install", []string{"install", "personal/writing", "--target", target, "--yes"}},
		{"update", []string{"update", "--target", target, "--yes"}},
		{"remove", []string{"remove", "personal/writing", "--target", target, "--yes"}},
		{"reset", []string{"reset", "personal/writing", "--target", target, "--yes"}},
		{"apply", []string{"apply", "--target", target, "--yes"}},
		{"pending clear", []string{"pending", "clear", "--target", target, "--yes"}},
		{"setup", []string{"setup", "--target", target, "--yes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := execute(t, testBuild, tt.args...)

			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
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

// Releasing the guard lets the same command through, the way an ended
// run does for the next one.
func TestReleasedGuardLetsTheCommandThrough(t *testing.T) {
	e := newTestEnv(t)
	g := heldRunGuard(t, e)

	_, stderr, code := execute(t, testBuild, "status", "--target", e.base)
	if code != 1 || !strings.HasPrefix(stderr, "Error [busy]: ") {
		t.Fatalf("with the guard held: exit code %d, stderr %q, want a busy refusal", code, stderr)
	}

	if err := g.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	_, stderr, code = execute(t, testBuild, "status", "--target", e.base)
	if code != 0 {
		t.Fatalf("after the release: exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
}

// The commands that touch no state run regardless of the guard.
func TestStateFreeCommandsWorkUnderHeldGuard(t *testing.T) {
	e := newTestEnv(t)
	heldRunGuard(t, e)

	for _, args := range [][]string{
		{"version"},
		{"licenses"},
		{"--help"},
	} {
		_, stderr, code := execute(t, testBuild, args...)
		if code != 0 {
			t.Errorf("%s: exit code = %d, want 0 (stderr: %s)", strings.Join(args, " "), code, stderr)
		}
	}
}

// A run whose guard cannot be taken at all — the data directory cannot
// be made — is a state failure, not a busy refusal: nothing holds
// anything, and repeating cannot help.
func TestUnmakeableDataDirIsAStateError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(blocker, "config"))

	_, stderr, code := execute(t, testBuild, "status", "--target", "/somewhere")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, "Error [state]: ") {
		t.Errorf("stderr = %q, want a state error", stderr)
	}
}

// One process holds one guard: the next command run in the same process
// releases the previous run's hold, the way the previous run's process
// ending would have. Tests run many runs back to back; real runs take
// the guard once per process and never see this.
func TestSequentialRunsShareTheProcess(t *testing.T) {
	e := newTestEnv(t)

	for i := range 2 {
		_, stderr, code := execute(t, testBuild, "kit", "list")
		if code != 0 {
			t.Fatalf("run %d over %s: exit code = %d, want 0 (stderr: %s)", i+1, e.dataDir, code, stderr)
		}
	}
	if runGuard == nil {
		t.Errorf("the run left no guard behind")
	}
	if err := runGuard.Release(); err != nil {
		t.Errorf("releasing the run's own guard: %v", err)
	}
	runGuard = nil
}

// The busy refusal maps into the contract with its category: ExitCode
// keeps it at 1 and Print keeps its one-line form. Anything else taking
// the guard reported is a state failure, not a busy refusal.
func TestGuardErrorCarriesItsCategory(t *testing.T) {
	e := GuardError(&guard.BusyError{Dir: "/somewhere"})
	if e.Category != CategoryBusy {
		t.Errorf("category = %q, want busy", e.Category)
	}
	want := "another penates run holds /somewhere (close it or retry)"
	if e.Message != want {
		t.Errorf("message = %q, want %q", e.Message, want)
	}

	e = GuardError(errors.New("mkdir /x: not a directory"))
	if e.Category != CategoryState {
		t.Errorf("category = %q, want state", e.Category)
	}
}
