# Dev containers

exon keeps your dev tools, their logins and your shell settings in one Docker volume called the
**toolbox**. Every dev container mounts it, so you install once and sign in once, and every
container has it.

Needs an Apple Silicon Mac with Docker running (OrbStack or Docker Desktop). `install` and `update`
also read your config repo (see [Getting started](getting-started.md)). Nothing else is installed
on the Mac.

| Command | What it does |
|---|---|
| `exon dev install SOURCE` | Puts the tools that are missing in the toolbox, plus your starter files |
| `exon dev update SOURCE [TOOL...]` | Brings tools to a newer version, or to one you choose |
| `exon dev init [DIR]` | Adds your own dev container to a project |
| `exon dev backup` | Saves your logins and settings to a file |
| `exon dev restore FILE` | Puts a backup back |

Like `exon setup`, they show a plan and ask first, with the same [options](how-it-works.md#options).

## Set up

1. Install the tools, once:

   ```sh
   exon dev install your-name/your-config
   ```

   The first run downloads a small helper image (about 100 MB) and the tools (about 270 MB).
   Tools already in the toolbox are left alone.
2. In a project folder, add your own dev container:

   ```sh
   exon dev init
   ```

   This makes `.devcontainer/local/devcontainer.json` and `post-create.sh`, and adds
   `.devcontainer/local/` to the project's `.gitignore`, so they are only yours. Commit that
   change. A project's own dev container, if it has one, is not touched.

   - If the project has a `.devcontainer/Dockerfile`, yours is built from it, and your script runs
     the project's own `.devcontainer/post-create.sh` after the toolbox init.
   - Otherwise yours starts from an image: the Debian dev container image, or the one you pass with
     `--image` (and `--user` for the user it runs as).
3. Edit them. Without a Dockerfile, set the image the project needs. Add your own steps to the
   script. A `devcontainer.json` cannot extend another one, so repeat the ports, environment
   variables and `runArgs` you need from the project's own `.devcontainer/devcontainer.json`.
4. Open the folder in your editor and choose `<project> (local)`.
5. In any one container, sign in and set your git identity. Every container shares them:

   ```sh
   gh auth login --web --git-protocol https && gh auth setup-git
   git config --global user.name "Your Name" && git config --global user.email you@example.com
   omp   # then /login
   claude   # then follow the prompt: open the address it prints on your Mac, paste the code back
   ```

`exon dev init` never overwrites a file you changed. Delete one to get a fresh copy.

## What the toolbox holds

| In the volume | What it is |
|---|---|
| `bin/`, `lib/` | The tools (the list is under [`[dev]`](configuration.md#dev)) |
| `state/` | Logins, git identity, settings, omp and Claude sessions and code indexes. Private to you |
| `shell/` | Your shell settings: `bashrc` and `inputrc` |
| the rest | What exon needs to manage the toolbox |

When a container is created, its `post-create.sh` runs `/opt/toolbox/init`. That links each tool's
login folder (`~/.omp`, `~/.config/gh`, `~/.cache/codebase-memory-mcp`) and `~/.gitconfig` into
`state/`, puts the tools on `PATH`, loads your shell settings, and links your other projects.

Claude Code has no link. `bin/claude` is a small launcher that starts the program in `lib/claude/`
with its login, settings and sessions in `state/claude`, so every container shares them. It also
blocks Claude's own updater, so every container runs the version in the volume: `exon dev update`
is the one way to change it. If `state/claude/mcp.json` exists, the launcher gives it to Claude as
its MCP servers; check them with `/mcp` inside Claude (`claude mcp list` does not show them). The
reasons are in [Decisions](decisions.md).

Your other projects appear **read-only** at `/workspaces/<name>`, the path each has in its own
container ([why](decisions.md)). The project you opened stays writable.

## Your config repo

`[dev]` in `exon.toml` picks the tools (all of them if left out), and the `toolbox/` folder holds
starter files. See [Configuration](configuration.md).

## Updating

```sh
exon dev update your-name/your-config                        # everything with a newer version
exon dev update your-name/your-config omp gh                 # just these
exon dev update your-name/your-config omp --version v18.8.7  # a version you choose, newer or older
```

The plan shows each change as `v18.8.7 → v18.12.6`. A tool that isn't installed is left to
`exon dev install`. Versions aren't pinned: `install` takes the newest, and `update` is the only
thing that changes them afterwards.

Replacing the codebase-memory MCP is safe for a session that has it open, but containers don't see
each other, so two of them could briefly run different versions on one index. When the MCP is about
to change, the plan names the containers that have it open. Close those sessions first.

## Backup and restore

```sh
exon dev backup                                                  # to ~/Backups/exon, or --to FOLDER
exon dev restore ~/Backups/exon/toolbox-2026-10-25_105503.tar.gz
exon dev install your-name/your-config                           # then: a backup has no tools
```

A backup holds what you can't download again: logins, git identity, settings, omp and Claude
sessions, code indexes and your shell settings. It contains login tokens, so only you can read the
file. Close your omp and Claude sessions first for a clean copy; a database being written may be
copied half-done.

A restore is checked first and refuses a file that writes outside the toolbox, or over the tools.
It won't write over a toolbox that already has logins unless you pass `--force`; then files of the
same name are replaced.

A Docker reset (or `docker volume rm toolbox`) deletes the volume. Back up before you reset.

## Limits

- Container images must be **Debian or Ubuntu** (not Alpine) with `git` and `sudo`: the tools are
  built for them.
- Every container must run as your Mac user ID, which `"updateRemoteUserUID": true` in the generated
  file does ([why](decisions.md)).
- GitHub allows about 60 version lookups an hour per network without signing in, and a run uses a
  few. At the limit exon says when to try again.
- Downloads are checked against the checksum GitHub publishes, or for Claude the one in Anthropic's
  manifest. If GitHub has none for a file, the plan warns and the file is not checked. Claude is
  never installed without one.
