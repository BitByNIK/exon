// Package events defines the progress events steps report while applying.
// Steps never print; a renderer turns events into output.
package events

import "time"

// Event is one progress report.
type Event interface{ event() }

// StepStarted reports that a step begins applying Items changes.
type StepStarted struct {
	Step  string
	Items int
}

// ItemStarted reports that work on one item (a package, a file) begins.
type ItemStarted struct{ Step, Item string }

// ItemPhase reports that an item moved to a new phase, e.g. "downloading".
type ItemPhase struct{ Step, Item, Phase string }

// ItemDone reports that an item finished successfully.
type ItemDone struct {
	Step, Item, Detail string
	Took               time.Duration
}

// ItemSkipped reports that an item needed no work or was skipped on purpose.
type ItemSkipped struct{ Step, Item, Reason string }

// ItemFailed reports that an item failed; the step continues with the next one.
type ItemFailed struct {
	Step, Item string
	Err        error
	Output     string // last lines of command output, may be empty
}

// Notice is something the user should see that is not tied to one item.
type Notice struct{ Step, Text string }

// StepFinished reports that a step is done. Err is set if the step as a whole failed.
type StepFinished struct {
	Step string
	Err  error
}

// StepSkipped reports that a step did not run.
type StepSkipped struct{ Step, Reason string }

func (StepStarted) event()  {}
func (ItemStarted) event()  {}
func (ItemPhase) event()    {}
func (ItemDone) event()     {}
func (ItemSkipped) event()  {}
func (ItemFailed) event()   {}
func (Notice) event()       {}
func (StepFinished) event() {}
func (StepSkipped) event()  {}

// Sink receives events. Implementations must be safe for concurrent use.
type Sink interface{ Emit(Event) }

// SinkFunc adapts a function to a Sink.
type SinkFunc func(Event)

// Emit calls f(e).
func (f SinkFunc) Emit(e Event) { f(e) }
