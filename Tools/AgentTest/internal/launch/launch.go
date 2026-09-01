// Package launch is the harness-neutral executor of a domain.SpawnPlan.
//
// It performs process control through mosaic-common/harness and nothing
// else: no argument construction, no envelope parsing and no executable
// resolution is written in this package. Envelope knowledge stays in
// whichever adapter package supplied the Decoder function value at
// construction — this package names no harness and imports no concrete
// adapter, which is what lets the per-test runner depend on
// domain.SubjectLauncher alone and still name no harness itself.
package launch

import (
	"context"
	"errors"
	"io"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
)

// Decoder maps a finished invocation onto the subject's result. Supplied by
// the composition root from the selected adapter's package (for example
// claudecode.DecodeEnvelope), and held here only as a plain function value.
type Decoder func(res commonharness.Response, err error) (domain.SubjectResult, error)

// Option configures a Launcher constructed by New.
type Option func(*config)

// ResolveExecutableFunc is the signature of a function that resolves an
// executable path to a Command, matching commonharness.ResolveExecutable.
// The field exists as a seam for testing; production code uses the default
// (commonharness.ResolveExecutable).
type ResolveExecutableFunc func(path string) (commonharness.Command, error)

// SinkFactory produces the diagnostic sink one spawn plan's process output is
// logged through, plus a closer the launcher always calls before Launch
// returns.
//
// It exists because the sink is a per-run resource and the launcher is a
// process-wide singleton: binding one sink at construction, as the launcher
// previously did, is what made N concurrent runs' output arrive interleaved
// and unattributed on one destination.
//
// A factory that returns an error causes Launch to proceed with the no-op
// sink rather than failing the run: losing diagnostics is a degradation, not
// a test result.
type SinkFactory func(plan domain.SpawnPlan) (commonharness.Sink, io.Closer, error)

// noopSink is the internal no-op sink used when no factory is configured or
// when a factory returns an error. It discards all events silently.
type noopSink struct{}

func (noopSink) Log(_ commonharness.Event) {}

// noopCloser is a trivial io.Closer that does nothing.
type noopCloser struct{}

func (noopCloser) Close() error { return nil }

type config struct {
	sink        commonharness.Sink
	sinkFactory SinkFactory
	resolveExe  ResolveExecutableFunc
}

// WithSinkFactory sets how the launcher obtains a diagnostic sink per plan.
//
// No factory this package supplies can yield the process's own standard
// output or standard error — the capability is absent by construction rather
// than conditioned on an output mode, which is what makes "no mode routes
// subject output to a terminal the TUI owns" structural.
func WithSinkFactory(f SinkFactory) Option {
	return func(c *config) { c.sinkFactory = f }
}

// WithSink sets a single sink used for every plan, ignoring
// plan.DiagnosticLog. Retained for tests that assert on captured events; the
// composition root uses WithSinkFactory.
func WithSink(s commonharness.Sink) Option {
	return func(c *config) { c.sink = s }
}

// WithResolveExecutable overrides the executable resolver used by Launch.
// The default is commonharness.ResolveExecutable. This seam exists so tests
// can inject a failing resolver to verify DispositionSpawnFailed handling
// without relying on OS-level executable resolution behavior.
func WithResolveExecutable(fn ResolveExecutableFunc) Option {
	return func(c *config) { c.resolveExe = fn }
}

// Launcher implements domain.SubjectLauncher.
type Launcher struct {
	dec Decoder
	cfg config
}

var _ domain.SubjectLauncher = (*Launcher)(nil)

// New returns a launcher that executes a plan through
// mosaic-common/harness's plan-execution entry point and hands the outcome
// to dec.
func New(dec Decoder, opts ...Option) domain.SubjectLauncher {
	cfg := config{resolveExe: commonharness.ResolveExecutable}
	for _, o := range opts {
		o(&cfg)
	}
	return &Launcher{dec: dec, cfg: cfg}
}

// Launch implements domain.SubjectLauncher.
//
// It performs process control through mosaic-common/harness.Run alone: no
// argument construction or envelope parsing happens in this package. Executable
// resolution is delegated to the shared package's ResolveExecutable (or, in
// tests, the injected resolver supplied via WithResolveExecutable). Once the
// run finishes (or fails to start), the result and error mosaic-common/harness
// produced are handed to the decoder supplied at construction, unmodified.
//
// Launch returns a domain.SubjectResult on every path on which the subject
// actually started, including a run the caller cancelled. It returns an
// error only when the subject could not be started at all — signalled by
// mosaic-common/harness.ErrExecutableNotFound or by the resolver — and then
// the returned result carries domain.DispositionSpawnFailed rather than being
// zero-valued, because a zero value would read downstream as a subject that
// completed silently.
//
// When a SinkFactory was set via WithSinkFactory, Launch calls it once per
// plan to obtain the per-run diagnostic sink and its closer, and calls the
// closer before returning. The closer is always called, even when Launch
// returns an error, so no file handle survives into sandbox teardown. A
// factory that returns an error causes Launch to proceed with the no-op sink
// rather than failing the run: losing diagnostics is a degradation, not a
// test result.
func (l *Launcher) Launch(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
	cmd, resolveErr := l.cfg.resolveExe(plan.Executable)
	if resolveErr != nil {
		return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}, resolveErr
	}

	// Resolve the sink for this plan. When a factory is configured, obtain a
	// per-plan sink and guarantee its closer runs before Launch returns.
	// When no factory is configured, fall back to the fixed sink (which tests
	// supply via WithSink) or the no-op sink.
	var sink commonharness.Sink
	var closer io.Closer

	if l.cfg.sinkFactory != nil {
		var factoryErr error
		sink, closer, factoryErr = l.cfg.sinkFactory(plan)
		if factoryErr != nil || sink == nil {
			// Factory failure is a degradation: proceed with no-op sink.
			sink = noopSink{}
			closer = noopCloser{}
		}
		defer func() {
			if closer != nil {
				_ = closer.Close()
			}
		}()
	} else {
		sink = l.cfg.sink
	}

	resp, runErr := commonharness.Run(ctx, cmd, plan.Args, commonharness.RunOptions{
		WorkingDir: plan.WorkingDir,
		Env:        plan.Env,
		Stdin:      plan.Stdin,
		Timeout:    plan.Timeout,
		Sink:       sink,
	})

	result, decErr := l.dec(resp, runErr)
	if decErr != nil {
		return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}, decErr
	}

	if runErr != nil && errors.Is(runErr, commonharness.ErrExecutableNotFound) {
		// The subject never started: propagate the failure alongside the
		// decoder's spawn-failed result rather than only the result, so a
		// caller checking err alone still sees the failure.
		return result, runErr
	}

	return result, nil
}
