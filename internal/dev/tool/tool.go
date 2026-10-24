// Package tool lists the programs exon puts in the shared toolbox volume.
// A tool is plain data: where to download it and which login folders belong
// to it. Adding a tool means adding one file here and one line in All.
package tool

import (
	"fmt"
	"strings"
)

// Kind is how a tool is put into the volume.
type Kind int

// Tool kinds.
const (
	Program Kind = iota // one file from a GitHub release, used as it is
	Archive             // a .tar.gz from a GitHub release that contains the program
	Clone               // a git repository, cloned into the volume
)

// Tool is one program or folder in the toolbox volume.
//
// The containers run Linux on Apple Silicon, so every asset name is the
// arm64 one.
type Tool struct {
	Name    string
	Summary string
	Kind    Kind
	// Repo is the GitHub repository, as "owner/repo". Releases (Program,
	// Archive) or the clone (Clone) come from here.
	Repo string
	// Asset is the release file name for a tag such as "v2.103.2".
	// Used by Program and Archive.
	Asset func(tag string) string
	// Member is the path of the program inside the archive. Used by Archive.
	Member func(tag string) string
	// Links are folders in a container's home that keep their content in
	// the volume, so a login made in one container shows in all of them.
	Links []Link
}

// Link says that Home, a path in the container user's home, lives in the
// volume under state/<State>.
type Link struct {
	State string // "omp"
	Home  string // ".omp"
}

// bare removes the leading "v" from a tag: "v2.103.2" is "2.103.2".
func bare(tag string) string { return strings.TrimPrefix(tag, "v") }

// All returns every tool, in install order.
func All() []Tool {
	return []Tool{OMP(), CodebaseMemory(), GH(), FZF(), EZA(), OhMyBash()}
}

// Find returns the tool with the given name.
func Find(name string) (Tool, error) {
	for _, t := range All() {
		if t.Name == name {
			return t, nil
		}
	}
	return Tool{}, fmt.Errorf("unknown tool %q", name)
}

// Names returns the name of every tool.
func Names() []string {
	all := All()
	names := make([]string, len(all))
	for i, t := range all {
		names[i] = t.Name
	}
	return names
}
