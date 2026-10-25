// Package backup knows what a backup of the toolbox volume is: which of the
// volume's files it holds, how its file is named, and how to check that a
// file is a backup that is safe to restore.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

// Excluded are the top-level names in the volume that a backup leaves out:
// exon puts them there again, so they are not worth keeping, and a restore
// never writes them, so a backup file cannot replace the programs.
var Excluded = []string{"bin", "lib", "versions", "init", "link-workspaces", "links"}

// DefaultDir is where backups go unless the user says otherwise.
func DefaultDir(home string) string { return filepath.Join(home, "Backups", "exon") }

// FileName is the name of the backup made at t.
func FileName(t time.Time) string { return "toolbox-" + t.Format("2006-01-02_150405") + ".tar.gz" }

// Info describes a backup file.
type Info struct {
	Files    int
	Bytes    int64    // the files' size once restored
	Problems []string // why the file is not safe to restore; empty if it is
}

// Inspect reads a backup file without extracting it. An error means the file
// is not a backup at all; Problems means it is one that must not be restored.
func Inspect(fsys sys.FS, file string) (Info, error) {
	f, err := fsys.Open(file)
	if err != nil {
		return Info{}, usererr.New("cannot read "+file, "", err)
	}
	defer f.Close()
	notABackup := func(err error) error {
		return usererr.New(file+" is not a toolbox backup", "Backups are the .tar.gz files exon dev backup makes.", err)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return Info{}, notABackup(err)
	}
	defer gz.Close()

	var info Info
	links := map[string]bool{} // paths of the links seen so far
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Info{}, notABackup(err)
		}
		if problem := check(h, links); problem != "" {
			info.Problems = append(info.Problems, problem)
		}
		switch h.Typeflag {
		case tar.TypeReg:
			info.Files++
			info.Bytes += h.Size
		case tar.TypeSymlink, tar.TypeLink:
			links[clean(h.Name)] = true
		}
	}
	if info.Files == 0 && len(info.Problems) == 0 {
		return Info{}, usererr.New(file+" has no files in it", "", nil)
	}
	return info, nil
}

func clean(name string) string { return path.Clean(strings.TrimPrefix(name, "./")) }

// outside says whether a path leaves the volume or is not relative.
func outside(p string) bool {
	c := clean(p)
	return path.IsAbs(p) || strings.HasPrefix(c, "/") || c == ".." || strings.HasPrefix(c, "../")
}

// check says what is wrong with an entry, or nothing if it is fine. links are
// the paths of the links earlier in the archive.
func check(h *tar.Header, links map[string]bool) string {
	name := clean(h.Name)
	if name == "." {
		return ""
	}
	if outside(h.Name) {
		return fmt.Sprintf("%s: not inside the volume", h.Name)
	}
	if top, _, _ := strings.Cut(name, "/"); slices.Contains(Excluded, top) {
		return fmt.Sprintf("%s: %s is installed by exon, not restored", h.Name, top)
	}
	// Writing "through" a link would put the file wherever the link points.
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		if links[dir] {
			return fmt.Sprintf("%s: is inside the link %s", h.Name, dir)
		}
	}
	switch h.Typeflag {
	case tar.TypeReg, tar.TypeDir, tar.TypeSymlink:
		return ""
	case tar.TypeLink: // a hard link must point at a file of the archive
		if outside(h.Linkname) {
			return fmt.Sprintf("%s: a hard link to %s", h.Name, h.Linkname)
		}
		return ""
	}
	return fmt.Sprintf("%s: not an ordinary file", h.Name)
}
