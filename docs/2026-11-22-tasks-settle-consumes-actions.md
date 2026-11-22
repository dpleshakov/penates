# 2026-11-22-tasks-settle-consumes-actions.md

**Status:** Active

## Context

The backlog entry `applied-selection-stays-staged` (recorded in 17608b1) stated the
problem: the apply that runs a selection keeps it staged forever, which traps the TUI
in a permanent restore offer and, since 0.1.2, deadlocks the CLI's staged refusal
against a satisfied selection. The fix was agreed with the user and reviewed by the
peer session on 2026-11-22:

- One consumption rule for all three item actions. An apply consumes `Select` (with
  its `Answers`) and `Reset` only when the item's outcome satisfies the intent: the
  step landed or the plan kept the item (`NoAction`). A failed step, an item the plan
  excluded, and an item that never reached the plan keep their actions staged and
  repeatable. `Remove` stays as it is: consumed on success only — both callers
  already fill `removed` per success.
- The settle input becomes per-item outcomes. `SettlePending`
  (internal/state/operations.go) receives the outcome per key instead of the
  selection list; select and reset on one key ride one plan step, so per-key outcomes
  suffice. A key with no outcome is not consumed: a bare reset of an unreachable item
  (the kit does not provide it, the lock lost it) stays for the load-time validity
  check to drop with a report.
- No session view state in the TUI. After an apply the tree shows no pending marks
  and the next open offers no restore dialog; the results screen (10.3) and the exit
  record (10.4) carry what was applied. This removes the visible v0.1.1 behavior of
  kept checkmarks.
- Release target is 0.1.3: CHANGELOG entries under `[Unreleased]` as Fixed — the TUI
  loop and the reset alignment are released behavior, the CLI deadlock shipped in
  0.1.2. Tags are not touched.
- docs/concept.md already defines pending actions as "the actions chosen since its
  last apply" and does not change; docs/tui-screens.md section 10 gains a sentence
  saying the applied actions are consumed.
- Old registries self-heal: the first `penates apply` after the fix consumes a kept
  already-installed selection — the CLI deadlock test pins this; no migration.
- Test ambition (the user's requirement): the tests close the class, not the
  instance — after any apply, the registry holds only unsatisfied intent, pinned at
  every layer.

Working agreement: the peer session (beta) implements one task at a time in a fresh
session; the lead (alpha) reviews, commits, and owns the overall solution.

---

### TASK-01 `settle-per-item-outcomes`
**Description:** internal/state: change `SettlePending` to consume per-item outcomes — the signature takes the outcome per key (satisfied, removed) instead of the selection list; the select with its answers and the reset of a satisfied key are consumed, keys that failed, were excluded, or never reached the plan keep their actions, an empty pending object dies. Thread the outcomes through both callers (internal/cli/mutate.go `settleAndComplete` and the `mutation.after` path; internal/tui/apply.go `runApply`) from the per-step outcomes they already hold — the CLI from `buildRows` verbs plus the error map, the TUI from `out.steps` states, where a kept step carrying exclusion lines counts as excluded, not satisfied. Add unit tests in internal/state covering every action × outcome combination.
**Definition of done:** `go build ./...` and `go test ./internal/state/...` pass; unit tests pin by name: a satisfied selection consumed with its answers, a satisfied reset consumed, a failed, an excluded, and an absent key stay staged, a successful removal consumed, the pending object removed when empty; the existing suites of both callers stay green.

### TASK-02 `cli-deadlock-and-staged-tests`
**Description:** internal/cli end-to-end tests. (1) The deadlock loop: install an item, stage `{select: true, version: <installed>}` exactly as the TUI leaves it after its apply, run `penates apply` — it answers kept, the pending actions are empty afterwards, and a one-shot command no longer refuses; this is also the self-heal pin for old registries. (2) A staged selection whose step fails at apply time (io injection, as `TestFailedStepPrintsIOCategory` does) stays staged, and a second apply retries it. (3) An item excluded at plan time (a missing answer, a conflict) stays staged after the apply. Fix what the tests expose in the settle threading.
**Definition of done:** the three scenarios are pinned by named tests in internal/cli; `go test ./internal/cli/...` passes together with the existing suite.

### TASK-03 `tui-clean-tree-after-apply`
**Description:** internal/tui tests. After an apply that lands: the applied rows show no pending action in the tree, and opening the target again (switch away and back) offers no restore dialog (11.1); a mixed apply with one failed item keeps that item's pending action marked while the satisfied ones clean up; the save and restore of unapplied selections across target switches (8.2, 11.1 — targets_test.go) stays green. Fix what the tests expose.
**Definition of done:** the behaviors are pinned by named tests in internal/tui; `go test ./internal/tui/...` passes together with the existing suite.

### TASK-04 `docs-changelog-and-close`
**Description:** docs and close-out. One sentence in docs/tui-screens.md section 10 saying the applied actions are consumed and the list returns clean; CHANGELOG `[Unreleased]` Fixed entries for the TUI restore loop, the reset alignment, and the CLI staged deadlock, worded per that file's format; verify docs/concept.md needs no change; run `make check` (CONTRIBUTING.md); lead review of the whole branch; archive this tasks file.
**Definition of done:** `make check` passes; docs and CHANGELOG update in the agreed shape; every task in this file is resolved and the file moves to docs/archive/ in the commit that brings it to that state.
