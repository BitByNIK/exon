package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/bitbynik/exon/internal/sys"
)

// brewEnv keeps brew from updating itself on every command (exon runs
// `brew update` once) and from printing tips.
var brewEnv = []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1"}

func brew(args ...string) sys.Cmd {
	return sys.Cmd{Name: "brew", Args: args, Env: brewEnv}
}

// formulaInfo is the part of `brew info --json=v2` exon uses for a formula.
type formulaInfo struct {
	Name       string            `json:"name"`
	FullName   string            `json:"full_name"`
	Tap        string            `json:"tap"`
	Deprecated bool              `json:"deprecated"`
	Disabled   bool              `json:"disabled"`
	Installed  []json.RawMessage `json:"installed"`
	Aliases    []string          `json:"aliases"`
	Oldnames   []string          `json:"oldnames"`
}

func (f formulaInfo) matches(name string) bool {
	return name == f.Name || name == f.FullName || slices.Contains(f.Aliases, name) || slices.Contains(f.Oldnames, name)
}

// caskInfo is the part of `brew info --json=v2` exon uses for a cask.
type caskInfo struct {
	Token      string                       `json:"token"`
	FullToken  string                       `json:"full_token"`
	Tap        string                       `json:"tap"`
	Deprecated bool                         `json:"deprecated"`
	Disabled   bool                         `json:"disabled"`
	Installed  *string                      `json:"installed"`
	OldTokens  []string                     `json:"old_tokens"`
	Artifacts  []map[string]json.RawMessage `json:"artifacts"`
}

func (c caskInfo) matches(name string) bool {
	return name == c.Token || name == c.FullToken || slices.Contains(c.OldTokens, name)
}

// apps returns the .app bundles the cask puts in /Applications.
func (c caskInfo) apps() []string {
	var out []string
	for _, a := range c.Artifacts {
		raw, ok := a["app"]
		if !ok {
			continue
		}
		var parts []any
		if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
			continue
		}
		name, _ := parts[0].(string)
		// ["Source.app", {"target": "Other Name.app"}] installs under the target name.
		if len(parts) > 1 {
			if opts, ok := parts[1].(map[string]any); ok {
				if t, ok := opts["target"].(string); ok {
					name = t
				}
			}
		}
		if name != "" {
			out = append(out, path.Base(name))
		}
	}
	return out
}

// needsPassword reports whether the cask runs a macOS installer package,
// which asks for an administrator password.
func (c caskInfo) needsPassword() bool {
	for _, a := range c.Artifacts {
		if _, ok := a["pkg"]; ok {
			return true
		}
		if _, ok := a["installer"]; ok {
			return true
		}
	}
	return false
}

type infoOutput struct {
	Formulae []formulaInfo `json:"formulae"`
	Casks    []caskInfo    `json:"casks"`
}

// infoFn looks up packages with `brew info`. kind is "--formula" or "--cask".
type infoFn func(ctx context.Context, kind string, names []string) (infoOutput, error)

// lookup returns info for every name it can resolve, plus the names brew does
// not know. One batched call in the common case; per-name calls only when the
// batch fails, to find out which names are unknown.
func lookup(ctx context.Context, info infoFn, kind string, names []string) (infoOutput, []string, error) {
	if len(names) == 0 {
		return infoOutput{}, nil, nil
	}
	out, err := info(ctx, kind, names)
	if err == nil {
		return out, nil, nil
	}
	if _, ok := errors.AsType[*sys.ExitError](err); !ok {
		return infoOutput{}, nil, err
	}
	var all infoOutput
	var unknown []string
	for _, n := range names {
		one, err := info(ctx, kind, []string{n})
		if err != nil {
			if _, ok := errors.AsType[*sys.ExitError](err); ok {
				unknown = append(unknown, n)
				continue
			}
			return infoOutput{}, nil, err
		}
		all.Formulae = append(all.Formulae, one.Formulae...)
		all.Casks = append(all.Casks, one.Casks...)
	}
	return all, unknown, nil
}

func brewInfo(run sys.Runner) infoFn {
	return func(ctx context.Context, kind string, names []string) (infoOutput, error) {
		args := append([]string{"info", "--json=v2", kind}, names...)
		res, err := run.Query(ctx, brew(args...))
		if err != nil {
			return infoOutput{}, err
		}
		var out infoOutput
		if err := json.Unmarshal(res.Stdout, &out); err != nil {
			return infoOutput{}, fmt.Errorf("parse brew info output: %w", err)
		}
		return out, nil
	}
}

// tapOf returns "user/repo" for a fully qualified "user/repo/name", else "".
func tapOf(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) != 3 {
		return ""
	}
	return strings.ToLower(parts[0] + "/" + parts[1])
}

// coreTaps are always available and never need tapping.
var coreTaps = map[string]bool{"homebrew/core": true, "homebrew/cask": true}

// isRetryable reports failures worth retrying: network problems and another
// brew process holding the lock.
func isRetryable(err error, output []byte) bool {
	if sys.IsNetworkError(err, output) {
		return true
	}
	s := string(output)
	return strings.Contains(s, "has already locked") || strings.Contains(s, "Another active Homebrew")
}
