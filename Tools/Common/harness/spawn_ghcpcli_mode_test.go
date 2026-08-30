package harness_test

// Tests for BuildGHCPCLIArgs with the two GHCP CLI permission modes
// (Blanket and Partial Allowlist) and the unresolved-mode error path.
//
// These tests are written in the TDD RED phase before the BuildGHCPCLIArgs
// two-mode implementation (I3.2). They reference the GHCPCLIPermissionMode
// type, constants, and sentinel errors defined in harness.go and
// spawn_ghcpcli.go (I3.1), so they compile once those contract additions are
// in place. The tests that assert new behavior (Partial Allowlist, unresolved
// mode) will fail at runtime until I3.2 modifies BuildGHCPCLIArgs to honour
// SpawnRequest.GHCPCLIMode.
//
// Blanket mode tests that mirror current unconditional behavior may happen to
// pass even before I3.2 (since the existing code always emits --yolo).
//
// Coverage:
//   - Blanket mode: --yolo and --no-ask-user always present, --allow-tool absent
//   - Blanket mode: DerivedTools is ignored (--yolo grants blanket permission)
//   - Blanket mode: exact argument order identical to pre-change behavior
//   - Partial Allowlist mode: --yolo absent; --allow-tool entries present
//   - Partial Allowlist mode: --no-ask-user always present
//   - Partial Allowlist mode: one --allow-tool entry per DerivedTools element
//   - Partial Allowlist mode: -p PROMPT remains the final two arguments
//   - Unresolved mode (zero value): returns ErrGHCPCLIModeUnresolved before any args are built
//   - Unresolved mode: returns nil slice on error
//   - Partial Allowlist mode with nil DerivedTools: returns ErrGHCPCLIAllowlistEmpty
//   - Partial Allowlist mode with empty DerivedTools slice: returns ErrGHCPCLIAllowlistEmpty
//   - New sentinels are errors.Is-distinguishable from each other and from existing sentinels

import (
	"errors"
	"reflect"
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// Blanket mode: identical to current behavior
// ---------------------------------------------------------------------------

// TestBuildGHCPCLIArgs_BlanketMode_EmitsYolo verifies that Blanket mode
// includes --yolo, preserving the unconditional current behavior.
func TestBuildGHCPCLIArgs_BlanketMode_EmitsYolo(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:       ordinaryAgent(),
		Prompt:      "hello",
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--yolo") {
		t.Errorf("want --yolo in Blanket mode args, got %v", args)
	}
}

// TestBuildGHCPCLIArgs_BlanketMode_EmitsNoAskUser verifies that Blanket mode
// includes --no-ask-user.
func TestBuildGHCPCLIArgs_BlanketMode_EmitsNoAskUser(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:       ordinaryAgent(),
		Prompt:      "hello",
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--no-ask-user") {
		t.Errorf("want --no-ask-user in Blanket mode args, got %v", args)
	}
}

// TestBuildGHCPCLIArgs_BlanketMode_DoesNotEmitAllowTool verifies that Blanket
// mode never emits --allow-tool entries. --yolo grants all permissions already.
func TestBuildGHCPCLIArgs_BlanketMode_DoesNotEmitAllowTool(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:       ordinaryAgent(),
		Prompt:      "hello",
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--allow-tool") {
		t.Errorf("want --allow-tool absent in Blanket mode, got %v", args)
	}
}

// TestBuildGHCPCLIArgs_BlanketMode_IgnoresDerivedTools verifies that Blanket
// mode does not emit --allow-tool entries even when DerivedTools is populated.
// --yolo already grants blanket permission regardless of which tools are listed.
func TestBuildGHCPCLIArgs_BlanketMode_IgnoresDerivedTools(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "hello",
		GHCPCLIMode:  harness.GHCPCLIModeBlanket,
		DerivedTools: []string{"write", "shell", "agent"},
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--allow-tool") {
		t.Errorf("want --allow-tool absent in Blanket mode even with DerivedTools set, got %v", args)
	}
	if !containsArg(args, "--yolo") {
		t.Errorf("want --yolo in Blanket mode even with DerivedTools set, got %v", args)
	}
}

// TestBuildGHCPCLIArgs_BlanketMode_FullArgumentOrder verifies the exact
// argument order for Blanket mode, which must be identical to the current
// --yolo behavior:
//
//	--output-format json --yolo --no-ask-user --agent NAME --model M [ExtraArgs...] -p PROMPT
func TestBuildGHCPCLIArgs_BlanketMode_FullArgumentOrder(t *testing.T) {
	agent := ordinaryAgent()
	req := harness.SpawnRequest{
		Agent:       agent,
		Prompt:      "the-prompt",
		Model:       "some-model",
		ExtraArgs:   []string{"--custom-flag", "custom-value"},
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"--output-format", "json",
		"--yolo",
		"--no-ask-user",
		"--agent", agent.Identifier,
		"--model", "some-model",
		"--custom-flag", "custom-value",
		"-p", "the-prompt",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("want exact argument order\n  want: %v\n  got:  %v", want, args)
	}
}

// ---------------------------------------------------------------------------
// Partial Allowlist mode: --allow-tool entries, no --yolo
// ---------------------------------------------------------------------------

// TestBuildGHCPCLIArgs_PartialAllowlist_NoYolo verifies that Partial Allowlist
// mode does not emit --yolo.
func TestBuildGHCPCLIArgs_PartialAllowlist_NoYolo(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "hello",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: []string{"write", "shell"},
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--yolo") {
		t.Errorf("want --yolo absent in Partial Allowlist mode, got %v", args)
	}
}

// TestBuildGHCPCLIArgs_PartialAllowlist_EmitsNoAskUser verifies that Partial
// Allowlist mode still emits --no-ask-user even without --yolo.
func TestBuildGHCPCLIArgs_PartialAllowlist_EmitsNoAskUser(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "hello",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: []string{"write"},
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--no-ask-user") {
		t.Errorf("want --no-ask-user in Partial Allowlist mode args, got %v", args)
	}
}

// TestBuildGHCPCLIArgs_PartialAllowlist_EmitsAllowToolForEachDerivedTool
// verifies that Partial Allowlist mode emits one --allow-tool entry per
// DerivedTools element.
func TestBuildGHCPCLIArgs_PartialAllowlist_EmitsAllowToolForEachDerivedTool(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "hello",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: []string{"shell", "write", "agent", "skill"},
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tool := range []string{"shell", "write", "agent", "skill"} {
		if !containsSequence(args, "--allow-tool", tool) {
			t.Errorf("want --allow-tool %s in Partial Allowlist args, got %v", tool, args)
		}
	}
}

// TestBuildGHCPCLIArgs_PartialAllowlist_AllowToolCountMatchesDerivedTools
// verifies that exactly one --allow-tool flag is emitted per DerivedTools
// entry: no duplicates, no missing entries.
func TestBuildGHCPCLIArgs_PartialAllowlist_AllowToolCountMatchesDerivedTools(t *testing.T) {
	tools := []string{"shell", "write", "agent"}
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "hello",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: tools,
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := 0
	for _, a := range args {
		if a == "--allow-tool" {
			count++
		}
	}
	if count != len(tools) {
		t.Errorf("want %d --allow-tool flags for %d DerivedTools entries, got %d in %v",
			len(tools), len(tools), count, args)
	}
}

// TestBuildGHCPCLIArgs_PartialAllowlist_PromptRemainsLast verifies that -p
// and the prompt value remain the final two arguments in Partial Allowlist mode.
func TestBuildGHCPCLIArgs_PartialAllowlist_PromptRemainsLast(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "partial-allowlist-prompt",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: []string{"write"},
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) < 2 {
		t.Fatalf("want at least 2 args, got %v", args)
	}
	if args[len(args)-2] != "-p" {
		t.Errorf("want -p as second-to-last arg, got %v", args)
	}
	if args[len(args)-1] != "partial-allowlist-prompt" {
		t.Errorf("want prompt as final arg, got %v", args)
	}
}

// TestBuildGHCPCLIArgs_PartialAllowlist_AllowToolPrecedesPrompt verifies that
// --allow-tool entries appear before -p PROMPT.
func TestBuildGHCPCLIArgs_PartialAllowlist_AllowToolPrecedesPrompt(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "the-prompt",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: []string{"write"},
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	allowToolIdx := indexOfArg(args, "--allow-tool")
	pIdx := indexOfArg(args, "-p")
	if allowToolIdx < 0 || pIdx < 0 {
		t.Fatalf("want --allow-tool and -p in args, got %v", args)
	}
	if allowToolIdx >= pIdx {
		t.Errorf("want --allow-tool to precede -p, got allowToolIdx=%d pIdx=%d in %v",
			allowToolIdx, pIdx, args)
	}
}

// TestBuildGHCPCLIArgs_PartialAllowlist_OutputFormatPresent verifies that
// --output-format json remains present in Partial Allowlist mode.
func TestBuildGHCPCLIArgs_PartialAllowlist_OutputFormatPresent(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "hello",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: []string{"write"},
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in Partial Allowlist args, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Error: unresolved mode (zero value)
// ---------------------------------------------------------------------------

// TestBuildGHCPCLIArgs_UnresolvedMode_ReturnsErrGHCPCLIModeUnresolved verifies
// that the zero value of GHCPCLIMode (GHCPCLIModeUnresolved) causes
// BuildGHCPCLIArgs to return ErrGHCPCLIModeUnresolved before producing any args.
func TestBuildGHCPCLIArgs_UnresolvedMode_ReturnsErrGHCPCLIModeUnresolved(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:  ordinaryAgent(),
		Prompt: "hello",
		// GHCPCLIMode is deliberately left as zero value (GHCPCLIModeUnresolved)
	}
	_, err := harness.BuildGHCPCLIArgs(req)
	if !errors.Is(err, harness.ErrGHCPCLIModeUnresolved) {
		t.Fatalf("want ErrGHCPCLIModeUnresolved when GHCPCLIMode is zero, got %v", err)
	}
}

// TestBuildGHCPCLIArgs_UnresolvedMode_ReturnsNilSlice verifies that the nil
// slice contract is preserved for the new unresolved-mode error path.
func TestBuildGHCPCLIArgs_UnresolvedMode_ReturnsNilSlice(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:  ordinaryAgent(),
		Prompt: "hello",
	}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err == nil {
		t.Fatal("want error for unresolved mode, got nil")
	}
	if args != nil {
		t.Errorf("want nil slice on unresolved-mode error, got %v", args)
	}
}

// TestBuildGHCPCLIArgs_UnresolvedMode_CheckPrecedesEmptyPromptCheck verifies
// the validation ordering: the mode check must occur before the empty-prompt
// check so that an unresolved mode is caught even when the prompt is also empty.
func TestBuildGHCPCLIArgs_UnresolvedMode_CheckPrecedesEmptyPromptCheck(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:  ordinaryAgent(),
		Prompt: "",
		// GHCPCLIMode is zero: both conditions hold; mode check must win.
	}
	_, err := harness.BuildGHCPCLIArgs(req)
	if !errors.Is(err, harness.ErrGHCPCLIModeUnresolved) {
		t.Fatalf("want ErrGHCPCLIModeUnresolved when both mode and prompt are unset, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Error: Partial Allowlist mode with empty DerivedTools
// ---------------------------------------------------------------------------

// TestBuildGHCPCLIArgs_PartialAllowlist_NilDerivedTools_ReturnsErrGHCPCLIAllowlistEmpty
// verifies that nil DerivedTools in Partial Allowlist mode returns
// ErrGHCPCLIAllowlistEmpty. Without at least one --allow-tool entry, the
// spawned process would have no tool permissions at all.
func TestBuildGHCPCLIArgs_PartialAllowlist_NilDerivedTools_ReturnsErrGHCPCLIAllowlistEmpty(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "hello",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: nil,
	}
	_, err := harness.BuildGHCPCLIArgs(req)
	if !errors.Is(err, harness.ErrGHCPCLIAllowlistEmpty) {
		t.Fatalf("want ErrGHCPCLIAllowlistEmpty for nil DerivedTools in Partial Allowlist mode, got %v", err)
	}
}

// TestBuildGHCPCLIArgs_PartialAllowlist_EmptyDerivedTools_ReturnsErrGHCPCLIAllowlistEmpty
// verifies that an empty (non-nil) DerivedTools slice also returns
// ErrGHCPCLIAllowlistEmpty in Partial Allowlist mode.
func TestBuildGHCPCLIArgs_PartialAllowlist_EmptyDerivedTools_ReturnsErrGHCPCLIAllowlistEmpty(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:        ordinaryAgent(),
		Prompt:       "hello",
		GHCPCLIMode:  harness.GHCPCLIModePartialAllowlist,
		DerivedTools: []string{},
	}
	_, err := harness.BuildGHCPCLIArgs(req)
	if !errors.Is(err, harness.ErrGHCPCLIAllowlistEmpty) {
		t.Fatalf("want ErrGHCPCLIAllowlistEmpty for empty DerivedTools slice in Partial Allowlist mode, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sentinel distinguishability
// ---------------------------------------------------------------------------

// TestBuildGHCPCLIArgs_NewSentinels_AreDistinguishableFromEachOther verifies
// that ErrGHCPCLIModeUnresolved and ErrGHCPCLIAllowlistEmpty do not satisfy
// each other's errors.Is checks.
func TestBuildGHCPCLIArgs_NewSentinels_AreDistinguishableFromEachOther(t *testing.T) {
	if errors.Is(harness.ErrGHCPCLIModeUnresolved, harness.ErrGHCPCLIAllowlistEmpty) {
		t.Error("ErrGHCPCLIModeUnresolved must not satisfy errors.Is for ErrGHCPCLIAllowlistEmpty")
	}
	if errors.Is(harness.ErrGHCPCLIAllowlistEmpty, harness.ErrGHCPCLIModeUnresolved) {
		t.Error("ErrGHCPCLIAllowlistEmpty must not satisfy errors.Is for ErrGHCPCLIModeUnresolved")
	}
}

// TestBuildGHCPCLIArgs_NewSentinels_DoNotCollideWithExistingSentinels verifies
// that the two new sentinels do not satisfy errors.Is checks for the existing
// ErrGHCPCLIUnsupportedOutputFormat and ErrGHCPCLIEmptyPrompt.
func TestBuildGHCPCLIArgs_NewSentinels_DoNotCollideWithExistingSentinels(t *testing.T) {
	existing := []error{
		harness.ErrGHCPCLIUnsupportedOutputFormat,
		harness.ErrGHCPCLIEmptyPrompt,
	}
	for _, e := range existing {
		if errors.Is(harness.ErrGHCPCLIModeUnresolved, e) {
			t.Errorf("ErrGHCPCLIModeUnresolved must not satisfy errors.Is for %v", e)
		}
		if errors.Is(harness.ErrGHCPCLIAllowlistEmpty, e) {
			t.Errorf("ErrGHCPCLIAllowlistEmpty must not satisfy errors.Is for %v", e)
		}
	}
}

// TestBuildGHCPCLIArgs_UnresolvedModeConstant_IsDistinguishableFromBlanket
// verifies that GHCPCLIModeUnresolved is distinct from GHCPCLIModeBlanket
// (they are different values, so requests with one mode do not behave like the other).
func TestBuildGHCPCLIArgs_UnresolvedModeConstant_IsDistinguishableFromBlanket(t *testing.T) {
	if harness.GHCPCLIModeUnresolved == harness.GHCPCLIModeBlanket {
		t.Error("GHCPCLIModeUnresolved must not equal GHCPCLIModeBlanket")
	}
	if harness.GHCPCLIModeUnresolved == harness.GHCPCLIModePartialAllowlist {
		t.Error("GHCPCLIModeUnresolved must not equal GHCPCLIModePartialAllowlist")
	}
	if harness.GHCPCLIModeBlanket == harness.GHCPCLIModePartialAllowlist {
		t.Error("GHCPCLIModeBlanket must not equal GHCPCLIModePartialAllowlist")
	}
}
