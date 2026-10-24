// Package release works out which version of a tool to install, where to
// download it and what its checksum should be. All three come from one answer
// of GitHub's release data.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

const api = "https://api.github.com/repos/"

// Release is one version of a tool, ready to download.
type Release struct {
	Tool tool.Tool
	Tag  string // "v18.12.6"; empty for a Clone tool, which has no releases
	URL  string // where the release file is; empty for a Clone tool
	// SHA256 is the checksum GitHub computed for the file, as lowercase hex.
	// Empty when GitHub has none, which the caller should tell the user.
	SHA256 string
}

// Latest finds the newest release of t. A Clone tool has no releases and
// comes back empty.
func Latest(ctx context.Context, dl sys.Downloader, t tool.Tool) (Release, error) {
	if t.Kind == tool.Clone {
		return Release{Tool: t}, nil
	}
	return lookup(ctx, dl, t, api+t.Repo+"/releases/latest", "")
}

// At finds the release of t with the given tag. The leading "v" is optional:
// every tool tags its releases with one, so "18.12.6" means "v18.12.6".
func At(ctx context.Context, dl sys.Downloader, t tool.Tool, tag string) (Release, error) {
	if t.Kind == tool.Clone {
		return Release{Tool: t}, nil
	}
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return lookup(ctx, dl, t, api+t.Repo+"/releases/tags/"+tag, tag)
}

// answer is the part of GitHub's release data that is used here.
type answer struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"` // "sha256:<hex>"
	} `json:"assets"`
}

// lookup asks GitHub about one release. asked is the tag the user asked for, "" for the latest.
func lookup(ctx context.Context, dl sys.Downloader, t tool.Tool, url, asked string) (Release, error) {
	body, err := dl.Get(ctx, url)
	if err != nil {
		return Release{}, lookupError(t, asked, err)
	}
	var a answer
	if err := json.Unmarshal(body, &a); err != nil || a.Tag == "" {
		return Release{}, usererr.New("GitHub's answer about "+t.Name+" was not understood",
			"Try again in a few minutes.", err)
	}
	want := t.Asset(a.Tag)
	for _, asset := range a.Assets {
		if asset.Name == want {
			sum, ok := strings.CutPrefix(asset.Digest, "sha256:")
			if !ok { // missing, or some other kind of digest: not usable
				sum = ""
			}
			return Release{Tool: t, Tag: a.Tag, URL: asset.URL, SHA256: strings.ToLower(sum)}, nil
		}
	}
	return Release{}, usererr.New(
		t.Name+" "+a.Tag+" has no download for Linux on Apple Silicon",
		"Looked for a file named "+want+".", nil)
}

func lookupError(t tool.Tool, asked string, err error) error {
	if se, ok := errors.AsType[*sys.StatusError](err); ok {
		switch {
		case se.Code == http.StatusNotFound && asked != "":
			return usererr.New(t.Name+" has no release "+asked, "Check the version number.", err)
		case se.Code == http.StatusNotFound:
			return usererr.New(t.Name+" has no releases", "", err)
		case (se.Code == http.StatusForbidden || se.Code == http.StatusTooManyRequests) &&
			se.Header.Get("X-RateLimit-Remaining") == "0":
			return usererr.New("GitHub is limiting requests from your network", "Try again"+resetTime(se.Header)+".", err)
		}
	}
	return usererr.New("could not look up the "+t.Name+" release",
		"Check your internet connection and try again.", err)
}

// resetTime says when GitHub's limit is lifted, as " after 14:05", or nothing if unknown.
func resetTime(h http.Header) string {
	secs, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return ""
	}
	return " after " + time.Unix(secs, 0).Format("15:04")
}
