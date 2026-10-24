// Package apps installs Homebrew taps, formulae and casks.
package apps

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

// Config is the [apps] section of exon.toml.
type Config struct {
	Taps     []string `toml:"taps"`
	Formulae []string `toml:"formulae"`
	Casks    []string `toml:"casks"`
}

// Answers to the "app already exists" question.
const (
	Adopt     = "adopt: keep it, let Homebrew manage it"
	Skip      = "skip: leave it alone"
	Reinstall = "reinstall: replace it with Homebrew's copy"
)

// Step is the apps step.
type Step struct {
	// AppDir is where casks put apps. Tests point it at a temporary folder.
	AppDir string
	// Attempts and Backoff control retries of network and lock failures.
	Attempts int
	Backoff  time.Duration
}

// New returns the apps step.
func New() *Step {
	return &Step{AppDir: "/Applications", Attempts: 4, Backoff: 3 * time.Second}
}

// Name implements step.Step.
func (*Step) Name() string { return "apps" }

// Summary implements step.Step.
func (*Step) Summary() string { return "Install Homebrew packages, apps and fonts" }

// Requires implements step.Step. brew itself is checked before any step runs.
func (*Step) Requires() []string { return nil }

// pkgKind is a formula or a cask.
type pkgKind string

const (
	formula pkgKind = "--formula"
	cask    pkgKind = "--cask"
)

// work is one thing the plan installs.
type work struct {
	kind     pkgKind // "" for a tap
	name     string  // as written in exon.toml (or the tap name)
	token    string  // name to install (follows renames)
	adoptQ   string  // question ID if an app of the same name already exists
	password bool    // runs a macOS installer that asks for a password
}

func (w work) label() string {
	if w.kind == "" {
		return "tap " + w.name
	}
	return w.name
}

type plan struct {
	s     *Step
	items []step.PlanItem
	todo  []work
	qs    []prompt.Question
}

// Prepare checks every package with brew and works out what is missing.
func (s *Step) Prepare(ctx context.Context, env step.Env) (step.Plan, error) {
	var cfg Config
	if err := env.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	p := &plan{s: s}

	tapped, err := s.taps(ctx, env.Run)
	if err != nil {
		return nil, err
	}
	for _, t := range cfg.Taps {
		t = strings.ToLower(t)
		if tapped[t] {
			p.items = append(p.items, step.PlanItem{Kind: step.Same, Action: "tapped", Subject: t})
			continue
		}
		p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "tap", Subject: t, Detail: "third-party tap"})
		p.todo = append(p.todo, work{name: t})
	}

	var problems []string
	for _, kind := range []pkgKind{formula, cask} {
		names := cfg.Formulae
		if kind == cask {
			names = cfg.Casks
		}
		// Packages from a tap that isn't tapped yet can't be looked up; brew
		// checks them when installing (installing user/repo/name taps automatically).
		var check []string
		for _, n := range names {
			if t := tapOf(n); t != "" && !coreTaps[t] && !tapped[t] {
				p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "install", Subject: n, Detail: "from tap " + t + ", checked when installing"})
				p.todo = append(p.todo, work{kind: kind, name: n, token: n})
				continue
			}
			check = append(check, n)
		}
		info, unknown, err := lookup(ctx, brewInfo(env.Run), string(kind), check)
		if err != nil {
			return nil, fmt.Errorf("brew info: %w", err)
		}
		for _, n := range unknown {
			problems = append(problems, fmt.Sprintf("%s %q not found", strings.TrimPrefix(string(kind), "--"), n))
		}
		for _, n := range check {
			if slices.Contains(unknown, n) {
				continue
			}
			if kind == formula {
				problems = append(problems, p.addFormula(n, info.Formulae)...)
			} else {
				problems = append(problems, p.addCask(env, n, info.Casks)...)
			}
		}
	}
	if len(problems) > 0 {
		return nil, usererr.New(
			"some packages in [apps] can't be installed:\n  "+strings.Join(problems, "\n  "),
			"Fix the names in exon.toml. Search with: brew search <name>", nil)
	}
	return p, nil
}

func (s *Step) taps(ctx context.Context, run sys.Runner) (map[string]bool, error) {
	res, err := run.Query(ctx, brew("tap"))
	if err != nil {
		return nil, fmt.Errorf("brew tap: %w", err)
	}
	out := map[string]bool{}
	for line := range strings.Lines(string(res.Stdout)) {
		if t := strings.TrimSpace(line); t != "" {
			out[strings.ToLower(t)] = true
		}
	}
	return out, nil
}

func (p *plan) addFormula(name string, infos []formulaInfo) []string {
	i := slices.IndexFunc(infos, func(f formulaInfo) bool { return f.matches(name) })
	if i < 0 {
		return []string{fmt.Sprintf("formula %q not found", name)}
	}
	f := infos[i]
	if f.Disabled {
		return []string{fmt.Sprintf("formula %q is disabled by Homebrew", name)}
	}
	if f.Deprecated {
		p.items = append(p.items, step.PlanItem{Kind: step.Warn, Action: "deprecated", Subject: name, Detail: "Homebrew will disable it eventually"})
	}
	if len(f.Installed) > 0 {
		p.items = append(p.items, step.PlanItem{Kind: step.Same, Action: "installed", Subject: name})
		return nil
	}
	p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "install", Subject: name})
	p.todo = append(p.todo, work{kind: formula, name: name, token: f.FullName})
	return nil
}

func (p *plan) addCask(env step.Env, name string, infos []caskInfo) []string {
	i := slices.IndexFunc(infos, func(c caskInfo) bool { return c.matches(name) })
	if i < 0 {
		return []string{fmt.Sprintf("cask %q not found", name)}
	}
	c := infos[i]
	if c.Disabled {
		return []string{fmt.Sprintf("cask %q is disabled by Homebrew", name)}
	}
	if c.Deprecated {
		p.items = append(p.items, step.PlanItem{Kind: step.Warn, Action: "deprecated", Subject: name, Detail: "Homebrew will disable it eventually"})
	}
	if slices.Contains(c.OldTokens, name) {
		p.items = append(p.items, step.PlanItem{Kind: step.Warn, Action: "renamed", Subject: name, Detail: "now called " + c.Token + "; update exon.toml"})
	}
	if c.Installed != nil {
		p.items = append(p.items, step.PlanItem{Kind: step.Same, Action: "installed", Subject: name})
		return nil
	}

	w := work{kind: cask, name: name, token: c.FullToken, password: c.needsPassword()}
	var details []string
	if t := tapOf(c.FullToken); t != "" && !coreTaps[t] {
		details = append(details, "third-party tap "+t)
	}
	if w.password {
		details = append(details, "needs your password")
	}
	for _, app := range c.apps() {
		if _, err := env.FS.Lstat(filepath.Join(p.s.AppDir, app)); err == nil {
			w.adoptQ = "apps.adopt." + c.Token
			p.qs = append(p.qs, prompt.Question{
				ID:      w.adoptQ,
				Text:    fmt.Sprintf("%s is already in %s but wasn't installed by Homebrew (%s).", app, p.s.AppDir, name),
				Options: []string{Adopt, Skip, Reinstall},
				Default: Adopt,
			})
			details = append(details, app+" already exists, will ask")
			break
		}
	}
	p.items = append(p.items, step.PlanItem{Kind: step.Add, Action: "install", Subject: name, Detail: strings.Join(details, "; ")})
	p.todo = append(p.todo, w)
	return nil
}

func (p *plan) Items() []step.PlanItem       { return p.items }
func (p *plan) Questions() []prompt.Question { return p.qs }

// Apply updates Homebrew once, then taps and installs one package at a time,
// so each one gets its own progress line and a failure only affects itself.
func (p *plan) Apply(ctx context.Context, env step.Env, answers prompt.Answers) error {
	if len(p.todo) == 0 {
		return nil
	}
	env.Report.Notice("Updating Homebrew…")
	if res, err := env.Run.Exec(ctx, brew("update")); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		env.Report.Notice("brew update failed, continuing with the current package index: " + sys.LastLines(string(res.Output()), 1))
	}

	for _, w := range p.todo {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := w.label()
		args := []string{"install", string(w.kind)}
		if w.kind == "" {
			args = []string{"tap", w.name}
		}
		switch answers[w.adoptQ] {
		case Skip:
			env.Report.Skip(item, "kept your existing app; not managed by Homebrew")
			continue
		case Adopt:
			args = append(args, "--adopt")
		case Reinstall:
			args = append(args, "--force")
		}
		if w.kind != "" {
			args = append(args, w.token)
		}

		env.Report.Start(item)
		cmd := brew(args...)
		if w.password {
			env.Report.Notice(item + " runs a macOS installer: enter your password if asked")
			cmd.Interactive = true
		} else {
			env.Report.Phase(item, "installing")
		}
		var res sys.Result
		err := sys.Retry(ctx, p.s.Attempts, p.s.Backoff,
			func(err error) bool { return isRetryable(err, res.Output()) },
			func() error {
				var err error
				res, err = env.Run.Exec(ctx, cmd)
				return err
			})
		if err != nil {
			if ctx.Err() != nil {
				env.Report.Fail(item, errors.New("cancelled"), res.Output())
				return ctx.Err()
			}
			env.Report.Fail(item, err, res.Output())
			continue
		}
		env.Report.Done(item, "")
	}
	return nil
}
