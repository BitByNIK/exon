// Package projectinit is the "init" step: it adds your own dev container to a
// project, so the project's container uses the shared toolbox volume. The
// files are the user's to edit afterwards, and exon never overwrites them.
package projectinit

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bitbynik/exon/internal/dev/project"
	"github.com/bitbynik/exon/internal/dev/volume"
	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

// ignoreBlock is what is added to the project's .gitignore.
const ignoreBlock = "# your own dev container (the toolbox and your other projects), see .devcontainer/\n" + project.IgnoreLine + "\n"

// Step adds the files of your own dev container to one project.
type Step struct {
	dir, image, user string
}

// New returns the step for the project in dir. An empty image means the user did
// not choose one: the project's own Dockerfile is used if it has one, otherwise
// the default image.
func New(dir, image, user string) *Step { return &Step{dir: dir, image: image, user: user} }

// Name implements step.Step.
func (*Step) Name() string { return "init" }

// Summary implements step.Step.
func (*Step) Summary() string { return "Add your own dev container to a project" }

// Requires implements step.Step.
func (*Step) Requires() []string { return nil }

// Prepare looks at what the project already has.
func (s *Step) Prepare(ctx context.Context, env step.Env) (step.Plan, error) {
	info, err := env.FS.Stat(s.dir)
	if err != nil || !info.IsDir() {
		return nil, usererr.New(s.dir+" is not a folder", "Run this in a project folder, or pass the folder as an argument.", err)
	}
	opts := project.Options{
		Name:          filepath.Base(s.dir),
		Image:         s.image,
		User:          s.user,
		ProjectsDir:   project.ProjectsDir(s.dir, env.Home),
		Dockerfile:    s.image == "" && exists(env.FS, filepath.Join(s.dir, ".devcontainer", "Dockerfile")),
		ProjectScript: exists(env.FS, filepath.Join(s.dir, ".devcontainer", "post-create.sh")),
	}
	if opts.Image == "" {
		opts.Image = project.DefaultImage
	}
	files, err := project.Files(opts)
	if err != nil {
		return nil, err
	}

	p := &plan{dir: s.dir, label: project.Label(opts.Name), dockerfile: opts.Dockerfile}
	for _, f := range files {
		existing, err := env.FS.ReadFile(filepath.Join(s.dir, f.Path))
		switch {
		case err == nil && bytes.Equal(existing, []byte(f.Content)):
			p.items = append(p.items, step.PlanItem{Kind: step.Same, Action: "present", Subject: f.Path})
		case err == nil:
			// It is the user's file by now: never overwrite it.
			p.items = append(p.items, step.PlanItem{
				Kind: step.Warn, Action: "kept", Subject: f.Path,
				Detail: "already exists and is different; delete it to make a new one",
			})
		case errors.Is(err, fs.ErrNotExist):
			p.create = append(p.create, f)
			p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "create", Subject: f.Path})
		default:
			return nil, err
		}
	}

	ignore := filepath.Join(s.dir, ".gitignore")
	switch {
	case !isRepo(ctx, env, s.dir):
		p.items = append(p.items, step.PlanItem{
			Kind: step.Note, Action: "not git", Subject: s.dir,
			Detail: "add " + project.IgnoreLine + " to your ignore file yourself",
		})
	case ignores(env.FS, ignore):
		p.items = append(p.items, step.PlanItem{Kind: step.Same, Action: "ignored", Subject: project.IgnoreLine})
	default:
		p.ignore = ignore
		p.items = append(p.items, step.PlanItem{
			Kind: step.Add, Action: "ignore", Subject: project.IgnoreLine,
			Detail: "in .gitignore, so it is never committed; commit that change",
		})
	}

	if missingVolume(ctx, env) {
		p.items = append(p.items, step.PlanItem{
			Kind: step.Warn, Action: "toolbox", Subject: volume.DefaultName,
			Detail: "the volume does not exist yet; run exon dev install before opening the container",
		})
	}
	return p, nil
}

// exists reports whether path is there.
func exists(fsys sys.FS, path string) bool {
	_, err := fsys.Stat(path)
	return err == nil
}

// isRepo reports whether dir is inside a git repository.
func isRepo(ctx context.Context, env step.Env, dir string) bool {
	_, err := env.Run.Query(ctx, sys.Cmd{Name: "git", Args: []string{"-C", dir, "rev-parse", "--git-dir"}})
	return err == nil
}

// ignores reports whether the .gitignore file already has a line for the folder.
func ignores(fsys sys.FS, path string) bool {
	data, err := fsys.ReadFile(path)
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(l) {
		case project.IgnoreLine, "/" + project.IgnoreLine, project.Dir, "/" + project.Dir:
			return true
		}
	}
	return false
}

// missingVolume is true when docker answers and the toolbox volume is not there.
// If docker does not answer, nothing is said: init does not need Docker.
func missingVolume(ctx context.Context, env step.Env) bool {
	vol := volume.New(env.Run, env.FS, 0, 0)
	if vol.Check(ctx) != nil {
		return false
	}
	exists, err := vol.Exists(ctx)
	return err == nil && !exists
}

type plan struct {
	dir        string
	label      string
	dockerfile bool
	items      []step.PlanItem
	create     []project.File
	ignore     string // the .gitignore file to add the line to, if it needs it
}

func (p *plan) Items() []step.PlanItem { return p.items }

func (*plan) Questions() []prompt.Question { return nil }

// Apply writes the files and the ignore line.
func (p *plan) Apply(_ context.Context, env step.Env, _ prompt.Answers) error {
	for _, f := range p.create {
		env.Report.Start(f.Path)
		path := filepath.Join(p.dir, f.Path)
		if err := env.FS.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			env.Report.Fail(f.Path, err, nil)
			continue
		}
		if err := env.FS.WriteFile(path, []byte(f.Content), f.Mode); err != nil {
			env.Report.Fail(f.Path, err, nil)
			continue
		}
		env.Report.Done(f.Path, "")
	}
	if p.ignore != "" {
		env.Report.Start(project.IgnoreLine)
		if err := appendBlock(env.FS, p.ignore, ignoreBlock); err != nil {
			env.Report.Fail(project.IgnoreLine, err, nil)
		} else {
			env.Report.Done(project.IgnoreLine, "in .gitignore")
		}
	}
	if len(p.create) > 0 {
		next := "next: add your steps to " + project.Dir + "/post-create.sh"
		if !p.dockerfile {
			next = "next: set the image in " + project.Dir + "/devcontainer.json and add your steps to " + project.Dir + "/post-create.sh"
		}
		env.Report.Notice(next + ", then open the folder in Zed and choose \"" + p.label + "\"")
	}
	return nil
}

// appendBlock adds block to the end of the file, keeping what is there and
// leaving a blank line before it.
func appendBlock(fsys sys.FS, path, block string) error {
	data, err := fsys.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(data) > 0 {
		if !bytes.HasSuffix(data, []byte("\n")) {
			data = append(data, '\n')
		}
		data = append(data, '\n')
	}
	if err := fsys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsys.WriteFile(path, append(data, []byte(block)...), 0o644)
}
