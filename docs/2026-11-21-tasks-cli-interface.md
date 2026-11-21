# 2026-11-21-tasks-cli-interface.md

**Status:** Active

## Context

The concept now fixes two interfaces of equal capability — the CLI for AI
agents and automation, the TUI for humans — and section 10.3 "The CLI" is the
complete contract: the command table, the registration guard, the immediate
variants of the deferred operations, the confirmation model, the staged-actions
refusal, the plain-text output skeleton, the error format, and the
permission-rule governability. The change landed with commit c25c5d2 on
branch `concept/dual-interfaces` ("docs: add the CLI to the concept as the
second full interface") — check whether that branch is merged when picking
this up. architecture.md already anticipates the work: the `internal/plan`
rule ("a future command-line client reuses the core unchanged"), the client
sentence in "Architecture", and the known-risks entry about concurrent runs,
which this work resolves.

Decisions recorded in the concept review and living nowhere else:

- The stable skeleton (concept 10.3) names what is stable but not the values:
  the state word set, the exit codes, and the error category set are pinned
  by this work, not by the concept. The category set then lives in code and
  in the `reference` skill.
- The sibling project `tfscli` (a local checkout; ask the user for the path)
  is the reference for the error contract: `Error [category]: message` on
  stderr, categories never removed or renamed, every error naming the next
  step.

The concept's intro note "accepted design ahead of their implementation" is
removed only when the CLI and the `operating` skill (its own tasks file) have
both shipped.

---

### TASK-01 `pin-contracts`
**Description:** Pin the values the concept leaves open: the state word set (one word per item state the TUI shows as a glyph), the exit codes, and the error category set — with their stability guarantees.
**Definition of done:** The three sets are written down, agreed with the user, and recorded where they will live from then on (code docs and the `reference` skill).
**Status:** Done — recorded in `internal/cli` (errors.go, cli.go, view state words) and in the `reference` skill (0.2.0); accepted on the lead's review, user ratification deferred to the wrap-up report.

### TASK-02 `command-surface`
**Description:** Implement the CLI as the second client — the module under `internal/` — per the concept 10.3 command table and its rules: one-shot verbs, flags after the command path, one form per command, the registration guard, the immediate variants of the deferred operations, `--target` and `--set` semantics, the staged-actions refusal, the confirmation model, and the error output.
**Definition of done:** Every command of the 10.3 table works per its rules; every scenario of concept section 10 is reachable from the CLI (a parity checklist in the tests); `make check` passes.
**Status:** Done — every command of the table landed with the mutating round; the parity scenarios hold in `internal/cli` tests (the concurrency leg is TASK-03's own).

### TASK-03 `concurrency-guard`
**Description:** Resolve the architecture known risk: guard overlapping runs over the one state file — refused or serialized, never silently merged (concept: application state).
**Definition of done:** Two concurrent penates processes cannot corrupt or silently merge state; the known-risks entry in architecture.md is resolved by this work.
**Status:** Pending

### TASK-04 `ship-riders`
**Description:** The riders that ship with the CLI: the architecture.md updates (module row, dependency register for the grown cobra surface, data flow with the second client), the README section for the CLI, the CHANGELOG entry under Unreleased, and — together with the shipped `operating` skill — removal of the concept's intro note about accepted design ahead of implementation.
**Definition of done:** architecture.md, README.md and CHANGELOG.md describe the shipped CLI; the concept intro note is gone; `make check` passes.
**Status:** Pending
