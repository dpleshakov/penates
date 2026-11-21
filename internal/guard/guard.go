// Package guard is the run guard of penates: one exclusive lock per
// process over the penates data directory, so two penates processes can
// never work on the same state.
//
// The lock is a whole-process hold, not a short one around each write.
// The clients keep their state in memory across a whole run — the lock
// object a mutation saves rides in memory from load to save — so a short
// lock would still let two runs that each saved a "whole" file merge
// their changes silently, which the concept rules out (concept:
// application state). The guard is taken before the first access to the
// data directory and held until the process ends; the process's death
// closes the descriptor and releases the hold on its own.
//
// The guard refuses, it does not wait: an overlapping run — a second
// terminal UI, any CLI command while the interface is open — gets the
// busy refusal at once and the caller retries when it chooses to. The
// hold spans an interactive session, so waiting would block a script for
// as long as a person keeps the interface open.
//
// The platforms differ in one file each (lock_unix.go, lock_windows.go);
// both use only the standard library. The lock file is left behind on
// release — an empty file beside the state, never unlinked: unlinking a
// held lock would let a third process create a new file and hold a lock
// the second process does not share.
package guard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// lockName is the file the run guard holds in the penates data directory.
const lockName = "penates.lock"

// errHeld is the platform lock's verdict that another handle holds the
// lock; Acquire turns it into a BusyError.
var errHeld = errors.New("run guard held by another process")

// BusyError reports that another penates process holds the run guard:
// its message names the data directory and the way out. The command-line
// client reports it under the busy category.
type BusyError struct {
	// Dir is the penates data directory the held guard covers.
	Dir string
}

// Error renders the refusal in the contract wording of the clients.
func (e *BusyError) Error() string {
	return fmt.Sprintf("another penates run holds %s (close it or retry)", e.Dir)
}

// Guard is a held run guard. Keep it referenced for as long as the hold
// must last — a real run keeps it until the process ends.
type Guard struct {
	file *os.File
	dir  string
}

// Acquire takes the exclusive run guard over dir, the penates data
// directory: it creates the directory and the lock file when they do not
// exist yet and locks the file, refusing with a BusyError when another
// process holds it. The callers take it before anything else touches the
// directory, so a refused run reads and writes nothing there.
func Acquire(dir string) (*Guard, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, lockName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the guard builds the path itself
	if err != nil {
		return nil, err
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		if errors.Is(err, errHeld) {
			return nil, &BusyError{Dir: dir}
		}
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	return &Guard{file: file, dir: dir}, nil
}

// Release lets the hold go, so another Acquire — the next run in the
// same process, a test's — can take it. The command-line client
// releases at the end of each Run; a hold no one releases ends with
// the process, whose death releases it on its own.
func (g *Guard) Release() error {
	err := unlockFile(g.file)
	if cerr := g.file.Close(); err == nil {
		err = cerr
	}
	return err
}
