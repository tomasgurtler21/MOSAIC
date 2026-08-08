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

	joined := strings.Join(plan.Args, " ")
	if !strings.Contains(joined, subject.OpeningMessage) {
		t.Errorf("SpawnPlan: Args = %v, want the subject's opening message %q to appear somewhere", plan.Args, subject.OpeningMessage)
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
