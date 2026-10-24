package configs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/configs"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/sys/systest"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// setup creates a repo with home/ files and a home folder in the given state.
func setup(t *testing.T) (step.Env, *steptest.Recorder) {
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "configs", "", &systest.Fake{}, rec)
	src := filepath.Join(env.Repo, "home")
	write(t, filepath.Join(src, ".zshrc"), "new zshrc")
	write(t, filepath.Join(src, ".tmux.conf"), "same")
	write(t, filepath.Join(src, ".config/starship.toml"), "starship")
	write(t, filepath.Join(src, ".ssh/config"), "Include x")
	write(t, filepath.Join(src, ".wezterm.lua"), "wez")
	write(t, filepath.Join(src, ".linked"), "real file")
	write(t, filepath.Join(src, ".DS_Store"), "junk")

	write(t, filepath.Join(env.Home, ".zshrc"), "old zshrc")
	write(t, filepath.Join(env.Home, ".tmux.conf"), "same")
	if err := os.MkdirAll(filepath.Join(env.Home, ".wezterm.lua"), 0o755); err != nil { // folder where a file should go
		t.Fatal(err)
	}
	write(t, filepath.Join(env.Home, "elsewhere"), "link target")
	if err := os.Symlink(filepath.Join(env.Home, "elsewhere"), filepath.Join(env.Home, ".linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.Home, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	return env, rec
}

func TestPlan(t *testing.T) {
	env, _ := setup(t)
	p, err := configs.New().Prepare(context.Background(), steptest.ReadOnly(env))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]step.Kind{}
	for _, it := range p.Items() {
		got[it.Subject] = it.Kind
	}
	want := map[string]step.Kind{
		"~/.zshrc":                step.Change,
		"~/.tmux.conf":            step.Same,
		"~/.config/starship.toml": step.Add,
		"~/.ssh/config":           step.Add,
		"~/.wezterm.lua":          step.Conflict,
		"~/.linked":               step.Change,
	}
	if len(got) != len(want) {
		t.Errorf("plan has %d items, want %d (.DS_Store must be skipped): %v", len(got), len(want), got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: kind %v, want %v", k, got[k], w)
		}
	}
}

func TestApply(t *testing.T) {
	env, rec := setup(t)
	p, err := configs.New().Prepare(context.Background(), steptest.ReadOnly(env))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), env, prompt.Answers{}); err != nil {
		t.Fatal(err)
	}

	t.Run("replaced file is backed up", func(t *testing.T) {
		if got := read(t, filepath.Join(env.Home, ".zshrc")); got != "new zshrc" {
			t.Errorf(".zshrc = %q", got)
		}
		if got := read(t, filepath.Join(env.BackupDir, ".zshrc")); got != "old zshrc" {
			t.Errorf("backup = %q, want the old content", got)
		}
	})
	t.Run("new file in a new folder", func(t *testing.T) {
		if got := read(t, filepath.Join(env.Home, ".config/starship.toml")); got != "starship" {
			t.Errorf("starship.toml = %q", got)
		}
	})
	t.Run("symlink replaced and link backed up", func(t *testing.T) {
		info, err := os.Lstat(filepath.Join(env.Home, ".linked"))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("~/.linked should now be a regular file (err %v)", err)
		}
		if got := read(t, filepath.Join(env.Home, "elsewhere")); got != "link target" {
			t.Errorf("the old link's target was modified: %q", got)
		}
		dest, err := os.Readlink(filepath.Join(env.BackupDir, ".linked"))
		if err != nil || dest != filepath.Join(env.Home, "elsewhere") {
			t.Errorf("backup link = %q (err %v)", dest, err)
		}
	})
	t.Run("ssh permissions", func(t *testing.T) {
		f, _ := os.Stat(filepath.Join(env.Home, ".ssh/config"))
		d, _ := os.Stat(filepath.Join(env.Home, ".ssh"))
		if f.Mode().Perm() != 0o600 || d.Mode().Perm() != 0o700 {
			t.Errorf("~/.ssh/config %v, ~/.ssh %v; want 0600 and 0700", f.Mode().Perm(), d.Mode().Perm())
		}
	})
	t.Run("conflict left alone and reported", func(t *testing.T) {
		info, err := os.Stat(filepath.Join(env.Home, ".wezterm.lua"))
		if err != nil || !info.IsDir() {
			t.Error("the conflicting folder was touched")
		}
	})
	t.Run("outcomes", func(t *testing.T) {
		want := map[string]string{
			"~/.zshrc": "done", "~/.config/starship.toml": "done", "~/.ssh/config": "done",
			"~/.linked": "done", "~/.tmux.conf": "skipped", "~/.wezterm.lua": "failed",
		}
		got := rec.Outcomes()
		for k, w := range want {
			if got[k] != w {
				t.Errorf("%s: %q, want %q", k, got[k], w)
			}
		}
	})
}

func TestMissingSourceFolder(t *testing.T) {
	env := steptest.Env(t, "configs", "[configs]\nsource = \"dotfiles\"\n", &systest.Fake{}, nil)
	if _, err := configs.New().Prepare(context.Background(), env); err == nil {
		t.Error("expected an error for a missing source folder")
	}
}

func TestSourceOutsideRepo(t *testing.T) {
	env := steptest.Env(t, "configs", "[configs]\nsource = \"../..\"\n", &systest.Fake{}, nil)
	if _, err := configs.New().Prepare(context.Background(), env); err == nil {
		t.Error("expected an error for a source outside the repo")
	}
}
