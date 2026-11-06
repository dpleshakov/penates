// Package lock reads and writes the per-target lock: the record of installed items
// and of everything penates owns in the target — files, directories, sections, JSON
// locations, and shared files created by penates.
package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Item records one installed item and everything it owns in the target. Path-like
// keys are target-relative with forward slashes.
type Item struct {
	// Version is the item version that was installed.
	Version string `json:"version"`
	// Answers are the variable answers the item was installed with.
	Answers map[string]string `json:"answers"`
	// Files maps every installed file to the SHA-256 of its installed content.
	Files map[string]string `json:"files"`
	// DirsCreated lists every directory penates created for the item.
	DirsCreated []string `json:"dirs_created"`
	// Sections maps each shared file the item writes a Markdown block to, to the
	// block record.
	Sections map[string]Section `json:"sections"`
	// JSON maps each shared file the item contributes values to, to the owned
	// locations in it.
	JSON map[string][]Location `json:"json"`
}

// Section is one Markdown block an item owns inside a shared file.
type Section struct {
	// Block is the block identifier between the markers.
	Block string `json:"block"`
	// Hash is the SHA-256 of the content between the markers at installation.
	Hash string `json:"hash"`
}

// Location is one JSON value an item owns inside a shared file.
type Location struct {
	// Pointer is the RFC 6901 JSON Pointer of an object member, or of an array the
	// element value belongs to.
	Pointer string `json:"pointer"`
	// Hash is the SHA-256 of the written value.
	Hash string `json:"hash"`
}

// Lock is the per-target record of installed items and of everything penates owns
// in the target. It is self-sufficient: removal and modification detection work when
// the source kit is unavailable.
type Lock struct {
	// Target is the target directory the lock belongs to.
	Target string `json:"target"`
	// Items maps item keys — "kit name/item id" — to their records.
	Items map[string]*Item `json:"items"`
	// CreatedSharedFiles lists shared files penates created.
	CreatedSharedFiles []string `json:"created_shared_files"`
}

// Store reads and writes lock files under the locks directory of the penates data
// directory. One file per target, keyed by the target path.
type Store struct {
	// Dir is the locks directory.
	Dir string
}

// NewStore returns the lock store of the penates data directory dataDir.
func NewStore(dataDir string) *Store {
	return &Store{Dir: filepath.Join(dataDir, "locks")}
}

// Path returns the lock file path for a target.
func (s *Store) Path(target string) string {
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:])[:16]+".json")
}

// Load reads the lock of a target. A missing file is an empty lock, not an error;
// the target itself is recorded either way, so the lock is self-describing.
func (s *Store) Load(target string) (*Lock, error) {
	path := s.Path(target)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Lock{Target: target, Items: map[string]*Item{}}, nil
	}
	if err != nil {
		return nil, err
	}
	l := &Lock{}
	if err := json.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if l.Items == nil {
		l.Items = map[string]*Item{}
	}
	return l, nil
}

// Save writes the lock through a temp file and a rename, creating the locks
// directory if needed.
func (s *Store) Save(l *Lock) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	path := s.Path(l.Target)
	tmp, err := os.CreateTemp(s.Dir, ".lock-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// FileOwner returns the item key that owns a target file, with the recorded hash of
// its content at installation.
func (l *Lock) FileOwner(path string) (itemKey, hash string, ok bool) {
	for key, item := range l.Items {
		if h, owned := item.Files[path]; owned {
			return key, h, true
		}
	}
	return "", "", false
}

// OwnsFile reports whether an item owns a target file.
func (l *Lock) OwnsFile(itemKey, path string) bool {
	item, ok := l.Items[itemKey]
	if !ok {
		return false
	}
	_, owned := item.Files[path]
	return owned
}

// SectionOwner returns the item key that owns a block in a shared file, with the
// recorded hash of the block content at installation.
func (l *Lock) SectionOwner(file, block string) (itemKey, hash string, ok bool) {
	for key, item := range l.Items {
		if s, owned := item.Sections[file]; owned && s.Block == block {
			return key, s.Hash, true
		}
	}
	return "", "", false
}

// OwnsJSON reports whether an item owns one exact location of a shared JSON
// file: pointer and value hash. Array elements are recorded under the pointer
// of their array (RFC 7.4), so the pair identifies the element.
func (l *Lock) OwnsJSON(itemKey, file, pointer, hash string) bool {
	item, ok := l.Items[itemKey]
	if !ok {
		return false
	}
	for _, loc := range item.JSON[file] {
		if loc.Pointer == pointer && loc.Hash == hash {
			return true
		}
	}
	return false
}

// JSONOwners lists the item keys that own one exact location of a shared JSON
// file — the co-owners included — sorted. An empty list means the location, as
// it stands, is not recorded in the lock.
func (l *Lock) JSONOwners(file, pointer, hash string) []string {
	var owners []string
	for key := range l.Items {
		if l.OwnsJSON(key, file, pointer, hash) {
			owners = append(owners, key)
		}
	}
	sort.Strings(owners)
	return owners
}

// OwnsJSONPointer reports whether an item owns any value at one pointer of a
// shared JSON file: the pointer of a member it wrote, or of an array it owns
// elements in. It is the basis of the three-state evaluation of an owned
// member, which runs on the pointer whatever value it holds now.
func (l *Lock) OwnsJSONPointer(itemKey, file, pointer string) bool {
	item, ok := l.Items[itemKey]
	if !ok {
		return false
	}
	for _, loc := range item.JSON[file] {
		if loc.Pointer == pointer {
			return true
		}
	}
	return false
}

// CreatedDir reports whether an item had a directory created for it.
func (l *Lock) CreatedDir(itemKey, dir string) bool {
	item, ok := l.Items[itemKey]
	if !ok {
		return false
	}
	for _, d := range item.DirsCreated {
		if d == dir {
			return true
		}
	}
	return false
}

// CreatedShared reports whether penates recorded the shared file as one it
// created (RFC 6.7): such a file is deleted when a removal leaves it without
// content (RFC 9.1).
func (l *Lock) CreatedShared(path string) bool {
	for _, el := range l.CreatedSharedFiles {
		if el == path {
			return true
		}
	}
	return false
}

// RecordedPaths lists the target-relative paths the lock records: every owned
// file, and every shared file holding an owned section or json location, each
// sorted and without duplicates. A client snapshots them to reconcile the
// lock with the target (RFC 7.7).
func (l *Lock) RecordedPaths() (files, shared []string) {
	fileSet := map[string]bool{}
	sharedSet := map[string]bool{}
	for _, item := range l.Items {
		for path := range item.Files {
			fileSet[path] = true
		}
		for path := range item.Sections {
			sharedSet[path] = true
		}
		for path := range item.JSON {
			sharedSet[path] = true
		}
	}
	for path := range fileSet {
		files = append(files, path)
	}
	for path := range sharedSet {
		shared = append(shared, path)
	}
	sort.Strings(files)
	sort.Strings(shared)
	return files, shared
}

// SetItem records a fully applied item, replacing any earlier record of it.
func (l *Lock) SetItem(itemKey string, item *Item) {
	l.Items[itemKey] = item
}

// RemoveItem drops an item's record from the lock. Its return value reports whether
// a record was there to remove.
func (l *Lock) RemoveItem(itemKey string) bool {
	if _, ok := l.Items[itemKey]; !ok {
		return false
	}
	delete(l.Items, itemKey)
	return true
}
