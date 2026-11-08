package builtin

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dpleshakov/penates/internal/state"
)

// kitA and kitB are two revisions of a kit: the same files, one content
// difference, so a hash over the tree tells them apart.
func kitFS(body string) fs.FS {
	return fstest.MapFS{
		"kit.yaml":                     &fstest.MapFile{Data: []byte("name: penates\n")},
		"items/reference/item.yaml":    &fstest.MapFile{Data: []byte("id: reference\n")},
		"items/reference/SKILL.md":     &fstest.MapFile{Data: []byte(body)},
		"items/kit-authoring/SKILL.md": &fstest.MapFile{Data: []byte("authoring\n")},
	}
}

var (
	kitA = kitFS("reference A\n")
	kitB = kitFS("reference B\n")
)

func sync(t *testing.T, dataDir string, store *state.Store, src fs.FS) Report {
	t.Helper()
	rep, err := Sync(dataDir, store, src)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return rep
}

// readFile returns a file of the managed kit directory.
func readKitFile(t *testing.T, dataDir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(KitDir(dataDir), name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

func hasNotice(rep Report, part string) bool {
	for _, n := range rep.Notices {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

func TestSyncFreshUnpacksStampsRegisters(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()

	rep := sync(t, dataDir, store, kitA)

	if !rep.Registered {
		t.Error("the fresh run did not register the kit")
	}
	if got := readKitFile(t, dataDir, "items/reference/SKILL.md"); got != "reference A\n" {
		t.Errorf("unpacked SKILL.md = %q, want the embedded content", got)
	}
	hash, err := hashTree(kitA)
	if err != nil {
		t.Fatalf("hashTree: %v", err)
	}
	if got := stampHash(filepath.Join(dataDir, stampName)); got != hash {
		t.Errorf("stamp = %q, want the embedded hash %q", got, hash)
	}
	reg, err := store.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if k := reg.Kits[state.BuiltinKitName]; k.Dir != KitDir(dataDir) {
		t.Errorf("registered dir = %q, want %q", k.Dir, KitDir(dataDir))
	}
}

func TestSyncSecondRunChangesNothing(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	sync(t, dataDir, store, kitA)

	rep := sync(t, dataDir, store, kitA)

	if rep.Registered {
		t.Error("the second run registered the kit again")
	}
	if len(rep.Notices) != 0 || rep.Modified || rep.Conflict {
		t.Errorf("second run = %+v, want a quiet no-op", rep)
	}
}

func TestSyncChangedContentReplaces(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	sync(t, dataDir, store, kitA)

	rep := sync(t, dataDir, store, kitB)

	if got := readKitFile(t, dataDir, "items/reference/SKILL.md"); got != "reference B\n" {
		t.Errorf("SKILL.md = %q, want the replaced content", got)
	}
	hash, _ := hashTree(kitB)
	if got := stampHash(filepath.Join(dataDir, stampName)); got != hash {
		t.Errorf("stamp = %q, want the new embedded hash", got)
	}
	if !hasNotice(rep, "updated") {
		t.Errorf("notices = %v, want the update notice", rep.Notices)
	}
	if rep.Conflict {
		t.Error("an untouched directory was reported as a conflict")
	}
}

func TestSyncUserEditWithChangedContentConflicts(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	sync(t, dataDir, store, kitA)

	edited := filepath.Join(KitDir(dataDir), "items/reference/SKILL.md")
	if err := os.WriteFile(edited, []byte("my local edit\n"), 0o600); err != nil {
		t.Fatalf("editing the kit directory: %v", err)
	}
	oldStamp := stampHash(filepath.Join(dataDir, stampName))

	rep := sync(t, dataDir, store, kitB)

	if !rep.Conflict || !rep.Modified {
		t.Errorf("report = %+v, want a conflict over a modification", rep)
	}
	if got := readKitFile(t, dataDir, "items/reference/SKILL.md"); got != "my local edit\n" {
		t.Errorf("SKILL.md = %q, want the local edit kept", got)
	}
	if got := stampHash(filepath.Join(dataDir, stampName)); got != oldStamp {
		t.Error("the stamp changed under a conflict")
	}
}

func TestSyncUserEditWithSameContentKeptQuietly(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	sync(t, dataDir, store, kitA)

	if err := os.WriteFile(
		filepath.Join(KitDir(dataDir), "items/reference/SKILL.md"),
		[]byte("my local edit\n"), 0o600); err != nil {
		t.Fatalf("editing the kit directory: %v", err)
	}

	rep := sync(t, dataDir, store, kitA)

	if !rep.Modified || rep.Conflict {
		t.Errorf("report = %+v, want modified without a conflict", rep)
	}
	if got := readKitFile(t, dataDir, "items/reference/SKILL.md"); got != "my local edit\n" {
		t.Errorf("SKILL.md = %q, want the local edit kept", got)
	}
}

func TestSyncMissingDirectoryUnderStampUnpacksAgain(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	sync(t, dataDir, store, kitA)

	if err := os.RemoveAll(KitDir(dataDir)); err != nil {
		t.Fatalf("removing the kit directory: %v", err)
	}

	sync(t, dataDir, store, kitB)

	if got := readKitFile(t, dataDir, "items/reference/SKILL.md"); got != "reference B\n" {
		t.Errorf("SKILL.md = %q, want the directory unpacked again", got)
	}
	hash, _ := hashTree(kitB)
	if got := stampHash(filepath.Join(dataDir, stampName)); got != hash {
		t.Errorf("stamp = %q, want the new embedded hash", got)
	}
}

func TestSyncDirectoryWithoutStampStandsDown(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	sync(t, dataDir, store, kitA)

	if err := os.Remove(filepath.Join(dataDir, stampName)); err != nil {
		t.Fatalf("removing the stamp: %v", err)
	}

	rep := sync(t, dataDir, store, kitB)

	if !hasNotice(rep, "not owned") {
		t.Errorf("notices = %v, want the not-owned notice", rep.Notices)
	}
	if got := readKitFile(t, dataDir, "items/reference/SKILL.md"); got != "reference A\n" {
		t.Errorf("SKILL.md = %q, want the directory untouched", got)
	}
	if rep.Registered {
		t.Error("the run registered over an unowned directory")
	}
}

func TestSyncForeignNameSuppressesEverything(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	if err := store.RegisterKit("mine", "/kits/mine"); err != nil {
		t.Fatalf("RegisterKit: %v", err)
	}
	if err := store.Update(func(reg *state.Registry) error {
		reg.Kits[state.BuiltinKitName] = state.Kit{Dir: "/kits/foreign"}
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	rep := sync(t, dataDir, store, kitA)

	if !hasNotice(rep, "is taken by the kit at /kits/foreign") {
		t.Errorf("notices = %v, want the taken-name notice", rep.Notices)
	}
	if _, err := os.Stat(KitDir(dataDir)); !os.IsNotExist(err) {
		t.Error("the managed directory was written under a foreign name")
	}
	if rep.Registered {
		t.Error("the run registered under a foreign name")
	}
}

func TestSyncRepointedEntrySuppressesWrites(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	sync(t, dataDir, store, kitA)

	// The fork escape hatch: the entry is repointed at a copy, so the
	// managed directory is no longer the registered one.
	if err := store.Update(func(reg *state.Registry) error {
		reg.Kits[state.BuiltinKitName] = state.Kit{Dir: "/kits/fork"}
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	rep := sync(t, dataDir, store, kitB)

	if got := readKitFile(t, dataDir, "items/reference/SKILL.md"); got != "reference A\n" {
		t.Errorf("SKILL.md = %q, want no replacement over a repointed entry", got)
	}
	if !hasNotice(rep, "is taken by the kit at /kits/fork") {
		t.Errorf("notices = %v, want the taken-name notice", rep.Notices)
	}
}

func TestSyncCrashWindowRestampsOnly(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()
	sync(t, dataDir, store, kitA)

	// A crash between the rename and the stamp: the directory holds the new
	// content while the stamp still names the old one. The directory must
	// not be mistaken for a local modification.
	hashA := stampHash(filepath.Join(dataDir, stampName))
	if err := os.RemoveAll(KitDir(dataDir)); err != nil {
		t.Fatalf("removing the kit directory: %v", err)
	}
	if err := unpack(dataDir, KitDir(dataDir), kitB); err != nil {
		t.Fatalf("unpack: %v", err)
	}

	rep := sync(t, dataDir, store, kitB)

	if rep.Conflict {
		t.Errorf("report = %+v, want the crash window restamped, not a conflict", rep)
	}
	hashB, _ := hashTree(kitB)
	if got := stampHash(filepath.Join(dataDir, stampName)); got != hashB || got == hashA {
		t.Errorf("stamp = %q, want the embedded hash after the restamp", got)
	}
}

func TestSyncSweepsLeftovers(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dataDir := t.TempDir()

	// The artifacts an interrupted run leaves: an unpack temp directory,
	// its .old copy, and a stamp temp file. They are swept; the managed
	// kit directory, which the same Sync unpacks, is not.
	if err := os.MkdirAll(filepath.Join(dataDir, ".builtin-kit-1"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, ".builtin-kit-2.old"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, ".builtin-stamp-3"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sync(t, dataDir, store, kitA)

	for _, leftover := range []string{".builtin-kit-1", ".builtin-kit-2.old", ".builtin-stamp-3"} {
		if _, err := os.Stat(filepath.Join(dataDir, leftover)); !os.IsNotExist(err) {
			t.Errorf("%s survived the sweep", leftover)
		}
	}
	if _, err := os.Stat(KitDir(dataDir)); err != nil {
		t.Errorf("the managed kit directory: %v", err)
	}
}

func TestRegisterKitRefusesReservedName(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.RegisterKit(state.BuiltinKitName, "/kits/mine"); err == nil {
		t.Fatal("RegisterKit under the reserved name succeeded")
	}
}
