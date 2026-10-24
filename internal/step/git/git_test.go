package git

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/step/steptest"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
)

const enabled = "[git]\nset_name_and_email = true\n"

// fakeGit answers `git config --global --get <key>` from current; a missing
// key exits with 1, like real git.
func fakeGit(current map[string]string) *systest.Fake {
	return &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) {
		if len(c.Args) == 4 && c.Args[2] == "--get" {
			v, ok := current[c.Args[3]]
			if !ok {
				return sys.Result{}, &sys.ExitError{Cmd: c, Code: 1}
			}
			return sys.Result{Stdout: []byte(v + "\n")}, nil
		}
		return sys.Result{}, nil
	}}
}

func prepare(t *testing.T, tomlText string, run *systest.Fake) (step.Plan, step.Env, *steptest.Recorder) {
	t.Helper()
	rec := &steptest.Recorder{}
	env := steptest.Env(t, "git", tomlText, run, rec)
	p, err := New().Prepare(context.Background(), steptest.ReadOnly(env))
	if err != nil {
		t.Fatal(err)
	}
	return p, env, rec
}

func defaults(p step.Plan) map[string]string {
	out := map[string]string{}
	for _, q := range p.Questions() {
		out[q.ID] = q.Default
	}
	return out
}

func TestOffUnlessEnabled(t *testing.T) {
	p, _, _ := prepare(t, "", fakeGit(nil))
	if len(p.Items()) != 0 || len(p.Questions()) != 0 {
		t.Errorf("without set_name_and_email: items %v, questions %v; want none", p.Items(), p.Questions())
	}
}

func TestNotSetYet(t *testing.T) {
	run := fakeGit(nil)
	p, env, rec := prepare(t, enabled, run)

	if it := p.Items(); len(it) != 1 || it[0].Kind != step.Add || it[0].Detail != "not set yet" {
		t.Errorf("items = %+v, want one Add 'not set yet'", it)
	}
	if d := defaults(p); d[qName] != "" || d[qEmail] != "" {
		t.Errorf("defaults = %v, want empty", d)
	}

	answers := prompt.Answers{qName: "Ada Lovelace", qEmail: "ada@example.com"}
	if err := p.Apply(context.Background(), env, answers); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"git config --global user.name Ada Lovelace",
		"git config --global user.email ada@example.com",
	}
	if got := run.Execs(); !slices.Equal(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}
	if rec.Outcomes()[item] != "done" {
		t.Errorf("outcome = %q, want done", rec.Outcomes()[item])
	}
}

func TestAlreadySet(t *testing.T) {
	current := map[string]string{"user.name": "Ada Lovelace", "user.email": "ada@example.com"}

	t.Run("current values are the suggestions", func(t *testing.T) {
		p, _, _ := prepare(t, enabled, fakeGit(current))
		if d := defaults(p); d[qName] != "Ada Lovelace" || d[qEmail] != "ada@example.com" {
			t.Errorf("defaults = %v", d)
		}
		if it := p.Items(); len(it) != 1 || it[0].Kind != step.Change || !strings.Contains(it[0].Detail, "Ada Lovelace <ada@example.com>") {
			t.Errorf("items = %+v, want one Change showing the current values", it)
		}
	})

	t.Run("same answers change nothing", func(t *testing.T) {
		run := fakeGit(current)
		p, env, rec := prepare(t, enabled, run)
		if err := p.Apply(context.Background(), env, prompt.Answers{qName: "Ada Lovelace", qEmail: "ada@example.com"}); err != nil {
			t.Fatal(err)
		}
		if len(run.Execs()) != 0 || rec.Outcomes()[item] != "skipped" {
			t.Errorf("ran %q, outcome %q; want nothing run, skipped", run.Execs(), rec.Outcomes()[item])
		}
	})

	t.Run("new email is saved", func(t *testing.T) {
		run := fakeGit(current)
		p, env, _ := prepare(t, enabled, run)
		if err := p.Apply(context.Background(), env, prompt.Answers{qName: "Ada Lovelace", qEmail: "ada@work.example"}); err != nil {
			t.Fatal(err)
		}
		if got := run.Execs(); len(got) != 2 || got[1] != "git config --global user.email ada@work.example" {
			t.Errorf("ran %q", got)
		}
	})

	t.Run("values removed before saving are written back", func(t *testing.T) {
		// configs runs between planning and saving and may replace ~/.gitconfig.
		now := map[string]string{"user.name": "Ada Lovelace", "user.email": "ada@example.com"}
		run := fakeGit(now)
		p, env, rec := prepare(t, enabled, run)
		clear(now)
		if err := p.Apply(context.Background(), env, prompt.Answers{qName: "Ada Lovelace", qEmail: "ada@example.com"}); err != nil {
			t.Fatal(err)
		}
		if len(run.Execs()) != 2 || rec.Outcomes()[item] != "done" {
			t.Errorf("ran %q, outcome %q; want both values written", run.Execs(), rec.Outcomes()[item])
		}
	})
}

func TestYesWithNothingSet(t *testing.T) {
	run := fakeGit(nil)
	p, env, rec := prepare(t, enabled, run)
	// --yes answers with the defaults, which are empty here.
	if err := p.Apply(context.Background(), env, prompt.Answers{qName: "", qEmail: ""}); err != nil {
		t.Fatal(err)
	}
	if len(run.Execs()) != 0 || rec.Outcomes()[item] != "skipped" {
		t.Errorf("ran %q, outcome %q; want nothing run, skipped", run.Execs(), rec.Outcomes()[item])
	}
}

func TestReadErrorFailsPlan(t *testing.T) {
	run := &systest.Fake{Handler: func(c sys.Cmd) (sys.Result, error) {
		return sys.Result{}, &sys.ExitError{Cmd: c, Code: 128, Stderr: []byte("fatal: bad config file")}
	}}
	env := steptest.Env(t, "git", enabled, run, nil)
	if _, err := New().Prepare(context.Background(), steptest.ReadOnly(env)); err == nil {
		t.Error("a broken git config should fail the plan")
	}
}

func TestValidEmail(t *testing.T) {
	good := []string{"ada@example.com", "177606878+BitByNIK@users.noreply.github.com", "a.b@c.co.uk"}
	bad := []string{"", "ada", "ada@", "@example.com", "ada@example", "ada@@example.com", "ada @example.com", "Ada <ada@example.com>", "ada@example.", "ada@.com"}
	for _, s := range good {
		if err := validEmail(s); err != nil {
			t.Errorf("validEmail(%q) = %v, want ok", s, err)
		}
	}
	for _, s := range bad {
		if validEmail(s) == nil {
			t.Errorf("validEmail(%q) accepted, want rejected", s)
		}
	}
}
