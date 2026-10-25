#!/bin/sh
# Gives this container read-only access to the Mac's other projects. The dev container mounts the
# folder that holds your projects at /mnt/projects (read-only); this links each project in it to
# /workspaces/<name>, the path that project has in its own container. Tools that remember projects
# by path (omp sessions, the code index) then find the same files everywhere.
#
# Run by init. Safe to run again. PROJECTS and WORKSPACES can be set to try it elsewhere.
set -u

PROJECTS=${PROJECTS:-/mnt/projects}
WORKSPACES=${WORKSPACES:-/workspaces}

[ -d "$PROJECTS" ] || { echo "link-workspaces: $PROJECTS is not mounted" >&2; exit 1; }

# /workspaces belongs to root in most images, and may not exist yet. Use sudo only when the folder
# to change (it, or the one above it when it is missing) is not ours.
writable() {
  [ -w "$WORKSPACES" ] || { [ ! -e "$WORKSPACES" ] && [ -w "$(dirname "$WORKSPACES")" ]; }
}
as_root() {
  if writable; then "$@"; else sudo "$@"; fi
}

[ -d "$WORKSPACES" ] || as_root mkdir -p "$WORKSPACES" || exit 1

for dir in "$PROJECTS"/*/; do
  [ -d "$dir" ] || continue
  target="$WORKSPACES/$(basename "$dir")"
  # A real folder here is this container's own project: leave it alone.
  if [ -e "$target" ] && [ ! -L "$target" ]; then continue; fi
  as_root ln -sfn "${dir%/}" "$target" || exit 1
done
