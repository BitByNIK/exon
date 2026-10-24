# Decisions

Why exon works the way it does. Read this before proposing to change one of these.

## Runs once, after a reset. No syncing.

exon sets a Mac up and then gets out of the way. It doesn't track your configs or sync them back.
To change what the next reset gets, edit your config repo.
Syncing tools exist (chezmoi, yadm); exon stays small by not being one.

## Copies files, doesn't link them

Many dotfile tools use symlinks. Apps like gh, omp and editors often replace a symlink with a
normal file when they save settings, silently breaking the link. Plain copies can't break, are easy
to understand, and keep working if the config repo folder moves.

## Never handles logins

No passwords, tokens or keys pass through exon or the config repo. That keeps a public config
repo safe and keeps exon simple. Users sign in to apps themselves after setup.

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
