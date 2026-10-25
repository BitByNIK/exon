package toolbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/dev/volume"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/step/toolbox"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
	"github.com/bitbynik/exon/internal/usererr"
)

const threeTools = "[dev]\ntools = [\"omp\", \"gh\", \"oh-my-bash\"]\n"

// docker is a pretend Docker whose state the tests choose.
type docker struct {
	volume     bool   // the toolbox volume exists
	versions   string // what the volume says is installed, as "name|version" lines
	image      bool   // the helper image is already downloaded
	missing    string // starter files the volume lacks, one per line
	installOut string // what the install container prints
	stage      stagedJob
}

// stagedJob is what the install container found in its stage folder.
type stagedJob struct {
	ran   bool
	plan  string
	files []string
}

func (d *docker) fake() *systest.Fake {
	return &systest.Fake{
		Paths: map[string]string{"docker": "/usr/local/bin/docker"},
		Handler: func(c sys.Cmd) (sys.Result, error) {
			out := func(s string) (sys.Result, error) { return sys.Result{Stdout: []byte(s)}, nil }
			switch cmd := c.String(); {
			case systest.Is(c, "docker info"), systest.Is(c, "docker pull"):
				return out("")
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
			case strings.Contains(cmd, "/versions/"):
				return out(d.versions)
			case strings.Contains(cmd, "|| printf"):
				return out(d.missing)
			case systest.Is(c, "docker run"):
				d.look(c)
				return out(d.installOut)
			}
			return out("")
		},
	}
}

// look records what the install container would see in its stage folder.
func (d *docker) look(c sys.Cmd) {
	for i, a := range c.Args {
		stage, ok := strings.CutSuffix(a, ":/stage:ro")
		if !ok || i == 0 || c.Args[i-1] != "-v" {
			continue
		}
		d.stage.ran = true
		plan, _ := os.ReadFile(filepath.Join(stage, "plan"))
		d.stage.plan = string(plan)
		entries, _ := os.ReadDir(stage)
		for _, e := range entries {
			d.stage.files = append(d.stage.files, e.Name())
		}
	}
}

type rel struct {
	tool   tool.Tool
	tag    string
	digest string
}

// github pretends to be GitHub: release data for the tools, and their files.
func github(t *testing.T, releases ...rel) *systest.FakeDownloader {
	t.Helper()
	dl := &systest.FakeDownloader{Files: map[string][]byte{}}
	for _, r := range releases {
		asset := r.tool.Asset(r.tag)
		url := "https://example.test/" + r.tag + "/" + asset
		type a struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest,omitempty"`
		}
		body, err := json.Marshal(map[string]any{"tag_name": r.tag, "assets": []a{{asset, url, 5 << 20, r.digest}}})
		if err != nil {
			t.Fatal(err)
		}
		dl.Files["https://api.github.com/repos/"+r.tool.Repo+"/releases/latest"] = body
		dl.Files[url] = []byte("bytes of " + r.tool.Name)
	}
	return dl
}

func standardGitHub(t *testing.T) *systest.FakeDownloader {
	return github(t,
		rel{tool.OMP(), "v18.12.6", "sha256:aaaa"},
		rel{tool.GH(), "v2.103.2", "sha256:bbbb"},
	)
}

func itemsOf(p step.Plan) map[string]step.PlanItem {
	out := map[string]step.PlanItem{}
	for _, it := range p.Items() {
		out[it.Action+" "+it.Subject] = it
	}
	return out
}

func prepare(t *testing.T, d *docker, dl *systest.FakeDownloader, tomlText string) (step.Plan, step.Env, *steptest.Recorder, error) {
	t.Helper()
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "dev", tomlText, d.fake(), rec)
	env.Net = dl
	p, err := toolbox.New(501, 20).Prepare(context.Background(), steptest.ReadOnly(env))
	return p, env, rec, err
}

func TestPlanForAFreshMachine(t *testing.T) {
	p, _, _, err := prepare(t, &docker{}, standardGitHub(t), threeTools)
	if err != nil {
		t.Fatal(err)
	}
	got := itemsOf(p)
	for _, key := range []string{"install omp", "install gh", "install oh-my-bash", "install container scripts", "download helper image"} {
		if it, ok := got[key]; !ok || it.Kind != step.Add {
			t.Errorf("plan lacks an Add for %q: %v", key, p.Items())
		}
	}
	if d := got["install omp"].Detail; d != "v18.12.6, 5 MB" {
		t.Errorf("omp detail = %q, want the version and size", d)
	}
	if d := got["install oh-my-bash"].Detail; d != "git clone" {
		t.Errorf("oh-my-bash detail = %q, want git clone", d)
	}
	if len(got) != 5 {
		t.Errorf("plan = %v, want exactly those five lines", p.Items())
	}
}

// currentContainer is the fingerprint of the container files for the three tools in threeTools.
func currentContainer() string {
	return volume.ContainerFor([]tool.Tool{tool.OMP(), tool.GH(), tool.OhMyBash()}).Hash
}

func TestPlanLeavesWhatIsInstalled(t *testing.T) {
	d := &docker{volume: true, image: true, versions: "omp|v18.12.6\ngh|v2.103.2\noh-my-bash|4d86087\ncontainer|" + currentContainer() + "\n"}
	p, _, _, err := prepare(t, d, standardGitHub(t), threeTools)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range p.Items() {
		if it.Kind != step.Same {
			t.Errorf("%+v: everything is installed, so nothing should change", it)
		}
	}
	if len(p.Items()) != 3 {
		t.Errorf("plan = %v, want one unchanged line per tool", p.Items())
	}
}

func TestPlanRefreshesContainerFilesThatAreOutOfDate(t *testing.T) {
	tests := map[string]string{
		"older script or link list": "container|0123456789abcdef\n",
		"never installed":           "",
	}
	for name, container := range tests {
		t.Run(name, func(t *testing.T) {
			d := &docker{volume: true, image: true, versions: "omp|v18.12.6\ngh|v2.103.2\noh-my-bash|4d86087\n" + container}
			p, _, _, err := prepare(t, d, standardGitHub(t), threeTools)
			if err != nil {
				t.Fatal(err)
			}
			it, ok := itemsOf(p)["install container scripts"]
			if !ok || it.Kind != step.Add || len(p.Items()) != 4 {
				t.Errorf("plan = %v, want the tools unchanged and one Add for the container scripts", p.Items())
			}
		})
	}
}

func TestContainerFilesFollowTheChosenTools(t *testing.T) {
	// The volume holds the files made for omp only; the config now asks for three tools.
	onlyOMP := volume.ContainerFor([]tool.Tool{tool.OMP()}).Hash
	d := &docker{volume: true, image: true, versions: "omp|v18.12.6\ngh|v2.103.2\noh-my-bash|4d86087\ncontainer|" + onlyOMP + "\n"}
	p, _, _, err := prepare(t, d, standardGitHub(t), threeTools)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := itemsOf(p)["install container scripts"]; !ok {
		t.Errorf("plan = %v, want the links to be rebuilt for the new tool list", p.Items())
	}
}

func TestPlanInstallsOnlyWhatIsMissing(t *testing.T) {
	d := &docker{volume: true, image: true, versions: "omp|v18.12.6\n"}
	p, _, _, err := prepare(t, d, standardGitHub(t), threeTools)
	if err != nil {
		t.Fatal(err)
	}
	got := itemsOf(p)
	if got["installed omp"].Kind != step.Same || got["install gh"].Kind != step.Add || got["install oh-my-bash"].Kind != step.Add {
		t.Errorf("plan = %v", p.Items())
	}
	if _, asked := got["download helper image"]; asked {
		t.Error("the helper image is already there and must not be planned")
	}
}

func TestPlanWarnsAboutAnUnverifiableDownload(t *testing.T) {
	dl := github(t, rel{tool.OMP(), "v18.12.6", ""}) // GitHub gave no checksum
	p, _, _, err := prepare(t, &docker{image: true}, dl, "[dev]\ntools = [\"omp\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if w, ok := itemsOf(p)["unverified omp"]; !ok || w.Kind != step.Warn {
		t.Errorf("plan = %v, want a warning that omp cannot be verified", p.Items())
	}
}

func TestPlanNeedsDocker(t *testing.T) {
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "dev", threeTools, &systest.Fake{}, rec) // no docker on PATH
	_, err := toolbox.New(501, 20).Prepare(context.Background(), steptest.ReadOnly(env))
	if ue, ok := usererr.As(err); !ok || !strings.Contains(ue.Msg, "docker") {
		t.Errorf("err = %v, want a user error about docker", err)
	}
}

func TestPlanFailsWholeWhenAReleaseCannotBeFound(t *testing.T) {
	dl := github(t, rel{tool.OMP(), "v18.12.6", "sha256:aa"}) // gh is unknown to this GitHub
	p, _, _, err := prepare(t, &docker{}, dl, threeTools)
	if err == nil || p != nil {
		t.Errorf("got plan %v and error %v, want an error and no plan: never apply part of a plan", p, err)
	}
}

func TestPlanRejectsAnUnknownTool(t *testing.T) {
	_, _, _, err := prepare(t, &docker{}, standardGitHub(t), "[dev]\ntools = [\"vim\"]\n")
	if _, ok := usererr.As(err); !ok {
		t.Errorf("err = %v, want a user error naming the known tools", err)
	}
}

func TestPlanStarterFiles(t *testing.T) {
	rec := &steptest.Recorder{}
	d := &docker{volume: true, image: true, missing: "shell/bashrc\n"}
	env := steptest.Env(t, "dev", "[dev]\ntools = [\"omp\"]\n", d.fake(), rec)
	env.Net = standardGitHub(t)
	d.versions = "omp|v18.12.6\n"
	for path, body := range map[string]string{
		"toolbox/shell/bashrc":                 "x",
		"toolbox/state/omp/agent/config.yml":   "x",
		"toolbox/bin/sneaky":                   "x",
		"toolbox/shell/.DS_Store":              "x",
		"not-the-toolbox-folder/shell/ignored": "x",
	} {
		full := filepath.Join(env.Repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	p, err := toolbox.New(501, 20).Prepare(context.Background(), steptest.ReadOnly(env))
	if err != nil {
		t.Fatal(err)
	}
	got := itemsOf(p)
	if it, ok := got["copy starter files"]; !ok || it.Kind != step.Add || it.Detail != "shell/bashrc" {
		t.Errorf("plan = %v, want one copy of the one missing file", p.Items())
	}
	if it, ok := got["ignored toolbox/bin/sneaky"]; !ok || it.Kind != step.Warn {
		t.Errorf("plan = %v, want a warning that bin/ files are ignored", p.Items())
	}
}

func TestApplyInstallsAndReportsEachTool(t *testing.T) {
	d := &docker{installOut: "ok|omp\nfail|gh|tar: broken\nok|oh-my-bash\nok|container\n"}
	p, env, rec, err := prepare(t, d, standardGitHub(t), threeTools)
	if err != nil {
		t.Fatal(err)
	}
	dl := env.Net.(*systest.FakeDownloader)
	fake := env.Run.(*systest.Fake)

	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}

	if got := rec.Outcomes(); got["omp"] != "done" || got["gh"] != "failed" || got["oh-my-bash"] != "done" || got["container scripts"] != "done" {
		t.Errorf("outcomes = %v, want omp, oh-my-bash and the container scripts done, gh failed", got)
	}
	var fetched []string
	for _, f := range dl.Fetches() {
		fetched = append(fetched, f.URL+" "+f.SHA256)
	}
	want := []string{
		"https://example.test/v18.12.6/omp-linux-arm64 aaaa",
		"https://example.test/v2.103.2/gh_2.103.2_linux_arm64.tar.gz bbbb",
	}
	if strings.Join(fetched, "|") != strings.Join(want, "|") {
		t.Errorf("downloads = %v, want %v (each with its checksum; the clone downloads nothing)", fetched, want)
	}
	if !d.stage.ran || !strings.Contains(d.stage.plan, "program|omp|v18.12.6|omp.download|") ||
		!strings.Contains(d.stage.plan, "clone|oh-my-bash||https://github.com/ohmybash/oh-my-bash|") {
		t.Errorf("the container saw plan %q", d.stage.plan)
	}
	if !slices.Contains(d.stage.files, "omp.download") || !slices.Contains(d.stage.files, "install.sh") || !slices.Contains(d.stage.files, "container") {
		t.Errorf("the container saw files %v, want the download, the script and the container folder", d.stage.files)
	}
	if !strings.Contains(d.stage.plan, "container|container|"+currentContainer()+"|") {
		t.Errorf("the plan %q lacks the container files line", d.stage.plan)
	}
	if n := countRuns(fake.Execs()); n != 1 {
		t.Errorf("%d containers started to install, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(env.CacheDir, "stage")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the stage folder was left behind: %v", err)
	}
}

func TestApplyKeepsGoingWhenOneDownloadFails(t *testing.T) {
	d := &docker{installOut: "ok|omp\n"}
	dl := standardGitHub(t)
	p, env, rec, err := prepare(t, d, dl, "[dev]\ntools = [\"omp\", \"gh\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	delete(dl.Files, "https://example.test/v2.103.2/gh_2.103.2_linux_arm64.tar.gz") // gh's file is gone

	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	if got := rec.Outcomes(); got["omp"] != "done" || got["gh"] != "failed" {
		t.Errorf("outcomes = %v, want omp done and gh failed", got)
	}
	if strings.Contains(d.stage.plan, "gh") {
		t.Errorf("a tool that could not be downloaded must not be in the plan: %q", d.stage.plan)
	}
}

func TestApplyDownloadsTheHelperImageFirst(t *testing.T) {
	d := &docker{installOut: "ok|omp\n"}
	p, env, _, err := prepare(t, d, standardGitHub(t), "[dev]\ntools = [\"omp\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	execs := env.Run.(*systest.Fake).Execs()
	if len(execs) != 2 || !strings.HasPrefix(execs[0], "docker pull") || !strings.HasPrefix(execs[1], "docker run") {
		t.Errorf("commands = %v, want a pull and then the install", execs)
	}
}

func TestApplyWhenTheContainerFailsTheStepFails(t *testing.T) {
	failing := &systest.Fake{
		Paths: map[string]string{"docker": "/usr/local/bin/docker"},
		Handler: func(c sys.Cmd) (sys.Result, error) {
			switch {
			case systest.Is(c, "docker run") && strings.Contains(c.String(), "-e OWNER"):
				return systest.Fail(c, "boom")
			case systest.Is(c, "docker image inspect"):
				return sys.Result{}, nil
			}
			return sys.Result{}, nil
		},
	}
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "dev", "[dev]\ntools = [\"omp\"]\n", failing, rec)
	env.Net = standardGitHub(t)
	p, err := toolbox.New(501, 20).Prepare(context.Background(), steptest.ReadOnly(env))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), env, nil); err == nil {
		t.Error("want the step to fail when the install container fails")
	}
	if rec.Outcomes()["omp"] == "done" {
		t.Error("omp must not be reported as installed")
	}
}

func countRuns(execs []string) int {
	n := 0
	for _, e := range execs {
		if strings.HasPrefix(e, "docker run") {
			n++
		}
	}
	return n
}
