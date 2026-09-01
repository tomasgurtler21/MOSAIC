package opencode

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
)

// relocatedConfigHome returns the XDG_CONFIG_HOME value the spawn plan sets:
// the parent of the opencode-specific subdirectory, so that when opencode
// appends its own "opencode/" segment to XDG_CONFIG_HOME it lands inside the
// sandbox's control directory, not in the real user configuration home.
func relocatedConfigHome(s domain.Sandbox) string {
	return filepath.Join(s.ControlDir, ConfigHomeRelDir)
}

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
// Isolation guarantee (code-established): Env sets ConfigHomeEnvVar
// (XDG_CONFIG_HOME) to a per-run directory inside p.Sandbox.ControlDir,
// relocating both user-scope configuration locations (user-config and
// user-plugins) into the run's control directory. Two concurrent runs
// therefore receive distinct, non-overlapping configuration home directories
// by construction.
//
// Isolation guarantee (provider-side, outside this repository): whether the
// spawned opencode process honours XDG_CONFIG_HOME at the version in use
// cannot be confirmed from code inspection alone. Relocation is a runtime
// property. If a future version of opencode stops honouring this variable,
// the adapter must update this to a documented limitation and mark the scopes
// InSandbox: false, Isolatable: false, with a non-empty Detail.
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
		// Relocate the user-scope configuration into the run's control
		// directory so the spawned process reads from an isolated location
		// rather than the real user home. See the doc comment above for the
		// code-established vs provider-side isolation boundary.
		Env: []string{
			fmt.Sprintf("%s=%s", ConfigHomeEnvVar, relocatedConfigHome(p.Sandbox)),
		},
	}, nil
}
