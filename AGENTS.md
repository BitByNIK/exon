# exon

Go CLI of personal setup tooling, driven by a config repo: it sets up a freshly reset Mac (`exon setup`)
and keeps a shared toolbox for dev containers (`exon dev`). How the code fits together:
`docs/architecture.md`. Why things are the way they are: `docs/decisions.md` (don't undo these casually).

## Environment

- Development runs in the devcontainer (Linux arm64). exon itself only runs on macOS/Apple Silicon:
  `make build` cross-compiles `bin/exon` for darwin/arm64; run it on the Mac, not in the container.
- Preflight fails on Linux by design, so don't try to run `exon setup` in the container. Use tests.
- `exon dev` commands also need Docker running on the Mac. Try them with the cross-built binary; use
  a throwaway project folder, and take a `exon dev backup` before trying anything on the real toolbox volume.

## Commands

| Task | Command |
|---|---|
| Everything CI runs | `make check` (lint, race tests, darwin build) |
| Tests | `make test` (`go test -race ./...`, needs cgo) |
| One package | `go test -race ./internal/step/apps/` |
| Lint | `make lint` (golangci-lint v2 `.golangci.yml`; rumdl for Markdown, `.rumdl.toml`) |
| Format | `make fmt` (gofumpt + rumdl). A pre-commit hook (lefthook) also formats staged files |

## Rules

- Steps (`internal/step/<name>/`) never call `os/exec` or write files via `os`: only through
  `step.Env` (`Run.Query` for reads, `Run.Exec` for changes, `FS` for files). `Prepare` gets a
  read-only Env; any write there is a bug and fails the plan.
- Steps report progress via `env.Report` (Start/Done/Skip/Fail/Notice), never by printing.
- Errors shown to users are `usererr.New(msg, hint, cause)`. Anything else is logged, not shown.
- A per-item failure is reported and the step continues; returning an error fails the whole step.
- The network goes through `env.Net` (`sys.Downloader`), which is read-only while planning: `Get`
  works, `Download` fails. Never use `net/http` outside `internal/sys`.
- A dev step that reads the Docker volume while planning must check that it exists first: mounting
  a volume that does not exist creates it. `Apply` builds its own `volume.Volume` from its own
  `Env`; the one from `Prepare` can only read.
- New step: follow "Adding a step" in `docs/architecture.md`, and document it in
  `docs/how-it-works.md` and `docs/configuration.md`. New dev tool: "Adding a tool" there.
- Docs describe only what exists today. Planned work belongs in GitHub issues, not in docs.
- Everything is under `internal/`: no public API to keep stable.

## Git workflow

- Work on an issue, on a branch named `issue-<N>` created from `dev`. Commit inside the container.
- One pull request per issue, into `dev`, changing at most 10 files. Split bigger work.
- PR title starts with a type: `feat:`, `fix:`, `docs:`, `build:`, `chore:`, `ci:`, `refactor:`,
  `test:` or `perf:`. Put `Closes #<N>` in the description.
- CI (`.github/workflows/ci.yml`) enforces these and must be green before merging.
- Never push to `main` or `dev` directly, and never create release tags: releases are the
  maintainer's call (see CONTRIBUTING.md).

## Tests

- Commands are faked with `internal/sys/systest.Fake`, the network with `systest.FakeDownloader`; files use
  real temp folders (`internal/step/steptest.Env`). Never touch the real home folder, run real `brew`,
  or start real containers.
- Test behaviour users would notice (plan kinds, retries, skip/fail outcomes, exit codes), not wiring.
- The container scripts `init.sh` and `link-workspaces.sh` (`internal/dev/volume/container/`) are tested by
  running the real file with `sh` against temp folders. A change to one needs a test that fails without it.
- `internal/dev/volume/install.sh` runs as root on `/opt/toolbox` and `/stage`, so it has no unit test.
  Try a change to it for real, against a scratch volume, before committing.
