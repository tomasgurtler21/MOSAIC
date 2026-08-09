package opencode_test

// Tests for spawn-plan construction: the plan carries the sandbox subject
// directory as the working directory, the subject's opening message, an
// executable named for the published OpenCode CLI, the early-exit sentinel
// path the supervisor watches, and arguments built by the shared
// mosaic-common/harness.BuildOpenCodeArgs — never re-implemented locally.
// The request behind those arguments also carries a SystemPrompt built from
// commonharness.EnvBlock(the sandbox subject dir): selecting a named agent
// replaces OpenCode's default system prompt, so the <env> preamble EnvBlock
// synthesizes must be supplied explicitly, and it must describe the sandbox
// the subject actually runs in rather than the tool's own working directory.
// Building the plan performs no process control: no subprocess is started
// and no file changes anywhere in the sandbox.
//
// The plan carries no test-declared timeout or turn limit: neither
// domain.SubjectUnderTest nor domain.Provisioning holds either, and
// domain.RunSettings never reaches an adapter. Plan.Timeout is therefore the
// adapter's own backstop constant, DefaultSpawnTimeout.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
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

// baseProvisioning builds the minimal domain.Provisioning SpawnPlan needs —
// a sandbox — directly, without going through Provision: this stage's
// Provision is not implemented yet, and SpawnPlan's own contract reads only
// p.Sandbox.
func baseProvisioning(t *testing.T, dir string) (domain.Sandbox, domain.Provisioning) {
	t.Helper()
	sb := newSandbox(t, dir)
	return sb, domain.Provisioning{Sandbox: sb}
}

func TestSpawnPlan_WorkingDirIsSandboxSubjectDir(t *testing.T) {
	a := opencode.New(opencode.Options{})
	sb, prov := baseProvisioning(t, t.TempDir())

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.WorkingDir != sb.SubjectDir {
		t.Errorf("SpawnPlan: WorkingDir = %q, want the sandbox subject dir %q", plan.WorkingDir, sb.SubjectDir)
	}
}

func TestSpawnPlan_ExecutableIsOpenCodeCLI(t *testing.T) {
	a := opencode.New(opencode.Options{})
	_, prov := baseProvisioning(t, t.TempDir())

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.Executable != opencode.OpenCodeCLIExecutable {
		t.Errorf("SpawnPlan: Executable = %q, want the published command name %q", plan.Executable, opencode.OpenCodeCLIExecutable)
	}
}

// TestSpawnPlan_ArgsMatchSharedOpenCodeArgumentBuilder asserts the plan's
// arguments are exactly what the shared mosaic-common/harness argument
// builder produces for the equivalent SpawnRequest — never a
// locally-reimplemented argument vector.
//
// The reference request's SystemPrompt is commonharness.EnvBlock(the sandbox
// subject dir), matching ContractsDesign.md's SpawnPlan contract: selecting
// a named agent replaces OpenCode's default system prompt, so the <env>
// preamble EnvBlock synthesizes must ride in the positional message via
// SystemPrompt. EnvBlock is an existing exported function this test calls
// directly, not a symbol under test here.
func TestSpawnPlan_ArgsMatchSharedOpenCodeArgumentBuilder(t *testing.T) {
	a := opencode.New(opencode.Options{})
	subject := spawnTestSubject()
	sb, prov := baseProvisioning(t, t.TempDir())

	plan, err := a.SpawnPlan(testContext(), subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	want, err := commonharness.BuildOpenCodeArgs(commonharness.SpawnRequest{
		Agent: commonharness.AgentRef{
			Identifier: subject.Identity,
			Kind:       commonharness.InvocationKind(subject.InvocationKind),
		},
		Prompt:       subject.OpeningMessage,
		SystemPrompt: commonharness.EnvBlock(sb.SubjectDir),
		Model:        subject.Model,
		OutputFormat: "json",
	})
	if err != nil {
		t.Fatalf("BuildOpenCodeArgs (reference): %v", err)
	}

	if !reflect.DeepEqual(plan.Args, want) {
		t.Errorf("SpawnPlan: Args = %v, want the shared argument builder's output %v", plan.Args, want)
	}
}

// TestSpawnPlan_SystemPromptUsesSandboxSubjectDirNotProcessCwd asserts the
// EnvBlock the plan's SystemPrompt carries reports the sandbox subject
// directory — where the subject actually runs — and not an empty working
// directory (which EnvBlock resolves to the tool's own process cwd). This is
// the one place OpenCode's SpawnPlan diverges from Runner's OpenCodeAdapter,
// which passes EnvBlock("") for both invocation kinds; AgentTest's subject
// runs in a sandbox, not in the tool's own working directory, so describing
// the wrong place would corrupt the subject's own picture of where it is.
func TestSpawnPlan_SystemPromptUsesSandboxSubjectDirNotProcessCwd(t *testing.T) {
	a := opencode.New(opencode.Options{})
	subject := spawnTestSubject()
	sb, prov := baseProvisioning(t, t.TempDir())

	plan, err := a.SpawnPlan(testContext(), subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	wrongCwdArgs, err := commonharness.BuildOpenCodeArgs(commonharness.SpawnRequest{
		Agent: commonharness.AgentRef{
			Identifier: subject.Identity,
			Kind:       commonharness.InvocationKind(subject.InvocationKind),
		},
		Prompt:       subject.OpeningMessage,
		SystemPrompt: commonharness.EnvBlock(""),
		Model:        subject.Model,
		OutputFormat: "json",
	})
	if err != nil {
		t.Fatalf("BuildOpenCodeArgs (reference, empty workingDir): %v", err)
	}

	// The sandbox subject dir is a fresh t.TempDir(), which is never equal
	// to the test process's own working directory, so a plan built against
	// the sandbox dir must diverge from one built against an empty
	// workingDir — unless the adapter is wrongly passing "" through.
	if reflect.DeepEqual(plan.Args, wrongCwdArgs) {
		t.Errorf("SpawnPlan: Args = %v matched EnvBlock(\"\")'s output; want EnvBlock(%q) — the sandbox subject dir, not the process's own working directory", plan.Args, sb.SubjectDir)
	}
}

func TestSpawnPlan_ArgsContainOpeningMessage(t *testing.T) {
	a := opencode.New(opencode.Options{})
	subject := spawnTestSubject()

	plan, err := a.SpawnPlan(testContext(), subject, mustProvisioning(t))
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	joined := strings.Join(plan.Args, " ")
	if !strings.Contains(joined, subject.OpeningMessage) {
		t.Errorf("SpawnPlan: Args = %v, want the subject's opening message %q to appear somewhere", plan.Args, subject.OpeningMessage)
	}
}

func TestSpawnPlan_ArgsCarryNoSessionReuseFlags(t *testing.T) {
	a := opencode.New(opencode.Options{})

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), mustProvisioning(t))
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	for _, forbidden := range []string{"--session", "--continue", "--fork"} {
		for _, arg := range plan.Args {
			if arg == forbidden {
				t.Errorf("SpawnPlan: Args = %v, want no %q — each invocation must create a fresh, ephemeral session", plan.Args, forbidden)
			}
		}
	}
}

func TestSpawnPlan_EarlyExitSentinelMatchesSandbox(t *testing.T) {
	a := opencode.New(opencode.Options{})
	sb, prov := baseProvisioning(t, t.TempDir())

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
// test-declared value: neither domain.SubjectUnderTest nor
// domain.Provisioning carries a timeout, so the plan cannot echo one.
func TestSpawnPlan_TimeoutEqualsAdapterBackstopConstant(t *testing.T) {
	a := opencode.New(opencode.Options{})

	plan, err := a.SpawnPlan(testContext(), spawnTestSubject(), mustProvisioning(t))
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.Timeout != opencode.DefaultSpawnTimeout {
		t.Errorf("SpawnPlan: Timeout = %v, want the adapter's backstop constant DefaultSpawnTimeout = %v", plan.Timeout, opencode.DefaultSpawnTimeout)
	}
}

// TestSpawnPlan_BuildingItStartsNoProcessAndTouchesNoFiles asserts building
// the plan is pure description: no file appears anywhere in the sandbox as
// a result of merely calling SpawnPlan, and no subprocess is left running.
func TestSpawnPlan_BuildingItStartsNoProcessAndTouchesNoFiles(t *testing.T) {
	a := opencode.New(opencode.Options{})
	sb, prov := baseProvisioning(t, t.TempDir())

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
// produces an equivalent plan, not one that accumulates state.
func TestSpawnPlan_RepeatedCallsAreEquivalent(t *testing.T) {
	a := opencode.New(opencode.Options{})
	subject := spawnTestSubject()
	prov := mustProvisioning(t)

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

func mustProvisioning(t *testing.T) domain.Provisioning {
	t.Helper()
	_, prov := baseProvisioning(t, t.TempDir())
	return prov
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
