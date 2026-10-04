package main

// mosaiclog_wiring.go holds the composition-root pieces that give the Runner
// its own MOSAIC log events: the run-lifecycle session decorator, the OpenCode
// invocation_end safety-net adapter decorator and the signal-aware CLI context.
//
// Logging is strictly optional. Nothing here changes what a session or an
// adapter returns; every write failure is swallowed by the mosaiclog writer.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/mosaiclog"
	"mosaic-run/internal/session"
)

// runLogConfig is the per-session input of the run-lifecycle decorator.
type runLogConfig struct {
	RunFolder string             // run folder the session's store is built at
	HarnessID string             // selected harness id
	Debug     domain.DebugLogger // where swallowed write failures are recorded
	Clock     domain.Clock       // timestamp source
}

// runIDReporter is the narrow assertion target for the run id an adapter was bound to.
type runIDReporter interface {
	RunID() string
}

// openCodeHarness is the closed capability set of the OpenCode adapter that the
// safety net decorates and re-exposes. The wrapped *opencode.OpenCodeAdapter
// exports exactly Invoke, InvokeRaw, ExecutablePath and RunID; the decorator
// forwards all four so no optional capability discovered by type assertion at
// the composition root is lost.
type openCodeHarness interface {
	domain.HarnessAdapter
	domain.RawInvoker
	domain.ExecutableRevealer
	runIDReporter
}

// runLifecycleSession writes one run_start before and one run_end after every
// Start call of the session it wraps.
type runLifecycleSession struct {
	inner session.Session
	cfg   runLogConfig
}

// newRunLifecycleSession returns the OUTERMOST session wrapper: any other
// session-level wrapper sits inside it, so every Start call yields exactly one
// run_start/run_end pair. It treats inner as opaque. The log lives under the
// parent directory of cfg.RunFolder; when the run folder is not an
// Orchestration-{run_id} folder or the harness is unknown the writer writes
// nothing and the decorator is a pure pass-through.
func newRunLifecycleSession(inner session.Session, cfg runLogConfig) session.Session {
	return &runLifecycleSession{inner: inner, cfg: cfg}
}

func (s *runLifecycleSession) Start(ctx context.Context, cfg domain.RunConfig) (domain.RunOutcome, error) {
	root := filepath.Dir(s.cfg.RunFolder)
	runID, _ := domain.ParseRunFolder(filepath.Base(s.cfg.RunFolder))
	w := mosaiclog.NewWriter(root, s.cfg.HarnessID,
		mosaiclog.WithClock(s.cfg.Clock), mosaiclog.WithDebugLogger(s.cfg.Debug))

	w.RunStart(runID, root)
	out, err := s.inner.Start(ctx, cfg)
	w.RunEnd(runID, mosaiclog.OutcomeForRun(out, err, ctx.Err()))
	return out, err
}

// openCodeSafetyNet closes an OpenCode invocation's event log when the
// mosaic-logger plugin did not.
//
// REMOVAL CONDITION: this net exists only because the OpenCode plugin has not
// yet been shown to write invocation_end reliably. Drop it (and its call in
// buildAdapter) once live Runner runs on OpenCode show the net never fires,
// i.e. every invocation folder already holds the plugin's invocation_end.
type openCodeSafetyNet struct {
	inner  openCodeHarness
	writer *mosaiclog.Writer
}

// newOpenCodeSafetyNet decorates the OpenCode adapter. Invoke is followed by
// the invocation_end check; InvokeRaw, ExecutablePath and RunID are forwarded
// unchanged.
func newOpenCodeSafetyNet(inner openCodeHarness, writer *mosaiclog.Writer) openCodeHarness {
	return &openCodeSafetyNet{inner: inner, writer: writer}
}

// Invoke runs the wrapped invocation and, only after its process has exited,
// appends an invocation_end when the invocation folder exists without one. The
// result and error are returned exactly as the wrapped adapter produced them.
func (n *openCodeSafetyNet) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	resp, err := n.inner.Invoke(ctx, agent, request)

	runID := request.RunID
	if runID == "" {
		runID = n.inner.RunID()
	}
	end := mosaiclog.InvocationEnd{AgentInstanceID: request.AgentInstanceID}
	if err == nil {
		end.StatusCode = string(resp.StatusCode)
		end.Response = protocolResponseText(resp)
	}
	n.writer.EnsureInvocationEnd(runID, end)
	return resp, err
}

// protocolResponseText is the text recorded as invocation_end.response: the
// protocol response the agent returned, serialised as JSON (what native
// OpenCode records as the agent's final assistant text).
func protocolResponseText(resp domain.ProtocolResponse) string {
	b, err := json.Marshal(resp)
	if err != nil {
		return ""
	}
	return string(b)
}

func (n *openCodeSafetyNet) InvokeRaw(ctx context.Context, agent domain.AgentReference, payload []byte) ([]byte, error) {
	return n.inner.InvokeRaw(ctx, agent, payload)
}

func (n *openCodeSafetyNet) ExecutablePath() string { return n.inner.ExecutablePath() }

func (n *openCodeSafetyNet) RunID() string { return n.inner.RunID() }

// cliInterrupt is the seam between the CLI composition path and OS signals.
type cliInterrupt struct {
	Notify func(c chan<- os.Signal, sig ...os.Signal)
	Stop   func(c chan<- os.Signal)
}

// osInterrupt is the production seam: real OS signal registration.
var osInterrupt = cliInterrupt{Notify: signal.Notify, Stop: signal.Stop}

// newCLIRunContext derives the context handed to cli.Run. The first interrupt
// cancels it and immediately restores default signal handling, so a second
// Ctrl+C terminates the process. release cancels the context, stops signal
// notification and is safe to call more than once.
func newCLIRunContext(parent context.Context, in cliInterrupt) (ctx context.Context, release func()) {
	ctx, cancel := context.WithCancel(parent)
	sigs := make(chan os.Signal, 1)
	in.Notify(sigs, os.Interrupt)

	var once sync.Once
	stop := func() { once.Do(func() { in.Stop(sigs) }) }

	go func() {
		select {
		case <-sigs:
			cancel()
			stop()
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		cancel()
		stop()
	}
}

// resolveSessionRunFolder returns the run folder a TUI session will be built at.
// A resolved runFolder is returned as is. An unresolved one (a contract
// violation by the caller) is replaced by a freshly minted run folder, exactly
// once, so the session's artifact store and its run log refer to the same run.
func resolveSessionRunFolder(runFolder string, in interactiveWiringInput) string {
	if _, err := resolveTUIArtifactPath(runFolder); !errors.Is(err, errUnresolvedRunFolder) {
		return runFolder
	}
	_, mintedFolder := in.Minter()
	in.Debug.Log(domain.EventRunnerError, "run folder unresolved; minting new run",
		domain.F("path", mintedFolder))
	fmt.Fprintf(os.Stderr, "notice: run folder unresolved; minting new run at %s\n", mintedFolder)
	return mintedFolder
}

// withOpenCodeSafetyNet applies the invocation_end safety net to the OpenCode
// adapter when runFolder is an Orchestration-{run_id} folder; otherwise the
// adapter is returned undecorated (there is no workspace to log under). The
// log root is the run folder's parent directory, as for the run lifecycle.
func withOpenCodeSafetyNet(adapter openCodeHarness, runFolder string, logger domain.DebugLogger) domain.HarnessAdapter {
	if _, ok := domain.ParseRunFolder(filepath.Base(runFolder)); !ok {
		return adapter
	}
	w := mosaiclog.NewWriter(filepath.Dir(runFolder), commonharness.HarnessIDOpenCode,
		mosaiclog.WithClock(&realClock{}), mosaiclog.WithDebugLogger(logger))
	return newOpenCodeSafetyNet(adapter, w)
}
