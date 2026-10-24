# Troubleshooting

## Where to look

| What | Where |
|---|---|
| Log of every run (commands and their full output) | `~/.local/state/exon/logs/` (last 10 kept) |
| Files exon replaced | `~/.local/state/exon/backups/<date and time>/` |
| Your config repo, as downloaded | `~/.local/share/exon/repos/<owner>/<repo>/` |

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

After fixing something, run the same command again. Finished work is skipped.

## Exit codes

For scripts:

| Code | Meaning |
|---|---|
| `0` | Everything worked (or a dry run found no problems) |
| `1` | Finished, but something failed, or the plan has problems |
| `2` | The checks failed; nothing was changed |
| `130` | Stopped with Ctrl+C |
