package step_test

import (
	"testing"
	"time"

	"github.com/bitbynik/exon/internal/events"
	"github.com/bitbynik/exon/internal/step"
)

func TestStartUntimedReportsNoDuration(t *testing.T) {
	var got []events.Event
	r := step.NewReporter(events.SinkFunc(func(e events.Event) { got = append(got, e) }), "demo")

	r.Start("timed")
	r.StartUntimed("untimed")
	time.Sleep(30 * time.Millisecond)
	r.Done("timed", "")
	r.Done("untimed", "")

	took := map[string]time.Duration{}
	started := 0
	for _, e := range got {
		switch e := e.(type) {
		case events.ItemStarted:
			started++
		case events.ItemDone:
			took[e.Item] = e.Took
		}
	}
	if started != 2 {
		t.Errorf("%d items announced, want 2: an untimed item is still announced", started)
	}
	if took["timed"] < 30*time.Millisecond {
		t.Errorf("timed item took %v, want at least 30ms", took["timed"])
	}
	if took["untimed"] != 0 {
		t.Errorf("untimed item took %v, want no duration", took["untimed"])
	}
}
