// Package target reads the target directory into a snapshot: file existence, content
// hashes, and the structure of shared files — Markdown block layout and JSONC trees.
// All target file-system access lives in this package.
package target

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileState is the observed state of one target file.
type FileState struct {
	// Exists reports whether anything occupies the path: a regular file, or a
	// directory where a file was expected.
	Exists bool
	// Hash is "sha256:<hex>" of the content of a regular file; empty for a missing
	// path or a directory.
	Hash string
}

// Snapshot is a plain value describing the observed files of a target directory. It
// is built for a list of paths so that the planner stays free of I/O.
type Snapshot struct {
	// Root is the absolute path of the target directory.
	Root string
	// Files maps each requested target-relative path, with forward slashes, to its
	// state.
	Files map[string]FileState
}

// SnapshotFiles reads the state of paths under root. Paths are target-relative with
// forward slashes; a path that is not a plain relative path is an error.
func SnapshotFiles(root string, paths []string) (*Snapshot, error) {
	snap := &Snapshot{Root: root, Files: make(map[string]FileState, len(paths))}
	for _, p := range paths {
		state, err := snapshotFile(root, p)
		if err != nil {
			return nil, err
		}
		snap.Files[p] = state
	}
	return snap, nil
}

// File returns the observed state of one target-relative path.
func (s *Snapshot) File(path string) FileState {
	return s.Files[path]
}

func snapshotFile(root, rel string) (FileState, error) {
	if !validRelPath(rel) {
		return FileState{}, fmt.Errorf("target path %q is not a relative slash-separated path", rel)
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return FileState{}, nil
	}
	if err != nil {
		return FileState{}, err
	}
	if info.IsDir() {
		return FileState{Exists: true}, nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return FileState{}, err
	}
	sum := sha256.Sum256(data)
	return FileState{Exists: true, Hash: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

// validRelPath reports whether p is a non-empty path of forward-slash-separated
// plain elements: no absolute paths, no "." or ".." elements, no backslashes, no
// drive letters. Kit-declared paths are checked the same way in internal/kit.
func validRelPath(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\:") {
		return false
	}
	for _, el := range strings.Split(p, "/") {
		if el == "" || el == "." || el == ".." {
			return false
		}
	}
	return true
}
