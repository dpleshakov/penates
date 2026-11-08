package licenses

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// fixed stands in for the licenses every build carries.
var fixed = []license{
	{name: "penates", text: "OWN LICENSE\n"},
	{name: "Go standard library and runtime", text: "GO LICENSE\n"},
}

func TestTextWithSet(t *testing.T) {
	set := fstest.MapFS{
		"github.com/charmbracelet/bubbletea/LICENSE":      {Data: []byte("MIT License\n")},
		"golang.org/x/mod/LICENSE":                        {Data: []byte("Copyright 2009 The Go Authors.\n")},
		"github.com/example/notice/LICENSE":               {Data: []byte("MIT License\n")},
		"github.com/example/notice/NOTICE":                {Data: []byte("Notice text\n")},
		"github.com/charmbracelet/x/cellbuf/LICENSE":      {Data: []byte("MIT License\n\nCopyright (c) 2024\n")},
		"github.com/charmbracelet/x/cellbuf/sub/LICENSE":  {Data: []byte("Nested\n")},
		"github.com/charmbracelet/x/cellbuf/LICENSE.more": {Data: []byte("More\n")},
	}

	got := text(fixed, set)

	wantList := "The penates binary contains the following components:\n\n" +
		"  penates\n" +
		"  Go standard library and runtime\n" +
		"  github.com/charmbracelet/bubbletea\n" +
		"  github.com/charmbracelet/x/cellbuf\n" +
		"  github.com/charmbracelet/x/cellbuf/sub\n" +
		"  github.com/example/notice\n" +
		"  golang.org/x/mod\n"
	if !strings.HasPrefix(got, wantList) {
		t.Errorf("text() does not start with the component list:\n%s", got)
	}
	if strings.Contains(got, "only in the release builds") {
		t.Errorf("text() with a set says the set is missing:\n%s", got)
	}

	framed := func(name, body string) string {
		return "\n" + separator + "\n" + name + "\n" + separator + "\n\n" + body
	}
	for _, want := range []string{
		framed("penates", "OWN LICENSE\n"),
		framed("Go standard library and runtime", "GO LICENSE\n"),
		framed("github.com/charmbracelet/bubbletea", "MIT License\n"),
		framed("github.com/example/notice", "MIT License\n\nNotice text\n"),
		framed("github.com/charmbracelet/x/cellbuf", "MIT License\n\nCopyright (c) 2024\n\nMore\n"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("text() lacks %q:\n%s", want, got)
		}
	}
	own := strings.Index(got, framed("penates", ""))
	goLic := strings.Index(got, framed("Go standard library and runtime", ""))
	cellbuf := strings.Index(got, framed("github.com/charmbracelet/x/cellbuf", ""))
	if own > goLic || goLic > cellbuf {
		t.Errorf("text() does not put penates, then Go, before the third-party licenses:\n%s", got)
	}
}

func TestTextWithoutSet(t *testing.T) {
	for name, set := range map[string]fs.FS{
		"nil":     nil,
		"empty":   fstest.MapFS{},
		"missing": sub(t, fstest.MapFS{"PLACEHOLDER": {}}, "third-party"),
	} {
		t.Run(name, func(t *testing.T) {
			got := text(fixed, set)
			want := "The penates binary contains the following components:\n\n" +
				"  penates\n" +
				"  Go standard library and runtime\n\n" +
				"This build does not carry the licenses of the third-party modules it\n" +
				"includes: they are embedded only in the release builds, available at\n" +
				"https://github.com/dpleshakov/penates/releases.\n" +
				"\n" + separator + "\npenates\n" + separator + "\n\n" +
				"OWN LICENSE\n" +
				"\n" + separator + "\nGo standard library and runtime\n" + separator + "\n\n" +
				"GO LICENSE\n"
			if got != want {
				t.Errorf("text() =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// sub is fs.Sub that fails the test on an error, which only an invalid dir
// can cause.
func sub(t *testing.T, fsys fs.FS, dir string) fs.FS {
	t.Helper()
	s, err := fs.Sub(fsys, dir)
	if err != nil {
		t.Fatalf("fs.Sub(%q): %v", dir, err)
	}
	return s
}

// TestTextOfThisBuild covers Text over the embedded set of the platform the
// tests run on. The set is usually absent, but a local goreleaser snapshot
// leaves one behind until make clean, so the test holds either way.
func TestTextOfThisBuild(t *testing.T) {
	got := Text()
	if want := text(embedded(), sub(t, platformFS, platformDir+"/third-party")); got != want {
		t.Errorf("Text() =\n%s\nwant\n%s", got, want)
	}
	for name, lic := range map[string]string{"penates": ownLicense, "Go": goLicense} {
		if !strings.Contains(got, lic) {
			t.Errorf("Text() lacks the %s license:\n%s", name, got)
		}
	}
}

// TestGoLicenseMatchesToolchain keeps the committed copy of the Go license
// equal to the one of the toolchain that builds penates.
func TestGoLicenseMatchesToolchain(t *testing.T) {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	want, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "LICENSE"))
	if err != nil {
		t.Fatalf("reading the toolchain license: %v", err)
	}
	if goLicense != string(want) {
		t.Errorf("internal/licenses/go.LICENSE differs from LICENSE of the Go toolchain; " +
			"copy $(go env GOROOT)/LICENSE over it")
	}
}

// TestOwnLicenseMatchesTheRepository keeps the embedded copy of the penates
// license equal to LICENSE at the root of the repository.
func TestOwnLicenseMatchesTheRepository(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "LICENSE"))
	if err != nil {
		t.Fatalf("reading LICENSE: %v", err)
	}
	if ownLicense != string(want) {
		t.Errorf("internal/licenses/penates.LICENSE differs from LICENSE; copy LICENSE over it")
	}
}
