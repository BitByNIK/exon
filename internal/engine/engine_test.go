package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitbynik/exon/internal/config"
	"github.com/bitbynik/exon/internal/events"
	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/sys/systest"
)

// fakeStep plans the given items and records whether Apply ran.
type fakeStep struct {
	name     string
	items    []step.PlanItem
	planErr  error
	requires []string
	// apply runs inside Apply; nil reports every changing item as done.
	apply   func(ctx context.Context, env step.Env) error
	prepare func(env step.Env) error // extra work during Prepare

	applied bool
}

func (s *fakeStep) Name() string       { return s.name }
func (s *fakeStep) Summary() string    { return s.name }
func (s *fakeStep) Requires() []string { return s.requires }

func (s *fakeStep) Prepare(_ context.Context, env step.Env) (step.Plan, error) {
	if s.prepare != nil {
		if err := s.prepare(env); err != nil {
			return nil, err
		}
	}
	if s.planErr != nil {
		return nil, s.planErr
	}
	return s, nil
}

func (s *fakeStep) Items() []step.PlanItem       { return s.items }
func (s *fakeStep) Questions() []prompt.Question { return nil }

func (s *fakeStep) Apply(ctx context.Context, env step.Env, _ prompt.Answers) error {
	s.applied = true
	if s.apply != nil {
		return s.apply(ctx, env)
	}
	for _, it := range s.items {
		if it.Kind.Changes() {
			env.Report.Done(it.Subject, "")
		}
	}
	return nil
}

func add(subject string) step.PlanItem { return step.PlanItem{Kind: step.Add, Subject: subject} }

type nullUI struct{}

func (nullUI) Emit(events.Event)     {}
func (nullUI) Plan([]StepPlan, bool) {}
func (nullUI) Summary(Report)        {}

type answer struct {
	confirm bool
	err     error
}

func (a answer) Confirm(context.Context, string, bool) (bool, error) { return a.confirm, a.err }
func (a answer) Ask(context.Context, []prompt.Question) (prompt.Answers, error) {
	return prompt.Answers{}, a.err
}

func options(t *testing.T, steps ...step.Step) Options {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, config.FileName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range steps {
		names = append(names, s.Name())
	}
	cfg, err := config.Load(path, names)
	if err != nil {
		t.Fatal(err)
	}
	return Options{
		Steps:     steps,
		Config:    cfg,
		Run:       &systest.Fake{Paths: map[string]string{"tmux": "/opt/homebrew/bin/tmux"}},
		FS:        sys.OSFS{},
		Prompt:    answer{confirm: true},
		UI:        nullUI{},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Repo:      dir,
		Home:      t.TempDir(),
		BackupDir: filepath.Join(t.TempDir(), "backups"),
	}
}

func TestDryRunNeverApplies(t *testing.T) {
	a := &fakeStep{name: "a", items: []step.PlanItem{add("x")}}
	o := options(t, a)
	o.DryRun = true
	_, code := Run(context.Background(), o)
	if a.applied {
		t.Error("Apply ran in a dry run")
	}
	if code != ExitOK {
		t.Errorf("exit %d, want %d", code, ExitOK)
	}
}

func TestDryRunWithConflictExitsProblems(t *testing.T) {
	a := &fakeStep{name: "a", items: []step.PlanItem{{Kind: step.Conflict, Subject: "x"}}}
	o := options(t, a)
	o.DryRun = true
	if _, code := Run(context.Background(), o); code != ExitProblems {
		t.Errorf("exit %d, want %d", code, ExitProblems)
	}
}

func TestPlanErrorBlocksEverything(t *testing.T) {
	good := &fakeStep{name: "good", items: []step.PlanItem{add("x")}}
	bad := &fakeStep{name: "bad", planErr: errors.New("unknown package")}
	_, code := Run(context.Background(), options(t, good, bad))
	if good.applied {
		t.Error("a step was applied although another step's plan failed")
	}
	if code != ExitProblems {
		t.Errorf("exit %d, want %d", code, ExitProblems)
	}
}

func TestPrepareIsReadOnly(t *testing.T) {
	sneaky := &fakeStep{name: "sneaky", prepare: func(env step.Env) error {
		return env.FS.WriteFile(filepath.Join(env.Home, "oops"), []byte("x"), 0o644)
	}}
	o := options(t, sneaky)
	o.DryRun = true
	_, code := Run(context.Background(), o)
	if _, err := os.Stat(filepath.Join(o.Home, "oops")); err == nil {
		t.Fatal("Prepare wrote a file")
	}
	if code != ExitProblems {
		t.Errorf("exit %d, want %d (a write during planning is a plan failure)", code, ExitProblems)
	}
}

func TestItemFailureContinuesAndExitsProblems(t *testing.T) {
	a := &fakeStep{name: "a", items: []step.PlanItem{add("x"), add("y")}, apply: func(_ context.Context, env step.Env) error {
		env.Report.Fail("x", errors.New("boom"), nil)
		env.Report.Done("y", "")
		return nil
	}}
	b := &fakeStep{name: "b", items: []step.PlanItem{add("z")}}
	rep, code := Run(context.Background(), options(t, a, b))
	if !b.applied {
		t.Error("step b did not run after an item in step a failed")
	}
	if code != ExitProblems {
		t.Errorf("exit %d, want %d", code, ExitProblems)
	}
	if got := rep.Steps[0]; got.Done != 1 || len(got.Failures) != 1 {
		t.Errorf("step a report = %+v, want 1 done, 1 failure", got)
	}
}

func TestPanicInStepIsContained(t *testing.T) {
	a := &fakeStep{name: "a", items: []step.PlanItem{add("x")}, apply: func(context.Context, step.Env) error { panic("bug") }}
	b := &fakeStep{name: "b", items: []step.PlanItem{add("y")}}
	rep, code := Run(context.Background(), options(t, a, b))
	if rep.Steps[0].Err == nil || !b.applied || code != ExitProblems {
		t.Errorf("panic not contained: report %+v, b applied %v, exit %d", rep.Steps, b.applied, code)
	}
}

func TestMissingRequirementSkipsStep(t *testing.T) {
	a := &fakeStep{name: "a", items: []step.PlanItem{add("x")}, requires: []string{"nonexistent"}}
	rep, _ := Run(context.Background(), options(t, a))
	if a.applied || rep.Steps[0].SkipReason == "" {
		t.Errorf("step with a missing requirement ran: %+v", rep.Steps[0])
	}
}

func TestDeclineChangesNothing(t *testing.T) {
	a := &fakeStep{name: "a", items: []step.PlanItem{add("x")}}
	o := options(t, a)
	o.Prompt = answer{confirm: false}
	rep, code := Run(context.Background(), o)
	if a.applied || !rep.Declined || code != ExitOK {
		t.Errorf("applied %v, declined %v, exit %d", a.applied, rep.Declined, code)
	}
}

func TestNothingToDo(t *testing.T) {
	a := &fakeStep{name: "a", items: []step.PlanItem{{Kind: step.Same, Subject: "x"}}}
	o := options(t, a)
	o.Prompt = answer{err: errors.New("must not ask")}
	rep, code := Run(context.Background(), o)
	if !rep.Nothing || code != ExitOK {
		t.Errorf("nothing = %v, exit %d", rep.Nothing, code)
	}
}

func TestCancelDuringApply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &fakeStep{name: "a", items: []step.PlanItem{add("x")}, apply: func(ctx context.Context, _ step.Env) error {
		cancel()
		return ctx.Err()
	}}
	b := &fakeStep{name: "b", items: []step.PlanItem{add("y")}}
	rep, code := Run(ctx, options(t, a, b))
	if b.applied {
		t.Error("step b ran after cancellation")
	}
	if code != ExitCancelled || !rep.Cancelled {
		t.Errorf("exit %d cancelled %v, want %d true", code, rep.Cancelled, ExitCancelled)
	}
	if rep.Steps[0].Err != nil {
		t.Errorf("cancellation reported as a step failure: %v", rep.Steps[0].Err)
	}
}
