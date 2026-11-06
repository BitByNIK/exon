package toolbox_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys/systest"
)

const (
	claudeOnly   = "[dev]\ntools = [\"claude\"]\n"
	claudeServer = "https://downloads.claude.ai/claude-code-releases"
	claudeSum    = "a7e687fd1cb054d651c7588223de3786c99d4c682365e3c3ae1e59ce7f3a591f"
)

// anthropic pretends to be Anthropic's release server: the newest version is 2.1.319,
// 2.1.300 is an older one, and each has a 5 MB file.
func anthropic() *systest.FakeDownloader {
	dl := &systest.FakeDownloader{Files: map[string][]byte{claudeServer + "/latest": []byte("2.1.319\n")}}
	for _, v := range []string{"2.1.319", "2.1.300"} {
		dl.Files[fmt.Sprintf("%s/%s/manifest.json", claudeServer, v)] = []byte(fmt.Sprintf(
			`{"version":%q,"platforms":{"linux-arm64":{"binary":"claude","checksum":%q,"size":%d}}}`, v, claudeSum, 5<<20))
		dl.Files[fmt.Sprintf("%s/%s/linux-arm64/claude", claudeServer, v)] = []byte("bytes of claude " + v)
	}
	return dl
}

func TestClaudeInstallsFromItsOwnServerWithALauncher(t *testing.T) {
	d := &docker{installOut: "ok|claude\nok|container\n"}
	p, env, rec, err := prepare(t, d, anthropic(), claudeOnly)
	if err != nil {
		t.Fatal(err)
	}
	got := itemsOf(p)
	if it := got["install claude"]; it.Kind != step.Add || it.Detail != "2.1.319, 5 MB" {
		t.Errorf("install claude = %+v, want an Add with the version and size in %v", it, p.Items())
	}
	if w, ok := got["unverified claude"]; ok {
		t.Errorf("%+v: the manifest gives a checksum, so nothing is unverified", w)
	}

	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	if rec.Outcomes()["claude"] != "done" {
		t.Errorf("outcomes = %v, want claude done", rec.Outcomes())
	}
	var fetched []string
	for _, f := range env.Net.(*systest.FakeDownloader).Fetches() {
		fetched = append(fetched, f.URL+" "+f.SHA256)
	}
	if want := claudeServer + "/2.1.319/linux-arm64/claude " + claudeSum; !slices.Equal(fetched, []string{want}) {
		t.Errorf("downloads = %v, want %v (checked against the manifest's checksum)", fetched, want)
	}
	if !strings.Contains(d.stage.plan, "launched|claude|2.1.319|claude.download|claude.launcher\n") {
		t.Errorf("the container saw plan %q", d.stage.plan)
	}
	if !slices.Contains(d.stage.files, "claude.download") || !slices.Contains(d.stage.files, "claude.launcher") {
		t.Errorf("the container saw files %v, want the program and its launcher", d.stage.files)
	}
}

// Versions on this server have no "v", so a chosen version works with or without one.
func TestClaudeUpdateGoesToTheChosenVersion(t *testing.T) {
	d := &docker{volume: true, image: true, versions: "claude|2.1.319\n" + containerFor(tool.Claude())}
	p, _, _, err := prepareUpdate(t, d, anthropic(), claudeOnly, []string{"claude"}, "v2.1.300")
	if err != nil {
		t.Fatal(err)
	}
	if it := itemsOf(p)["update claude"]; it.Kind != step.Change || it.Detail != "2.1.319 → 2.1.300, 5 MB" {
		t.Errorf("plan = %v, want claude moved back to the chosen version", p.Items())
	}
}

func TestClaudeUpdateLeavesTheNewestAlone(t *testing.T) {
	d := &docker{volume: true, image: true, versions: "claude|2.1.319\n" + containerFor(tool.Claude())}
	p, _, _, err := prepareUpdate(t, d, anthropic(), claudeOnly, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if it := itemsOf(p)["installed claude"]; it.Kind != step.Same || len(p.Items()) != 1 {
		t.Errorf("plan = %v, want claude unchanged", p.Items())
	}
}
