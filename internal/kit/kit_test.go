package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLoadValid(t *testing.T) {
	k, err := Load(filepath.Join("testdata", "example"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if k.Name != "example" {
		t.Errorf("name = %q, want %q", k.Name, "example")
	}
	if k.TargetHint != "~/.config/example-harness" {
		t.Errorf("target_hint = %q", k.TargetHint)
	}
	if diff := cmp.Diff([]string{"AGENTS.md", "config.jsonc"}, k.Shared); diff != "" {
		t.Errorf("shared mismatch (-want +got):\n%s", diff)
	}

	var ids []string
	for _, it := range k.Items {
		ids = append(ids, it.ID)
	}
	if diff := cmp.Diff([]string{"agenda", "gh-note", "writing"}, ids); diff != "" {
		t.Errorf("items (-want +got):\n%s", diff)
	}

	writing := k.Items[2]
	if !writing.HasChangelog {
		t.Error("writing: HasChangelog = false, want true")
	}
	if len(writing.Vars) != 0 {
		t.Errorf("writing: vars = %v, want none", writing.Vars)
	}
	want := []Contribution{{Kind: KindFile, Src: "skill/SKILL.md", To: "skills/writing/SKILL.md"}}
	if diff := cmp.Diff(want, writing.Contributes); diff != "" {
		t.Errorf("writing contributions (-want +got):\n%s", diff)
	}

	ghNote := k.Items[1]
	if ghNote.HasChangelog {
		t.Error("gh-note: HasChangelog = true, want false")
	}
	want = []Contribution{
		{Kind: KindSection, Src: "note.md", To: "AGENTS.md"},
		{Kind: KindJSON, Src: "perms.jsonc", To: "config.jsonc"},
	}
	if diff := cmp.Diff(want, ghNote.Contributes); diff != "" {
		t.Errorf("gh-note contributions (-want +got):\n%s", diff)
	}

	if key := ItemKey(k.Name, writing.ID); key != "example/writing" {
		t.Errorf("ItemKey = %q", key)
	}
}

func TestLoadInvalid(t *testing.T) {
	validKit := map[string]string{
		"kit.yaml":          "name: example\ntarget_hint: ~/t\nshared:\n  - AGENTS.md\n",
		"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills/a/f\n",
		"items/a/f":         "content\n",
	}

	tests := map[string]struct {
		files   map[string]string // written on top of a copy of validKit; nil value deletes
		wantErr string
	}{
		"missing kit.yaml": {
			files:   map[string]string{"kit.yaml": ""},
			wantErr: "kit.yaml",
		},
		"missing items directory": {
			files: map[string]string{
				"items/a/item.yaml": "",
				"items/a/f":         "",
			},
			wantErr: "reading items",
		},
		"empty name": {
			files:   map[string]string{"kit.yaml": "target_hint: ~/t\n"},
			wantErr: "name: is required",
		},
		"name must be a token": {
			files:   map[string]string{"kit.yaml": "name: Example Kit\n"},
			wantErr: "name: must match",
		},
		"shared path with dotdot": {
			files:   map[string]string{"kit.yaml": "name: example\nshared:\n  - ../AGENTS.md\n"},
			wantErr: `shared[0]: "../AGENTS.md" is not a relative`,
		},
		"no items": {
			files: map[string]string{
				"items/a/item.yaml": "",
				"items/a/f":         "",
				"items/README.md":   "not an item\n",
			},
			wantErr: "no items found",
		},
		"item without item.yaml": {
			files:   map[string]string{"items/a/item.yaml": ""},
			wantErr: "items/a/item.yaml: cannot be read",
		},
		"bad item id": {
			files:   map[string]string{"items/a/item.yaml": "id: A!\nversion: 1.0.0\n"},
			wantErr: "id: must match",
		},
		"duplicate item id": {
			files: map[string]string{
				"items/b/item.yaml": "id: a\nversion: 1.0.0\n",
			},
			wantErr: `"a" is already used by another item`,
		},
		"missing version": {
			files:   map[string]string{"items/a/item.yaml": "id: a\n"},
			wantErr: "version: is required",
		},
		"version not semantic": {
			files:   map[string]string{"items/a/item.yaml": "id: a\nversion: 1.2\n"},
			wantErr: `"1.2" is not a semantic version`,
		},
		"contribution without kind": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - to: skills/a/f\n",
			},
			wantErr: "needs exactly one of file, section, or json",
		},
		"contribution with two kinds": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    section: s\n    to: skills/a/f\n",
			},
			wantErr: "declares more than one of file, section, and json",
		},
		"absolute to": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: /etc/f\n",
			},
			wantErr: `to: "/etc/f" is not a relative`,
		},
		"to with dotdot element": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills/../f\n",
			},
			wantErr: `to: "skills/../f" is not a relative`,
		},
		"to with backslash": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills\\a\\f\n",
			},
			wantErr: "contributes[0].to:",
		},
		"file contribution targets shared file": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: AGENTS.md\n",
			},
			wantErr: "a file contribution may not target a shared file",
		},
		"section contribution outside shared": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - section: s\n    to: NOTES.md\n",
			},
			wantErr: "section contributions may target only files listed in the kit's shared",
		},
		"json contribution outside shared": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - json: f\n    to: NOTES.md\n",
			},
			wantErr: "json contributions may target only files listed in the kit's shared",
		},
		"duplicate variable": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\nvars:\n  - {id: p}\n  - {id: p}\n",
			},
			wantErr: `"p" is declared twice`,
		},
		"source path with dotdot": {
			files: map[string]string{
				"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: ../f\n    to: skills/a/f\n",
			},
			wantErr: `file: "../f" is not a relative`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, validKit)
			writeFiles(t, dir, tc.files)

			_, err := Load(dir)
			if err == nil {
				t.Fatalf("Load succeeded, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestLoadSources checks how load treats contribution sources: directories are
// expanded into per-file contributions, everything else must be a readable file.
func TestLoadSources(t *testing.T) {
	kitWith := func(itemYAML string) map[string]string {
		return map[string]string{
			"kit.yaml":                  "name: example\n",
			"items/a/item.yaml":         itemYAML,
			"items/a/f":                 "content\n",
			"items/a/s":                 "section\n",
			"items/a/skill/SKILL.md":    "skill\n",
			"items/a/skill/lib/util.md": "util\n",
		}
	}

	t.Run("directory file source expands to one contribution per file", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, kitWith("id: a\nversion: 1.0.0\ncontributes:\n  - file: skill\n    to: skills/a\n    template: true\n"))
		k, err := Load(dir)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := []Contribution{
			{Kind: KindFile, Src: "skill/SKILL.md", To: "skills/a/SKILL.md", Template: true},
			{Kind: KindFile, Src: "skill/lib/util.md", To: "skills/a/lib/util.md", Template: true},
		}
		if diff := cmp.Diff(want, k.Items[0].Contributes); diff != "" {
			t.Errorf("contributions (-want +got):\n%s", diff)
		}
	})

	t.Run("missing source", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, kitWith("id: a\nversion: 1.0.0\ncontributes:\n  - file: gone\n    to: skills/a/f\n"))
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), `source "gone" cannot be read`) {
			t.Fatalf("Load error = %v", err)
		}
	})

	t.Run("section source may not be a directory", func(t *testing.T) {
		dir := t.TempDir()
		files := kitWith("id: a\nversion: 1.0.0\ncontributes:\n  - section: skill\n    to: AGENTS.md\n")
		files["kit.yaml"] = "name: example\nshared:\n  - AGENTS.md\n"
		writeFiles(t, dir, files)
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "only file contributions may use directories") {
			t.Fatalf("Load error = %v", err)
		}
	})

	t.Run("json source may not be a directory", func(t *testing.T) {
		dir := t.TempDir()
		files := kitWith("id: a\nversion: 1.0.0\ncontributes:\n  - json: skill\n    to: AGENTS.md\n")
		files["kit.yaml"] = "name: example\nshared:\n  - AGENTS.md\n"
		writeFiles(t, dir, files)
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "only file contributions may use directories") {
			t.Fatalf("Load error = %v", err)
		}
	})

	t.Run("empty directory source", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, kitWith("id: a\nversion: 1.0.0\ncontributes:\n  - file: empty\n    to: skills/a\n"))
		if err := os.MkdirAll(filepath.Join(dir, "items", "a", "empty"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "holds no files") {
			t.Fatalf("Load error = %v", err)
		}
	})
}

// TestLoadExecutable pins the declared executable flag: it parses on a file
// contribution, rides the directory expansion onto every file, and a section
// or json contribution carrying it is a broken declaration reported on the
// item's row.
func TestLoadExecutable(t *testing.T) {
	files := map[string]string{
		"kit.yaml":                  "name: example\nshared:\n  - AGENTS.md\n",
		"items/a/item.yaml":         "id: a\nversion: 1.0.0\ncontributes:\n  - file: peer-chat\n    to: skills/a/peer-chat\n    executable: true\n  - file: f\n    to: skills/a/f\n",
		"items/a/peer-chat":         "#!/bin/sh\n",
		"items/a/f":                 "content\n",
		"items/a/skill/SKILL.md":    "skill\n",
		"items/a/skill/lib/util.md": "util\n",
	}

	t.Run("flag parses on a file contribution", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, files)
		k, err := Load(dir)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := []Contribution{
			{Kind: KindFile, Src: "peer-chat", To: "skills/a/peer-chat", Executable: true},
			{Kind: KindFile, Src: "f", To: "skills/a/f"},
		}
		if diff := cmp.Diff(want, k.Items[0].Contributes); diff != "" {
			t.Errorf("contributions (-want +got):\n%s", diff)
		}
	})

	t.Run("directory source carries the flag on every file", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, files)
		writeFiles(t, dir, map[string]string{"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: skill\n    to: skills/a\n    executable: true\n"})
		k, err := Load(dir)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := []Contribution{
			{Kind: KindFile, Src: "skill/SKILL.md", To: "skills/a/SKILL.md", Executable: true},
			{Kind: KindFile, Src: "skill/lib/util.md", To: "skills/a/lib/util.md", Executable: true},
		}
		if diff := cmp.Diff(want, k.Items[0].Contributes); diff != "" {
			t.Errorf("contributions (-want +got):\n%s", diff)
		}
	})

	t.Run("flag on a section contribution is a broken declaration", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, files)
		writeFiles(t, dir, map[string]string{"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - section: f\n    to: AGENTS.md\n    executable: true\n"})
		writeFiles(t, dir, map[string]string{
			"items/b/item.yaml": "id: b\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills/b/f\n",
			"items/b/f":         "content\n",
		})
		k, problems, err := Read(dir)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if len(k.Items) != 2 {
			t.Fatalf("items = %d, want the broken one beside the healthy one", len(k.Items))
		}
		// Healthy items sort first; the broken one is found by its id.
		var a *Item
		for _, it := range k.Items {
			if it.ID == "a" {
				a = it
			}
		}
		if a == nil {
			t.Fatalf("items = %v, want the broken item among them", k.Items)
		}
		if len(a.Problems) != 1 || !strings.Contains(a.Problems[0].Error(), "contributes[0].executable: is only valid on a file contribution") {
			t.Errorf("a: problems = %v, want the flag problem on the row", a.Problems)
		}
		if len(a.Contributes) != 0 {
			t.Errorf("a: contributes = %v, want the contribution left out", a.Contributes)
		}
		if len(problems) != 1 {
			t.Errorf("problems = %v, want the one item problem", problems)
		}
	})

	t.Run("flag on a json contribution is a broken declaration", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, files)
		writeFiles(t, dir, map[string]string{"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - json: f\n    to: AGENTS.md\n    executable: true\n"})
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "contributes[0].executable: is only valid on a file contribution") {
			t.Fatalf("Load error = %v", err)
		}
	})
}

func TestReadSources(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"kit.yaml":          "name: example\nshared:\n  - AGENTS.md\n",
		"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills/a/f\n",
		"items/a/f":         "file content\n",
		"items/b/item.yaml": "id: b\nversion: 1.0.0\ncontributes:\n  - section: s\n    to: AGENTS.md\n",
		"items/b/s":         "section content\n",
	})
	k, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	sources, err := ReadSources([]*Kit{k}, []string{"example/b", "example/missing"})
	if err != nil {
		t.Fatalf("ReadSources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %d entries, want only the selected item's", len(sources))
	}
	b := k.Items[1]
	key := b.SourcePath(b.Contributes[0])
	if string(sources[key]) != "section content\n" {
		t.Errorf("sources[%s] = %q", key, sources[key])
	}
}

// TestReadSourcesSkipsBrokenItems pins the source index's half of the same
// protection: an item with problems never enters the index, so it is never
// read and cannot shadow a healthy item under a duplicated id.
func TestReadSourcesSkipsBrokenItems(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"kit.yaml":             "name: example\n",
		"items/a/item.yaml":    "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills/a/f\n",
		"items/a/f":            "healthy\n",
		"items/twin/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: g\n    to: skills/twin/g\n",
		"items/twin/g":         "broken\n",
	})
	k, _, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var healthy *Item
	for _, it := range k.Items {
		if len(it.Problems) == 0 {
			healthy = it
		}
	}
	if healthy == nil {
		t.Fatal("no healthy item in the kit")
	}

	sources, err := ReadSources([]*Kit{k}, []string{ItemKey(k.Name, healthy.ID)})
	if err != nil {
		t.Fatalf("ReadSources: %v", err)
	}
	key := healthy.SourcePath(healthy.Contributes[0])
	if string(sources[key]) != "healthy\n" {
		t.Errorf("sources[%s] = %q, want the healthy item's content", key, sources[key])
	}

	sources, err = ReadSources([]*Kit{k}, []string{"example/twin"})
	if err != nil {
		t.Fatalf("ReadSources: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("sources = %v, want nothing for a broken item's row key", sources)
	}
}

// writeFiles writes a map of slash-separated relative paths to file contents into
// dir; an empty content deletes the file, and directories left empty by a deletion
// are removed as well. Parent directories are created as needed.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if content == "" {
			if err := os.Remove(full); err != nil {
				t.Fatalf("remove %s: %v", path, err)
			}
			pruneEmptyDirs(t, dir, filepath.Dir(full))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

// pruneEmptyDirs removes the empty directories between full and root, so that a test
// case can make a whole subtree disappear by deleting its files.
func pruneEmptyDirs(t *testing.T, root, dir string) {
	t.Helper()
	for dir != root && strings.HasPrefix(dir, root) {
		if err := os.Remove(dir); err != nil {
			return // not empty or already gone: stop climbing
		}
		dir = filepath.Dir(dir)
	}
}

// TestReadTolerant checks the tolerant read: a broken item is reported
// beside the kit and does not fail it, while a kit.yaml problem stays
// fatal. Load keeps the strict contract on top of it.
func TestReadTolerant(t *testing.T) {
	valid := map[string]string{
		"kit.yaml":          "name: example\ntarget_hint: ~/t\n",
		"items/a/item.yaml": "id: a\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills/a/f\n",
		"items/a/f":         "content\n",
	}

	t.Run("broken item reported beside the kit", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		writeFiles(t, dir, map[string]string{"items/b/item.yaml": "id: b\nversion: nope\n"})
		k, problems, err := Read(dir)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if len(k.Items) != 2 {
			t.Fatalf("items = %d, want the healthy one and the broken one", len(k.Items))
		}
		a, b := k.Items[0], k.Items[1]
		if a.ID != "a" || len(a.Problems) != 0 {
			t.Errorf("a: id = %q, problems = %v, want healthy and first", a.ID, a.Problems)
		}
		if b.ID != "b" || b.RowID != "b" || len(b.Problems) != 1 || !strings.Contains(b.Problems[0].Error(), `"nope" is not a semantic version`) {
			t.Errorf("b: id = %q, row id = %q, problems = %v", b.ID, b.RowID, b.Problems)
		}
		if len(problems) != 1 || !strings.Contains(problems[0].Error(), `"nope" is not a semantic version`) {
			t.Errorf("problems = %v, want the broken item's problem", problems)
		}
		_, err = Load(dir)
		if err == nil || !strings.Contains(err.Error(), `"nope" is not a semantic version`) {
			t.Errorf("Load error = %v, want the strict refusal", err)
		}
	})

	t.Run("kit.yaml problems stay fatal", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		writeFiles(t, dir, map[string]string{
			"kit.yaml":          "name: Bad Name\n",
			"items/b/item.yaml": "id: b\nversion: nope\n",
		})
		k, problems, err := Read(dir)
		if err == nil || !strings.Contains(err.Error(), "name: must match") {
			t.Fatalf("Read error = %v, want the kit.yaml problem", err)
		}
		if k != nil {
			t.Errorf("Read returned a kit for a fatal problem: %+v", k)
		}
		if len(problems) != 1 {
			t.Errorf("problems = %v, want the broken item beside the fatal error", problems)
		}
	})

	t.Run("parse error carries the position", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		writeFiles(t, dir, map[string]string{"kit.yaml": "name: example\nshared: oops\n"})
		_, problems, err := Read(dir)
		if err == nil || !strings.Contains(err.Error(), "kit.yaml:2") {
			t.Fatalf("Read error = %v, want the parser position", err)
		}
		if len(problems) != 0 {
			t.Errorf("problems = %v, want none: the parser stopped before the items", problems)
		}
	})

	t.Run("item without item.yaml reported by its directory", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		if err := os.MkdirAll(filepath.Join(dir, "items", "empty"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, problems, err := Read(dir)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0].Error(), "items/empty/item.yaml: cannot be read") {
			t.Errorf("problems = %v, want the missing item.yaml", problems)
		}
	})

	t.Run("healthy items keep the id sort, broken ones follow", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		writeFiles(t, dir, map[string]string{
			"items/zeta/item.yaml":   "id: zeta\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills/zeta/f\n",
			"items/zeta/f":           "content\n",
			"items/broken/item.yaml": "id: broken\nversion: nope\n",
		})
		k, _, err := Read(dir)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		var got []string
		for _, it := range k.Items {
			got = append(got, it.RowID)
		}
		if diff := cmp.Diff([]string{"a", "zeta", "broken"}, got); diff != "" {
			t.Errorf("row ids (-want +got):\n%s", diff)
		}
	})

	t.Run("an item whose id is unusable keys by its directory", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		writeFiles(t, dir, map[string]string{
			"items/no-id/item.yaml":  "version: 1.0.0\n",
			"items/bad-id/item.yaml": "id: No!\nversion: 1.0.0\n",
		})
		k, problems, err := Read(dir)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if len(problems) != 2 {
			t.Fatalf("problems = %v, want one per broken item", problems)
		}
		rowIDs := map[string]bool{}
		for _, it := range k.Items {
			if len(it.Problems) == 0 {
				continue
			}
			rowIDs[it.RowID] = true
		}
		if !rowIDs["no-id"] || !rowIDs["bad-id"] {
			t.Errorf("row ids of the broken items = %v, want the directory names", rowIDs)
		}
	})

	t.Run("a duplicated id keys the second item by its directory", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		writeFiles(t, dir, map[string]string{"items/twin/item.yaml": "id: a\nversion: 1.0.0\n"})
		k, problems, err := Read(dir)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0].Error(), `"a" is already used`) {
			t.Errorf("problems = %v, want the duplicate id", problems)
		}
		if k.Items[0].ID != "a" || k.Items[0].RowID != "a" || len(k.Items[0].Problems) != 0 {
			t.Errorf("first: id = %q, row id = %q, problems = %v, want healthy owner of the id", k.Items[0].ID, k.Items[0].RowID, k.Items[0].Problems)
		}
		if k.Items[1].RowID != "twin" || len(k.Items[1].Problems) != 1 {
			t.Errorf("second: row id = %q, problems = %v, want keyed by its directory", k.Items[1].RowID, k.Items[1].Problems)
		}
	})

	t.Run("a directory name that collides with a row token is made unique", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		writeFiles(t, dir, map[string]string{
			"items/x/item.yaml":    "version: 1.0.0\n",
			"items/xdir/item.yaml": "id: x\nversion: 1.0.0\ncontributes:\n  - file: f\n    to: skills/x/f\n",
			"items/xdir/f":         "content\n",
		})
		k, _, err := Read(dir)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		var got []string
		for _, it := range k.Items {
			got = append(got, it.RowID)
		}
		if diff := cmp.Diff([]string{"a", "x", "x~"}, got); diff != "" {
			t.Errorf("row ids (-want +got):\n%s", diff)
		}
	})

	t.Run("a kit whose items are all broken reads as one without items", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, valid)
		writeFiles(t, dir, map[string]string{
			"items/a/item.yaml": "",
			"items/a/f":         "",
			"items/b/item.yaml": "id: b\nversion: nope\n",
		})
		k, problems, err := Read(dir)
		if err == nil || !strings.Contains(err.Error(), "no items found") {
			t.Fatalf("Read error = %v, want the no-items refusal", err)
		}
		if k != nil {
			t.Errorf("Read returned a kit: %+v", k)
		}
		if len(problems) != 1 {
			t.Errorf("problems = %v, want the broken item beside the refusal", problems)
		}
	})
}
