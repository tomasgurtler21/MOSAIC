package e2e

// The scripted stand-in subject.
//
// internal/harness/fake.Adapter.SpawnPlan names an executable
// ("fake-subject") that mosaic-common/harness.Run spawns as a real OS
// process — that is what "no special case inside the pipeline" buys: the
// runner, the launcher and the interceptor shell all run exactly as they do
// for a real harness, with only the harness adapter and the process at the
// far end of the spawn being scripted.
//
// This test binary doubles as that stand-in, exactly the way
// internal/launch's own tests double as a fake harness CLI
// (GO_WANT_HELPER_PROCESS): when standinEnv is set, TestMain dispatches into
// runStandin instead of the ordinary test runner, and RunScenario arranges
// for a copy of this compiled binary to be resolvable on PATH under the
// literal name "fake-subject" the adapter hard-codes.
//
// What the stand-in does once running, and why it needs a second channel of
// information beyond the SpawnPlan's Stdin:
//
// fake.Options.SubjectTurns (encoded into Stdin by the adapter) says only
// which collaborator identity the subject invokes next, or that it is
// finished — enough to drive sequencing, but not enough to give an invoked
// call a task message worth asserting on, and not enough to reconstruct
// fake.Options.Script (the collaborator responses that drive echo fidelity)
// across the process boundary a real subprocess spawn creates. Both are
// supplied out of band, over an environment variable naming a JSON sidecar
// file (standinScript, below) that RunScenario writes before the run starts.
// This is test-only wiring invented for this suite; it changes no
// production contract and is invisible to everything under test.
//
// For each invocation the script names, the stand-in performs the identical
// pre/post interception sequence a real harness's hooks would trigger,
// through the real internal/interceptor shell, against a fresh
// internal/harness/fake.Adapter reconstructed from the sidecar — the same
// package, and the same conformance-suite-passing translation logic, that
// governs every other scripted run in this module.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/harness/fake"
	"mosaic-agent-test/internal/interceptor"
	"mosaic-agent-test/internal/invlog"
	"mosaic-agent-test/internal/runstate"
	"mosaic-agent-test/internal/sideeffects"
)

// standinEnv, when "1", tells this compiled test binary to behave as the
// scripted stand-in subject instead of running its own tests.
const standinEnv = "MOSAIC_E2E_STANDIN"

// scriptEnvVar names the environment variable carrying the sidecar script
// file's path.
const scriptEnvVar = "MOSAIC_E2E_SCRIPT_FILE"

// probeEnvVar names the environment variable carrying the path the stand-in
// writes its side-effect-timing probe results to, when Scenario.ProbeFiles
// is non-empty. See standinScript.ProbeFiles.
const probeEnvVar = "MOSAIC_E2E_PROBE_FILE"

func TestMain(m *testing.M) {
	if os.Getenv(standinEnv) == "1" {
		os.Exit(runStandin())
	}
	os.Exit(m.Run())
}

// standinScript is the sidecar this package's RunScenario writes and the
// stand-in reads back, carrying everything a real fake.Options carries that
// SpawnPlan's Stdin does not, plus the per-invocation task message content a
// task-message assertion needs.
type standinScript struct {
	Capabilities domain.HarnessCapabilities `json:"capabilities"`
	// CollaboratorTurns mirrors fake.Options.Script: keyed by
	// domain.CollaboratorIdentity.Key(), consumed in order.
	CollaboratorTurns map[string][]sidecarTurn `json:"collaborator_turns"`
	// InvocationDetails supplies the task message content for each Invoke
	// turn, keyed and consumed the same way.
	InvocationDetails map[string][]sidecarDetail `json:"invocation_details"`
	// SleepBeforeExit, when non-zero, blocks runStandin before it processes
	// any turn — this suite's own way of making a scripted subject genuinely
	// never reach a Result turn within a declared timeout, so the parent
	// process's context cancellation (which kills this stand-in's own OS
	// process, see mosaic-common/harness.Run) is what actually ends it,
	// exactly as a real hung subject would be ended. A JSON-safe duration
	// string (time.ParseDuration syntax) rather than a raw number, matching
	// how the authored suite's own "timeout:" field is spelled.
	SleepBeforeExit string `json:"sleep_before_exit,omitempty"`
	// SeedDeadLock, when true, makes runStandin overwrite the run-state lock
	// runner.setup already initialized with one naming a PID that cannot be
	// alive, before performing any interception — reproducing exactly the
	// fixture internal/runstate's own lock_test.go uses
	// (TestUpdate_ReclaimsALockHeldByAVerifiablyDeadHolder) but through the
	// real end-to-end pipeline: the first Update call any interception makes
	// reclaims it immediately, no wall-clock wait needed, and records
	// domain.RunEventLockReclaimed the same way genuine contention would.
	// This is test-only wiring — no production code simulates a lock holder
	// dying, because runner.setup always initializes state honestly; this
	// field's whole job is to make the sandbox look, at the moment the
	// subject starts, exactly like one a crashed prior holder left behind.
	SeedDeadLock bool `json:"seed_dead_lock,omitempty"`
	// SeedDeadLockOnce seeds the dead lock on the first spawn only. A marker
	// file is written beside the script path (scriptEnvVar) after the first
	// seed; subsequent spawns that find the marker skip seeding. This lets
	// the retry run clean and demonstrate that a recovered repetition reports
	// one excluded attempt, one counted attempt and aggregate PASS.
	//
	// The marker persists across spawns because the script path lives in a
	// per-test temp directory that survives for the duration of the Go test,
	// while each spawn gets a fresh sandbox — the same script path is shared
	// across attempts even as sandboxes are isolated.
	SeedDeadLockOnce bool `json:"seed_dead_lock_once,omitempty"`
	// ProbeFiles, when non-empty, are paths (relative to the sandbox's
	// subject directory) runStandin checks for existence right after a
	// batch's pre-invocation interception phase returns — before the
	// collaborator's own reply is delivered, since that reply is what a
	// subject "acts on" next — and reports back over probeEnvVar. This is
	// the only way this suite can observe a declared side effect's timing
	// rather than only its post-run presence, because the check has to
	// happen from inside the stand-in's own process: the parent test process
	// never runs concurrently with the sandbox the run is using.
	ProbeFiles []string `json:"probe_files,omitempty"`
	// SkipInvokeOnRuns names run numbers (matching domain.RunKey.RunNumber,
	// read back via readTestIdentity) on which runStandin treats every
	// Invoke turn in the script as a no-op: no interception call is made, no
	// stub is matched, no side effect fires — the scripted subject simply
	// "decides" not to dispatch a collaborator on that run. This lets one
	// fixed subject-turn script (arriving via SpawnPlan.Stdin, identical for
	// every repetition of one test) still produce different observable
	// outcomes across repetitions, without a second divergent script.
	SkipInvokeOnRuns []int `json:"skip_invoke_on_runs,omitempty"`
	// SkipInvokeForTestIDs names test ids (matching domain.RunKey.TestName,
	// read back via readTestIdentity) on which runStandin treats every
	// Invoke turn in the script as a no-op, the same way SkipInvokeOnRuns
	// does for a run number. This is what lets one suite's several tests
	// share a single Scenario.Script (SpawnPlan.Stdin is replayed identically
	// from its start on every fresh stand-in process, one per run,
	// regardless of which test that run belongs to) while still letting one
	// test's own subject genuinely dispatch nothing — e.g. a single-decision
	// test whose subject must not invoke any collaborator at all, sharing a
	// suite with a test whose subject does.
	SkipInvokeForTestIDs []string `json:"skip_invoke_for_test_ids,omitempty"`
}

// deadLockPID names a PID this suite's own runtime cannot be using: negative
// PIDs are never valid process identifiers on any platform this tool
// targets, so domain.ProcessAlive reports it dead unconditionally, with no
// dependency on what PIDs happen to be free on the machine running the test.
const deadLockPID = -1

// sidecarTurn mirrors fake.Turn in a JSON-safe shape (error values do not
// marshal).
type sidecarTurn struct {
	Body   string          `json:"body,omitempty"`
	Raw    json.RawMessage `json:"raw,omitempty"`
	ErrMsg string          `json:"err_msg,omitempty"`
}

// sidecarDetail carries the fields the fake adapter's own native wire shape
// accepts for a call (see wireCallOut below) — the subset of TaskMessage
// this test double can populate.
type sidecarDetail struct {
	AgentInstanceID string `json:"agent_instance_id"`
	TaskDescription string `json:"task_description"`
}

// wireSubjectTurn/wireIdentity/wireResult decode exactly the JSON shape
// internal/harness/fake encodes into SpawnPlan.Stdin (its subjectTurnWire /
// subjectResultWire). The shapes are private to that package, but the wire
// itself is plain JSON with stable, documented field names, so decoding here
// needs no access to the unexported Go types.
type wireSubjectTurn struct {
	Invoke *wireIdentity `json:"invoke,omitempty"`
	Result *wireResult   `json:"result,omitempty"`
}

type wireIdentity struct {
	Tool  string `json:"tool"`
	Agent string `json:"agent,omitempty"`
}

type wireResult struct {
	ProtocolMessage string `json:"protocol_message"`
	Disposition     string `json:"disposition"`
	ExitCode        int    `json:"exit_code"`
}

// wireCallOut is the native payload this stand-in sends into
// fake.Adapter.TranslateCall, matching that package's own private wireCall
// field names exactly (phase, tool, agent, agent_instance_id,
// task_description, token). AgentID is only populated for the completion
// phase, where fake.Adapter expects an agent_id field instead of a token.
type wireCallOut struct {
	Phase           string `json:"phase"`
	Tool            string `json:"tool"`
	Agent           string `json:"agent"`
	AgentInstanceID string `json:"agent_instance_id"`
	TaskDescription string `json:"task_description"`
	Token           string `json:"token"`
	AgentID         string `json:"agent_id,omitempty"`
}

// wireReplyIn decodes what fake.Adapter.TranslateOutcome writes back,
// matching that package's own private wireReply field names.
type wireReplyIn struct {
	Kind   string          `json:"kind"`
	Body   json.RawMessage `json:"body,omitempty"`
	Prompt string          `json:"prompt,omitempty"`
	Token  string          `json:"token"`
}

// standinClock is the real wall clock. It is a distinct type from any other
// package's so the stand-in never needs to import a test helper.
type standinClock struct{}

func (standinClock) Now() time.Time { return time.Now() }

// runStandin implements the scripted stand-in subject's whole process
// lifetime: read its script, replay it against the real interceptor shell,
// print a final result envelope, exit zero.
//
// Every failure here is reported to stderr and answered with a plausible
// completion rather than a non-zero exit or a hang: a stand-in that breaks
// the run it is standing in for would produce evidence about itself, the
// same obligation internal/interceptor.Run documents for its own process.
func runStandin() int {
	controlDir := standinFlag(os.Args[1:], "--sandbox")
	subjectDir, _ := os.Getwd()

	sc, err := loadStandinScript()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-subject: loading script:", err)
		return printFallbackResult()
	}

	if sc.SleepBeforeExit != "" {
		d, parseErr := time.ParseDuration(sc.SleepBeforeExit)
		if parseErr != nil {
			fmt.Fprintln(os.Stderr, "fake-subject: parsing sleep_before_exit:", parseErr)
			return printFallbackResult()
		}
		// A run whose declared timeout is shorter than d must never observe
		// this sleep complete: the parent context's cancellation kills this
		// process outright (mosaic-common/harness.Run's Process.Kill), so a
		// scenario scripting a short timeout alongside a longer sleep proves
		// the subject genuinely never reached a Result turn, rather than
		// merely never being told to.
		time.Sleep(d)
	}

	if sc.SeedDeadLock {
		if err := seedDeadLock(controlDir); err != nil {
			fmt.Fprintln(os.Stderr, "fake-subject: seeding dead lock:", err)
			return printFallbackResult()
		}
	}

	if sc.SeedDeadLockOnce {
		markerPath := os.Getenv(scriptEnvVar) + ".seed_once_done"
		if _, statErr := os.Stat(markerPath); os.IsNotExist(statErr) {
			// First spawn: seed the dead lock and create the marker so
			// subsequent spawns (the retry) skip seeding and run clean.
			if err := seedDeadLock(controlDir); err != nil {
				fmt.Fprintln(os.Stderr, "fake-subject: seeding dead lock (once):", err)
				return printFallbackResult()
			}
			if err := os.WriteFile(markerPath, []byte("done"), 0o644); err != nil {
				// Marker creation failed; log it but continue — the lock is
				// already seeded for this spawn, so skipping the marker only
				// risks seeding twice on the retry rather than corrupting
				// this attempt's result.
				fmt.Fprintln(os.Stderr, "fake-subject: writing seed-once marker:", err)
			}
		}
		// Subsequent spawns: marker exists, skip seeding.
	}

	turns, err := readSubjectTurns()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-subject: reading subject turns:", err)
		return printFallbackResult()
	}

	adapter := fake.New(fake.Options{
		Capabilities: sc.Capabilities,
		Script:       reconstructScript(sc.CollaboratorTurns),
	})

	clock := standinClock{}
	registry, regErr := interceptor.LoadRegistry(controlDir)
	if regErr != nil {
		fmt.Fprintln(os.Stderr, "fake-subject: loading registry:", regErr)
	}
	groups, groupErr := interceptor.LoadGroups(controlDir)
	if groupErr != nil {
		fmt.Fprintln(os.Stderr, "fake-subject: loading groups:", groupErr)
	}
	store := runstate.NewStore(controlDir, clock)
	log := invlog.NewLog(domain.Sandbox{ControlDir: controlDir}.InvocationLogPath())

	resolver, resErr := fixtures.NewResolver(controlDir)
	if resErr != nil {
		fmt.Fprintln(os.Stderr, "fake-subject: resolving fixtures:", resErr)
		return printFallbackResult()
	}
	effects := sideeffects.NewApplier(resolver)

	detailIdx := map[string]int{}
	seq := 0
	var lastResult *wireResult
	var halted bool
	probeResults := map[string]bool{}

	currentTestID, currentRunNumber := readTestIdentity(store)
	skipInvoke := intInSlice(sc.SkipInvokeOnRuns, currentRunNumber) || stringInSlice(sc.SkipInvokeForTestIDs, currentTestID)

	i := 0
	for i < len(turns) && !halted {
		t := turns[i]
		switch {
		case t.Invoke != nil:
			// A run of consecutive Invoke turns (no Result turn between
			// them) is one concurrency batch: every member's pre-invocation
			// interception runs before any member's post-invocation
			// interception does, so their recorded start/end intervals
			// genuinely overlap — see concurrencyBatch's own doc comment for
			// why this needs no goroutines to be honest evidence about
			// concurrency.
			batchStart := i
			for i < len(turns) && turns[i].Invoke != nil {
				i++
			}
			if skipInvoke {
				// SkipInvokeOnRuns names this run: the scripted subject
				// "decides" not to dispatch any collaborator in this batch —
				// no interception call is made, so no stub is matched and no
				// side effect fires. Used to make repetitions of one test
				// produce different observable outcomes from one fixed
				// subject-turn script.
				continue
			}
			batch := turns[batchStart:i]
			if !concurrencyBatch(adapter, store, log, effects, clock, registry, groups,
				controlDir, subjectDir, sc.InvocationDetails, detailIdx, &seq, sc.ProbeFiles, probeResults, batch) {
				halted = true
			}

		case t.Result != nil:
			lastResult = t.Result
			i++

		default:
			i++
		}
	}

	if probePath := os.Getenv(probeEnvVar); probePath != "" && len(sc.ProbeFiles) > 0 {
		if data, err := json.Marshal(probeResults); err == nil {
			if err := os.WriteFile(probePath, data, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "fake-subject: writing probe results:", err)
			}
		}
	}

	if lastResult == nil {
		// If the early-exit sentinel was written by a cutoff during this run,
		// report EarlyExit so the runner's supervisor records the correct
		// termination reason. Without this check the standin always emits
		// DispositionCompleted and the supervisor's sentinel poller (250 ms
		// tick) never wins the race against the fast subprocess exit.
		sentinelPath := filepath.Join(controlDir, domain.EarlyExitSentinelName)
		if _, statErr := os.Stat(sentinelPath); statErr == nil {
			lastResult = &wireResult{Disposition: string(domain.DispositionEarlyExit)}
		} else {
			lastResult = &wireResult{Disposition: string(domain.DispositionCompleted)}
		}
	}
	out, _ := json.Marshal(lastResult)
	fmt.Fprintln(os.Stdout, string(out))
	return 0
}

// intInSlice reports whether n appears in xs.
func intInSlice(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

// stringInSlice reports whether s appears in xs.
func stringInSlice(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// concurrencyBatch drives the pre-invocation interception for every member
// of batch, in order, before driving any member's post-invocation
// interception — also in order. A batch of one behaves exactly as the
// previous turn-at-a-time loop did (pre immediately followed by post),
// so every existing scenario's behaviour is unchanged.
//
// A batch of two or more is what makes a genuine, honest overlap
// reconstructible from the invocation log: internal/concurrency.Peaks
// derives overlap from each invocation's recorded start and end timestamps
// alone, so two invocations whose starts both precede either one's end are,
// by that same evidence, concurrent — regardless of whether the process
// that produced them used real goroutines. A single-process stand-in
// cannot honestly claim OS-level concurrency for a scripted subject in the
// first place (there is no real second collaborator process to overlap
// with); reordering when each interception point fires is the whole of what
// "the subject dispatched two collaborators in parallel" can mean here, and
// it is exactly what internal/concurrency.Peaks is defined to read off the
// log.
//
// When the adapter declares SupportsReplyRecovery, concurrencyBatch
// additionally drives a completion-phase interception for every dispatch
// that did not terminate at the pre-invocation point. The completion event
// is driven after all post-phase events for the batch, in the same order
// the post events were driven. The synthetic agent_id is stable for a
// given sequence number, so the decision core's sole-outstanding fallback
// can correlate it back to its dispatch when exactly one is in flight.
//
// Returns false when a member's pre-invocation interception halts the run,
// in which case the caller stops the whole script rather than continuing
// with a partial batch.
func concurrencyBatch(
	adapter *fake.Adapter,
	store runstate.Store,
	log invlog.Log,
	effects sideeffects.Applier,
	clock domain.Clock,
	registry domain.StubRegistry,
	groups []domain.ParallelGroup,
	controlDir, subjectDir string,
	details map[string][]sidecarDetail,
	detailIdx map[string]int,
	seq *int,
	probeFiles []string,
	probeResults map[string]bool,
	batch []wireSubjectTurn,
) bool {
	type pending struct {
		id      domain.CollaboratorIdentity
		token   string
		agentID string // synthetic agent_id for the completion phase
		needsPost bool // true for OutcomeRewritePrompt dispatches
	}
	var toProcess []pending // all non-terminating dispatches

	replyRecovery := adapter.Capabilities().SupportsReplyRecovery

	for _, t := range batch {
		id := domain.CollaboratorIdentity{ToolName: t.Invoke.Tool, AgentIdentity: t.Invoke.Agent}
		key := id.Key()
		idx := detailIdx[key]
		detailIdx[key] = idx + 1
		var detail sidecarDetail
		if ds := details[key]; idx < len(ds) {
			detail = ds[idx]
		}
		*seq++
		token := fmt.Sprintf("standin-%d", *seq)
		agentID := fmt.Sprintf("fake-agent-%d", *seq)
		testID, runNumber := readTestIdentity(store)

		reply, ok := runInterception(adapter, store, log, effects, clock, registry, groups,
			controlDir, subjectDir, testID, runNumber, domain.PhasePre,
			wireCallOut{Phase: "pre", Tool: id.ToolName, Agent: id.AgentIdentity,
				AgentInstanceID: detail.AgentInstanceID, TaskDescription: detail.TaskDescription, Token: token})
		if !ok {
			return false
		}
		switch reply.Kind {
		case string(domain.OutcomeHalt):
			return false
		case string(domain.OutcomeSubstitute):
			// Substitution terminates at the pre-invocation point: no post or
			// completion event follows. TerminatesAtPre(OutcomeSubstitute) = true.
		case string(domain.OutcomeRewritePrompt):
			// OutcomeRewritePrompt always needs a post-phase close; when reply
			// recovery is enabled it also needs a completion event.
			toProcess = append(toProcess, pending{
				id: id, token: reply.Token, agentID: agentID, needsPost: true,
			})
		default:
			// OutcomePassthrough and anything else: the collaborator runs without
			// interception. With reply recovery, a completion event still follows
			// so the decision core can apply the cutoff rule.
			if replyRecovery {
				toProcess = append(toProcess, pending{
					id: id, token: token, agentID: agentID, needsPost: false,
				})
			}
		}
	}

	// Every declared side effect from this batch's stub matches has already
	// been applied (internal/interceptor.Run applies side effects during the
	// pre-invocation phase, before writing its reply — see interceptor.go's
	// own ordering doc comment), so this is the point at which a probed
	// file's existence genuinely predates the collaborator's reply being
	// delivered to the subject.
	for _, path := range probeFiles {
		_, statErr := os.Stat(filepath.Join(subjectDir, filepath.FromSlash(path)))
		probeResults[path] = statErr == nil
	}

	// Drive the post-phase for all dispatches that need it.
	for _, p := range toProcess {
		if !p.needsPost {
			continue
		}
		testID, runNumber := readTestIdentity(store)
		runInterception(adapter, store, log, effects, clock, registry, groups,
			controlDir, subjectDir, testID, runNumber, domain.PhasePost,
			wireCallOut{Phase: "post", Tool: p.id.ToolName, Agent: p.id.AgentIdentity, Token: p.token})
	}

	// When reply recovery is declared, drive a completion-phase event for
	// every non-terminating dispatch after all post events for the batch. This
	// mirrors what a real reply-recovery harness does: its SubagentStop hook
	// fires once per completed collaborator, after the collaborator finishes,
	// and is what the decision core applies the cutoff rule to.
	if replyRecovery {
		for _, p := range toProcess {
			testID, runNumber := readTestIdentity(store)
			runInterception(adapter, store, log, effects, clock, registry, groups,
				controlDir, subjectDir, testID, runNumber, domain.PhaseCompletion,
				wireCallOut{Phase: "completion", Tool: p.id.ToolName, Agent: p.id.AgentIdentity, AgentID: p.agentID})
		}
	}

	return true
}

// runInterception drives one interceptor.Run call and decodes its reply. The
// second return is false when the reply could not be decoded, in which case
// the caller stops the script rather than guessing.
func runInterception(
	adapter *fake.Adapter,
	store runstate.Store,
	log invlog.Log,
	effects sideeffects.Applier,
	clock domain.Clock,
	registry domain.StubRegistry,
	groups []domain.ParallelGroup,
	controlDir, subjectDir, testID string,
	runNumber int,
	phase domain.InterceptionPhase,
	call wireCallOut,
) (wireReplyIn, bool) {
	native, err := json.Marshal(call)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-subject: encoding native call:", err)
		return wireReplyIn{}, false
	}

	var out bytes.Buffer
	interceptor.Run(context.Background(), interceptor.Config{
		ControlDir: controlDir,
		SubjectDir: subjectDir,
		Phase:      phase,

		Adapter: adapter,
		State:   store,
		Log:     log,
		Effects: effects,
		Clock:   clock,

		Registry: registry,
		Groups:   groups,

		TestID:    testID,
		RunNumber: runNumber,

		In:   bytes.NewReader(native),
		Out:  &out,
		Diag: os.Stderr,
	})

	var reply wireReplyIn
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		fmt.Fprintln(os.Stderr, "fake-subject: decoding interceptor reply:", err)
		return wireReplyIn{}, false
	}
	return reply, true
}

// seedDeadLock overwrites the run-state lock runner.setup already
// initialized (as an uncontended, healthy lock — Initialize never writes a
// lock file of its own, only acquireLock's first caller does) with one
// naming a PID domain.ProcessAlive can never report alive, using the same
// on-disk shape internal/runstate/lock_test.go's own writeLockFile helper
// writes. The very first Update call any interception in this run makes
// then reclaims it immediately.
func seedDeadLock(controlDir string) error {
	info := domain.LockInfo{
		PID:        deadLockPID,
		Host:       "e2e-seeded-dead-holder",
		AcquiredAt: time.Now(),
		ControlDir: controlDir,
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(domain.Sandbox{ControlDir: controlDir}.LockPath(), data, 0o644)
}

// readTestIdentity reads the run identity setup wrote, best-effort — exactly
// the pattern cmd/mosaic-agent-test/interceptor.go uses for a real harness's
// hook invocation.
func readTestIdentity(store runstate.Store) (string, int) {
	if s, err := store.Read(); err == nil {
		return s.TestID, s.RunNumber
	}
	return "", 0
}

// reconstructScript converts the sidecar's JSON-safe turns back into
// fake.Options.Script.
func reconstructScript(in map[string][]sidecarTurn) map[string][]fake.Turn {
	if in == nil {
		return nil
	}
	out := make(map[string][]fake.Turn, len(in))
	for key, turns := range in {
		converted := make([]fake.Turn, 0, len(turns))
		for _, t := range turns {
			ft := fake.Turn{Body: t.Body, Raw: t.Raw}
			if t.ErrMsg != "" {
				ft.Err = errors.New(t.ErrMsg)
			}
			converted = append(converted, ft)
		}
		out[key] = converted
	}
	return out
}

// loadStandinScript reads and decodes the sidecar RunScenario wrote.
func loadStandinScript() (standinScript, error) {
	path := os.Getenv(scriptEnvVar)
	if path == "" {
		return standinScript{}, fmt.Errorf("%s is not set", scriptEnvVar)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return standinScript{}, err
	}
	var sc standinScript
	if err := json.Unmarshal(data, &sc); err != nil {
		return standinScript{}, err
	}
	return sc, nil
}

// readSubjectTurns decodes the script SpawnPlan.Stdin carried.
func readSubjectTurns() ([]wireSubjectTurn, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, err
	}
	var turns []wireSubjectTurn
	if len(data) == 0 {
		return turns, nil
	}
	if err := json.Unmarshal(data, &turns); err != nil {
		return nil, err
	}
	return turns, nil
}

// standinFlag returns the value following a named flag in args, or "".
func standinFlag(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// printFallbackResult writes a minimal completion envelope so a stand-in
// that could not even read its own script still lets the run terminate
// observably instead of hanging the launcher until its timeout.
func printFallbackResult() int {
	out, _ := json.Marshal(wireResult{Disposition: string(domain.DispositionSpawnFailed)})
	fmt.Fprintln(os.Stdout, string(out))
	return 0
}
