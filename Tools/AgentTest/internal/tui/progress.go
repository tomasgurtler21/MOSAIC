package tui

// progress.go adapts the domain.ProgressSink port to the Bubble Tea message
// loop (I16.2), so events are folded on arrival rather than discovered by
// polling the suite.

import (
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/domain"
)

// NewProgramSink adapts the progress port to the Bubble Tea message loop, so
// events are folded on arrival rather than discovered by polling the suite.
// This is trivial forwarding, not folding logic: what it feeds (Model.Fold)
// carries the actual behaviour.
func NewProgramSink(send func(tea.Msg)) domain.ProgressSink {
	return programSink{send: send}
}

type programSink struct {
	send func(tea.Msg)
}

func (s programSink) Emit(ev domain.ProgressEvent) {
	if s.send != nil {
		s.send(ProgressMsg{Event: ev})
	}
}

var _ domain.ProgressSink = programSink{}

// sinkBox holds the domain.ProgressSink a running suite emits into. It
// exists so Run can install the real, program-backed sink (built from the
// tea.Program's own Send method) after tea.NewProgram has already made its
// own copy of the initial Model — every copy of a Model built from the same
// NewModel call shares one *sinkBox, so installing the sink once reaches
// every copy.
//
// A freshly constructed sinkBox holds a discard sink, so a Model driven
// directly (as every test in this package does, with no real tea.Program)
// never calls through a nil interface.
type sinkBox struct {
	mu   sync.Mutex
	sink domain.ProgressSink
}

func newSinkBox() *sinkBox {
	return &sinkBox{sink: discardSink{}}
}

func (b *sinkBox) get() domain.ProgressSink {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sink
}

func (b *sinkBox) set(sink domain.ProgressSink) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sink = sink
}

// discardSink is a domain.ProgressSink that drops every event. It is the
// sinkBox default so a Model built and driven without a real program (every
// test in this package) is always safe to hand to a SuiteRunner.
type discardSink struct{}

func (discardSink) Emit(domain.ProgressEvent) {}

var _ domain.ProgressSink = discardSink{}
