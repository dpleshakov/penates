package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
var testBuild = build{version: "dev", commit: "unknown"}

// execute runs the command tree the way main does and returns what the
// process would have written and exited with.
func execute(t *testing.T, b build, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(b, args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// stubTUI replaces the injectable TUI runner for the test and returns the
// counter of its invocations.
func stubTUI(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := runTUI
	runTUI = func(io.Writer) error { calls++; return nil }
	t.Cleanup(func() { runTUI = orig })
	return &calls
}

// The headline behavior of the change: zero arguments run the interface,
// not the help.
func TestZeroArgumentsOpenTheTUI(t *testing.T) {
	calls := stubTUI(t)

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
	if *calls != 1 {
		t.Errorf("the TUI runner ran %d times, want once", *calls)
	}
}

func TestVersionDoesNotOpenTheTUI(t *testing.T) {
	calls := stubTUI(t)

	stdout, stderr, code := execute(t, testBuild, "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "penates dev (unknown)\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if *calls != 0 {
		t.Errorf("the TUI runner ran %d times, want none", *calls)
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
	frame := strings.Repeat("=", 80)
	framed := func(name string) string {
		return "\n" + frame + "\n" + name + "\n" + frame + "\n\n"
	}
	wantList := "The penates binary contains the following components:\n\n" +
		"  penates\n" +
		"  Go standard library and runtime\n"
	if !strings.HasPrefix(stdout, wantList) {
		t.Errorf("stdout does not start with the component list:\n%.200s", stdout)
	}
	for _, want := range []string{
		"only in the release builds",
		"https://github.com/dpleshakov/penates/releases",
		framed("penates"),
		framed("Go standard library and runtime"),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%.400s", want, stdout)
		}
	}
}

func TestExtraArgumentsToLicensesAreAnError(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "licenses", "extra")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := `Error: unknown command "extra" for "penates licenses" (run "penates licenses --help" for the available commands)` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

func TestVersionPrintsTheBuildStamp(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if want := "penates dev (unknown)\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestStampedBuildReportsItsStamp(t *testing.T) {
	b := build{version: "1.2.3", commit: "abc1234"}

	stdout, stderr, code := execute(t, b, "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "penates 1.2.3 (abc1234)\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// The doctrine for stderr errors: one line in the Error: form, naming the
// concrete next step.
func TestUnknownCommandNamesTheNextStep(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{
			name: "a word like no command",
			arg:  "frobnicate",
			want: `Error: unknown command "frobnicate" for "penates" (run "penates --help" for the available commands)` + "\n",
		},
		{
			name: "a word close to a command",
			arg:  "versio",
			want: `Error: unknown command "versio" for "penates" (did you mean "version"?)` + "\n",
		},
		{
			// The positional kit path of the old command line is gone
			// without an alias: a directory is an unknown command.
			name: "a kit path",
			arg:  "/home/user/kits/opencode",
			want: `Error: unknown command "/home/user/kits/opencode" for "penates" (run "penates --help" for the available commands)` + "\n",
		},
		{
			// An empty argument prefix-matches every command, so it must
			// not ask "did you mean".
			name: "an empty argument",
			arg:  "",
			want: `Error: unknown command "" for "penates" (run "penates --help" for the available commands)` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := execute(t, testBuild, tt.arg)

			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if stderr != tt.want {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
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

func TestExtraArgumentsToVersionAreAnError(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "version", "extra")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := `Error: unknown command "extra" for "penates version" (run "penates version --help" for the available commands)` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

// Below the root, cobra itself would print the completion group's help and
// exit 0 for an unknown shell; rejectArgs turns that into an error.
func TestUnknownCompletionSubcommandIsAnError(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "completion", "bashh")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := `Error: unknown command "bashh" for "penates completion" (did you mean "bash"?)` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
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

func TestHelpGoesToStdoutWithoutError(t *testing.T) {
	stdout, stderr, code := execute(t, testBuild, "--help")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	for _, want := range []string{"version", "Available Commands:", "completion"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %s", want)
		}
	}
}

func TestVersionHelpShowsTheCommandHelp(t *testing.T) {
	stdout, _, code := execute(t, testBuild, "version", "--help")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "Print the version") {
		t.Errorf("help = %q, want the version command's own help", stdout)
	}
}
