package settings_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/config"
	"github.com/bitbynik/exon/internal/dev/settings"
	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/usererr"
)

func load(t *testing.T, toml string) (settings.Settings, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := config.Load(path, []string{settings.SectionName})
	if err != nil {
		t.Fatal(err)
	}
	return settings.Load(f.Section(settings.SectionName))
}

func names(s settings.Settings) []string {
	out := make([]string, len(s.Tools))
	for i, tl := range s.Tools {
		out[i] = tl.Name
	}
	return out
}

func TestEveryToolByDefault(t *testing.T) {
	for name, toml := range map[string]string{"no section": "", "empty section": "[dev]\n"} {
		t.Run(name, func(t *testing.T) {
			s, err := load(t, toml)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(s); !slices.Equal(got, tool.Names()) {
				t.Errorf("tools = %v, want all: %v", got, tool.Names())
			}
		})
	}
}

func TestChosenToolsKeepInstallOrder(t *testing.T) {
	s, err := load(t, "[dev]\ntools = [\"gh\", \"omp\", \"gh\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(s), []string{"omp", "gh"}; !slices.Equal(got, want) {
		t.Errorf("tools = %v, want %v (install order, no repeats)", got, want)
	}
}

func TestUnknownToolListsTheKnownOnes(t *testing.T) {
	_, err := load(t, "[dev]\ntools = [\"omp\", \"vim\"]\n")
	ue, ok := usererr.As(err)
	if !ok {
		t.Fatalf("err = %v, want a user error", err)
	}
	if want := "Known tools: " + strings.Join(tool.Names(), ", "); ue.Hint != want {
		t.Errorf("hint = %q, want %q", ue.Hint, want)
	}
}

func TestMisspeltKeyIsRejected(t *testing.T) {
	if _, err := load(t, "[dev]\ntoolz = [\"omp\"]\n"); err == nil {
		t.Error("want an error for an unknown key")
	}
}
