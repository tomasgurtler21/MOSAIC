package main

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

const mlWait = 2 * time.Second

// mlInterruptSeam is a fake cliInterrupt that captures the registered channel.
type mlInterruptSeam struct {
	mu      sync.Mutex
	ch      chan<- os.Signal
	sigs    []os.Signal
	stopped []chan<- os.Signal
}

func (s *mlInterruptSeam) seam() cliInterrupt {
	return cliInterrupt{
		Notify: func(c chan<- os.Signal, sig ...os.Signal) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.ch, s.sigs = c, sig
		},
		Stop: func(c chan<- os.Signal) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.stopped = append(s.stopped, c)
		},
	}
}

func (s *mlInterruptSeam) stopCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.stopped)
}

// interrupt delivers one os.Interrupt through the registered channel.
func (s *mlInterruptSeam) interrupt(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch == nil {
		t.Fatal("no channel registered through Notify")
	}
	select {
	case ch <- os.Interrupt:
	case <-time.After(mlWait):
		t.Fatal("registered channel did not accept the signal")
	}
}

func mlWaitDone(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(mlWait):
		t.Fatal("context was not cancelled")
	}
}

func TestCLIRunContext_RegistersForInterruptAndStartsLive(t *testing.T) {
	seam := &mlInterruptSeam{}

	ctx, release := newCLIRunContext(context.Background(), seam.seam())
	defer release()

	if ctx.Err() != nil {
		t.Fatalf("context already done: %v", ctx.Err())
	}
	found := false
	for _, s := range seam.sigs {
		if s == os.Interrupt {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered signals = %v, want os.Interrupt among them", seam.sigs)
	}
}

func TestCLIRunContext_FirstInterruptCancelsAndRestoresDefaultHandling(t *testing.T) {
	seam := &mlInterruptSeam{}
	ctx, release := newCLIRunContext(context.Background(), seam.seam())
	defer release()

	seam.interrupt(t)

	mlWaitDone(t, ctx)
	deadline := time.Now().Add(mlWait)
	for seam.stopCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if seam.stopCount() == 0 {
		t.Fatal("Stop was not called after the first interrupt: a second Ctrl+C would not terminate the process")
	}
	seam.mu.Lock()
	defer seam.mu.Unlock()
	if seam.stopped[0] != seam.ch {
		t.Error("Stop was given a different channel than Notify registered")
	}
}

func TestCLIRunContext_ParentCancellationPropagates(t *testing.T) {
	seam := &mlInterruptSeam{}
	parent, cancel := context.WithCancel(context.Background())
	ctx, release := newCLIRunContext(parent, seam.seam())
	defer release()

	cancel()

	mlWaitDone(t, ctx)
}

func TestCLIRunContext_ReleaseCancelsStopsNotificationAndIsIdempotent(t *testing.T) {
	seam := &mlInterruptSeam{}
	ctx, release := newCLIRunContext(context.Background(), seam.seam())

	release()
	release()

	mlWaitDone(t, ctx)
	if seam.stopCount() == 0 {
		t.Fatal("release did not stop signal notification")
	}
}

func TestCLIRunContext_InterruptedRunYieldsExactlyOneInterruptedRunEnd(t *testing.T) {
	seam := &mlInterruptSeam{}
	runFolder, ws := mlRunFolder(t)
	ctx, release := newCLIRunContext(context.Background(), seam.seam())
	defer release()
	started := make(chan struct{})
	inner := &mlSession{start: func(c context.Context, _ domain.RunConfig) (domain.RunOutcome, error) {
		close(started)
		<-c.Done()
		return domain.RunOutcome{}, c.Err()
	}}
	sess := newRunLifecycleSession(inner, mlLogConfig(runFolder, "claude-code"))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = sess.Start(ctx, domain.RunConfig{})
	}()
	<-started

	seam.interrupt(t)

	select {
	case <-done:
	case <-time.After(mlWait):
		t.Fatal("the session did not return after the interrupt")
	}
	events := mlReadEvents(t, mlRunLogPath(ws))
	mlAssertPairs(t, events, 1)
	if events[1]["outcome"] != "interrupted" {
		t.Fatalf("outcome = %v, want interrupted", events[1]["outcome"])
	}
}
