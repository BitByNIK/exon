// Package toolbox is the "dev" step: it puts the dev tools (omp, gh and the
// rest) in the shared toolbox volume that every dev container mounts, so a
// tool installed or logged in once is there for all of them.
package toolbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bitbynik/exon/internal/dev/release"
	"github.com/bitbynik/exon/internal/dev/settings"
	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/dev/volume"
	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys"
)

const (
	starterItem = "starter files"
	seedFolder  = "toolbox" // in the config repo; mirrors the volume's layout
)

// Step installs the tools that are not in the volume yet.
type Step struct {
	// uid and gid are the Mac user's: the volume belongs to them, because the
	// dev containers run as that user.
	uid, gid int
}

// New returns the dev step for the Mac user with the given ids.
func New(uid, gid int) *Step { return &Step{uid: uid, gid: gid} }

// Name implements step.Step. It is also the [dev] section of exon.toml.
func (*Step) Name() string { return "dev" }

// Summary implements step.Step.
func (*Step) Summary() string { return "Put your dev tools in the shared toolbox volume" }

// Requires implements step.Step. Docker is checked while planning instead, so
// that its absence is explained rather than the step being silently skipped.
func (*Step) Requires() []string { return nil }

// Prepare looks at the volume and at GitHub, and plans what to install.
func (s *Step) Prepare(ctx context.Context, env step.Env) (step.Plan, error) {
	cfg, err := settings.Load(env.Config)
	if err != nil {
		return nil, err
	}
	vol := volume.New(env.Run, env.FS, s.uid, s.gid)
	if err := vol.Check(ctx); err != nil {
		return nil, err
	}
	installed, err := vol.Installed(ctx)
	if err != nil {
		return nil, err
	}

	p := &plan{uid: s.uid, gid: s.gid}
	for _, t := range cfg.Tools {
		if _, ok := installed[t.Name]; ok {
			p.items = append(p.items, step.PlanItem{Kind: step.Same, Action: "installed", Subject: t.Name})
			continue
		}
		rel, err := release.Latest(ctx, env.Net, t)
		if err != nil {
			return nil, err
		}
		p.todo = append(p.todo, rel)
		p.items = append(p.items, installItem(rel))
		if t.Kind != tool.Clone && rel.SHA256 == "" {
			p.items = append(p.items, step.PlanItem{
				Kind: step.Warn, Action: "unverified", Subject: t.Name,
				Detail: "GitHub publishes no checksum for this file",
			})
		}
	}

	seedDir := filepath.Join(env.Repo, seedFolder)
	files, ignored, err := starterFiles(env.FS, seedDir)
	if err != nil {
		return nil, err
	}
	for _, f := range ignored {
		p.items = append(p.items, step.PlanItem{
			Kind: step.Warn, Action: "ignored", Subject: seedFolder + "/" + f,
			Detail: "starter files never go in bin/ or versions/",
		})
	}
	missing, err := vol.Missing(ctx, files)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		p.seedDir = seedDir
		p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "copy", Subject: starterItem, Detail: describeFiles(missing)})
	}

	if len(p.todo) > 0 || len(missing) > 0 {
		present, err := vol.ImagePresent(ctx)
		if err != nil {
			return nil, err
		}
		if !present {
			p.pullImage = true
			p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "download", Subject: "helper image", Detail: "once, about 100 MB"})
		}
	}
	return p, nil
}

func installItem(rel release.Release) step.PlanItem {
	detail := "git clone"
	if rel.Tool.Kind != tool.Clone {
		detail = rel.Tag
		if rel.Size > 0 {
			detail += fmt.Sprintf(", %d MB", (rel.Size+(1<<20)-1)>>20)
		}
	}
	return step.PlanItem{Kind: step.Add, Action: "install", Subject: rel.Tool.Name, Detail: detail}
}

// starterFiles lists the files of the config repo's toolbox/ folder, as paths
// inside the volume. Files for places exon manages itself are returned apart.
func starterFiles(fsys sys.FS, dir string) (files, ignored []string, err error) {
	info, err := fsys.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	err = fsys.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == ".DS_Store" {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "bin/") || strings.HasPrefix(rel, "versions/") {
			ignored = append(ignored, rel)
		} else {
			files = append(files, rel)
		}
		return nil
	})
	return files, ignored, err
}

// describeFiles names up to three files and counts the rest.
func describeFiles(files []string) string {
	if len(files) <= 3 {
		return strings.Join(files, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(files[:3], ", "), len(files)-3)
}

type plan struct {
	uid, gid  int // the Mac user, who will own the volume
	items     []step.PlanItem
	todo      []release.Release // tools to install, in install order
	seedDir   string            // set when starter files are to be copied
	pullImage bool
}

func (p *plan) Items() []step.PlanItem { return p.items }

func (*plan) Questions() []prompt.Question { return nil }

// Apply downloads the tools, then fills the volume in one throwaway container.
func (p *plan) Apply(ctx context.Context, env step.Env, _ prompt.Answers) error {
	// A new handle from this Env: the one used while planning could only read.
	vol := volume.New(env.Run, env.FS, p.uid, p.gid)
	stage := filepath.Join(env.CacheDir, "stage")
	if err := env.FS.RemoveAll(stage); err != nil {
		return err
	}
	if err := env.FS.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	defer func() { _ = env.FS.RemoveAll(stage) }()

	if p.pullImage {
		env.Report.Notice("downloading the helper image (one time, about 100 MB)")
		if err := vol.PullImage(ctx); err != nil {
			return err
		}
	}

	var items []volume.Item
	for _, rel := range p.todo {
		name := rel.Tool.Name
		env.Report.StartUntimed(name) // the tool is only in once the one container has run
		it := volume.Item{Tool: rel.Tool, Version: rel.Tag}
		if rel.Tool.Kind != tool.Clone {
			it.File = name + ".download"
			err := env.Net.Download(ctx, sys.Fetch{URL: rel.URL, Dest: filepath.Join(stage, it.File), SHA256: rel.SHA256})
			if err != nil {
				env.Report.Fail(name, err, nil)
				continue
			}
		}
		items = append(items, it)
	}
	if len(items) == 0 && p.seedDir == "" {
		return nil
	}
	if p.seedDir != "" {
		env.Report.StartUntimed(starterItem)
	}

	out, err := vol.Install(ctx, volume.Job{StageDir: stage, Items: items, SeedDir: p.seedDir})
	for _, it := range items {
		name := it.Tool.Name
		switch why, failed := out.Failed[name]; {
		case failed:
			env.Report.Fail(name, errors.New(why), nil)
		case slices.Contains(out.Installed, name):
			env.Report.Done(name, versionOf(it))
		default:
			env.Report.Fail(name, errors.New("the container did not report this tool"), nil)
		}
	}
	if p.seedDir != "" && err == nil {
		env.Report.Done(starterItem, fmt.Sprintf("%d copied", len(out.Seeded)))
	}
	return err
}

func versionOf(it volume.Item) string {
	if it.Tool.Kind == tool.Clone {
		return ""
	}
	return it.Version
}
