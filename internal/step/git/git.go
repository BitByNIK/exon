// Package git asks for the name and email git puts on commits and saves them
// in ~/.gitconfig. They are never stored in the config repo.
package git

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys"
)

// Config is the [git] section of exon.toml.
type Config struct {
	// SetNameAndEmail asks for the git name and email during setup.
	SetNameAndEmail bool `toml:"set_name_and_email"`
}

// Question IDs.
const (
	qName  = "git.name"
	qEmail = "git.email"
)

// item is how this step's one piece of work appears in plans and progress.
const item = "git name and email"

// Step is the git step.
type Step struct{}

// New returns the git step.
func New() *Step { return &Step{} }

// Name implements step.Step.
func (*Step) Name() string { return "git" }

// Summary implements step.Step.
func (*Step) Summary() string { return "Set your git name and email" }

// Requires implements step.Step.
func (*Step) Requires() []string { return []string{"git"} }

// Prepare reads the current name and email, to suggest them as answers.
func (*Step) Prepare(ctx context.Context, env step.Env) (step.Plan, error) {
	var cfg Config
	if err := env.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	if !cfg.SetNameAndEmail {
		return &plan{}, nil
	}
	name, err := current(ctx, env.Run, "user.name")
	if err != nil {
		return nil, err
	}
	email, err := current(ctx, env.Run, "user.email")
	if err != nil {
		return nil, err
	}
	return &plan{enabled: true, name: name, email: email}, nil
}

// current returns a global git setting, or "" if it isn't set.
func current(ctx context.Context, run sys.Runner, key string) (string, error) {
	res, err := run.Query(ctx, sys.Cmd{Name: "git", Args: []string{"config", "--global", "--get", key}})
	if err != nil {
		// git exits with 1 when the setting doesn't exist.
		if ee, ok := errors.AsType[*sys.ExitError](err); ok && ee.Code == 1 {
			return "", nil
		}
		return "", fmt.Errorf("read git %s: %w", key, err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

type plan struct {
	enabled     bool
	name, email string // current values, "" if not set
}

func (p *plan) Items() []step.PlanItem {
	if !p.enabled {
		return nil
	}
	if p.name == "" || p.email == "" {
		return []step.PlanItem{{Kind: step.Add, Action: "ask", Subject: item, Detail: "not set yet"}}
	}
	return []step.PlanItem{{
		Kind: step.Change, Action: "ask", Subject: item,
		Detail: fmt.Sprintf("now %s <%s>; Enter keeps it", p.name, p.email),
	}}
}

func (p *plan) Questions() []prompt.Question {
	if !p.enabled {
		return nil
	}
	return []prompt.Question{
		{ID: qName, Text: "Your name for git commits", Input: true, Default: p.name},
		{ID: qEmail, Text: "Your email for git commits", Input: true, Default: p.email, Validate: validEmail},
	}
}

// Apply saves the answers in ~/.gitconfig, unless they're already there.
func (p *plan) Apply(ctx context.Context, env step.Env, answers prompt.Answers) error {
	if !p.enabled {
		return nil
	}
	name, email := answers[qName], answers[qEmail]
	if name == "" || email == "" {
		// Only possible with --yes when nothing is set yet.
		env.Report.Skip(item, "no name or email given")
		return nil
	}
	if err := validEmail(email); err != nil {
		env.Report.Fail(item, err, nil)
		return nil
	}
	// Compare with what's set now, not when the plan was made: configs may
	// have replaced ~/.gitconfig since, removing the name and email.
	nowName, err := current(ctx, env.Run, "user.name")
	if err != nil {
		env.Report.Fail(item, err, nil)
		return nil
	}
	nowEmail, err := current(ctx, env.Run, "user.email")
	if err != nil {
		env.Report.Fail(item, err, nil)
		return nil
	}
	if name == nowName && email == nowEmail {
		env.Report.Skip(item, "unchanged")
		return nil
	}

	env.Report.Start(item)
	for _, kv := range [][2]string{{"user.name", name}, {"user.email", email}} {
		res, err := env.Run.Exec(ctx, sys.Cmd{Name: "git", Args: []string{"config", "--global", kv[0], kv[1]}})
		if err != nil {
			env.Report.Fail(item, err, res.Output())
			return nil
		}
	}
	env.Report.Done(item, fmt.Sprintf("%s <%s>", name, email))
	return nil
}

// validEmail does a light check: one @, something before it, a dot after it, no spaces.
func validEmail(s string) error {
	at := strings.Index(s, "@")
	domain := s[at+1:]
	if at < 1 || strings.Count(s, "@") != 1 || strings.ContainsAny(s, " \t<>") ||
		!strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return errors.New("that doesn't look like an email address")
	}
	return nil
}
