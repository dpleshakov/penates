# 2026-11-21-tasks-operating-skill.md

**Status:** Archived

## Context

The concept now fixes penates as a dual-interface tool — the CLI for AI agents
and automation, the TUI for humans — and commits the built-in kit to three
skills: `reference`, `kit-authoring`, and `operating` (concept: the built-in
kit). `operating` is new, committed by one line, and designed nowhere: it is
the actual agent-facing product and must be designed before or together with
the CLI it documents. The interface contract it teaches is concept section 10,
"The CLI" above all; the concept change landed with commit c25c5d2 on branch
`concept/dual-interfaces` ("docs: add the CLI to the concept as the second
full interface") — check whether that branch is merged when picking this up.

Content the skill must carry — decided in the concept review and recorded
nowhere else, so it lives here:

- The daily workflows: read state (`status`, `show`), change content by
  editing the kit repository and running `penates update` — never by editing
  the installed copy in the target; machine-specific variation goes through
  variables, not edits.
- The norms: on a refusal caused by staged pending actions, tell the person
  who staged them — apply, clear, or ask, never decide silently; the target
  holds installed content, the kit holds source.
- The boundary: `operating` teaches driving penates; `reference` remains the
  behavior specification. The error category set lives in code and in
  `reference`, not in `operating` (a concept-review decision).

The skill ships together with the CLI; the concept's intro note "accepted
design ahead of their implementation" is removed only when both have shipped.

---

### TASK-01 `design`
**Description:** Design the `operating` skill: scope, structure, and content — the workflows, norms, and boundaries listed in Context — as a draft the user accepts.
**Definition of done:** A draft of the skill's content exists, covers every item above, and the user has accepted it as the basis for writing.
**Status:** Pending

### TASK-02 `write-and-install`
**Description:** Write the accepted skill into the built-in kit (`internal/builtin/kit/`), as a new item installing alongside `reference` and `kit-authoring`.
**Definition of done:** The skill installs and updates through penates like any other kit item; the built-in kit's items and versions reflect it; `make check` passes.
**Status:** Pending
