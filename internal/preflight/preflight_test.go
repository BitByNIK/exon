package preflight

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/sys"
)

type fakeHost struct {
	os, arch string
	euid     int
	free     uint64
	offline  bool
	paths    map[string]string
}

func (h fakeHost) OS() string                       { return h.os }
func (h fakeHost) Arch() string                     { return h.arch }
func (h fakeHost) Euid() int                        { return h.euid }
func (h fakeHost) FreeBytes(string) (uint64, error) { return h.free, nil }
func (h fakeHost) LookPath(n string) (string, error) {
	if p, ok := h.paths[n]; ok {
		return p, nil
	}
	return "", exec.ErrNotFound
}

func (h fakeHost) Online(context.Context) error {
	if h.offline {
		return errors.New("no route")
	}
	return nil
}

func goodHost() fakeHost {
	return fakeHost{
		os: "darwin", arch: "arm64", euid: 501, free: 100 << 30,
		paths: map[string]string{"brew": "/opt/homebrew/bin/brew", "git": "/usr/bin/git"},
	}
}

func homeWithZprofile(t *testing.T, content string) string {
	home := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(home, ".zprofile"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func find(rs []Result, name string) (Result, bool) {
	for _, r := range rs {
		if r.Name == name {
			return r, true
		}
	}
	return Result{}, false
}

func TestRun(t *testing.T) {
	const shellenv = `eval "$(/opt/homebrew/bin/brew shellenv zsh)"`
	tests := []struct {
		name     string
		host     func(h *fakeHost)
		zprofile string
		network  bool
		check    string
		want     Status
		hint     string // substring expected in the hint
	}{
		{name: "all good", check: "Homebrew", want: OK, zprofile: shellenv},
		{name: "intel mac", host: func(h *fakeHost) { h.arch = "amd64" }, check: "Apple Silicon Mac", want: Fail},
		{name: "linux", host: func(h *fakeHost) { h.os = "linux" }, check: "Apple Silicon Mac", want: Fail},
		{name: "root", host: func(h *fakeHost) { h.euid = 0 }, check: "Not running as root", want: Fail, hint: "without sudo"},
		{name: "no brew", host: func(h *fakeHost) { delete(h.paths, "brew") }, check: "Homebrew", want: Fail, hint: "install.sh"},
		{name: "brew elsewhere", host: func(h *fakeHost) { h.paths["brew"] = "/usr/local/bin/brew" }, check: "Homebrew", want: Warn},
		{name: "zprofile missing", check: "Homebrew in ~/.zprofile", want: Warn, hint: "brew shellenv"},
		{name: "no git", host: func(h *fakeHost) { delete(h.paths, "git") }, check: "git", want: Fail, hint: "xcode-select"},
		{name: "offline", host: func(h *fakeHost) { h.offline = true }, network: true, check: "Internet connection", want: Fail},
		{name: "low disk", host: func(h *fakeHost) { h.free = 2 << 30 }, check: "Free disk space", want: Warn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := goodHost()
			if tt.host != nil {
				tt.host(&h)
			}
			rs := Run(context.Background(), h, Options{
				Home: homeWithZprofile(t, tt.zprofile), FS: sys.OSFS{},
				NeedNetwork: tt.network, MinFreeBytes: 10 << 30,
			})
			r, ok := find(rs, tt.check)
			if !ok {
				t.Fatalf("no %q check in %+v", tt.check, rs)
			}
			if r.Status != tt.want {
				t.Errorf("status = %v, want %v (%+v)", r.Status, tt.want, r)
			}
			if !strings.Contains(r.Hint, tt.hint) {
				t.Errorf("hint %q does not contain %q", r.Hint, tt.hint)
			}
			if Failed(rs) != (tt.want == Fail) {
				t.Errorf("Failed() = %v", Failed(rs))
			}
		})
	}
}

func TestNetworkSkippedWhenNotNeeded(t *testing.T) {
	h := goodHost()
	h.offline = true
	rs := Run(context.Background(), h, Options{Home: t.TempDir(), FS: sys.OSFS{}})
	if _, ok := find(rs, "Internet connection"); ok {
		t.Error("network was checked although not needed")
	}
}
