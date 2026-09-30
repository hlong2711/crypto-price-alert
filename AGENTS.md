# Repository Guidelines

## Project Structure

This repository is a Go backend. The executable starts in `cmd/server`; application packages live under `internal/` (API, chat adapters, configuration, database, domain, market providers, notifications, repositories, scheduling, and services). Package tests are kept beside source as `*_test.go`. Runtime settings and examples are in `configs/`, environment variable examples are in `.env.example`, and design notes are in `docs/plans/`. Docker build and local orchestration files are at the repository root.

## Build, Test, and Development

- `go run ./cmd/server` starts the service locally; configure it with `configs/config.yaml` and environment variables.
- `go test ./...` runs all package tests.
- `go test ./internal/chat/...` runs the chat packages' tests while iterating on adapters or commands.
- `go build ./...` checks that all packages compile.
- `docker compose config` validates Compose settings; `docker compose up --build` builds and starts the app with PostgreSQL.

## Coding Style

Use standard Go formatting: tabs for indentation and `gofmt` before submitting changes (`gofmt -w path/to/file.go`). Keep packages focused under `internal/`, use short, descriptive lowercase package names, and follow Go exported-name conventions for public types and functions. Add configuration fields to the appropriate config types and update the matching example YAML or `.env.example` when needed. Keep secrets out of committed files.

## Testing

Use Go's built-in `testing` package. Name test files `*_test.go` and test functions `TestThing`. Add or update focused package tests for behavior changes, including error paths and configuration validation where relevant, then run `go test ./...` before submitting.

## Commits and Pull Requests

Recent history uses concise conventional prefixes such as `feat:`, `fix:`, and `docs:`. Use the same pattern and describe the change in the subject (for example, `fix: validate chat webhook settings`). Pull requests should explain the behavior changed, note relevant configuration or deployment implications, link related plans or issues when applicable, and report the tests run. Include screenshots only when a change has a visual interface.

## Configuration and Deployment

Use `.env.example` as the source for required environment variables and keep real credentials in an untracked `.env`. Chat webhooks require externally reachable HTTPS endpoints; review the Telegram and Slack validation settings before enabling them in a deployment.
