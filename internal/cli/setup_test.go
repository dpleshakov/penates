package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpleshakov/penates/internal/state"
)

// setup runs "penates setup --target <target>" with the given flags and
// returns what the process would have written and exited with.
func runSetup(t *testing.T, target string, flags ...string) (stdout, stderr string, code int) {
	t.Helper()
	args := append([]string{"setup", "--target", target}, flags...)
	return execute(t, testBuild, args...)
}

// TestSetupBootstrapsAFreshTarget pins the first run's CLI form: on a
// virgin machine setup registers the target, attaches the built-in kit
// to it, and installs the kit's skills — the product's own
// documentation — through the common mutating path: summary, one
// confirmation answered by --yes, result.
func TestSetupBootstrapsAFreshTarget(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")

	stdout, stderr, code := runSetup(t, target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	for _, want := range []string{
		"target " + target,
		"install  penates/kit-authoring",
		"install  penates/operating",
		"install  penates/reference",
		"3 writes",
		"3 applied",
		"note  attached the built-in kit to " + target,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	for _, skill := range []string{
		"skills/penates-kit-authoring/SKILL.md",
		"skills/penates-operating/SKILL.md",
		"skills/penates/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(skill))); err != nil {
			t.Errorf("the skill did not land: %v", err)
		}
	}
	reg := registryOf(e)
	if _, ok := reg.Targets[target]; !ok {
		t.Error("the target was not registered")
	}
	if names := reg.KitsFor(target); len(names) != 1 || names[0] != state.BuiltinKitName {
		t.Errorf("kits of the target = %v, want the built-in kit", names)
	}
	lk, err := e.locks.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(lk.Items); n != 3 {
		t.Errorf("installed items = %d, want the built-in kit's three skills", n)
	}
	if reg.LastTarget != "" {
		t.Errorf("the CLI wrote the last active target: %q", reg.LastTarget)
	}
}

// TestSetupIsIdempotent pins the repeat: the attachment already there
// and the items already installed are kept and reported, not errors —
// exit 0, nothing asked beyond the one confirmation, nothing broken.
func TestSetupIsIdempotent(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	if _, stderr, code := runSetup(t, target, "--yes"); code != 0 {
		t.Fatalf("the first setup exited %d (stderr: %s)", code, stderr)
	}

	stdout, stderr, code := runSetup(t, target, "--yes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	for _, want := range []string{
		"  = skills/penates/SKILL.md  kept: already installed",
		"0 writes",
		"3 kept",
		"note  the built-in kit is already attached to " + target,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "installed  penates/") {
		t.Errorf("stdout reports a fresh install on a repeat:\n%s", stdout)
	}
}

// TestSetupRefusesStagedPendingBeforeItAttaches: a one-shot refusal
// comes before the attachment — a target that holds staged pending
// actions is left as it stands, the built-in kit unattached.
func TestSetupRefusesStagedPendingBeforeItAttaches(t *testing.T) {
	e := newTestEnv(t)
	dir := e.mutateKit("personal")
	target := filepath.Join(e.base, "harness")
	e.register("personal", dir)
	e.attach("personal", target)
	stage(e, target, &state.Pending{
		Items: map[string]*state.ItemPending{
			"personal/writing": {Select: true, Version: "1.0.0"},
		},
	})

	_, stderr, code := runSetup(t, target, "--yes")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, "Error [staged]: ") {
		t.Errorf("stderr = %q, want a staged refusal", stderr)
	}
	if isAttached(registryOf(e), state.BuiltinKitName, target) {
		t.Error("the setup attached the built-in kit despite the staged refusal")
	}
}

// TestSetupUnreadableBuiltinIsKitError: the built-in kit installs from
// its registered directory, and a directory that cannot be read is the
// kit's fault — the kit category, not a crash.
func TestSetupUnreadableBuiltinIsKitError(t *testing.T) {
	e := newTestEnv(t)
	// The first command unpacks the built-in kit; the second finds it
	// modified with a stamp it does not know: the sync stands down and
	// the directory stays as the user left it — without kit.yaml.
	if _, _, code := execute(t, testBuild, "kit", "list"); code != 0 {
		t.Fatalf("kit list exited %d", code)
	}
	kitYaml := filepath.Join(e.dataDir, "builtin-kit", "kit.yaml")
	data, err := os.ReadFile(kitYaml)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kitYaml, append(data, []byte("edited locally\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := strings.Repeat("b", 64)
	if err := os.WriteFile(filepath.Join(e.dataDir, "builtin-kit.sha256"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(kitYaml); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(e.base, "harness")

	_, stderr, code := runSetup(t, target, "--yes")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Error [kit]: ") || !strings.Contains(stderr, "kit.yaml") {
		t.Errorf("stderr = %q, want the kit error naming kit.yaml", stderr)
	}
}

// TestSetupDryRunWritesNothing: the summary is the whole preview — the
// target stays unregistered, the kit unattached, nothing installed.
func TestSetupDryRunWritesNothing(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")

	stdout, stderr, code := runSetup(t, target, "--dry-run")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if !strings.Contains(stdout, "install  penates/reference") || !strings.Contains(stdout, "3 writes") {
		t.Errorf("the dry-run printed no summary:\n%s", stdout)
	}
	if strings.Contains(stdout, "attached the built-in kit") {
		t.Errorf("the dry-run reports an attachment:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(target, "skills")); !os.IsNotExist(err) {
		t.Errorf("the dry-run wrote the target: %v", err)
	}
	reg := registryOf(e)
	if _, ok := reg.Targets[target]; ok {
		t.Error("the dry-run registered the target")
	}
	lk, err := e.locks.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(lk.Items) != 0 {
		t.Errorf("the dry-run wrote the lock: %+v", lk.Items)
	}
}
