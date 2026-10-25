package volume_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/dev/volume"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
	"github.com/bitbynik/exon/internal/usererr"
)

func newVolume(fake *systest.Fake) volume.Volume {
	return volume.New(fake, sys.OSFS{}, 501, 20)
}

func out(s string) (sys.Result, error) { return sys.Result{Stdout: []byte(s)}, nil }

func TestCheck(t *testing.T) {
	t.Run("docker missing", func(t *testing.T) {
		err := newVolume(&systest.Fake{}).Check(context.Background())
		if ue, ok := usererr.As(err); !ok || !strings.Contains(ue.Msg, "docker not found") {
			t.Errorf("err = %v, want a user error saying docker was not found", err)
		}
	})
	t.Run("docker not running", func(t *testing.T) {
		fake := &systest.Fake{
			Paths:   map[string]string{"docker": "/usr/local/bin/docker"},
			Handler: func(c sys.Cmd) (sys.Result, error) { return systest.Fail(c, "Cannot connect to the Docker daemon") },
		}
		err := newVolume(fake).Check(context.Background())
		if ue, ok := usererr.As(err); !ok || !strings.Contains(ue.Msg, "not running") || ue.Hint == "" {
			t.Errorf("err = %v, want a user error with a hint", err)
		}
	})
	t.Run("ready", func(t *testing.T) {
		fake := &systest.Fake{Paths: map[string]string{"docker": "/usr/local/bin/docker"}}
		if err := newVolume(fake).Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestInstalledReadsRecordedVersions(t *testing.T) {
	fake := &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) {
		if systest.Is(c, "docker volume ls") {
			return out("toolbox\n")
		}
		return out("omp|v18.12.6\nfzf|v0.74.5\n\nnot a version line\n")
	}}
	got, err := newVolume(fake).Installed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["omp"] != "v18.12.6" || got["fzf"] != "v0.74.5" {
		t.Errorf("Installed = %v, want omp and fzf with their versions", got)
	}
	run := fake.Queries()[1]
	if !strings.Contains(run, "-v toolbox:/opt/toolbox:ro") {
		t.Errorf("the volume must be mounted read-only to look: %s", run)
	}
}

// Mounting a volume that does not exist creates it, which planning must never do.
func TestInstalledNeverCreatesTheVolume(t *testing.T) {
	fake := &systest.Fake{}
	got, err := newVolume(fake).Installed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Installed = %v, want nothing", got)
	}
	for _, q := range fake.Queries() {
		if strings.HasPrefix(q, "docker run") {
			t.Errorf("ran a container although there is no volume: %s", q)
		}
	}
	if len(fake.Execs()) != 0 {
		t.Errorf("changing commands ran while looking: %v", fake.Execs())
	}
}

func sampleItems() []volume.Item {
	gh := tool.GH()
	return []volume.Item{
		{Tool: tool.OMP(), Version: "v18.12.6", File: "omp"},
		{Tool: gh, Version: "v2.103.2", File: "gh.tgz"},
		{Tool: tool.OhMyBash()},
	}
}

func TestInstallWritesThePlanAndRunsOneContainer(t *testing.T) {
	stage, seed := t.TempDir(), t.TempDir()
	fake := &systest.Fake{}
	_, err := newVolume(fake).Install(context.Background(), volume.Job{StageDir: stage, Items: sampleItems(), SeedDir: seed})
	if err != nil {
		t.Fatal(err)
	}

	plan, _ := os.ReadFile(filepath.Join(stage, "plan"))
	want := "program|omp|v18.12.6|omp|\n" +
		"state|omp|||\n" +
		"archive|gh|v2.103.2|gh.tgz|gh_2.103.2_linux_arm64/bin/gh\n" +
		"state|gh|||\n" +
		"clone|oh-my-bash||https://github.com/ohmybash/oh-my-bash|\n"
	if string(plan) != want {
		t.Errorf("plan =\n%s\nwant\n%s", plan, want)
	}
	if info, err := os.Stat(filepath.Join(stage, "install.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("install.sh must be in the stage folder and runnable: %v", err)
	}
	execs := fake.Execs()
	if len(execs) != 1 {
		t.Fatalf("ran %d changing commands, want exactly one container: %v", len(execs), execs)
	}
	for _, part := range []string{
		"docker run --rm",
		"-e OWNER=501:20",
		"-v toolbox:/opt/toolbox ",
		"-v " + stage + ":/stage:ro",
		"-v " + seed + ":/seed:ro",
		volume.Image + " sh /stage/install.sh",
	} {
		if !strings.Contains(execs[0], part) {
			t.Errorf("command lacks %q:\n%.300s", part, execs[0])
		}
	}
}

func TestInstallWithoutStarterFilesMountsNoSeed(t *testing.T) {
	fake := &systest.Fake{}
	if _, err := newVolume(fake).Install(context.Background(), volume.Job{StageDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fake.Execs()[0], ":/seed:ro") {
		t.Error("a seed folder was mounted although none was given")
	}
}

func TestInstallReportsEachTool(t *testing.T) {
	fake := &systest.Fake{Handler: func(sys.Cmd) (sys.Result, error) {
		return out("ok|omp\nfail|gh|tar: not in archive\nok|oh-my-bash\nseeded|shell/bashrc\nskipped|bin/evil\nnoise\n")
	}}
	got, err := newVolume(fake).Install(context.Background(), volume.Job{StageDir: t.TempDir(), Items: sampleItems()})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"omp", "oh-my-bash"}; !slices.Equal(got.Installed, want) {
		t.Errorf("Installed = %v, want %v", got.Installed, want)
	}
	if got.Failed["gh"] != "tar: not in archive" || len(got.Failed) != 1 {
		t.Errorf("Failed = %v, want only gh, with the reason", got.Failed)
	}
	if !slices.Equal(got.Seeded, []string{"shell/bashrc"}) || !slices.Equal(got.Skipped, []string{"bin/evil"}) {
		t.Errorf("Seeded = %v, Skipped = %v", got.Seeded, got.Skipped)
	}
}

func TestInstallFailureKeepsWhatWasDone(t *testing.T) {
	fake := &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) {
		res, err := systest.Fail(c, "docker: Error response from daemon: boom")
		res.Stdout = []byte("ok|omp\nfail|*|could not set the owner of the toolbox\n")
		return res, err
	}}
	got, err := newVolume(fake).Install(context.Background(), volume.Job{StageDir: t.TempDir(), Items: sampleItems()})
	ue, ok := usererr.As(err)
	if !ok || !strings.Contains(ue.Hint, "boom") {
		t.Fatalf("err = %v, want a user error that shows what Docker said", err)
	}
	if !slices.Equal(got.Installed, []string{"omp"}) || got.Failed["*"] == "" {
		t.Errorf("outcome = %+v, want the partial result kept", got)
	}
}

func TestOpenSessions(t *testing.T) {
	handler := func(exec map[string]int) func(sys.Cmd) (sys.Result, error) {
		return func(c sys.Cmd) (sys.Result, error) {
			if systest.Is(c, "docker ps") {
				return out("web\napi\nslim\n")
			}
			name := c.Args[1]
			if code := exec[name]; code != 0 {
				return sys.Result{}, &sys.ExitError{Cmd: c, Code: code}
			}
			return out("123\n")
		}
	}

	t.Run("only containers with the MCP running", func(t *testing.T) {
		got, err := newVolume(&systest.Fake{Handler: handler(map[string]int{"api": 1, "slim": 127})}).OpenSessions(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []string{"web"}) {
			t.Errorf("OpenSessions = %v, want [web]", got)
		}
	})
	t.Run("a real failure is not hidden", func(t *testing.T) {
		if _, err := newVolume(&systest.Fake{Handler: handler(map[string]int{"api": 2})}).OpenSessions(context.Background()); err == nil {
			t.Error("want the error from the failed exec")
		}
	})
}

func TestImagePresent(t *testing.T) {
	tests := []struct {
		name    string
		handler func(sys.Cmd) (sys.Result, error)
		want    bool
		wantErr bool
	}{
		{"present", nil, true, false},
		{"not downloaded yet", func(c sys.Cmd) (sys.Result, error) { return systest.Fail(c, "No such image") }, false, false},
		{"docker itself failed", func(sys.Cmd) (sys.Result, error) { return sys.Result{}, errors.New("socket vanished") }, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newVolume(&systest.Fake{Handler: tt.handler}).ImagePresent(context.Background())
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Errorf("ImagePresent = %v, %v; want %v, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestPullImage(t *testing.T) {
	fake := &systest.Fake{}
	if err := newVolume(fake).PullImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.Execs(); len(got) != 1 || got[0] != "docker pull --quiet "+volume.Image {
		t.Errorf("commands = %v, want one quiet pull of the helper image", got)
	}

	failing := &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) { return systest.Fail(c, "no route to host") }}
	ue, ok := usererr.As(newVolume(failing).PullImage(context.Background()))
	if !ok || ue.Hint == "" {
		t.Error("a failed pull should be a user error with a hint")
	}
}

func TestMissing(t *testing.T) {
	files := []string{"shell/bashrc", "state/omp/agent/config.yml"}

	t.Run("nothing asked, nothing run", func(t *testing.T) {
		fake := &systest.Fake{}
		got, err := newVolume(fake).Missing(context.Background(), nil)
		if err != nil || got != nil || len(fake.Queries()) != 0 {
			t.Errorf("got %v, %v after %v", got, err, fake.Queries())
		}
	})
	t.Run("no volume yet: everything is missing and nothing is mounted", func(t *testing.T) {
		fake := &systest.Fake{}
		got, err := newVolume(fake).Missing(context.Background(), files)
		if err != nil || !slices.Equal(got, files) {
			t.Fatalf("Missing = %v, %v; want all of %v", got, err, files)
		}
		for _, q := range fake.Queries() {
			if strings.HasPrefix(q, "docker run") {
				t.Errorf("mounted a volume that does not exist: %s", q)
			}
		}
	})
	t.Run("asks the volume", func(t *testing.T) {
		fake := &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) {
			if systest.Is(c, "docker volume ls") {
				return out("toolbox\n")
			}
			return out("state/omp/agent/config.yml\n")
		}}
		got, err := newVolume(fake).Missing(context.Background(), files)
		if err != nil || !slices.Equal(got, []string{"state/omp/agent/config.yml"}) {
			t.Fatalf("Missing = %v, %v", got, err)
		}
		run := fake.Queries()[1]
		if !strings.Contains(run, ":ro") || !strings.HasSuffix(run, " sh "+strings.Join(files, " ")) {
			t.Errorf("want a read-only mount and the files as arguments: %s", run)
		}
	})
}

func TestRemoteHead(t *testing.T) {
	const sha = "4d86087aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	fake := &systest.Fake{Handler: func(sys.Cmd) (sys.Result, error) { return out(sha + "\tHEAD\n") }}
	got, err := newVolume(fake).RemoteHead(context.Background(), "https://github.com/ohmybash/oh-my-bash")
	if err != nil || got != sha {
		t.Fatalf("RemoteHead = %q, %v; want %s", got, err, sha)
	}
	if q := fake.Queries(); len(q) != 1 || q[0] != "docker run --rm "+volume.Image+" git ls-remote https://github.com/ohmybash/oh-my-bash HEAD" {
		t.Errorf("commands = %v, want git to run inside the helper container, not on the Mac", q)
	}
	if len(fake.Execs()) != 0 {
		t.Error("asking must not change anything")
	}

	for name, answer := range map[string]string{"empty": "", "not a commit": "fatal\n"} {
		t.Run(name, func(t *testing.T) {
			f := &systest.Fake{Handler: func(sys.Cmd) (sys.Result, error) { return out(answer) }}
			if _, err := newVolume(f).RemoteHead(context.Background(), "u"); err == nil {
				t.Error("want an error for an answer that is not a commit")
			}
		})
	}
}
