// Package target reads the target directory into a snapshot: file existence, content
// hashes, and the structure of shared files — Markdown block layout and full content
// for the shared-file slices. All target file-system access lives in this package.
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

	"github.com/dpleshakov/penates/internal/markdown"
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

// SharedFileState is the observed state of one shared file.
type SharedFileState struct {
	// Exists reports whether anything occupies the path.
	Exists bool
	// IsDir reports whether a directory occupies the path.
	IsDir bool
	// Content is the full file content of a regular file; empty otherwise.
	Content string
	// Blocks maps each managed block id to "sha256:<hex>" of its content.
	Blocks map[string]string
	// Headings lists the lowercased heading texts outside managed blocks.
	Headings []string
}

// BlockHash returns the recorded hash of one managed block, if present.
func (s SharedFileState) BlockHash(id string) (string, bool) {
	h, ok := s.Blocks[id]
	return h, ok
}

// Snapshot is a plain value describing the observed files of a target directory. It
// is built for lists of paths so that the planner stays free of I/O.
type Snapshot struct {
	// Root is the absolute path of the target directory.
	Root string
	// Files maps each requested target-relative path, with forward slashes, to its
	// state.
	Files map[string]FileState
	// Shared maps each requested shared-file path, with forward slashes, to its
	// parsed state.
	Shared map[string]SharedFileState
}

// Read observes paths under root. Files are target-relative paths with forward
// slashes reduced to existence and hashes; shared paths are read in full and parsed
// for managed Markdown blocks. A path that is not a plain relative path is an error.
func Read(root string, files, shared []string) (*Snapshot, error) {
	snap := &Snapshot{
		Root:   root,
		Files:  make(map[string]FileState, len(files)),
		Shared: make(map[string]SharedFileState, len(shared)),
	}
	for _, p := range files {
		state, err := snapshotFile(root, p)
		if err != nil {
			return nil, err
		}
		snap.Files[p] = state
	}
	for _, p := range shared {
		state, err := snapshotShared(root, p)
		if err != nil {
			return nil, err
		}
		snap.Shared[p] = state
	}
	return snap, nil
}

// File returns the observed state of one target-relative path.
func (s *Snapshot) File(path string) FileState {
	return s.Files[path]
}

// SharedFile returns the observed state of one shared-file path.
func (s *Snapshot) SharedFile(path string) SharedFileState {
	return s.Shared[path]
}

func snapshotFile(root, rel string) (FileState, error) {
	if !validRelPath(rel) {
		return FileState{}, fmt.Errorf("target path %q is not a relative slash-separated path", rel)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return FileState{}, nil
	}
	if err != nil {
		return FileState{}, err
	}
	if info.IsDir() {
		return FileState{Exists: true}, nil
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // rel passed validRelPath above; root is the user's target
	if err != nil {
		return FileState{}, err
	}
	return FileState{Exists: true, Hash: hashOf(data)}, nil
}

func snapshotShared(root, rel string) (SharedFileState, error) {
	if !validRelPath(rel) {
		return SharedFileState{}, fmt.Errorf("target path %q is not a relative slash-separated path", rel)
	}
	state := SharedFileState{Blocks: map[string]string{}}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if info.IsDir() {
		return SharedFileState{Exists: true, IsDir: true, Blocks: map[string]string{}}, nil
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // rel passed validRelPath above; root is the user's target
	if err != nil {
		return state, err
	}
	state.Exists = true
	state.Content = string(data)
	lay := markdown.Parse(state.Content)
	for _, b := range lay.Blocks {
		state.Blocks[b.ID] = hashOf([]byte(b.Content))
	}
	state.Headings = lay.Headings
	return state, nil
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
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
