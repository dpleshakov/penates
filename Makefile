# Makefile for penates.
#
# Every verification the project has is reachable through `make check`, and CI
# runs that same target, so "green locally, red in CI" cannot happen by
# construction. Recipes avoid Unix-only utilities: file removal goes through
# git, which every platform has. Releasing is not done from here.
#
# Prerequisites: Go, and — for `lint` — golangci-lint v2 on PATH.

.PHONY: build lint test check clean

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

# Vet, test, and compile the binary into the repository root. The everyday
# target while working on the code.
build:
	go vet ./...
	go test ./...
	go build ./cmd/penates

# ---------------------------------------------------------------------------
# Test
# ---------------------------------------------------------------------------

# Make sure the module files match the imports, and run golangci-lint. Run it
# before committing; CI runs it as part of check. `go mod tidy -diff` writes
# nothing: it prints the changes the module files need and fails, and fixing
# them is a deliberate `go mod tidy` whose result goes into the commit.
lint:
	go mod tidy -diff
	golangci-lint run

# Measure statement coverage of the internal packages, print the per-function
# breakdown, and fail below the threshold. cmd/penates stays out of the
# profile: the threshold guards the packages that carry the product
# behaviour, while the command tree is pinned by its own tests, which
# `build` runs.
test:
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -func=coverage.out
	go run tools/check-coverage.go 70

# Everything the project verifies, in one target. This is what CI runs.
check: build lint test

# ---------------------------------------------------------------------------
# Clean
# ---------------------------------------------------------------------------

# Remove every build artifact — the binary and the coverage profile — leaving
# the working tree as git sees it. -X limits git clean to ignored files, -d
# lets it remove directories, and the paths restrict it to exactly these
# artifacts, so untracked work is never touched.
clean:
	git clean -fdX -- penates penates.exe coverage.out
