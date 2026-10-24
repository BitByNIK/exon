# Getting started

## 1. Make your config repo (once)

Your config repo holds what exon sets up. It's a normal GitHub repo with this layout:

```
your-config/
├── exon.toml          # which apps to install
└── home/              # your config files, laid out like your home folder
    ├── .zshrc
    ├── .tmux.conf
    └── .config/
        └── starship.toml
```

A file at `home/.config/starship.toml` ends up at `~/.config/starship.toml`.

A minimal `exon.toml`:

```toml
[apps]
formulae = ["git", "gh", "tmux"]
casks    = ["google-chrome", "wezterm"]
```

All options: [Configuration](configuration.md). A full real example: [bitbynik/exon-config](https://github.com/bitbynik/exon-config).

Make the repo **public**. exon downloads it without logging in to GitHub.
Don't put passwords, tokens or keys in it.

## 2. On a fresh Mac

1. Install Homebrew. This also installs Apple's command line tools, which include git:
   ```sh
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
   ```
   Follow the "Next steps" it prints, so new terminal windows find `brew`.
2. Install exon:
   ```sh
   brew install bitbynik/tap/exon
   ```
3. Look first. This changes nothing:
   ```sh
   exon setup your-name/your-config --dry-run
   ```
4. Run it for real:
   ```sh
   exon setup your-name/your-config
   ```
   exon checks your Mac, shows the plan, and asks `Apply these changes?`.
   It may ask a few more questions, for example what to do with an app you already installed by hand.
5. Sign in to your apps yourself: GitHub (`gh auth login`), your password manager, and so on.
   exon never handles logins.

## Running only part of it

Add `apps` or `configs` to do just that part:

```sh
exon setup your-name/your-config configs   # only copy config files
exon setup your-name/your-config apps      # only install apps
```

Running exon again is safe: anything already done is skipped.

## Using a folder instead of GitHub

Pass a path. Handy while you're editing your config:

```sh
exon setup ~/Projects/your-config --dry-run
```
