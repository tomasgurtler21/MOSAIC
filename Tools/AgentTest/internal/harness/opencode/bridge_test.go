package opencode_test

// Tests for interceptor bridge construction: the generated plugin must
// invoke this tool's interceptor entry point with the control directory,
// harness identity and interception phase carried as explicit arguments —
// never inferred from a process working directory, because the plugin runs
// inside the harness's own process, from wherever that process happens to
// be.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
)

func baseProvisionRequest(sb domain.Sandbox) domain.ProvisionRequest {
	return domain.ProvisionRequest{
		Sandbox:         sb,
		InterceptorPath: "/abs/path/to/mosaic-agent-test",
		InterceptorArgs: []string{"intercept", "--workspace", sb.SubjectDir, "--harness", opencode.HarnessID},
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

// TestBuildBridge_ExecutableIsInterceptorPathVerbatim asserts that the
// bridge's executable is exactly what the provision request already
// resolved — BuildBridge performs no further resolution of its own.
func TestBuildBridge_ExecutableIsInterceptorPathVerbatim(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	bridge, err := opencode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge: %v", err)
	}

	if bridge.Executable != req.InterceptorPath {
		t.Errorf("BuildBridge: Executable = %q, want exactly req.InterceptorPath %q (no re-resolution)", bridge.Executable, req.InterceptorPath)
	}
}

// TestBuildBridge_ArgsCarryControlDirExplicitly asserts that the control
// directory reaches the interceptor as an explicit argument, never left for
// the interceptor to infer from a process working directory.
func TestBuildBridge_ArgsCarryControlDirExplicitly(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	bridge, err := opencode.BuildBridge(req, domain.PhasePre)
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

	bridge, err := opencode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge: %v", err)
	}

	if !containsAdjacent(bridge.Args, "--harness", opencode.HarnessID) {
		t.Errorf("BuildBridge: Args = %v, want an explicit --harness %q", bridge.Args, opencode.HarnessID)
	}
}

// TestBuildBridge_PhaseDistinguishesPreFromPost asserts each phase produces
// a bridge naming its own phase explicitly, so the interceptor knows which
// point it was invoked from without inferring it from context.
func TestBuildBridge_PhaseDistinguishesPreFromPost(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	pre, err := opencode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePre): %v", err)
	}
	post, err := opencode.BuildBridge(req, domain.PhasePost)
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
// whatever arguments the driver already put in InterceptorArgs survive into
// the bridge unchanged, in their original order, ahead of this function's
// own additions.
func TestBuildBridge_PreservesDriverSuppliedInterceptorArgs(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)

	bridge, err := opencode.BuildBridge(req, domain.PhasePre)
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
// interceptor path fails loudly rather than silently producing a bridge
// that invokes nothing.
func TestBuildBridge_EmptyInterceptorPathIsError(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	req := baseProvisionRequest(sb)
	req.InterceptorPath = ""

	if _, err := opencode.BuildBridge(req, domain.PhasePre); err == nil {
		t.Errorf("BuildBridge: expected an error for an empty InterceptorPath, got nil")
	}
}
