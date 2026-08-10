package claudecode_test

// Tests for spawned-bridge wiring: the generated interception configuration
// must invoke this tool's interceptor entry point by the absolute path of
// the currently running binary, with the control directory and harness
// identity carried as explicit arguments — never inferred from the
// executable search path and never inferred from a process working
// directory, because the harness may launch a hook command from anywhere.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func baseProvisionRequest(sb domain.Sandbox) domain.ProvisionRequest {
	return domain.ProvisionRequest{
		Sandbox:         sb,
		InterceptorPath: "/abs/path/to/mosaic-agent-test",
		InterceptorArgs: []string{"intercept", "--workspace", sb.SubjectDir, "--harness", claudecode.HarnessID},
	}
}

// TestBuildBridge_ExecutableIsInterceptorPathVerbatim asserts that the
// bridge's executable is exactly what the provision request already
// resolved — BuildBridge performs no further resolution of its own. A bare
// command name here would be resolved through the executable search path at
// spawn time, silently binding a stale copy elsewhere on the machine.
func TestBuildBridge_ExecutableIsInterceptorPathVerbatim(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	bridge, err := claudecode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge: %v", err)
	}

	if bridge.Executable != req.InterceptorPath {
		t.Errorf("BuildBridge: Executable = %q, want exactly req.InterceptorPath %q (no re-resolution)", bridge.Executable, req.InterceptorPath)
	}
}

// TestBuildBridge_ArgsCarryControlDirExplicitly asserts that the control
// directory reaches the interceptor as an explicit argument rather than
// being left for the interceptor to infer from its own working directory —
// the harness may launch the hook command from anywhere.
func TestBuildBridge_ArgsCarryControlDirExplicitly(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	bridge, err := claudecode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge: %v", err)
	}

	if !containsAdjacent(bridge.Args, "--control-dir", sb.ControlDir) {
		t.Errorf("BuildBridge: Args = %v, want an explicit --control-dir %q", bridge.Args, sb.ControlDir)
	}
}

// TestBuildBridge_ArgsCarryHarnessIdentity asserts the harness identity is
// present so a multi-adapter driver can tell which harness produced a call.
func TestBuildBridge_ArgsCarryHarnessIdentity(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	bridge, err := claudecode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge: %v", err)
	}

	if !containsAdjacent(bridge.Args, "--harness", claudecode.HarnessID) {
		t.Errorf("BuildBridge: Args = %v, want an explicit --harness %q", bridge.Args, claudecode.HarnessID)
	}
}

// TestBuildBridge_PhaseDistinguishesPreFromPost asserts each phase produces
// a bridge naming its own phase, so the interceptor knows which point it was
// invoked from without inferring it from context.
func TestBuildBridge_PhaseDistinguishesPreFromPost(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	pre, err := claudecode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePre): %v", err)
	}
	post, err := claudecode.BuildBridge(req, domain.PhasePost)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePost): %v", err)
	}

	if !containsAdjacent(pre.Args, "--phase", string(domain.PhasePre)) {
		t.Errorf("BuildBridge(PhasePre): Args = %v, want --phase %q", pre.Args, domain.PhasePre)
	}
	if !containsAdjacent(post.Args, "--phase", string(domain.PhasePost)) {
		t.Errorf("BuildBridge(PhasePost): Args = %v, want --phase %q", post.Args, domain.PhasePost)
	}
}

// TestBuildBridge_PreservesDriverSuppliedInterceptorArgs asserts that
// whatever arguments the driver already put in InterceptorArgs (the
// interceptor subcommand, the workspace argument) survive into the bridge
// unchanged, in their original order, ahead of this function's own
// additions.
func TestBuildBridge_PreservesDriverSuppliedInterceptorArgs(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	bridge, err := claudecode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge: %v", err)
	}

	if len(bridge.Args) < len(req.InterceptorArgs) {
		t.Fatalf("BuildBridge: Args = %v, shorter than driver-supplied InterceptorArgs %v", bridge.Args, req.InterceptorArgs)
	}
	for i, a := range req.InterceptorArgs {
		if bridge.Args[i] != a {
			t.Errorf("BuildBridge: Args[%d] = %q, want driver-supplied %q preserved in order", i, bridge.Args[i], a)
		}
	}
}

// TestBuildBridge_EmptyInterceptorPathIsError asserts that an unresolved
// interceptor path fails loudly rather than silently producing a bridge that
// invokes nothing.
func TestBuildBridge_EmptyInterceptorPathIsError(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)
	req.InterceptorPath = ""

	if _, err := claudecode.BuildBridge(req, domain.PhasePre); err == nil {
		t.Errorf("BuildBridge: expected an error for an empty InterceptorPath, got nil")
	}
}

// TestInterceptorEntries_PreIsSynchronousPostIsAsynchronous asserts the
// pre-invocation entry is registered synchronously — it must run and return
// before the tool call proceeds, since rewriting input only means anything
// before the call runs — while the post-invocation entry, which can only
// observe, is asynchronous.
func TestInterceptorEntries_PreIsSynchronousPostIsAsynchronous(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	pre, err := claudecode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePre): %v", err)
	}
	post, err := claudecode.BuildBridge(req, domain.PhasePost)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePost): %v", err)
	}

	contrib := claudecode.InterceptorEntries(pre, post)

	preMatchers, ok := contrib.Settings.Hooks["PreToolUse"]
	if !ok || len(preMatchers) == 0 || len(preMatchers[0].Hooks) == 0 {
		t.Fatalf("InterceptorEntries: no PreToolUse hook entry in %+v", contrib.Settings.Hooks)
	}
	preEntry := preMatchers[0].Hooks[0]
	if preEntry.Async == nil || *preEntry.Async {
		t.Errorf("InterceptorEntries: PreToolUse entry Async = %v, want a synchronous (false) entry", preEntry.Async)
	}

	postMatchers, ok := contrib.Settings.Hooks["PostToolUse"]
	if !ok || len(postMatchers) == 0 || len(postMatchers[0].Hooks) == 0 {
		t.Fatalf("InterceptorEntries: no PostToolUse hook entry in %+v", contrib.Settings.Hooks)
	}
	postEntry := postMatchers[0].Hooks[0]
	if postEntry.Async == nil || !*postEntry.Async {
		t.Errorf("InterceptorEntries: PostToolUse entry Async = %v, want an asynchronous (true) entry", postEntry.Async)
	}
}

// TestInterceptorEntries_RewritesInputAndMatchesInterceptedTool asserts the
// contribution declares itself as an input-rewriter (so the single-rewriter
// composition invariant can see it) and matches exactly the dispatch tool
// this adapter intercepts.
func TestInterceptorEntries_RewritesInputAndMatchesInterceptedTool(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	pre, err := claudecode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePre): %v", err)
	}
	post, err := claudecode.BuildBridge(req, domain.PhasePost)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePost): %v", err)
	}

	contrib := claudecode.InterceptorEntries(pre, post)

	if !contrib.RewritesInput {
		t.Errorf("InterceptorEntries: RewritesInput = false, want true")
	}

	preMatchers := contrib.Settings.Hooks["PreToolUse"]
	if len(preMatchers) == 0 || preMatchers[0].Matcher != claudecode.InterceptedToolName {
		t.Errorf("InterceptorEntries: PreToolUse matcher = %+v, want it to select %q", preMatchers, claudecode.InterceptedToolName)
	}
}

// TestCompletionEntry_SubagentStopIsAsyncAndObservational asserts
// CompletionEntry registers the harness's own completion signal — the
// SubagentStop hook — as an asynchronous, non-input-rewriting entry,
// matching PhaseCompletion's observation-only contract: this phase never
// substitutes, rewrites or halts.
func TestCompletionEntry_SubagentStopIsAsyncAndObservational(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	completion, err := claudecode.BuildBridge(req, domain.PhaseCompletion)
	if err != nil {
		t.Fatalf("BuildBridge(PhaseCompletion): %v", err)
	}

	contrib := claudecode.CompletionEntry(completion)

	if contrib.RewritesInput {
		t.Errorf("CompletionEntry: RewritesInput = true, want false (observation only)")
	}

	matchers, ok := contrib.Settings.Hooks["SubagentStop"]
	if !ok || len(matchers) == 0 || len(matchers[0].Hooks) == 0 {
		t.Fatalf("CompletionEntry: no SubagentStop hook entry in %+v", contrib.Settings.Hooks)
	}
	entry := matchers[0].Hooks[0]
	if entry.Async == nil || !*entry.Async {
		t.Errorf("CompletionEntry: SubagentStop entry Async = %v, want an asynchronous (true) entry", entry.Async)
	}
	if entry.Command != completion.Executable {
		t.Errorf("CompletionEntry: Command = %q, want the completion bridge's Executable %q", entry.Command, completion.Executable)
	}
}

// containsAdjacent reports whether flag immediately precedes value anywhere
// in args.
func containsAdjacent(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
