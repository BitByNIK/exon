package release_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/release"
	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
	"github.com/bitbynik/exon/internal/usererr"
)

type asset struct{ name, digest string }

// answer builds GitHub's release data for a tag with the given files.
func answer(tag string, assets ...asset) []byte {
	type a struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest,omitempty"`
	}
	out := struct {
		Tag    string `json:"tag_name"`
		Assets []a    `json:"assets"`
	}{Tag: tag}
	for _, x := range assets {
		out.Assets = append(out.Assets, a{x.name, "https://example.test/" + tag + "/" + x.name, int64(len(x.name)), x.digest})
	}
	b, _ := json.Marshal(out)
	return b
}

func fake(url string, body []byte) *systest.FakeDownloader {
	return &systest.FakeDownloader{Files: map[string][]byte{url: body}}
}

const ghLatest = "https://api.github.com/repos/cli/cli/releases/latest"

func TestLatestTakesTagAddressAndChecksumFromTheRelease(t *testing.T) {
	dl := fake(ghLatest, answer("v2.103.2",
		asset{"gh_2.103.2_checksums.txt", "sha256:1111"},
		asset{"gh_2.103.2_linux_amd64.tar.gz", "sha256:2222"},
		asset{"gh_2.103.2_linux_arm64.deb", "sha256:3333"},
		asset{"gh_2.103.2_linux_arm64.tar.gz", "sha256:ABCDEF"},
	))
	r, err := release.Latest(context.Background(), dl, tool.GH())
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v2.103.2" {
		t.Errorf("Tag = %s, want v2.103.2", r.Tag)
	}
	if want := "https://example.test/v2.103.2/gh_2.103.2_linux_arm64.tar.gz"; r.URL != want {
		t.Errorf("URL = %s, want %s (the exact file, not a look-alike)", r.URL, want)
	}
	if r.SHA256 != "abcdef" {
		t.Errorf("SHA256 = %q, want abcdef (lowercase, without the prefix)", r.SHA256)
	}
	if want := int64(len("gh_2.103.2_linux_arm64.tar.gz")); r.Size != want {
		t.Errorf("Size = %d, want %d", r.Size, want)
	}
}

func TestEachToolPicksItsOwnFile(t *testing.T) {
	for _, tl := range []tool.Tool{tool.OMP(), tool.CodebaseMemory(), tool.GH(), tool.FZF(), tool.EZA()} {
		t.Run(tl.Name, func(t *testing.T) {
			tag := "v1.2.3"
			want := tl.Asset(tag)
			dl := fake("https://api.github.com/repos/"+tl.Repo+"/releases/latest", answer(tag,
				asset{want + ".bundle", "sha256:00"}, // look-alikes around the real file
				asset{want, "sha256:aa"},
				asset{"other-" + want, "sha256:00"},
			))
			r, err := release.Latest(context.Background(), dl, tl)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(r.URL, "/"+want) || r.SHA256 != "aa" {
				t.Errorf("got URL %s and checksum %s, want the file %s with checksum aa", r.URL, r.SHA256, want)
			}
		})
	}
}

func TestAtAsksForThatTagAndAcceptsItWithoutV(t *testing.T) {
	dl := fake("https://api.github.com/repos/cli/cli/releases/tags/v2.100.0", answer("v2.100.0",
		asset{"gh_2.100.0_linux_arm64.tar.gz", "sha256:aa"}))
	r, err := release.At(context.Background(), dl, tool.GH(), "2.100.0")
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v2.100.0" {
		t.Errorf("Tag = %s, want v2.100.0", r.Tag)
	}
}

func TestMissingOrUnusableChecksumIsLeftEmpty(t *testing.T) {
	for name, digest := range map[string]string{"none": "", "other algorithm": "sha512:abc"} {
		t.Run(name, func(t *testing.T) {
			dl := fake(ghLatest, answer("v2.103.2", asset{"gh_2.103.2_linux_arm64.tar.gz", digest}))
			r, err := release.Latest(context.Background(), dl, tool.GH())
			if err != nil {
				t.Fatal(err)
			}
			if r.SHA256 != "" {
				t.Errorf("SHA256 = %q, want empty so the caller can warn", r.SHA256)
			}
		})
	}
}

func TestErrorsAreWrittenForTheUser(t *testing.T) {
	limited := http.Header{}
	limited.Set("X-RateLimit-Remaining", "0")
	limited.Set("X-RateLimit-Reset", "1792870000")

	tests := []struct {
		name string
		dl   *systest.FakeDownloader
		call func(*systest.FakeDownloader) error
		msg  string
		hint string
	}{
		{"no releases", &systest.FakeDownloader{}, latest, "has no releases", ""},
		{"unknown version", &systest.FakeDownloader{}, at("9.9.9"), "has no release v9.9.9", "version number"},
		{"rate limit", &systest.FakeDownloader{Errs: map[string]error{
			ghLatest: &sys.StatusError{URL: ghLatest, Code: 403, Header: limited},
		}}, latest, "limiting requests", "Try again after"},
		{"forbidden for another reason", &systest.FakeDownloader{Errs: map[string]error{
			ghLatest: &sys.StatusError{URL: ghLatest, Code: 403, Header: http.Header{}},
		}}, latest, "could not look up", "internet connection"},
		{"unreadable answer", fake(ghLatest, []byte("<html>")), latest, "was not understood", "few minutes"},
		{
			"no build for this platform", fake(ghLatest, answer("v2.103.2", asset{"gh_2.103.2_linux_amd64.tar.gz", ""})),
			latest, "no download for Linux on Apple Silicon", "gh_2.103.2_linux_arm64.tar.gz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ue, ok := usererr.As(tt.call(tt.dl))
			if !ok {
				t.Fatal("want a user error")
			}
			if !strings.Contains(ue.Msg, tt.msg) || !strings.Contains(ue.Hint, tt.hint) {
				t.Errorf("message %q, hint %q; want them to contain %q and %q", ue.Msg, ue.Hint, tt.msg, tt.hint)
			}
		})
	}
}

func latest(dl *systest.FakeDownloader) error {
	_, err := release.Latest(context.Background(), dl, tool.GH())
	return err
}

func at(tag string) func(*systest.FakeDownloader) error {
	return func(dl *systest.FakeDownloader) error {
		_, err := release.At(context.Background(), dl, tool.GH(), tag)
		return err
	}
}

func TestACloneToolHasNoReleaseAndNeedsNoNetwork(t *testing.T) {
	dl := &systest.FakeDownloader{} // any request would fail
	for name, call := range map[string]func() (release.Release, error){
		"latest": func() (release.Release, error) { return release.Latest(context.Background(), dl, tool.OhMyBash()) },
		"at":     func() (release.Release, error) { return release.At(context.Background(), dl, tool.OhMyBash(), "v1") },
	} {
		t.Run(name, func(t *testing.T) {
			r, err := call()
			if err != nil {
				t.Fatal(err)
			}
			if r.Tag != "" || r.URL != "" || r.SHA256 != "" {
				t.Errorf("got %+v, want everything empty", r)
			}
		})
	}
}
