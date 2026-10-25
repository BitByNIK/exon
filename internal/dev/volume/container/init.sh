#!/bin/sh
# Per-container setup, run from a dev container's post-create script as the container's own user:
#
#   /opt/toolbox/init
#
# It makes this container use the shared toolbox volume: each tool's login folder in the home folder
# becomes a link into the volume, so a login made in one container is there in all of them; the
# toolbox programs go on PATH; your shell settings load; the Mac's other projects appear read-only
# under /workspaces. Safe to run again. `exon dev install` puts this file in the volume.
#
# TOOLBOX, HOME and PROJECTS can be set to try it elsewhere; in a container they are the defaults.
set -u

T=${TOOLBOX:-/opt/toolbox}
PROJECTS=${PROJECTS:-/mnt/projects}

if [ ! -d "$T/state" ]; then
  echo "toolbox: $T is not mounted in this container." >&2
  echo "  The container needs \"source=toolbox,target=$T,type=volume\" in \"mounts\"." >&2
  exit 1
fi
# Logins live in the volume, so it belongs to one user: the Mac user. Every container must run as them.
if [ ! -O "$T/state" ]; then
  echo "toolbox: the volume belongs to another user ID than this container's ($(id -u))." >&2
  echo "  Add \"updateRemoteUserUID\": true to the devcontainer.json and rebuild the container." >&2
  exit 1
fi

# link <target> <path>: an existing link is repointed; an empty folder is replaced; anything else is
# left alone and reported, because it may hold something that exists nowhere else.
link() {
  if [ -L "$2" ]; then
    ln -sfn "$1" "$2"
  elif [ -d "$2" ] && [ -z "$(ls -A "$2")" ]; then
    rmdir "$2" && ln -sn "$1" "$2"
  elif [ -e "$2" ]; then
    echo "toolbox: $2 already exists and is not a link; move it aside and run this again." >&2
    return 1
  else
    mkdir -p "$(dirname "$2")" && ln -sn "$1" "$2"
  fi
}

status=0
linked=""

# $T/links says what to link, one per line: dir|<name in state/>|<path in the home folder>
# or file|<name in state/>|<path in the home folder>.
if [ -f "$T/links" ]; then
  while IFS='|' read -r kind state home <&3; do
    case "$kind" in
      dir) mkdir -p "$T/state/$state" ;;
      file) mkdir -p "$(dirname "$T/state/$state")" && : >>"$T/state/$state" ;;
      *) continue ;;
    esac || { status=1; continue; }
    if link "$T/state/$state" "$HOME/$home"; then
      linked="$linked ~/$home"
    else
      status=1
    fi
  done 3<"$T/links"
fi

if [ -f "$T/shell/inputrc" ]; then
  if link "$T/shell/inputrc" "$HOME/.inputrc"; then linked="$linked ~/.inputrc"; else status=1; fi
fi

# Two small blocks in ~/.bashrc, each added once.
# 1. PATH, as the FIRST lines: Debian's ~/.bashrc returns early for non-interactive shells, and login
#    shells reset PATH before reading it, so the line must come before that return.
# 2. The user's shell settings, LAST, so they override whatever the image set up.
rc="$HOME/.bashrc"
touch "$rc"
if ! grep -qxF '# >>> exon toolbox path >>>' "$rc"; then
  tmp=$(mktemp "$HOME/.exon-bashrc.XXXXXX") || exit 1
  {
    printf '%s\n' '# >>> exon toolbox path >>>'
    printf 'case ":$PATH:" in *:%s/bin:*) ;; *) export PATH="%s/bin:$PATH" ;; esac\n' "$T" "$T"
    printf '%s\n\n' '# <<< exon toolbox path <<<'
    cat "$rc"
  } >"$tmp" && cat "$tmp" >"$rc"
  rm -f "$tmp"
fi
if ! grep -qxF '# >>> exon toolbox shell >>>' "$rc"; then
  {
    printf '\n%s\n' '# >>> exon toolbox shell >>>'
    printf '[ -f "%s/shell/bashrc" ] && . "%s/shell/bashrc"\n' "$T" "$T"
    printf '%s\n' '# <<< exon toolbox shell <<<'
  } >>"$rc"
fi

# The Mac's other projects, read-only, under the paths their own containers use.
if [ -d "$PROJECTS" ]; then
  sh "$T/link-workspaces" || status=1
fi

[ -n "$linked" ] && echo "toolbox: linked$linked"
exit "$status"
