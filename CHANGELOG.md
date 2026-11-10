# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- The binary now ships its own documentation as a built-in kit: on every start it is unpacked into the penates data directory, registered under the reserved name `penates`, and kept in step with the binary's content. Attaching and installing its items is the user's choice.
- The built-in kit carries its content: a product reference installing into `skills/penates/` and a kit-authoring guide installing into `skills/penates-kit-authoring/`.
- The first-run screen offers attaching the built-in kit, and the command palette gains `Attach kit` for registered kits that are not attached to the current target.
- A locally modified built-in kit shows on its kit row, and `Reset builtin kit` discards the local edits after a confirmation naming what it loses.

### Fixed
- A kit holding an item whose declaration is broken — an invalid `item.yaml`, an empty, invalid, or duplicated id — no longer disappears from the tree as a whole: the kit attaches and loads, the healthy items show, and the problem shows on the affected item's row, which offers no actions. An installed item with a broken declaration stays frozen: removal only, no updates or reset.
- Removing all co-owners of a shared json value in one removal run now takes the value away with the first item's plan. The run saw each item's record still pointing at the other as a living owner, so the value stayed in the file with no owner left recorded and only a second run removed it.

## [0.1.0] - 2026-11-08

### Added
- Initial release of penates: a terminal UI that installs, updates, and removes agent-harness customization — skills, instruction files, plugins, configuration fragments — from local kit directories into a harness configuration directory such as `~/.config/opencode`.
- Everything penates writes is recorded in a per-target lock; what penates did not write it never touches.
