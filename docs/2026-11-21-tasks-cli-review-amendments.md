# 2026-11-21-tasks-cli-review-amendments.md

**Status:** Active

## Context

After the dual-interface work shipped on this branch (2ea1626…f252273), the owner
reviewed the contested decisions and closed them. Three changes follow; everything
else was ratified as shipped (exit code 2 for usage, the conflict category's scope,
reset of any installed item, downgrade via install, the declined-prompt outcome,
the two accepted test gaps).

The `--target` flag becomes mandatory wherever a command acts on a target, and the
CLI stops reading the TUI's active target entirely — switching targets is the
TUI's own state, and a command line that does not name its target is not
self-describing. The first-run guard is removed: first use is usually a person's,
but an agent may bootstrap alone, and `penates setup` is that path — the CLI twin
of the TUI's first run, attaching the built-in kit with its skills. The TUI's
deferred-only apply starts completing deferred operations the way the CLI already
does.

---

### TASK-01 `require-target`
**Description:** Make `--target` required for every command that acts on a target (status, show, install, update, remove, reset with an item, apply, pending clear, kit add/attach/detach); remove the last-active fallback from the CLI; add a pending count to `target list`; update concept 10.3, the reference and operating skills, README, and the unreleased CHANGELOG entries.
**Definition of done:** No CLI command depends on or writes the TUI's active target; a missing `--target` is a usage error naming the flag; `target list` shows `pending N` for targets holding staged actions; `make check` passes.
**Status:** Done — the CLI reads no TUI state at all; `target list` also dropped its `active` marker (a display read is a read).

### TASK-02 `setup-and-no-guard`
**Description:** Remove the first-run guard and the `first-run` error category; add `penates setup [--target] [--dry-run] [--yes]` — attach the built-in kit to the target (registering it when new) and install its skills, idempotently; update concept §1, §10.1, §10.3, the reference and operating skills, README, CHANGELOG.
**Definition of done:** `kit add` and `kit attach` work on a virgin machine; `setup` bootstraps a fresh target, re-runs as `already attached`/`already installed` with exit 0, and refuses under staged pending like any one-shot command; the category set is nine; `make check` passes.
**Status:** Done

### TASK-03 `tui-deferred-apply`
**Description:** The TUI's apply with only deferred operations staged completes or drops them with a report, mirroring the CLI's semantics, instead of showing an empty results screen; close the `tui-deferred-only-apply` backlog entry.
**Definition of done:** A staged detach (or forget) with no item actions, applied in the TUI, shows the deferred operations in the summary and completes or drops them at apply with the report; tui-screens.md is updated if it describes the old behavior; the backlog entry is removed; `make check` passes.
**Status:** Pending
