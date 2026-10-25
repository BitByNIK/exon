// Package toolbox is the "dev" step: it puts the dev tools (omp, gh and the
// rest) in the shared toolbox volume that every dev container mounts, so a
// tool installed or logged in once is there for all of them. It installs the
// tools that are missing, or updates the ones that are there.
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
	"github.com/bitbynik/exon/internal/usererr"
)

const (
	starterItem   = "starter files"
	containerItem = "container scripts"
	seedFolder    = "toolbox" // in the config repo; mirrors the volume's layout
	// mcpName is the tool whose running copies matter when it is replaced.
	mcpName = "codebase-memory-mcp"
)

type mode int

const (
	modeInstall mode = iota // put in the tools that are not there yet
	modeUpdate              // bring the tools that are there to a newer version
)

// Step installs or updates the tools in the volume.
type Step struct {
	// uid and gid are the Mac user's: the volume belongs to them, because the
	// dev containers run as that user.
	uid, gid int
	mode     mode
	only     []string // update: the tools to update; none means all of them
	version  string   // update: the version to put, for exactly one tool
}

// New returns the step that installs the missing tools, for the Mac user with the given ids.
func New(uid, gid int) *Step { return &Step{uid: uid, gid: gid} }

// NewUpdate returns the step that updates tools. only names the tools to
// update (all of the chosen tools if empty); version, if not empty, is the
// version to put instead of the newest, and needs exactly one tool.
func NewUpdate(uid, gid int, only []string, version string) *Step {
	return &Step{uid: uid, gid: gid, mode: modeUpdate, only: only, version: version}
}

// Name implements step.Step. It is also the [dev] section of exon.toml.
func (*Step) Name() string { return "dev" }

// Summary implements step.Step.
func (s *Step) Summary() string {
	if s.mode == modeUpdate {
		return "Update your dev tools in the shared toolbox volume"
	}
	return "Put your dev tools in the shared toolbox volume"
}

// Requires implements step.Step. Docker is checked while planning instead, so
// that its absence is explained rather than the step being silently skipped.
func (*Step) Requires() []string { return nil }

// job is one tool to put in the volume.
type job struct {
	rel  release.Release
	from string // the version in the volume; empty for a new install
	to   string // the version it will have; empty if not known (a new clone)
}

// detail is what the finished item says: the version, or "from → to".
func (j job) detail() string {
	if j.from != "" {
		return j.from + " → " + j.to
	}
	return j.to
}

// Prepare looks at the volume and at GitHub, and plans what to put in.
func (s *Step) Prepare(ctx context.Context, env step.Env) (step.Plan, error) {
	cfg, err := settings.Load(env.Config)
	if err != nil {
		return nil, err
	}
	tools, err := s.chosen(cfg)
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

	p := &plan{uid: s.uid, gid: s.gid, update: s.mode == modeUpdate}
	pl := &planner{ctx: ctx, env: env, vol: vol, p: p, installed: installed, version: s.version}
	for _, t := range tools {
		if s.mode == modeUpdate {
			err = pl.update(t)
		} else {
			err = pl.install(t)
		}
		if err != nil {
			return nil, err
		}
	}
	if s.mode == modeUpdate {
		pl.warnAboutOpenSessions()
	}

	// The container files are rebuilt for all the chosen tools; they go in only when they differ from
	// what the volume has, which also covers a new exon with a better script.
	if c := volume.ContainerFor(cfg.Tools); installed[volume.ContainerName] != c.Hash {
		p.container = &c
		p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "install", Subject: containerItem, Detail: "init, link-workspaces"})
	}

	missing, err := pl.starterFiles()
	if err != nil {
		return nil, err
	}

	if len(p.todo) > 0 || len(missing) > 0 || p.container != nil {
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

// chosen returns the tools this run is about, in install order.
func (s *Step) chosen(cfg settings.Settings) ([]tool.Tool, error) {
	tools := cfg.Tools
	if s.mode == modeUpdate && len(s.only) > 0 {
		tools = nil
		for _, name := range s.only {
			t, err := tool.Find(name)
			if err != nil {
				return nil, usererr.New(fmt.Sprintf("unknown tool %q", name), "Known tools: "+strings.Join(tool.Names(), ", "), err)
			}
			if !slices.ContainsFunc(cfg.Tools, func(c tool.Tool) bool { return c.Name == name }) {
				return nil, usererr.New(name+" is not one of your dev tools", "Add it to [dev] tools in exon.toml first.", nil)
			}
			tools = append(tools, t)
		}
		order := tool.Names()
		slices.SortFunc(tools, func(a, b tool.Tool) int { return slices.Index(order, a.Name) - slices.Index(order, b.Name) })
		tools = slices.CompactFunc(tools, func(a, b tool.Tool) bool { return a.Name == b.Name })
	}
	if s.version != "" {
		if len(tools) != 1 {
			return nil, usererr.New("--version needs exactly one tool",
				"Name the tool, for example: exon dev update SOURCE omp --version v18.12.6", nil)
		}
		if tools[0].Kind == tool.Clone {
			return nil, usererr.New(tools[0].Name+" has no versions to choose from", "It always follows the newest commit.", nil)
		}
	}
	return tools, nil
}

// planner builds the plan one tool at a time.
type planner struct {
	ctx       context.Context
	env       step.Env
	vol       volume.Volume
	p         *plan
	installed map[string]string
	version   string // update: the version asked for; empty means the newest
}

// install plans a tool that is not in the volume yet.
func (pl *planner) install(t tool.Tool) error {
	if _, ok := pl.installed[t.Name]; ok {
		pl.same(t.Name)
		return nil
	}
	rel, err := release.Latest(pl.ctx, pl.env.Net, t)
	if err != nil {
		return err
	}
	pl.add(job{rel: rel, to: rel.Tag}, step.PlanItem{Kind: step.Add, Action: "install", Subject: t.Name, Detail: describe(rel, "git clone")})
	return nil
}

// update plans a tool that is in the volume: nothing if it is already at the wanted version.
func (pl *planner) update(t tool.Tool) error {
	have, ok := pl.installed[t.Name]
	if !ok {
		pl.p.items = append(pl.p.items, step.PlanItem{
			Kind: step.Note, Action: "not installed", Subject: t.Name,
			Detail: "run exon dev install first",
		})
		return nil
	}
	if t.Kind == tool.Clone {
		return pl.updateClone(t, have)
	}
	var rel release.Release
	var err error
	if pl.version != "" {
		rel, err = release.At(pl.ctx, pl.env.Net, t, pl.version)
	} else {
		rel, err = release.Latest(pl.ctx, pl.env.Net, t)
	}
	if err != nil {
		return err
	}
	if rel.Tag == have {
		pl.same(t.Name)
		return nil
	}
	pl.add(job{rel: rel, from: have, to: rel.Tag},
		step.PlanItem{Kind: step.Change, Action: "update", Subject: t.Name, Detail: have + " → " + describe(rel, "")})
	return nil
}

// updateClone compares the commit in the volume with the newest one on GitHub.
func (pl *planner) updateClone(t tool.Tool, have string) error {
	if present, err := pl.vol.ImagePresent(pl.ctx); err != nil || !present {
		// Asking would download the helper image, which planning must not do.
		pl.p.items = append(pl.p.items, step.PlanItem{
			Kind: step.Note, Action: "not checked", Subject: t.Name,
			Detail: "the helper image is not downloaded yet",
		})
		return err
	}
	head, err := pl.vol.RemoteHead(pl.ctx, t.CloneURL())
	if err != nil {
		return usererr.New("could not look up the newest "+t.Name, "Check your internet connection and try again.", err)
	}
	if strings.HasPrefix(head, have) {
		pl.same(t.Name)
		return nil
	}
	to := head[:7]
	pl.add(job{rel: release.Release{Tool: t}, from: have, to: to},
		step.PlanItem{Kind: step.Change, Action: "update", Subject: t.Name, Detail: have + " → " + to})
	return nil
}

func (pl *planner) same(name string) {
	pl.p.items = append(pl.p.items, step.PlanItem{Kind: step.Same, Action: "installed", Subject: name})
}

// add puts a job and its plan line in the plan, with a warning if the file cannot be verified.
func (pl *planner) add(j job, item step.PlanItem) {
	pl.p.todo = append(pl.p.todo, j)
	pl.p.items = append(pl.p.items, item)
	if t := j.rel.Tool; t.Kind != tool.Clone && j.rel.SHA256 == "" {
		pl.p.items = append(pl.p.items, step.PlanItem{
			Kind: step.Warn, Action: "unverified", Subject: t.Name,
			Detail: "GitHub publishes no checksum for this file",
		})
	}
}

// warnAboutOpenSessions says so when the MCP is about to be replaced while a
// container has it open. The running copies keep working, but two containers
// could briefly run different versions on one code index.
func (pl *planner) warnAboutOpenSessions() {
	if !slices.ContainsFunc(pl.p.todo, func(j job) bool { return j.rel.Tool.Name == mcpName }) {
		return
	}
	open, err := pl.vol.OpenSessions(pl.ctx)
	if err != nil {
		pl.env.Log.Warn("could not look for open sessions", "err", err)
		return
	}
	if len(open) > 0 {
		pl.p.items = append(pl.p.items, step.PlanItem{
			Kind: step.Warn, Action: "close first", Subject: mcpName,
			Detail: "open in " + strings.Join(open, ", ") + "; close those sessions, or containers may run different versions on one index",
		})
	}
}

// starterFiles plans copying the config repo's toolbox/ files that the volume
// lacks. Only an install does this. It returns the files to copy.
func (pl *planner) starterFiles() ([]string, error) {
	if pl.p.update {
		return nil, nil
	}
	seedDir := filepath.Join(pl.env.Repo, seedFolder)
	files, ignored, err := starterFiles(pl.env.FS, seedDir)
	if err != nil {
		return nil, err
	}
	for _, f := range ignored {
		pl.p.items = append(pl.p.items, step.PlanItem{
			Kind: step.Warn, Action: "ignored", Subject: seedFolder + "/" + f,
			Detail: "starter files never go in bin/ or versions/",
		})
	}
	missing, err := pl.vol.Missing(pl.ctx, files)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		pl.p.seedDir = seedDir
		pl.p.items = append(pl.p.items, step.PlanItem{Kind: step.Add, Action: "copy", Subject: starterItem, Detail: describeFiles(missing)})
	}
	return missing, nil
}

// describe says what is to be downloaded: the version and size, or clone for a git repository.
func describe(rel release.Release, clone string) string {
	if rel.Tool.Kind == tool.Clone {
		return clone
	}
	detail := rel.Tag
	if rel.Size > 0 {
		detail += fmt.Sprintf(", %d MB", (rel.Size+(1<<20)-1)>>20)
	}
	return detail
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
	update    bool
	items     []step.PlanItem
	todo      []job
	seedDir   string            // set when starter files are to be copied
	container *volume.Container // set when the container files are to be put in
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
	details := map[string]string{}
	for _, j := range p.todo {
		rel := j.rel
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
		details[name] = j.detail()
	}
	if len(items) == 0 && p.seedDir == "" && p.container == nil {
		return nil
	}
	if p.container != nil {
		env.Report.StartUntimed(containerItem)
	}
	if p.seedDir != "" {
		env.Report.StartUntimed(starterItem)
	}

	out, err := vol.Install(ctx, volume.Job{StageDir: stage, Items: items, SeedDir: p.seedDir, Container: p.container})
	for _, it := range items {
		reportOutcome(env.Report, it.Tool.Name, it.Tool.Name, details[it.Tool.Name], out)
	}
	if p.container != nil {
		reportOutcome(env.Report, containerItem, volume.ContainerName, "", out)
	}
	if p.seedDir != "" && err == nil {
		env.Report.Done(starterItem, fmt.Sprintf("%d copied", len(out.Seeded)))
	}
	return err
}

// reportOutcome reports one item as the container answered about it. item is
// the name shown; key is the name the container used.
func reportOutcome(r *step.Reporter, item, key, detail string, out volume.Outcome) {
	switch why, failed := out.Failed[key]; {
	case failed:
		r.Fail(item, errors.New(why), nil)
	case slices.Contains(out.Installed, key):
		r.Done(item, detail)
	default:
		r.Fail(item, errors.New("the container did not report this"), nil)
	}
}
