package release

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

// A Launched tool's server answers <server>/latest with the newest version,
// and <server>/<version>/manifest.json with the checksum and size of each
// platform's file. The file itself is <server>/<version>/<platform>/<binary>.
var (
	versionPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	checksumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// manifest is the part of the server's manifest.json that is used here.
type manifest struct {
	Version   string `json:"version"`
	Platforms map[string]struct {
		Binary   string `json:"binary"`
		Checksum string `json:"checksum"`
		Size     int64  `json:"size"` // bytes
	} `json:"platforms"`
}

func latestFromServer(ctx context.Context, dl sys.Downloader, t tool.Tool) (Release, error) {
	body, err := dl.Get(ctx, t.Server+"/latest")
	if err != nil {
		return Release{}, lookupError(t, "", err)
	}
	version := strings.TrimSpace(string(body))
	if !versionPattern.MatchString(version) {
		return Release{}, notUnderstood(t, nil)
	}
	return fromManifest(ctx, dl, t, version, "")
}

func atOnServer(ctx context.Context, dl sys.Downloader, t tool.Tool, version string) (Release, error) {
	if !versionPattern.MatchString(version) {
		return Release{}, usererr.New(version+" is not a version number of "+t.Name, "Use numbers like 2.1.319.", nil)
	}
	return fromManifest(ctx, dl, t, version, version)
}

// fromManifest reads the manifest of one version. asked is the version the
// user asked for, "" for the latest.
func fromManifest(ctx context.Context, dl sys.Downloader, t tool.Tool, version, asked string) (Release, error) {
	body, err := dl.Get(ctx, t.Server+"/"+version+"/manifest.json")
	if err != nil {
		return Release{}, lookupError(t, asked, err)
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil || m.Version != version {
		return Release{}, notUnderstood(t, err)
	}
	p, ok := m.Platforms[t.Platform]
	if !ok || p.Binary == "" {
		return Release{}, usererr.New(
			t.Name+" "+version+" has no download for Linux on Apple Silicon",
			"Looked for the build "+t.Platform+".", nil)
	}
	// Nothing else vouches for this file, so a download without a checksum is not offered.
	if !checksumPattern.MatchString(p.Checksum) {
		return Release{}, usererr.New(
			"the release server gave no usable checksum for "+t.Name+" "+version,
			"Try again in a few minutes.", nil)
	}
	return Release{
		Tool:   t,
		Tag:    version,
		URL:    t.Server + "/" + version + "/" + t.Platform + "/" + p.Binary,
		Size:   p.Size,
		SHA256: p.Checksum,
	}, nil
}

func notUnderstood(t tool.Tool, cause error) error {
	return usererr.New("the release server's answer about "+t.Name+" was not understood",
		"Try again in a few minutes.", cause)
}
