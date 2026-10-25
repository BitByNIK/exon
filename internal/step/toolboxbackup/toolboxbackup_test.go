package toolboxbackup_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitbynik/exon/internal/events"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/step/toolboxbackup"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
	"github.com/bitbynik/exon/internal/usererr"
)

var when = time.Date(2026, 10, 25, 10, 55, 3, 0, time.UTC)

// docker is a pretend Docker whose state the tests choose.
type docker struct {
	volume     bool     // the toolbox volume exists
	image      bool     // the helper image is downloaded
	hasData    bool     // state/ holds real data
	containers []string // running containers on the volume
	failBackup bool
}

func (d *docker) fake() *systest.Fake {
	return &systest.Fake{
		Paths: map[string]string{"docker": "/usr/local/bin/docker"},
		Handler: func(c sys.Cmd) (sys.Result, error) {
			out := func(s string) (sys.Result, error) { return sys.Result{Stdout: []byte(s)}, nil }
			cmd := c.String()
			switch {
			case systest.Is(c, "docker volume ls"):
				if d.volume {
					return out("toolbox\n")
				}
				return out("")
			case systest.Is(c, "docker image inspect"):
				if d.image {
					return out("sha256:abc\n")
				}
				return systest.Fail(c, "No such image")
			case systest.Is(c, "docker ps"):
				return out(strings.Join(d.containers, "\n") + "\n")
			case strings.Contains(cmd, "-size +0"):
				if d.hasData {
					return out("/opt/toolbox/state/gh/hosts.yml\n")
				}
				return out("")
			case strings.Contains(cmd, "umask 077"): // the backup container: make the file it would make
				if d.failBackup {
					return systest.Fail(c, "no space left on device")
				}
				writeBackupFile(c)
			}
			return out("")
		},
	}
}

// writeBackupFile creates the file the backup container would have written, so a size can be read.
func writeBackupFile(c sys.Cmd) {
	var dir string
	for i, a := range c.Args {
		if d, ok := strings.CutSuffix(a, ":/backup"); ok && c.Args[i-1] == "-v" {
			dir = d
		}
	}
	_ = os.WriteFile(filepath.Join(dir, c.Args[len(c.Args)-1]), []byte(strings.Repeat("x", 3000)), 0o600)
}

func actions(p step.Plan) map[string]step.PlanItem {
	out := map[string]step.PlanItem{}
	for _, it := range p.Items() {
		out[it.Action+" "+it.Subject] = it
	}
	return out
}

func kinds(p step.Plan, action string) []step.Kind {
	var ks []step.Kind
	for _, it := range p.Items() {
		if it.Action == action {
			ks = append(ks, it.Kind)
		}
	}
	return ks
}

func prepareBackup(t *testing.T, d *docker) (step.Plan, step.Env, *steptest.Recorder, string, error) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Backups", "exon")
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "backup", "", d.fake(), rec)
	p, err := toolboxbackup.NewBackup(501, 20, dir, when).Prepare(context.Background(), steptest.ReadOnly(env))
	return p, env, rec, dir, err
}

func TestBackupPlansAPrivateFile(t *testing.T) {
	p, _, _, dir, err := prepareBackup(t, &docker{volume: true, image: true})
	if err != nil {
		t.Fatal(err)
	}
	save, ok := actions(p)["save "+filepath.Join(dir, "toolbox-2026-10-25_105503.tar.gz")]
	if !ok || save.Kind != step.Add {
		t.Errorf("plan = %v, want the dated file to be saved", p.Items())
	}
	if ks := kinds(p, "private"); len(ks) != 1 || ks[0] != step.Note {
		t.Errorf("plan = %v, want a note that the file holds login tokens", p.Items())
	}
	if len(kinds(p, "running")) != 0 || len(kinds(p, "download")) != 0 {
		t.Errorf("plan = %v, want no warning and no image download here", p.Items())
	}
}

func TestBackupWarnsAboutRunningContainersAndTheImage(t *testing.T) {
	p, _, _, _, err := prepareBackup(t, &docker{volume: true, containers: []string{"web", "api"}})
	if err != nil {
		t.Fatal(err)
	}
	if w := actions(p)["running web, api"]; w.Kind != step.Warn {
		t.Errorf("plan = %v, want a warning naming the running containers", p.Items())
	}
	if ks := kinds(p, "download"); len(ks) != 1 {
		t.Errorf("plan = %v, want the helper image download planned", p.Items())
	}
}

func TestBackupWithoutAVolumeSaysWhatToDo(t *testing.T) {
	_, _, _, _, err := prepareBackup(t, &docker{})
	if ue, ok := usererr.As(err); !ok || !strings.Contains(ue.Hint, "exon dev install") {
		t.Errorf("err = %v, want a user error pointing at install", err)
	}
}

func TestBackupApply(t *testing.T) {
	p, env, rec, dir, err := prepareBackup(t, &docker{volume: true, image: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("backup folder: %v, %v; want it created and private (0700)", info, err)
	}
	if got := rec.Outcomes()[filepath.Join(dir, "toolbox-2026-10-25_105503.tar.gz")]; got != "done" {
		t.Errorf("outcomes = %v, want the backup done", rec.Outcomes())
	}
	var detail string
	for _, e := range rec.Events {
		if done, ok := e.(events.ItemDone); ok {
			detail = done.Detail
		}
	}
	if detail != "3 KB" { // the pretend container wrote 3000 bytes
		t.Errorf("finished with %q, want the size of the file", detail)
	}
}

func TestBackupFailureIsReportedNotHidden(t *testing.T) {
	p, env, rec, dir, err := prepareBackup(t, &docker{volume: true, image: true, failBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	if got := rec.Outcomes()[filepath.Join(dir, "toolbox-2026-10-25_105503.tar.gz")]; got != "failed" {
		t.Errorf("outcomes = %v, want the failed backup reported", rec.Outcomes())
	}
}

// makeBackup writes a .tar.gz holding the given files and returns its path.
func makeBackup(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "toolbox-test.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func prepareRestore(t *testing.T, d *docker, file string, force bool) (step.Plan, step.Env, *steptest.Recorder, error) {
	t.Helper()
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "restore", "", d.fake(), rec)
	p, err := toolboxbackup.NewRestore(501, 20, file, force).Prepare(context.Background(), steptest.ReadOnly(env))
	return p, env, rec, err
}

func TestRestoreIntoAnEmptyToolbox(t *testing.T) {
	file := makeBackup(t, map[string]string{"state/gh/hosts.yml": "token", "shell/bashrc": "alias ll=ls"})
	p, env, rec, err := prepareRestore(t, &docker{volume: true, image: true}, file, false)
	if err != nil {
		t.Fatal(err)
	}
	got := actions(p)
	if it := got["restore "+file]; it.Kind != step.Add || it.Detail != "2 files, 16 bytes" {
		t.Errorf("plan = %v, want the restore described by its files", p.Items())
	}
	if it := got["afterwards exon dev install"]; it.Kind != step.Note {
		t.Errorf("plan = %v, want a note that the tools come back with install", p.Items())
	}

	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	execs := env.Run.(*systest.Fake).Execs()
	if len(execs) != 1 || !strings.Contains(execs[0], "-v "+filepath.Dir(file)+":/backup:ro") || !strings.HasSuffix(execs[0], " sh toolbox-test.tar.gz") {
		t.Errorf("commands = %v, want one container with the backup's folder mounted read-only", execs)
	}
	if rec.Outcomes()[file] != "done" {
		t.Errorf("outcomes = %v, want the restore done", rec.Outcomes())
	}
}

func TestRestoreWontWriteOverRealDataWithoutForce(t *testing.T) {
	file := makeBackup(t, map[string]string{"state/gh/hosts.yml": "token"})
	d := &docker{volume: true, image: true, hasData: true}

	_, _, _, err := prepareRestore(t, d, file, false)
	if ue, ok := usererr.As(err); !ok || !strings.Contains(ue.Hint, "--force") {
		t.Errorf("err = %v, want a user error that names --force", err)
	}

	p, _, _, err := prepareRestore(t, d, file, true)
	if err != nil {
		t.Fatal(err)
	}
	if ks := kinds(p, "restore"); len(ks) != 1 || ks[0] != step.Change {
		t.Errorf("plan = %v, want a Change, since it replaces what is there", p.Items())
	}
}

func TestRestoreRefusesAnUnsafeBackup(t *testing.T) {
	file := makeBackup(t, map[string]string{
		"state/ok": "x", "bin/omp": "x", "versions/omp": "x", "init": "x", "links": "x",
	})
	_, _, _, err := prepareRestore(t, &docker{volume: true}, file, true)
	ue, ok := usererr.As(err)
	if !ok || !strings.Contains(ue.Msg, "not safe") || !strings.Contains(ue.Hint, "and 1 more") {
		t.Errorf("err = %v, want a user error that shows three problems and counts the rest", err)
	}
}

func TestRestoreOfSomethingThatIsNotABackup(t *testing.T) {
	notes := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notes, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := prepareRestore(t, &docker{volume: true}, notes, true)
	if ue, ok := usererr.As(err); !ok || !strings.Contains(ue.Msg, "not a toolbox backup") {
		t.Errorf("err = %v", err)
	}
}
