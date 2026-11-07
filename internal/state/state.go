package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// FileName is the name of the state file inside the data directory.
const FileName = "state.json"

// Store reads and writes the state file of one penates data directory.
type Store struct {
	// Dir is the absolute path of the penates data directory.
	Dir string
}

// State is the persisted application state. All paths are absolute; target paths
// identify locks and selections.
type State struct {
	// Kits maps a kit name to the absolute path of the kit directory.
	Kits map[string]string `json:"kits"`
	// Targets lists the registered target directories.
	Targets []string `json:"targets"`
	// KitTargets maps a kit name to the target the kit is installed into.
	KitTargets map[string]string `json:"kit_targets"`
	// Selections maps a target path to the item keys selected there, most recent
	// last. An item key is "kit name/item id".
	Selections map[string][]string `json:"selections"`
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

// Load reads the state file. A missing file is an empty state, not an error.
func (s *Store) Load() (*State, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return &State{
			Kits:       map[string]string{},
			KitTargets: map[string]string{},
			Selections: map[string][]string{},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	st := &State{}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("state %s: %w", filepath.Join(s.Dir, FileName), err)
	}
	if st.Kits == nil {
		st.Kits = map[string]string{}
	}
	if st.KitTargets == nil {
		st.KitTargets = map[string]string{}
	}
	if st.Selections == nil {
		st.Selections = map[string][]string{}
	}
	return st, nil
}

// Save writes the state through a temp file and a rename, so that an interruption
// cannot leave a half-written file behind.
func (s *Store) Save(st *State) error {
	data, err := json.MarshalIndent(st, "", "  ")
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

// RegisterKit records a kit and the target it installs into, registering the target
// as well.
func (st *State) RegisterKit(name, kitDir, target string) {
	if !containsTarget(st.Targets, target) {
		st.Targets = append(st.Targets, target)
	}
	st.Kits[name] = kitDir
	st.KitTargets[name] = target
}

// KitTarget returns the target a kit is registered to install into.
func (st *State) KitTarget(name string) (string, bool) {
	target, ok := st.KitTargets[name]
	return target, ok
}

// KitsForTarget returns the names of the kits registered to install into target, in
// ascending order.
func (st *State) KitsForTarget(target string) []string {
	var names []string
	for name, t := range st.KitTargets {
		if t == target {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// SelectionFor returns the last selection recorded for target; nil when none was
// recorded.
func (st *State) SelectionFor(target string) []string {
	return st.Selections[target]
}

// SetSelection records the selection for target, replacing any earlier one.
func (st *State) SetSelection(target string, keys []string) {
	st.Selections[target] = keys
}

func containsTarget(targets []string, target string) bool {
	for _, t := range targets {
		if t == target {
			return true
		}
	}
	return false
}
