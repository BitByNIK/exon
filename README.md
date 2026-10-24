# exon

Set up a freshly reset Mac in one command.

You keep a small **config repo** on GitHub: the apps you want and your config files.
After a reset, exon reads it, installs your apps, and puts your config files in place.

```sh
exon setup your-name/your-config
```

- Shows you exactly what it will change and asks first. `--dry-run` only shows.
- Backs up any file it replaces.
- Never touches logins, passwords or keys. You sign in to things yourself.

Works on **Apple Silicon Macs** with **Homebrew** installed.

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

New here? Read [Getting started](docs/getting-started.md).

## Docs

| | |
|---|---|
| [Getting started](docs/getting-started.md) | From a fresh Mac to done, and how to make your config repo |
| [Configuration](docs/configuration.md) | What goes in `exon.toml` and the `home/` folder |
| [How it works](docs/how-it-works.md) | What exon does, in order, and how it handles tricky cases |
| [Troubleshooting](docs/troubleshooting.md) | Logs, backups, errors, exit codes |
| [Contributing](CONTRIBUTING.md) | Working on exon itself |

## License

[MIT](LICENSE)
