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

type config struct {
	sink            commonharness.Sink
	resolveExe      ResolveExecutableFunc
}

// WithSink sets the diagnostic sink process output is logged through.
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
func (l *Launcher) Launch(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
	cmd, resolveErr := l.cfg.resolveExe(plan.Executable)
	if resolveErr != nil {
		return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}, resolveErr
	}

	resp, runErr := commonharness.Run(ctx, cmd, plan.Args, commonharness.RunOptions{
		WorkingDir: plan.WorkingDir,
		Env:        plan.Env,
		Stdin:      plan.Stdin,
		Timeout:    plan.Timeout,
		Sink:       l.cfg.sink,
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
