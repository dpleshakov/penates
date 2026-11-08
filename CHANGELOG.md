# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- The binary now ships its own documentation as a built-in kit: on every start it is unpacked into the penates data directory, registered under the reserved name `penates`, and kept in step with the binary's content. Attaching and installing its items is the user's choice.

## [0.1.0] - 2026-11-08

### Added
- Initial release of penates: a terminal UI that installs, updates, and removes agent-harness customization — skills, instruction files, plugins, configuration fragments — from local kit directories into a harness configuration directory such as `~/.config/opencode`.
- Everything penates writes is recorded in a per-target lock; what penates did not write it never touches.
