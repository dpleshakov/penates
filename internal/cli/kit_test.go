package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The parity scenarios of the kit management commands: the CLI registers
// freely — on a virgin registry included; `penates setup` is the first
// run's CLI form, tested in setup_test.go. The management commands never
// ask and never touch which target the terminal UI opens; detach and
// forget are the immediate variants.

// TestKitAddOnAnEmptyRegistry pins the first half of parity scenario 1:
// on a virgin machine kit add registers the kit and attaches it to the
// --target, registering the target — the guard the refusal once raised
// is gone, and `penates setup` is the agent's other way to begin.
func TestKitAddOnAnEmptyRegistry(t *testing.T) {
	e := newTestEnv(t)
	dir := e.mutateKit("personal")
	target := filepath.Join(e.base, "harness")

	stdout, stderr, code := execute(t, testBuild, "kit", "add", dir, "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"kit personal  " + dir,
		"  attached  " + target,
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	reg := registryOf(e)
	if _, ok := reg.Kits["personal"]; !ok {
		t.Error("the kit was not registered")
	}
	if _, ok := reg.Targets[target]; !ok {
		t.Error("the target was not registered")
	}
}

// TestKitAddRegistersAndAttaches pins parity scenario 12: the kit is
// registered under the name its kit.yaml carries and attached to the
// target the --target names, and the last active target stays the CLI's
// to never write — or read.
func TestKitAddRegistersAndAttaches(t *testing.T) {
	e := newTestEnv(t)
	dir := e.mutateKit("personal")
	target := filepath.Join(e.base, "harness")

	stdout, stderr, code := execute(t, testBuild, "kit", "add", dir, "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		"kit personal  " + dir,
		"  attached  " + target,
		"",
	}, "\n")
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	reg := registryOf(e)
	if _, ok := reg.Kits["personal"]; !ok {
		t.Error("the kit was not registered")
	}
	if _, ok := personalAttachment(e, target); !ok {
		t.Error("the kit was not attached to the target")
	}
	if reg.LastTarget != "" {
		t.Errorf("the CLI wrote the last active target: %q", reg.LastTarget)
	}
}

// TestKitAddUnreadablePathIsKitError: the kit content is to blame, and
// the refusal names the file that could not be read.
func TestKitAddUnreadablePathIsKitError(t *testing.T) {
	e := newTestEnv(t)
	missing := filepath.Join(e.base, "nowhere")

	_, stderr, code := execute(t, testBuild, "kit", "add", missing, "--target", filepath.Join(e.base, "harness"))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Error [kit]: ") || !strings.Contains(stderr, "kit.yaml") {
		t.Errorf("stderr = %q, want the kit error naming kit.yaml", stderr)
	}
}

// TestKitAttachRegistersNewTargetAndRefusesRepeat: attach registers an
// unregistered target, and an attachment the registry already holds is
// refused as a conflict.
func TestKitAttachRegistersNewTargetAndRefusesRepeat(t *testing.T) {
	e := newTestEnv(t)
	dir := e.mutateKit("personal")
	e.register("personal", dir)
	target := filepath.Join(e.base, "harness")

	stdout, stderr, code := execute(t, testBuild, "kit", "attach", "personal", "--target", target)
	if code != 0 {
		t.Fatalf("attach exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := fmt.Sprintf("attached  personal  %s\n", target); stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	reg := registryOf(e)
	if _, ok := reg.Targets[target]; !ok {
		t.Error("attach did not register the new target")
	}
	if reg.LastTarget != "" {
		t.Errorf("the CLI wrote the last active target: %q", reg.LastTarget)
	}

	_, stderr, code = execute(t, testBuild, "kit", "attach", "personal", "--target", target)
	if code != 1 {
		t.Fatalf("repeat exit code = %d, want 1", code)
	}
	want := fmt.Sprintf("Error [conflict]: kit \"personal\" is already attached to %s\n", target)
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestKitAttachUnregisteredKitIsNotFound: a kit the registry does not
// hold cannot be attached.
func TestKitAttachUnregisteredKitIsNotFound(t *testing.T) {
	e := newTestEnv(t)

	_, stderr, code := execute(t, testBuild, "kit", "attach", "personal", "--target", filepath.Join(e.base, "harness"))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	want := "Error [not_found]: kit \"personal\" is not registered\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestKitDetachListsInstalled pins parity scenario 11: the immediate
// detach removes the attachment at once, the installed items stay
// installed and owned, and the report lists them for remove.
func TestKitDetachListsInstalled(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}

	stdout, stderr, code := execute(t, testBuild, "kit", "detach", "personal", "--target", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := strings.Join([]string{
		fmt.Sprintf("detached  personal  %s", target),
		"",
		"installed items remain owned (remove them with \"penates remove <kit/item>\"):",
		"  personal/writing  1.0.0",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if _, attached := personalAttachment(e, target); attached {
		t.Error("the attachment survived the detach")
	}
	// The installed item stays installed and owned: status shows it as an
	// orphan of the trailing group.
	stdout, _, _ = execute(t, testBuild, "status", "--target", target)
	if !strings.Contains(stdout, "orphan  personal/writing  1.0.0") {
		t.Errorf("status lacks the orphaned item:\n%s", stdout)
	}
}

// TestKitDetachNotAttachedIsNotFound: an attachment that does not exist
// cannot be removed.
func TestKitDetachNotAttachedIsNotFound(t *testing.T) {
	e := newTestEnv(t)
	e.readyKit("personal")
	other := filepath.Join(e.base, "other")
	if err := e.states.RegisterTarget(other); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := execute(t, testBuild, "kit", "detach", "personal", "--target", other)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	want := fmt.Sprintf("Error [not_found]: kit \"personal\" is not attached to %s\n", other)
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestKitPath prints the registered path on its own line — the working
// round of editing a kit where it lives.
func TestKitPath(t *testing.T) {
	e := newTestEnv(t)
	dir := e.mutateKit("personal")
	target := filepath.Join(e.base, "harness")
	e.register("personal", dir)
	e.attach("personal", target)

	stdout, stderr, code := execute(t, testBuild, "kit", "path", "personal")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := dir + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}

	_, stderr, code = execute(t, testBuild, "kit", "path", "absent")
	if code != 1 {
		t.Fatalf("unregistered exit code = %d, want 1", code)
	}
	want := "Error [not_found]: kit \"absent\" is not registered (run \"penates kit list\" to see what is)\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestTargetForgetIsImmediate pins the forget half of parity scenario
// 11: the lock is deleted at once, the files in the target are left
// alone, and a target the registry does not hold is a not_found.
func TestTargetForgetIsImmediate(t *testing.T) {
	e := newTestEnv(t)
	_, target := e.readyKit("personal")
	if _, _, code := execute(t, testBuild, "install", "personal/writing", "--target", target, "--yes"); code != 0 {
		t.Fatalf("the install exited %d", code)
	}
	installed := filepath.Join(target, "skills/writing/SKILL.md")

	stdout, stderr, code := execute(t, testBuild, "target", "forget", target)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "forgotten  " + target + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("the forget touched the files: %v", err)
	}
	lk, err := e.locks.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(lk.Items) != 0 {
		t.Errorf("the lock survived the forget: %+v", lk.Items)
	}
	reg := registryOf(e)
	if len(reg.Targets) != 0 {
		t.Errorf("the target survived the forget: %+v", reg.Targets)
	}

	_, stderr, code = execute(t, testBuild, "target", "forget", target)
	if code != 1 {
		t.Fatalf("unregistered exit code = %d, want 1", code)
	}
	want := fmt.Sprintf("Error [not_found]: target %s is not registered\n", target)
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
