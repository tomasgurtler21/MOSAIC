package claudecode

import (
	"fmt"

	"mosaic-agent-test/internal/domain"
)

// Bridge is the interceptor invocation written into the generated
// configuration. Both fields are fixed at provisioning time.
type Bridge struct {
	// Executable is the absolute path of the currently running binary,
	// resolved from the running process. It is never a bare command name:
	// resolving through the executable search path could bind a stale copy
	// elsewhere on the machine, whose failures would be unattributable to
	// any source change.
	Executable string

	// Args carry the interceptor subcommand, the control directory as an
	// explicit argument, the harness identity, and the interception phase.
	// The control directory is never inferred from the process working
	// directory: this harness may launch a hook command from anywhere, and
	// a bridge that works only when it happens to launch from the sandbox
	// is a defect that presents as a mysteriously empty run.
	Args []string
}

// BuildBridge composes the bridge for one phase from the interceptor path
// and arguments the driver supplied in the provision request. The control
// directory is always passed as an explicit argument, never inferred from a
// process working directory.
func BuildBridge(req domain.ProvisionRequest, phase domain.InterceptionPhase) (Bridge, error) {
	if req.InterceptorPath == "" {
		return Bridge{}, fmt.Errorf("claudecode: building bridge: ProvisionRequest.InterceptorPath is empty")
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

// InterceptorEntries generates this adapter's own registration contribution
// from the bridge — the entries that rewrite the intercepted call's input.
//
// The pre-invocation entry is synchronous: it must run and return before the
// tool call proceeds, because rewriting the call's input only means anything
// if it happens before the call runs. The post-invocation entry is
// asynchronous and purely observational: this adapter's post-invocation
// point cannot rewrite anything, only watch.
func InterceptorEntries(pre, post Bridge) Contribution {
	preAsync := false
	postAsync := true

	return Contribution{
		Source:        "interceptor",
		RewritesInput: true,
		Settings: Settings{
			Hooks: map[string][]Matcher{
				"PreToolUse": {
					{
						Matcher: InterceptedToolName,
						Hooks: []Entry{
							{Type: "command", Command: pre.Executable, Args: pre.Args, Async: &preAsync},
						},
					},
				},
				"PostToolUse": {
					{
						Matcher: InterceptedToolName,
						Hooks: []Entry{
							{Type: "command", Command: post.Executable, Args: post.Args, Async: &postAsync},
						},
					},
				},
			},
		},
	}
}

// CompletionEntry generates this adapter's registration contribution for the
// harness's own completion signal — the SubagentStop hook, the source of the
// collaborator's real reply once the collaborator has actually finished
// (unlike PostToolUse, which fires at launch). It is registered separately
// from InterceptorEntries, and matches on every SubagentStop event rather
// than on InterceptedToolName: SubagentStop is not a tool-use hook and
// carries no tool matcher to filter on.
//
// The entry is asynchronous and purely observational, exactly like the
// post-invocation entry: this phase never rewrites anything and never
// halts the subject, per PhaseCompletion's doc comment in
// domain/interception.go ("Observation only: an outcome for this phase is
// never a substitution, a rewrite or a halt"). It therefore does not
// contribute to the single-rewriter composition invariant Compose enforces.
func CompletionEntry(completion Bridge) Contribution {
	completionAsync := true

	return Contribution{
		Source:        "interceptor",
		RewritesInput: false,
		Settings: Settings{
			Hooks: map[string][]Matcher{
				"SubagentStop": {
					{
						Hooks: []Entry{
							{Type: "command", Command: completion.Executable, Args: completion.Args, Async: &completionAsync},
						},
					},
				},
			},
		},
	}
}
