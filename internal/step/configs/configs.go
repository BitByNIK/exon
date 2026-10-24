// Package configs copies the files under the config repo's home/ folder to the
// same paths under the user's home folder, backing up anything it replaces.
package configs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/usererr"
)

// Config is the [configs] section of exon.toml.
type Config struct {
	// Source is the folder in the config repo that mirrors ~. Default "home".
	Source string `toml:"source"`
}

// Step is the configs step.
type Step struct{}

// New returns the configs step.
func New() *Step { return &Step{} }

// Name implements step.Step.
func (*Step) Name() string { return "configs" }

// Summary implements step.Step.
func (*Step) Summary() string { return "Copy config files into your home folder" }

// Requires implements step.Step.
func (*Step) Requires() []string { return nil }

// skipNames are files never copied, wherever they appear.
var skipNames = map[string]bool{".DS_Store": true, ".gitkeep": true}

// Prepare walks the source folder and compares every file with its target.
func (*Step) Prepare(_ context.Context, env step.Env) (step.Plan, error) {
	if !env.Config.Present() {
		return &plan{}, nil // no [configs] section: nothing to copy
	}
	cfg := Config{Source: "home"}
	if err := env.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	if !filepath.IsLocal(cfg.Source) {
		return nil, usererr.New(
			fmt.Sprintf("[configs] source %q must be a folder inside the config repo", cfg.Source), "", nil)
	}
	root := filepath.Join(env.Repo, cfg.Source)
	if info, err := env.FS.Stat(root); err != nil || !info.IsDir() {
		return nil, usererr.New(
			fmt.Sprintf("config folder %s/ not found in the config repo", cfg.Source),
			"Create it and put files in it at the same paths they should have under ~.", err)
	}

	p := &plan{}
	err := env.FS.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || skipNames[d.Name()] {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		f, err := inspect(env, path, rel)
		if err != nil {
			return err
		}
		p.files = append(p.files, f)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s/: %w", cfg.Source, err)
	}
	return p, nil
}

// file is one file to place.
type file struct {
	rel     string // path relative to ~
	target  string // absolute target path
	data    []byte
	perm    fs.FileMode
	kind    step.Kind
	symlink bool   // the existing target is a symlink
	reason  string // detail shown in the plan
}

func inspect(env step.Env, src, rel string) (file, error) {
	info, err := env.FS.Stat(src) // follows symlinks inside the repo
	if err != nil {
		return file{}, err
	}
	if info.IsDir() {
		return file{}, fmt.Errorf("%s is a symlink to a folder; only files are supported", rel)
	}
	data, err := env.FS.ReadFile(src)
	if err != nil {
		return file{}, err
	}
	f := file{rel: rel, target: filepath.Join(env.Home, rel), data: data, perm: info.Mode().Perm()}
	if isSSH(rel) {
		f.perm = 0o600 // ssh refuses config files others can read
	}

	existing, err := env.FS.Lstat(f.target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		f.kind = step.Add
	case err != nil:
		return file{}, err
	case existing.IsDir():
		f.kind, f.reason = step.Conflict, "a folder exists here; not touched"
	case existing.Mode()&fs.ModeSymlink != 0:
		f.kind, f.symlink, f.reason = step.Change, true, "replaces a symlink (link is backed up)"
	default:
		current, err := env.FS.ReadFile(f.target)
		if err != nil {
			return file{}, err
		}
		if bytes.Equal(current, data) {
			f.kind = step.Same
		} else {
			f.kind, f.reason = step.Change, "existing file is backed up"
		}
	}
	return f, nil
}

func isSSH(rel string) bool {
	return rel == ".ssh" || strings.HasPrefix(rel, ".ssh"+string(filepath.Separator))
}

type plan struct{ files []file }

func (p *plan) Items() []step.PlanItem {
	items := make([]step.PlanItem, 0, len(p.files))
	for _, f := range p.files {
		action := map[step.Kind]string{
			step.Add: "new", step.Change: "replace", step.Same: "same", step.Conflict: "conflict",
		}[f.kind]
		items = append(items, step.PlanItem{Kind: f.kind, Action: action, Subject: "~/" + f.rel, Detail: f.reason})
	}
	return items
}

func (*plan) Questions() []prompt.Question { return nil }

func (p *plan) Apply(ctx context.Context, env step.Env, _ prompt.Answers) error {
	for _, f := range p.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := "~/" + f.rel
		switch f.kind {
		case step.Same:
			env.Report.Skip(item, "already up to date")
		case step.Conflict:
			env.Report.Fail(item, errors.New(f.reason), nil)
		case step.Add, step.Change:
			env.Report.Start(item)
			if err := place(env, f); err != nil {
				env.Report.Fail(item, err, nil)
				continue
			}
			detail := "copied"
			if f.kind == step.Change {
				detail = "replaced (backup saved)"
			}
			env.Report.Done(item, detail)
		}
	}
	return nil
}

// place backs up the existing target if needed, then writes the new file.
func place(env step.Env, f file) error {
	if f.kind == step.Change {
		if err := backup(env, f); err != nil {
			return fmt.Errorf("back up: %w", err)
		}
	}
	dirPerm := fs.FileMode(0o755)
	if isSSH(f.rel) {
		dirPerm = 0o700
	}
	if err := env.FS.MkdirAll(filepath.Dir(f.target), dirPerm); err != nil {
		return err
	}
	if err := env.FS.WriteFile(f.target, f.data, f.perm); err != nil {
		return err
	}
	if isSSH(f.rel) {
		// MkdirAll leaves an existing ~/.ssh as it was; ssh needs it private.
		return env.FS.Chmod(filepath.Join(env.Home, ".ssh"), 0o700)
	}
	return nil
}

// backup copies the current target (file or symlink) into env.BackupDir, keeping its path.
func backup(env step.Env, f file) error {
	dst := filepath.Join(env.BackupDir, f.rel)
	if err := env.FS.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if f.symlink {
		link, err := env.FS.Readlink(f.target)
		if err != nil {
			return err
		}
		return env.FS.Symlink(link, dst)
	}
	info, err := env.FS.Lstat(f.target)
	if err != nil {
		return err
	}
	data, err := env.FS.ReadFile(f.target)
	if err != nil {
		return err
	}
	return env.FS.WriteFile(dst, data, info.Mode().Perm())
}
