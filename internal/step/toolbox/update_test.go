package toolbox_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/dev/volume"
	"github.com/bitbynik/exon/internal/events"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/step/toolbox"
	"github.com/bitbynik/exon/internal/sys/systest"
	"github.com/bitbynik/exon/internal/usererr"
)

const (
	twoTools     = "[dev]\ntools = [\"omp\", \"gh\"]\n"
	withMCP      = "[dev]\ntools = [\"omp\", \"codebase-memory-mcp\"]\n"
	withOhMyBash = "[dev]\ntools = [\"omp\", \"oh-my-bash\"]\n"
	oldOMP       = "omp|v18.8.7\n"
	currentGH    = "gh|v2.103.2\n"
	ohMyBashSHA  = "4d86087"
	newerCommit  = "9999999000000000000000000000000000000000"
	sameCommit   = "4d86087aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	currentOMP   = "omp|v18.12.6\n"
)

// containerFor is the fingerprint the volume holds when its container files are current.
func containerFor(tools ...tool.Tool) string {
	return "container|" + volume.ContainerFor(tools).Hash + "\n"
}

func prepareUpdate(t *testing.T, d *docker, dl *systest.FakeDownloader, tomlText string, only []string, version string) (step.Plan, step.Env, *steptest.Recorder, error) {
	t.Helper()
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "dev", tomlText, d.fake(), rec)
	env.Net = dl
	p, err := toolbox.NewUpdate(501, 20, only, version).Prepare(context.Background(), steptest.ReadOnly(env))
	return p, env, rec, err
}

func latestGitHub(t *testing.T) *systest.FakeDownloader {
	return github(t,
		rel{tool.OMP(), "v18.8.7", "sha256:0ld"},
		rel{tool.OMP(), "v18.12.6", "sha256:aaaa"}, // registered last, so it is the latest
		rel{tool.GH(), "v2.103.2", "sha256:bbbb"},
		rel{tool.CodebaseMemory(), "v0.12.1", "sha256:cccc"},
	)
}

func TestUpdateShowsWhatChangesFromAndTo(t *testing.T) {
	d := &docker{volume: true, image: true, versions: oldOMP + currentGH + containerFor(tool.OMP(), tool.GH())}
	p, _, _, err := prepareUpdate(t, d, latestGitHub(t), twoTools, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := itemsOf(p)
	if it := got["update omp"]; it.Kind != step.Change || it.Detail != "v18.8.7 → v18.12.6, 5 MB" {
		t.Errorf("update omp = %+v, want a Change from the old version to the new one", it)
	}
	if it := got["installed gh"]; it.Kind != step.Same {
		t.Errorf("gh is current and must be left alone: %v", p.Items())
	}
	if len(got) != 2 {
		t.Errorf("plan = %v, want just those two lines", p.Items())
	}
}

func TestUpdateWhenEverythingIsCurrentChangesNothing(t *testing.T) {
	d := &docker{volume: true, image: true, versions: currentOMP + currentGH + containerFor(tool.OMP(), tool.GH())}
	p, _, _, err := prepareUpdate(t, d, latestGitHub(t), twoTools, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range p.Items() {
		if it.Kind != step.Same {
			t.Errorf("%+v: nothing is newer, so nothing should change", it)
		}
	}
}

func TestUpdateNeverInstallsWhatIsNotThere(t *testing.T) {
	d := &docker{volume: true, image: true, versions: oldOMP + containerFor(tool.OMP(), tool.GH())} // no gh
	p, _, _, err := prepareUpdate(t, d, latestGitHub(t), twoTools, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := itemsOf(p)
	if it := got["not installed gh"]; it.Kind != step.Note || !strings.Contains(it.Detail, "exon dev install") {
		t.Errorf("plan = %v, want a note pointing at install for gh", p.Items())
	}
	for key, it := range got {
		if it.Kind == step.Add && strings.HasSuffix(key, " gh") {
			t.Errorf("update must never install: %v", p.Items())
		}
	}
}

func TestUpdateOneToolToAChosenVersion(t *testing.T) {
	d := &docker{volume: true, image: true, versions: currentOMP + currentGH + containerFor(tool.OMP(), tool.GH())}
	p, _, _, err := prepareUpdate(t, d, latestGitHub(t), twoTools, []string{"omp"}, "18.8.7") // an older one: a rollback
	if err != nil {
		t.Fatal(err)
	}
	got := itemsOf(p)
	if it := got["update omp"]; it.Kind != step.Change || !strings.HasPrefix(it.Detail, "v18.12.6 → v18.8.7") {
		t.Errorf("plan = %v, want omp moved back to the chosen version", p.Items())
	}
	if _, touched := got["installed gh"]; touched || len(got) != 1 {
		t.Errorf("plan = %v, want only the named tool", p.Items())
	}
}

func TestUpdateWithTheVersionItAlreadyHasChangesNothing(t *testing.T) {
	d := &docker{volume: true, image: true, versions: currentOMP + containerFor(tool.OMP(), tool.GH())}
	p, _, _, err := prepareUpdate(t, d, latestGitHub(t), twoTools, []string{"omp"}, "v18.12.6")
	if err != nil {
		t.Fatal(err)
	}
	if it := itemsOf(p)["installed omp"]; it.Kind != step.Same {
		t.Errorf("plan = %v, want omp unchanged", p.Items())
	}
}

func TestUpdateArgumentRules(t *testing.T) {
	d := &docker{volume: true, image: true, versions: currentOMP + currentGH}
	tests := []struct {
		name    string
		toml    string
		only    []string
		version string
		msg     string
	}{
		{"version without a tool", twoTools, nil, "v1", "exactly one tool"},
		{"version with two tools", twoTools, []string{"omp", "gh"}, "v1", "exactly one tool"},
		{"version for a cloned tool", withOhMyBash, []string{"oh-my-bash"}, "v1", "no versions"},
		{"unknown tool", twoTools, []string{"vim"}, "", "unknown tool"},
		{"known tool that is not chosen", twoTools, []string{"fzf"}, "", "not one of your dev tools"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := prepareUpdate(t, d, latestGitHub(t), tt.toml, tt.only, tt.version)
			ue, ok := usererr.As(err)
			if !ok || !strings.Contains(ue.Msg, tt.msg) {
				t.Errorf("err = %v, want a user error containing %q", err, tt.msg)
			}
		})
	}
}

func TestUpdateOfAClonedToolComparesCommits(t *testing.T) {
	versions := ohMyBashVersions() + containerFor(tool.OMP(), tool.OhMyBash())

	t.Run("up to date", func(t *testing.T) {
		d := &docker{volume: true, image: true, versions: versions, remoteHead: sameCommit}
		p, _, _, err := prepareUpdate(t, d, latestGitHub(t), withOhMyBash, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if it := itemsOf(p)["installed oh-my-bash"]; it.Kind != step.Same {
			t.Errorf("plan = %v, want oh-my-bash unchanged", p.Items())
		}
	})
	t.Run("newer commit", func(t *testing.T) {
		d := &docker{volume: true, image: true, versions: versions, remoteHead: newerCommit}
		p, _, _, err := prepareUpdate(t, d, latestGitHub(t), withOhMyBash, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if it := itemsOf(p)["update oh-my-bash"]; it.Kind != step.Change || it.Detail != ohMyBashSHA+" → 9999999" {
			t.Errorf("plan = %v, want the short commits, old to new", p.Items())
		}
	})
	t.Run("helper image missing: say so, do not download it", func(t *testing.T) {
		d := &docker{volume: true, image: false, versions: versions, remoteHead: newerCommit}
		p, env, _, err := prepareUpdate(t, d, latestGitHub(t), withOhMyBash, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if it := itemsOf(p)["not checked oh-my-bash"]; it.Kind != step.Note {
			t.Errorf("plan = %v, want a note that it was not checked", p.Items())
		}
		for _, q := range env.Run.(*systest.Fake).Queries() {
			if strings.Contains(q, "ls-remote") {
				t.Errorf("asked GitHub through a container that would have pulled the image: %s", q)
			}
		}
	})
}

func ohMyBashVersions() string { return currentOMP + "oh-my-bash|" + ohMyBashSHA + "\n" }

func TestUpdateWarnsAboutOpenMCPSessions(t *testing.T) {
	base := "omp|v18.12.6\ncodebase-memory-mcp|v0.12.0\n" + containerFor(tool.OMP(), tool.CodebaseMemory())

	t.Run("sessions open", func(t *testing.T) {
		d := &docker{volume: true, image: true, versions: base, containers: []string{"web", "api"}, open: []string{"web"}}
		p, _, _, err := prepareUpdate(t, d, latestGitHub(t), withMCP, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		w := itemsOf(p)["close first codebase-memory-mcp"]
		if w.Kind != step.Warn || !strings.Contains(w.Detail, "web") || strings.Contains(w.Detail, "api") {
			t.Errorf("plan = %v, want a warning naming only the container with it open", p.Items())
		}
		if _, changed := itemsOf(p)["update codebase-memory-mcp"]; !changed {
			t.Error("the warning must not stop the update from being planned")
		}
	})
	t.Run("no session open", func(t *testing.T) {
		d := &docker{volume: true, image: true, versions: base, containers: []string{"web"}}
		p, _, _, err := prepareUpdate(t, d, latestGitHub(t), withMCP, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, warned := itemsOf(p)["close first codebase-memory-mcp"]; warned {
			t.Errorf("plan = %v, want no warning when nothing has it open", p.Items())
		}
	})
	t.Run("the MCP is not changing", func(t *testing.T) {
		d := &docker{
			volume: true, image: true, versions: "omp|v18.8.7\ncodebase-memory-mcp|v0.12.1\n" + containerFor(tool.OMP(), tool.CodebaseMemory()),
			containers: []string{"web"}, open: []string{"web"},
		}
		p, env, _, err := prepareUpdate(t, d, latestGitHub(t), withMCP, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, warned := itemsOf(p)["close first codebase-memory-mcp"]; warned {
			t.Errorf("plan = %v: replacing omp does not need the MCP closed", p.Items())
		}
		for _, q := range env.Run.(*systest.Fake).Queries() {
			if strings.HasPrefix(q, "docker ps") {
				t.Error("looked for open sessions although the MCP was not changing")
			}
		}
	})
}

func TestUpdateDoesNotCopyStarterFiles(t *testing.T) {
	d := &docker{volume: true, image: true, missing: "shell/bashrc\n", versions: oldOMP + containerFor(tool.OMP())}
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "dev", "[dev]\ntools = [\"omp\"]\n", d.fake(), rec)
	env.Net = latestGitHub(t)
	if err := os.MkdirAll(filepath.Join(env.Repo, "toolbox", "shell"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Repo, "toolbox", "shell", "bashrc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := toolbox.NewUpdate(501, 20, nil, "").Prepare(context.Background(), steptest.ReadOnly(env))
	if err != nil {
		t.Fatal(err)
	}
	if _, copied := itemsOf(p)["copy starter files"]; copied {
		t.Errorf("plan = %v: starter files are for install, not update", p.Items())
	}
}

func TestUpdateAppliesAndReportsFromAndTo(t *testing.T) {
	d := &docker{volume: true, image: true, versions: oldOMP + currentGH + containerFor(tool.OMP(), tool.GH()), installOut: "ok|omp\n"}
	p, env, rec, err := prepareUpdate(t, d, latestGitHub(t), twoTools, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}

	var detail string
	for _, e := range rec.Events {
		if done, ok := e.(events.ItemDone); ok && done.Item == "omp" {
			detail = done.Detail
		}
	}
	if detail != "v18.8.7 → v18.12.6" {
		t.Errorf("omp finished with %q, want the old and new versions", detail)
	}
	fetches := env.Net.(*systest.FakeDownloader).Fetches()
	if len(fetches) != 1 || !strings.Contains(fetches[0].URL, "v18.12.6/omp-linux-arm64") || fetches[0].SHA256 != "aaaa" {
		t.Errorf("downloads = %+v, want only omp's new file, with its checksum", fetches)
	}
	if strings.Contains(d.stage.plan, "gh") {
		t.Errorf("the container was asked to touch gh, which is current: %q", d.stage.plan)
	}
}
