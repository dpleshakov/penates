package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/apply"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// The embedded kit itself, installed the way a client installs any kit:
// sync unpacks it, the kit loads from the managed directory, and both
// items land in the scratch target at the paths their contributions
// declare.
func TestEmbeddedKitInstalls(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "harness")

	stateStore, err := state.Open(dataDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sync(t, dataDir, stateStore, Content)

	k, err := kit.Load(KitDir(dataDir))
	if err != nil {
		t.Fatalf("loading the unpacked kit: %v", err)
	}
	if k.Name != state.BuiltinKitName {
		t.Fatalf("kit name = %q, want %q", k.Name, state.BuiltinKitName)
	}

	locks := lock.NewStore(dataDir)
	lk, err := locks.Load(targetDir)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	snap, err := target.Read(targetDir, []string{
		"skills/penates/SKILL.md",
		"skills/penates-kit-authoring/SKILL.md",
	}, nil)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	selection := []string{
		state.BuiltinKitName + "/reference",
		state.BuiltinKitName + "/kit-authoring",
	}
	sources, err := kit.ReadSources([]*kit.Kit{k}, selection)
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	p := plan.Build(plan.Input{
		Kits:      []*kit.Kit{k},
		Selection: selection,
		Lock:      lk,
		Snapshot:  snap,
		Sources:   sources,
	})
	if len(p.Installable()) != 2 {
		t.Fatalf("installable = %v, want both items", p.Installable())
	}
	results := apply.Apply(targetDir, p, lk, locks, nil)
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("apply %s: %v", r.Key, r.Err)
		}
	}

	for _, tc := range []struct{ path, want string }{
		{filepath.Join("skills", "penates", "SKILL.md"), "# penates reference"},
		{filepath.Join("skills", "penates-kit-authoring", "SKILL.md"), "# penates kit authoring"},
	} {
		data, err := os.ReadFile(filepath.Join(targetDir, tc.path))
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if !strings.Contains(string(data), tc.want) {
			t.Errorf("%s does not carry the real text (%q missing)", tc.path, tc.want)
		}
	}
}
