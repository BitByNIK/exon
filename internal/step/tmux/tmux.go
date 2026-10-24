// Package tmux installs tmux's plugin manager (tpm), so plugins listed in
// ~/.tmux.conf can be installed.
package tmux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys"
)

// Config is the [tmux] section of exon.toml.
type Config struct {
	// InstallPluginManager installs tpm into ~/.tmux/plugins/tpm.
	InstallPluginManager bool `toml:"install_plugin_manager"`
}

const (
	item   = "tmux plugin manager"
	tpmURL = "https://github.com/tmux-plugins/tpm"
)

// Step is the tmux step.
type Step struct {
	// Attempts and Backoff control retries of network failures.
	Attempts int
	Backoff  time.Duration
}

// New returns the tmux step.
func New() *Step { return &Step{Attempts: 3, Backoff: 2 * time.Second} }

// Name implements step.Step.
func (*Step) Name() string { return "tmux" }

// Summary implements step.Step.
func (*Step) Summary() string { return "Install tmux's plugin manager" }

// Requires implements step.Step: without tmux there's nothing to install it for.
func (*Step) Requires() []string { return []string{"tmux", "git"} }

// Prepare checks whether tpm is already there.
func (s *Step) Prepare(_ context.Context, env step.Env) (step.Plan, error) {
	var cfg Config
	if err := env.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	p := &plan{s: s, dir: filepath.Join(env.Home, ".tmux", "plugins", "tpm")}
	if !cfg.InstallPluginManager {
		return p, nil
	}
	_, err := env.FS.Lstat(p.dir)
	switch {
	case err == nil:
		p.kind = step.Same
	case errors.Is(err, fs.ErrNotExist):
		p.kind = step.Add
	default:
		return nil, err
	}
	p.enabled = true
	return p, nil
}

type plan struct {
	s       *Step
	enabled bool
	kind    step.Kind
	dir     string
}

func (p *plan) Items() []step.PlanItem {
	if !p.enabled {
		return nil
	}
	if p.kind == step.Same {
		return []step.PlanItem{{Kind: step.Same, Action: "installed", Subject: item}}
	}
	return []step.PlanItem{{Kind: step.Add, Action: "install", Subject: item, Detail: "into ~/.tmux/plugins/tpm"}}
}

func (*plan) Questions() []prompt.Question { return nil }

// Apply clones tpm, retrying network failures.
func (p *plan) Apply(ctx context.Context, env step.Env, _ prompt.Answers) error {
	if !p.enabled || p.kind != step.Add {
		return nil
	}
	if _, err := env.FS.Lstat(p.dir); err == nil {
		env.Report.Skip(item, "already installed")
		return nil
	}
	env.Report.Start(item)
	cmd := sys.Cmd{
		Name: "git",
		Args: []string{"clone", "--depth", "1", tpmURL, p.dir},
		Env:  []string{"GIT_TERMINAL_PROMPT=0"},
	}
	var res sys.Result
	err := sys.Retry(ctx, p.s.Attempts, p.s.Backoff,
		func(err error) bool { return sys.IsNetworkError(err, res.Output()) },
		func() error {
			var err error
			res, err = env.Run.Exec(ctx, cmd)
			return err
		})
	if err != nil {
		env.Report.Fail(item, fmt.Errorf("clone tpm: %w", err), res.Output())
		return nil
	}
	env.Report.Done(item, "press prefix + I in tmux to install your plugins")
	return nil
}
