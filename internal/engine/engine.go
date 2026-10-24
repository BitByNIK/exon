// Package engine runs steps: plan all of them, show the plan, confirm, ask
// questions, apply in order, and summarise. It owns the failure rules.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/bitbynik/exon/internal/config"
	"github.com/bitbynik/exon/internal/events"
	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

// Exit codes.
const (
	ExitOK        = 0
	ExitProblems  = 1 // finished with failures, or the plan has problems
	ExitPreflight = 2 // pre-run checks failed
	ExitCancelled = 130
)

// UI shows plans, progress and the summary.
type UI interface {
	events.Sink
	Plan(plans []StepPlan, dryRun bool)
	Summary(r Report)
}

// Options configure a run.
type Options struct {
	Steps  []step.Step // selected steps, in run order
	Config *config.File
	DryRun bool
	Run    sys.Runner
	FS     sys.FS
	Prompt prompt.Prompter
	UI     UI
	Log    *slog.Logger

	Repo      string
	Home      string
	BackupDir string
	LogPath   string
	// Checklist is printed at the end of every run: things to do by hand.
	Checklist []string
}

// StepPlan is one step's plan, or the error that stopped it from planning.
type StepPlan struct {
	Step  step.Step
	Plan  step.Plan
	Err   error
	items []step.PlanItem
}

// Items returns the plan's items (empty if planning failed).
func (sp StepPlan) Items() []step.PlanItem { return sp.items }

// Report is the outcome of a run.
type Report struct {
	Steps     []StepReport
	Took      time.Duration
	Cancelled bool
	DryRun    bool
	Nothing   bool // everything was already set up
	Declined  bool // the user said no at the confirmation
	LogPath   string
	BackupDir string   // set only if backups were written
	Checklist []string // things to do by hand
}

// StepReport is the outcome of one step.
type StepReport struct {
	Name       string
	Done       int
	Skipped    int
	Failures   []events.ItemFailed
	Err        error  // the step as a whole failed
	SkipReason string // the step did not run
	// NotConfigured: exon.toml doesn't ask this step for anything. The plan
	// already says so, so progress and the summary leave it out.
	NotConfigured bool
}

// Failed reports whether anything in the run failed.
func (r Report) Failed() bool {
	for _, s := range r.Steps {
		if s.Err != nil || len(s.Failures) > 0 {
			return true
		}
	}
	return false
}

// Run plans and, unless DryRun, applies the selected steps. It returns the
// report and the process exit code.
func Run(ctx context.Context, o Options) (Report, int) {
	start := time.Now()
	report := Report{DryRun: o.DryRun, LogPath: o.LogPath, Checklist: o.Checklist}

	plans := planAll(ctx, o)
	o.UI.Plan(plans, o.DryRun)

	planFailed, conflicts, changes, questions := false, false, false, []prompt.Question(nil)
	for _, sp := range plans {
		if sp.Err != nil {
			planFailed = true
			continue
		}
		questions = append(questions, sp.Plan.Questions()...)
		for _, it := range sp.items {
			conflicts = conflicts || it.Kind == step.Conflict
			changes = changes || it.Kind.Changes()
		}
	}

	switch {
	case ctx.Err() != nil:
		report.Cancelled = true
		return finish(o, report, start, ExitCancelled)
	case planFailed:
		// Never apply part of a plan the user couldn't fully review.
		return finish(o, report, start, ExitProblems)
	case o.DryRun:
		if conflicts {
			return finish(o, report, start, ExitProblems)
		}
		return finish(o, report, start, ExitOK)
	case !changes && !conflicts:
		report.Nothing = true
		return finish(o, report, start, ExitOK)
	}

	ok, err := o.Prompt.Confirm(ctx, "Apply these changes?", false)
	if err != nil {
		return abort(ctx, o, report, start, err)
	}
	if !ok {
		report.Declined = true
		return finish(o, report, start, ExitOK)
	}
	answers, err := o.Prompt.Ask(ctx, questions)
	if err != nil {
		return abort(ctx, o, report, start, err)
	}

	tally := newTally(o.UI)
	for _, sp := range plans {
		sr := applyOne(ctx, o, tally, sp, answers)
		report.Steps = append(report.Steps, sr)
	}
	report.Cancelled = ctx.Err() != nil
	if info, err := o.FS.Stat(o.BackupDir); err == nil && info.IsDir() {
		report.BackupDir = o.BackupDir
	}

	code := ExitOK
	switch {
	case report.Cancelled:
		code = ExitCancelled
	case report.Failed():
		code = ExitProblems
	}
	return finish(o, report, start, code)
}

func finish(o Options, r Report, start time.Time, code int) (Report, int) {
	r.Took = time.Since(start)
	o.UI.Summary(r)
	return r, code
}

func abort(ctx context.Context, o Options, r Report, start time.Time, err error) (Report, int) {
	if ctx.Err() != nil {
		r.Cancelled = true
		return finish(o, r, start, ExitCancelled)
	}
	r.Steps = append(r.Steps, StepReport{Name: "input", Err: err})
	return finish(o, r, start, ExitProblems)
}

// planAll prepares every step with a read-only Env.
func planAll(ctx context.Context, o Options) []StepPlan {
	plans := make([]StepPlan, 0, len(o.Steps))
	for _, s := range o.Steps {
		env := o.env(s, events.SinkFunc(func(events.Event) {}))
		env.Run = sys.ReadOnlyRunner(o.Run)
		env.FS = sys.ReadOnlyFS(o.FS)
		sp := StepPlan{Step: s}
		sp.Plan, sp.Err = safePrepare(ctx, s, env)
		if sp.Err != nil {
			o.Log.Error("plan failed", "step", s.Name(), "err", sp.Err)
		} else {
			sp.items = sp.Plan.Items()
		}
		plans = append(plans, sp)
	}
	return plans
}

func safePrepare(ctx context.Context, s step.Step, env step.Env) (p step.Plan, err error) {
	defer recoverInto(&err, env.Log, s.Name())
	p, err = s.Prepare(ctx, env)
	if err == nil && p == nil {
		err = fmt.Errorf("step %s returned no plan", s.Name())
	}
	return p, err
}

func applyOne(ctx context.Context, o Options, tally *tally, sp StepPlan, answers prompt.Answers) StepReport {
	name := sp.Step.Name()
	skip := func(reason string) StepReport {
		o.UI.Emit(events.StepSkipped{Step: name, Reason: reason})
		return StepReport{Name: name, SkipReason: reason}
	}
	if len(sp.items) == 0 {
		return StepReport{Name: name, NotConfigured: true}
	}
	if ctx.Err() != nil {
		return skip("cancelled")
	}
	for _, cmd := range sp.Step.Requires() {
		if _, err := o.Run.LookPath(cmd); err != nil {
			return skip(cmd + " is not installed")
		}
	}
	// Conflicts are reported by Apply too, but only changes count as progress.
	changes, conflicts := 0, 0
	for _, it := range sp.items {
		switch {
		case it.Kind.Changes():
			changes++
		case it.Kind == step.Conflict:
			conflicts++
		}
	}
	if changes+conflicts == 0 {
		return skip("nothing to do")
	}

	o.UI.Emit(events.StepStarted{Step: name, Items: changes})
	env := o.env(sp.Step, tally)
	err := safeApply(ctx, sp, env, answers)
	if err != nil && errors.Is(err, context.Canceled) {
		err = nil // reported as a cancelled run, not a step failure
	}
	o.UI.Emit(events.StepFinished{Step: name, Err: err})

	sr := tally.result(name)
	sr.Err = err
	return sr
}

func safeApply(ctx context.Context, sp StepPlan, env step.Env, answers prompt.Answers) (err error) {
	defer recoverInto(&err, env.Log, sp.Step.Name())
	return sp.Plan.Apply(ctx, env, answers)
}

// recoverInto turns a panic in a step into an error and logs the stack.
func recoverInto(err *error, log *slog.Logger, name string) {
	if r := recover(); r != nil {
		log.Error("step panicked", "step", name, "panic", r, "stack", string(debug.Stack()))
		*err = usererr.New(fmt.Sprintf("internal error in step %s", name), "This is a bug in exon. Please report it with the log file.", fmt.Errorf("%v", r))
	}
}

func (o Options) env(s step.Step, sink events.Sink) step.Env {
	return step.Env{
		Run:       o.Run,
		FS:        o.FS,
		Report:    step.NewReporter(sink, s.Name()),
		Log:       o.Log.With("step", s.Name()),
		Config:    o.Config.Section(s.Name()),
		Repo:      o.Repo,
		Home:      o.Home,
		BackupDir: o.BackupDir,
	}
}

// tally forwards events to the UI and counts outcomes per step.
type tally struct {
	next  events.Sink
	mu    sync.Mutex
	steps map[string]*StepReport
}

func newTally(next events.Sink) *tally {
	return &tally{next: next, steps: map[string]*StepReport{}}
}

func (t *tally) Emit(e events.Event) {
	t.mu.Lock()
	switch e := e.(type) {
	case events.ItemDone:
		t.get(e.Step).Done++
	case events.ItemSkipped:
		t.get(e.Step).Skipped++
	case events.ItemFailed:
		sr := t.get(e.Step)
		sr.Failures = append(sr.Failures, e)
	}
	t.mu.Unlock()
	t.next.Emit(e)
}

func (t *tally) get(name string) *StepReport {
	sr, ok := t.steps[name]
	if !ok {
		sr = &StepReport{Name: name}
		t.steps[name] = sr
	}
	return sr
}

func (t *tally) result(name string) StepReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	return *t.get(name)
}
