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
| `exon dev init [DIR]` | Adds a personal dev container to a project |
| `exon dev backup` | Saves your logins and settings to a file |
| `exon dev restore FILE` | Puts a backup back |

Like `exon setup`, they show a plan and ask first. `--dry-run` only shows, `--yes` doesn't ask,
`--verbose` shows every command's output.

## Set up

1. Install the tools, once:

   ```sh
   exon dev install your-name/your-config
   ```

   The first run downloads a small helper image (about 100 MB) and the tools (about 270 MB).
   Tools already in the toolbox are left alone.
2. In a project folder, add its personal dev container:

   ```sh
   exon dev init
   ```

   This makes `.devcontainer/personal/devcontainer.json` and `post-create.sh`, and adds them to
   the project's `.git/info/exclude`, so they are only yours and never committed. A project's own
   dev container, if it has one, is not touched. `--image` and `--user` start from another image
   (the default is the Debian dev container image).
3. Edit them: set the image the project needs, and add the project's own steps to the script.
4. Open the folder in your editor and choose "personal".
5. In any one container, sign in and set your git identity. Every container shares them:

   ```sh
   gh auth login --web --git-protocol https && gh auth setup-git
   git config --global user.name "Your Name" && git config --global user.email you@example.com
   omp   # then /login
   ```

`exon dev init` never overwrites a file you changed. Delete one to get a fresh copy.

## What the toolbox holds

| In the volume | What it is |
|---|---|
| `bin/`, `lib/` | The tools: omp, codebase-memory-mcp, gh, fzf, eza, and oh-my-bash |
| `state/` | Logins, git identity, settings, omp sessions and code indexes. Private to you |
| `shell/` | Your shell settings: `bashrc` and `inputrc` |
| the rest | What exon needs to manage the toolbox |

When a container is created, its `post-create.sh` runs `/opt/toolbox/init`. That links each tool's
login folder (`~/.omp`, `~/.config/gh`, `~/.cache/codebase-memory-mcp`) and `~/.gitconfig` into
`state/`, puts the tools on `PATH`, loads your shell settings, and links your other projects.

Your other projects appear **read-only** at `/workspaces/<name>`: the same path each has in its own
container, so omp sessions and code indexes line up. The project you opened stays writable.

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

A backup holds what you can't download again: logins, git identity, settings, omp sessions, code
indexes and your shell settings. It contains login tokens, so only you can read the file. Close
your omp sessions first for a clean copy; a database being written may be copied half-done.

A restore is checked first and refuses a file that writes outside the toolbox, or over the tools.
It won't write over a toolbox that already has logins unless you pass `--force`; then files of the
same name are replaced.

A Docker reset (or `docker volume rm toolbox`) deletes the volume. Back up before you reset.

## Limits

- Container images must be **Debian or Ubuntu** (not Alpine) with `git` and `sudo`: the tools are
  built for them.
- The toolbox belongs to your Mac user ID, because it holds logins. Every container must run as
  that user, which is what `"updateRemoteUserUID": true` in the generated file does.
- GitHub allows about 60 version lookups an hour per network without signing in, and a run uses a
  few. At the limit exon says when to try again.
- Downloads are checked against the checksum GitHub publishes. If GitHub has none for a file, the
  plan warns and the file is not checked.
