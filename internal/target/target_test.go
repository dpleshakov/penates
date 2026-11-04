package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "skills", "one", "SKILL.md"), "content\n")
	write(t, filepath.Join(root, "AGENTS.md"), "notes\n")
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	snap, err := SnapshotFiles(root, []string{
		"skills/one/SKILL.md",
		"AGENTS.md",
		"agents",        // a directory where a file could be expected
		"missing/x.txt", // never created
	})
	if err != nil {
		t.Fatalf("SnapshotFiles: %v", err)
	}

	got := snap.File("skills/one/SKILL.md")
	if !got.Exists || !strings.HasPrefix(got.Hash, "sha256:") {
		t.Errorf("existing file: %+v", got)
	}
	if snap.File("missing/x.txt").Exists {
		t.Error("missing file reported as existing")
	}
	dir := snap.File("agents")
	if !dir.Exists || dir.Hash != "" {
		t.Errorf("directory: %+v, want Exists without a hash", dir)
	}

	// A changed file yields a different hash.
	write(t, filepath.Join(root, "AGENTS.md"), "changed\n")
	changed, err := SnapshotFiles(root, []string{"AGENTS.md"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.File("AGENTS.md").Hash == got.Hash {
		t.Error("hash did not change with the content")
	}
}

func TestSnapshotRejectsBadPaths(t *testing.T) {
	for _, p := range []string{"", "/abs", "../x", "a/../b", `a\b`, "C:x"} {
		if _, err := SnapshotFiles(t.TempDir(), []string{p}); err == nil {
			t.Errorf("path %q was accepted", p)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
