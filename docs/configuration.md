# Configuration

A config repo has three parts: `exon.toml` at the top, the `home/` folder for your Mac, and the
`toolbox/` folder for dev containers.

## `exon.toml`

Each section turns on one part: `[apps]`, `[configs]`, `[git]`, `[ssh]` and `[tmux]` are used by
`exon setup`, `[dev]` by `exon dev`, plus a `[checklist]` of reminders. Leave a section out and
that part is skipped. A typo in a section or key name is an error, so mistakes don't go unnoticed.

### `[apps]`

```toml
[apps]
taps     = ["migsilva89/imark"]
formulae = ["git", "gh", "tmux"]
casks    = ["google-chrome", "wezterm", "font-meslo-lg-nerd-font"]
```

| Key | What it is | Example |
|---|---|---|
| `formulae` | Command line tools | `"git"`, `"tmux"` |
| `casks` | Apps and fonts | `"google-chrome"`, `"font-jetbrains-mono"` |
| `taps` | Extra package sources, for apps not in Homebrew's main list | `"migsilva89/imark"` |

Find names with `brew search <name>`.
For an app from an extra source, write its full name: `"migsilva89/imark/imark"`.

### `[configs]`

```toml
[configs]
source = "home"
```

| Key | What it is | Default |
|---|---|---|
| `source` | The folder in your repo that holds your config files | `"home"` |

### `[git]`

```toml
[git]
set_name_and_email = true
```

| Key | What it is | Default |
|---|---|---|
| `set_name_and_email` | Ask for the name and email git puts on your commits | `false` |

Your answers are saved on your Mac only, never in the config repo.

### `[ssh]`

```toml
[ssh]
ask_for_hosts = true
ask_about_1password = true
```

| Key | What it is | Default |
|---|---|---|
| `ask_for_hosts` | Ask for SSH hosts to add to `~/.ssh/config` | `false` |
| `ask_about_1password` | Ask whether to use the 1Password SSH agent | `false` |

### `[tmux]`

```toml
[tmux]
install_plugin_manager = true
```

| Key | What it is | Default |
|---|---|---|
| `install_plugin_manager` | Install tmux's plugin manager (tpm) | `false` |

### `[dev]`

```toml
[dev]
tools = ["omp", "gh", "fzf", "eza"]
```

| Key | What it is | Default |
|---|---|---|
| `tools` | The tools `exon dev install` and `exon dev update` look after | All of them |

The tools: `omp` (the omp coding agent), `codebase-memory-mcp` (its code index), `gh` (GitHub's
command line), `fzf` (the fuzzy finder), `eza` (a modern `ls`) and `oh-my-bash` (the shell
framework). `exon setup` ignores this section.

### `[checklist]`

```toml
[checklist]
items = [
  "gh auth login --web",
  "1Password → Settings → Developer → turn on the SSH agent",
]
```

| Key | What it is |
|---|---|
| `items` | Things to do by hand, printed at the end of every run |

## The `home/` folder

It mirrors your home folder. Every file in it is copied to the same place under `~`:

| In your repo | On your Mac |
|---|---|
| `home/.zshrc` | `~/.zshrc` |
| `home/.config/starship.toml` | `~/.config/starship.toml` |
| `home/.ssh/config` | `~/.ssh/config` |

- Folders are created as needed.
- `.DS_Store` and `.gitkeep` files are ignored.
- Files under `.ssh/` are made private, because SSH refuses config files others can read.

What not to put in it:

- Passwords, tokens, keys, or files that contain them.
- Files that apps rewrite with login data, like `~/.config/gh/hosts.yml`.

## The `toolbox/` folder

Starter files for the shared toolbox that dev containers use ([Dev containers](dev-containers.md)).
It has the same layout as the toolbox volume. `exon dev install` copies a file in only if the
volume doesn't have it yet, so changes made inside a container are kept.

| In your repo | In the toolbox |
|---|---|
| `toolbox/state/omp/agent/config.yml` | omp's settings in every container (`~/.omp/agent/config.yml`) |
| `toolbox/shell/bashrc` | Your shell settings. Every container's `~/.bashrc` loads it last, so it wins over the image's own |
| `toolbox/shell/inputrc` | Your readline settings (`~/.inputrc`) |

Files under `bin/`, `lib/` or `versions/` are ignored, with a warning: exon manages those.
Don't put logins in it.
