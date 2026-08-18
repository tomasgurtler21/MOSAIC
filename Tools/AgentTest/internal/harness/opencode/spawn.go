package opencode

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
)

// DefaultSpawnTimeout is this adapter's own backstop on a single invocation.
// It is NOT the test's declared timeout, which the runner's supervisor
// enforces by cancelling the launch context.
const DefaultSpawnTimeout = 30 * time.Minute

// SpawnPlan implements domain.HarnessAdapter. Building the plan is pure
// description over its inputs: no process is started, no subprocess API is
// touched, and no result is interpreted here. Argument construction is
// delegated to mosaic-common/harness.BuildOpenCodeArgs so this package never
// re-implements it.
//
// SystemPrompt is commonharness.EnvBlock(p.Sandbox.SubjectDir), not
// EnvBlock(""): selecting a named agent replaces OpenCode's default system
// prompt entirely, so the <env> preamble EnvBlock synthesizes must be
// supplied explicitly, and it must describe the sandbox the subject actually
// runs in rather than this tool's own process working directory. This is the
// one place OpenCode's SpawnPlan diverges from Runner's OpenCodeAdapter,
// which passes EnvBlock("") for both invocation kinds.
//
// The plan carries neither a test-declared timeout nor a turn limit: no
// argument this method receives could supply either (domain.SubjectUnderTest
// and domain.Provisioning both lack them, and domain.RunSettings never
// reaches an adapter). Timeout is set from this adapter's own backstop
// constant, DefaultSpawnTimeout.
//
// Env stays empty: no environment variable relocating OpenCode's user-scope
// configuration into the sandbox is confirmed to exist, so none is claimed
// here. The user scopes are reported inspected rather than neutralized (see
// scopes.go, a later stage).
func (a *Adapter) SpawnPlan(ctx context.Context, subject domain.SubjectUnderTest, p domain.Provisioning) (domain.SpawnPlan, error) {
	spawnReq := commonharness.SpawnRequest{
		Agent: commonharness.AgentRef{
			Identifier: subject.Identity,
			// DefinitionPath is carried for symmetry with the Claude Code
			// adapter; BuildOpenCodeArgs does not read it. Selecting the
			// agent by name loads the definition MOSAIC already deploys to
			// .opencode/agents/<identifier>.md.
			DefinitionPath: filepath.Join(p.Sandbox.SubjectDir, filepath.FromSlash(subject.DefinitionPath)),
			Kind:           commonharness.InvocationKind(subject.InvocationKind),
		},
		Prompt:       subject.OpeningMessage,
		SystemPrompt: commonharness.EnvBlock(p.Sandbox.SubjectDir),
		Model:        subject.Model,
		AllowedTools: subject.AllowedTools,
		OutputFormat: "json",
	}

	args, err := commonharness.BuildOpenCodeArgs(spawnReq)
	if err != nil {
		return domain.SpawnPlan{}, fmt.Errorf("opencode: building spawn arguments: %w", err)
	}

	return domain.SpawnPlan{
		Executable:        OpenCodeCLIExecutable,
		Args:              args,
		WorkingDir:        p.Sandbox.SubjectDir,
		Timeout:           DefaultSpawnTimeout,
		EarlyExitSentinel: p.Sandbox.EarlyExitSentinelPath(),
	}, nil
}
