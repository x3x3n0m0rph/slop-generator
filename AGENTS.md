# Repository Guidelines

## Project Structure & Module Organization

`cmd/slop-generator/` contains CLI flags, startup diagnostics, and dependency wiring. `internal/` separates configuration (`config`), API clients (`inference`), generation and Python validation (`pipeline`), Git operations (`gitrepo`), JSON persistence (`history`), scheduling (`task`), and terminal UI (`tui`). Unit tests sit beside source files as `*_test.go`; `tests/integration/` contains the optional live API test. `config.example.yaml` documents configuration. `playground/` is a local scratch area.

Keep interfaces with their consumers and inject concrete dependencies through the command entry point. Keep commit and publication orchestration in `task`.

## Build, Test, and Development Commands

Use Go 1.26+, Git, and Python 3 for Python pipeline checks. Run commands from the repository root:

- `go build -o slop-generator.exe ./cmd/slop-generator`: build the CLI.
- `go run ./cmd/slop-generator -config config.yaml`: launch the TUI.
- `go run ./cmd/slop-generator -config config.yaml -check-config`: validate configuration and local Git settings.
- `go test ./...`: run automated tests; the live test skips by default.
- `go vet ./...`: check for suspicious Go constructs.

Create local configuration from `config.example.yaml` and replace its sample repository path before running.

## Coding Style & Naming Conventions

Format Go changes with `gofmt`; use its standard tab indentation. Keep package names lowercase, exported identifiers in PascalCase, and unexported identifiers in camelCase. Use descriptive filenames such as `validation.go` and `validation_test.go`. Follow existing context-aware interfaces and return errors with actionable configuration or operation details.

## Testing Guidelines

Tests use Go's `testing` package, local HTTP servers, and temporary Git repositories. Name tests `TestBehavior` and cover observable behavior, including failure paths. No coverage percentage is currently specified. Run `go test ./...` and `go vet ./...` before submitting code changes.

The live test requires credentials and consumes API tokens: in PowerShell, set `$env:SLOP_LIVE = '1'`, then run `go test ./tests/integration -run TestLiveInference -v` only when explicitly intended.

## Commit & Pull Request Guidelines

The repository has no commits yet, so no historical convention exists. Use concise imperative subjects, such as `Fix repository branch validation`. PRs should describe the resulting behavior, reference relevant issues, and report validation commands and results. Include terminal screenshots when changing visible TUI behavior.

## Security & Configuration

Never commit API keys, proxy credentials, local `config.yaml`, or private task history. Preserve credential omission from snapshots and persistence. Keep generated Python validation limited to syntax checks; preserve source working trees and normal fast-forward publication behavior.
