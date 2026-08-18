package opencode

// Interceptor bridge construction. See ContractsDesign.md's "AgentTest:
// Bridge and Plugin Generation" for the full contract.

import (
	"fmt"

	"mosaic-agent-test/internal/domain"
)

// Bridge is the interceptor invocation baked into the generated plugin.
type Bridge struct {
	// Executable is the absolute path of the currently running binary,
	// resolved from the running process. Never a bare command name: the
	// executable search path could bind a stale copy elsewhere on the
	// machine, whose failures would be unattributable to any source change.
	Executable string

	// Args carry the interceptor subcommand, the control directory as an
	// explicit argument, the harness identity and the interception phase.
	// The control directory is never inferred from a process working
	// directory: the plugin runs inside the harness's own process, from
	// wherever that process happens to be.
	Args []string
}

// BuildBridge composes the bridge for one phase from the interceptor path
// and arguments the driver supplied in the provision request. An empty
// ProvisionRequest.InterceptorPath is refused.
func BuildBridge(req domain.ProvisionRequest, phase domain.InterceptionPhase) (Bridge, error) {
	if req.InterceptorPath == "" {
		return Bridge{}, fmt.Errorf("opencode: building bridge: ProvisionRequest.InterceptorPath is empty")
	}

	args := make([]string, 0, len(req.InterceptorArgs)+6)
	args = append(args, req.InterceptorArgs...)
	args = append(args,
		"--control-dir", req.Sandbox.ControlDir,
		"--harness", HarnessID,
		"--phase", string(phase),
	)

	return Bridge{Executable: req.InterceptorPath, Args: args}, nil
}
