# 2026-11-04-tasks-walking-skeleton.md

**Status:** Active

## Context

Walking skeleton of penates: the smallest end-to-end path — start with a kit path,
register the kit and its target, list items, install an item that contributes files
only, record the lock. `section` and `json` contributions, variables, updates,
removal, and interruption reconciliation are out of scope; items using them are
reported as not supported yet. Spec: docs/rfc-penates.md; module rules:
docs/architecture.md.

---

### TASK-01 `kit-loader`
**Description:** `internal/kit`: load a kit directory — `kit.yaml`, `items/`,
`item.yaml` for every item — into typed structures, with full validation of ids,
versions, source and target paths, and shared-file targets; validation errors name
the file and field.
**Definition of done:** `Load` accepts a fixture kit with `file`, `section`, and `json`
contributions and rejects invalid kits with precise messages; table tests cover both.
**Status:** Pending

### TASK-02 `state-store`
**Description:** `internal/state`: application state — registered kits with paths,
targets, kit-to-target mapping, last selection per target — persisted as JSON under
the penates data directory; directory injectable for tests.
**Definition of done:** registering a kit and target, saving, and loading again yields
the identical state; round-trip test passes.
**Status:** Pending

### TASK-03 `lock-format`
**Description:** `internal/lock`: the per-target lock in the RFC 6.7 JSON shape,
stored outside the target, keyed by the target path; ownership queries needed for
file installation.
**Definition of done:** a lock with one installed item round-trips through JSON; file
ownership and created-directory queries answer correctly; tests pass.
**Status:** Pending

### TASK-04 `target-snapshot`
**Description:** `internal/target`: read target file state — existence and SHA-256
content hashes for a given list of paths — into a plain value for the planner.
**Definition of done:** snapshot of a fixture target reports existence and hashes of
listed paths; tests pass.
**Status:** Pending

### TASK-05 `file-planner`
**Description:** `internal/plan`: pure planning of installs for items with `file`
contributions only, implementing the RFC 7.2 state table, cross-item file conflicts,
and the not-supported-yet errors for other contribution kinds and variables.
**Definition of done:** table tests cover every row of the RFC 7.2 table plus conflict
and unsupported-kind errors; the planner performs no I/O.
**Status:** Pending

### TASK-06 `apply-writer`
**Description:** `internal/apply`: execute a plan per item — create directories,
write files through temp-and-rename, record hashes and created directories in the
lock, save the lock after each item.
**Definition of done:** applying a plan installs files and updates the lock; an item
that fails mid-way writes no files and leaves the lock unchanged; tests pass.
**Status:** Pending

### TASK-07 `tui-shell`
**Description:** `internal/tui` + `cmd/penates`: minimal client — kit registration
with a target form prefilled from `target_hint`, item list with select and installed
markers, apply with a results screen; target picker when several targets are
registered.
**Definition of done:** `go build` produces a binary that runs the register → list →
select → apply flow against a fixture kit; manual smoke test performed.
**Status:** Pending

### TASK-08 `integration-slice`
**Description:** End-to-end test without the TUI: load a kit, register it, snapshot
the target, plan, apply, verify files and the lock, re-plan and observe no actions.
**Definition of done:** the test passes and covers install plus the no-action
re-plan; CHANGELOG gains an `Unreleased` entry for the install path.
**Status:** Pending
