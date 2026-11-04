// Package apply executes a plan: applies an item completely or not at all, writes
// atomically through temp files, updates the lock on success, and reconciles after an
// interruption.
package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
)

// Result reports the outcome of applying one item.
type Result struct {
	// Key is the item key.
	Key string
	// Err is set when the item failed; its message says why.
	Err error
}

// Apply executes every installable item of a plan into the target root. Each item is
// staged in full — every file written to a temp name beside its destination — before
// anything is renamed into place, and the lock records the item only after all of
// its files are in place, so the lock never describes an item that is not fully
// applied. A failing item leaves no files and no empty directories behind; the items
// around it are unaffected (RFC 7.7).
func Apply(root string, p *plan.Plan, lk *lock.Lock, store *lock.Store) []Result {
	var results []Result
	for _, ip := range p.Installable() {
		if err := applyItem(root, ip, lk, store); err != nil {
			results = append(results, Result{Key: ip.Key, Err: err})
			continue
		}
		results = append(results, Result{Key: ip.Key})
	}
	return results
}

func applyItem(root string, ip *plan.ItemPlan, lk *lock.Lock, store *lock.Store) error {
	// Read every source before touching the target, so a broken kit fails the item
	// with the target unchanged.
	contents := make([][]byte, len(ip.Actions))
	for i, a := range ip.Actions {
		data, err := os.ReadFile(a.Src)
		if err != nil {
			return fmt.Errorf("read %s: %w", a.Src, err)
		}
		contents[i] = data
	}

	var createdDirs []string
	var staged []string
	fail := func(err error) error {
		cleanup(root, staged, createdDirs)
		return err
	}

	// Stage every file beside its destination, creating directories as needed.
	files := make(map[string]string, len(ip.Actions))
	for i, a := range ip.Actions {
		dst := filepath.Join(root, filepath.FromSlash(a.To))
		dirs, err := mkdirAllTracking(root, filepath.Dir(dst))
		if err != nil {
			return fail(fmt.Errorf("create directory for %s: %w", a.To, err))
		}
		createdDirs = append(createdDirs, dirs...)

		sum := sha256.Sum256(contents[i])
		tmp, err := stage(dst, contents[i])
		if err != nil {
			return fail(fmt.Errorf("stage %s: %w", a.To, err))
		}
		staged = append(staged, tmp)
		files[a.To] = "sha256:" + hex.EncodeToString(sum[:])
	}

	// Everything is staged: move the files into place.
	for i, a := range ip.Actions {
		dst := filepath.Join(root, filepath.FromSlash(a.To))
		if err := os.Rename(staged[i], dst); err != nil {
			return fail(fmt.Errorf("write %s: %w", a.To, err))
		}
	}

	lk.SetItem(ip.Key, &lock.Item{
		Version:     ip.Version,
		Answers:     map[string]string{},
		Files:       files,
		DirsCreated: createdDirs,
	})
	if err := store.Save(lk); err != nil {
		return fail(fmt.Errorf("save lock: %w", err))
	}
	return nil
}

// stage writes content to a fresh temp file in the destination's directory and
// returns its path; the rename into place happens only after the whole item is
// staged.
func stage(dst string, content []byte) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".penates-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// mkdirAllTracking creates dir and its missing parents under root, returning the
// created directories as target-relative slash paths, outermost first.
func mkdirAllTracking(root, dir string) ([]string, error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, nil
	}
	var missing []string
	cur := dir
	for {
		if _, err := os.Stat(cur); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		missing = append(missing, cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// missing was collected innermost-first; record outermost-first.
	var created []string
	for i := len(missing) - 1; i >= 0; i-- {
		rel, err := filepath.Rel(root, missing[i])
		if err != nil {
			return nil, err
		}
		created = append(created, filepath.ToSlash(rel))
	}
	return created, nil
}

// cleanup removes staged temp files and directories the failed item created, deepest
// first, so that a failed item leaves nothing behind. Directories that are no longer
// empty are kept: something else put content there.
func cleanup(root string, staged, createdDirs []string) {
	for _, tmp := range staged {
		os.Remove(tmp)
	}
	for i := len(createdDirs) - 1; i >= 0; i-- {
		os.Remove(filepath.Join(root, filepath.FromSlash(createdDirs[i])))
	}
}
