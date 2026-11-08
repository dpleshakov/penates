package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// fakeLocks is a LockDeleter over real files: it records every Delete and
// removes the file recorded for the target, one per target.
type fakeLocks struct {
	files   map[string]string
	deleted []string
	err     error
}

func (f *fakeLocks) Delete(target string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, target)
	if path, ok := f.files[target]; ok {
		return os.Remove(path)
	}
	return nil
}

func seedRegistry(t *testing.T, store *Store, fn func(reg *Registry)) {
	t.Helper()
	if err := store.Update(func(reg *Registry) error {
		fn(reg)
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func loadRegistry(t *testing.T, store *Store) *Registry {
	t.Helper()
	reg, err := store.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	return reg
}

func TestAttachKit(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seedRegistry(t, store, func(reg *Registry) {
		reg.Kits["example"] = Kit{Dir: "/kits/example"}
	})

	// Attaching to a new target registers the target and saves.
	if err := store.AttachKit("example", "/targets/main"); err != nil {
		t.Fatalf("AttachKit: %v", err)
	}
	reg := loadRegistry(t, store)
	if _, ok := reg.Targets["/targets/main"]; !ok {
		t.Error("the attached target was not registered")
	}
	if diff := cmp.Diff([]string{"example"}, reg.KitsFor("/targets/main")); diff != "" {
		t.Errorf("kits of the new target (-want +got):\n%s", diff)
	}

	// A second target keeps the first attachment.
	if err := store.AttachKit("example", "/targets/wsl"); err != nil {
		t.Fatalf("AttachKit to a second target: %v", err)
	}
	reg = loadRegistry(t, store)
	if diff := cmp.Diff([]string{"example"}, reg.KitsFor("/targets/wsl")); diff != "" {
		t.Errorf("kits of the second target (-want +got):\n%s", diff)
	}

	// Attaching an already attached kit is refused.
	err = store.AttachKit("example", "/targets/main")
	if err == nil || !strings.Contains(err.Error(), "already attached") {
		t.Fatalf("repeated AttachKit = %v, want an already-attached error", err)
	}

	// An unregistered kit is refused.
	err = store.AttachKit("ghost", "/targets/main")
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("AttachKit of an unregistered kit = %v, want a not-registered error", err)
	}
}

func TestDetachKit(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seedRegistry(t, store, func(reg *Registry) {
		reg.Kits["example"] = Kit{Dir: "/kits/example"}
		reg.Kits["team"] = Kit{Dir: "/kits/team"}
		reg.Targets["/targets/main"] = Target{}
		reg.Targets["/targets/wsl"] = Target{}
		reg.Attachments = []Attachment{
			{Kit: "example", Target: "/targets/main"},
			{Kit: "team", Target: "/targets/main"},
			{Kit: "example", Target: "/targets/wsl"},
		}
		reg.Pending["/targets/main"] = &Pending{
			Items: map[string]*ItemPending{
				"example/writing": {Select: true, Answers: map[string]string{"src": "/src"}},
				"team/review":     {Remove: true},
			},
		}
		reg.Pending["/targets/wsl"] = &Pending{
			Items: map[string]*ItemPending{
				"example/writing": {Select: true},
			},
		}
	})

	// Detaching removes the attachment alone: the kit and the target stay
	// registered, the other kit's attachment is untouched.
	if err := store.DetachKit("example", "/targets/main"); err != nil {
		t.Fatalf("DetachKit: %v", err)
	}
	reg := loadRegistry(t, store)
	if diff := cmp.Diff([]string{"team"}, reg.KitsFor("/targets/main")); diff != "" {
		t.Errorf("kits of the target after the detach (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"example"}, reg.KitsFor("/targets/wsl")); diff != "" {
		t.Errorf("kits of the other target (-want +got):\n%s", diff)
	}
	if _, ok := reg.Kits["example"]; !ok {
		t.Error("the detached kit was unregistered")
	}
	if _, ok := reg.Targets["/targets/main"]; !ok {
		t.Error("the target of the detach was unregistered")
	}

	// The kit's pending select in the target is dropped with its answers,
	// the other kit's remove mark stays.
	items := reg.Pending["/targets/main"].Items
	if item := items["example/writing"]; item != nil {
		t.Errorf("detached kit's pending select = %+v, want it dropped", item)
	}
	if item := items["team/review"]; item == nil || !item.Remove {
		t.Errorf("other kit's pending remove = %+v, want it kept", items["team/review"])
	}

	// A select in another target is untouched.
	if item := reg.Pending["/targets/wsl"].Items["example/writing"]; item == nil || !item.Select {
		t.Errorf("select in another target = %+v, want it kept", reg.Pending["/targets/wsl"].Items["example/writing"])
	}

	// Detaching the last attachment leaves no pending object behind.
	if err := store.DetachKit("example", "/targets/wsl"); err != nil {
		t.Fatalf("DetachKit of the last attachment: %v", err)
	}
	reg = loadRegistry(t, store)
	if got := reg.KitsFor("/targets/wsl"); len(got) != 0 {
		t.Errorf("kits of the target = %v, want none", got)
	}
	if _, ok := reg.Pending["/targets/wsl"]; ok {
		t.Error("the pending object survived the detach of its kit")
	}

	// Errors: an unregistered kit and an attachment that is not there.
	err = store.DetachKit("ghost", "/targets/main")
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("DetachKit of an unregistered kit = %v, want a not-registered error", err)
	}
	err = store.DetachKit("team", "/targets/wsl")
	if err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("DetachKit of an unattached kit = %v, want a not-attached error", err)
	}
}

func TestDeferDetachKit(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seedRegistry(t, store, func(reg *Registry) {
		reg.Kits["example"] = Kit{Dir: "/kits/example"}
		reg.Kits["team"] = Kit{Dir: "/kits/team"}
		reg.Targets["/targets/main"] = Target{}
		reg.Attachments = []Attachment{{Kit: "example", Target: "/targets/main"}}
		reg.Pending["/targets/main"] = &Pending{
			Items: map[string]*ItemPending{
				"team/review": {Select: true},
			},
		}
	})

	// Queueing records the deferred detach and keeps the attachment and
	// the pending items.
	if err := store.DeferDetachKit("example", "/targets/main"); err != nil {
		t.Fatalf("DeferDetachKit: %v", err)
	}
	reg := loadRegistry(t, store)
	want := []Deferred{{Kind: DeferredDetachKit, Kit: "example"}}
	if diff := cmp.Diff(want, reg.Pending["/targets/main"].Deferred); diff != "" {
		t.Errorf("deferred operations (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"example"}, reg.KitsFor("/targets/main")); diff != "" {
		t.Errorf("kits of the target after queueing (-want +got):\n%s", diff)
	}
	if reg.Pending["/targets/main"].Items["team/review"] == nil {
		t.Error("queueing dropped the pending items")
	}

	// Queueing again changes nothing.
	if err := store.DeferDetachKit("example", "/targets/main"); err != nil {
		t.Fatalf("repeated DeferDetachKit: %v", err)
	}
	reg = loadRegistry(t, store)
	if diff := cmp.Diff(want, reg.Pending["/targets/main"].Deferred); diff != "" {
		t.Errorf("deferred operations after queueing twice (-want +got):\n%s", diff)
	}

	// A target without pending actions gains one holding the deferred
	// detach only.
	seedRegistry(t, store, func(reg *Registry) {
		reg.Targets["/targets/other"] = Target{}
		reg.Attachments = append(reg.Attachments, Attachment{Kit: "example", Target: "/targets/other"})
	})
	if err := store.DeferDetachKit("example", "/targets/other"); err != nil {
		t.Fatalf("DeferDetachKit to a target without pending actions: %v", err)
	}
	reg = loadRegistry(t, store)
	if diff := cmp.Diff(want, reg.Pending["/targets/other"].Deferred); diff != "" {
		t.Errorf("deferred operations of the new pending object (-want +got):\n%s", diff)
	}
	if len(reg.Pending["/targets/other"].Items) != 0 {
		t.Errorf("items = %v, want none", reg.Pending["/targets/other"].Items)
	}

	// Errors: an unregistered kit and an attachment that is not there.
	if err := store.DeferDetachKit("ghost", "/targets/main"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("DeferDetachKit of an unregistered kit = %v, want a not-registered error", err)
	}
	if err := store.DeferDetachKit("example", "/targets/gone"); err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("DeferDetachKit of an unattached kit = %v, want a not-attached error", err)
	}
}

func TestEditKitPath(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seedRegistry(t, store, func(reg *Registry) {
		reg.Kits["example"] = Kit{Dir: "/kits/example"}
	})

	// The edit is accepted when the new directory holds the same kit.
	if err := store.EditKitPath("example", "/kits/moved", "example"); err != nil {
		t.Fatalf("EditKitPath: %v", err)
	}
	reg := loadRegistry(t, store)
	if got := reg.Kits["example"].Dir; got != "/kits/moved" {
		t.Errorf("kit dir = %q, want /kits/moved", got)
	}

	// A mismatch is refused with the reason naming both kits, and the
	// registered directory stands.
	err = store.EditKitPath("example", "/kits/other", "team-tools")
	if err == nil || !strings.Contains(err.Error(), `holds the kit "team-tools", not "example"`) {
		t.Fatalf("EditKitPath mismatch = %v, want a reason naming both kits", err)
	}
	reg = loadRegistry(t, store)
	if got := reg.Kits["example"].Dir; got != "/kits/moved" {
		t.Errorf("kit dir after the refused edit = %q, want /kits/moved", got)
	}

	// An unregistered kit is refused.
	if err := store.EditKitPath("ghost", "/kits/ghost", "ghost"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("EditKitPath of an unregistered kit = %v, want a not-registered error", err)
	}
}

func TestForgetTarget(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The target holds a file, the data directory holds the lock file.
	targetDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(targetDir, "AGENTS.md")
	if err := os.WriteFile(targetFile, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	lockFile := filepath.Join(dir, "locks", "main.json")
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	seedRegistry(t, store, func(reg *Registry) {
		reg.Kits["example"] = Kit{Dir: "/kits/example"}
		reg.Kits["team"] = Kit{Dir: "/kits/team"}
		reg.Targets["/targets/main"] = Target{Name: "main"}
		reg.Targets["/targets/wsl"] = Target{}
		reg.Attachments = []Attachment{
			{Kit: "example", Target: "/targets/main"},
			{Kit: "team", Target: "/targets/main"},
		}
		reg.Pending["/targets/main"] = &Pending{
			Items: map[string]*ItemPending{
				"example/writing": {Select: true},
			},
		}
		reg.LastTarget = "/targets/main"
	})
	locks := &fakeLocks{files: map[string]string{"/targets/main": lockFile}}

	// The forget deletes the lock and removes the target with its
	// attachments, pending actions and last-target mark.
	if err := store.ForgetTarget("/targets/main", locks); err != nil {
		t.Fatalf("ForgetTarget: %v", err)
	}
	if diff := cmp.Diff([]string{"/targets/main"}, locks.deleted); diff != "" {
		t.Errorf("deleted locks (-want +got):\n%s", diff)
	}
	if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
		t.Errorf("the lock file survived the forget: %v", err)
	}
	reg := loadRegistry(t, store)
	if _, ok := reg.Targets["/targets/main"]; ok {
		t.Error("the forgotten target stayed registered")
	}
	if got := reg.KitsFor("/targets/main"); len(got) != 0 {
		t.Errorf("kits of the forgotten target = %v, want none", got)
	}
	if _, ok := reg.Pending["/targets/main"]; ok {
		t.Error("the pending actions of the forgotten target survived")
	}
	if reg.LastTarget != "" {
		t.Errorf("last target = %q, want it cleared", reg.LastTarget)
	}
	if _, ok := reg.Targets["/targets/wsl"]; !ok {
		t.Error("another target was unregistered")
	}
	if _, ok := reg.Kits["example"]; !ok {
		t.Error("a kit was unregistered by the forget")
	}

	// The target's files are untouched.
	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("the target's file did not survive the forget: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("target file content = %q, want it untouched", data)
	}

	// Forgetting an unregistered target is refused, and the lock of no
	// target is touched.
	before := len(locks.deleted)
	err = store.ForgetTarget("/targets/main", locks)
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("ForgetTarget of an unregistered target = %v, want a not-registered error", err)
	}
	if len(locks.deleted) != before {
		t.Error("the lock of an unregistered target was deleted")
	}

	// A failing lock deletion abandons the forget.
	seedRegistry(t, store, func(reg *Registry) {
		reg.Targets["/targets/doomed"] = Target{}
	})
	failing := &fakeLocks{err: os.ErrPermission}
	err = store.ForgetTarget("/targets/doomed", failing)
	if err == nil || !strings.Contains(err.Error(), "forget /targets/doomed") {
		t.Fatalf("ForgetTarget with a failing deletion = %v, want the deletion error", err)
	}
	reg = loadRegistry(t, store)
	if _, ok := reg.Targets["/targets/doomed"]; !ok {
		t.Error("the target was forgotten although the lock deletion failed")
	}
}

func TestDeferForgetTarget(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seedRegistry(t, store, func(reg *Registry) {
		reg.Kits["example"] = Kit{Dir: "/kits/example"}
		reg.Targets["/targets/main"] = Target{}
		reg.Attachments = []Attachment{{Kit: "example", Target: "/targets/main"}}
	})

	locks := &fakeLocks{}
	// Queueing records the deferred forget and keeps the target.
	if err := store.DeferForgetTarget("/targets/main"); err != nil {
		t.Fatalf("DeferForgetTarget: %v", err)
	}
	reg := loadRegistry(t, store)
	want := []Deferred{{Kind: DeferredForgetTarget}}
	if diff := cmp.Diff(want, reg.Pending["/targets/main"].Deferred); diff != "" {
		t.Errorf("deferred operations (-want +got):\n%s", diff)
	}
	if _, ok := reg.Targets["/targets/main"]; !ok {
		t.Error("queueing unregistered the target")
	}
	if len(locks.deleted) != 0 {
		t.Error("queueing deleted the lock")
	}

	// Queueing again changes nothing.
	if err := store.DeferForgetTarget("/targets/main"); err != nil {
		t.Fatalf("repeated DeferForgetTarget: %v", err)
	}
	reg = loadRegistry(t, store)
	if diff := cmp.Diff(want, reg.Pending["/targets/main"].Deferred); diff != "" {
		t.Errorf("deferred operations after queueing twice (-want +got):\n%s", diff)
	}

	// An unregistered target is refused.
	if err := store.DeferForgetTarget("/targets/gone"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("DeferForgetTarget of an unregistered target = %v, want a not-registered error", err)
	}
}

func TestCompleteDeferred(t *testing.T) {
	locks := &fakeLocks{}

	t.Run("detach completes", func(t *testing.T) {
		store, err := Open(t.TempDir())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		seedRegistry(t, store, func(reg *Registry) {
			reg.Kits["example"] = Kit{Dir: "/kits/example"}
			reg.Kits["team"] = Kit{Dir: "/kits/team"}
			reg.Targets["/targets/main"] = Target{}
			reg.Attachments = []Attachment{
				{Kit: "example", Target: "/targets/main"},
				{Kit: "team", Target: "/targets/main"},
			}
			reg.Pending["/targets/main"] = &Pending{
				Items: map[string]*ItemPending{
					"example/writing": {Select: true, Answers: map[string]string{"src": "/src"}},
					"team/review":     {Remove: true},
				},
				Deferred: []Deferred{{Kind: DeferredDetachKit, Kit: "example"}},
			}
		})

		// The apply removed everything the detach covers.
		report, err := store.CompleteDeferred("/targets/main", nil, locks)
		if err != nil {
			t.Fatalf("CompleteDeferred: %v", err)
		}
		want := []string{"completed the deferred detach of example from /targets/main"}
		if diff := cmp.Diff(want, report); diff != "" {
			t.Errorf("report (-want +got):\n%s", diff)
		}
		reg := loadRegistry(t, store)
		if diff := cmp.Diff([]string{"team"}, reg.KitsFor("/targets/main")); diff != "" {
			t.Errorf("kits of the target (-want +got):\n%s", diff)
		}
		items := reg.Pending["/targets/main"].Items
		if item := items["example/writing"]; item != nil {
			t.Errorf("detached kit's pending select = %+v, want it dropped", item)
		}
		if item := items["team/review"]; item == nil || !item.Remove {
			t.Errorf("other kit's pending remove = %+v, want it kept", items["team/review"])
		}
		if len(reg.Pending["/targets/main"].Deferred) != 0 {
			t.Errorf("deferred operations = %v, want them consumed", reg.Pending["/targets/main"].Deferred)
		}
	})

	t.Run("detach drops", func(t *testing.T) {
		store, err := Open(t.TempDir())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		seedRegistry(t, store, func(reg *Registry) {
			reg.Kits["example"] = Kit{Dir: "/kits/example"}
			reg.Targets["/targets/main"] = Target{}
			reg.Attachments = []Attachment{{Kit: "example", Target: "/targets/main"}}
			reg.Pending["/targets/main"] = &Pending{
				Deferred: []Deferred{{Kind: DeferredDetachKit, Kit: "example"}},
			}
		})

		// The apply left an item of the kit installed, so the detach is
		// dropped with the reason and the attachment stays.
		report, err := store.CompleteDeferred("/targets/main", []string{"team/x", "example/writing"}, locks)
		if err != nil {
			t.Fatalf("CompleteDeferred: %v", err)
		}
		want := []string{"dropped the deferred detach of example from /targets/main: installed items remain: example/writing"}
		if diff := cmp.Diff(want, report); diff != "" {
			t.Errorf("report (-want +got):\n%s", diff)
		}
		reg := loadRegistry(t, store)
		if diff := cmp.Diff([]string{"example"}, reg.KitsFor("/targets/main")); diff != "" {
			t.Errorf("kits of the target (-want +got):\n%s", diff)
		}
		// No items and no deferred operations are left, so the pending
		// object is gone.
		if _, ok := reg.Pending["/targets/main"]; ok {
			t.Errorf("the pending object = %+v, want it collapsed", reg.Pending["/targets/main"])
		}
	})

	t.Run("forget completes", func(t *testing.T) {
		dir := t.TempDir()
		store, err := Open(dir)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		lockFile := filepath.Join(dir, "locks", "main.json")
		if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lockFile, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		forgetLocks := &fakeLocks{files: map[string]string{"/targets/main": lockFile}}
		seedRegistry(t, store, func(reg *Registry) {
			reg.Kits["example"] = Kit{Dir: "/kits/example"}
			reg.Targets["/targets/main"] = Target{}
			reg.Attachments = []Attachment{{Kit: "example", Target: "/targets/main"}}
			reg.Pending["/targets/main"] = &Pending{
				Deferred: []Deferred{{Kind: DeferredForgetTarget}},
			}
			reg.LastTarget = "/targets/main"
		})

		// The apply removed the last installed item: the forget deletes
		// the lock and removes the target with everything hanging on it.
		report, err := store.CompleteDeferred("/targets/main", nil, forgetLocks)
		if err != nil {
			t.Fatalf("CompleteDeferred: %v", err)
		}
		want := []string{"completed the deferred forget of /targets/main"}
		if diff := cmp.Diff(want, report); diff != "" {
			t.Errorf("report (-want +got):\n%s", diff)
		}
		if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
			t.Errorf("the lock file survived the forget: %v", err)
		}
		reg := loadRegistry(t, store)
		if _, ok := reg.Targets["/targets/main"]; ok {
			t.Error("the forgotten target stayed registered")
		}
		if _, ok := reg.Pending["/targets/main"]; ok {
			t.Error("the pending actions of the forgotten target survived")
		}
		if reg.LastTarget != "" {
			t.Errorf("last target = %q, want it cleared", reg.LastTarget)
		}
	})

	t.Run("forget drops while a covered detach completes", func(t *testing.T) {
		store, err := Open(t.TempDir())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		seedRegistry(t, store, func(reg *Registry) {
			reg.Kits["example"] = Kit{Dir: "/kits/example"}
			reg.Kits["team"] = Kit{Dir: "/kits/team"}
			reg.Targets["/targets/main"] = Target{}
			reg.Attachments = []Attachment{
				{Kit: "example", Target: "/targets/main"},
				{Kit: "team", Target: "/targets/main"},
			}
			reg.Pending["/targets/main"] = &Pending{
				Deferred: []Deferred{
					{Kind: DeferredForgetTarget},
					{Kind: DeferredDetachKit, Kit: "example"},
				},
			}
		})

		// The apply removed example's items but team's remain: the forget
		// drops, the detach of example completes, the lock of the living
		// target stays.
		report, err := store.CompleteDeferred("/targets/main", []string{"team/x"}, locks)
		if err != nil {
			t.Fatalf("CompleteDeferred: %v", err)
		}
		want := []string{
			"dropped the deferred forget of /targets/main: installed items remain: team/x",
			"completed the deferred detach of example from /targets/main",
		}
		if diff := cmp.Diff(want, report); diff != "" {
			t.Errorf("report (-want +got):\n%s", diff)
		}
		if len(locks.deleted) != 0 {
			t.Errorf("deleted locks = %v, want none for a living target", locks.deleted)
		}
		reg := loadRegistry(t, store)
		if _, ok := reg.Targets["/targets/main"]; !ok {
			t.Error("the target was forgotten although its items remain")
		}
		if diff := cmp.Diff([]string{"team"}, reg.KitsFor("/targets/main")); diff != "" {
			t.Errorf("kits of the target (-want +got):\n%s", diff)
		}
	})

	t.Run("forget and detach both drop", func(t *testing.T) {
		store, err := Open(t.TempDir())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		seedRegistry(t, store, func(reg *Registry) {
			reg.Kits["example"] = Kit{Dir: "/kits/example"}
			reg.Kits["team"] = Kit{Dir: "/kits/team"}
			reg.Targets["/targets/main"] = Target{}
			reg.Attachments = []Attachment{
				{Kit: "example", Target: "/targets/main"},
				{Kit: "team", Target: "/targets/main"},
			}
			reg.Pending["/targets/main"] = &Pending{
				Deferred: []Deferred{
					{Kind: DeferredForgetTarget},
					{Kind: DeferredDetachKit, Kit: "example"},
				},
			}
		})

		report, err := store.CompleteDeferred("/targets/main", []string{"example/w", "team/x"}, locks)
		if err != nil {
			t.Fatalf("CompleteDeferred: %v", err)
		}
		want := []string{
			"dropped the deferred forget of /targets/main: installed items remain: example/w, team/x",
			"dropped the deferred detach of example from /targets/main: installed items remain: example/w",
		}
		if diff := cmp.Diff(want, report); diff != "" {
			t.Errorf("report (-want +got):\n%s", diff)
		}
		reg := loadRegistry(t, store)
		if diff := cmp.Diff([]string{"example", "team"}, reg.KitsFor("/targets/main")); diff != "" {
			t.Errorf("kits of the target (-want +got):\n%s", diff)
		}
		if _, ok := reg.Pending["/targets/main"]; ok {
			t.Errorf("the pending object = %+v, want it collapsed with the dropped operations consumed", reg.Pending["/targets/main"])
		}
	})

	t.Run("nothing to complete", func(t *testing.T) {
		store, err := Open(t.TempDir())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		seedRegistry(t, store, func(reg *Registry) {
			reg.Kits["example"] = Kit{Dir: "/kits/example"}
			reg.Targets["/targets/main"] = Target{}
			reg.Targets["/targets/bare"] = Target{}
			reg.Attachments = []Attachment{{Kit: "example", Target: "/targets/main"}}
			reg.Pending["/targets/main"] = &Pending{
				Items: map[string]*ItemPending{
					"example/writing": {Select: true},
				},
			}
		})

		// Pending items without deferred operations and a target without
		// pending actions complete nothing and change nothing.
		report, err := store.CompleteDeferred("/targets/main", []string{"example/w"}, locks)
		if err != nil {
			t.Fatalf("CompleteDeferred: %v", err)
		}
		if report != nil {
			t.Errorf("report = %v, want none", report)
		}
		report, err = store.CompleteDeferred("/targets/bare", nil, locks)
		if err != nil {
			t.Fatalf("CompleteDeferred on a target without pending actions: %v", err)
		}
		if report != nil {
			t.Errorf("report = %v, want none", report)
		}
		reg := loadRegistry(t, store)
		if reg.Pending["/targets/main"].Items["example/writing"] == nil {
			t.Error("the pending items were touched without deferred operations")
		}
	})

	t.Run("errors", func(t *testing.T) {
		store, err := Open(t.TempDir())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		seedRegistry(t, store, func(reg *Registry) {
			reg.Targets["/targets/main"] = Target{}
		})

		// An unregistered target is refused.
		if _, err := store.CompleteDeferred("/targets/gone", nil, locks); err == nil || !strings.Contains(err.Error(), "not registered") {
			t.Fatalf("CompleteDeferred of an unregistered target = %v, want a not-registered error", err)
		}

		// A failing lock deletion abandons the completion.
		seedRegistry(t, store, func(reg *Registry) {
			reg.Targets["/targets/doomed"] = Target{}
			reg.Pending["/targets/doomed"] = &Pending{
				Deferred: []Deferred{{Kind: DeferredForgetTarget}},
			}
		})
		failing := &fakeLocks{err: os.ErrPermission}
		report, err := store.CompleteDeferred("/targets/doomed", nil, failing)
		if err == nil || !strings.Contains(err.Error(), "forget /targets/doomed") {
			t.Fatalf("CompleteDeferred with a failing deletion = %v, want the deletion error", err)
		}
		if report != nil {
			t.Errorf("report = %v, want none for a failed completion", report)
		}
		reg := loadRegistry(t, store)
		if _, ok := reg.Targets["/targets/doomed"]; !ok {
			t.Error("the target was forgotten although the lock deletion failed")
		}
		if len(reg.Pending["/targets/doomed"].Deferred) != 1 {
			t.Error("the deferred forget was consumed although the lock deletion failed")
		}
	})
}

func TestRegisterKit(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := store.RegisterKit("ex", "/kits/ex"); err != nil {
		t.Fatalf("RegisterKit: %v", err)
	}
	reg := loadRegistry(t, store)
	if k := reg.Kits["ex"]; k.Dir != "/kits/ex" {
		t.Errorf("registered dir = %q, want /kits/ex", k.Dir)
	}

	// Registering the same directory again changes nothing.
	if err := store.RegisterKit("ex", "/kits/ex"); err != nil {
		t.Fatalf("re-register: %v", err)
	}

	// The name is the identity: another directory under it is refused.
	if err := store.RegisterKit("ex", "/kits/ex-new"); err == nil ||
		!strings.Contains(err.Error(), `kit "ex" is already registered at /kits/ex`) {
		t.Fatalf("re-register elsewhere = %v, want the refusal", err)
	}
	if k := loadRegistry(t, store).Kits["ex"]; k.Dir != "/kits/ex" {
		t.Errorf("registered dir = %q, want the refused registration to change nothing", k.Dir)
	}
}

func TestRegisterKitReservedName(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The name of the built-in kit is refused on the user-facing path and
	// nothing is written under it.
	err = store.RegisterKit(BuiltinKitName, "/kits/mine")
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("RegisterKit under the reserved name = %v, want a reserved error", err)
	}
	if _, ok := loadRegistry(t, store).Kits[BuiltinKitName]; ok {
		t.Error("the reserved name was written by the refused registration")
	}

	// RegisterBuiltinKit is the only other writer of the name: it adds the
	// entry once, keeps it over the same directory, and refuses another
	// directory under the name.
	added, err := store.RegisterBuiltinKit("/data/builtin-kit")
	if err != nil || !added {
		t.Fatalf("RegisterBuiltinKit = %v, %v; want added, nil", added, err)
	}
	added, err = store.RegisterBuiltinKit("/data/builtin-kit")
	if err != nil || added {
		t.Fatalf("repeated RegisterBuiltinKit = %v, %v; want not added, nil", added, err)
	}
	_, err = store.RegisterBuiltinKit("/data/other")
	if err == nil || !strings.Contains(err.Error(), "already registered at /data/builtin-kit") {
		t.Fatalf("RegisterBuiltinKit elsewhere = %v, want the refusal", err)
	}
	if k := loadRegistry(t, store).Kits[BuiltinKitName]; k.Dir != "/data/builtin-kit" {
		t.Errorf("registered dir = %q, want the refused registration to change nothing", k.Dir)
	}
}

func TestRegisterTarget(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// A target registers without a kit attached to it.
	if err := store.RegisterTarget("/targets/wsl"); err != nil {
		t.Fatalf("RegisterTarget: %v", err)
	}
	reg := loadRegistry(t, store)
	if _, ok := reg.Targets["/targets/wsl"]; !ok {
		t.Errorf("targets = %v, want /targets/wsl registered", reg.Targets)
	}

	// A target path is the identity: registering it again changes nothing
	// and is not an error.
	if err := store.RegisterTarget("/targets/wsl"); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if n := len(loadRegistry(t, store).Targets); n != 1 {
		t.Errorf("targets = %d, want 1", n)
	}
}
