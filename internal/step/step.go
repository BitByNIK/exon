// Package step defines what a setup step is. Each step lives in its own
// sub-package and is listed in package step/all.
package step

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bitbynik/exon/internal/config"
	"github.com/bitbynik/exon/internal/events"
	"github.com/bitbynik/exon/internal/prompt"
	"github.com/bitbynik/exon/internal/sys"
)

// Step is one setup step, e.g. "apps".
type Step interface {
	// Name is the short id used on the command line and as the exon.toml section.
	Name() string
	// Summary is a one-line description.
	Summary() string
	// Requires lists commands that must be installed when the step applies.
	// They may be installed by an earlier step in the same run.
	Requires() []string
	// Prepare inspects the system and returns what the step would do.
	// It receives a read-only Env: any attempt to change something fails.
	Prepare(ctx context.Context, env Env) (Plan, error)
}

// Plan is what a step would do. Building and showing it changes nothing.
type Plan interface {
	Items() []PlanItem
	// Questions are asked after the user confirms the plan, before Apply.
	Questions() []prompt.Question
	// Apply makes the changes. Per-item failures are reported through
	// env.Report and do not stop the step; a returned error means the whole
	// step failed.
	Apply(ctx context.Context, env Env, answers prompt.Answers) error
}

// Kind classifies a plan item.
type Kind int

// Plan item kinds.
const (
	Add      Kind = iota // something new will be created or installed
	Change               // something existing will be replaced
	Same                 // already as wanted; nothing to do
	Conflict             // cannot be done; will be skipped and reported
	Note                 // information only
	Warn                 // a warning; does not block
)

// Changes reports whether a kind means work will be done.
func (k Kind) Changes() bool { return k == Add || k == Change }

// PlanItem is one line of a plan.
type PlanItem struct {
	Kind    Kind
	Action  string // "install", "replace", "adopt?" ...
	Subject string // "zed", "~/.zshrc"
	Detail  string // extra context, may be empty
}

// Env is everything a step may touch. Steps never use os/exec or write files
// through package os directly.
type Env struct {
	Run       sys.Runner
	FS        sys.FS
	Net       sys.Downloader // fetches release data and files
	Report    *Reporter
	Log       *slog.Logger
	Config    config.Section // this step's [section]
	Repo      string         // config repo root
	Home      string         // the user's home folder
	BackupDir string         // where to back up replaced files for this run
	CacheDir  string         // exon's folder for downloads that are safe to delete
}

// Reporter sends one step's progress events and measures item durations.
type Reporter struct {
	sink  events.Sink
	step  string
	mu    sync.Mutex
	start map[string]time.Time
}

// NewReporter returns a Reporter for step that emits to sink.
func NewReporter(sink events.Sink, step string) *Reporter {
	return &Reporter{sink: sink, step: step, start: map[string]time.Time{}}
}

// Start reports that work on item begins.
func (r *Reporter) Start(item string) {
	r.mu.Lock()
	r.start[item] = time.Now()
	r.mu.Unlock()
	r.sink.Emit(events.ItemStarted{Step: r.step, Item: item})
}

// StartUntimed is Start for an item that is announced well before its work
// finishes, so that the time from here to Done would be misleading. Done and
// Fail then report no duration.
func (r *Reporter) StartUntimed(item string) {
	r.sink.Emit(events.ItemStarted{Step: r.step, Item: item})
}

// Phase reports that item moved to a new phase.
func (r *Reporter) Phase(item, phase string) {
	r.sink.Emit(events.ItemPhase{Step: r.step, Item: item, Phase: phase})
}

// Done reports that item succeeded.
func (r *Reporter) Done(item, detail string) {
	r.sink.Emit(events.ItemDone{Step: r.step, Item: item, Detail: detail, Took: r.took(item)})
}

// Skip reports that item was skipped.
func (r *Reporter) Skip(item, reason string) {
	r.sink.Emit(events.ItemSkipped{Step: r.step, Item: item, Reason: reason})
}

// Fail reports that item failed. output may be the command output; only its last lines are kept.
func (r *Reporter) Fail(item string, err error, output []byte) {
	r.took(item)
	r.sink.Emit(events.ItemFailed{Step: r.step, Item: item, Err: err, Output: sys.LastLines(string(output), 5)})
}

// Notice reports a message not tied to one item.
func (r *Reporter) Notice(text string) {
	r.sink.Emit(events.Notice{Step: r.step, Text: text})
}

func (r *Reporter) took(item string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.start[item]
	if !ok {
		return 0
	}
	delete(r.start, item)
	return time.Since(t)
}
