package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the name of the state file inside the data directory.
const FileName = "state.json"

// SchemaVersion is the version of the state file format. The project has no
// releases and no live use, so a file of another version is refused with the
// path to delete instead of migrated.
const SchemaVersion = 2

// Registry is the persisted state: a registry of kits, targets and their
// attachments, the last active target, and the pending actions of each
// target. All paths are absolute; a target path is the target's identity.
type Registry struct {
	Version int `json:"version"`
	// Kits maps a kit name to the kit.
	Kits map[string]Kit `json:"kits"`
	// Targets maps a target path to the target.
	Targets map[string]Target `json:"targets"`
	// Attachments ties kits to targets in attachment order: a kit attaches
	// to several targets, and a target exists without kits.
	Attachments []Attachment `json:"attachments"`
	// LastTarget is the target that was active when penates last exited.
	LastTarget string `json:"last_target"`
	// Pending holds the unapplied actions of each target, keyed by the
	// target path.
	Pending map[string]*Pending `json:"pending"`
	// Report lists the entries load dropped because they no longer hold.
	// Load fills it for the client to show; it is never persisted.
	Report []string `json:"-"`
}

// Kit is a registered kit. Its name is the registry key and the identity the
// item keys use; Dir is the directory the kit loads from.
type Kit struct {
	Dir string `json:"dir"`
}

// Target is a registered target directory. Name is the optional display name
// the interface shows instead of the path.
type Target struct {
	Name string `json:"name,omitempty"`
}

// Attachment ties a kit to a target the kit is installed into.
type Attachment struct {
	Kit    string `json:"kit"`
	Target string `json:"target"`
}

// Pending is the per-target object of unapplied actions: the item actions
// selected since the last apply, the deferred operations waiting for it, and
// the answers given at selection time. It is saved on every mutation and
// kept until the apply consumes it.
type Pending struct {
	// Items holds the pending actions per item key ("kit name/item id").
	Items map[string]*ItemPending `json:"items"`
	// Deferred lists the operations that wait for the apply.
	Deferred []Deferred `json:"deferred,omitempty"`
}

// ItemPending holds the actions pending for one item and the answers given
// when it was selected. The answers stay until the apply writes them into
// the lock, so a restored selection does not ask again.
type ItemPending struct {
	// Select marks the item chosen for installation: an install, an update
	// or a downgrade, whatever the plan decides.
	Select bool `json:"select,omitempty"`
	// Reset marks an installed item for reset to the kit content.
	Reset bool `json:"reset,omitempty"`
	// Remove marks an installed item for removal.
	Remove bool `json:"remove,omitempty"`
	// Answers maps a variable id to the answer given for this selection.
	Answers map[string]string `json:"answers,omitempty"`
}

// DeferredKind names a deferred operation.
type DeferredKind string

const (
	// DeferredDetachKit detaches a kit from the target once the apply has
	// removed the kit's installed items there.
	DeferredDetachKit DeferredKind = "detach_kit"
	// DeferredForgetTarget forgets the target once the apply has removed
	// all of its installed items.
	DeferredForgetTarget DeferredKind = "forget_target"
)

// Deferred is one operation waiting for the apply that removes what it
// covers. The apply completes it, or drops it with a reason shown to the
// user.
type Deferred struct {
	Kind DeferredKind `json:"kind"`
	// Kit names the kit of a DeferredDetachKit.
	Kit string `json:"kit,omitempty"`
}

// Store reads and writes the state file of one penates data directory.
type Store struct {
	// Dir is the absolute path of the penates data directory.
	Dir string
}

// DefaultDir returns the penates data directory: $XDG_CONFIG_HOME/penates when
// XDG_CONFIG_HOME is set and non-empty, otherwise ~/.config/penates.
func DefaultDir() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "penates"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "penates"), nil
}

// Open returns the store of dir, creating the directory if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

// newRegistry returns an empty registry of the current schema version.
func newRegistry() *Registry {
	return &Registry{
		Version: SchemaVersion,
		Kits:    map[string]Kit{},
		Targets: map[string]Target{},
		Pending: map[string]*Pending{},
	}
}

// LoadRegistry reads the state file into the registry. A missing file is an
// empty registry, not an error. Entries that no longer hold are dropped,
// reported in the registry's Report, and the file is rewritten without them.
func (s *Store) LoadRegistry() (*Registry, error) {
	path := filepath.Join(s.Dir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return newRegistry(), nil
	}
	if err != nil {
		return nil, err
	}
	// The version probe runs before the decode: the old format has no
	// version field, and its shapes do not decode into the registry — the
	// user must see which file to delete, not a decode error.
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("state %s: %w", path, err)
	}
	if probe.Version == 0 {
		return nil, fmt.Errorf("state %s: old format, no longer supported; delete the file and register kits again", path)
	}
	if probe.Version != SchemaVersion {
		return nil, fmt.Errorf("state %s: format version %d, want %d; delete the file and register kits again", path, probe.Version, SchemaVersion)
	}
	reg := newRegistry()
	if err := json.Unmarshal(data, reg); err != nil {
		return nil, fmt.Errorf("state %s: %w", path, err)
	}
	reg.normalize()
	if report := reg.prune(); len(report) > 0 {
		reg.Report = report
		if err := s.save(reg); err != nil {
			return nil, fmt.Errorf("state %s: %w", path, err)
		}
	}
	return reg, nil
}

// Update applies fn to the registry and saves — the store operation the
// schema mutations build on, so that every mutation reaches the file. fn
// validates and mutates; its error abandons the change without saving.
func (s *Store) Update(fn func(*Registry) error) error {
	reg, err := s.LoadRegistry()
	if err != nil {
		return err
	}
	if err := fn(reg); err != nil {
		return err
	}
	return s.save(reg)
}

// save writes the registry through a temp file and a rename, so that an
// interruption cannot leave a half-written file behind.
func (s *Store) save(reg *Registry) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.Dir, FileName)
	tmp, err := os.CreateTemp(s.Dir, ".state-*")
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

// normalize fills the collections a file may have left out, so that callers
// never see nil maps.
func (reg *Registry) normalize() {
	if reg.Kits == nil {
		reg.Kits = map[string]Kit{}
	}
	if reg.Targets == nil {
		reg.Targets = map[string]Target{}
	}
	if reg.Pending == nil {
		reg.Pending = map[string]*Pending{}
	}
	for _, p := range reg.Pending {
		if p.Items == nil {
			p.Items = map[string]*ItemPending{}
		}
	}
}

// prune drops the entries that no longer hold and returns the report of what
// went. Only facts the registry itself knows prune an entry: an unregistered
// target, a kit that is not registered, answers without their selection.
// Whether a restored action still matches the kit and lock content is the
// client's check at restore time.
func (reg *Registry) prune() []string {
	var report []string
	for path, p := range reg.Pending {
		if _, ok := reg.Targets[path]; !ok {
			delete(reg.Pending, path)
			report = append(report, fmt.Sprintf("dropped pending actions for %s: the target is not registered", path))
			continue
		}
		for key, item := range p.Items {
			kitName, _, _ := strings.Cut(key, "/")
			if _, ok := reg.Kits[kitName]; !ok {
				// An installed item whose kit is not registered stays
				// manageable from the lock: a removal is planned from the
				// lock alone, and a reset is the user's explicit request
				// (9.2, concept 7.6). The selection goes — nothing can be
				// planned for an item whose kit is gone.
				if item.Select {
					item.Select = false
					item.Answers = nil
					report = append(report, fmt.Sprintf("dropped the selection of %s for %s: the kit %s is not registered", key, path, kitName))
				}
				if !item.Reset && !item.Remove {
					delete(p.Items, key)
				}
				continue
			}
			if !item.Select && len(item.Answers) > 0 {
				item.Answers = nil
				report = append(report, fmt.Sprintf("dropped the variable answers of %s for %s: the item is not selected", key, path))
			}
			if !item.Select && !item.Reset && !item.Remove {
				delete(p.Items, key)
			}
		}
		if len(p.Items) == 0 && len(p.Deferred) == 0 {
			delete(reg.Pending, path)
		}
	}
	if reg.LastTarget != "" {
		if _, ok := reg.Targets[reg.LastTarget]; !ok {
			report = append(report, fmt.Sprintf("dropped the last active target %s: the target is not registered", reg.LastTarget))
			reg.LastTarget = ""
		}
	}
	sort.Strings(report)
	return report
}

// TargetPaths lists the registered target paths in ascending order.
func (reg *Registry) TargetPaths() []string {
	paths := make([]string, 0, len(reg.Targets))
	for path := range reg.Targets {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// KitsFor lists the names of the kits attached to target, in ascending order.
func (reg *Registry) KitsFor(target string) []string {
	var names []string
	for _, a := range reg.Attachments {
		if a.Target == target {
			names = append(names, a.Kit)
		}
	}
	sort.Strings(names)
	return names
}

// pendingFor returns the pending object of target, an empty one when the
// target holds none. The caller writes the object back into reg.Pending.
func (reg *Registry) pendingFor(target string) *Pending {
	p := reg.Pending[target]
	if p == nil {
		p = &Pending{}
	}
	if p.Items == nil {
		p.Items = map[string]*ItemPending{}
	}
	return p
}

// State is the view the current TUI reads and mutates until the TUI rewrite
// replaces it: the registry seen as the flat state of the old format. Pending
// installs read as the selection, and registration attaches. Its mutations
// keep their old shape — they change the registry in memory, and the client's
// explicit Save persists it; the schema mutations go through Store operations,
// which save themselves.
type State struct {
	store *Store
	reg   *Registry

	// Kits maps a kit name to the absolute path of the kit directory.
	Kits map[string]string
	// Targets lists the registered target paths in ascending order.
	Targets []string
}

// Load reads the state file and returns the state the current TUI works
// through. A missing file is an empty state, not an error; an old-format file
// is refused with the path to delete.
func (s *Store) Load() (*State, error) {
	reg, err := s.LoadRegistry()
	if err != nil {
		return nil, err
	}
	return s.state(reg), nil
}

// Save writes the state's registry through a temp file and a rename.
func (s *Store) Save(st *State) error {
	return s.save(st.reg)
}

// state builds the compatibility view of a registry.
func (s *Store) state(reg *Registry) *State {
	st := &State{store: s, reg: reg, Kits: map[string]string{}}
	for name, k := range reg.Kits {
		st.Kits[name] = k.Dir
	}
	st.Targets = reg.TargetPaths()
	return st
}

// RegisterKit records a kit and attaches it to target, registering the target
// when it is new. A kit already attached elsewhere keeps the earlier
// attachment and moves this one last: a kit may attach to several targets,
// and KitTarget answers the most recent attachment.
func (st *State) RegisterKit(name, kitDir, target string) {
	k := st.reg.Kits[name]
	k.Dir = kitDir
	st.reg.Kits[name] = k
	if _, ok := st.reg.Targets[target]; !ok {
		st.reg.Targets[target] = Target{}
	}
	kept := st.reg.Attachments[:0]
	for _, a := range st.reg.Attachments {
		if a.Kit == name && a.Target == target {
			continue
		}
		kept = append(kept, a)
	}
	kept = append(kept, Attachment{Kit: name, Target: target})
	st.reg.Attachments = kept
	st.Kits[name] = kitDir
	st.Targets = st.reg.TargetPaths()
}

// KitTarget returns the target of the kit's most recent attachment: the old
// registration moved a kit to the target it was registered with, so the
// newest attachment is the answer.
func (st *State) KitTarget(name string) (string, bool) {
	for i := len(st.reg.Attachments) - 1; i >= 0; i-- {
		if a := st.reg.Attachments[i]; a.Kit == name {
			return a.Target, true
		}
	}
	return "", false
}

// KitsForTarget lists the names of the kits attached to target, in ascending
// order.
func (st *State) KitsForTarget(target string) []string {
	return st.reg.KitsFor(target)
}

// SelectionFor lists the item keys selected for target, in ascending order;
// nil when none is.
func (st *State) SelectionFor(target string) []string {
	p := st.reg.Pending[target]
	if p == nil {
		return nil
	}
	var keys []string
	for key, item := range p.Items {
		if item.Select {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// SetSelection records the selection for target: the items of keys are marked
// selected, the items the list leaves out lose the mark and the answers given
// for it. Reset and remove marks and deferred operations are untouched — the
// selection is one part of the pending actions, not all of them.
func (st *State) SetSelection(target string, keys []string) {
	p := st.reg.pendingFor(target)
	selected := make(map[string]bool, len(keys))
	for _, key := range keys {
		selected[key] = true
	}
	for key, item := range p.Items {
		if !item.Select || selected[key] {
			continue
		}
		item.Select = false
		item.Answers = nil
		if !item.Reset && !item.Remove {
			delete(p.Items, key)
		}
	}
	for _, key := range keys {
		if p.Items[key] == nil {
			p.Items[key] = &ItemPending{}
		}
		p.Items[key].Select = true
	}
	// A target that held no pending actions gains none from an empty
	// selection.
	if _, ok := st.reg.Pending[target]; !ok && len(p.Items) == 0 && len(p.Deferred) == 0 {
		return
	}
	st.reg.Pending[target] = p
}
