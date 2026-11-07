package state

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load on a fresh directory: %v", err)
	}
	if len(loaded.Kits) != 0 || len(loaded.Targets) != 0 || len(loaded.KitTargets) != 0 || len(loaded.Selections) != 0 {
		t.Fatalf("fresh state is not empty: %+v", loaded)
	}

	target := filepath.Join(dir, "target")
	want := &State{
		Kits:       map[string]string{"example": filepath.Join(dir, "kit")},
		Targets:    []string{target},
		KitTargets: map[string]string{"example": target},
		Selections: map[string][]string{target: {"example/writing", "example/gh-note"}},
	}
	want.RegisterKit("example", filepath.Join(dir, "kit"), target)
	want.SetSelection(target, []string{"example/writing", "example/gh-note"})
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("state mismatch after round trip (-want +got):\n%s", diff)
	}
}

func TestQueries(t *testing.T) {
	target1 := filepath.Join(t.TempDir(), "one")
	target2 := filepath.Join(t.TempDir(), "two")
	st := &State{
		Kits:       map[string]string{},
		KitTargets: map[string]string{},
		Selections: map[string][]string{},
	}
	st.RegisterKit("alpha", "/kits/alpha", target1)
	st.RegisterKit("beta", "/kits/beta", target1)
	st.RegisterKit("gamma", "/kits/gamma", target2)

	if got, ok := st.KitTarget("beta"); !ok || got != target1 {
		t.Errorf("KitTarget(beta) = %q, %v", got, ok)
	}
	if _, ok := st.KitTarget("delta"); ok {
		t.Error("KitTarget(delta) reported a kit that was never registered")
	}
	if diff := cmp.Diff([]string{"alpha", "beta"}, st.KitsForTarget(target1)); diff != "" {
		t.Errorf("KitsForTarget (-want +got):\n%s", diff)
	}
	if got := st.SelectionFor(target2); got != nil {
		t.Errorf("SelectionFor with no recorded selection = %v, want nil", got)
	}
	st.SetSelection(target2, []string{"gamma/x"})
	if diff := cmp.Diff([]string{"gamma/x"}, st.SelectionFor(target2)); diff != "" {
		t.Errorf("SelectionFor (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{target1, target2}, st.Targets); diff != "" {
		t.Errorf("Targets (-want +got):\n%s", diff)
	}
}

func TestDefaultDirXDGConfigHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if want := filepath.Join(base, "penates"); dir != want {
		t.Errorf("DefaultDir = %q, want %q", dir, want)
	}
}

func TestDefaultDirUnderHome(t *testing.T) {
	home := t.TempDir()
	// An empty XDG_CONFIG_HOME counts as unset, so the default branch applies.
	t.Setenv("XDG_CONFIG_HOME", "")
	// os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows; set both so
	// the test is deterministic on every OS.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if want := filepath.Join(home, ".config", "penates"); dir != want {
		t.Errorf("DefaultDir = %q, want %q", dir, want)
	}
}
