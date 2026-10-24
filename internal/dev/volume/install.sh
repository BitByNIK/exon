#!/bin/sh
# Runs inside a throwaway container, as root, started by exon (volume/install.go). It puts the tools
# listed in /stage/plan into the toolbox volume at /opt/toolbox, then copies the starter files in
# /seed that the volume does not have yet, then hands the volume to the user in $OWNER.
#
# /stage/plan has one tool per line, fields separated by "|":  kind|name|version|file|member
#   program  a file used as it is
#   archive  a .tar.gz holding the program; member is its path inside the archive
#   clone    a git repository; file is the repository address
#   state    a folder for the tool's login files (name only)
#
# Answers, one per line on stdout:  ok|name   fail|name|why   seeded|path   skipped|path
set -u

A=/opt/toolbox
: "${OWNER:?OWNER is not set}"
umask 022
mkdir -p "$A/bin" "$A/lib" "$A/versions" "$A/state" "$A/shell" || exit 1

# Scratch space inside the volume, so every final `mv` is a rename on one filesystem: a program that
# is running in a dev container keeps its old copy instead of failing with "Text file busy".
work=$(mktemp -d "$A/.install.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT

ok() { printf 'ok|%s\n' "$1"; }
fail() { printf 'fail|%s|%s\n' "$1" "$2"; }

# put <file> <destination>: make it runnable and move it into place in one step.
put() { chmod 755 "$1" && mv -f "$1" "$2"; }

# record <name> <version>: remember which version is installed.
record() { printf '%s\n' "$2" >"$work/v.$1" && mv -f "$work/v.$1" "$A/versions/$1"; }

# git: this clone belongs to exon and is never edited, and root runs it on files that belong to the
# Mac user, so git's ownership check is switched off for these commands.
git() { command git -c safe.directory='*' "$@"; }

install_program() { # name file
  cp "/stage/$2" "$work/$1" && put "$work/$1" "$A/bin/$1"
}

install_archive() { # name file member
  d=$(mktemp -d "$work/x.XXXXXX") && tar xzf "/stage/$2" -C "$d" "$3" && put "$d/$3" "$A/bin/$1"
}

install_clone() { # name address
  dir="$A/lib/$1"
  if [ -d "$dir/.git" ]; then
    git -C "$dir" fetch -q --depth 1 origin HEAD && git -C "$dir" reset -q --hard FETCH_HEAD
  else
    rm -rf "$work/c.$1" && git clone -q --depth 1 "$2" "$work/c.$1" && rm -rf "$dir" && mv "$work/c.$1" "$dir"
  fi
}

if [ -f /stage/plan ]; then
  while IFS='|' read -r kind name version file member <&3; do
    status=0
    case "$kind" in
      program) err=$(install_program "$name" "$file" 2>&1 </dev/null) || status=1 ;;
      archive) err=$(install_archive "$name" "$file" "$member" 2>&1 </dev/null) || status=1 ;;
      clone)
        err=$(install_clone "$name" "$file" 2>&1 </dev/null) || status=1
        [ "$status" = 0 ] && version=$(git -C "$A/lib/$name" rev-parse --short HEAD)
        ;;
      state)
        mkdir -p "$A/state/$name"
        continue
        ;;
      *)
        fail "$name" "unknown kind $kind"
        continue
        ;;
    esac
    if [ "$status" = 0 ]; then
      if record "$name" "$version"; then ok "$name"; else fail "$name" "could not record the version"; fi
    else
      fail "$name" "$(printf '%s' "$err" | tail -n 1)"
    fi
  done 3</stage/plan
fi

# Starter files from the config repo's toolbox/ folder mirror the volume's layout. A file that is
# already there is kept (it may hold changes made inside a container). bin/ and versions/ are
# managed by exon, so starter files never go there.
if [ -d /seed ]; then
  (cd /seed && find . -type f ! -name .DS_Store) | while IFS= read -r f; do
    rel=${f#./}
    case "$rel" in
      bin/* | versions/*) printf 'skipped|%s\n' "$rel" && continue ;;
    esac
    [ -e "$A/$rel" ] && continue
    mkdir -p "$(dirname "$A/$rel")" && cp "/seed/$rel" "$A/$rel" && printf 'seeded|%s\n' "$rel"
  done
fi

# The volume holds login tokens: it belongs to the Mac user, and state/ to nobody else.
if ! { chown -R "$OWNER" "$A" && chmod 755 "$A" "$A/bin" "$A/lib" && chmod -R go-rwx "$A/state"; }; then
  fail '*' 'could not set the owner of the toolbox'
  exit 1
fi
