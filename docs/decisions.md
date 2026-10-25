# Decisions

Why exon works the way it does. Read this before proposing to change one of these.

## Runs once, after a reset. No syncing

exon sets a Mac up and then gets out of the way. It doesn't track your configs or sync them back.
To change what the next reset gets, edit your config repo.
Syncing tools exist (chezmoi, yadm); exon stays small by not being one. The same goes for the dev
toolbox: starter files are copied in only where the volume has none, and nothing is copied back.

## Copies files, doesn't link them

Many dotfile tools use symlinks. Apps like gh, omp and editors often replace a symlink with a
normal file when they save settings, silently breaking the link. Plain copies can't break, are easy
to understand, and keep working if the config repo folder moves.

## Never handles logins

No passwords, tokens or keys pass through exon or the config repo. That keeps a public config
repo safe and keeps exon simple. Users sign in themselves after setup: to apps on a Mac, and once,
in any one container, for dev containers.

## Requires Homebrew first

Homebrew's installer also installs Apple's command line tools (and so git). Making it a
requirement means exon doesn't need its own installer, and can itself be installed with
`brew install`.

## Apple Silicon Macs only

One platform to support and test. Intel Macs are no longer sold.

## No macOS system settings

Settings like Dock size or key repeat are stored under undocumented names that Apple changes between
macOS versions. Scripting them breaks quietly, and setting them by hand takes minutes.

## Shows a plan and asks first

Setting up a machine changes a lot. Showing everything before doing anything, and offering
`--dry-run`, makes exon safe to try and safe to run again.

## Written in Go

exon must run on a freshly reset Mac with nothing installed, so it has to be a single program
with no runtime to install. Go builds that easily, and has good libraries for terminal tools.

## Dev containers

### The toolbox is a Docker volume, not a folder on the Mac

A volume lives on the container engine's own disk. It is faster than a shared Mac folder, it has
real Linux permissions, and several containers can use the same SQLite databases at once, because
they run on one Linux kernel with no file-sharing layer between. The cost: a Docker reset deletes it,
which is why `exon dev backup` exists.

### The tools live in the volume, not in each image

Install once, sign in once, and every container has them. Images stay the team's or yours. The cost:
images must be Debian or Ubuntu with git and sudo, because the tools are built for them.

### Links in containers, copies on the Mac

"Copies files, doesn't link them" is about config files that an app rewrites. In a container the
logins live in the volume and `init` links the home folders (`~/.omp`, `~/.config/gh`) to them. Apps
write *inside* those folders, so nothing replaces the link. `~/.gitconfig` is a file link, and git
writes through it; two containers were checked to see the same name and email.

### One user ID everywhere

The volume holds login tokens, so it is private to your Mac user ID. Every container must run as that
user, which `"updateRemoteUserUID": true` does. `init` stops with a message if it doesn't.

### Versions aren't pinned

`exon dev install` takes the newest; only `exon dev update` changes a version later, and it shows
`old → new` first. `--version` goes to a chosen version, which is also the way back from a bad
release. A pin in the config repo would go stale and need upkeep.

### Downloads happen on the Mac, in pieces, and are checked

GitHub limits each connection, not the line. In tests one connection ran at about 1 MB/s, and eight at
once downloaded omp (218 MB) in under a minute. The checksum is the one GitHub itself publishes with
the release. The container only unpacks and runs git, so docker is the only thing that runs on the Mac.

### Other projects are read-only, at their own paths

omp names its session folders after the project path, and the code index names its indexes after it
too. Mounting each project at `/workspaces/<name>`, the path it has in its own container, keeps them
consistent. Read-only, so a tool in one container can't change another project.

### A backup leaves out what exon installs, and a restore never writes there

The tools can be downloaded again, and a backup file must not be able to replace them. A restore
also refuses paths that leave the toolbox and files written through a link.

### `init` writes new files and never edits yours

A `devcontainer.json` has comments and its own layout, and patching it is fragile. `exon dev init`
makes its own file next to the project's, keeps it out of git, and leaves any file you changed alone.
