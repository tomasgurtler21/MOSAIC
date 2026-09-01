package claudecode_test

// Tests for spawn-plan construction (T11.6): the plan carries the sandbox
// subject directory as the working directory, the subject's definition and
// opening message, the environment entry that relocates this harness's
// user-scope configuration into the sandbox, and the early-exit sentinel
// path the supervisor watches. Building the plan performs no process
// control and touches no subprocess machinery — a test that builds a plan
// and observes a child process is testing a design error.
//
// The plan does NOT carry a test-declared timeout or turn limit, and no
// argument SpawnPlan receives could supply one: domain.SubjectUnderTest and
// domain.Provisioning both lack them, and domain.RunSettings — the only type
// that holds them — is never handed to an adapter. The declared timeout is
// enforced by the runner's supervisor cancelling the launch context; the
// declared turn limit is enforced through interception, and a harness's own
// turn limit is observed post-hoc by DecodeEnvelope. Plan.Timeout is
// therefore the adapter's own backstop constant, DefaultSpawnTimeout — tests
// assert equality with that constant, not merely positivity, and assert that
// no turn-limit value reaches the plan's arguments.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func spawnTestSubject() domain.SubjectUnderTest {
	return domain.SubjectUnderTest{
		Identity:       "orchestrator",
		DefinitionPath: "agents/orchestrator.md",
		OpeningMessage: "begin the run",
		InvocationKind: "orchestrator",
		Model:          "test-model",
	}
}

func provisionedAdapter(t *testing.T) (*claudecode.Adapter, domain.Sandbox, domain.Provisioning) {
	t.Helper()
	dir := t.TempDir()
	a := claudecode.New(claudecode.Options{})
	sb := newSandbox(t, dir)

	prov, err := a.Provision(testContext(), domain.ProvisionRequest{
		Sandbox:         sb,
		Subject:         spawnTestSubject(),
		InterceptorPath: "/abs/path/to/mosaic-agent-test",
		InterceptorArgs: []string{"intercept"},
	})
	if err != nil {
		t.Fatalf("provisionedAdapter: Provision: %v", err)
	}
	return a, sb, prov
}

func TestSpawnPlan_WorkingDirIsSandboxSubjectDir(t *testing.T) {
	a, sb, prov := provisionedAdapter(t)

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.WorkingDir != sb.SubjectDir {
		t.Errorf("SpawnPlan: WorkingDir = %q, want the sandbox subject dir %q", plan.WorkingDir, sb.SubjectDir)
	}
}

func TestSpawnPlan_ExecutableIsNamed(t *testing.T) {
	a, _, prov := provisionedAdapter(t)

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.Executable == "" {
		t.Errorf("SpawnPlan: Executable is empty; the plan must name what to run")
	}
}

func TestSpawnPlan_ArgsCarrySubjectDefinitionAndOpeningMessage(t *testing.T) {
	a, _, prov := provisionedAdapter(t)
	subject := spawnTestSubject()

	plan, err := a.SpawnPlan(testContext(), subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	// The opening message travels on stdin (not in argv) so that multi-line
	// prompts survive cmd.exe on Windows. Assert it is in Stdin.
	if plan.Stdin == nil {
		t.Fatalf("SpawnPlan: Stdin is nil; want the opening message %q delivered on stdin", subject.OpeningMessage)
	}
	if !strings.Contains(string(plan.Stdin), subject.OpeningMessage) {
		t.Errorf("SpawnPlan: Stdin = %q, want the subject's opening message %q in the stdin payload", plan.Stdin, subject.OpeningMessage)
	}
}

// TestSpawnPlan_StdinCarriesOpeningMessage is an explicit assertion that
// SpawnPlan.Stdin is non-nil and contains the subject's opening message.
// The prompt travels on stdin rather than as an argv value, so that
// multi-line prompts are not truncated by cmd.exe on Windows.
func TestSpawnPlan_StdinCarriesOpeningMessage(t *testing.T) {
	a, _, prov := provisionedAdapter(t)
	subject := spawnTestSubject()

	plan, err := a.SpawnPlan(testContext(), subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.Stdin == nil {
		t.Fatalf("SpawnPlan: Stdin is nil; want the opening message %q delivered on stdin", subject.OpeningMessage)
	}
	if !strings.Contains(string(plan.Stdin), subject.OpeningMessage) {
		t.Errorf("SpawnPlan: Stdin = %q, want %q in the stdin payload", plan.Stdin, subject.OpeningMessage)
	}
}

// TestSpawnPlan_ArgsDoNotCarryOpeningMessageAsValue asserts that no argv
// element equals the raw opening message. The prompt travels on stdin; an
// argv element that equals the prompt would indicate the delivery fix is
// absent.
func TestSpawnPlan_ArgsDoNotCarryOpeningMessageAsValue(t *testing.T) {
	a, _, prov := provisionedAdapter(t)
	subject := spawnTestSubject()

	plan, err := a.SpawnPlan(testContext(), subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	for _, arg := range plan.Args {
		if strings.Contains(arg, subject.OpeningMessage) {
			t.Errorf("SpawnPlan: Args = %v, opening message %q must not appear in any argv element (including embedded in a larger value); it must travel on Stdin", plan.Args, subject.OpeningMessage)
		}
	}
}

func TestSpawnPlan_EnvRelocatesUserScopeConfigIntoSandbox(t *testing.T) {
	a, sb, prov := provisionedAdapter(t)

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	var found string
	prefix := claudecode.ConfigHomeEnvVar + "="
	for _, e := range plan.Env {
		if strings.HasPrefix(e, prefix) {
			found = strings.TrimPrefix(e, prefix)
			break
		}
	}
	if found == "" {
		t.Fatalf("SpawnPlan: Env = %v, want a %s entry that relocates the user scope", plan.Env, claudecode.ConfigHomeEnvVar)
	}
	if !strings.HasPrefix(found, sb.Root) && !strings.HasPrefix(found, sb.ControlDir) && !strings.HasPrefix(found, sb.SubjectDir) {
		t.Errorf("SpawnPlan: %s = %q, want it to point inside the sandbox (root %q) so the scope is neutralized rather than merely inspected", claudecode.ConfigHomeEnvVar, found, sb.Root)
	}
}

func TestSpawnPlan_EarlyExitSentinelMatchesSandbox(t *testing.T) {
	a, sb, prov := provisionedAdapter(t)

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.EarlyExitSentinel != sb.EarlyExitSentinelPath() {
		t.Errorf("SpawnPlan: EarlyExitSentinel = %q, want %q", plan.EarlyExitSentinel, sb.EarlyExitSentinelPath())
	}
}

// TestSpawnPlan_TimeoutEqualsAdapterBackstopConstant asserts that
// SpawnPlan.Timeout is the adapter's own backstop constant, not a
// test-declared value. RunSettings never reaches an adapter: neither
// domain.SubjectUnderTest nor domain.Provisioning carries a timeout or a
// turn limit, so the plan cannot echo one. The declared timeout is enforced
// separately by the runner's supervisor cancelling the launch context; a
// declared value reaching the plan would be a design error, not a feature.
func TestSpawnPlan_TimeoutEqualsAdapterBackstopConstant(t *testing.T) {
	a, _, prov := provisionedAdapter(t)

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.Timeout != claudecode.DefaultSpawnTimeout {
		t.Errorf("SpawnPlan: Timeout = %v, want the adapter's backstop constant DefaultSpawnTimeout = %v", plan.Timeout, claudecode.DefaultSpawnTimeout)
	}
}

// TestSpawnPlan_ArgsCarryNoTurnLimitFlag asserts that no turn-limit value
// reaches the plan's CLI arguments. The declared turn limit is enforced
// through interception (written into RunState.TurnLimit at setup), and the
// harness's own turn limit is observed post-hoc from the output envelope by
// DecodeEnvelope; neither path runs through SpawnPlan, and domain.SpawnPlan
// has no TurnLimit field for one to occupy.
func TestSpawnPlan_ArgsCarryNoTurnLimitFlag(t *testing.T) {
	a, _, prov := provisionedAdapter(t)

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	joined := strings.Join(plan.Args, " ")
	if strings.Contains(joined, "max-turns") || strings.Contains(joined, "turn-limit") || strings.Contains(joined, "turn_limit") {
		t.Errorf("SpawnPlan: Args = %v, want no turn-limit flag; the declared turn limit is enforced through interception, not carried in the plan", plan.Args)
	}
}

// TestSpawnPlan_BuildingItStartsNoProcessAndTouchesNoFiles asserts that
// building the plan is pure description: no file appears anywhere in the
// sandbox (subject dir or control dir) as a result of merely calling
// SpawnPlan, and no subprocess is left running.
func TestSpawnPlan_BuildingItStartsNoProcessAndTouchesNoFiles(t *testing.T) {
	a, sb, prov := provisionedAdapter(t)

	beforeSubject := listSpawnDir(t, sb.SubjectDir)
	beforeControl := listSpawnDir(t, sb.ControlDir)

	if _, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov); err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	afterSubject := listSpawnDir(t, sb.SubjectDir)
	afterControl := listSpawnDir(t, sb.ControlDir)

	if !reflect.DeepEqual(beforeSubject, afterSubject) {
		t.Errorf("SpawnPlan: subject dir changed from %v to %v; SpawnPlan must describe, not perform, process control", beforeSubject, afterSubject)
	}
	if !reflect.DeepEqual(beforeControl, afterControl) {
		t.Errorf("SpawnPlan: control dir changed from %v to %v; SpawnPlan must describe, not perform, process control", beforeControl, afterControl)
	}
}

// TestSpawnPlan_StdinEnvBlockNamesSubjectDir verifies that the stdin payload
// of the spawn plan names the sandbox subject directory in its synthesized
// <env> block. plan.WorkingDir (the OS-level working directory for the
// subprocess) was already set to the subject directory; this test pins the
// complementary requirement that the text the spawned orchestrator agent is
// told about its working directory also names the subject directory — not
// AgentTest's own tool directory, which is what the process CWD fallback
// would produce when no working directory is passed to the spawn request.
//
// Without the fix, SpawnPlan does not set WorkingDir on the commonharness
// SpawnRequest, so BuildArgs falls through to EnvBlock(""), which reads the
// process's own working directory. The spawned agent is therefore told the
// wrong directory in its <env> block. This test will fail until the fix
// populates SpawnRequest.WorkingDir from the sandbox.
func TestSpawnPlan_StdinEnvBlockNamesSubjectDir(t *testing.T) {
	a, sb, prov := provisionedAdapter(t)
	subject := spawnTestSubject()

	plan, err := a.SpawnPlan(testContext(), subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.Stdin == nil {
		t.Fatalf("SpawnPlan: Stdin is nil; want a stdin payload containing the env block for orchestrator invocations")
	}
	if !strings.Contains(string(plan.Stdin), sb.SubjectDir) {
		t.Errorf("SpawnPlan: stdin env block = %q, want the sandbox subject directory %q named in the env block so the spawned agent is told the correct working directory", plan.Stdin, sb.SubjectDir)
	}
}

// TestSpawnPlan_RepeatedCallsAreEquivalent asserts SpawnPlan is a pure
// description over its inputs — calling it twice with the same provisioning
// produces an equivalent plan, not one that accumulates state from the
// first call.
func TestSpawnPlan_RepeatedCallsAreEquivalent(t *testing.T) {
	a, _, prov := provisionedAdapter(t)
	subject := spawnTestSubject()

	first, err := a.SpawnPlan(testContext(), subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan (first): %v", err)
	}
	second, err := a.SpawnPlan(testContext(), subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan (second): %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Errorf("SpawnPlan: first call = %+v, second call = %+v; identical inputs must produce an equivalent plan", first, second)
	}
}

func listSpawnDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listSpawnDir(%q): %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, filepath.Join(dir, e.Name()))
	}
	return names
}

// ---------------------------------------------------------------------------
// Session persistence: subject opts in, default path does not (T4.2)
// ---------------------------------------------------------------------------

// TestSpawnPlan_SubjectLaunchOmitsNoSessionPersistenceFlag verifies that the
// plan produced by SpawnPlan does NOT include --no-session-persistence. The
// subject is spawned with session persistence enabled so that the Claude Code
// CLI writes its transcript file to the path it supplies in every hook payload.
// Without session persistence the transcript file is never written, even though
// the payload carries a valid path — which means the logger bundle's
// emit_usage_records returns 0 silently on every hook firing, and no model or
// token fields are ever written into OrchestrationLogs.
func TestSpawnPlan_SubjectLaunchOmitsNoSessionPersistenceFlag(t *testing.T) {
	a, _, prov := provisionedAdapter(t)

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	for _, arg := range plan.Args {
		if arg == "--no-session-persistence" {
			t.Errorf("SpawnPlan: Args = %v, want --no-session-persistence absent from the subject spawn plan; the subject must be launched with session persistence enabled so the logger bundle can capture usage records", plan.Args)
			return
		}
	}
}

// TestSpawnPlan_EnvCarriesMosaicRunID asserts that the spawn plan's Env slice
// contains a MOSAIC_RUN_ID entry whose value equals the sandbox's run
// identifier. The MOSAIC logger bundle reads this variable as a fallback when
// no run id can be extracted from the opening prompt, attributing the
// session's events to the correct run bucket from the first hook firing.
func TestSpawnPlan_EnvCarriesMosaicRunID(t *testing.T) {
	a, sb, prov := provisionedAdapter(t)

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	prefix := claudecode.RunIDEnvVar + "="
	var found string
	for _, e := range plan.Env {
		if strings.HasPrefix(e, prefix) {
			found = strings.TrimPrefix(e, prefix)
			break
		}
	}
	if found == "" {
		t.Fatalf("SpawnPlan: Env = %v, want a %s entry so the logger bundle can attribute events to the correct run bucket", plan.Env, claudecode.RunIDEnvVar)
	}
	if found != sb.Key.RunID {
		t.Errorf("SpawnPlan: %s = %q, want %q (the sandbox run id)", claudecode.RunIDEnvVar, found, sb.Key.RunID)
	}
}

// TestSpawnPlan_DefaultSpawnRequestPreservesNoSessionPersistenceFlag verifies
// that a zero-value SpawnRequest — the shape used by any spawn that has not
// explicitly opted into transcript persistence — still produces
// --no-session-persistence. This guards against the subject-specific opt-in
// accidentally widening to all builds: only the subject launch (via SpawnPlan)
// enables transcript persistence; every other call path that uses the default
// SpawnRequest must continue to emit the flag unchanged.
func TestSpawnPlan_DefaultSpawnRequestPreservesNoSessionPersistenceFlag(t *testing.T) {
	args, _, err := commonharness.BuildArgs(commonharness.SpawnRequest{
		Agent:        commonharness.AgentRef{Kind: commonharness.InvocationOrdinary},
		OutputFormat: "json",
		// SessionPersistence deliberately omitted (zero value = false).
	})
	if err != nil {
		t.Fatalf("BuildArgs with default SpawnRequest: %v", err)
	}

	found := false
	for _, arg := range args {
		if arg == "--no-session-persistence" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args = %v, want --no-session-persistence present for a zero-value SpawnRequest; the opt-in must be explicit and scoped only to the subject launch", args)
	}
}
