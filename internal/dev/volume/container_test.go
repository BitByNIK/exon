package volume_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/dev/volume"
)

func TestContainerLinksFollowTheChosenTools(t *testing.T) {
	c := volume.ContainerFor([]tool.Tool{tool.OMP(), tool.GH(), tool.EZA()}) // eza has no login folder
	want := "file|git/config|.gitconfig\n" +
		"dir|omp|.omp\n" +
		"dir|gh|.config/gh\n"
	if got := string(c.Files["links"]); got != want {
		t.Errorf("links =\n%s\nwant\n%s", got, want)
	}
	for _, name := range []string{"init", "link-workspaces", "links"} {
		if len(c.Files[name]) == 0 {
			t.Errorf("container file %q is empty", name)
		}
	}
}

func TestContainerFingerprintChangesWithTheFiles(t *testing.T) {
	omp := volume.ContainerFor([]tool.Tool{tool.OMP()})
	if again := volume.ContainerFor([]tool.Tool{tool.OMP()}); again.Hash != omp.Hash {
		t.Error("the same tools must give the same fingerprint, or every run would refresh the files")
	}
	if other := volume.ContainerFor([]tool.Tool{tool.OMP(), tool.GH()}); other.Hash == omp.Hash {
		t.Error("another tool list must give another fingerprint, or the new links would never be made")
	}
}

// A box is a toolbox folder and a home folder to run the real init script in.
type box struct {
	t          *testing.T
	toolbox    string
	home       string
	projects   string
	workspaces string
}

// newBox writes the container files for tools into a fresh folder, as `exon dev install` would.
func newBox(t *testing.T, tools ...tool.Tool) *box {
	t.Helper()
	b := &box{t: t, toolbox: t.TempDir(), home: t.TempDir(), projects: filepath.Join(t.TempDir(), "mnt"), workspaces: t.TempDir()}
	for name, content := range volume.ContainerFor(tools).Files {
		b.write(filepath.Join(b.toolbox, name), string(content))
	}
	b.mkdir(filepath.Join(b.toolbox, "state"))
	return b
}

func (b *box) write(path, content string) {
	b.t.Helper()
	b.mkdir(filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

func (b *box) mkdir(path string) {
	b.t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		b.t.Fatal(err)
	}
}

func (b *box) read(rel string) string {
	b.t.Helper()
	got, err := os.ReadFile(filepath.Join(b.home, rel))
	if err != nil {
		b.t.Fatal(err)
	}
	return string(got)
}

// init runs the real init script and returns its output and exit code.
func (b *box) init() (string, int) {
	b.t.Helper()
	cmd := exec.Command("sh", filepath.Join(b.toolbox, "init"))
	cmd.Env = append(os.Environ(),
		"TOOLBOX="+b.toolbox, "HOME="+b.home, "PROJECTS="+b.projects, "WORKSPACES="+b.workspaces)
	out, err := cmd.CombinedOutput()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return string(out), exit.ExitCode()
	}
	if err != nil {
		b.t.Fatal(err)
	}
	return string(out), 0
}

func (b *box) linkTarget(rel string) string {
	b.t.Helper()
	target, err := os.Readlink(filepath.Join(b.home, rel))
	if err != nil {
		b.t.Fatalf("%s is not a link: %v", rel, err)
	}
	return target
}

func TestInitLinksTheLoginFoldersIntoTheVolume(t *testing.T) {
	b := newBox(t, tool.OMP(), tool.GH())
	out, code := b.init()
	if code != 0 {
		t.Fatalf("init exited %d: %s", code, out)
	}

	for rel, state := range map[string]string{".omp": "omp", ".config/gh": "gh", ".gitconfig": "git/config"} {
		if got, want := b.linkTarget(rel), filepath.Join(b.toolbox, "state", state); got != want {
			t.Errorf("~/%s -> %s, want %s", rel, got, want)
		}
	}
	if info, err := os.Stat(filepath.Join(b.toolbox, "state", "omp")); err != nil || !info.IsDir() {
		t.Errorf("state/omp must be a folder that exists, so the link does not dangle: %v", err)
	}
	if info, err := os.Stat(filepath.Join(b.toolbox, "state", "git", "config")); err != nil || !info.Mode().IsRegular() {
		t.Errorf("state/git/config must be a file that exists: %v", err)
	}
	if !strings.Contains(out, "toolbox: linked") || !strings.Contains(out, "~/.omp") {
		t.Errorf("output = %q, want a line naming what was linked", out)
	}
}

func TestInitCanBeRunAgain(t *testing.T) {
	b := newBox(t, tool.OMP())
	if out, code := b.init(); code != 0 {
		t.Fatalf("first run exited %d: %s", code, out)
	}
	first := b.read(".bashrc")
	if out, code := b.init(); code != 0 {
		t.Fatalf("second run exited %d: %s", code, out)
	}
	if second := b.read(".bashrc"); second != first {
		t.Errorf("a second run changed ~/.bashrc:\n%s\n--- became ---\n%s", first, second)
	}
	if n := strings.Count(b.read(".bashrc"), "# >>> exon toolbox"); n != 2 {
		t.Errorf("~/.bashrc has %d exon blocks, want 2 (path and shell)", n)
	}
}

func TestInitReplacesAnEmptyFolderButNeverAFullOne(t *testing.T) {
	b := newBox(t, tool.OMP(), tool.GH())
	b.mkdir(filepath.Join(b.home, ".omp")) // a tool ran before init: empty folder in the way
	b.write(filepath.Join(b.home, ".config", "gh", "hosts.yml"), "a login that exists nowhere else\n")

	out, code := b.init()
	if code == 0 {
		t.Error("init must report that it could not link ~/.config/gh")
	}
	if !strings.Contains(out, filepath.Join(b.home, ".config", "gh")) {
		t.Errorf("output = %q, want it to name the folder in the way", out)
	}
	if got, want := b.linkTarget(".omp"), filepath.Join(b.toolbox, "state", "omp"); got != want {
		t.Errorf("the empty ~/.omp should have become a link to %s, got %s", want, got)
	}
	if got := b.read(filepath.Join(".config", "gh", "hosts.yml")); !strings.Contains(got, "exists nowhere else") {
		t.Error("a folder with content was touched")
	}
	if _, err := os.Readlink(filepath.Join(b.home, ".gitconfig")); err != nil {
		t.Error("one blocked link must not stop the others")
	}
}

func TestInitWritesTheBashrcBlocksAroundWhatIsThere(t *testing.T) {
	b := newBox(t, tool.OMP())
	b.write(filepath.Join(b.home, ".bashrc"), "case $- in *i*) ;; *) return;; esac\nalias mine=1\n")
	if out, code := b.init(); code != 0 {
		t.Fatalf("init exited %d: %s", code, out)
	}

	got := b.read(".bashrc")
	pathBlock := "# >>> exon toolbox path >>>\ncase \":$PATH:\" in *:" + b.toolbox + "/bin:*) ;; *) export PATH=\"" + b.toolbox + "/bin:$PATH\" ;; esac\n"
	if !strings.HasPrefix(got, pathBlock) {
		t.Errorf("PATH must come first, before the image's early return, got:\n%s", got)
	}
	shellBlock := "# >>> exon toolbox shell >>>\n[ -f \"" + b.toolbox + "/shell/bashrc\" ] && . \"" + b.toolbox + "/shell/bashrc\"\n# <<< exon toolbox shell <<<\n"
	if !strings.HasSuffix(got, shellBlock) {
		t.Errorf("the shell settings must come last, so they win, got:\n%s", got)
	}
	if !strings.Contains(got, "alias mine=1") {
		t.Error("what was in ~/.bashrc must be kept")
	}
}

func TestInitLinksTheShellSettingsOnlyWhenThereAreSome(t *testing.T) {
	b := newBox(t, tool.OMP())
	if out, code := b.init(); code != 0 {
		t.Fatalf("init exited %d: %s", code, out)
	}
	if _, err := os.Lstat(filepath.Join(b.home, ".inputrc")); err == nil {
		t.Error("~/.inputrc was made although the volume has no inputrc")
	}

	b.write(filepath.Join(b.toolbox, "shell", "inputrc"), "set completion-ignore-case on\n")
	if out, code := b.init(); code != 0 {
		t.Fatalf("init exited %d: %s", code, out)
	}
	if got, want := b.linkTarget(".inputrc"), filepath.Join(b.toolbox, "shell", "inputrc"); got != want {
		t.Errorf("~/.inputrc -> %s, want %s", got, want)
	}
}

func TestInitExplainsAMissingToolbox(t *testing.T) {
	b := newBox(t, tool.OMP())
	if err := os.RemoveAll(filepath.Join(b.toolbox, "state")); err != nil {
		t.Fatal(err)
	}
	out, code := b.init()
	if code == 0 || !strings.Contains(out, "not mounted") {
		t.Errorf("exit %d, output %q; want a failure that says the toolbox is not mounted", code, out)
	}
}

func TestInitGivesAccessToTheOtherProjectsReadOnly(t *testing.T) {
	b := newBox(t, tool.OMP())
	for _, name := range []string{"api", "web", ".hidden"} {
		b.mkdir(filepath.Join(b.projects, name))
	}
	b.write(filepath.Join(b.projects, "notes.txt"), "a file, not a project")
	// This container's own project is a real folder at /workspaces/<name>.
	b.write(filepath.Join(b.workspaces, "web", "mine.txt"), "this container's own files")

	for run := 1; run <= 2; run++ { // the second run must change nothing
		if out, code := b.init(); code != 0 {
			t.Fatalf("run %d: init exited %d: %s", run, code, out)
		}
	}

	if got, err := os.Readlink(filepath.Join(b.workspaces, "api")); err != nil || got != filepath.Join(b.projects, "api") {
		t.Errorf("/workspaces/api -> %q, %v; want a link to the project", got, err)
	}
	if info, err := os.Lstat(filepath.Join(b.workspaces, "web")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Error("this container's own project folder was replaced by a link")
	}
	if _, err := os.Stat(filepath.Join(b.workspaces, "web", "mine.txt")); err != nil {
		t.Error("files in this container's own project were lost")
	}
	for _, name := range []string{".hidden", "notes.txt"} {
		if _, err := os.Lstat(filepath.Join(b.workspaces, name)); err == nil {
			t.Errorf("%s is not a project and must not be linked", name)
		}
	}
}

func TestInitWithoutOtherProjectsMountedIsFine(t *testing.T) {
	b := newBox(t, tool.OMP()) // b.projects does not exist
	if out, code := b.init(); code != 0 {
		t.Errorf("init exited %d: %s", code, out)
	}
}
