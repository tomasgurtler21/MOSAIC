// Package interceptor is the imperative shell around the pure decision core
// (internal/intercept): the short-lived process a harness's interception
// point reaches once per intercepted call.
//
// All decision-making stays in internal/intercept — this package sequences
// I/O and nothing else, so any conditional here that looks like policy
// belongs upstream. This process must never damage the run it is
// intercepting: whatever goes wrong — absent state, corrupt state, a lock it
// cannot acquire, a payload it cannot parse, a side effect it cannot write,
// or a panic anywhere inside — it emits a valid native reply and lets the
// subject continue rather than crashing or hanging the subject's turn. A
// test tool that breaks the thing it is measuring produces evidence about
// itself.
package interceptor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
	"mosaic-agent-test/internal/invlog"
	"mosaic-agent-test/internal/runstate"
	"mosaic-agent-test/internal/sideeffects"
)

// FlushDelay is a deliberate pause before Run returns, so the reply written
// to Config.Out is fully flushed before the process embedding this shell
// exits. The first-generation implementation lost completion records
// intermittently without it (see Research.md); the value is carried forward
// from that implementation rather than rediscovered.
const FlushDelay = 75 * time.Millisecond

// Config is everything the shell needs. All of it arrives explicitly;
// nothing is inferred from the process working directory, because the
// harness may launch this process from anywhere.
type Config struct {
	ControlDir string
	SubjectDir string
	Phase      domain.InterceptionPhase

	Adapter domain.HarnessAdapter
	State   runstate.Store
	Log     invlog.Log
	Effects sideeffects.Applier
	Clock   domain.Clock

	// Registry and Groups are read from the control directory by the caller
	// and passed in, so the shell's sequencing is testable without a
	// filesystem behind them.
	Registry domain.StubRegistry
	Groups   []domain.ParallelGroup

	TestID    string
	RunNumber int

	// RunID is the run's identifier, read back from the persisted run state
	// by the caller. Empty when the read did not succeed — a legal state that
	// leaves placeholder expansion yielding an empty segment rather than
	// failing the interception.
	RunID string

	In   io.Reader
	Out  io.Writer
	Diag io.Writer
}

// Run performs one interception and always answers.
//
// Ordering: the native payload is read and translated outside any lock;
// the early-exit sentinel is checked next, also without a lock — a halted
// call needs no state; state is then read, decided (via intercept.Decide)
// and committed atomically as a single lock-guarded read-modify-write, with
// nothing else done under that lock; side effects and log records are
// applied and appended after the lock is released but before the reply is
// written, so a subject acting immediately on the reply cannot outrun
// either; the reply is written and flushed last.
//
// It returns no error and always exits zero. That is a contract, not
// laziness: a non-zero exit or a missing reply from this process is
// interpreted by the harness as a failed hook, which damages the run this
// tool is measuring. A test tool that breaks the thing it measures produces
// evidence about itself.
//
// Every failure — an unparseable payload, absent state, corrupt state,
// unreadable state, a lock it could not acquire, a side effect it could not
// write, or a panic anywhere inside — converges on the same outermost
// boundary: emit the neutral native reply, write a diagnostic to Config.Diag,
// append an error record when the log is reachable, and return. Containment
// is structural, so a failure mode added later cannot bypass it.
//
// Lock reclamation: runstate.Store.Update reports reclamation to its caller
// and does not log it, because runstate may not import invlog. Run is that
// caller: on UpdateResult.LockReclaimed it appends a RecordRun record with
// RunEventLockReclaimed, naming the prior holder.
//
// Early exit: when intercept.Decide returns OutcomeHalt with HaltEarlyExit,
// Run writes the sentinel file the driver's supervisor watches, and appends
// a RunEventEarlyExitTriggered record. Later calls in the same sandbox halt
// on entry.
func Run(ctx context.Context, cfg Config) int {
	// FlushDelay runs after everything else, including the deferred panic
	// recovery below, so the reply is fully flushed before this process (or
	// this call, inside a test) returns.
	defer time.Sleep(FlushDelay)
	runOneInterception(ctx, cfg)
	// Always zero: a non-zero exit or a missing reply is read by the harness
	// as a failed hook, which damages the run this tool is measuring.
	// Everything that can go wrong converges on handleFailure instead.
	return 0
}

// runOneInterception drives the pre- or post-invocation sequence described
// on Config, containing every failure — including a panic anywhere in this
// function or anything it calls — behind a single outermost boundary.
func runOneInterception(ctx context.Context, cfg Config) {
	var call domain.InterceptedCall
	var haveCall bool

	defer func() {
		if r := recover(); r != nil {
			var callPtr *domain.InterceptedCall
			if haveCall {
				callPtr = &call
			}
			handleFailure(cfg, callPtr, fmt.Errorf("unexpected panic: %v", r))
		}
	}()

	native, err := io.ReadAll(cfg.In)
	if err != nil {
		handleFailure(cfg, nil, fmt.Errorf("reading native payload: %w", err))
		return
	}

	translated, err := cfg.Adapter.TranslateCall(cfg.Phase, native)
	if err != nil {
		handleFailure(cfg, nil, fmt.Errorf("translating native payload: %w", err))
		return
	}
	call = translated
	haveCall = true

	// Halt-on-entry: a file-existence test only, so a halted call needs no
	// state and this shell never reaches runstate for it.
	if sentinelPresent(cfg.ControlDir) {
		outcome := domain.InterceptionOutcome{
			Kind:             domain.OutcomeHalt,
			HaltReason:       domain.HaltEarlyExit,
			CorrelationToken: call.CorrelationToken,
			Message:          "This operation cannot proceed further in this run.",
		}
		reply, err := cfg.Adapter.TranslateOutcome(outcome, call)
		if err != nil {
			handleFailure(cfg, &call, fmt.Errorf("translating halt-on-entry outcome: %w", err))
			return
		}
		_, _ = cfg.Out.Write(reply)
		return
	}

	// The state read, the decision and the commit happen inside one
	// lock-guarded read-modify-write. Nothing else — no side effects, no log
	// append — happens inside the lock: it sits on the critical path of a
	// live agent turn.
	var decision intercept.Decision
	updateResult, err := cfg.State.Update(func(current domain.RunState) (domain.RunState, error) {
		d, decideErr := intercept.Decide(intercept.Input{
			Call:     call,
			State:    current,
			Registry: cfg.Registry,
			Groups:   cfg.Groups,
			Now:      cfg.Clock.Now(),
			RunID:    cfg.RunID,
		})
		if decideErr != nil {
			return domain.RunState{}, decideErr
		}
		decision = d
		return current.Apply(d.Delta), nil
	})
	if err != nil {
		handleFailure(cfg, &call, fmt.Errorf("updating run state: %w", err))
		return
	}

	records := append([]domain.LogRecord{}, decision.Records...)

	// runstate.Store.Update reports reclamation but cannot log it itself
	// (runstate may not import invlog); surfacing it is this shell's
	// obligation, because swallowing it would turn a lost state update into
	// a silently wrong verdict.
	if updateResult.LockReclaimed {
		records = append(records, domain.LogRecord{
			Kind:      domain.RecordRun,
			TestID:    cfg.TestID,
			RunNumber: cfg.RunNumber,
			Timestamp: cfg.Clock.Now(),
			Event:     domain.RunEventLockReclaimed,
			Detail: fmt.Sprintf(
				"lock reclaimed from prior holder pid=%d host=%q acquired_at=%s",
				updateResult.PriorHolder.PID, updateResult.PriorHolder.Host,
				updateResult.PriorHolder.AcquiredAt.Format(time.RFC3339Nano)),
		})
	}

	// Side effects and log records are applied after the lock releases but
	// before the reply is emitted, so a subject acting immediately on the
	// reply cannot outrun either.
	if len(decision.SideEffects) > 0 {
		// Expand the run-ID placeholder in every effect's path before Apply
		// is called. This mirrors seedFile's expand-then-write shape and
		// ensures the escape guard inside Apply evaluates the post-expansion
		// path. Content expansion (both inline and $ref-resolved) happens
		// inside Apply after $ref resolution.
		expanded := make([]domain.FileEffect, len(decision.SideEffects))
		for i, e := range decision.SideEffects {
			e.Path = strings.ReplaceAll(e.Path, domain.RunIDPlaceholder, cfg.RunID)
			expanded[i] = e
		}
		if _, err := cfg.Effects.Apply(cfg.SubjectDir, expanded, cfg.RunID); err != nil {
			handleFailure(cfg, &call, fmt.Errorf("applying side effects: %w", err))
			return
		}
	}

	if len(records) > 0 {
		if err := cfg.Log.Append(records...); err != nil {
			handleFailure(cfg, &call, fmt.Errorf("appending log records: %w", err))
			return
		}
	}

	reply, err := cfg.Adapter.TranslateOutcome(decision.Outcome, call)
	if err != nil {
		handleFailure(cfg, &call, fmt.Errorf("translating outcome: %w", err))
		return
	}
	_, _ = cfg.Out.Write(reply)

	// Early exit: the sentinel the driver's supervisor watches for. Written
	// after the reply so the supervisor cannot observe the sentinel and cancel
	// the subject's context before the Nth reply reaches the subject.
	if decision.TerminateSubject {
		if err := writeSentinel(cfg.ControlDir); err != nil {
			// The reply was already delivered; do not call handleFailure
			// (which would write a second neutral reply). Log the failure
			// and append an error record so the log reflects the fault.
			if cfg.Diag != nil {
				fmt.Fprintf(cfg.Diag, "interceptor: writing early-exit sentinel: %v\n", err)
			}
			appendDiagnosticRecord(cfg, call, fmt.Errorf("writing early-exit sentinel: %w", err))
		}
	}
}

// sentinelPresent reports whether the early-exit sentinel has been written
// into controlDir by a previous interception in this sandbox.
func sentinelPresent(controlDir string) bool {
	_, err := os.Stat(filepath.Join(controlDir, domain.EarlyExitSentinelName))
	return err == nil
}

// writeSentinel writes the early-exit sentinel the driver's supervisor
// watches for.
func writeSentinel(controlDir string) error {
	if err := os.MkdirAll(controlDir, 0o755); err != nil {
		return fmt.Errorf("interceptor: creating control dir %s: %w", controlDir, err)
	}
	path := filepath.Join(controlDir, domain.EarlyExitSentinelName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return fmt.Errorf("interceptor: writing sentinel %s: %w", path, err)
	}
	return nil
}

// handleFailure is the single outermost boundary every failure path
// converges on: it writes a diagnostic to Config.Diag, emits a best-effort
// neutral native reply so the harness never sees a missing or malformed
// answer, and appends an error record to the log when the log is reachable.
// callOrNil is nil when the failure happened before translation succeeded,
// so no correlation token or identity is available yet.
func handleFailure(cfg Config, callOrNil *domain.InterceptedCall, cause error) {
	if cfg.Diag != nil {
		fmt.Fprintf(cfg.Diag, "interceptor: %v\n", cause)
	}

	var call domain.InterceptedCall
	if callOrNil != nil {
		call = *callOrNil
	}

	if cfg.Out != nil {
		reply := neutralReply(cfg, call)
		_, _ = cfg.Out.Write(reply)
	}

	appendDiagnosticRecord(cfg, call, cause)
}

// neutralReply asks the adapter to translate a passthrough outcome — the
// reply that changes nothing — and falls back to a minimal, always-decodable
// JSON object if the adapter itself cannot be trusted to answer (it errored,
// or it is the panicking adapter this package's own tests plant).
func neutralReply(cfg Config, call domain.InterceptedCall) (reply []byte) {
	defer func() {
		if r := recover(); r != nil {
			reply = []byte(`{"kind":"passthrough"}`)
		}
	}()

	if cfg.Adapter == nil {
		return []byte(`{"kind":"passthrough"}`)
	}

	outcome := domain.InterceptionOutcome{
		Kind:             domain.OutcomePassthrough,
		CorrelationToken: call.CorrelationToken,
	}
	b, err := cfg.Adapter.TranslateOutcome(outcome, call)
	if err != nil || len(b) == 0 {
		return []byte(`{"kind":"passthrough"}`)
	}
	return b
}

// appendDiagnosticRecord appends an error record describing cause, so a
// contained failure still leaves evidence in the invocation log. Best
// effort: a log that is itself unreachable must not turn a contained
// failure into a crash.
func appendDiagnosticRecord(cfg Config, call domain.InterceptedCall, cause error) {
	defer func() { recover() }()

	if cfg.Log == nil {
		return
	}

	now := time.Now()
	if cfg.Clock != nil {
		now = cfg.Clock.Now()
	}

	rec := domain.LogRecord{
		Kind:             domain.RecordError,
		TestID:           cfg.TestID,
		RunNumber:        cfg.RunNumber,
		Timestamp:        now,
		Identity:         call.Identity,
		CorrelationToken: call.CorrelationToken,
		Detail:           cause.Error(),
	}
	_ = cfg.Log.Append(rec)
}
