# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.3] - 2026-11-22

### Fixed
- An apply now consumes what it satisfied: a staged selection that landed — or that the plan kept as already installed — leaves the pending actions together with its answers. In the terminal UI the applied rows stop carrying the pending mark and the target stops offering the restore dialog for what was already applied; in the CLI a staged already-installed selection no longer deadlocks the one-shot commands behind the staged refusal. What the apply did not satisfy — a failed step, an excluded item — stays staged and repeatable as before.
- A reset whose step failed at apply time now stays staged and repeatable, like selections and removals, instead of being consumed unconditionally.

## [0.1.2] - 2026-11-22

### Added
- The command-line interface: every scenario of the terminal UI is reachable from commands — `status`, `show`, `install`, `update`, `remove`, `reset` (with `--builtin`), `apply`, `pending clear`, `setup`, and the `kit` and `target` management groups. `--target <path>` names the target a command acts on and is required wherever a command acts on one — the CLI never depends on which target the terminal UI opens; `target list` counts each target's staged pending actions. A mutating command prints its summary and asks once; `--yes` confirms in advance, `--dry-run` previews without writing; without a terminal a command that would ask requires `--yes`.
- `penates setup`, the first run's CLI form: it attaches the built-in kit to `--target` — registering the target when it is new — and installs the kit's skills, idempotently; a repeat is kept and reported, not an error. An agent may begin a machine alone; the trust it acts under is the same recorded ownership.
- The CLI output contract: plain text with a stable skeleton — items named `kit/item`, a stable state word opening every item line, one reason per line — and failures printing one `Error [category]: message` line each, the categories a stable set, every message naming the concrete next step. Exit codes: `0` success, `1` any other failure, `2` a wrong command line alone. Flags come after the command path and each command has exactly one form, so permission rules can govern the CLI by the command text.
- A run guard over the penates data directory: one penates process at a time — an overlapping run, any command while the terminal UI is open included, is refused with the `busy` error instead of silently merging state, and a process's death releases the hold.
- The `operating` skill in the built-in kit, the third beside `reference` and `kit-authoring`: it teaches AI agents to drive penates day to day through the CLI — reading state, changing kit content in the kit rather than the target, keeping machine specifics in variables, and the conduct on the `staged` and `busy` refusals. Installs into `skills/penates-operating/`.
- `penates kit move <kit> <dir>`: point a registered kit at the directory that now holds it — the CLI form of the terminal UI's "Edit kit path". The directory must hold a kit of the same name; the kit's attachments and pending actions survive the move.

### Changed
- Errors of `version` and `licenses` print in the `Error [category]: message` form like every CLI failure, and a wrong command line exits `2` instead of `1`.

### Fixed
- A reset staged in the terminal UI without a selection now applies: pressing `r` on an item and then `a` silently applied nothing before.
- Applying in the terminal UI with only deferred operations staged now completes or drops them with a report, instead of showing an empty results screen that left the pending actions waiting forever.

## [0.1.1] - 2026-11-08

### Added
- The binary now ships its own documentation as a built-in kit: on every start it is unpacked into the penates data directory, registered under the reserved name `penates`, and kept in step with the binary's content. Attaching and installing its items is the user's choice.
- The built-in kit carries its content: a product reference installing into `skills/penates/` and a kit-authoring guide installing into `skills/penates-kit-authoring/`.
- The first-run screen offers attaching the built-in kit, and the command palette gains `Attach kit` for registered kits that are not attached to the current target.
- A locally modified built-in kit shows on its kit row, and `Reset builtin kit` discards the local edits after a confirmation naming what it loses.

### Fixed
- A kit holding an item whose declaration is broken — an invalid `item.yaml`, an empty, invalid, or duplicated id — no longer disappears from the tree as a whole: the kit attaches and loads, the healthy items show, and the problem shows on the affected item's row, which offers no actions. An installed item with a broken declaration stays frozen: removal only, no updates or reset.
- Removing all co-owners of a shared json value in one removal run now takes the value away with the first item's plan. The run saw each item's record still pointing at the other as a living owner, so the value stayed in the file with no owner left recorded and only a second run removed it.
- A saved selection or reset whose item has moved to another version since it was made is no longer restored silently and applied at the new version: the pending actions record the item version they were made against, restoring drops the ones it no longer matches, and the notice line reports what went. Selections saved before this change restore as they always did. Removals are unaffected — they are planned from the lock and survive version changes.

## [0.1.0] - 2026-11-08

### Added
- Initial release of penates: a terminal UI that installs, updates, and removes agent-harness customization — skills, instruction files, plugins, configuration fragments — from local kit directories into a harness configuration directory such as `~/.config/opencode`.
- Everything penates writes is recorded in a per-target lock; what penates did not write it never touches.
