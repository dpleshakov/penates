package cli

import (
	"bytes"
	"errors"
	"testing"
)

// The error contract, pinned: the format of the one stderr line, the
// fallback category, and the three exit codes.

func TestPrintTypedError(t *testing.T) {
	var buf bytes.Buffer
	Print(&Error{Category: CategoryUsage, Message: "unknown flag: --x (run \"penates --help\")"}, &buf)

	if want := "Error [usage]: unknown flag: --x (run \"penates --help\")\n"; buf.String() != want {
		t.Errorf("Print wrote %q, want %q", buf.String(), want)
	}
}

func TestPrintWrapsCauseInContractForm(t *testing.T) {
	var buf bytes.Buffer
	Print(&Error{Category: CategoryState, Message: "state is broken", Cause: errors.New("underlying")}, &buf)

	if want := "Error [state]: state is broken\n"; buf.String() != want {
		t.Errorf("Print wrote %q, want %q", buf.String(), want)
	}
}

// An error that reaches printing without a category prints under the
// fallback category io.
func TestPrintUntypedErrorFallsBackToIO(t *testing.T) {
	var buf bytes.Buffer
	Print(errors.New("a plain failure"), &buf)

	if want := "Error [io]: a plain failure\n"; buf.String() != want {
		t.Errorf("Print wrote %q, want %q", buf.String(), want)
	}
}

func TestPrintNilWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	Print(nil, &buf)

	if buf.Len() != 0 {
		t.Errorf("Print(nil) wrote %q, want nothing", buf.String())
	}
}

func TestErrorRendersAndUnwraps(t *testing.T) {
	cause := errors.New("the file is gone")
	err := &Error{Category: CategoryIO, Message: "cannot read the lock", Cause: cause}

	if want := "[io] cannot read the lock"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is does not reach the cause")
	}
	var e *Error
	if !errors.As(err, &e) || e.Category != CategoryIO {
		t.Errorf("errors.As does not reach the category")
	}
}

func TestExitCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, 0},
		{"usage", &Error{Category: CategoryUsage, Message: "wrong line"}, 2},
		{"usage wrapped", &Error{Category: CategoryUsage, Message: "wrong line", Cause: errors.New("pflag")}, 2},
		{"not found", &Error{Category: CategoryNotFound, Message: "no such item"}, 1},
		{"io", &Error{Category: CategoryIO, Message: "cannot read"}, 1},
		{"untyped", errors.New("a plain failure"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode = %d, want %d", got, tt.want)
			}
		})
	}
}
