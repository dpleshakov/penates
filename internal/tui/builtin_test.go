package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/muesli/termenv"

	"github.com/dpleshakov/penates/internal/builtin"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/state"
)

// The built-in kit in the interface (concept 6.2, 10.1): the first-run
// offer, the Attach kit command, the modified kit row, and the reset.

// writeBuiltinKit writes a loadable kit named penates proposing target as
// its target hint, the way the startup sync registers it.
func writeBuiltinKit(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	return writeKitFiles(t, dir, map[string]string{
		"kit.yaml":                      "name: penates\ndescription: Documentation\ntarget_hint: " + target + "\n",
		"items/reference/item.yaml":     "id: reference\nversion: 0.1.0\ndescription: Reference\ncontributes:\n  - file: SKILL.md\n    to: skills/penates/SKILL.md\n",
		"items/reference/SKILL.md":      "# penates\n",
		"items/kit-authoring/item.yaml": "id: kit-authoring\nversion: 0.1.0\ndescription: Authoring\ncontributes:\n  - file: SKILL.md\n    to: skills/penates-kit-authoring/SKILL.md\n",
		"items/kit-authoring/SKILL.md":  "# authoring\n",
	})
}

func TestStartupNoticesRender(t *testing.T) {
	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))
	app.Startup = &builtin.Report{Notices: []string{"built-in kit unpacked", "built-in kit not registered: name taken"}}

	lm := resize(t, app.Initial(), 120, 30).(*model)

	if !lm.notice.Visible() {
		t.Fatal("the startup report did not reach the notice line")
	}
	if got := lm.notice.Text(); !strings.Contains(got, "built-in kit unpacked") || !strings.Contains(got, "built-in kit not registered") {
		t.Errorf("notice = %q, want both startup notices", got)
	}
}

func TestFirstRunOffersBuiltinKit(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeBuiltinKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RegisterBuiltinKit(kitDir); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	lm := resize(t, app.Initial(), 120, 30).(*model)

	// The empty tree holds the offer row above the add-kit row, and the
	// body names the built-in kit instead of claiming nothing registered.
	if len(lm.rows) != 2 || lm.rows[0].Kind != KindAttachBuiltin || lm.rows[1].Kind != KindAddKit {
		t.Fatalf("rows = %v, want the built-in offer above the add-kit row", lm.rows)
	}
	if !strings.Contains(strings.Join(lm.firstRunLines(120), "\n"), "ships its own documentation") {
		t.Fatalf("first-run lines = %v", lm.firstRunLines(120))
	}
	view := lm.View()
	if !strings.Contains(view, AttachBuiltinLabel) {
		t.Errorf("the view misses the offer row:\n%s", view)
	}
	if !strings.Contains(lm.footer(120), "enter attach") {
		t.Errorf("footer = %q, want enter attach on the offer row", lm.footer(120))
	}

	// Enter on the offer row opens the target proposal; choosing the first
	// option registers the target_hint and attaches there.
	lm = drive(t, lm, "enter").(*model)
	if lm.dialog == nil || lm.dialog.kind != dialogAttach || !strings.Contains(lm.dialog.title, "penates") {
		t.Fatalf("dialog = %+v, want the attach proposal of the built-in kit", lm.dialog)
	}
	lm = drive(t, lm, "enter").(*model)
	if got := lm.notice.Text(); !strings.Contains(got, "Attached penates") {
		t.Fatalf("attach notice = %q", got)
	}
	reg, err := st.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if names := reg.KitsFor(target); len(names) != 1 || names[0] != state.BuiltinKitName {
		t.Fatalf("kits of the target = %v, want the built-in kit", names)
	}
}

func TestAttachKitCommand(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writePlainKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterTarget(target); err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterKit("ex", kitDir); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, lock.NewStore(t.TempDir()))

	lm := resize(t, app.Initial(), 120, 30).(*model)

	// The command asks which registered kit to attach: the picker holds
	// the unattached one, enter attaches it to the current target.
	lm = runPalette(t, lm, 1).(*model)
	if lm.kitPick == nil || len(lm.kitPick.kits) != 1 || lm.kitPick.kits[0] != "ex" {
		t.Fatalf("picker = %+v, want the kit ex", lm.kitPick)
	}
	lm = drive(t, lm, "enter").(*model)
	if got := lm.notice.Text(); got != "Attached ex at "+displayPath(target)+"." {
		t.Fatalf("attach notice = %q", got)
	}
	reg, err := st.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(reg.KitsFor(target)); n != 1 {
		t.Fatalf("attachments after the attach = %d", n)
	}

	// With every registered kit attached the question answers itself.
	lm = runPalette(t, lm, 1).(*model)
	if lm.kitPick != nil {
		t.Fatal("a fully attached target opened a picker")
	}
	if got := lm.notice.Text(); got != "Every registered kit is attached to this target." {
		t.Fatalf("full notice = %q", got)
	}
}

func TestBuiltinKitRowShowsModification(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	kitDir := writeBuiltinKit(t, target)

	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerKitAt(t, st, state.BuiltinKitName, kitDir, target)
	reg, err := st.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := kit.Load(kitDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		report  builtin.Report
		wantRow string
	}{
		{"conflict", builtin.Report{Modified: true, Conflict: true}, "modified locally — update blocked"},
		{"modified", builtin.Report{Modified: true}, "modified locally"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := BuildTree(TreeInput{
				Target:   target,
				Registry: reg,
				Kits:     []*kit.Kit{loaded},
				Builtin:  &tc.report,
			})
			if len(rows) == 0 || rows[0].Kind != KindKit || rows[0].Kit.Name != state.BuiltinKitName {
				t.Fatalf("rows = %v, want the built-in kit group", rows)
			}
			g := rows[0].Kit
			if !g.Modified || g.UpdateBlocked != tc.report.Conflict {
				t.Errorf("kit row = %+v, want the modification carried", g)
			}
			forceVisual(t, termenv.Ascii, true)
			if got := renderKitRow(g, SetFromEnv(func(string) string { return "" })); !strings.Contains(got, tc.wantRow) {
				t.Errorf("kit row = %q, want %q", got, tc.wantRow)
			}
		})
	}

	// A quiet report leaves the row to its summary.
	rows := BuildTree(TreeInput{Target: target, Registry: reg, Kits: []*kit.Kit{loaded}})
	if rows[0].Kit.Modified {
		t.Error("a quiet sync marked the kit row as modified")
	}
}

func TestResetBuiltinKitFlow(t *testing.T) {
	dataDir := t.TempDir()
	st, err := state.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	src := fstest.MapFS{
		"kit.yaml":                  &fstest.MapFile{Data: []byte("name: penates\n")},
		"items/reference/item.yaml": &fstest.MapFile{Data: []byte("id: reference\n")},
		"items/reference/SKILL.md":  &fstest.MapFile{Data: []byte("embedded\n")},
	}
	if _, err := builtin.Sync(dataDir, st, src); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(builtin.KitDir(dataDir), "items/reference/SKILL.md")
	if err := os.WriteFile(edited, []byte("my local edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := builtin.Sync(dataDir, st, src)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Modified {
		t.Fatal("the second sync did not report the modification")
	}

	app := NewApp(st, lock.NewStore(dataDir))
	app.Startup = &report
	app.BuiltinFS = src
	lm := resize(t, app.Initial(), 120, 30).(*model)

	// The reset asks first: the dialog names exactly what it discards.
	lm = runPalette(t, lm, 4).(*model)
	if lm.dialog == nil || lm.dialog.kind != dialogResetBuiltin {
		t.Fatalf("dialog = %+v, want the reset confirmation", lm.dialog)
	}
	if !strings.Contains(strings.Join(lm.dialog.body, " "), "local edits") {
		t.Errorf("dialog body = %v", lm.dialog.body)
	}

	// Enter confirms: the embedded content is back, the report clears.
	lm = drive(t, lm, "enter").(*model)
	data, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "embedded\n" {
		t.Errorf("SKILL.md = %q, want the embedded content after the reset", data)
	}
	if got := lm.notice.Text(); !strings.Contains(got, "reset") {
		t.Errorf("reset notice = %q", got)
	}
	if lm.startup.Modified {
		t.Error("the report still reports the modification after the reset")
	}

	// With nothing modified the command says so instead of asking.
	lm = runPalette(t, lm, 4).(*model)
	if lm.dialog != nil {
		t.Error("an unmodified built-in kit opened the reset dialog")
	}
	if got := lm.notice.Text(); got != "The built-in kit is not modified." {
		t.Errorf("unmodified notice = %q", got)
	}
}
