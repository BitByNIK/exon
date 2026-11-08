# AGENTS.md

Go CLI of personal setup tooling, driven by a config repo: it sets up a freshly reset Mac (`exon setup`) and
keeps a shared toolbox for dev containers (`exon dev`). This file lists the rules you must not break.

## Rules

- exon only runs on macOS/Apple Silicon. `exon setup` fails on Linux by design (preflight), so don't run it
  there: use the tests and `make check`.
- To try an `exon dev` command, run the cross-built `bin/exon` on the Mac. It works on the real toolbox volume, so
  use a throwaway project folder and run `exon dev backup` first.
- Steps change the system only through `step.Env`: no `os/exec`, no writing files with `os`, no `net/http`
  outside `internal/sys`. `Prepare` gets a read-only Env, and a write there is a bug. They report through
  `env.Report`, never by printing, and show errors as `usererr.New(msg, hint, cause)`.
- A dev step that reads the Docker volume while planning must check that it exists first: mounting a volume that
  does not exist creates it.
- A new step or dev tool follows "Adding a step" or "Adding a tool" in `docs/architecture.md`, and is documented in
  `docs/how-it-works.md` and `docs/configuration.md`.
- The container scripts `init.sh` and `link-workspaces.sh` (`internal/dev/volume/container/`) are tested by running
  the real file with `sh` against temp folders. A change to one needs a test that fails without it.
- `internal/dev/volume/install.sh` runs as root on `/opt/toolbox` and `/stage`, so it has no unit test. Try a change
  to it for real, against a scratch volume, before committing.
- Docs describe only what exists today. Planned work belongs in GitHub issues.
- Never push to `main` or `dev` directly, and never create release tags: releases are the maintainer's call.

## Docs

- [CONTRIBUTING.md](CONTRIBUTING.md): the commands (`make check` runs everything CI runs), tests, branches, PR
  titles and releases.
- [docs/architecture.md](docs/architecture.md): how the code fits together.
- [docs/decisions.md](docs/decisions.md): why things are the way they are. Don't undo these casually.
