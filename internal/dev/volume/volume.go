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

	"github.com/bitbynik/exon/internal/dev/backup"
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

// UsedBy returns the names of the running containers that have the volume mounted.
func (v Volume) UsedBy(ctx context.Context) ([]string, error) {
	res, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{
		"ps", "--filter", "volume=" + v.Name, "--format", "{{.Names}}",
	}})
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(res.Stdout)), nil
}

// OpenSessions returns the names of running containers that have the
// codebase-memory MCP running. Replacing the program under them is safe, but
// the MCP refuses to start in a container of another version while an older
// one is open elsewhere, so the user should close them first.
func (v Volume) OpenSessions(ctx context.Context) ([]string, error) {
	names, err := v.UsedBy(ctx)
	if err != nil {
		return nil, err
	}
	var open []string
	for _, name := range names {
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

// RemoteHead returns the commit the default branch of a git repository is at.
// It asks from inside a container, so git runs there and not on the Mac.
func (v Volume) RemoteHead(ctx context.Context, url string) (string, error) {
	res, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{
		"run", "--rm", Image, "git", "ls-remote", url, "HEAD",
	}})
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(res.Stdout))
	if len(fields) == 0 || len(fields[0]) < 7 {
		return "", fmt.Errorf("git ls-remote %s: unexpected answer %q", url, res.Stdout)
	}
	return fields[0], nil
}

// ImagePresent reports whether the helper image is already on this machine,
// so the first install can say it is about to download it.
func (v Volume) ImagePresent(ctx context.Context) (bool, error) {
	_, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{"image", "inspect", "--format", "{{.Id}}", Image}})
	if _, isExit := errors.AsType[*sys.ExitError](err); isExit {
		return false, nil // docker answered: it has no such image
	}
	return err == nil, err
}

// PullImage downloads the helper image.
func (v Volume) PullImage(ctx context.Context) error {
	if _, err := v.Run.Exec(ctx, sys.Cmd{Name: "docker", Args: []string{"pull", "--quiet", Image}}); err != nil {
		return usererr.New("could not download the helper image",
			"Check your internet connection and that Docker is running, then try again.", err)
	}
	return nil
}

// Missing returns those of files (paths inside the volume, such as
// "shell/bashrc") that the volume does not have yet. Asking never creates the volume.
func (v Volume) Missing(ctx context.Context, files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	exists, err := v.Exists(ctx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return files, nil
	}
	args := append([]string{"run", "--rm", "-v", v.Name + ":" + MountPoint + ":ro", Image, "sh", "-c", listMissing, "sh"}, files...)
	res, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: args})
	if err != nil {
		return nil, err
	}
	if out := strings.TrimSpace(string(res.Stdout)); out != "" {
		return strings.Split(out, "\n"), nil
	}
	return nil, nil
}

// listMissing prints the arguments that are not files in the volume.
const listMissing = `for f; do [ -e "` + MountPoint + `/$f" ] || printf '%s\n' "$f"; done`

// HasData reports whether the volume holds anything of the user's in state/:
// a file that is not empty. The empty placeholders init makes do not count.
// Asking never creates the volume.
func (v Volume) HasData(ctx context.Context) (bool, error) {
	exists, err := v.Exists(ctx)
	if err != nil || !exists {
		return false, err
	}
	res, err := v.Run.Query(ctx, sys.Cmd{Name: "docker", Args: []string{
		"run", "--rm", "-v", v.Name + ":" + MountPoint + ":ro", Image,
		"sh", "-c", "find " + MountPoint + "/state -type f -size +0 2>/dev/null | head -n 1",
	}})
	return strings.TrimSpace(string(res.Stdout)) != "", err
}

// Backup writes the volume's own data (everything but what exon installs)
// to dir/file, readable by the Mac user only, because it holds login tokens.
// dir must exist and be somewhere Docker can see, such as under the home folder.
func (v Volume) Backup(ctx context.Context, dir, file string) error {
	script := `umask 077; part="/backup/.$1.part"; ` +
		"tar czf \"$part\" --anchored" + excludeFlags() + " -C " + MountPoint + ` . && mv "$part" "/backup/$1" || { rm -f "$part"; exit 1; }`
	_, err := v.Run.Exec(ctx, sys.Cmd{Name: "docker", Args: []string{
		"run", "--rm", "--user", v.Owner,
		"-v", v.Name + ":" + MountPoint + ":ro",
		"-v", dir + ":/backup",
		Image, "sh", "-c", script, "sh", file,
	}})
	if err != nil {
		return usererr.New("could not make the backup", "Check that Docker is running and that there is room for it in "+dir+".", err)
	}
	return nil
}

// excludeFlags is the tar flags that leave out what exon installs.
func excludeFlags() string {
	var b strings.Builder
	for _, name := range backup.Excluded {
		b.WriteString(" --exclude=./" + name)
	}
	return b.String()
}

// Restore unpacks the backup dir/file into the volume and gives the volume
// to the Mac user. Files in the backup replace those of the same name. The
// backup must have been checked with backup.Inspect first.
func (v Volume) Restore(ctx context.Context, dir, file string) error {
	script := `set -e; tar xzf "/backup/$1" -C ` + MountPoint + ` --no-same-owner; ` +
		`chown -R "$OWNER" ` + MountPoint + `; chmod 755 ` + MountPoint + `; ` +
		`[ ! -d ` + MountPoint + `/state ] || chmod -R go-rwx ` + MountPoint + `/state`
	_, err := v.Run.Exec(ctx, sys.Cmd{Name: "docker", Args: []string{
		"run", "--rm", "-e", "OWNER=" + v.Owner,
		"-v", v.Name + ":" + MountPoint,
		"-v", dir + ":/backup:ro",
		Image, "sh", "-c", script, "sh", file,
	}})
	if err != nil {
		return usererr.New("could not restore the backup", "Check that Docker is running, then try again.", err)
	}
	return nil
}
