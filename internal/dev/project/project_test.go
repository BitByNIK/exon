package project_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/project"
)

// parse reads a devcontainer.json, which may have // comment lines.
func parse(t *testing.T, text string) map[string]any {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "//") {
			lines = append(lines, l)
		}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &out); err != nil {
		t.Fatalf("not valid JSON once comments are removed: %v\n%s", err, text)
	}
	return out
}

func files(t *testing.T, o project.Options) map[string]project.File {
	t.Helper()
	fs, err := project.Files(o)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]project.File{}
	for _, f := range fs {
		out[f.Path] = f
	}
	return out
}

func TestFilesForAProject(t *testing.T) {
	got := files(t, project.Options{
		Name: "api", Image: "mcr.microsoft.com/devcontainers/base:trixie", User: "vscode",
		ProjectsDir: "${localEnv:HOME}/Projects",
	})
	if len(got) != 2 {
		t.Fatalf("files = %v, want devcontainer.json and post-create.sh", got)
	}

	cfg := parse(t, got[".devcontainer/personal/devcontainer.json"].Content)
	want := map[string]any{
		"name":                "api (personal)",
		"image":               "mcr.microsoft.com/devcontainers/base:trixie",
		"remoteUser":          "vscode",
		"updateRemoteUserUID": true,
		"postCreateCommand":   "./.devcontainer/personal/post-create.sh",
	}
	for k, v := range want {
		if cfg[k] != v {
			t.Errorf("%s = %v, want %v", k, cfg[k], v)
		}
	}
	if m := cfg["mounts"].([]any); len(m) != 1 || m[0] != "source=toolbox,target=/opt/toolbox,type=volume" {
		t.Errorf("mounts = %v, want the toolbox volume", m)
	}
	if r := cfg["runArgs"].([]any); len(r) != 1 ||
		r[0] != "--mount=type=bind,source=${localEnv:HOME}/Projects,target=/mnt/projects,readonly" {
		t.Errorf("runArgs = %v, want the projects folder, read-only", r)
	}
	if env := cfg["remoteEnv"].(map[string]any); env["PATH"] != "/opt/toolbox/bin:${containerEnv:PATH}" {
		t.Errorf("remoteEnv = %v, want the toolbox first on PATH", env)
	}
}

func TestThePostCreateScriptRunsInitAndCanBeRun(t *testing.T) {
	f := files(t, project.Options{Name: "api", Image: "i", User: "u", ProjectsDir: "/p"})[".devcontainer/personal/post-create.sh"]
	if f.Mode&0o111 == 0 {
		t.Errorf("mode = %v, want it executable: the hook is the script's own path", f.Mode)
	}
	if !strings.HasPrefix(f.Content, "#!/bin/sh\n") {
		t.Errorf("the script needs a shebang, got %q", f.Content[:20])
	}
	if !strings.Contains(f.Content, "\n/opt/toolbox/init\n") {
		t.Errorf("the script must run the toolbox init on a line of its own:\n%s", f.Content)
	}
}

// A folder can be named anything; whatever it is, the JSON must stay valid.
func TestNamesThatNeedEscapingDoNotBreakTheFile(t *testing.T) {
	for _, name := range []string{`has "quotes"`, `back\slash`, "tab\there", "unicode ✓"} {
		t.Run(name, func(t *testing.T) {
			f := files(t, project.Options{Name: name, Image: `img"x`, User: `u\v`, ProjectsDir: `/Users/a "b"/Projects`})
			cfg := parse(t, f[".devcontainer/personal/devcontainer.json"].Content)
			if cfg["name"] != name+" (personal)" || cfg["image"] != `img"x` || cfg["remoteUser"] != `u\v` {
				t.Errorf("values did not survive: %v", cfg)
			}
		})
	}
}

func TestProjectsDir(t *testing.T) {
	tests := []struct{ project, home, want string }{
		{"/Users/ng/Projects/api", "/Users/ng", "${localEnv:HOME}/Projects"},
		{"/Users/ng/Work/clients/api", "/Users/ng", "${localEnv:HOME}/Work/clients"},
		{"/Users/ng/api", "/Users/ng", "${localEnv:HOME}"},
		{"/Volumes/Disk/code/api", "/Users/ng", "/Volumes/Disk/code"},
		{"/Users/nigel/Projects/api", "/Users/ng", "/Users/nigel/Projects"}, // not under /Users/ng, only a similar name
		{"/Users/ng/..cache/api", "/Users/ng", "${localEnv:HOME}/..cache"},  // a folder whose name merely starts with ..
	}
	for _, tt := range tests {
		if got := project.ProjectsDir(tt.project, tt.home); got != tt.want {
			t.Errorf("ProjectsDir(%s, %s) = %s, want %s", tt.project, tt.home, got, tt.want)
		}
	}
}
