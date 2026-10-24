package tool_test

import (
	"testing"

	"github.com/bitbynik/exon/internal/dev/tool"
)

// A tool that is missing a field fails halfway through an install, so the
// shape of the list is checked here.
func TestEveryToolIsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range tool.All() {
		if tl.Name == "" || tl.Summary == "" || tl.Repo == "" {
			t.Errorf("%+v: name, summary and repo are required", tl)
		}
		if seen[tl.Name] {
			t.Errorf("duplicate tool name %q", tl.Name)
		}
		seen[tl.Name] = true

		switch tl.Kind {
		case tool.Program:
			if tl.Asset == nil {
				t.Errorf("%s: a program needs an asset", tl.Name)
			}
		case tool.Archive:
			if tl.Asset == nil || tl.Member == nil {
				t.Errorf("%s: an archive needs an asset and a member", tl.Name)
			}
		case tool.Clone:
			if len(tl.Links) > 0 {
				t.Errorf("%s: a cloned folder has no login folders", tl.Name)
			}
		}
		for _, l := range tl.Links {
			if l.State == "" || l.Home == "" {
				t.Errorf("%s: link %+v needs both fields", tl.Name, l)
			}
		}
	}
}

func TestFind(t *testing.T) {
	if got, err := tool.Find("gh"); err != nil || got.Name != "gh" {
		t.Errorf("Find(gh) = %v, %v", got.Name, err)
	}
	if _, err := tool.Find("nope"); err == nil {
		t.Error("want an error for an unknown tool")
	}
}

// These are the real file names of the releases current on 24 Oct 2026; each
// answered 200 from GitHub when this was written.
func TestAssetNames(t *testing.T) {
	tests := []struct {
		tool          tool.Tool
		tag           string
		asset, member string
	}{
		{tool.OMP(), "v18.12.6", "omp-linux-arm64", ""},
		{tool.CodebaseMemory(), "v0.12.1", "codebase-memory-mcp-linux-arm64.tar.gz", "codebase-memory-mcp"},
		{tool.GH(), "v2.103.2", "gh_2.103.2_linux_arm64.tar.gz", "gh_2.103.2_linux_arm64/bin/gh"},
		{tool.FZF(), "v0.74.5", "fzf-0.74.5-linux_arm64.tar.gz", "fzf"},
		{tool.EZA(), "v0.23.5", "eza_aarch64-unknown-linux-gnu.tar.gz", "./eza"},
	}
	for _, tt := range tests {
		t.Run(tt.tool.Name, func(t *testing.T) {
			if got := tt.tool.Asset(tt.tag); got != tt.asset {
				t.Errorf("asset = %s, want %s", got, tt.asset)
			}
			if tt.member != "" {
				if got := tt.tool.Member(tt.tag); got != tt.member {
					t.Errorf("member = %s, want %s", got, tt.member)
				}
			}
		})
	}
}
