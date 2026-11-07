package plan

import (
	"github.com/dpleshakov/penates/internal/jsonc"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/target"
)

// Reconciliation is the outcome of comparing the lock with the target by
// recorded hashes: which installed items the target no longer
// describes faithfully. Both sets stay nil while every item stands as
// recorded.
type Reconciliation struct {
	// Modified holds the item keys whose recorded content the target holds
	// in another form: a file, a block, or a json value changed after
	// installation without the lock knowing.
	Modified map[string]bool
	// Broken holds the item keys whose recorded content the target lost: a
	// deleted file, a removed block, a gone value, a shared file that no
	// longer holds a document.
	Broken map[string]bool
}

// BrokenItem reports whether reconciliation found the item's recorded content
// missing from the target. The zero outcome has broken nothing.
func (r *Reconciliation) BrokenItem(key string) bool {
	return r != nil && r.Broken[key]
}

// ModifiedItem reports whether reconciliation found the item's recorded
// content changed in the target. The zero outcome has modified nothing.
func (r *Reconciliation) ModifiedItem(key string) bool {
	return r != nil && r.Modified[key]
}

// Reconcile compares every recorded hash of every installed item with the
// snapshot: content that differs marks its item modified, content
// that is missing marks it broken. It is pure: snap must cover the paths the
// lock records — see lock.RecordedPaths — and nothing is written.
func Reconcile(lk *lock.Lock, snap *target.Snapshot) *Reconciliation {
	modified := map[string]bool{}
	broken := map[string]bool{}
	for key, item := range lk.Items {
		for path, hash := range item.Files {
			st := snap.File(path)
			switch {
			case !st.Exists || st.Hash == "":
				// The file is gone, or a directory took its place; either
				// way the recorded content is lost.
				broken[key] = true
			case st.Hash != hash:
				modified[key] = true
			}
		}
		for file, sec := range item.Sections {
			st := snap.SharedFile(file)
			block, present := st.Blocks[sec.Block]
			switch {
			case !st.Exists || st.IsDir || !present:
				// The shared file or the owned block is gone.
				broken[key] = true
			case block != sec.Hash:
				modified[key] = true
			}
		}
		for file, locs := range item.JSON {
			reconcileJSON(key, file, locs, snap, modified, broken)
		}
	}
	if len(modified) == 0 {
		modified = nil
	}
	if len(broken) == 0 {
		broken = nil
	}
	return &Reconciliation{Modified: modified, Broken: broken}
}

// reconcileJSON checks the json locations of one item against the snapshot: a
// value the file holds in another form marks the item modified, a lost one
// marks it broken. Arrays are sets: an element stands while the array holds
// an element with the recorded hash.
func reconcileJSON(key, file string, locs []lock.Location, snap *target.Snapshot, modified, broken map[string]bool) {
	st := snap.SharedFile(file)
	if !st.Exists || st.IsDir {
		broken[key] = true
		return
	}
	doc, err := jsonc.Parse([]byte(st.Content))
	if err != nil {
		// The file no longer holds a document; the recorded values are lost
		// with it.
		broken[key] = true
		return
	}
	for _, loc := range locs {
		min, found, err := doc.Get(loc.Pointer)
		if err != nil || !found {
			broken[key] = true // the location is gone
			continue
		}
		if min[0] == '[' {
			// The lock records array elements under the pointer of their
			// array; the value stands while any element matches.
			elems, _, err := doc.Elements(loc.Pointer)
			if err != nil {
				broken[key] = true
				continue
			}
			present := false
			for _, e := range elems {
				if hashOf(e) == loc.Hash {
					present = true
					break
				}
			}
			if !present {
				broken[key] = true
			}
			continue
		}
		if hashOf(min) != loc.Hash {
			modified[key] = true
		}
	}
}
