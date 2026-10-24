// Package settings reads the [dev] section of exon.toml.
package settings

import (
	"slices"
	"strings"

	"github.com/bitbynik/exon/internal/config"
	"github.com/bitbynik/exon/internal/dev/tool"
	"github.com/bitbynik/exon/internal/usererr"
)

// SectionName is the exon.toml section these settings come from.
const SectionName = "dev"

// Settings are the user's choices for the dev containers.
type Settings struct {
	// Tools to keep in the toolbox volume, in install order.
	Tools []tool.Tool
}

type raw struct {
	Tools []string `toml:"tools"`
}

// Load reads [dev]. Without it, or without a tools list, every known tool is used.
func Load(sec config.Section) (Settings, error) {
	var r raw
	if err := sec.Decode(&r); err != nil {
		return Settings{}, err
	}
	if len(r.Tools) == 0 {
		return Settings{Tools: tool.All()}, nil
	}
	for _, name := range r.Tools {
		if _, err := tool.Find(name); err != nil {
			return Settings{}, usererr.New(
				"[dev] tools: unknown tool \""+name+"\"",
				"Known tools: "+strings.Join(tool.Names(), ", "), err)
		}
	}
	var chosen []tool.Tool
	for _, t := range tool.All() { // install order, whatever order the user wrote
		if slices.Contains(r.Tools, t.Name) {
			chosen = append(chosen, t)
		}
	}
	return Settings{Tools: chosen}, nil
}
