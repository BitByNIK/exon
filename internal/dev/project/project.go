// Package project makes the personal dev container files for a project:
// a devcontainer.json that uses the shared toolbox volume, and the script
// that runs once when the container is created.
package project

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/bitbynik/exon/internal/dev/volume"
)

// Defaults for the choices the user can change.
const (
	// DefaultImage is a ready-made Debian image with git and sudo, which
	// the toolbox programs run on. The user changes it to suit the project.
	DefaultImage = "mcr.microsoft.com/devcontainers/base:trixie"
	DefaultUser  = "vscode" // the user that image runs as
)

// Dir is the folder, inside the project, that holds the personal files.
const Dir = ".devcontainer/personal"

// ExcludeLine keeps the personal files out of git.
const ExcludeLine = Dir + "/"

//go:embed devcontainer.json.tmpl
var devcontainerTmpl string

//go:embed post-create.sh.tmpl
var postCreateTmpl string

// Options are what the files are made from.
type Options struct {
	Name        string // the project's folder name
	Image       string
	User        string
	ProjectsDir string // where the Mac's projects are, as devcontainer.json should say it
}

// File is one file to create, with a path relative to the project folder.
type File struct {
	Path    string
	Content string
	Mode    fs.FileMode
}

// Files returns the files for a project, in the order they are shown.
func Files(o Options) ([]File, error) {
	data := struct {
		Options
		Volume, MountPoint string
	}{o, volume.DefaultName, volume.MountPoint}

	devcontainer, err := render("devcontainer.json", devcontainerTmpl, data)
	if err != nil {
		return nil, err
	}
	script, err := render("post-create.sh", postCreateTmpl, data)
	if err != nil {
		return nil, err
	}
	return []File{
		{Path: Dir + "/devcontainer.json", Content: devcontainer, Mode: 0o644},
		{Path: Dir + "/post-create.sh", Content: script, Mode: 0o755},
	}, nil
}

func render(name, text string, data any) (string, error) {
	t, err := template.New(name).Funcs(template.FuncMap{"json": jsonString}).Parse(text)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// jsonString writes s as a JSON string, quotes included, so that no name can break the file.
func jsonString(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

// ProjectsDir says where the Mac's projects are, from the project's own
// folder: its parent. Under the home folder it is written with
// ${localEnv:HOME}, so the file reads the same wherever the home folder is.
func ProjectsDir(projectDir, home string) string {
	parent := filepath.Dir(projectDir)
	if rel, err := filepath.Rel(home, parent); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		if rel == "." {
			return "${localEnv:HOME}"
		}
		return "${localEnv:HOME}/" + filepath.ToSlash(rel)
	}
	return parent
}
