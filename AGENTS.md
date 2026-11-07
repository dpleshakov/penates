<!-- author: dpleshakov, version: 0.1 -->

# Agent instructions

Instructions for AI coding agents working in this repository. People contributing
manually follow the same rules where they apply.

## Commands

- Build: `go build ./...`
- Test: `go test ./...`
- Lint: `golangci-lint run`; format: `golangci-lint fmt`

## Public repository

This repository is public. Never commit secrets, credentials, tokens, personal paths,
machine names, or personal data of any kind. Review the full diff for such content
before every commit.

## Documentation

Product behaviour is specified in `docs/concept.md` — a living document; the stack,
modules, and structure are recorded in `docs/architecture.md`. A change that alters
documented behaviour updates the document in the same commit.

User-visible changes get an entry under `## [Unreleased]` in `CHANGELOG.md`, in the
format that file defines.

## Architecture rules

- `internal/plan` performs no I/O: it receives kits, lock, and the target snapshot as
  values (see `docs/architecture.md`).
- No package knows any harness; destination paths come from kit data only.

## Commits

Imperative subject line in English, one logical change per commit.
