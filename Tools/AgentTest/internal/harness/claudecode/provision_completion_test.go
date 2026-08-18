package claudecode_test

// Regression coverage for a defect implementation-review#88 found and
// implementation-tdd#89 fixed: translateCompletion existed and was covered
// by unit and conformance tests, but the completion signal was never
// actually registered with the harness, so PhaseCompletion was unreachable
// in a real run — every existing test drove TranslateCall/Decide directly
// and none exercised hook registration itself.
//
// This pins the fix at the Provision level, the only place "translated but
// never delivered" can be caught: a SupportsReplyRecovery=true adapter's
// generated configuration must actually contain a hook for the completion
// event, not merely be able to translate one if it ever arrived.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func TestProvision_ReplyRecoveryCapableAdapterRegistersCompletionHook(t *testing.T) {
	if !claudecode.Capabilities().SupportsReplyRecovery {
		t.Fatalf("precondition: claudecode.Capabilities().SupportsReplyRecovery = false, want true — this test is regression coverage for that declared capability's registration")
	}

	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := claudecode.New(claudecode.Options{})

	req := domain.ProvisionRequest{
		Sandbox:         sb,
		InterceptorPath: "/abs/path/to/mosaic-agent-test",
		InterceptorArgs: []string{"intercept"},
	}
	if _, err := adapter.Provision(testContext(), req); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	settingsPath := filepath.Join(sb.SubjectDir, claudecode.SettingsRelPath)
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading generated settings: %v", err)
	}

	settings, err := claudecode.ParseFragment(string(data))
	if err != nil {
		t.Fatalf("parsing generated settings: %v", err)
	}

	matchers, ok := settings.Hooks["SubagentStop"]
	if !ok || len(matchers) == 0 || len(matchers[0].Hooks) == 0 {
		t.Fatalf("Provision: generated settings has no SubagentStop hook entry (%+v) — a SupportsReplyRecovery=true adapter must register the completion event in its output, not merely translate it if one arrived", settings.Hooks)
	}

	entry := matchers[0].Hooks[0]
	if !containsAdjacent(entry.Args, "--phase", string(domain.PhaseCompletion)) {
		t.Errorf("Provision: SubagentStop entry Args = %v, want an explicit --phase %q", entry.Args, domain.PhaseCompletion)
	}
	if entry.Async == nil || !*entry.Async {
		t.Errorf("Provision: SubagentStop entry Async = %v, want an asynchronous (true) entry (observation only)", entry.Async)
	}
}
