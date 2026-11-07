// Package apply executes a plan step by step: every step applies or removes one
// item completely or not at all, writes atomically through temp files, records
// the lock only for completed items, and reconciles after an interruption.
package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/target"
)

// Result reports the outcome of applying one item.
type Result struct {
	// Key is the item key.
	Key string
	// Err is set when the item failed; its message says why.
	Err error
}

// StepEvent reports one boundary of a step: the start of a step, then its
// result. A client shows per-item progress from the stream of events.
type StepEvent struct {
	// Key is the item key the step executes.
	Key string
	// Start marks the start boundary; the result boundary reports it false.
	Start bool
	// Err is the step's failure, set on the result boundary only.
	Err error
}

// emit reports one step boundary to on, which may be nil.
func emit(on func(StepEvent), ev StepEvent) {
	if on != nil {
		on(ev)
	}
}

// Apply executes every installable item of a plan into the target root, one
// step per item. Each item is staged in full — every file written to a temp
// name beside its destination — before anything is renamed into place, and the
// lock records the item only after all of its files are in place, so the lock
// never describes an item that is not fully applied. A failing item leaves no
// files and no empty directories behind; the items around it are unaffected.
// on, when not nil, sees the start and the result of every step, so a client
// can show per-item progress. The caller plans from a snapshot fresh to the
// target and hands the steps that plan: a step writes what its planning
// observed, nothing more.
func Apply(root string, p *plan.Plan, lk *lock.Lock, store *lock.Store, on func(StepEvent)) []Result {
	var results []Result
	for _, ip := range p.Installable() {
		emit(on, StepEvent{Key: ip.Key, Start: true})
		err := Item(root, ip, lk, store)
		emit(on, StepEvent{Key: ip.Key, Err: err})
		if err != nil {
			results = append(results, Result{Key: ip.Key, Err: err})
			continue
		}
		results = append(results, Result{Key: ip.Key})
	}
	return results
}

// Item executes one install step: it applies the item plan completely or
// not at all, writing atomically through temp files, and the lock records the
// item only after all of its files are in place, so the lock never describes
// an item that is not fully applied. A failing step leaves no files and no
// empty directories behind.
func Item(root string, ip *plan.ItemPlan, lk *lock.Lock, store *lock.Store) error {
	var createdDirs []string
	var staged []string
	fail := func(err error) error {
		cleanup(root, staged, createdDirs)
		return err
	}

	files := make(map[string]string, len(ip.Actions))
	sections := make(map[string]lock.Section)
	existed := make(map[string]bool, len(ip.Actions))

	// Stage every file beside its destination, creating directories as needed.
	for _, a := range ip.Actions {
		dst := filepath.Join(root, filepath.FromSlash(a.To))
		if _, err := os.Stat(dst); err == nil {
			existed[a.To] = true
		} else if !os.IsNotExist(err) {
			return fail(fmt.Errorf("inspect %s: %w", a.To, err))
		}
		dirs, err := mkdirAllTracking(root, filepath.Dir(dst))
		if err != nil {
			return fail(fmt.Errorf("create directory for %s: %w", a.To, err))
		}
		createdDirs = append(createdDirs, dirs...)

		tmp, err := stage(dst, a.Content)
		if err != nil {
			return fail(fmt.Errorf("stage %s: %w", a.To, err))
		}
		staged = append(staged, tmp)
		switch a.Kind {
		case plan.ActionSection:
			sections[a.To] = lock.Section{Block: a.Block, Hash: a.BlockHash}
		case plan.ActionJSON:
			// The composed file bytes are staged and renamed like every other
			// action; the locations the item owns are recorded from the item
			// plan, which covers satisfied and co-owned values without actions.
		default:
			files[a.To] = hashOf(a.Content)
		}
	}

	// Everything is staged: move the files into place.
	for i, a := range ip.Actions {
		dst := filepath.Join(root, filepath.FromSlash(a.To))
		if err := os.Rename(staged[i], dst); err != nil {
			return fail(fmt.Errorf("write %s: %w", a.To, err))
		}
	}

	// Shared files this item brought into existence belong to penates as long as
	// they exist.
	var createdShared []string
	for _, a := range ip.Actions {
		sharedKind := a.Kind == plan.ActionSection || a.Kind == plan.ActionJSON
		if sharedKind && !existed[a.To] && !containsPath(lk.CreatedSharedFiles, a.To) {
			createdShared = append(createdShared, a.To)
		}
	}
	if len(createdShared) > 0 {
		lk.CreatedSharedFiles = append(lk.CreatedSharedFiles, createdShared...)
		sort.Strings(lk.CreatedSharedFiles)
	}

	rec := &lock.Item{
		Version:     ip.Version,
		Answers:     answersOf(ip),
		Files:       files,
		DirsCreated: createdDirs,
		Sections:    sections,
		JSON:        jsonLocations(ip.JSONLocations),
	}
	// Content the plan keeps standing — a file or block recorded in the lock
	// that this plan writes no action for — stays recorded with its
	// installation base: the three-state rules of a later update must still
	// see the user's modification against it. The directories of
	// the earlier install stay recorded, so a removal still cleans them up
	// while they are empty.
	if old := lk.Items[ip.Key]; old != nil {
		for path, hash := range old.Files {
			if _, written := rec.Files[path]; !written {
				rec.Files[path] = hash
			}
		}
		for file, sec := range old.Sections {
			if _, written := rec.Sections[file]; !written {
				rec.Sections[file] = sec
			}
		}
		for _, dir := range old.DirsCreated {
			if !containsPath(rec.DirsCreated, dir) {
				rec.DirsCreated = append(rec.DirsCreated, dir)
			}
		}
	}
	lk.SetItem(ip.Key, rec)
	if err := store.Save(lk); err != nil {
		return fail(fmt.Errorf("save lock: %w", err))
	}
	return nil
}

// Remove executes every removal item of a plan into the target root, one step
// per item. Content actions are staged like writes and renamed into place only
// after the whole item is staged, and the deletions happen after the staging,
// so a failing item leaves the target as it was. On success the item's lock
// record dies, the shared files deleted here leave the created list, and the
// directories penates created for the item are removed while they are empty.
// on, when not nil, sees the start and the result of every step, so a client
// can show per-item progress. The caller plans from a snapshot fresh to the
// target and hands the steps that plan: a step writes what its planning
// observed, nothing more.
func Remove(root string, p *plan.Plan, lk *lock.Lock, store *lock.Store, on func(StepEvent)) []Result {
	var results []Result
	for _, ip := range p.Removals() {
		emit(on, StepEvent{Key: ip.Key, Start: true})
		err := RemoveItem(root, ip, lk, store)
		emit(on, StepEvent{Key: ip.Key, Err: err})
		if err != nil {
			results = append(results, Result{Key: ip.Key, Err: err})
			continue
		}
		results = append(results, Result{Key: ip.Key})
	}
	return results
}

// RemoveItem executes one removal step: it rewrites and deletes what the item
// plan describes, and the lock stops recording the item only after the target
// is rewritten and the deletions are done. A failing step leaves the target as
// it was and the item recorded.
func RemoveItem(root string, ip *plan.ItemPlan, lk *lock.Lock, store *lock.Store) error {
	rec := lk.Items[ip.Key]
	if rec == nil {
		return fmt.Errorf("remove %s: the item is not recorded in the lock", ip.Key)
	}
	var staged []string
	fail := func(err error) error {
		cleanup(root, staged, nil)
		return err
	}

	type write struct {
		to  string
		tmp string
	}
	var writes []write
	var deletes []string
	for _, a := range ip.Actions {
		dst := filepath.Join(root, filepath.FromSlash(a.To))
		switch a.Kind {
		case plan.ActionRemove:
			deletes = append(deletes, dst)
		case plan.ActionSectionRemove, plan.ActionJSONRemove:
			tmp, err := stage(dst, a.Content)
			if err != nil {
				return fail(fmt.Errorf("stage %s: %w", a.To, err))
			}
			staged = append(staged, tmp)
			writes = append(writes, write{to: dst, tmp: tmp})
		default:
			return fail(fmt.Errorf("remove %s: the action kind %q is not a removal", ip.Key, a.Kind))
		}
	}

	// Everything is staged: move the files into place, then take the deletions.
	for _, w := range writes {
		if err := os.Rename(w.tmp, w.to); err != nil {
			return fail(fmt.Errorf("write %s: %w", w.to, err))
		}
	}
	for _, dst := range deletes {
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return fail(fmt.Errorf("delete %s: %w", dst, err))
		}
	}

	// Directories penates created for the item go while they are empty,
	// deepest first; a directory that holds user files stays.
	dirs := append([]string(nil), rec.DirsCreated...)
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(dir)))
	}

	// A shared file deleted here also leaves the created list.
	for _, a := range ip.Actions {
		if a.Kind == plan.ActionRemove {
			lk.CreatedSharedFiles = withoutPath(lk.CreatedSharedFiles, a.To)
		}
	}
	lk.RemoveItem(ip.Key)
	if err := store.Save(lk); err != nil {
		return fail(fmt.Errorf("save lock: %w", err))
	}
	return nil
}

// Reconcile reconciles the lock with the target after an interruption:
// the staged temp files an interrupted apply left beside their
// destinations are removed, and the installed items whose recorded content
// the target holds modified or has lost are reported. It returns the
// reconciliation and the staged paths it removed, relative to root with
// slash separators — the evidence that an apply was interrupted at all,
// for the client to report. snap must cover the paths the lock records;
// the client reads them with lock.RecordedPaths.
func Reconcile(root string, lk *lock.Lock, snap *target.Snapshot) (*plan.Reconciliation, []string, error) {
	staged, err := removeStaged(root)
	if err != nil {
		return nil, nil, err
	}
	return plan.Reconcile(lk, snap), staged, nil
}

// stagedPrefix names the temp files stage writes beside their destinations.
const stagedPrefix = ".penates-"

// removeStaged deletes the temp files of an interrupted apply: regular files
// named .penates-* anywhere under root, returned as root-relative slash
// paths. A target that does not exist yet has nothing to clean; directories
// and anything else the pattern may name are left alone.
func removeStaged(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	// The walk covers the user's target directory: penates itself creates no
	// symlinks (concept principle 7), the stdlib offers no root-scoped walk,
	// and a racing local writer is outside this tool's threat model.
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || !strings.HasPrefix(d.Name(), stagedPrefix) {
			return nil
		}
		if err := os.Remove(path); err != nil { //nolint:gosec // the target is the user's directory; penates creates no symlinks (concept 7)
			return fmt.Errorf("remove staged file %s: %w", path, err)
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		removed = append(removed, filepath.ToSlash(rel))
		return nil
	})
	return removed, err
}

// withoutPath drops one path from a list, keeping the order of the rest.
func withoutPath(list []string, p string) []string {
	var out []string
	for _, el := range list {
		if el != p {
			out = append(out, el)
		}
	}
	return out
}

// answersOf normalizes the plan's answers for the lock: an item with no
// variables records an empty, not a null, map.
func answersOf(ip *plan.ItemPlan) map[string]string {
	if ip.Answers == nil {
		return map[string]string{}
	}
	return ip.Answers
}

// jsonLocations converts the plan's owned locations into their lock form. An
// item may own locations in a file it did not write: values found satisfied
// and made co-owned bring no action, yet the lock records them.
func jsonLocations(locs map[string][]plan.Location) map[string][]lock.Location {
	if len(locs) == 0 {
		return nil
	}
	out := make(map[string][]lock.Location, len(locs))
	for file, list := range locs {
		converted := make([]lock.Location, len(list))
		for i, loc := range list {
			converted[i] = lock.Location{Pointer: loc.Pointer, Hash: loc.Hash}
		}
		out[file] = converted
	}
	return out
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func containsPath(list []string, p string) bool {
	for _, el := range list {
		if el == p {
			return true
		}
	}
	return false
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
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := os.Chmod(name, 0o644); err != nil { //nolint:gosec // an installed file is an ordinary user file, not a secret
		_ = os.Remove(name)
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
		// The action targets the root itself; create it when it is missing. The
		// root is the user's chosen target, not a directory penates created for
		// the item, so it is not recorded.
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the target root is an ordinary user directory
			return nil, err
		}
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
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // target directories are ordinary user directories
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
		_ = os.Remove(tmp)
	}
	for i := len(createdDirs) - 1; i >= 0; i-- {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(createdDirs[i])))
	}
}
