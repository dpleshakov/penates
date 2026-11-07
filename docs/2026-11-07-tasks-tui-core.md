# 2026-11-07-tasks-tui-core.md

**Status:** Active

## Context

Core changes that docs/tui-screens.md requires beneath its screens: they
land before the TUI rewrite, one task per reviewable change, with the
current TUI still working after each. Decisions spanning the tasks:

- State becomes a registry of entities: kits (name to directory), targets
  (path with an optional display name), and attachments between them — a
  kit attaches to several targets, and a target exists without kits.
- Pending actions become a first-class per-target object: item actions
  (select, reset, remove), deferred operations (detach kit, forget target),
  and variable answers given at selection time, kept until the apply writes
  them into the lock so a restored selection does not re-ask. The object is
  saved on every mutation; entries that no longer hold are dropped at load
  with a report the UI can show.
- No migration: the project has no releases and no live use. Loading an
  old-format state.json reports an error naming the file to delete.
- A deferred operation completes when the apply removed everything it
  covers; otherwise it is dropped with a reason shown to the user.
- Documentation and CHANGELOG ride in the same commits as the behaviour
  they describe (repository rule).

Spec: docs/tui-screens.md; model rules: docs/concept.md.

---

### TASK-01 `state-schema`
**Description:** Rework internal/state around the entities above: kits,
targets with display names, attachments, the last active target,
per-target pending actions with deferred operations and session variable
answers. Store operations make
saving on every mutation possible. An old-format state.json is refused with
an error naming the file to delete. A thin compatibility layer keeps the
current TUI running on the new schema — pending installs read as the
selection, registration attaches — until the rewrite replaces it.
**Definition of done:** state tests cover the new schema round-trip,
mutation-level saves, and the old-format refusal; the existing TUI tests
pass unchanged through the compatibility layer; architecture.md (state
row) and concept.md 6.7 describe the new model and CHANGELOG records the
format change with the refusal, in the same commit.
**Status:** Done

### TASK-02 `state-operations`
**Description:** Operations over the schema with their invariants: attach a
kit to a target; detach — immediate when the kit holds no installed items
in that target, deferred until the covering apply otherwise. Which path
runs is the interface's choice (the dialogs of tui-screens 6.7 and 8.3),
so both are operations with their invariants and the immediate one never
touches the lock; edit kit path,
accepted only when the new directory holds a kit with the same name; forget
target, which deletes the target's lock — lock.Store learns deletion — and
never touches the target's files. Deferred operations complete when the
apply removed everything they cover and are dropped with a shown reason
otherwise.
**Definition of done:** tests cover each operation and invariant —
attach and both detach paths, edit-path validation accepting a matching
name and rejecting a mismatch with the reason, forget deleting the lock and
leaving the target directory untouched, deferred completion and dropping;
concept.md 6.6 states targets and attachment in the same commit.
**Status:** Done

### TASK-03 `plan-conflict-structure`
**Description:** internal/plan reports conflicts as structure instead of
prose: the kind, the file, section, or JSON location concerned, and the
owning installed item when the collision is with owned content. Prose for
the user is rendered from that structure in the UI. Non-conflict exclusion
reasons — a missing variable and the rest — stay reasons.
**Definition of done:** the planner returns structured conflict data for
file, section, and json collisions, including the owning item where the
collision is with owned content; existing tests assert the structure
instead of strings; the current TUI shows the same conflict information as
before, rendered from the structure.
**Status:** Pending

### TASK-04 `apply-stepping`
**Description:** internal/apply exposes per-item execution: a step applies
or removes one item while the invariants hold — removals before installs,
each step planned from a fresh snapshot, the item applied completely or not
at all, the lock recorded only for completed items. The monolithic
Apply/Remove paths become compositions of steps, one behaviour for a future
command-line client. Steps report their start and result so the UI can show
per-item progress.
**Definition of done:** interruption between any two steps leaves a state
the existing reconciliation repairs (test); a mixed plan stepped through
produces the same lock and target content as the monolithic path (test);
step boundaries expose per-item start and result.
**Status:** Pending
