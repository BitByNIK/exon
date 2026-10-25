// Package projectinit is the "init" step: it adds a personal dev container to
// a project, so the project's container uses the shared toolbox volume. The
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

// Step adds the personal dev container files to one project.
type Step struct {
	dir, image, user string
}

// New returns the step for the project in dir, with the given image and container user.
func New(dir, image, user string) *Step { return &Step{dir: dir, image: image, user: user} }

// Name implements step.Step.
func (*Step) Name() string { return "init" }

// Summary implements step.Step.
func (*Step) Summary() string { return "Add a personal dev container to a project" }

// Requires implements step.Step.
func (*Step) Requires() []string { return nil }

// Prepare looks at what the project already has.
func (s *Step) Prepare(ctx context.Context, env step.Env) (step.Plan, error) {
	info, err := env.FS.Stat(s.dir)
	if err != nil || !info.IsDir() {
		return nil, usererr.New(s.dir+" is not a folder", "Run this in a project folder, or pass the folder as an argument.", err)
	}
	files, err := project.Files(project.Options{
		Name:        filepath.Base(s.dir),
		Image:       s.image,
		User:        s.user,
		ProjectsDir: project.ProjectsDir(s.dir, env.Home),
	})
	if err != nil {
		return nil, err
	}

	p := &plan{dir: s.dir}
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

	exclude, isRepo := excludeFile(ctx, env, s.dir)
	switch {
	case !isRepo:
		p.items = append(p.items, step.PlanItem{
			Kind: step.Note, Action: "not git", Subject: s.dir,
			Detail: "add " + project.ExcludeLine + " to your ignore file yourself",
		})
	case hasLine(env.FS, exclude, project.ExcludeLine):
		p.items = append(p.items, step.PlanItem{Kind: step.Same, Action: "excluded", Subject: project.ExcludeLine})
	default:
		p.exclude = exclude
		p.items = append(p.items, step.PlanItem{
			Kind: step.Add, Action: "exclude", Subject: project.ExcludeLine,
			Detail: "in .git/info/exclude, so it is never committed",
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

// excludeFile returns the project's git exclude file, and whether dir is a git repository.
func excludeFile(ctx context.Context, env step.Env, dir string) (string, bool) {
	res, err := env.Run.Query(ctx, sys.Cmd{Name: "git", Args: []string{
		"-C", dir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude",
	}})
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(string(res.Stdout))
	return path, path != ""
}

// hasLine reports whether the file has line as one of its lines.
func hasLine(fsys sys.FS, path, line string) bool {
	data, err := fsys.ReadFile(path)
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == line {
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
	dir     string
	items   []step.PlanItem
	create  []project.File
	exclude string // the git exclude file to add the line to, if it needs it
}

func (p *plan) Items() []step.PlanItem { return p.items }

func (*plan) Questions() []prompt.Question { return nil }

// Apply writes the files and the exclude line.
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
	if p.exclude != "" {
		env.Report.Start(project.ExcludeLine)
		if err := appendLine(env.FS, p.exclude, project.ExcludeLine); err != nil {
			env.Report.Fail(project.ExcludeLine, err, nil)
		} else {
			env.Report.Done(project.ExcludeLine, "in .git/info/exclude")
		}
	}
	if len(p.create) > 0 {
		env.Report.Notice("next: set the image in devcontainer.json and add your steps to post-create.sh, then open the folder in Zed and choose \"personal\"")
	}
	return nil
}

// appendLine adds line to the file, keeping what is there.
func appendLine(fsys sys.FS, path, line string) error {
	data, err := fsys.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	if err := fsys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsys.WriteFile(path, append(data, []byte(line+"\n")...), 0o644)
}
