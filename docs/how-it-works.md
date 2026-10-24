# How it works

When you run `exon setup`, it:

1. **Checks** your Mac.
2. **Shows the plan**: everything it would change. Nothing has changed yet.
3. **Asks** `Apply these changes?`, then any other questions.
4. Does each part you set up in `exon.toml`, in this order:
   **installs apps** (`apps`), **copies config files** (`configs`), **sets your git name and email** (`git`),
   **adds SSH hosts** (`ssh`), **installs tmux's plugin manager** (`tmux`).
5. **Summarises** what was done, skipped or failed, then shows your **checklist** of things to do by hand.

If anything in the plan is wrong (for example a misspelled app name), exon stops before changing anything.

## Checks

| Check | If it fails |
|---|---|
| Apple Silicon Mac | Stops |
| Not run with `sudo` | Stops |
| Homebrew installed | Stops and shows the install command |
| git installed | Stops and shows how to install it |
| Internet connection | Stops (not checked for a dry run with a local folder) |
| Homebrew set up in `~/.zprofile` | Warns and shows the line to add |
| At least 10 GB free | Warns, you can continue |

## `apps`

Installs everything in `[apps]`.

**Before installing anything**, it looks up every name:

- A name Homebrew doesn't know, or an app Homebrew has disabled: exon stops, nothing is installed.
- An app Homebrew plans to remove, or one that was renamed: a warning in the plan.
- Already installed: skipped.

**Then** it updates Homebrew once and installs one package at a time.

| Situation | What exon does |
|---|---|
| The app is already in `/Applications`, installed by hand | Asks: **adopt** (keep it, let Homebrew manage it), **skip**, or **reinstall** |
| The app's installer needs your Mac password | Says so in the plan; type your password when macOS asks |
| Network trouble, or Homebrew is busy | Tries up to 4 times before giving up on that package |
| One package fails | Reports it and carries on with the rest |

## `configs`

Copies every file from `home/` to the same place under `~`.

| Situation | What exon does |
|---|---|
| File doesn't exist yet | Copies it |
| Same file already there | Skips it |
| Different file already there | Backs it up, then replaces it |
| A link (symlink) is there, e.g. from an older dotfiles setup | Backs up the link, then puts a real file there |
| A folder is there instead of a file | Leaves it alone and reports it |

Backups go to `~/.local/state/exon/backups/<date and time>/`, keeping their paths.
Files are written in one go, so a crash or Ctrl+C never leaves a half-written file.

## `git`

Only runs if `[git]` has `set_name_and_email = true`. Asks:

```text
Your name for git commits [Nikhil Gupta]:
Your email for git commits [nikhil@example.com]:
```

| Situation | What exon does |
|---|---|
| Already set | Shows the current values in brackets; Enter keeps them |
| Not set yet | Asks until you type something |
| Email doesn't look like one | Says so and asks again |
| Same answers as before | Changes nothing |
| With `--yes` and nothing set yet | Skips it, since there's nothing to answer with |

The answers are saved in `~/.gitconfig` (with `git config --global`), never in your config repo.
This runs after `configs`, so if your repo has a `.gitconfig` that replaces yours, your name and
email are put back.

## `ssh`

Only asks what `[ssh]` turns on. First, whether to use the 1Password SSH agent (if it's set up
already, Enter keeps it). Then SSH hosts, one after another:

```text
Add an SSH host? [y/N] y
  Name (what you'll type after ssh): pi
  Address (IP or host name): 192.168.1.100
  User (Enter for your Mac user name): pi
  Port [22]:
Add another SSH host? [y/N] n
```

| Situation | What exon does |
|---|---|
| The name is already in `~/.ssh/config` | Asks whether to replace it; if not, keeps the old one |
| Other hosts already there | Keeps them |
| 1Password agent chosen | Adds it for all hosts; says so if 1Password isn't installed yet |
| Name, address or port not valid | Says why and asks again |

The old `~/.ssh/config` is backed up first, and `~/.ssh` is kept private (SSH refuses config
files others can read). No keys are ever copied or created. This runs after `configs`, so hosts
are added to the `~/.ssh/config` from your repo, if it has one.

## `tmux`

If `[tmux]` has `install_plugin_manager = true`, installs tpm into `~/.tmux/plugins/tpm`.
Then press **prefix + I** in tmux to install the plugins listed in your `.tmux.conf`.

| Situation | What exon does |
|---|---|
| Already installed | Skips it |
| tmux not installed | Skips it, and says so |
| Network trouble | Tries up to 3 times before giving up |

## Checklist

The items in `[checklist]` are printed at the end of every run, under **Next, by hand**.
Use it for what exon doesn't do on purpose, like signing in to apps.

## Options

| Option | What it does |
|---|---|
| `--dry-run` | Checks and shows the plan, then stops. Changes nothing |
| `--yes`, `-y` | Doesn't ask `Apply these changes?`; questions get their default answer |
| `--verbose`, `-v` | Also shows the full output of every command |
