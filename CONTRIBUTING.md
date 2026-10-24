# Contributing

## Setup

Development happens in a dev container, so you don't need Go on your Mac.
Open the repo in an editor that supports dev containers and choose to open it in the container.
The image (`.devcontainer/Dockerfile`) includes Go and every tool below, at fixed versions.

## Everyday commands

Run these in the container:

| Command | What it does |
|---|---|
| `make check` | Everything CI runs: lint, tests, build |
| `make test` | Tests, with Go's race detector |
| `make lint` | golangci-lint, and a Markdown check (rumdl) |
| `make fmt` | Format all Go (gofumpt) and Markdown (rumdl) files |
| `make build` | Build `bin/exon` for Apple Silicon Macs |

exon only runs on a Mac, not in the container. After `make build`, run it from a Mac terminal:

```sh
./bin/exon setup ~/Projects/exon-config --dry-run
```

To try real changes without touching your own files, point `HOME` at a throwaway folder:

```sh
HOME=/tmp/exon-test ./bin/exon setup ~/Projects/exon-config configs
```

## Tests

- Never run real `brew` or touch the real home folder in tests.
- Fake commands with `internal/sys/systest.Fake`; use temp folders for files (`internal/step/steptest`).
- Test what a user would notice: what gets planned, retried, skipped or reported, and exit codes.

## Formatting on commit

When you commit, a git hook formats the Go and Markdown files you're committing and adds the
formatted versions to the commit. Changes you haven't staged are left alone.

The hook is set up automatically when the dev container is created (`lefthook.yml`).
Commit from the container: on the Mac the formatters aren't installed, so the hook only prints
`Can't find lefthook in PATH` and commits without formatting. `make lint` catches anything missed.

## Commit messages

Start with a type, so release notes can be generated from them:

| Prefix | For |
|---|---|
| `feat:` | Something new for users |
| `fix:` | A bug fix |
| `docs:` | Documentation only |
| `build:`, `chore:` | Tooling, dependencies, housekeeping |

Example: `feat(apps): ask before replacing apps installed by hand`

## Coding agents

`AGENTS.md` gives coding agents the project rules. `.omp/` holds omp's language server and
editor settings. The container already has the tools they use: gopls, golangci-lint-langserver
and delve.

## More

- [Architecture](docs/architecture.md): how the code fits together, and how to add a step
- [Decisions](docs/decisions.md): why things are the way they are
