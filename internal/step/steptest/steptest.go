// Package steptest helps test steps: build an Env and record events.
package steptest

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bitbynik/exon/internal/config"
	"github.com/bitbynik/exon/internal/events"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys"
)

// Recorder is an events.Sink that keeps every event.
type Recorder struct {
	mu     sync.Mutex
	Events []events.Event
}

// Emit records e.
func (r *Recorder) Emit(e events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, e)
}

// Outcomes returns item -> "done" / "skipped" / "failed".
func (r *Recorder) Outcomes() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]string{}
	for _, e := range r.Events {
		switch e := e.(type) {
		case events.ItemDone:
			out[e.Item] = "done"
		case events.ItemSkipped:
			out[e.Item] = "skipped"
		case events.ItemFailed:
			out[e.Item] = "failed"
		}
	}
	return out
}

// Env returns an Env for the named step with home and repo in temp folders.
// tomlText is the whole exon.toml content. The Env uses the real file system
// (inside those temp folders) and run for commands.
func Env(t *testing.T, name, tomlText string, run sys.Runner, rec *Recorder) step.Env {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, config.FileName), []byte(tomlText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(repo, config.FileName), []string{"apps", "configs"})
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		rec = &Recorder{}
	}
	return step.Env{
		Run:       run,
		FS:        sys.OSFS{},
		Report:    step.NewReporter(rec, name),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:    cfg.Section(name),
		Repo:      repo,
		Home:      t.TempDir(),
		BackupDir: filepath.Join(t.TempDir(), "backup"),
	}
}

// ReadOnly returns env with read-only Run and FS, as the engine uses for Prepare.
func ReadOnly(env step.Env) step.Env {
	env.Run = sys.ReadOnlyRunner(env.Run)
	env.FS = sys.ReadOnlyFS(env.FS)
	return env
}
