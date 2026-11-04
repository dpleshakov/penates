package lock

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	store := NewStore(dataDir)
	target := filepath.Join(dataDir, "target")

	want := &Lock{
		Target: target,
		Items: map[string]*Item{
			"example/git-via-gh": {
				Version: "1.2.0",
				Answers: map[string]string{"path": "/somewhere"},
				Files:   map[string]string{"skills/git-via-gh/SKILL.md": "sha256:9ab2"},
				DirsCreated: []string{"skills/git-via-gh"},
				Sections: map[string]Section{
					"AGENTS.md": {Block: "example/git-via-gh", Hash: "sha256:41c0"},
				},
				JSON: map[string][]Location{
					"opencode.json": {{Pointer: "/permission/bash/gh *", Hash: "sha256:77de"}},
				},
			},
		},
		CreatedSharedFiles: []string{"AGENTS.md"},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load(target)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("lock mismatch after round trip (-want +got):\n%s", diff)
	}
}

func TestLoadMissing(t *testing.T) {
	store := NewStore(t.TempDir())
	target := filepath.Join(t.TempDir(), "target")

	l, err := store.Load(target)
	if err != nil {
		t.Fatalf("Load on a fresh directory: %v", err)
	}
	if l.Target != target {
		t.Errorf("Target = %q, want %q", l.Target, target)
	}
	if len(l.Items) != 0 {
		t.Errorf("Items = %v, want empty", l.Items)
	}
}

func TestOwnership(t *testing.T) {
	l := &Lock{
		Target: "/target",
		Items: map[string]*Item{
			"alpha/one": {
				Version:     "1.0.0",
				Files:       map[string]string{"skills/one/SKILL.md": "sha256:aa"},
				DirsCreated: []string{"skills/one", "skills"},
			},
			"beta/two": {
				Version:     "2.0.0",
				Files:       map[string]string{"agents/two.md": "sha256:bb"},
				DirsCreated: []string{"agents"},
			},
		},
	}

	if key, hash, ok := l.FileOwner("skills/one/SKILL.md"); !ok || key != "alpha/one" || hash != "sha256:aa" {
		t.Errorf("FileOwner(skills/one/SKILL.md) = %q, %q, %v", key, hash, ok)
	}
	if _, _, ok := l.FileOwner("user/own.md"); ok {
		t.Error("FileOwner reported a file no item owns")
	}
	if !l.OwnsFile("alpha/one", "skills/one/SKILL.md") {
		t.Error("OwnsFile(alpha/one, skills/one/SKILL.md) = false")
	}
	if l.OwnsFile("beta/two", "skills/one/SKILL.md") {
		t.Error("OwnsFile claimed another item's file")
	}
	if !l.CreatedDir("alpha/one", "skills/one") {
		t.Error("CreatedDir(alpha/one, skills/one) = false")
	}
	if l.CreatedDir("beta/two", "skills/one") {
		t.Error("CreatedDir claimed another item's directory")
	}

	l.SetItem("alpha/one", &Item{Version: "1.1.0"})
	if got := l.Items["alpha/one"].Version; got != "1.1.0" {
		t.Errorf("SetItem did not replace the record; version = %q", got)
	}
	if !l.RemoveItem("beta/two") {
		t.Error("RemoveItem(beta/two) = false, want true")
	}
	if l.RemoveItem("beta/two") {
		t.Error("RemoveItem removed the same record twice")
	}
	if _, _, ok := l.FileOwner("agents/two.md"); ok {
		t.Error("FileOwner found a file of a removed item")
	}
}
