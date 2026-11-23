//go:build windows

package guard

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// The LockFileEx flags and its verdict for a held range. The stdlib
// syscall package carries no constants for either; the values are fixed
// by the Windows API.
const (
	lockFailImmediately = 0x1               // LOCKFILE_FAIL_IMMEDIATELY
	lockExclusive       = 0x2               // LOCKFILE_EXCLUSIVE_LOCK
	errLockViolation    = syscall.Errno(33) // ERROR_LOCK_VIOLATION
)

// kernel32 carries the file-locking calls; the syscall package has no
// direct bindings for them, and its DLL plumbing is the standard-library
// way to reach one.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

// lockFile takes an exclusive, no-wait LockFileEx hold on one byte of the
// open file. The hold belongs to the file handle, so a second Acquire —
// in the same process on another handle, or in any other process —
// refuses with ERROR_LOCK_VIOLATION.
func lockFile(file *os.File) error {
	// One byte at offset zero is the whole range the guard needs: a file
	// lock is about ownership, not content.
	var ol syscall.Overlapped
	r1, _, err := procLockFileEx.Call(
		file.Fd(),
		lockFailImmediately|lockExclusive,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ol)), //nolint:gosec // the Overlapped pointer is the syscall call's own shape; the caller's live reference holds it past the call
	)
	if r1 != 0 {
		return nil
	}
	if errors.Is(err, errLockViolation) {
		return errHeld
	}
	return err
}

// unlockFile drops the exclusive hold; closing the handle would release
// it too, but the explicit drop keeps both platforms symmetric.
func unlockFile(file *os.File) error {
	var ol syscall.Overlapped
	r1, _, err := procUnlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol))) //nolint:gosec // the Overlapped pointer is the syscall call's own shape; the caller's live reference holds it past the call
	if r1 != 0 {
		return nil
	}
	return err
}
