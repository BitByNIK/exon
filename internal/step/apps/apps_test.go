package apps

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
)

// fakeBrew answers brew commands from a small package database.
type fakeBrew struct {
	tapped   []string
	formulae map[string]formulaInfo
	casks    map[string]caskInfo
	// install answers `brew install`/`brew tap`; nil means success.
	install func(c sys.Cmd, attempt int) (sys.Result, error)
	tries   map[string]int
}

func (b *fakeBrew) handle(c sys.Cmd) (sys.Result, error) {
	args := c.Args
	switch {
	case slices.Equal(args, []string{"tap"}):
		return sys.Result{Stdout: []byte(strings.Join(b.tapped, "\n") + "\n")}, nil
	case len(args) > 2 && args[0] == "info":
		var out infoOutput
		for _, n := range args[3:] {
			if f, ok := b.formulae[n]; ok && args[2] == "--formula" {
				out.Formulae = append(out.Formulae, f)
			} else if c2, ok := b.casks[n]; ok && args[2] == "--cask" {
				out.Casks = append(out.Casks, c2)
			} else {
				return systest.Fail(c, "Error: No available formula with the name \""+n+"\".")
			}
		}
		data, _ := json.Marshal(out)
		return sys.Result{Stdout: data}, nil
	case args[0] == "install" || args[0] == "tap" || args[0] == "update":
		if b.tries == nil {
			b.tries = map[string]int{}
		}
		key := c.String()
		b.tries[key]++
		if b.install != nil {
			return b.install(c, b.tries[key])
		}
		return sys.Result{}, nil
	}
	return systest.Fail(c, "unexpected command")
}

func installed() []json.RawMessage { return []json.RawMessage{json.RawMessage(`{}`)} }
func ver() *string                 { v := "1.0"; return &v }

func app(name string) map[string]json.RawMessage {
	return map[string]json.RawMessage{"app": json.RawMessage(`["` + name + `"]`)}
}

func pkg() map[string]json.RawMessage {
	return map[string]json.RawMessage{"pkg": json.RawMessage(`["x.pkg"]`)}
}

func newBrew() *fakeBrew {
	return &fakeBrew{
		tapped: []string{"homebrew/cask"},
		formulae: map[string]formulaInfo{
			"git":  {Name: "git", FullName: "git"},
			"tmux": {Name: "tmux", FullName: "tmux", Installed: installed()},
			"old":  {Name: "old", FullName: "old", Deprecated: true},
			"dead": {Name: "dead", FullName: "dead", Disabled: true},
		},
		casks: map[string]caskInfo{
			"zed":           {Token: "zed", FullToken: "zed", Artifacts: []map[string]json.RawMessage{app("Zed.app")}},
			"iina":          {Token: "iina", FullToken: "iina", Installed: ver()},
			"google-chrome": {Token: "google-chrome", FullToken: "google-chrome", Artifacts: []map[string]json.RawMessage{app("Google Chrome.app")}},
			"tailscale-app": {Token: "tailscale-app", FullToken: "tailscale-app", Artifacts: []map[string]json.RawMessage{pkg()}},
			"tailscale":     {Token: "tailscale-app", FullToken: "tailscale-app", OldTokens: []string{"tailscale"}, Artifacts: []map[string]json.RawMessage{pkg()}},
		},
	}
}

func testStep(t *testing.T) *Step {
	appDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(appDir, "Google Chrome.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Step{AppDir: appDir, Attempts: 3, Backoff: time.Millisecond}
}

func prepare(t *testing.T, s *Step, b *fakeBrew, tomlText string) (step.Plan, step.Env, *steptest.Recorder, *systest.Fake, error) {
	t.Helper()
	fake := &systest.Fake{Handler: b.handle}
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "apps", tomlText, fake, rec)
	p, err := s.Prepare(context.Background(), steptest.ReadOnly(env))
	return p, env, rec, fake, err
}

func items(p step.Plan) map[string]step.PlanItem {
	out := map[string]step.PlanItem{}
	for _, it := range p.Items() {
		key := it.Subject
		if it.Kind == step.Warn {
			key = it.Action + ":" + it.Subject
		}
		out[key] = it
	}
	return out
}

func TestPlan(t *testing.T) {
	p, _, _, fake, err := prepare(t, testStep(t), newBrew(), `
[apps]
taps = ["someone/tools"]
formulae = ["git", "tmux", "old"]
casks = ["zed", "iina", "google-chrome", "tailscale", "someone/tools/thing"]
`)
	if err != nil {
		t.Fatal(err)
	}
	got := items(p)

	check := func(key string, kind step.Kind, detail string) {
		t.Helper()
		it, ok := got[key]
		if !ok {
			t.Errorf("no plan item %q in %v", key, got)
			return
		}
		if it.Kind != kind || !strings.Contains(it.Detail, detail) {
			t.Errorf("%s: kind %v detail %q; want %v containing %q", key, it.Kind, it.Detail, kind, detail)
		}
	}
	check("git", step.Add, "")
	check("tmux", step.Same, "")
	check("iina", step.Same, "")
	check("deprecated:old", step.Warn, "")
	check("zed", step.Add, "")
	check("google-chrome", step.Add, "already exists")
	check("tailscale", step.Add, "needs your password")
	check("renamed:tailscale", step.Warn, "tailscale-app")
	check("someone/tools", step.Add, "third-party")
	check("someone/tools/thing", step.Add, "checked when installing")

	qs := p.Questions()
	if len(qs) != 1 || qs[0].ID != "apps.adopt.google-chrome" || qs[0].Default != Adopt {
		t.Errorf("questions = %+v; want one adopt question for google-chrome defaulting to adopt", qs)
	}
	for _, q := range fake.Queries() {
		if strings.Contains(q, "someone/tools/thing") {
			t.Errorf("looked up a package from a tap that isn't tapped yet: %s", q)
		}
	}
}

func TestPlanRejectsBadNames(t *testing.T) {
	_, _, _, _, err := prepare(t, testStep(t), newBrew(), `
[apps]
formulae = ["git", "gitt", "dead"]
casks = ["zedd"]
`)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`formula "gitt" not found`, `formula "dead" is disabled`, `cask "zedd" not found`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestApply(t *testing.T) {
	b := newBrew()
	b.install = func(c sys.Cmd, attempt int) (sys.Result, error) {
		switch {
		case systest.Is(c, "brew update"):
			return systest.Fail(c, "fatal: unable to access")
		case systest.Is(c, "brew install --formula git") && attempt < 3:
			return systest.Fail(c, "curl: (28) Operation timed out")
		case systest.Is(c, "brew install --cask zed"):
			return systest.Fail(c, "Error: It seems there is already an App at '/Applications/Zed.app'")
		}
		return sys.Result{}, nil
	}
	p, env, rec, fake, err := prepare(t, testStep(t), b, `
[apps]
formulae = ["git", "tmux"]
casks = ["zed", "google-chrome", "tailscale-app"]
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), env, prompt.Answers{"apps.adopt.google-chrome": Adopt}); err != nil {
		t.Fatal(err)
	}

	execs := fake.Execs()
	count := func(prefix string) int {
		n := 0
		for _, e := range execs {
			if strings.HasPrefix(e, prefix) {
				n++
			}
		}
		return n
	}
	if count("brew install --formula git") != 3 {
		t.Errorf("git: %d attempts, want 3 (network errors retried until success)", count("brew install --formula git"))
	}
	if count("brew install --cask zed") != 1 {
		t.Errorf("zed: %d attempts, want 1 (real errors are not retried)", count("brew install --cask zed"))
	}
	if count("brew install --cask --adopt google-chrome") != 1 {
		t.Errorf("google-chrome not installed with --adopt: %v", execs)
	}
	if count("brew install --formula tmux") != 0 {
		t.Error("already installed tmux was installed again")
	}

	want := map[string]string{"git": "done", "zed": "failed", "google-chrome": "done", "tailscale-app": "done"}
	got := rec.Outcomes()
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %q, want %q (a failed brew update or item must not stop the rest)", k, got[k], w)
		}
	}
}

func TestApplySkipAnswer(t *testing.T) {
	p, env, rec, fake, err := prepare(t, testStep(t), newBrew(), "[apps]\ncasks = [\"google-chrome\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), env, prompt.Answers{"apps.adopt.google-chrome": Skip}); err != nil {
		t.Fatal(err)
	}
	for _, e := range fake.Execs() {
		if strings.Contains(e, "google-chrome") {
			t.Errorf("ran %q although the user chose skip", e)
		}
	}
	if rec.Outcomes()["google-chrome"] != "skipped" {
		t.Error("google-chrome not reported as skipped")
	}
}

func TestApplyStopsWhenCancelled(t *testing.T) {
	p, env, _, fake, err := prepare(t, testStep(t), newBrew(), "[apps]\nformulae = [\"git\"]\ncasks = [\"zed\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Apply(ctx, env, nil); err == nil {
		t.Error("Apply returned nil after cancellation")
	}
	for _, e := range fake.Execs() {
		if strings.HasPrefix(e, "brew install") {
			t.Errorf("installed %q after cancellation", e)
		}
	}
}

func TestCaskApps(t *testing.T) {
	c := caskInfo{Artifacts: []map[string]json.RawMessage{
		{"app": json.RawMessage(`["Source.app", {"target": "Renamed.app"}]`)},
		{"app": json.RawMessage(`["Plain.app"]`)},
		{"zap": json.RawMessage(`[]`)},
	}}
	if got := c.apps(); !slices.Equal(got, []string{"Renamed.app", "Plain.app"}) {
		t.Errorf("apps() = %v", got)
	}
}
