package cli

import (
	"errors"
	"fmt"
	"io"
)

// The error contract of the command line, one stderr line per failure:
//
//	Error [category]: message
//
// The message names the concrete next step — a corrected command or flag, a
// value to pass, or the person the decision belongs to; an error without a
// next step is a defect. The categories are a stable contract: existing
// categories are never removed or renamed, new ones may be added. The set
// lives here and in the built-in kit's reference skill, and nowhere else.

// Category is a machine-readable error category of the CLI contract.
type Category string

const (
	// CategoryUsage — the command line itself is wrong or incomplete: an
	// unknown command or flag, a misplaced flag, a malformed --set, an
	// unknown variable id, a required variable without a value,
	// incompatible flags. The only category that exits 2: repeating the
	// same line cannot help.
	CategoryUsage Category = "usage"
	// CategoryNotFound — a named thing does not exist: an item the kits
	// do not provide and the lock does not hold, an unregistered kit or
	// target, removing or resetting an uninstalled item.
	CategoryNotFound Category = "not_found"
	// CategoryConflict — the planner refused a write under the ownership
	// rules: user content in the path, a place owned by another item,
	// changed content against changed kit content, claims between items.
	// A registration that collides with the state the registry already
	// holds is a conflict too: a kit already attached, a kit or target
	// name already registered. The message carries the conflict's own
	// prose.
	CategoryConflict Category = "conflict"
	// CategoryKit — the kit content is at fault: an unreadable kit.yaml,
	// a broken item declaration, an unknown placeholder in a template.
	// The next step is fixing the kit.
	CategoryKit Category = "kit"
	// CategoryStaged — a one-shot command refused because the target
	// holds staged pending actions; the message names them and the three
	// ways out: apply, clear, or ask the person who staged them.
	CategoryStaged Category = "staged"
	// CategoryBusy — another penates process holds the state; close it
	// or retry.
	CategoryBusy Category = "busy"
	// CategoryConfirmation — an asking command refused to run without
	// --yes and without a terminal to ask on.
	CategoryConfirmation Category = "confirmation"
	// CategoryState — penates' own state is unreadable or incompatible:
	// a state file of another format version, broken JSON, an unreadable
	// data directory. The message names the file and the way out.
	CategoryState Category = "state"
	// CategoryIO — a file-system failure outside the state: an unreadable
	// kit path, a target that cannot be read, a write that failed. Also
	// the fallback for an error that reached printing without a category.
	CategoryIO Category = "io"
)

// Error is the typed error of the contract: a category, the one-line
// message, and the cause for errors.Is and errors.As to reach through.
type Error struct {
	Category Category
	Message  string
	Cause    error
	// Details are plain lines printed under the Error line, on the same
	// stderr: the itemized expansion of a refusal whose Error line stays
	// one line long, such as the list of staged pending actions. They are
	// ordinary text, not part of the error format — nothing parses them.
	Details []string
}

// Error renders the error for the error interface: "[category] message".
// The user-facing rendering is Print.
func (e *Error) Error() string {
	return fmt.Sprintf("[%s] %s", e.Category, e.Message)
}

// Unwrap returns the underlying error, so that errors.Is and errors.As
// reach through the category wrapper.
func (e *Error) Unwrap() error { return e.Cause }

// Print writes err to w in the contract format, one line:
// `Error [category]: message`, followed by the error's detail lines when
// it carries any. An error that is not, and does not wrap, an *Error
// prints under the fallback category io: `Error [io]: message`.
// A nil err writes nothing.
func Print(err error, w io.Writer) {
	if err == nil {
		return
	}
	var e *Error
	if errors.As(err, &e) {
		_, _ = fmt.Fprintf(w, "Error [%s]: %s\n", e.Category, e.Message)
		for _, d := range e.Details {
			_, _ = fmt.Fprintln(w, d)
		}
		return
	}
	_, _ = fmt.Fprintf(w, "Error [%s]: %s\n", CategoryIO, err)
}

// ExitCode maps err to the process exit code. The codes are a stable
// contract of exactly three values:
//
//	0 — the command fully did what it was asked: success, including the
//	    empty ones (nothing staged, nothing to update), a clean --dry-run
//	    plan, and --help;
//	1 — any other failure: an operational failure, a refusal, a
//	    confirmation the command could not ask for, a partially failed
//	    command;
//	2 — the usage category alone: the command line is wrong, and no
//	    repeat of it can help.
//
// An error that is not, and does not wrap, an *Error exits 1; nil exits 0.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if errors.As(err, &e) && e.Category == CategoryUsage {
		return 2
	}
	return 1
}
