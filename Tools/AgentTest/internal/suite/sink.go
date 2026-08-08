package suite

import (
	"sync"

	"mosaic-agent-test/internal/domain"
)

// DefaultSinkCapacity is the default buffer depth for NewAsyncSink.
const DefaultSinkCapacity = 1024

// AsyncSink wraps a sink so a slow, blocking or failing consumer can
// neither stall the suite nor change a verdict. Emit hands off and returns
// immediately; delivery happens on the wrapper's own goroutine.
//
// Under saturation the wrapper drops ProgressInvocation events, oldest
// first, and never drops any other kind. That asymmetry is deliberate: an
// invocation event is a progress detail a display can survive missing,
// while a dropped test-finished event would lose a verdict — and a display
// that silently loses verdicts is worse than one that lags.
type AsyncSink struct {
	inner    domain.ProgressSink
	capacity int

	mu              sync.Mutex
	cond            *sync.Cond
	queue           []domain.ProgressEvent
	invocationCount int
	dropped         int
	closed          bool
	done            chan struct{}
}

// NewAsyncSink constructs an AsyncSink wrapping inner with the given buffer
// capacity. A non-positive capacity falls back to DefaultSinkCapacity.
func NewAsyncSink(inner domain.ProgressSink, capacity int) *AsyncSink {
	if capacity <= 0 {
		capacity = DefaultSinkCapacity
	}
	s := &AsyncSink{
		inner:    inner,
		capacity: capacity,
		done:     make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)
	go s.deliver()
	return s
}

// Emit hands ev off for asynchronous delivery. It never blocks the caller.
//
// When the buffer already holds capacity ProgressInvocation events, the
// oldest queued invocation event is dropped to make room. Any other event
// kind is always enqueued, uncapped by capacity: only an invocation event
// is a detail a display can survive missing.
func (s *AsyncSink) Emit(ev domain.ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if ev.Kind == domain.ProgressInvocation {
		if s.invocationCount >= s.capacity {
			for i, q := range s.queue {
				if q.Kind == domain.ProgressInvocation {
					s.queue = append(s.queue[:i], s.queue[i+1:]...)
					s.invocationCount--
					s.dropped++
					break
				}
			}
		}
		s.invocationCount++
	}
	s.queue = append(s.queue, ev)
	s.cond.Signal()
}

// Dropped reports how many ProgressInvocation events have been dropped
// under saturation since construction.
func (s *AsyncSink) Dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Close stops the delivery goroutine, waiting for any in-flight delivery to
// finish or its inner sink to fail, whichever happens first.
func (s *AsyncSink) Close() {
	s.mu.Lock()
	s.closed = true
	s.cond.Broadcast()
	s.mu.Unlock()
	<-s.done
}

// deliver drains the queue in order on its own goroutine, recovering a
// panic from the inner sink so a failing consumer never stops delivery of
// what follows or reaches back into the suite.
func (s *AsyncSink) deliver() {
	defer close(s.done)
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.cond.Wait()
		}
		if len(s.queue) == 0 && s.closed {
			s.mu.Unlock()
			return
		}
		ev := s.queue[0]
		s.queue = s.queue[1:]
		if ev.Kind == domain.ProgressInvocation {
			s.invocationCount--
		}
		s.mu.Unlock()

		deliverOne(s.inner, ev)
	}
}

func deliverOne(inner domain.ProgressSink, ev domain.ProgressEvent) {
	defer func() { recover() }()
	inner.Emit(ev)
}

// MultiSink fans one stream out to several consumers. Used by the frontends
// when a run must both stream lines and feed a model. A panic from one
// inner sink never stops delivery to the others.
func MultiSink(sinks ...domain.ProgressSink) domain.ProgressSink {
	return multiSink{sinks: sinks}
}

type multiSink struct {
	sinks []domain.ProgressSink
}

func (m multiSink) Emit(ev domain.ProgressEvent) {
	for _, s := range m.sinks {
		if s == nil {
			continue
		}
		deliverOne(s, ev)
	}
}

// DiscardSink is the no-op sink, for a run whose progress nobody watches.
func DiscardSink() domain.ProgressSink {
	return discardSink{}
}

type discardSink struct{}

func (discardSink) Emit(domain.ProgressEvent) {}
