// Package volume is how exon reaches the shared toolbox volume: the Docker
// volume that holds the tools, their logins and the user's shell settings for
// every dev container. All of it goes through the docker command, so nothing
// is installed on the Mac.
package volume

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

const (
	// DefaultName is the volume every dev container mounts.
	DefaultName = "toolbox"
	// MountPoint is where the volume appears inside containers.
	MountPoint = "/opt/toolbox"
	// Image runs the throwaway containers that fill the volume. It is Debian
	// with git and GNU tar, so nothing needs to be installed on the Mac, and
	// it is the base image of most dev containers, so it is often there already.
	Image = "mcr.microsoft.com/devcontainers/base:trixie"
)

// Volume is the toolbox volume, reached through docker.
type Volume struct {
	Run sys.Runner
	FS  sys.FS
	// Name of the Docker volume; DefaultName unless a test uses another.
	Name string
	// Owner is the "uid:gid" the files in the volume belong to: the Mac user,
	// because every dev container runs as that user.
	Owner string
}

// New returns the toolbox volume, owned by the given Mac user.
func New(run sys.Runner, fs sys.FS, uid, gid int) Volume {
	return Volume{Run: run, FS: fs, Name: DefaultName, Owner: fmt.Sprintf("%d:%d", uid, gid)}
}

// Check makes sure docker is installed and running.
func (v Volume) Check(ctx context.Context) error {
	if _, err := v.Run.LookPath("docker"); err != nil {
		return usererr.New("docker not found",
			"Install OrbStack or Docker Desktop, then try again.", err)
	}
	if _, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{"info", "--format", "{{.ServerVersion}}"}}); err != nil {
		return usererr.New("Docker is not running",
			"Start OrbStack or Docker Desktop, then try again.", err)
	}
	return nil
}

// Exists reports whether the volume has been created. Asking never creates it.
func (v Volume) Exists(ctx context.Context) (bool, error) {
	res, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{
		"volume", "ls", "--quiet", "--filter", "name=^" + v.Name + "$",
	}})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(res.Stdout)) == v.Name, nil
}

// Installed returns the installed version of each tool, by tool name. A
// volume that does not exist yet has nothing installed.
func (v Volume) Installed(ctx context.Context) (map[string]string, error) {
	exists, err := v.Exists(ctx)
	if err != nil || !exists {
		return map[string]string{}, err
	}
	// Mounting a volume that does not exist would create it, hence the check above.
	res, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{
		"run", "--rm", "-v", v.Name + ":" + MountPoint + ":ro", Image, "sh", "-c", listVersions,
	}})
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(res.Stdout))
	for sc.Scan() {
		if name, version, ok := strings.Cut(sc.Text(), "|"); ok && name != "" {
			versions[name] = version
		}
	}
	return versions, sc.Err()
}

// listVersions prints "name|version" for every tool the volume has a version recorded for.
const listVersions = `for f in ` + MountPoint + `/versions/*; do [ -f "$f" ] && printf '%s|%s\n' "${f##*/}" "$(cat "$f")"; done; true`

// OpenSessions returns the names of running containers that have the
// codebase-memory MCP running. Replacing the program under them is safe, but
// the MCP refuses to start in a container of another version while an older
// one is open elsewhere, so the user should close them first.
func (v Volume) OpenSessions(ctx context.Context) ([]string, error) {
	res, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{
		"ps", "--filter", "volume=" + v.Name, "--format", "{{.Names}}",
	}})
	if err != nil {
		return nil, err
	}
	var open []string
	for _, name := range strings.Fields(string(res.Stdout)) {
		_, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{
			"exec", name, "pgrep", "-f", "codebase-memory-mcp",
		}})
		var exit *sys.ExitError
		switch {
		case err == nil:
			open = append(open, name)
		case errors.As(err, &exit) && (exit.Code == 1 || exit.Code == 126 || exit.Code == 127):
			// 1: pgrep found nothing. 126/127: the container has no pgrep, so it can't be checked.
		default:
			return nil, err
		}
	}
	return open, nil
}
