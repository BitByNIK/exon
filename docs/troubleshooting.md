# Troubleshooting

## Where to look

| What | Where |
|---|---|
| Log of every run (commands and their full output) | `~/.local/state/exon/logs/` (last 10 kept) |
| Files exon replaced | `~/.local/state/exon/backups/<date and time>/` |
| Your config repo, as downloaded | `~/.local/share/exon/repos/<owner>/<repo>/` |
| Downloads in progress (removed after each run) | `~/.cache/exon/stage/` |
| Toolbox backups | `~/Backups/exon/` |

The end of each run prints the log path. When asking for help, share that log.

To undo a replaced file, copy it back from the backups folder.

## Common problems

| Message | Fix |
|---|---|
| `Homebrew: not found` | Install Homebrew (command shown), then run exon again |
| `Homebrew in ~/.zprofile` warning | Add the line exon shows, then open a new terminal |
| `some packages in [apps] can't be installed` | Fix the names in `exon.toml`. Search with `brew search <name>` |
| `unknown section` / `unknown key` | A typo in `exon.toml`; the message lists the valid names |
| `could not clone` | Check the repo name and that it's public. For a private repo, clone it yourself and pass the folder |
| `could not update the config repo` | Delete the folder the message names, then run exon again |
| `not running in a terminal` | Run exon in a terminal, or add `--yes` to accept the defaults |
| `a folder exists here; not touched` | Move or delete that folder, then run exon again |

Dev containers (`exon dev`):

| Message | Fix |
|---|---|
| `docker not found` | Install OrbStack or Docker Desktop, then run exon again |
| `Docker is not running` | Start OrbStack or Docker Desktop, then run exon again |
| `GitHub is limiting requests from your network` | Wait until the time it shows. A run uses only a few of the 60 requests an hour |
| `<tool> has no download for Linux on Apple Silicon` | That release has no build for it. Pick another with `--version` |
| `the volume belongs to another user ID` (from `init`, inside a container) | Add `"updateRemoteUserUID": true` to the container's `devcontainer.json`, then rebuild it |
| `<path> already exists and is not a link` (from `init`) | Move it aside, since it may hold something you need, then run `init` again |
| `the toolbox already holds logins and settings` | Add `--force` to `exon dev restore`, if replacing files of the same name is what you want |
| `this backup is not safe to restore` | The file is not one exon made, or was changed. Don't restore it |
| `there is no toolbox volume to back up` | Run `exon dev install` first |
| A `close first` warning in the plan | Close your omp sessions in the containers it names, then run the update again |

After fixing something, run the same command again. Finished work is skipped.

## Exit codes

For scripts:

| Code | Meaning |
|---|---|
| `0` | Everything worked (or a dry run found no problems) |
| `1` | Finished, but something failed, or the plan has problems |
| `2` | The checks failed; nothing was changed |
| `130` | Stopped with Ctrl+C |
