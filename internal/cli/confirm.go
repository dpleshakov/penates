package cli

import (
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// exitError is returned by a command that has already reported its outcome
// in full and pinned the process exit code: Run returns the code and
// prints nothing on top of it. The contract errors take the other road —
// they print one Error line and map their category to a code (errors.go).
type exitError struct{ code int }

// Error returns the message the error interface sees; nothing prints it.
func (e *exitError) Error() string { return "the outcome is already reported" }

// exitFailed is the failed-but-fully-reported outcome, exit 1 without an
// Error line: a confirmation the person declined, a run whose result
// carries failed or excluded items — the result on stdout and the
// per-item lines on stderr are the report, and no error of penates
// belongs on top of them.
func exitFailed() error { return &exitError{code: 1} }

// The confirmation model of the mutating commands (concept 10.3): the
// summary stands on stdout, the prompt goes to stderr — stdout stays
// machine-readable — and the answer is read from stdin. A command asks
// at most once; --yes confirms in advance, --dry-run never asks.

// confirmer is the one question a mutating command may ask. The input
// and the terminal test come in as dependencies: the process wires the
// real stdin and the isatty test, the tests pin both and run the ask
// without a terminal.
type confirmer struct {
	// stdin carries the answer. nil reads nothing and declines.
	stdin io.Reader
	// stdout takes the refusal line; stderr takes the prompt.
	stdout, stderr io.Writer
	// isTerminal reports whether stdin holds a terminal.
	isTerminal func() bool
}

// ready decides whether the command may go on to ask: --yes confirms in
// advance, otherwise stdin must hold a terminal. Without either there is
// no asking — the command refuses before it prints anything, so the
// refusal is the only output of the run.
func (c *confirmer) ready(yes bool) *Error {
	if yes || c.isTerminal() {
		return nil
	}
	return &Error{
		Category: CategoryConfirmation,
		Message:  "cannot ask for confirmation without a terminal (rerun with --yes)",
	}
}

// ask prints the prompt on stderr and reads one answer line from stdin.
// A missing, empty, or non-affirmative answer declines — the default is
// no, as the prompt says.
func (c *confirmer) ask() bool {
	_, _ = io.WriteString(c.stderr, "Apply? [y/N] ")
	line := ""
	if c.stdin != nil {
		sc := bufio.NewScanner(c.stdin)
		if sc.Scan() {
			line = sc.Text()
		}
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// declined reports the refusal the person gave — the words go to stdout,
// where the summary already stands, and the exit is 1 with no Error
// line: penates did not fail, the person said no.
func (c *confirmer) declined() error {
	_, _ = io.WriteString(c.stdout, "aborted (nothing was applied)\n")
	return exitFailed()
}

// stdinIsTerminal is the process's terminal test: stdin holds a
// terminal. go-isatty answers it; the stdlib way — os.Stdin.Stat and
// ModeCharDevice — misreads a character device such as /dev/null as a
// terminal, and a prompt read back from /dev/null would end the run on
// an empty answer.
func stdinIsTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}
