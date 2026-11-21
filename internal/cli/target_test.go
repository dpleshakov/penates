package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The parity scenarios of the target management commands: register,
// rename, forget — the registry alone, nothing asked, the last active
// target never written.

// TestTargetAddRegisters pins the registration: the target appears in
// the list, unattached, and the last active target stays untouched.
func TestTargetAddRegisters(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")

	stdout, stderr, code := execute(t, testBuild, "target", "add", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "target " + target + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	reg := registryOf(e)
	if _, ok := reg.Targets[target]; !ok {
		t.Error("the target was not registered")
	}
	if reg.LastTarget != "" {
		t.Errorf("the CLI wrote the last active target: %q", reg.LastTarget)
	}
}

// TestTargetRenameSetsAndResetsTheName pins the display name: an empty
// name resets it, and the path stays the identity the command names.
func TestTargetRenameSetsAndResetsTheName(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	if err := e.states.RegisterTarget(target); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := execute(t, testBuild, "target", "rename", target, "harness-config")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "target " + target + "  name harness-config\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	// The list shows the name.
	stdout, _, _ = execute(t, testBuild, "target", "list")
	if !strings.Contains(stdout, "name harness-config") {
		t.Errorf("target list lacks the name:\n%s", stdout)
	}

	// An empty name resets.
	stdout, _, code = execute(t, testBuild, "target", "rename", target, "")
	if code != 0 {
		t.Fatalf("reset exit code = %d, want 0", code)
	}
	if want := "target " + target + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	stdout, _, _ = execute(t, testBuild, "target", "list")
	if strings.Contains(stdout, "name ") {
		t.Errorf("target list still shows a name:\n%s", stdout)
	}
}

// TestTargetRenameUnregisteredIsNotFound: the path must be registered.
func TestTargetRenameUnregisteredIsNotFound(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")

	_, stderr, code := execute(t, testBuild, "target", "rename", target, "name")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	want := fmt.Sprintf("Error [not_found]: target %s is not registered\n", target)
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestManagementCommandsTakeNoYes pins the management rule: the
// management commands never ask, --yes is not defined for them, and
// passing it is a wrong command line.
func TestManagementCommandsTakeNoYes(t *testing.T) {
	e := newTestEnv(t)
	target := filepath.Join(e.base, "harness")
	if err := e.states.RegisterTarget(target); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"target", "add", target, "--yes"},
		{"target", "rename", target, "n", "--yes"},
		{"target", "forget", target, "--yes"},
	} {
		_, stderr, code := execute(t, testBuild, args...)
		if code != 2 {
			t.Errorf("%v: exit code = %d, want 2 (stderr: %s)", args, code, stderr)
		}
		if !strings.Contains(stderr, "Error [usage]: unknown flag: --yes") {
			t.Errorf("%v: stderr = %q, want the unknown-flag refusal", args, stderr)
		}
	}
}
