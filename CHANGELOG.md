# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Selected items with file contributions install from a kit into a harness configuration directory, and a per-target lock records every file that penates writes.
- Starting with a kit path registers the kit and its target directory; without arguments, the registered target opens directly.
- Items with json contributions install their values into shared JSON and JSONC files: each value lands by its RFC 6901 location, comments and formatting of untouched parts survive, an equal value owned by another installed item makes the item a co-owner, and the lock records every owned location with the hash of its value.
- Items with variables ask for the missing required ones in a form before planning: answers are stored in the lock and reused on the next run, templated contributions get `{{var}}` substituted, and an unknown placeholder or a required variable without a value excludes the item.
- Installed items are updated from their kit when its version is higher: owned files, sections, and json values follow the three-state rules, so a user modification is kept when the kit content is unchanged and reported as a conflict when it differs. An equal version with changed content warns without an update, a lower version is shown as a downgrade, and reset to kit replaces owned content on explicit request.
- Installed items are removed on request: unchanged owned files are deleted and user-modified ones are kept and reported, owned sections go with their markers or stay without them, json values go when their item is the last owner, directories and shared files penates created are cleaned up while the removal leaves them empty, and orphans — installed items no kit provides anymore — stay listed and removable.

### Fixed
- Applying into a target directory that does not exist yet no longer fails when an item writes a file directly into the target root.
