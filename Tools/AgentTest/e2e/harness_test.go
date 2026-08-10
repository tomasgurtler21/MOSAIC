// Package e2e drives the tool's real non-interactive entry point
// (internal/cli.Execute) through the whole real pipeline — pre-flight,
// workspace, setup, interception, evidence collection, evaluation,
// aggregation, reporting — with no language model and no network, using the
// scripted internal/harness/fake adapter in place of a real harness.
//
// See standin_test.go for how the scripted subject a real run spawns is
// itself provided by this test binary, and why that needed a sidecar
// channel of information beyond what internal/harness/fake.Adapter's
// SpawnPlan carries.
//
// Every scenario is an authored suite/definition/registry under
// testdata/e2e, per this stage's own rule: an authored fixture is what is
// under test, not a suite assembled in Go — see Scenario.Dir.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/harness/fake"
	"mosaic-agent-test/internal/launch"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/sideeffects"
	"mosaic-agent-test/internal/suite"
	"mosaic-agent-test/internal/workspace"
)

// frozenClock is a domain.Clock that always returns the same instant,
// making time-dependent behaviour deterministic regardless of when the
// test runs. A scenario that injects a frozenClock causes defaultRunID
// to produce the same id for both attempts of one repetition (before
// I14.1 folds the attempt index into the suffix), so state-integrity
// tests reliably fail in the RED phase rather than depending on whether
// the two attempts straddle a real wall-clock second boundary.
type frozenClock struct{ t time.Time }

func (c frozenClock) Now() time.Time { return c.t }

var _ domain.Clock = frozenClock{}

// Scenario is one authored end-to-end case.
//
// Script is the scripted adapter's own configuration — Capabilities and
// SubjectTurns reach the real run exactly as internal/harness/fake defines
// them. InvocationDetails is this suite's own addition (see
// standin_test.go): the per-invocation task message content SpawnPlan's
// Stdin has no field for, keyed by domain.CollaboratorIdentity.Key() and
// consumed in the same order as the matching Invoke turns.
type Scenario struct {
	// Dir is the scenario's directory, relative to this module's root
	// (Tools/AgentTest), e.g. "testdata/e2e/happy-path".
	Dir string
	// SuitePath is the suite file's name within Dir.
	SuitePath string

	Script            fake.Options
	InvocationDetails map[string][]sidecarDetail

	// SleepBeforeExit, SeedDeadLock and SeedDeadLockOnce, when set, are
	// forwarded verbatim into the stand-in's own sidecar — see
	// standin_test.go's standinScript for what each one makes the
	// stand-in do before processing any turn.
	SleepBeforeExit  string
	SeedDeadLock     bool
	// SeedDeadLockOnce seeds the dead lock only on the first spawn: a
	// marker file written beside the script path prevents subsequent
	// spawns from seeding again, so attempt two can run clean and
	// demonstrate recovery.
	SeedDeadLockOnce bool

	// Clock, when set, replaces standinClock{} as the domain.Clock passed
	// to both workspace.NewManager and suite.New. A frozenClock makes
	// suite.defaultRunID (which calls clock.Now() and is used when
	// Options.RunID is nil) produce the same id on both attempts of one
	// repetition — because the timestamp component does not advance — so a
	// state-integrity scenario's RED-phase failure is guaranteed by
	// construction rather than by the odds of two OS-scheduled goroutines
	// both happening to fall within the same wall-clock second.
	Clock domain.Clock

	// ProbeFiles, when non-empty, are paths (relative to the sandbox's
	// subject directory) the stand-in checks for existence immediately after
	// a batch's pre-invocation interception returns — before the
	// collaborator's own reply is delivered — so a scenario can observe a
	// declared side effect's timing rather than only its post-run presence.
	// See standin_test.go's standinScript.ProbeFiles and RunScenario's
	// probeEnvVar wiring.
	ProbeFiles []string

	// SkipInvokeOnRuns, when non-empty, names the run numbers (1-based, as
	// domain.RunKey.RunNumber counts them) on which the stand-in treats every
	// Invoke turn as a no-op — the scripted subject "decides" not to dispatch
	// any collaborator on that run, so no stub matches and no side effect
	// fires, without needing a second, divergent subject-turn script. See
	// standin_test.go's standinScript.SkipInvokeOnRuns.
	SkipInvokeOnRuns []int

	// SkipInvokeForTestIDs, when non-empty, are test ids (matching
	// domain.RunKey.TestID) for which the stand-in treats every Invoke turn
	// as a no-op — see standin_test.go's standinScript.SkipInvokeForTestIDs.
	// Lets a suite's several tests share one Scenario.Script while one of
	// them dispatches no collaborator at all.
	SkipInvokeForTestIDs []string

	// WorkspaceRoot, when set, is used as the sandbox root directory instead
	// of a fresh t.TempDir(): a scenario that wants to assert nothing outside
	// what a run created was touched needs to control — and inspect after the
	// run — the same directory the run used.
	WorkspaceRoot string

	// Cost, when set, replaces the default zeroCostProvider — a scenario
	// exercising cost reporting supplies its own domain.CostProvider double.
	Cost domain.CostProvider

	// Args, when non-nil, replaces the default ["run", suite, "--format",
	// "json"] invocation — set to exercise "validate" or a different flag
	// surface. Overriding replaces the whole argument list; SuitePath is not
	// re-injected.
	Args []string
}

// Outcome is everything one scenario run produced.
type Outcome struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	// Result is decoded from the machine-readable report when the
	// invocation produced one (format json); zero otherwise.
	Result wireResultDoc

	// ProbeResults holds, for each of Scenario.ProbeFiles, whether the
	// stand-in observed it existing immediately after the corresponding
	// pre-invocation interception returned — before the collaborator's own
	// reply was delivered. Absent (nil) when Scenario.ProbeFiles was empty.
	ProbeResults map[string]bool

	// WorkspaceRoot is the sandbox root directory the run actually used —
	// either Scenario.WorkspaceRoot verbatim, or the fresh t.TempDir() this
	// call created when it was left empty.
	WorkspaceRoot string
}

// wireResultDoc is a minimal, permissive decoding of the machine-readable
// report (internal/report's wire schema) — only the fields this suite's
// assertions need, so a report field added later does not need a matching
// field added here.
type wireResultDoc struct {
	SuiteID                string              `json:"suite_id"`
	Counts                 map[string]int      `json:"counts"`
	InfrastructureFailures int                 `json:"infrastructure_failures"`
	Tests                  []wireTestReportDoc `json:"tests"`
}

type wireTestReportDoc struct {
	TestID    string             `json:"test_id"`
	Aggregate wireAggregateDoc   `json:"aggregate"`
	Runs      []wireRunReportDoc `json:"runs"`
}

type wireAggregateDoc struct {
	Verdict               string `json:"verdict"`
	Counted               int    `json:"counted"`
	Excluded              int    `json:"excluded"`
	InfrastructureFailure bool   `json:"infrastructure_failure"`
}

type wireRunKeyDoc struct {
	RunID  string `json:"run_id"`
	TestID string `json:"test_id"`
}

type wireRunReportDoc struct {
	Run        wireRunKeyDoc      `json:"run"`
	Verdict    string             `json:"verdict"`
	Reasons    []string           `json:"reasons"`
	Assertions []wireAssertionDoc `json:"assertions"`
	Conditions []wireConditionDoc `json:"conditions"`
	Cost       wireCostDoc        `json:"cost"`
}

type wireAssertionDoc struct {
	Class   string `json:"class"`
	Target  string `json:"target"`
	Outcome string `json:"outcome"`
}

type wireConditionDoc struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

type wireCostDoc struct {
	TotalUSD    float64 `json:"total_usd"`
	Attribution string  `json:"attribution"`
	Detail      string  `json:"detail"`
}

// moduleRoot is Tools/AgentTest, resolved once from this file's own path so
// scenarios can be addressed the same way regardless of the working
// directory `go test` happens to use.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("moduleRoot: could not resolve this file's own path")
	}
	// This file lives at <moduleRoot>/e2e/harness_test.go.
	return filepath.Dir(filepath.Dir(file))
}

// RunScenario executes one scenario through the binary's real non-interactive
// entry point — argument parsing, dispatch, pre-flight, suite execution and
// report rendering — with the scripted adapter and its own stand-in subject
// standing in for a real harness and a real subject.
func RunScenario(t *testing.T, sc Scenario) Outcome {
	t.Helper()

	standinExe := buildStandinOnPath(t)
	standinDir := filepath.Dir(standinExe)
	sep := string(os.PathListSeparator)
	t.Setenv("PATH", standinDir+sep+os.Getenv("PATH"))
	t.Setenv(standinEnv, "1")

	scriptFile := writeScriptSidecar(t, sc)
	t.Setenv(scriptEnvVar, scriptFile)

	var probeFile string
	if len(sc.ProbeFiles) > 0 {
		probeFile = filepath.Join(t.TempDir(), "probe.json")
		t.Setenv(probeEnvVar, probeFile)
	}

	root := moduleRoot(t)
	dir := filepath.Join(root, filepath.FromSlash(sc.Dir))
	fixtureRoot := filepath.Join(dir, "fixtures")
	if _, err := os.Stat(fixtureRoot); err != nil {
		fixtureRoot = dir
	}

	adapter := fake.New(fake.Options{
		Capabilities: sc.Script.Capabilities,
		SubjectTurns: sc.Script.SubjectTurns,
	})
	launcher := launch.New(standinDecoder)
	resolver, err := fixtures.NewResolver(fixtureRoot)
	if err != nil {
		t.Fatalf("RunScenario: constructing fixture resolver: %v", err)
	}
	effects := sideeffects.NewApplier(resolver)
	var clock domain.Clock = standinClock{}
	if sc.Clock != nil {
		clock = sc.Clock
	}
	var cost domain.CostProvider = zeroCostProvider{}
	if sc.Cost != nil {
		cost = sc.Cost
	}

	workspaceRoot := sc.WorkspaceRoot
	if workspaceRoot == "" {
		workspaceRoot = t.TempDir()
	}

	var stdout, stderr bytes.Buffer
	opts := cli.Options{
		Preflight: preflight.Validate,
		Suite: func(rc cli.RunConfig) (cli.SuiteRunner, error) {
			return scenarioSuiteRunner{
				ws:       workspace.NewManager(rc.WorkspaceRoot, clock),
				adapter:  adapter,
				launcher: launcher,
				fixtures: resolver,
				effects:  effects,
				cost:     cost,
				clock:    clock,
			}, nil
		},
		Stdout:         &stdout,
		Stderr:         &stderr,
		WorkspaceRoot:  workspaceRoot,
		FixtureRoot:    fixtureRoot,
		DefaultHarness: "fake",
	}

	args := sc.Args
	if args == nil {
		args = []string{"run", filepath.Join(dir, sc.SuitePath), "--format", "json"}
	}

	exitCode := cli.Execute(context.Background(), args, opts)

	out := Outcome{ExitCode: exitCode, Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), WorkspaceRoot: workspaceRoot}
	var doc wireResultDoc
	if json.Unmarshal(stdout.Bytes(), &doc) == nil {
		out.Result = doc
	}
	if probeFile != "" {
		if data, err := os.ReadFile(probeFile); err == nil {
			var results map[string]bool
			if json.Unmarshal(data, &results) == nil {
				out.ProbeResults = results
			}
		}
	}
	return out
}

// scenarioSuiteRunner adapts the scenario's wired collaborators into
// cli.SuiteRunner, mirroring cmd/mosaic-agent-test's own composedSuiteRunner
// — the same real internal/suite and internal/runner packages, wired here
// because the composition root itself is package main and cannot be
// imported from outside it.
type scenarioSuiteRunner struct {
	ws       workspace.Manager
	adapter  domain.HarnessAdapter
	launcher domain.SubjectLauncher
	fixtures fixtures.Resolver
	effects  sideeffects.Applier
	cost     domain.CostProvider
	clock    domain.Clock
}

func (r scenarioSuiteRunner) Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink) (report.Result, error) {
	rd := runner.Deps{
		Workspaces: r.ws,
		Adapter:    r.adapter,
		Launcher:   r.launcher,
		Fixtures:   r.fixtures,
		Effects:    r.effects,
		Cost:       r.cost,
		Clock:      r.clock,
		Progress:   sink,
	}
	s := suite.New(suite.Options{
		Runner:   scenarioTestRunner{deps: rd},
		Progress: sink,
		Clock:    r.clock,
	})
	return s.Run(ctx, p)
}

type scenarioTestRunner struct{ deps runner.Deps }

func (a scenarioTestRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest) (domain.RunEvidence, error) {
	return runner.Run(ctx, a.deps, runner.Request{Key: key, Test: t, Settings: t.Settings})
}

// zeroCostProvider reports an attributed zero cost, standing in for
// internal/cost (which shells out to the log-analyzer tool): no scenario in
// this suite needs a real cost figure except the cost scenarios, which
// supply their own domain.CostProvider double instead of this one.
type zeroCostProvider struct{}

func (zeroCostProvider) Cost(ctx context.Context, q domain.CostQuery) (domain.CostReport, error) {
	return domain.CostReport{Attribution: domain.AttributionAttributed}, nil
}

// standinDecoder maps the stand-in's own final-line JSON envelope onto a
// domain.SubjectResult. It is this suite's own launch.Decoder, entirely
// separate from any real harness's envelope format, because the stand-in's
// wire format is this suite's own invention (see standin_test.go).
func standinDecoder(res commonharness.Response, runErr error) (domain.SubjectResult, error) {
	sr := domain.SubjectResult{
		ExitCode: res.ExitCode,
		Duration: res.Duration,
	}
	if runErr != nil {
		sr.Disposition = domain.DispositionSpawnFailed
		return sr, nil
	}

	var wr wireResult
	line := lastNonEmptyLine(res.Stdout)
	if err := json.Unmarshal(line, &wr); err != nil {
		sr.Disposition = domain.DispositionCompleted
		sr.RawOutput = string(res.Stdout)
		return sr, nil
	}

	sr.ProtocolMessage = wr.ProtocolMessage
	sr.RawOutput = string(res.Stdout)
	sr.ExitCode = wr.ExitCode
	switch domain.RunDisposition(wr.Disposition) {
	case domain.DispositionCompleted, domain.DispositionTimedOut, domain.DispositionEarlyExit,
		domain.DispositionTurnLimit, domain.DispositionSpawnFailed:
		sr.Disposition = domain.RunDisposition(wr.Disposition)
	default:
		sr.Disposition = domain.DispositionCompleted
	}
	return sr, nil
}

// buildStandinOnPath copies this compiled test binary to a temp directory
// under the literal executable name internal/harness/fake.Adapter.SpawnPlan
// hard-codes ("fake-subject"), so mosaic-common/harness's exec.Command call
// can resolve it by that name once the directory is prepended to PATH.
func buildStandinOnPath(t *testing.T) string {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("buildStandinOnPath: resolving this test binary's path: %v", err)
	}
	src, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("buildStandinOnPath: reading this test binary: %v", err)
	}

	name := "fake-subject"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dst, src, 0o755); err != nil {
		t.Fatalf("buildStandinOnPath: writing stand-in copy: %v", err)
	}
	return dst
}

// writeScriptSidecar serialises the scenario's script into the sidecar file
// the stand-in reads back via scriptEnvVar.
func writeScriptSidecar(t *testing.T, sc Scenario) string {
	t.Helper()

	turns := map[string][]sidecarTurn{}
	for key, ts := range sc.Script.Script {
		converted := make([]sidecarTurn, 0, len(ts))
		for _, turn := range ts {
			st := sidecarTurn{Body: turn.Body, Raw: turn.Raw}
			if turn.Err != nil {
				st.ErrMsg = turn.Err.Error()
			}
			converted = append(converted, st)
		}
		turns[key] = converted
	}

	sidecar := standinScript{
		Capabilities:         sc.Script.Capabilities,
		CollaboratorTurns:    turns,
		InvocationDetails:    sc.InvocationDetails,
		SleepBeforeExit:      sc.SleepBeforeExit,
		SeedDeadLock:         sc.SeedDeadLock,
		SeedDeadLockOnce:     sc.SeedDeadLockOnce,
		ProbeFiles:           sc.ProbeFiles,
		SkipInvokeOnRuns:     sc.SkipInvokeOnRuns,
		SkipInvokeForTestIDs: sc.SkipInvokeForTestIDs,
	}

	data, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatalf("writeScriptSidecar: encoding sidecar: %v", err)
	}
	path := filepath.Join(t.TempDir(), "script.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeScriptSidecar: writing sidecar: %v", err)
	}
	return path
}

// scenarioTimeout is the guard this package's own tests use around a
// scenario run, distinct from any subject-declared timeout the authored
// test carries.
const scenarioTimeout = 30 * time.Second

// lastNonEmptyLine returns the last non-blank line of b, which is where
// runStandin's final result envelope always is: earlier lines may carry
// nothing in this stand-in, but the pattern is kept general in case a future
// scenario has the stand-in emit diagnostics to stdout ahead of its result.
func lastNonEmptyLine(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := bytes.TrimSpace(lines[i])
		if len(trimmed) > 0 {
			return trimmed
		}
	}
	return nil
}
