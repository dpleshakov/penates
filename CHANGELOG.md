# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- A major version bump is marked for review: the update marker of an item whose new major differs from the installed one renders in the error style, next to the version arrow.
- Selected items with file contributions install from a kit into a harness configuration directory, and a per-target lock records every file that penates writes.
- Starting with a kit path registers the kit and its target directory; without arguments, the registered target opens directly.
- Items with json contributions install their values into shared JSON and JSONC files: each value lands by its RFC 6901 location, comments and formatting of untouched parts survive, an equal value owned by another installed item makes the item a co-owner, and the lock records every owned location with the hash of its value.
- Items with variables ask for the missing required ones in a form before planning: answers are stored in the lock and reused on the next run, templated contributions get `{{var}}` substituted, and an unknown placeholder or a required variable without a value excludes the item.
- Installed items are updated from their kit when its version is higher: owned files, sections, and json values follow the three-state rules, so a user modification is kept when the kit content is unchanged and reported as a conflict when it differs. An equal version with changed content warns without an update, a lower version is shown as a downgrade, and reset to kit replaces owned content on explicit request.
- Installed items are removed on request: unchanged owned files are deleted and user-modified ones are kept and reported, owned sections go with their markers or stay without them, json values go when their item is the last owner, directories and shared files penates created are cleaned up while the removal leaves them empty, and orphans — installed items no kit provides anymore — stay listed and removable.
- After an interruption, penates reconciles the lock with the target before planning: staged temp files an interrupted apply left behind are removed, an installed item whose recorded content the target holds in another form is shown as modified and kept, and one whose recorded content the target lost is shown as broken and is installed again when applied.
- The item list follows the RFC 10.2 shape: rows are grouped under their kit's header, every status — selected, installed, update, downgrade, modified, broken, warning, conflict, orphan, kit unavailable — renders its own bracket marker, `/` filters by id, tags, and description, and `i` opens the item's details: description, contributions, changelog when the kit carries one, source kit, and the variable answers recorded at installation. The items of a kit that no longer loads appear as kit unavailable under the kit's header, and installed items of a kit that is not registered are grouped as orphans under its name.
- Applying is confirmed through a summary first (RFC 10.2): it lists every change per item with the reason of each action, the removals marked with `d` together with the content they leave behind, the items kept unchanged with a warning when the kit content differs, and every excluded item with its reasons. Nothing is written until the summary is confirmed with enter; esc returns to the list with the selection intact. The variable form interrupts before anything is written, and a required variable answered with an empty string shows the item excluded on the summary instead of asking again.

### Changed
- The product specification lives in `docs/concept.md`, a living document, instead of `docs/rfc-penates.md`: the concept absorbs the model and the rules of ownership, updates, and removal, states the three deviations the shipped build made as the actual behaviour, and drops the alternatives survey. The RFC is archived under `docs/archive/` with its status stamped, and code comments state their rules directly instead of citing RFC section numbers.
- The README introduces the tool to a new reader — the problem it solves, the main screen, a minimal kit, and the ownership rules — instead of a summary paragraph with links to the design documents.
- Updating an item no longer releases ownership of the json values its new version stops declaring: owned files, blocks, and json locations stay recorded exactly as installed until the item is removed, so a removal still cleans up everything penates wrote and a later version that declares the location again evaluates the user's content against the installation base.
- The penates data directory is `~/.config/penates` on every OS, honoring `XDG_CONFIG_HOME` when it is set and non-empty, instead of the platform configuration directory (`%AppData%` on Windows, `~/Library/Application Support` on macOS). Existing state is not migrated.

### Fixed
- An update that keeps a user-modified file or block no longer drops it from the lock record: content a plan leaves standing stays recorded with its installation base, so later updates and removals still see it.
- Removing an item no longer passes silently over the json content the user reshaped: an array element the user replaced or removed, and a value standing where the item installed another one, stay in the target and are named in the removal report like a modified value is.
- Applying into a target directory that does not exist yet no longer fails when an item writes a file directly into the target root.
