// Package source finds the config repo: a local folder, or a GitHub repo
// cloned over HTTPS.
package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

// Source is a parsed SOURCE argument.
type Source struct {
	Dir   string // local folder (absolute), for local sources
	Owner string // GitHub owner, for remote sources
	Repo  string // GitHub repo, for remote sources
}

// Remote reports whether the source is a GitHub repo.
func (s Source) Remote() bool { return s.Owner != "" }

// URL is the HTTPS clone URL of a remote source.
func (s Source) URL() string { return "https://github.com/" + s.Owner + "/" + s.Repo + ".git" }

func (s Source) String() string {
	if s.Remote() {
		return s.Owner + "/" + s.Repo
	}
	return s.Dir
}

var (
	shortRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9-]*)/([A-Za-z0-9._-]+)$`)
	urlRe   = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9][A-Za-z0-9-]*)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)
)

// Parse interprets arg as a local path or a GitHub repo. Paths win when the
// argument looks like one (starts with /, ., ~) or names an existing folder.
func Parse(arg, home string) (Source, error) {
	if strings.HasPrefix(arg, "~/") || arg == "~" {
		arg = filepath.Join(home, strings.TrimPrefix(arg, "~"))
	}
	looksLocal := strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, ".")
	if !looksLocal {
		if info, err := os.Stat(arg); err == nil && info.IsDir() {
			looksLocal = true
		}
	}
	if looksLocal {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return Source{}, err
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return Source{}, usererr.New("config folder not found: "+abs, "", err)
		}
		return Source{Dir: abs}, nil
	}
	for _, re := range []*regexp.Regexp{urlRe, shortRe} {
		if m := re.FindStringSubmatch(arg); m != nil {
			return Source{Owner: m[1], Repo: m[2]}, nil
		}
	}
	return Source{}, usererr.New(
		fmt.Sprintf("can't tell what %q is", arg),
		"Pass a GitHub repo (owner/repo or https://github.com/owner/repo) or a folder path (./my-config, ~/my-config).",
		nil)
}

// Fetch returns a folder containing the config repo. Local sources are used
// as they are. Remote ones are cloned into cacheDir/owner/repo, or updated if
// already there. With temp set (dry run), the clone goes to a temporary folder
// that cleanup removes.
func Fetch(ctx context.Context, s Source, run sys.Runner, cacheDir string, temp bool) (dir string, cleanup func(), err error) {
	noop := func() {}
	if !s.Remote() {
		return s.Dir, noop, nil
	}
	// Never let git ask for credentials: exon only reads public repos.
	gitEnv := []string{"GIT_TERMINAL_PROMPT=0"}
	retry := func(c sys.Cmd) error {
		var res sys.Result
		return sys.Retry(ctx, 3, 2*time.Second,
			func(err error) bool { return sys.IsNetworkError(err, res.Output()) },
			func() error {
				var err error
				res, err = run.Exec(ctx, c)
				return err
			})
	}

	if temp {
		tmp, err := os.MkdirTemp("", "exon-config-*")
		if err != nil {
			return "", noop, err
		}
		cleanup = func() { _ = os.RemoveAll(tmp) }
		dir = filepath.Join(tmp, s.Repo)
		if err := retry(sys.Cmd{Name: "git", Args: []string{"clone", "--depth", "1", s.URL(), dir}, Env: gitEnv}); err != nil {
			cleanup()
			return "", noop, cloneError(s, err)
		}
		return dir, cleanup, nil
	}

	dir = filepath.Join(cacheDir, s.Owner, s.Repo)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if err := retry(sys.Cmd{Name: "git", Args: []string{"-C", dir, "pull", "--ff-only"}, Env: gitEnv}); err != nil {
			return "", noop, usererr.New("could not update the config repo in "+dir,
				"Delete that folder and run exon again to clone it fresh.", err)
		}
		return dir, noop, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", noop, err
	}
	if err := retry(sys.Cmd{Name: "git", Args: []string{"clone", "--depth", "1", s.URL(), dir}, Env: gitEnv}); err != nil {
		return "", noop, cloneError(s, err)
	}
	return dir, noop, nil
}

func cloneError(s Source, err error) error {
	return usererr.New("could not clone "+s.String(),
		"Check the name and that the repo is public. For a private repo, clone it yourself and pass the folder: exon setup ./"+s.Repo,
		err)
}
