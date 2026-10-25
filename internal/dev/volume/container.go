package volume

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/bitbynik/exon/internal/dev/tool"
)

//go:embed container/init.sh
var initScript string

//go:embed container/link-workspaces.sh
var linkWorkspacesScript string

// ContainerName is the name under which the container files' fingerprint is
// recorded in the volume, next to the tools' versions.
const ContainerName = "container"

// Container is what every dev container runs from the volume: the init
// script, the script that links the other projects, and the list of links
// init makes. They are put in the volume next to the tools.
type Container struct {
	// Files maps a file name in the volume to its content.
	Files map[string][]byte
	// Hash fingerprints Files. It is recorded in the volume, so a later
	// install can tell that the files are out of date.
	Hash string
}

// ContainerFor builds the container files for the given tools: init links
// the login folders of exactly these tools.
func ContainerFor(tools []tool.Tool) Container {
	files := map[string][]byte{
		"init":            []byte(initScript),
		"link-workspaces": []byte(linkWorkspacesScript),
		"links":           []byte(linksText(tools)),
	}
	return Container{Files: files, Hash: fingerprint(files)}
}

// linksText is the file init reads: one link per line, "dir|state|home" or "file|state|home".
func linksText(tools []tool.Tool) string {
	links := tool.SharedLinks()
	for _, t := range tools {
		links = append(links, t.Links...)
	}
	var b strings.Builder
	for _, l := range links {
		kind := "dir"
		if l.File {
			kind = "file"
		}
		b.WriteString(kind + "|" + l.State + "|" + l.Home + "\n")
	}
	return b.String()
}

func fingerprint(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(files[name])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
