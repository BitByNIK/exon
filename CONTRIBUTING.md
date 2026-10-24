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

The dev container turns the hook on when it's created (`.githooks/pre-commit`, rules in
`lefthook.yml`). Commit from the container. Anywhere else the hook only prints that formatting
was skipped; `make lint` and CI catch anything missed.

## Branches and pull requests

| Branch | What it's for |
|---|---|
| `main` | Released code |
| `dev` | Finished work, waiting for the next release. The default branch |
| `issue-<N>` | Work on issue #N. One branch per issue |

1. Pick or open an issue, say #12.
2. Branch from `dev`: `git switch -c issue-12 dev`.
3. Open a pull request into `dev`. Write `Closes #12` in the description.
4. Merge once the checks are green.

Every pull request is checked automatically:

| Check | Rule |
|---|---|
| PR rules | Branch is named `issue-<N>`, the PR changes at most 10 files, the title starts with a type (below) |
| Lint, test, build | Lint, tests and build pass on a Mac, and exon does a dry run there |

`dev` is merged into `main` when it's time to release. In an emergency, a pull request from an
`issue-<N>` branch can go straight into `main`.

## PR titles and commit messages

Start with a type. Pull requests are merged as one commit titled like the PR, and the release
notes are built from these titles.

| Type | For |
|---|---|
| `feat:` | Something new for users |
| `fix:` | A bug fix |
| `docs:` | Documentation only |
| `build:`, `chore:`, `ci:`, `refactor:`, `test:`, `perf:` | Everything else |

Example: `feat(apps): ask before replacing apps installed by hand`

## Releasing

After merging into `main`, tag the version and push the tag:

```sh
git switch main && git pull
git tag v0.2.0
git push origin v0.2.0
```

GitHub then builds exon, publishes the release with a list of changes, and updates
[homebrew-tap](https://github.com/BitByNIK/homebrew-tap), so `brew upgrade` picks it up.
Pick the number from what changed: new features raise the middle number (`0.2.0` → `0.3.0`),
bug fixes only raise the last one (`0.2.0` → `0.2.1`).

Publishing needs the `HOMEBREW_TAP_TOKEN` secret in this repo: a GitHub token allowed to write
to `BitByNIK/homebrew-tap` only.

## Coding agents

`AGENTS.md` gives coding agents the project rules. `.omp/` holds omp's language server and
editor settings. The container already has the tools they use: gopls, golangci-lint-langserver
and delve.

## More

- [Architecture](docs/architecture.md): how the code fits together, and how to add a step
- [Decisions](docs/decisions.md): why things are the way they are
