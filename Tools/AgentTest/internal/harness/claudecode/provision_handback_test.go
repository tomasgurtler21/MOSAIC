package claudecode_test

// Registration coverage for the subagent hand-back: the generated settings
// register a PostToolUse hook matched on SubagentHandback, routed to the
// completion bridge, observation only, and never on PreToolUse. Every
// pre-existing entry stays as it was.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func handbackBridge() claudecode.Bridge {
	return claudecode.Bridge{
		Executable: "/abs/path/to/mosaic-agent-test",
		Args:       []string{"intercept", "--control-dir", "/ctl", "--harness", claudecode.HarnessID, "--phase", string(domain.PhaseCompletion)},
	}
}

func TestHandbackEntry_IsPostToolUseOnlyMatchedOnHandbackAndObservationOnly(t *testing.T) {
	bridge := handbackBridge()

	c := claudecode.HandbackEntry(bridge)

	if c.RewritesInput {
		t.Errorf("RewritesInput = true, want false: only one rewriter is allowed and the pre entry is it")
	}
	if c.Source != "interceptor" {
		t.Errorf("Source = %q, want %q", c.Source, "interceptor")
	}
	if len(c.Settings.Hooks) != 1 {
		t.Fatalf("hook events = %v, want exactly PostToolUse", c.Settings.Hooks)
	}
	if _, ok := c.Settings.Hooks["PreToolUse"]; ok {
		t.Errorf("hand-back registered on PreToolUse; it must never be (it fires before delivery)")
	}
	matchers := c.Settings.Hooks["PostToolUse"]
	if len(matchers) != 1 || len(matchers[0].Hooks) != 1 {
		t.Fatalf("PostToolUse matchers = %+v, want one matcher group with one entry", matchers)
	}
	if matchers[0].Matcher != claudecode.HandbackToolName {
		t.Errorf("Matcher = %q, want %q", matchers[0].Matcher, claudecode.HandbackToolName)
	}
	e := matchers[0].Hooks[0]
	if e.Type != "command" || e.Command != bridge.Executable {
		t.Errorf("entry = %+v, want a command entry running the bridge executable", e)
	}
	if !containsAdjacent(e.Args, "--phase", string(domain.PhaseCompletion)) {
		t.Errorf("entry Args = %v, want --phase %q", e.Args, domain.PhaseCompletion)
	}
}

func TestHandbackEntry_ComposesAlongsideExistingEntriesWithoutSecondRewriter(t *testing.T) {
	composed, err := claudecode.Compose(
		claudecode.InterceptorEntries(handbackBridge(), handbackBridge()),
		claudecode.CompletionEntry(handbackBridge()),
		claudecode.HandbackEntry(handbackBridge()),
	)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	seen := map[string]bool{}
	for _, m := range composed.Hooks["PostToolUse"] {
		seen[m.Matcher] = true
	}
	for _, want := range []string{claudecode.InterceptedToolName, claudecode.HandbackToolName} {
		if !seen[want] {
			t.Errorf("composed PostToolUse matchers = %v, missing %q", seen, want)
		}
	}
}

func TestProvision_RegistersHandbackPostToolUseHookOnCompletionBridge(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	adapter := claudecode.New(claudecode.Options{})
	req := domain.ProvisionRequest{Sandbox: sb, InterceptorPath: "/abs/path/to/mosaic-agent-test", InterceptorArgs: []string{"intercept"}}

	if _, err := adapter.Provision(testContext(), req); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(sb.SubjectDir, claudecode.SettingsRelPath))
	if err != nil {
		t.Fatalf("reading generated settings: %v", err)
	}
	settings, err := claudecode.ParseFragment(string(data))
	if err != nil {
		t.Fatalf("parsing generated settings: %v", err)
	}

	var handback *claudecode.Matcher
	for i, m := range settings.Hooks["PostToolUse"] {
		if m.Matcher == claudecode.HandbackToolName {
			handback = &settings.Hooks["PostToolUse"][i]
		}
	}
	if handback == nil || len(handback.Hooks) == 0 {
		t.Fatalf("generated PostToolUse matchers = %+v, want one matched on %q", settings.Hooks["PostToolUse"], claudecode.HandbackToolName)
	}
	e := handback.Hooks[0]
	if !containsAdjacent(e.Args, "--phase", string(domain.PhaseCompletion)) {
		t.Errorf("hand-back entry Args = %v, want --phase %q", e.Args, domain.PhaseCompletion)
	}
	if !containsAdjacent(e.Args, "--control-dir", sb.ControlDir) {
		t.Errorf("hand-back entry Args = %v, want the explicit control directory %q", e.Args, sb.ControlDir)
	}

	for _, m := range settings.Hooks["PreToolUse"] {
		if m.Matcher == claudecode.HandbackToolName {
			t.Errorf("hand-back registered on PreToolUse: %+v", m)
		}
	}

	// Pre-existing registrations are unchanged.
	if pre := settings.Hooks["PreToolUse"]; len(pre) != 1 || pre[0].Matcher != claudecode.InterceptedToolName {
		t.Errorf("PreToolUse = %+v, want exactly the one dispatch-tool entry", pre)
	}
	for _, event := range []string{"SubagentStop", "SubagentStart"} {
		if len(settings.Hooks[event]) != 1 {
			t.Errorf("%s matchers = %+v, want the single pre-existing entry", event, settings.Hooks[event])
		}
	}
	found := false
	for _, m := range settings.Hooks["PostToolUse"] {
		if m.Matcher == claudecode.InterceptedToolName {
			found = true
		}
	}
	if !found {
		t.Errorf("PostToolUse lost the dispatch-tool entry: %+v", settings.Hooks["PostToolUse"])
	}
}
