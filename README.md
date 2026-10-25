# exon

Personal setup tooling, driven by a config repo you own.

You keep a small **config repo** on GitHub: the apps you want, your config files and a few
settings. exon reads it and does the setup. It always shows its plan and asks before it changes anything.

## What it does today

- **Sets up a freshly reset Mac**: installs your apps, copies your config files, sets your git name
  and email, adds SSH hosts, installs tmux's plugin manager, and ends with a checklist of things to
  sign in to.
- **Gives your dev containers a shared toolbox**: your tools, their logins and your shell settings
  live in one Docker volume, so you install and sign in once for every container.

```sh
exon setup your-name/your-config        # a Mac
exon dev install your-name/your-config  # the toolbox for dev containers
```

`--dry-run` shows the plan and changes nothing. Files that exon replaces on your Mac are backed up
first. exon never handles your logins, passwords or keys: you sign in to things yourself.

Works on **Apple Silicon Macs**. Setting up a Mac needs **Homebrew**; dev containers need **Docker**.

## Quick start

1. Install Homebrew:

   ```sh
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
   ```

2. Install exon:

   ```sh
   brew install bitbynik/tap/exon
   ```

3. Run it with your config repo:

   ```sh
   exon setup your-name/your-config
   ```

New here? Read [Getting started](docs/getting-started.md). For dev containers, read
[Dev containers](docs/dev-containers.md).

## Docs

| | |
|---|---|
| [Getting started](docs/getting-started.md) | From a fresh Mac to done, and how to make your config repo |
| [Configuration](docs/configuration.md) | What goes in `exon.toml`, `home/` and `toolbox/` |
| [How it works](docs/how-it-works.md) | What `exon setup` does, in order, and how it handles tricky cases |
| [Dev containers](docs/dev-containers.md) | The shared toolbox: install, add a project, update, back up |
| [Troubleshooting](docs/troubleshooting.md) | Logs, backups, errors, exit codes |
| [Contributing](CONTRIBUTING.md) | Working on exon itself |

## License

[MIT](LICENSE)
