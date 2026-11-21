//go:build unix

package guard

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes an exclusive, non-blocking flock on the open file. The
// hold belongs to the open file description, so a second Acquire — in the
// same process on another descriptor, or in any other process — refuses.
func lockFile(file *os.File) error {
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue // a signal interrupted the wait for the lock; try again
		case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EAGAIN):
			return errHeld
		default:
			return err
		}
	}
}

// unlockFile drops the exclusive hold; closing the descriptor would
// release it too, but the explicit drop keeps both platforms symmetric.
func unlockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
