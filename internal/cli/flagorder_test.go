package cli

import (
	"errors"
	"io"
	"testing"
)

// The flags-after-the-command-path rule, pinned: the refusal is a usage
// error whose message names the corrected command, an unknown flag before
// the path is reported as itself, and the lines that are already right
// pass untouched.

func TestFlagOrderRefusesFlagsBeforeThePath(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "a flag of the command",
			args: []string{"--target", "/x", "status"},
			want: `Error [usage]: the command "penates status" must come first, before its flags and arguments (run "penates status --help")`,
		},
		{
			name: "a flag of a command below a group",
			args: []string{"--target", "/x", "show", "a/b"},
			want: `Error [usage]: the command "penates show" must come first, before its flags and arguments (run "penates show --help")`,
		},
		{
			// A leaf's flag reported before the path would not become
			// valid after the move: the flag itself is the report.
			name: "an unknown flag before a leaf",
			args: []string{"--frob", "kit", "list"},
			want: `Error [usage]: unknown flag: --frob (run "penates kit list --help")`,
		},
		{
			// A group ignores the flags it does not know, so the refusal
			// is the command-first one, and cobra's parse of the corrected
			// line reports the mistyped name.
			name: "an unknown flag before a group",
			args: []string{"--frob", "kit"},
			want: `Error [usage]: the command "penates kit" must come first, before its flags and arguments (run "penates kit --help")`,
		},
		{
			// A word that only looks like a command after the reorder:
			// cobra parses the flags first, and the unknown flag is the
			// report.
			name: "a flag before a mistyped command",
			args: []string{"--frob", "frobnicate"},
			want: `Error [usage]: unknown flag: --frob (run "penates --help")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, code := execute(t, testBuild, tt.args...)

			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stderr != tt.want+"\n" {
				t.Errorf("stderr = %q, want %q", stderr, tt.want+"\n")
			}
		})
	}
}

func TestFlagOrderPassesWhatIsAlreadyRight(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"the path first", []string{"status"}},
		{"flags after the path", []string{"status", "--target", "/x"}},
		{"help on the root", []string{"--help"}},
		{"help after the path", []string{"status", "--help"}},
		{"help before a group", []string{"kit", "--help"}},
		{"a group and its command", []string{"kit", "list"}},
		{"the completion request passes through", []string{"__complete", "status", "--ta"}},
		{"words after -- are arguments", []string{"show", "--", "a/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newRoot(testBuild, Options{TUI: func(io.Writer) error { return nil }}, nil, nil)
			if err := checkFlagOrder(root, tt.args); err != nil {
				t.Errorf("checkFlagOrder(%q) = %v, want nothing", tt.args, err)
			}
		})
	}
}

// An unknown flag before a leaf command would not become valid after the
// move: it is reported as itself, in pflag's wording, with the help as the
// next step.
func TestFlagOrderReportsUnknownFlagsThemselves(t *testing.T) {
	root := newRoot(testBuild, Options{TUI: func(io.Writer) error { return nil }}, nil, nil)

	err := checkFlagOrder(root, []string{"--frob", "status"})
	if err == nil {
		t.Fatal("checkFlagOrder reported nothing, want the unknown flag")
	}
	var e *Error
	if !errors.As(err, &e) || e.Category != CategoryUsage {
		t.Fatalf("error = %v, want a usage error", err)
	}
	if want := "unknown flag: --frob (run \"penates status --help\")"; e.Message != want {
		t.Errorf("message = %q, want %q", e.Message, want)
	}
}
