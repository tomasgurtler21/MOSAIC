package claudecode

import (
	"context"
	"fmt"
	"path/filepath"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
)

// ClaudeCLIExecutable names the product CLI this adapter spawns as the
// subject-under-test's own process. Unlike the interceptor bridge
// (bridge.go), which must resolve to the absolute path of the currently
// running binary, the subject's CLI is the published command name an
// operator would invoke directly; resolving it (including the Windows
// .cmd/.bat shim case) is mosaic-common/harness's job at spawn time, not
// this adapter's.
const ClaudeCLIExecutable = "claude"

// SpawnPlan implements domain.HarnessAdapter. Building the plan is pure
// description over its inputs: no process is started, no subprocess API is
// touched, and no result is interpreted here. Argument construction is
// delegated to mosaic-common/harness.BuildArgs so this package never
// re-implements it.
//
// The plan carries neither a test-declared timeout nor a turn limit: no
// argument this method receives could supply either (domain.SubjectUnderTest
// and domain.Provisioning both lack them, and domain.RunSettings never
// reaches an adapter). Timeout is set from this adapter's own backstop
// constant, DefaultSpawnTimeout; no turn-limit flag is ever added to Args.
func (a *Adapter) SpawnPlan(ctx context.Context, subject domain.SubjectUnderTest, p domain.Provisioning) (domain.SpawnPlan, error) {
	spawnReq := commonharness.SpawnRequest{
		Agent: commonharness.AgentRef{
			Identifier:     subject.Identity,
			DefinitionPath: filepath.Join(p.Sandbox.SubjectDir, filepath.FromSlash(subject.DefinitionPath)),
			Kind:           commonharness.InvocationKind(subject.InvocationKind),
		},
		Prompt:       subject.OpeningMessage,
		Model:        subject.Model,
		AllowedTools: subject.AllowedTools,
		OutputFormat: "json",
	}

	args, err := commonharness.BuildArgs(spawnReq)
	if err != nil {
		return domain.SpawnPlan{}, fmt.Errorf("claudecode: building spawn arguments: %w", err)
	}

	// ConfigHomeEnvVar relocates this harness's user-scope configuration
	// entirely inside the sandbox's control directory, which is what lets
	// the adapter report that scope as neutralized rather than merely
	// inspected (see scopes.go).
	configHome := filepath.Join(p.Sandbox.ControlDir, ConfigHomeRelDir)

	return domain.SpawnPlan{
		Executable:        ClaudeCLIExecutable,
		Args:              args,
		WorkingDir:        p.Sandbox.SubjectDir,
		Env:               []string{ConfigHomeEnvVar + "=" + configHome},
		Timeout:           DefaultSpawnTimeout,
		EarlyExitSentinel: p.Sandbox.EarlyExitSentinelPath(),
	}, nil
}
