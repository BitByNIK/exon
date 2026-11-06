package tool_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/dev/tool"
)

// runLauncher runs the real launcher, with the toolbox moved to a temp folder and a
// stand-in for the claude program that prints what it was started with.
func runLauncher(t *testing.T, withMCP bool, args ...string) string {
	t.Helper()
	root := t.TempDir()
	script := strings.Replace(tool.Claude().Launcher, "T=/opt/toolbox", "T="+root, 1)
	if script == tool.Claude().Launcher {
		t.Fatal("the launcher no longer starts with T=/opt/toolbox")
	}
	for path, content := range map[string]string{
		"bin/claude":        script,
		"lib/claude/claude": "#!/bin/sh\necho \"dir=$CLAUDE_CONFIG_DIR updates_off=$DISABLE_UPDATES args=$*\"\n",
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if withMCP {
		if err := os.MkdirAll(filepath.Join(root, "state/claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "state/claude/mcp.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("sh", append([]string{filepath.Join(root, "bin/claude")}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("launcher failed: %v\n%s", err, out)
	}
	return strings.ReplaceAll(strings.TrimSpace(string(out)), root, "<toolbox>")
}

func TestLauncherKeepsClaudeInTheVolumeAndBlocksItsUpdates(t *testing.T) {
	got := runLauncher(t, false, "--version")
	if want := "dir=<toolbox>/state/claude updates_off=1 args=--version"; got != want {
		t.Errorf("claude started with %q, want %q", got, want)
	}
}

// The flag takes several files, so it must be written with "=": otherwise "mcp list" or a
// prompt after it would be read as more file names.
func TestLauncherPassesTheMCPFileOnlyWhenThereIsOne(t *testing.T) {
	if got := runLauncher(t, false, "mcp", "list"); strings.Contains(got, "--mcp-config") {
		t.Errorf("claude started with %q, want no --mcp-config without a file", got)
	}
	got := runLauncher(t, true, "mcp", "list")
	if want := "args=--mcp-config=<toolbox>/state/claude/mcp.json mcp list"; !strings.HasSuffix(got, want) {
		t.Errorf("claude started with %q, want it to end with %q", got, want)
	}
}
