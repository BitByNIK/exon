package ssh

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/sys/systest"
)

const both = "[ssh]\nask_for_hosts = true\nask_about_1password = true\n"

const orbstack = "Include ~/.orbstack/ssh/config\n"

type fixture struct {
	t    *testing.T
	env  step.Env
	rec  *steptest.Recorder
	step *Step
}

// newFixture sets up a home folder with ~/.ssh/config (if content != "") and
// optionally 1Password installed.
func newFixture(t *testing.T, tomlText, content string, with1Password bool) *fixture {
	t.Helper()
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "ssh", tomlText, &systest.Fake{}, rec)
	if content != "" {
		writeFile(t, filepath.Join(env.Home, ".ssh", "config"), content)
	}
	s := &Step{AppDir: t.TempDir()}
	if with1Password {
		if err := os.Mkdir(filepath.Join(s.AppDir, "1Password.app"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &fixture{t: t, env: env, rec: rec, step: s}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) prepare() step.Plan {
	f.t.Helper()
	p, err := f.step.Prepare(context.Background(), steptest.ReadOnly(f.env))
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) apply(p step.Plan, answers prompt.Answers) {
	f.t.Helper()
	if err := p.Apply(context.Background(), f.env, answers); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) config() string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.env.Home, ".ssh", "config"))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

// hosts builds answers for a Repeat group of hosts.
func hosts(entries ...map[string]string) prompt.Answers {
	a := prompt.Answers{qHosts + ".count": strconv.Itoa(len(entries))}
	for i, e := range entries {
		for k, v := range e {
			a[qHosts+"."+strconv.Itoa(i)+"."+k] = v
		}
	}
	return a
}

func question(p step.Plan, id string) (prompt.Question, bool) {
	for _, q := range p.Questions() {
		if q.ID == id {
			return q, true
		}
	}
	return prompt.Question{}, false
}

func TestOffUnlessEnabled(t *testing.T) {
	f := newFixture(t, "", orbstack, true)
	p := f.prepare()
	if len(p.Items()) != 0 || len(p.Questions()) != 0 {
		t.Errorf("items %v questions %v, want none", p.Items(), p.Questions())
	}
}

func TestAgentQuestion(t *testing.T) {
	t.Run("1Password installed: default yes", func(t *testing.T) {
		q, ok := question(newFixture(t, both, orbstack, true).prepare(), qAgent)
		if !ok || q.Default != "yes" {
			t.Errorf("question %+v asked=%v, want default yes", q, ok)
		}
	})
	t.Run("1Password missing: default no, plan says so", func(t *testing.T) {
		p := newFixture(t, both, orbstack, false).prepare()
		q, ok := question(p, qAgent)
		if !ok || q.Default != "no" {
			t.Errorf("question %+v asked=%v, want default no", q, ok)
		}
		if !strings.Contains(p.Items()[0].Detail, "isn't installed") {
			t.Errorf("plan item %+v should say 1Password isn't installed", p.Items()[0])
		}
	})
	t.Run("set up now: still asked, default yes", func(t *testing.T) {
		cfg := orbstack + "Host *\n  IdentityAgent \"" + agentSocket + "\"\n"
		q, ok := question(newFixture(t, both, cfg, false).prepare(), qAgent)
		if !ok || q.Default != "yes" {
			t.Errorf("question %+v asked=%v, want asked with default yes", q, ok)
		}
	})
}

func TestAgentKeptOrPutBack(t *testing.T) {
	withAgent := orbstack + "\nHost *\n  IdentityAgent \"" + agentSocket + "\"\n"

	t.Run("still there: not added twice", func(t *testing.T) {
		f := newFixture(t, both, withAgent, true)
		f.apply(f.prepare(), prompt.Answers{qAgent: "yes"})
		if f.config() != withAgent {
			t.Errorf("config changed:\n%s", f.config())
		}
	})

	t.Run("removed by configs before saving: put back", func(t *testing.T) {
		f := newFixture(t, both, withAgent, true)
		p := f.prepare()
		writeFile(t, filepath.Join(f.env.Home, ".ssh", "config"), orbstack) // what configs would do
		f.apply(p, prompt.Answers{qAgent: "yes"})
		if !strings.Contains(f.config(), "IdentityAgent") {
			t.Errorf("agent lost:\n%s", f.config())
		}
	})
}

func TestAddHostAndAgent(t *testing.T) {
	f := newFixture(t, both, orbstack, true)
	p := f.prepare()
	answers := hosts(map[string]string{qAlias: "pi", qAddr: "192.168.1.100", qUser: "nik", qPort: "2222"})
	answers[qAgent] = "yes"
	f.apply(p, answers)

	got := f.config()
	if !strings.HasPrefix(got, orbstack) {
		t.Errorf("existing content (the Include line) must stay first:\n%s", got)
	}
	for _, want := range []string{"IdentityAgent \"" + agentSocket + "\"", "Host pi\n  HostName 192.168.1.100\n  User nik\n  Port 2222\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}

	file, _ := os.Stat(filepath.Join(f.env.Home, ".ssh", "config"))
	dir, _ := os.Stat(filepath.Join(f.env.Home, ".ssh"))
	if file.Mode().Perm() != 0o600 || dir.Mode().Perm() != 0o700 {
		t.Errorf("permissions file %v dir %v, want 0600 and 0700", file.Mode().Perm(), dir.Mode().Perm())
	}
	backup, err := os.ReadFile(filepath.Join(f.env.BackupDir, ".ssh", "config"))
	if err != nil || string(backup) != orbstack {
		t.Errorf("backup = %q (%v), want the old file", backup, err)
	}
	if o := f.rec.Outcomes(); o["SSH host pi"] != "done" || o[agentItem] != "done" {
		t.Errorf("outcomes %v", o)
	}
}

func TestDefaultsLeftOut(t *testing.T) {
	f := newFixture(t, both, "", true)
	f.apply(f.prepare(), hosts(map[string]string{qAlias: "box", qAddr: "box.local", qUser: "", qPort: "22"}))
	if got := f.config(); got != "Host box\n  HostName box.local\n" {
		t.Errorf("config = %q, want no User line and no Port 22", got)
	}
}

func TestExistingHost(t *testing.T) {
	cfg := orbstack + "\nHost pi\n  HostName 10.0.0.1\n\nHost nas\n  HostName 10.0.0.2\n"

	t.Run("replace: old entry removed, others kept", func(t *testing.T) {
		f := newFixture(t, both, cfg, true)
		p := f.prepare()
		f.apply(p, hosts(map[string]string{qAlias: "pi", qReplace: "yes", qAddr: "10.0.0.9", qPort: "22"}))
		got := f.config()
		if strings.Count(got, "Host pi") != 1 || !strings.Contains(got, "HostName 10.0.0.9") || strings.Contains(got, "10.0.0.1") {
			t.Errorf("pi not replaced cleanly:\n%s", got)
		}
		if !strings.Contains(got, "Host nas\n  HostName 10.0.0.2") {
			t.Errorf("other host lost:\n%s", got)
		}
	})

	t.Run("keep: nothing written", func(t *testing.T) {
		f := newFixture(t, both, cfg, true)
		f.apply(f.prepare(), hosts(map[string]string{qAlias: "pi", qReplace: "no"}))
		if f.config() != cfg {
			t.Errorf("config changed although the user kept the host:\n%s", f.config())
		}
		if f.rec.Outcomes()["SSH host pi"] != "skipped" {
			t.Errorf("outcomes %v, want pi skipped", f.rec.Outcomes())
		}
	})

	t.Run("replace question only for existing names", func(t *testing.T) {
		q, _ := question(newFixture(t, both, cfg, true).prepare(), qHosts)
		var when func(map[string]string) bool
		for _, sub := range q.Repeat {
			if sub.ID == qReplace {
				when = sub.When
			}
		}
		if when == nil || !when(map[string]string{qAlias: "pi"}) || when(map[string]string{qAlias: "new"}) {
			t.Error("replace should be asked for pi (exists) and not for a new name")
		}
	})
}

func TestRereadsConfigBeforeSaving(t *testing.T) {
	// configs runs between planning and saving and may replace ~/.ssh/config.
	f := newFixture(t, both, "Host old\n  HostName 1.1.1.1\n", true)
	p := f.prepare()
	writeFile(t, filepath.Join(f.env.Home, ".ssh", "config"), orbstack)
	f.apply(p, hosts(map[string]string{qAlias: "pi", qAddr: "10.0.0.9", qPort: "22"}))
	got := f.config()
	if !strings.HasPrefix(got, orbstack) || strings.Contains(got, "Host old") || !strings.Contains(got, "Host pi") {
		t.Errorf("should add to the file as it is now:\n%s", got)
	}
}

func TestNothingAnswered(t *testing.T) {
	f := newFixture(t, both, orbstack, true)
	f.apply(f.prepare(), prompt.Answers{qAgent: "no"})
	if f.config() != orbstack {
		t.Error("config changed with nothing to add")
	}
	if _, err := os.Stat(f.env.BackupDir); err == nil {
		t.Error("made a backup with nothing to change")
	}
}

func TestValidators(t *testing.T) {
	tests := []struct {
		name  string
		check func(string) error
		good  []string
		bad   []string
	}{
		{"alias", validAlias, []string{"pi", "raspberrypi_5", "nas-2"}, []string{"my pi", "*", "pi?", "-x", "a,b"}},
		{"address", validAddress, []string{"192.168.1.100", "box.local"}, []string{"192.168 .1.1"}},
		{"user", validUser, []string{"nik"}, []string{"ni k"}},
		{"port", validPort, []string{"22", "1", "65535"}, []string{"0", "65536", "ssh", "-1"}},
	}
	for _, tt := range tests {
		for _, s := range tt.good {
			if err := tt.check(s); err != nil {
				t.Errorf("%s %q rejected: %v", tt.name, s, err)
			}
		}
		for _, s := range tt.bad {
			if tt.check(s) == nil {
				t.Errorf("%s %q accepted", tt.name, s)
			}
		}
	}
}
