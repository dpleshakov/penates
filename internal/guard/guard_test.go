package guard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSecondAcquireRefuses(t *testing.T) {
	dir := t.TempDir()
	g, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = g.Release() }()

	_, err = Acquire(dir)
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("the second Acquire error = %v, want a BusyError", err)
	}
	if busy.Dir != dir {
		t.Errorf("BusyError.Dir = %q, want %q", busy.Dir, dir)
	}
	want := "another penates run holds " + dir + " (close it or retry)"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestReleaseLetsTheGuardGo(t *testing.T) {
	dir := t.TempDir()
	g, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := g.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	g2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	if err := g2.Release(); err != nil {
		t.Fatalf("Release after re-Acquire: %v", err)
	}
}

func TestAcquireCreatesTheDirectoryAndTheLockFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "penates")
	g, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = g.Release() }()

	st, err := os.Stat(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatalf("the lock file: %v", err)
	}
	if st.IsDir() {
		t.Errorf("the lock file is a directory")
	}
}

// The lock file survives a release. Unlinking it would hand a third
// Acquire a new file under the same name while the second still holds
// the old one — two guards over one directory.
func TestReleaseKeepsTheLockFile(t *testing.T) {
	dir := t.TempDir()
	g, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := g.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, lockName)); err != nil {
		t.Errorf("the lock file after Release: %v", err)
	}
}

// A directory that cannot be made is a plain failure, not a busy
// refusal: nothing holds the guard, there is no run to wait out.
func TestUnmakeableDataDirIsNotBusy(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Acquire(filepath.Join(blocker, "penates"))
	var busy *BusyError
	if errors.As(err, &busy) {
		t.Errorf("error = %v, want a plain failure", err)
	}
	if err == nil {
		t.Fatal("Acquire under a regular file path succeeded")
	}
}
