package claudecode_test

// Tests for structural composition of the workspace's hook configuration
// (T10.1) and for the single-rewriter invariant (T10.2).
//
// Composition must go through the parsed registration model — never through
// text splicing of the published fragment — because the single-rewriter
// invariant is only expressible over a parsed model and byte determinism is
// only assertable over a re-serialized one. The golden file in this test is
// the decisive property for T10.1: a test tool that generates configuration
// it cannot itself assert on is trusting the most fragile step of setup to
// luck.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/harness/claudecode"
)

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

// interceptorContribution builds the interceptor's own registration
// contribution directly (bypassing the not-yet-implemented
// claudecode.InterceptorEntries), so this test exercises Compose and
// Marshal against a fixed, known-good input.
func interceptorContribution() claudecode.Contribution {
	return claudecode.Contribution{
		Source:        "interceptor",
		RewritesInput: true,
		Settings: claudecode.Settings{
			Hooks: map[string][]claudecode.Matcher{
				"PreToolUse": {
					{
						Matcher: "Task",
						Hooks: []claudecode.Entry{
							{
								Type:    "command",
								Command: "/abs/path/to/interceptor",
								Args:    []string{"intercept", "--phase", "pre"},
								Async:   boolPtr(false),
							},
						},
					},
				},
			},
		},
	}
}

func loadFragment(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "claudecode", "fixtures", name))
	if err != nil {
		t.Fatalf("registration: reading fixture %q: %v", name, err)
	}
	return string(data)
}

// TestParseFragment_DecodesIntoModel asserts that the bundle's published
// registration fragment is parsed into the registration model, never left
// as opaque text.
func TestParseFragment_DecodesIntoModel(t *testing.T) {
	raw := loadFragment(t, "fragment.json")

	settings, err := claudecode.ParseFragment(raw)
	if err != nil {
		t.Fatalf("ParseFragment: %v", err)
	}

	if len(settings.Hooks) != 2 {
		t.Fatalf("ParseFragment: got %d event keys, want 2 (PreToolUse, SessionStart); settings=%#v", len(settings.Hooks), settings)
	}
	if _, ok := settings.Hooks["PreToolUse"]; !ok {
		t.Errorf("ParseFragment: missing PreToolUse event key")
	}
	if _, ok := settings.Hooks["SessionStart"]; !ok {
		t.Errorf("ParseFragment: missing SessionStart event key")
	}
}

// TestParseFragment_MalformedIsError asserts that a malformed fragment is a
// diagnosable error, never a panic and never a silently empty model.
func TestParseFragment_MalformedIsError(t *testing.T) {
	raw := loadFragment(t, "fragment-malformed.json")

	_, err := claudecode.ParseFragment(raw)
	if err == nil {
		t.Fatalf("ParseFragment: want error for malformed fragment, got nil")
	}
}

// TestCompose_CombinesPerEventKey_RewriterFirst asserts the composition
// rule: entries are combined per event key, the interceptor's rewriting
// entry ordered first, event keys present in only one contribution survive
// intact including their synchronous/asynchronous flag, and the whole
// document is serialized exactly once — byte-identical to a golden file for
// a given bundle version.
func TestCompose_CombinesPerEventKey_RewriterFirst(t *testing.T) {
	bundleSettings, err := claudecode.ParseFragment(loadFragment(t, "fragment.json"))
	if err != nil {
		t.Fatalf("ParseFragment: %v", err)
	}

	bundleContribution := claudecode.Contribution{
		Source:        "fixture-logger",
		RewritesInput: false,
		Settings:      bundleSettings,
	}

	composed, err := claudecode.Compose(interceptorContribution(), bundleContribution)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	// PreToolUse: present in both sources. The interceptor's rewriting
	// matcher block must be ordered first, the bundle's observational block
	// after.
	pre, ok := composed.Hooks["PreToolUse"]
	if !ok {
		t.Fatalf("Compose: composed settings missing PreToolUse")
	}
	if len(pre) != 2 {
		t.Fatalf("Compose: PreToolUse has %d matcher blocks, want 2 (one per contribution)", len(pre))
	}
	if pre[0].Matcher != "Task" {
		t.Errorf("Compose: PreToolUse[0].Matcher = %q, want %q (interceptor's rewriting block first)", pre[0].Matcher, "Task")
	}
	if len(pre[0].Hooks) != 1 || pre[0].Hooks[0].Command != "/abs/path/to/interceptor" {
		t.Errorf("Compose: PreToolUse[0] is not the interceptor's rewriting entry: %#v", pre[0])
	}
	if len(pre[1].Hooks) != 1 || pre[1].Hooks[0].Command != "python3" {
		t.Errorf("Compose: PreToolUse[1] is not the bundle's observational entry: %#v", pre[1])
	}
	// The bundle's asynchronous flag and timeout must survive composition
	// unnormalized.
	if pre[1].Hooks[0].Async == nil || !*pre[1].Hooks[0].Async {
		t.Errorf("Compose: bundle's PreToolUse entry lost its async flag: %#v", pre[1].Hooks[0])
	}
	if pre[1].Hooks[0].Timeout == nil || *pre[1].Hooks[0].Timeout != 30 {
		t.Errorf("Compose: bundle's PreToolUse entry lost its timeout: %#v", pre[1].Hooks[0])
	}
	// The interceptor's own entry must equally survive unnormalized: it was
	// authored synchronous (Async false) so it can rewrite input before the
	// tool runs.
	if pre[0].Hooks[0].Async == nil || *pre[0].Hooks[0].Async {
		t.Errorf("Compose: interceptor's PreToolUse entry lost its synchronous flag: %#v", pre[0].Hooks[0])
	}

	// SessionStart: present only in the bundle contribution. It must survive
	// intact, not be dropped and not be merged with anything.
	start, ok := composed.Hooks["SessionStart"]
	if !ok {
		t.Fatalf("Compose: composed settings missing SessionStart (present in only one contribution)")
	}
	if len(start) != 1 {
		t.Fatalf("Compose: SessionStart has %d matcher blocks, want 1", len(start))
	}

	// The composed document is serialized exactly once, byte-deterministically,
	// and must match the golden file for this fixture's inputs.
	got, err := claudecode.Marshal(composed)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "claudecode", "golden", "composed-settings.json"))
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	// Normalize line endings before comparing: json.Marshal always emits LF,
	// but the golden file on disk may have CRLF on machines with
	// core.autocrlf=true. Normalizing here keeps the comparison byte-exact
	// for content while remaining portable across checkout configurations.
	goldenStr := strings.ReplaceAll(string(golden), "\r\n", "\n")
	if string(got) != goldenStr {
		t.Errorf("Marshal output does not match golden file.\n--- got ---\n%s\n--- want ---\n%s", got, golden)
	}

	// Byte determinism: re-marshaling the same model twice must be
	// byte-identical, so this step is golden-file testable at all.
	got2, err := claudecode.Marshal(composed)
	if err != nil {
		t.Fatalf("Marshal (second call): %v", err)
	}
	if string(got) != string(got2) {
		t.Errorf("Marshal is not deterministic across repeated calls:\nfirst:  %s\nsecond: %s", got, got2)
	}
}

// TestCompose_OrderingIsDecidedByRewritesInputTagNotArgumentPosition asserts
// that the rewriter-first ordering rule is driven by each contribution's
// RewritesInput tag, not by the order contributions are passed to Compose:
// passing the bundle (observational) contribution before the interceptor's
// (rewriting) contribution must still yield the interceptor's entry first
// in the composed PreToolUse block.
func TestCompose_OrderingIsDecidedByRewritesInputTagNotArgumentPosition(t *testing.T) {
	bundleSettings, err := claudecode.ParseFragment(loadFragment(t, "fragment.json"))
	if err != nil {
		t.Fatalf("ParseFragment: %v", err)
	}
	bundleContribution := claudecode.Contribution{
		Source:        "fixture-logger",
		RewritesInput: false,
		Settings:      bundleSettings,
	}

	// Bundle (observational) passed first, interceptor (rewriting) second —
	// the reverse of TestCompose_CombinesPerEventKey_RewriterFirst.
	composed, err := claudecode.Compose(bundleContribution, interceptorContribution())
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	pre, ok := composed.Hooks["PreToolUse"]
	if !ok {
		t.Fatalf("Compose: composed settings missing PreToolUse")
	}
	if len(pre) != 2 {
		t.Fatalf("Compose: PreToolUse has %d matcher blocks, want 2 (one per contribution)", len(pre))
	}
	if pre[0].Matcher != "Task" || len(pre[0].Hooks) != 1 || pre[0].Hooks[0].Command != "/abs/path/to/interceptor" {
		t.Errorf("Compose: PreToolUse[0] is not the interceptor's rewriting entry even though it was passed second: %#v", pre[0])
	}
	if len(pre[1].Hooks) != 1 || pre[1].Hooks[0].Command != "python3" {
		t.Errorf("Compose: PreToolUse[1] is not the bundle's observational entry even though it was passed first: %#v", pre[1])
	}
}

// TestMarshal_ValidJSON asserts that Marshal's output is at minimum valid,
// parseable JSON — a precondition for byte-determinism to mean anything.
func TestMarshal_ValidJSON(t *testing.T) {
	s := claudecode.Settings{
		Hooks: map[string][]claudecode.Matcher{
			"Stop": {{Hooks: []claudecode.Entry{{Type: "command", Command: "x", Async: boolPtr(true), Timeout: intPtr(5)}}}},
		},
	}
	b, err := claudecode.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("Marshal produced invalid JSON: %v\noutput: %s", err, b)
	}
}

// TestCompose_MultipleRewritersFails is the single-rewriter invariant test
// (T10.2). Because composition holds every entry that will exist in the
// sandbox at one moment, setup can assert directly that at most one entry
// across the composed set rewrites the intercepted call's input. A composed
// set containing two rewriting entries must fail before any agent is
// spawned, with a message naming the competing entries.
func TestCompose_MultipleRewritersFails(t *testing.T) {
	first := claudecode.Contribution{
		Source:        "interceptor",
		RewritesInput: true,
		Settings: claudecode.Settings{
			Hooks: map[string][]claudecode.Matcher{
				"PreToolUse": {{Matcher: "Task", Hooks: []claudecode.Entry{{Type: "command", Command: "/abs/interceptor"}}}},
			},
		},
	}
	second := claudecode.Contribution{
		Source:        "some-other-plugin",
		RewritesInput: true,
		Settings: claudecode.Settings{
			Hooks: map[string][]claudecode.Matcher{
				"PreToolUse": {{Matcher: "Task", Hooks: []claudecode.Entry{{Type: "command", Command: "/abs/other-rewriter"}}}},
			},
		},
	}

	_, err := claudecode.Compose(first, second)
	if err == nil {
		t.Fatalf("Compose: want error when two contributions rewrite the same event key, got nil")
	}
	if !errors.Is(err, claudecode.ErrMultipleRewriters) {
		t.Errorf("Compose: error %v does not wrap ErrMultipleRewriters", err)
	}
	if !strings.Contains(err.Error(), "interceptor") || !strings.Contains(err.Error(), "some-other-plugin") {
		t.Errorf("Compose: error message %q does not name both competing contributions", err.Error())
	}
}

// TestCompose_SingleRewriterAcrossDifferentEventKeysSucceeds asserts that
// the invariant is scoped to "rewrites the intercepted call's input", not
// to "more than one hook exists": the logger's entries are asynchronous and
// observational and must coexist with the interceptor's single rewriting
// entry, since cost capture depends on them.
func TestCompose_SingleRewriterAcrossDifferentEventKeysSucceeds(t *testing.T) {
	rewriter := interceptorContribution()
	observer := claudecode.Contribution{
		Source:        "fixture-logger",
		RewritesInput: false,
		Settings: claudecode.Settings{
			Hooks: map[string][]claudecode.Matcher{
				"PreToolUse":   {{Hooks: []claudecode.Entry{{Type: "command", Command: "python3", Async: boolPtr(true), Timeout: intPtr(30)}}}},
				"SessionStart": {{Hooks: []claudecode.Entry{{Type: "command", Command: "python3", Async: boolPtr(true), Timeout: intPtr(30)}}}},
			},
		},
	}

	if _, err := claudecode.Compose(rewriter, observer); err != nil {
		t.Fatalf("Compose: want success when only one contribution rewrites input, got error: %v", err)
	}
}
