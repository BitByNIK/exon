# exon

Set up a freshly reset Mac from a config repo: install your apps, place your config files, answer a few questions, done.

> Status: in design. Nothing to install yet. See [docs/design.md](docs/design.md).

## What it will do

- Install your Homebrew packages, apps and fonts
- Copy your config files (zsh, Starship, WezTerm, tmux, omp, git, ssh) into place, backing up anything it replaces
- Ask for personal values (git name/email, SSH hosts) instead of storing them in the repo
- Show a plan first; `--dry-run` shows it without changing anything
- Never touch logins or credentials: it ends with a checklist of what to sign into by hand

Apple Silicon only. Requires Homebrew.

## License

[MIT](LICENSE)
