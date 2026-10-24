# exon

Go CLI that sets up a freshly reset Mac from a config repo. How the code fits together:
`docs/architecture.md`. Why things are the way they are: `docs/decisions.md` (don't undo these casually).

## Environment

- Development runs in the devcontainer (Linux arm64). exon itself only runs on macOS/Apple Silicon:
  `make build` cross-compiles `bin/exon` for darwin/arm64; run it on the Mac, not in the container.
- Preflight fails on Linux by design, so don't try to run `exon setup` in the container. Use tests.

## Commands

| Task | Command |
|---|---|
| Everything CI runs | `make check` (lint, race tests, darwin build) |
| Tests | `make test` (`go test -race ./...`, needs cgo) |
| One package | `go test -race ./internal/step/apps/` |
| Lint | `make lint` (golangci-lint v2, config `.golangci.yml`) |
| Format | `make fmt` (gofumpt) |

## Rules

- Steps (`internal/step/<name>/`) never call `os/exec` or write files via `os`: only through
  `step.Env` (`Run.Query` for reads, `Run.Exec` for changes, `FS` for files). `Prepare` gets a
  read-only Env; any write there is a bug and fails the plan.
- Steps report progress via `env.Report` (Start/Done/Skip/Fail/Notice), never by printing.
- Errors shown to users are `usererr.New(msg, hint, cause)`. Anything else is logged, not shown.
- A per-item failure is reported and the step continues; returning an error fails the whole step.
- New step: follow "Adding a step" in `docs/architecture.md`, and document it in
  `docs/how-it-works.md` and `docs/configuration.md`.
- Docs describe only what exists. Planned work goes in the README's "Coming soon" list.
- Everything is under `internal/`: no public API to keep stable.

## Tests

- Commands are faked with `internal/sys/systest.Fake`; files use real temp folders
  (`internal/step/steptest.Env`). Never touch the real home folder or run real `brew`.
- Test behaviour users would notice (plan kinds, retries, skip/fail outcomes, exit codes), not wiring.
