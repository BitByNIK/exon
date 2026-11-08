package projectinit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/project"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/projectinit"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
)

// world is a project folder and a pretend git and Docker around it.
type world struct {
	t      *testing.T
	dir    string
	isRepo bool
	docker bool // docker is installed and running
	volume bool // the toolbox volume exists
	image  string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &world{t: t, dir: dir, isRepo: true, docker: true, volume: true, image: project.DefaultImage}
}

func (w *world) run() *systest.Fake {
	fake := &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) {
		switch {
		case systest.Is(c, "git -C"):
			if !w.isRepo {
				return systest.Fail(c, "fatal: not a git repository")
			}
			return sys.Result{Stdout: []byte(".git\n")}, nil
		case systest.Is(c, "docker volume ls") && w.volume:
			return sys.Result{Stdout: []byte("toolbox\n")}, nil
		}
		return sys.Result{}, nil
	}}
	if w.docker {
		fake.Paths = map[string]string{"docker": "/usr/local/bin/docker"}
	}
	return fake
}

func (w *world) env() (step.Env, *steptest.Recorder) {
	rec := &steptest.Recorder{}
	return steptest.Env(w.t, "init", "", w.run(), rec), rec
}

func (w *world) prepare() (step.Plan, step.Env, *steptest.Recorder, error) {
	env, rec := w.env()
	p, err := projectinit.New(w.dir, w.image, project.DefaultUser).Prepare(context.Background(), steptest.ReadOnly(env))
	return p, env, rec, err
}

func (w *world) write(rel, content string) {
	w.t.Helper()
	path := filepath.Join(w.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) read(rel string) string {
	w.t.Helper()
	b, err := os.ReadFile(filepath.Join(w.dir, rel))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

func actions(p step.Plan) map[string]step.Kind {
	out := map[string]step.Kind{}
	for _, it := range p.Items() {
		out[it.Action+" "+it.Subject] = it.Kind
	}
	return out
}

func apply(t *testing.T, p step.Plan, env step.Env) {
	t.Helper()
	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
}

func TestInitCreatesTheFilesAndTheGitignoreLine(t *testing.T) {
	w := newWorld(t)
	w.write(".gitignore", "*.log") // no trailing newline
	p, env, rec, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]step.Kind{
		"create .devcontainer/local/devcontainer.json": step.Add,
		"create .devcontainer/local/post-create.sh":    step.Add,
		"ignore .devcontainer/local/":                  step.Add,
	}
	got := actions(p)
	for k, kind := range want {
		if got[k] != kind {
			t.Errorf("plan lacks %q as kind %v: %v", k, kind, p.Items())
		}
	}

	apply(t, p, env)

	if !strings.Contains(w.read(".devcontainer/local/devcontainer.json"), `"name": "api (local)"`) {
		t.Error("devcontainer.json was not written for this project")
	}
	info, err := os.Stat(filepath.Join(w.dir, ".devcontainer/local/post-create.sh"))
	if err != nil || info.Mode()&0o100 == 0 {
		t.Errorf("post-create.sh must exist and be executable: %v", err)
	}
	want1 := "*.log\n\n# your own dev container (the toolbox and your other projects), see .devcontainer/\n.devcontainer/local/\n"
	if got := w.read(".gitignore"); got != want1 {
		t.Errorf(".gitignore = %q, want the old line kept and the new one added after a blank line", got)
	}
	if got := rec.Outcomes(); len(got) != 3 || got[".devcontainer/local/devcontainer.json"] != "done" {
		t.Errorf("outcomes = %v, want the three items done", got)
	}
}

func TestInitMakesTheGitignoreWhenThereIsNone(t *testing.T) {
	w := newWorld(t)
	p, env, _, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p, env)
	want := "# your own dev container (the toolbox and your other projects), see .devcontainer/\n.devcontainer/local/\n"
	if got := w.read(".gitignore"); got != want {
		t.Errorf(".gitignore = %q, want just the new block, with no blank line first", got)
	}
}

func TestInitLeavesAGitignoreThatAlreadyHasTheLine(t *testing.T) {
	for _, line := range []string{".devcontainer/local/", "/.devcontainer/local/", ".devcontainer/local", "  .devcontainer/local/  "} {
		t.Run(line, func(t *testing.T) {
			w := newWorld(t)
			w.write(".gitignore", "build/\n"+line+"\n")
			p, env, _, err := w.prepare()
			if err != nil {
				t.Fatal(err)
			}
			if k := actions(p)["ignored .devcontainer/local/"]; k != step.Same {
				t.Errorf("plan = %v, want the existing line recognised", p.Items())
			}
			apply(t, p, env)
			if got := w.read(".gitignore"); got != "build/\n"+line+"\n" {
				t.Errorf(".gitignore was changed to %q", got)
			}
		})
	}
}

func TestInitNeverWritesTheGitExcludeFile(t *testing.T) {
	w := newWorld(t)
	exclude := filepath.Join(w.dir, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, env, _, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p, env)
	if got, _ := os.ReadFile(exclude); string(got) != "*.log\n" {
		t.Errorf("exclude file = %q, want it untouched: the ignore line goes in .gitignore", got)
	}
}

func TestInitFindsNothingToDoTheSecondTime(t *testing.T) {
	w := newWorld(t)
	p, env, _, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p, env)

	again, _, _, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range again.Items() {
		if it.Kind.Changes() {
			t.Errorf("%+v: a second run must find everything in place", it)
		}
	}
}

func TestInitNeverOverwritesAFileTheUserChanged(t *testing.T) {
	w := newWorld(t)
	w.write(".devcontainer/local/devcontainer.json", `{"image": "my own"}`)

	p, env, _, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	if k := actions(p)["kept .devcontainer/local/devcontainer.json"]; k != step.Warn {
		t.Errorf("plan = %v, want a warning that the changed file is kept", p.Items())
	}
	if k := actions(p)["create .devcontainer/local/post-create.sh"]; k != step.Add {
		t.Errorf("plan = %v, want the missing script still created", p.Items())
	}

	apply(t, p, env)
	if got := w.read(".devcontainer/local/devcontainer.json"); got != `{"image": "my own"}` {
		t.Errorf("the user's file was changed to %q", got)
	}
	if _, err := os.Stat(filepath.Join(w.dir, ".devcontainer/local/post-create.sh")); err != nil {
		t.Error("the missing script was not created")
	}
}

func TestInitInAFolderThatIsNotARepositoryStillWorks(t *testing.T) {
	w := newWorld(t)
	w.isRepo = false
	p, env, _, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	if k := actions(p)["not git "+w.dir]; k != step.Note {
		t.Errorf("plan = %v, want a note about adding the ignore line by hand", p.Items())
	}
	apply(t, p, env)
	if _, err := os.Stat(filepath.Join(w.dir, ".devcontainer/local/devcontainer.json")); err != nil {
		t.Error("the files must still be made")
	}
	if _, err := os.Stat(filepath.Join(w.dir, ".gitignore")); err == nil {
		t.Error("a .gitignore was written although this is not a repository")
	}
}

func TestInitBuildsFromTheProjectsDockerfileUnlessAnImageIsChosen(t *testing.T) {
	tests := []struct {
		name, image string
		wantBuild   bool
	}{
		{"no image chosen: use the Dockerfile", "", true},
		{"an image chosen: it wins", "my/image:1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.write(".devcontainer/Dockerfile", "FROM scratch\n")
			w.image = tt.image
			p, env, _, err := w.prepare()
			if err != nil {
				t.Fatal(err)
			}
			apply(t, p, env)
			cfg := w.read(".devcontainer/local/devcontainer.json")
			if built := strings.Contains(cfg, `"dockerfile": "../Dockerfile"`); built != tt.wantBuild {
				t.Errorf("built from the Dockerfile = %v, want %v:\n%s", built, tt.wantBuild, cfg)
			}
			if hasImage := strings.Contains(cfg, `"image":`); hasImage == tt.wantBuild {
				t.Errorf("image set = %v, want %v:\n%s", hasImage, !tt.wantBuild, cfg)
			}
		})
	}
}

func TestInitWithoutAProjectDockerfileUsesTheDefaultImage(t *testing.T) {
	w := newWorld(t)
	w.image = ""
	p, env, _, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p, env)
	if cfg := w.read(".devcontainer/local/devcontainer.json"); !strings.Contains(cfg, `"image": "`+project.DefaultImage+`"`) {
		t.Errorf("want the default image when nothing is chosen and there is no Dockerfile:\n%s", cfg)
	}
}

func TestInitRunsTheProjectsOwnPostCreateScript(t *testing.T) {
	w := newWorld(t)
	w.write(".devcontainer/post-create.sh", "#!/bin/sh\n")
	p, env, _, err := w.prepare()
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p, env)
	if script := w.read(".devcontainer/local/post-create.sh"); !strings.Contains(script, "/../post-create.sh") {
		t.Errorf("want the project's script run from the generated one:\n%s", script)
	}
}

func TestInitWarnsWhenTheToolboxDoesNotExistYet(t *testing.T) {
	tests := []struct {
		name       string
		docker     bool
		volume     bool
		wantWarned bool
	}{
		{"volume missing", true, false, true},
		{"volume there", true, true, false},
		{"docker not running: init does not need it", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.docker, w.volume = tt.docker, tt.volume
			p, _, _, err := w.prepare()
			if err != nil {
				t.Fatal(err)
			}
			_, warned := actions(p)["toolbox toolbox"]
			if warned != tt.wantWarned {
				t.Errorf("warned = %v, want %v: %v", warned, tt.wantWarned, p.Items())
			}
		})
	}
}

func TestInitNeedsAFolder(t *testing.T) {
	w := newWorld(t)
	w.dir = filepath.Join(w.dir, "does-not-exist")
	if _, _, _, err := w.prepare(); err == nil {
		t.Error("want an error for a folder that does not exist")
	}
}
