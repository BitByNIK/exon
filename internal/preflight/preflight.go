// Package preflight checks the Mac before any step runs.
package preflight

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/bitbynik/exon/internal/sys"
)

// Status is a check result.
type Status int

// Check results.
const (
	OK Status = iota
	Warn
	Fail
)

// Result is one check.
type Result struct {
	Name   string
	Status Status
	Detail string
	Hint   string
}

// Failed reports whether any check failed.
func Failed(rs []Result) bool {
	for _, r := range rs {
		if r.Status == Fail {
			return true
		}
	}
	return false
}

// Host is what the checks need to know about the machine.
type Host interface {
	OS() string
	Arch() string
	Euid() int
	FreeBytes(path string) (uint64, error)
	Online(ctx context.Context) error
	LookPath(name string) (string, error)
}

// Options select which checks apply.
type Options struct {
	Home         string
	FS           sys.FS
	NeedNetwork  bool // installs or a remote config repo
	MinFreeBytes uint64
	// SkipHomebrew leaves out the Homebrew checks, for commands that do not use it.
	SkipHomebrew bool
}

// BrewInstall is Homebrew's official install command.
const BrewInstall = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

// shellenvLine is what Homebrew's installer tells users to add to ~/.zprofile.
const shellenvLine = `eval "$(/opt/homebrew/bin/brew shellenv zsh)"`

// Run runs all checks and returns their results in order.
func Run(ctx context.Context, h Host, o Options) []Result {
	var rs []Result

	if h.OS() != "darwin" || h.Arch() != "arm64" {
		return append(rs, Result{
			Name: "Apple Silicon Mac", Status: Fail,
			Detail: fmt.Sprintf("this is %s/%s", h.OS(), h.Arch()),
			Hint:   "exon supports macOS on Apple Silicon only.",
		})
	}
	rs = append(rs, Result{Name: "Apple Silicon Mac", Status: OK})

	if h.Euid() == 0 {
		rs = append(rs, Result{
			Name: "Not running as root", Status: Fail,
			Hint: "Run exon as your normal user, without sudo. Homebrew refuses to run as root, and files would end up owned by root.",
		})
	}

	if !o.SkipHomebrew {
		rs = append(rs, checkHomebrew(o, h)...)
	}

	if _, err := h.LookPath("git"); err != nil {
		rs = append(rs, Result{Name: "git", Status: Fail, Detail: "not found", Hint: "Install the Xcode Command Line Tools: xcode-select --install"})
	}

	if o.NeedNetwork {
		if err := h.Online(ctx); err != nil {
			rs = append(rs, Result{Name: "Internet connection", Status: Fail, Detail: err.Error(), Hint: "Connect to the internet and try again."})
		} else {
			rs = append(rs, Result{Name: "Internet connection", Status: OK})
		}
	}

	if free, err := h.FreeBytes(o.Home); err == nil {
		gb := float64(free) / (1 << 30)
		r := Result{Name: "Free disk space", Status: OK, Detail: fmt.Sprintf("%.0f GB", gb)}
		if free < o.MinFreeBytes {
			r.Status = Warn
			r.Hint = fmt.Sprintf("Less than %.0f GB free; large apps may fail to install.", float64(o.MinFreeBytes)/(1<<30))
		}
		rs = append(rs, r)
	}
	return rs
}

// checkHomebrew checks that Homebrew is installed where expected and that
// new terminal windows will find it.
func checkHomebrew(o Options, h Host) []Result {
	brewPath, err := h.LookPath("brew")
	switch {
	case err != nil:
		return []Result{{Name: "Homebrew", Status: Fail, Detail: "not found", Hint: "Install it first:\n" + BrewInstall}}
	case brewPath != "/opt/homebrew/bin/brew":
		return []Result{
			{Name: "Homebrew", Status: Warn, Detail: "found at " + brewPath + ", expected /opt/homebrew/bin/brew"},
			checkShellenv(o),
		}
	default:
		return []Result{{Name: "Homebrew", Status: OK, Detail: brewPath}, checkShellenv(o)}
	}
}

// checkShellenv makes sure new terminal windows will find brew.
func checkShellenv(o Options) Result {
	data, err := o.FS.ReadFile(filepath.Join(o.Home, ".zprofile"))
	if err == nil && strings.Contains(string(data), "brew shellenv") {
		return Result{Name: "Homebrew in ~/.zprofile", Status: OK}
	}
	return Result{
		Name: "Homebrew in ~/.zprofile", Status: Warn,
		Detail: "new terminal windows won't find brew",
		Hint:   "Add it with:\necho '" + shellenvLine + "' >> ~/.zprofile",
	}
}

// OSHost is the real machine.
type OSHost struct{}

// OS returns runtime.GOOS.
func (OSHost) OS() string { return runtime.GOOS }

// Arch returns runtime.GOARCH.
func (OSHost) Arch() string { return runtime.GOARCH }

// Euid returns the effective user id.
func (OSHost) Euid() int { return syscall.Geteuid() }

// LookPath finds a command on PATH.
func (OSHost) LookPath(name string) (string, error) { return (&sys.OSRunner{}).LookPath(name) }

// FreeBytes returns the space available to the user on path's volume.
func (OSHost) FreeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil // Bsize is uint32 on darwin, int64 on linux
}

// Online checks that github.com is reachable.
func (OSHost) Online(ctx context.Context) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", "github.com:443")
	if err != nil {
		return fmt.Errorf("cannot reach github.com: %w", err)
	}
	return conn.Close()
}
