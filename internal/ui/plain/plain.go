// Package plain renders exon's output as plain text lines: for pipes, CI,
// NO_COLOR, and until the TUI renderer exists.
package plain

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/bitbynik/exon/internal/engine"
	"github.com/bitbynik/exon/internal/events"
	"github.com/bitbynik/exon/internal/preflight"
	"github.com/bitbynik/exon/internal/step"
	"github.com/bitbynik/exon/internal/usererr"
)

// Renderer writes plain text to w.
type Renderer struct {
	w    io.Writer
	home string

	mu    sync.Mutex
	total int // items to install/copy in the current step
	n     int // items started so far in the current step
}

// New returns a Renderer writing to w. Paths under home are shown as ~/...
func New(w io.Writer, home string) *Renderer { return &Renderer{w: w, home: home} }

func (r *Renderer) printf(format string, args ...any) { fmt.Fprintf(r.w, format, args...) }

// Checks prints the pre-run check results.
func (r *Renderer) Checks(rs []preflight.Result) {
	r.printf("Checking your Mac\n")
	for _, c := range rs {
		sym := map[preflight.Status]string{preflight.OK: "✓", preflight.Warn: "!", preflight.Fail: "✗"}[c.Status]
		line := "  " + sym + " " + c.Name
		if c.Detail != "" {
			line += ": " + c.Detail
		}
		r.printf("%s\n", line)
		if c.Hint != "" && c.Status != preflight.OK {
			r.printf("%s\n", indent(c.Hint, "      "))
		}
	}
	r.printf("\n")
}

var kindSym = map[step.Kind]string{
	step.Add: "+", step.Change: "~", step.Same: "=", step.Conflict: "✗", step.Note: "·", step.Warn: "!",
}

// Plan prints every step's plan. Unchanged items are grouped on one line.
func (r *Renderer) Plan(plans []engine.StepPlan, dryRun bool) {
	if dryRun {
		r.printf("DRY RUN: nothing will be changed\n\n")
	}
	r.printf("Plan\n")
	for _, sp := range plans {
		r.printf("  %s\n", sp.Step.Name())
		if sp.Err != nil {
			r.printf("%s\n", indent(errorText(sp.Err), "    ✗ "))
			continue
		}
		var same []string
		for _, it := range sp.Items() {
			if it.Kind == step.Same {
				same = append(same, it.Subject)
				continue
			}
			line := fmt.Sprintf("    %s %-11s %s", kindSym[it.Kind], it.Action, it.Subject)
			if it.Detail != "" {
				line += "  (" + it.Detail + ")"
			}
			r.printf("%s\n", line)
		}
		if len(same) > 0 {
			r.printf("    = %-11s %s (%d)\n", "unchanged", strings.Join(same, ", "), len(same))
		}
		if len(sp.Items()) == 0 {
			r.printf("    · nothing configured\n")
		}
	}
	r.printf("\n")
}

// Emit prints progress events.
func (r *Renderer) Emit(e events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch e := e.(type) {
	case events.StepStarted:
		r.total, r.n = e.Items, 0
		r.printf("%s\n", e.Step)
	case events.StepSkipped:
		r.printf("%s: skipped, %s\n", e.Step, e.Reason)
	case events.ItemStarted:
		r.n++
		if r.n <= r.total {
			r.printf("  → [%d/%d] %s\n", r.n, r.total, e.Item)
		} else {
			// Answers can turn one planned item into several ("SSH hosts" → each host).
			r.printf("  → %s\n", e.Item)
		}
	case events.ItemDone:
		line := "  ✓ " + e.Item
		if e.Took >= 100*time.Millisecond {
			line += "  " + e.Took.Round(100*time.Millisecond).String()
		}
		if e.Detail != "" {
			line += "  " + e.Detail
		}
		r.printf("%s\n", line)
	case events.ItemSkipped:
		r.printf("  ↷ %s  %s\n", e.Item, e.Reason)
	case events.ItemFailed:
		r.printf("  ✗ %s  %s\n", e.Item, errorText(e.Err))
		if e.Output != "" {
			r.printf("%s\n", indent(e.Output, "      │ "))
		}
	case events.Notice:
		r.printf("  · %s\n", e.Text)
	case events.StepFinished:
		if e.Err != nil {
			r.printf("  ✗ %s failed: %s\n", e.Step, errorText(e.Err))
		}
		r.printf("\n")
	}
}

// Summary prints the outcome of the run, then the checklist of things to do by hand.
func (r *Renderer) Summary(rep engine.Report) {
	defer r.checklist(rep.Checklist)
	took := rep.Took.Round(time.Second)
	switch {
	case rep.DryRun:
		r.printf("Dry run finished: nothing was changed.\n")
		return
	case rep.Nothing:
		r.printf("Everything is already set up. Nothing to do.\n")
		return
	case rep.Declined:
		r.printf("Nothing changed.\n")
		return
	case len(rep.Steps) == 0 && !rep.Cancelled:
		r.printf("Fix the problems above, then run exon again. Nothing was changed.\n")
		return
	}

	problems := 0
	for _, s := range rep.Steps {
		problems += len(s.Failures)
		if s.Err != nil {
			problems++
		}
	}
	switch {
	case rep.Cancelled:
		r.printf("Cancelled after %s\n", took)
	case problems > 0:
		r.printf("Done in %s with %d problem%s\n", took, problems, plural(problems))
	default:
		r.printf("Done in %s\n", took)
	}

	for _, s := range rep.Steps {
		switch {
		case s.NotConfigured:
			continue
		case s.SkipReason != "":
			r.printf("  %-10s skipped: %s\n", s.Name, s.SkipReason)
		default:
			line := fmt.Sprintf("  %-10s ✓ %d done", s.Name, s.Done)
			if s.Skipped > 0 {
				line += fmt.Sprintf("  ↷ %d skipped", s.Skipped)
			}
			if n := len(s.Failures); n > 0 {
				line += fmt.Sprintf("  ✗ %d failed", n)
			}
			r.printf("%s\n", line)
		}
	}

	if problems > 0 {
		r.printf("\nProblems\n")
		for _, s := range rep.Steps {
			if s.Err != nil {
				r.printf("%s\n", indent(errorText(s.Err), "  ✗ "+s.Name+": "))
			}
			for _, f := range s.Failures {
				r.printf("  ✗ %s · %s: %s\n", s.Name, f.Item, errorText(f.Err))
			}
		}
		r.printf("  Fix the cause and run the same command again: finished items are skipped.\n")
	}
	if rep.BackupDir != "" {
		r.printf("\nBackups: %s\n", r.short(rep.BackupDir))
	}
	if rep.LogPath != "" {
		r.printf("Log:     %s\n", r.short(rep.LogPath))
	}
}

// checklist prints the things left to do by hand, if there are any.
func (r *Renderer) checklist(items []string) {
	if len(items) == 0 {
		return
	}
	r.printf("\nNext, by hand:\n")
	for _, it := range items {
		r.printf("  □ %s\n", it)
	}
}

// Error prints an error that ended the run before the engine started.
func (r *Renderer) Error(err error) {
	r.printf("%s\n", indent(errorText(err), "✗ "))
}

func (r *Renderer) short(p string) string {
	if r.home != "" && strings.HasPrefix(p, r.home+"/") {
		return "~" + strings.TrimPrefix(p, r.home)
	}
	return p
}

// errorText shows a usererr's message and hint; other errors as they are.
func errorText(err error) string {
	if ue, ok := usererr.As(err); ok {
		if ue.Hint != "" {
			return ue.Msg + "\n" + indent(ue.Hint, "  ")
		}
		return ue.Msg
	}
	return err.Error()
}

// indent prefixes the first line with first and aligns the rest under it.
func indent(s, first string) string {
	pad := strings.Repeat(" ", len([]rune(first)))
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
		} else {
			lines[i] = pad + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
