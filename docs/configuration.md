# Configuration

A config repo has two parts: `exon.toml` at the top, and the `home/` folder.

## `exon.toml`

`[apps]` lists what to install, `[configs]` where your config files are. Leave one out and that part is skipped.
A typo in a section or key name is an error, so mistakes don't go unnoticed.

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
