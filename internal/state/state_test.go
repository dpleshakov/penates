package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	reg, err := store.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry on a fresh directory: %v", err)
	}
	if reg.Version != SchemaVersion || len(reg.Kits) != 0 || len(reg.Targets) != 0 || len(reg.Attachments) != 0 || len(reg.Pending) != 0 || reg.LastTarget != "" {
		t.Fatalf("fresh registry is not empty: %+v", reg)
	}

	want := &Registry{
		Version: SchemaVersion,
		Kits: map[string]Kit{
			"example": {Dir: filepath.Join(dir, "kit")},
			"team":    {Dir: filepath.Join(dir, "team-kit")},
		},
		Targets: map[string]Target{
			"/targets/main": {Name: "main"},
			"/targets/wsl":  {},
		},
		Attachments: []Attachment{
			{Kit: "example", Target: "/targets/main"},
			{Kit: "example", Target: "/targets/wsl"},
			{Kit: "team", Target: "/targets/main"},
		},
		LastTarget: "/targets/wsl",
		Pending: map[string]*Pending{
			"/targets/main": {
				Items: map[string]*ItemPending{
					"example/writing": {Select: true, Answers: map[string]string{"src": "/src/opencode"}},
					"example/old":     {Remove: true},
					"team/review":     {Select: true, Reset: true},
				},
				Deferred: []Deferred{
					{Kind: DeferredDetachKit, Kit: "example"},
					{Kind: DeferredForgetTarget},
				},
			},
			"/targets/wsl": {
				Items: map[string]*ItemPending{
					"example/writing": {Reset: true},
				},
			},
		},
	}
	if err := store.save(want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry after save: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("registry mismatch after round trip (-want +got):\n%s", diff)
	}
}

func TestUpdateSaves(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	target := filepath.Join(dir, "target")
	err = store.Update(func(reg *Registry) error {
		reg.Kits["example"] = Kit{Dir: filepath.Join(dir, "kit")}
		reg.Targets[target] = Target{Name: "main"}
		reg.Attachments = append(reg.Attachments, Attachment{Kit: "example", Target: target})
		reg.LastTarget = target
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A fresh store reads what the mutation wrote: the Update saved it
	// without an explicit save.
	fresh, err := Open(dir)
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	reg, err := fresh.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if reg.Kits["example"].Dir != filepath.Join(dir, "kit") {
		t.Errorf("kit dir = %q, want the registered one", reg.Kits["example"].Dir)
	}
	if reg.Targets[target].Name != "main" {
		t.Errorf("target name = %q, want %q", reg.Targets[target].Name, "main")
	}
	if reg.LastTarget != target {
		t.Errorf("last target = %q, want %q", reg.LastTarget, target)
	}
	if diff := cmp.Diff([]string{"example"}, reg.KitsFor(target)); diff != "" {
		t.Errorf("kits attached to the target (-want +got):\n%s", diff)
	}

	// A mutation that fails validation leaves the file as it was.
	stop := errors.New("stop")
	err = store.Update(func(reg *Registry) error {
		reg.Targets["/targets/other"] = Target{}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("Update with a failing fn = %v, want the fn's error", err)
	}
	reg, err = fresh.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if _, ok := reg.Targets["/targets/other"]; ok {
		t.Error("a failed mutation was saved anyway")
	}
}

func TestOldFormatRefused(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	path := filepath.Join(dir, FileName)

	data, err := json.Marshal(map[string]any{
		"kits":        map[string]string{"example": "/kits/example"},
		"targets":     []string{"/targets/main"},
		"kit_targets": map[string]string{"example": "/targets/main"},
		"selections":  map[string][]string{"/targets/main": {"example/writing"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, load := range []func() error{
		func() error { _, err := store.LoadRegistry(); return err },
		func() error { _, err := store.Load(); return err },
	} {
		err := load()
		if err == nil {
			t.Fatal("the old format loaded without an error")
		}
		if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "delete") {
			t.Errorf("old-format error %q, want one naming %s and asking to delete it", err, path)
		}
	}

	// A version the build does not know is refused the same way.
	if err := os.WriteFile(path, []byte(`{"version": 99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = store.LoadRegistry()
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("future-version error %q, want one naming %s", err, path)
	}
}

func TestPruneDropsStaleEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	reg := newRegistry()
	reg.Kits["example"] = Kit{Dir: "/kits/example"}
	reg.Targets["/targets/main"] = Target{}
	reg.Pending["/targets/gone"] = &Pending{
		Items: map[string]*ItemPending{
			"example/writing": {Select: true},
		},
	}
	reg.Pending["/targets/main"] = &Pending{
		Items: map[string]*ItemPending{
			"ghost/writing":   {Select: true},
			"example/writing": {Remove: true, Answers: map[string]string{"p": "/src"}},
		},
	}
	reg.LastTarget = "/targets/gone"
	if err := store.save(reg); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if _, ok := got.Pending["/targets/gone"]; ok {
		t.Error("pending actions of an unregistered target survived the load")
	}
	if got.Pending["/targets/main"].Items["ghost/writing"] != nil {
		t.Error("a pending action of an unregistered kit survived the load")
	}
	item := got.Pending["/targets/main"].Items["example/writing"]
	if item == nil || !item.Remove || item.Answers != nil {
		t.Errorf("kept item = %+v, want the remove mark kept and the answers dropped", item)
	}
	if got.LastTarget != "" {
		t.Errorf("last target = %q, want it dropped with the unregistered target", got.LastTarget)
	}
	if len(got.Report) != 4 {
		t.Errorf("report = %v, want one line per dropped entry", got.Report)
	}

	// The rewrite already happened: the next load reports nothing.
	reg2, err := store.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry again: %v", err)
	}
	if len(reg2.Report) != 0 {
		t.Errorf("second load reported %v, want a clean state", reg2.Report)
	}
	if diff := cmp.Diff(reg.TargetPaths(), reg2.TargetPaths()); diff != "" {
		t.Errorf("registered targets changed (-want +got):\n%s", diff)
	}
}

func TestCompatRegistration(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	st.RegisterKit("example", "/kits/example", "/targets/main")
	if got, ok := st.KitTarget("example"); !ok || got != "/targets/main" {
		t.Errorf("KitTarget(example) = %q, %v; want /targets/main", got, ok)
	}
	if _, ok := st.KitTarget("ghost"); ok {
		t.Error("KitTarget(ghost) reported a kit that was never registered")
	}

	// Registering another target keeps the first attachment — a kit may
	// attach to several targets — and becomes the most recent one.
	st.RegisterKit("example", "/kits/example-moved", "/targets/wsl")
	if got, ok := st.KitTarget("example"); !ok || got != "/targets/wsl" {
		t.Errorf("KitTarget(example) = %q, %v; want the most recent attachment /targets/wsl", got, ok)
	}
	if got := st.Kits["example"]; got != "/kits/example-moved" {
		t.Errorf("Kits[example] = %q, want the last registered %q", got, "/kits/example-moved")
	}
	if diff := cmp.Diff([]string{"example"}, st.KitsForTarget("/targets/main")); diff != "" {
		t.Errorf("kits of the first target (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"/targets/main", "/targets/wsl"}, st.Targets); diff != "" {
		t.Errorf("targets (-want +got):\n%s", diff)
	}

	// The attachments survive a reload, and the kit names answer again.
	if err := store.Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st2, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, ok := st2.KitTarget("example"); !ok || got != "/targets/wsl" {
		t.Errorf("KitTarget(example) after reload = %q, %v; want /targets/wsl", got, ok)
	}
	if diff := cmp.Diff([]string{"example"}, st2.KitsForTarget("/targets/wsl")); diff != "" {
		t.Errorf("kits of the second target after reload (-want +got):\n%s", diff)
	}
}

func TestCompatSelection(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st.RegisterKit("example", "/kits/example", "/targets/main")

	if got := st.SelectionFor("/targets/main"); got != nil {
		t.Errorf("SelectionFor with nothing recorded = %v, want nil", got)
	}
	st.SetSelection("/targets/main", []string{"example/writing", "example/old"})
	if diff := cmp.Diff([]string{"example/old", "example/writing"}, st.SelectionFor("/targets/main")); diff != "" {
		t.Errorf("SelectionFor (-want +got):\n%s", diff)
	}

	// Answers given at selection time ride with the selection: they stay
	// for the items still selected and go with the items leaving it.
	p := st.reg.pendingFor("/targets/main")
	p.Items["example/writing"].Answers = map[string]string{"src": "/src"}
	p.Items["example/old"].Answers = map[string]string{"p": "/old"}
	p.Deferred = []Deferred{{Kind: DeferredForgetTarget}}
	st.SetSelection("/targets/main", []string{"example/writing"})

	item := p.Items["example/writing"]
	if item == nil || !item.Select || item.Answers["src"] != "/src" {
		t.Errorf("selected item = %+v, want the selection and its answers kept", item)
	}
	if item := p.Items["example/old"]; item != nil {
		t.Errorf("deselected item = %+v, want the entry dropped with its answers", item)
	}
	if len(p.Deferred) != 1 {
		t.Errorf("deferred operations = %v, want them untouched by the selection", p.Deferred)
	}

	// The pending object reaches the file through the explicit save the
	// current TUI performs.
	if err := store.Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st2, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if diff := cmp.Diff([]string{"example/writing"}, st2.SelectionFor("/targets/main")); diff != "" {
		t.Errorf("SelectionFor after reload (-want +got):\n%s", diff)
	}

	// An empty selection clears the marks but not the deferred operations,
	// and a target without pending actions gains none.
	st2.SetSelection("/targets/main", nil)
	if len(st2.reg.Pending["/targets/main"].Deferred) != 1 {
		t.Error("an empty selection dropped the deferred operations")
	}
	st3, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st3.SetSelection("/targets/other", nil)
	if _, ok := st3.reg.Pending["/targets/other"]; ok {
		t.Error("an empty selection created pending actions for a target that had none")
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
