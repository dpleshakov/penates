# 2026-11-27-tasks-file-exec-bit.md

**Status:** Archived

## Context

The backlog entry `file-exec-bit` (docs/backlog.md, added 2026-11-18) asked for kits
able to ship runnable scripts: every staged file chmods to 0o644 (internal/apply
stage), and the kit loader reads bytes only, so a script symlinked onto PATH
installs unrunnable. The entry sketched threading the source mode through kit
load → plan → stage. That direction is rejected after checking the real transports:

- A 9p drvfs mount without metadata stats every file 0777 — a kit read from such a
  mount would install every file executable, READMEs and tests included. The git
  index holds the true mode, but penates reads the working tree and performs no
  version-control operations (a concept non-goal).
- The built-in kit travels through embed and is unpacked with a fixed 0600
  (internal/builtin/sync.go): no source mode exists to inherit.
- penates is cross-platform first — Linux, macOS, Windows. A source-derived bit
  means a different install per filesystem; a declaration means the same install
  everywhere, and on Windows it is a no-op (executability follows the file
  extension there).

Decisions:

- The declaration is `executable: true` on a `file` contribution — the direct
  precedent of `template: true`. A directory source applies the flag to every file
  of the tree (directories expand at load).
- Install mode: 0o755 with the flag, 0o644 without. The distinction exists on Unix
  only.
- `executable: true` on a section or json contribution is a broken declaration and
  becomes an item Problem — the same channel as "a file contribution may not
  target a shared file".
- The lock is unchanged: content hashes only. A user chmod is not a content
  modification and stays invisible to the three-state model. The mode is
  re-derived from the declaration on every write of the file — install, update
  with changed content, reset, the reinstall of lost content — and a change of
  the declaration alone writes nothing (an update whose content is unchanged
  writes nothing either, per ownedAction). Reset is the escape-hatch for a
  declaration-only change.
- Nothing else moves: ReadSources, plan.Input, the clients, and the builtin sync
  are untouched — the flag rides kit.Contribution, which the planner already
  iterates.

Settled with the peer session (beta) on 2026-11-27:

- The action field is `Exec bool`: the domain has two file classes, and
  `fs.FileMode` would carry an OS type and modes the model never discusses
  into the planner.
- The Windows leg runs on every platform — success and content are asserted
  everywhere, and the mode comparison only where directory modes are honored
  (every Unix), with the `runtime.GOOS` check the cli tests already use.

Working agreement: alpha (this session) writes and hands the work over uncommitted
with the gate run; beta (the peer session) stays read-only and reviews when the
user requests it; commits follow the review.

---

### TASK-01 `kit-declared-executable`
**Description:** internal/kit: `contributionFile` and `Contribution` gain `Executable bool` (`yaml:"executable"`); validation rejects the flag on section and json contributions through the item's Problems; the directory expansion carries the flag onto every expanded contribution. Tests: the field parses and reaches the kit value the planner iterates; the section/json rejection lands on the item's row and keeps the item out of planning; a directory source expansion carries it per file.
**Definition of done:** `go test ./internal/kit/...` passes with the three behaviors pinned by named tests.
**Status:** Done

### TASK-02 `thread-mode-plan-apply`
**Description:** internal/plan: `Action` gains `Exec bool`; the three file-action sites (the actions reasoned "target file is absent", "reset to kit replaces the owned file", "update replaces the owned file") set it from the contribution's `Executable`; section, json, and removal actions never carry it. internal/apply: `stage` chmods 0o755 for an executable action and 0o644 otherwise, instead of the hardcoded 0o644, and both call sites (Item, RemoveItem) pass the action's value; the gosec suppression keeps an updated reason. Tests: plan_test pins the bit on file actions only; the apply integration installs an executable-flagged file and asserts its content everywhere and its mode 0o755 where directory modes are honored (every Unix, the runtime.GOOS check the cli tests use) and 0o644 for an ordinary file.
**Definition of done:** `go build ./...`, `go test ./internal/plan/... ./internal/apply/...` pass; the pins above are named tests; `make lint` is clean.
**Status:** Done

### TASK-03 `docs-changelog-close`
**Description:** concept.md: the 6.3 example gains an `executable: true` line; 6.4 gains the rule — an optional flag on file contributions, a 0o755 install, no effect on Windows, invalid on section and json, directory sources apply it per file, the mode re-derived on every write of the file and a declaration-only change writing nothing until a reset. The built-in kit: the kit-authoring skill's item.yaml example and file-kind entry teach the flag, and the item version moves to 0.2.0 (item.yaml and the SKILL.md frontmatter together) so installed copies see the update; `make builtin-kit` stays green. The reference skill stays unchanged: it documents the model and ownership, not the item.yaml schema, and nothing operator-visible changes. CHANGELOG under `[Unreleased]`: the flag as Added, the skill's update as Changed. The backlog entry leaves docs/backlog.md with the commit that carries this breakdown. Close: `make check`, hand over uncommitted, archive this file after the review.
**Definition of done:** `make check` passes; concept, the kit-authoring skill, and the CHANGELOG are updated; the backlog entry is gone; this file is archived after the review.
**Status:** Done
