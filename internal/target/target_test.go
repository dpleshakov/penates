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

	snap, err := Read(root, []string{
		"skills/one/SKILL.md",
		"AGENTS.md",
		"agents",        // a directory where a file could be expected
		"missing/x.txt", // never created
	}, nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
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
	changed, err := Read(root, []string{"AGENTS.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed.File("AGENTS.md").Hash == got.Hash {
		t.Error("hash did not change with the content")
	}
}

func TestSnapshotShared(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "AGENTS.md"),
		"# Notes\n\n<!-- penates:begin kit/one -->\n## House rules\n\nbody\n<!-- penates:end kit/one -->\n\ntail\n")

	snap, err := Read(root, nil, []string{"AGENTS.md", "missing/Shared.md"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	shared := snap.SharedFile("AGENTS.md")
	if !shared.Exists || shared.IsDir {
		t.Errorf("shared file: %+v", shared)
	}
	if !strings.Contains(shared.Content, "# Notes") || !strings.Contains(shared.Content, "<!-- penates:begin kit/one -->") {
		t.Errorf("content = %q", shared.Content)
	}
	h, ok := shared.BlockHash("kit/one")
	if !ok || !strings.HasPrefix(h, "sha256:") {
		t.Errorf("block hash = %q, %v", h, ok)
	}
	if len(shared.Headings) != 1 || shared.Headings[0] != "notes" {
		t.Errorf("headings = %q, want only the outside \"notes\"", shared.Headings)
	}

	missing := snap.SharedFile("missing/Shared.md")
	if missing.Exists || missing.Content != "" || len(missing.Blocks) != 0 {
		t.Errorf("missing shared file: %+v", missing)
	}
}

func TestSnapshotRejectsBadPaths(t *testing.T) {
	for _, p := range []string{"", "/abs", "../x", "a/../b", `a\b`, "C:x"} {
		if _, err := Read(t.TempDir(), []string{p}, nil); err == nil {
			t.Errorf("file path %q was accepted", p)
		}
		if _, err := Read(t.TempDir(), nil, []string{p}); err == nil {
			t.Errorf("shared path %q was accepted", p)
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
