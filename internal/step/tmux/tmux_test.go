package tmux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
)

const enabled = "[tmux]\ninstall_plugin_manager = true\n"

func prepare(t *testing.T, run *systest.Fake, existing bool) (step.Plan, step.Env, *steptest.Recorder) {
	t.Helper()
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "tmux", enabled, run, rec)
	if existing {
		if err := os.MkdirAll(filepath.Join(env.Home, ".tmux", "plugins", "tpm"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := &Step{Attempts: 3, Backoff: time.Millisecond}
	p, err := s.Prepare(context.Background(), steptest.ReadOnly(env))
	if err != nil {
		t.Fatal(err)
	}
	return p, env, rec
}

func TestAlreadyInstalled(t *testing.T) {
	run := &systest.Fake{}
	p, env, _ := prepare(t, run, true)
	if it := p.Items(); len(it) != 1 || it[0].Kind != step.Same {
		t.Errorf("items %+v, want one Same", it)
	}
	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	if len(run.Execs()) != 0 {
		t.Errorf("ran %q although tpm is installed", run.Execs())
	}
}

func TestInstall(t *testing.T) {
	tries := 0
	run := &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) {
		tries++
		if tries < 3 {
			return systest.Fail(c, "fatal: unable to access: Could not resolve host: github.com")
		}
		return sys.Result{}, nil
	}}
	p, env, rec := prepare(t, run, false)
	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	execs := run.Execs()
	want := "git clone --depth 1 https://github.com/tmux-plugins/tpm " + filepath.Join(env.Home, ".tmux", "plugins", "tpm")
	if len(execs) != 3 || execs[2] != want {
		t.Errorf("ran %q, want 3 tries of %q (network errors are retried)", execs, want)
	}
	if rec.Outcomes()["tmux plugin manager"] != "done" {
		t.Errorf("outcomes %v", rec.Outcomes())
	}
}

func TestCloneFails(t *testing.T) {
	run := &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) {
		return systest.Fail(c, "fatal: repository not found")
	}}
	p, env, rec := prepare(t, run, false)
	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	if len(run.Execs()) != 1 {
		t.Errorf("ran %d times, want 1 (not a network error, no retry)", len(run.Execs()))
	}
	if !strings.Contains(rec.Outcomes()["tmux plugin manager"], "failed") {
		t.Errorf("outcomes %v, want failed", rec.Outcomes())
	}
}
