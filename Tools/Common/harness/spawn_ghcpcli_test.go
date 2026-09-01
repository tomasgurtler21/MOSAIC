package harness_test

// Tests for BuildGHCPCLIArgs: argument construction against the
// `copilot --output-format json --yolo --no-ask-user -p PROMPT` single-shot
// non-interactive contract. Mirrors spawn_opencode_test.go's style; reuses
// ordinaryAgent/orchestratorAgent fixtures and containsArg/containsSequence/
// indexOfArg helpers from helper_test.go since both live in this package.
//
// Coverage summary:
//   - Fixed flags (--output-format json, --yolo, --no-ask-user) always present
//   - Empty OutputFormat treated as "json"; explicit "json" accepted
//   - -p and prompt are the final two arguments for every valid request
//   - --agent emitted with the identifier when non-empty; absent entirely when empty
//     (an empty identifier is not an error for this harness — it selects the default persona)
//   - --model emitted only when Model is non-empty; omitted when empty
//   - ExtraArgs appear in caller-supplied order, after fixed flags and before -p
//   - SystemPrompt, DefinitionPath, MaxTurns, AllowedTools cannot affect output for any input
//   - -s, --resume, --continue, --name never emitted for any input
//   - Unsupported OutputFormat returns ErrGHCPCLIUnsupportedOutputFormat (errors.Is-distinguishable)
//   - Empty Prompt returns ErrGHCPCLIEmptyPrompt (errors.Is-distinguishable)
//   - When both conditions hold, output-format check takes precedence
//   - Error paths return a nil slice
//   - Purity: identical input yields identical output; returned slice does not alias req.ExtraArgs

import (
	"errors"
	"reflect"
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// Fixed flags always present for every valid request
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_AlwaysEmitsOutputFormatJSON(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "hello", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in args, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_AlwaysEmitsYolo(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "hello", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--yolo") {
		t.Errorf("want --yolo in Blanket mode args, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_AlwaysEmitsNoAskUser(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "hello", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--no-ask-user") {
		t.Errorf("want --no-ask-user in args, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_EmptyOutputFormatTreatedAsJSON(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "hello", OutputFormat: "", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error for empty OutputFormat: %v", err)
	}
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json when OutputFormat is empty, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_ExplicitJSONOutputFormatIsAccepted(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "hello", OutputFormat: "json", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error for explicit OutputFormat \"json\": %v", err)
	}
	if !containsSequence(args, "--output-format", "json") {
		t.Errorf("want --output-format json in args, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Prompt placement: -p flag and prompt value are the final two arguments
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_PromptCarriedByDashPFlag(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "unique-prompt-marker", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pIdx := indexOfArg(args, "-p")
	if pIdx < 0 || pIdx+1 >= len(args) {
		t.Fatalf("want -p <prompt> in args, got %v", args)
	}
	if args[pIdx+1] != "unique-prompt-marker" {
		t.Errorf("want -p value %q, got %q", "unique-prompt-marker", args[pIdx+1])
	}
}

func TestBuildGHCPCLIArgs_DashPAndPromptAreLastTwoArgs(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "the-prompt", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) < 2 {
		t.Fatalf("want at least 2 args, got %v", args)
	}
	if args[len(args)-2] != "-p" {
		t.Errorf("want -p as second-to-last arg, got %v", args)
	}
	if args[len(args)-1] != "the-prompt" {
		t.Errorf("want prompt as final arg, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// --agent flag: conditional on a non-empty identifier
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_AgentFlagEmittedWhenIdentifierNonEmpty(t *testing.T) {
	agent := ordinaryAgent()
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: agent, Prompt: "x", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args, got %v", agent.Identifier, args)
	}
}

func TestBuildGHCPCLIArgs_AgentFlagAbsentWhenIdentifierEmpty(t *testing.T) {
	// An empty agent identifier is not an error for this harness: the CLI
	// resolves an agent by name against its scoped directories, and an
	// absent --agent selects the default assistant persona.
	agent := ordinaryAgent()
	agent.Identifier = ""
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: agent, Prompt: "x", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: empty agent identifier must not be an error for ghcp-cli, got %v", err)
	}
	if containsArg(args, "--agent") {
		t.Errorf("want --agent absent when identifier is empty, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// --model flag: emitted only when Model is non-empty
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_ModelFlagOmittedWhenEmpty(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", Model: "", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--model") {
		t.Errorf("want --model absent when Model is empty, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_ModelFlagIncludedWhenSet(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", Model: "some-model", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--model", "some-model") {
		t.Errorf("want --model some-model in args, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// ExtraArgs: caller order preserved, positioned after fixed flags and before -p
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_ExtraArgsPrecedeDashPPrompt(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{
		Agent:       ordinaryAgent(),
		Prompt:      "the-prompt",
		ExtraArgs:   []string{"--custom-flag", "custom-value"},
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--custom-flag", "custom-value") {
		t.Fatalf("want ExtraArgs present in args, got %v", args)
	}
	extraIdx := indexOfArg(args, "--custom-flag")
	pIdx := indexOfArg(args, "-p")
	if extraIdx == -1 || pIdx == -1 {
		t.Fatalf("want --custom-flag and -p in args, got %v", args)
	}
	if extraIdx >= pIdx {
		t.Errorf("want ExtraArgs to precede -p, got extraIdx=%d pIdx=%d in %v", extraIdx, pIdx, args)
	}
}

func TestBuildGHCPCLIArgs_MultipleExtraArgsPreservedVerbatimAndInOrder(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{
		Agent:       ordinaryAgent(),
		Prompt:      "the-prompt",
		ExtraArgs:   []string{"--foo", "--bar", "baz"},
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fooIdx := indexOfArg(args, "--foo")
	barIdx := indexOfArg(args, "--bar")
	bazIdx := indexOfArg(args, "baz")
	if fooIdx == -1 || barIdx == -1 || bazIdx == -1 {
		t.Fatalf("want all ExtraArgs entries present in args, got %v", args)
	}
	if !(fooIdx < barIdx && barIdx < bazIdx) {
		t.Errorf("want ExtraArgs preserved in order --foo, --bar, baz, got %v", args)
	}
	pIdx := indexOfArg(args, "-p")
	if bazIdx >= pIdx {
		t.Errorf("want ExtraArgs to precede -p, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Ephemeral-session guarantee: forbidden flags never emitted for any input
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_NeverEmitsDashS(t *testing.T) {
	// -s was empirically verified to have no effect alongside --output-format json.
	// Its absence is a finding, not a gap: adding it would imply a behavioural
	// difference that does not exist.
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "-s") {
		t.Errorf("-s must never appear, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_NeverEmitsSessionContinuationFlags(t *testing.T) {
	// --resume, --continue, --name are never emitted; every invocation creates
	// a fresh session. This is a structural guarantee, not a conditional one.
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, forbidden := range []string{"--resume", "--continue", "--name"} {
		if containsArg(args, forbidden) {
			t.Errorf("%s must never appear, got %v", forbidden, args)
		}
	}
}

// ---------------------------------------------------------------------------
// Deliberately ignored fields: SystemPrompt, DefinitionPath, MaxTurns, AllowedTools
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_SystemPromptDoesNotAffectOutput(t *testing.T) {
	// GHCP CLI layers instructions from files it discovers itself; there is no
	// system-prompt injection flag, and none should be invented.
	withSP, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", SystemPrompt: "injected system prompt", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	withoutSP, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(withSP, withoutSP) {
		t.Errorf("want SystemPrompt to have no effect on args\n  with SystemPrompt: %v\n  without:          %v", withSP, withoutSP)
	}
	if containsArg(withSP, "injected system prompt") {
		t.Errorf("want SystemPrompt content absent from args, got %v", withSP)
	}
}

func TestBuildGHCPCLIArgs_DefinitionPathDoesNotAppearInArgs(t *testing.T) {
	// The CLI resolves an agent by name, not by file path; DefinitionPath is unused.
	agent := ordinaryAgent()
	agent.DefinitionPath = "/should/not/appear/anywhere.md"
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: agent, Prompt: "x", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, agent.DefinitionPath) {
		t.Errorf("want DefinitionPath never to appear in args, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_MaxTurnsDoesNotAffectOutput(t *testing.T) {
	// The CLI offers no turn-limit flag; MaxTurns is unused.
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", MaxTurns: 5, GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range args {
		if a == "5" {
			t.Errorf("want no value emitted for MaxTurns, got %v", args)
		}
	}
	if containsArg(args, "--max-turns") {
		t.Errorf("want no --max-turns flag emitted, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_AllowedToolsDoesNotAffectOutput(t *testing.T) {
	// AllowedTools is a separate field from DerivedTools and is never read by
	// BuildGHCPCLIArgs regardless of mode. In Blanket mode, --yolo already
	// grants all permissions; in Partial Allowlist mode, DerivedTools carries
	// the per-tool list. AllowedTools is left for AgentTest compatibility.
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", AllowedTools: []string{"some-tool"}, GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "some-tool") {
		t.Errorf("want AllowedTools content never to appear in args, got %v", args)
	}
	if containsArg(args, "--allowed-tools") || containsArg(args, "--allowedTools") {
		t.Errorf("want no allowed-tools flag emitted, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Error conditions: unsupported OutputFormat and empty Prompt
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_UnsupportedOutputFormatIsError(t *testing.T) {
	_, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "stream-json"})
	if !errors.Is(err, harness.ErrGHCPCLIUnsupportedOutputFormat) {
		t.Fatalf("want ErrGHCPCLIUnsupportedOutputFormat, got %v", err)
	}
}

func TestBuildGHCPCLIArgs_EmptyPromptIsError(t *testing.T) {
	// GHCPCLIMode must be set to Blanket so the mode check passes and the
	// empty-prompt check fires.
	_, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "", GHCPCLIMode: harness.GHCPCLIModeBlanket})
	if !errors.Is(err, harness.ErrGHCPCLIEmptyPrompt) {
		t.Fatalf("want ErrGHCPCLIEmptyPrompt, got %v", err)
	}
}

func TestBuildGHCPCLIArgs_ErrorPathReturnsNilSlice(t *testing.T) {
	args, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: ""})
	if err == nil {
		t.Fatal("want error for empty prompt, got nil")
	}
	if args != nil {
		t.Errorf("want nil slice on error, got %v", args)
	}
}

func TestBuildGHCPCLIArgs_OutputFormatCheckPrecedesEmptyPromptCheck(t *testing.T) {
	// When both an unsupported OutputFormat and an empty Prompt are present,
	// the output-format sentinel is returned (validation order per design).
	_, err := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "", OutputFormat: "text"})
	if !errors.Is(err, harness.ErrGHCPCLIUnsupportedOutputFormat) {
		t.Fatalf("want ErrGHCPCLIUnsupportedOutputFormat when both conditions hold, got %v", err)
	}
}

func TestBuildGHCPCLIArgs_TwoSentinelsAreDistinguishable(t *testing.T) {
	// Both sentinels must be errors.Is-distinguishable: neither must satisfy
	// the other's check.
	_, outputFmtErr := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "text"})
	_, emptyPromptErr := harness.BuildGHCPCLIArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: ""})
	if errors.Is(outputFmtErr, harness.ErrGHCPCLIEmptyPrompt) {
		t.Errorf("ErrGHCPCLIUnsupportedOutputFormat must not satisfy errors.Is for ErrGHCPCLIEmptyPrompt")
	}
	if errors.Is(emptyPromptErr, harness.ErrGHCPCLIUnsupportedOutputFormat) {
		t.Errorf("ErrGHCPCLIEmptyPrompt must not satisfy errors.Is for ErrGHCPCLIUnsupportedOutputFormat")
	}
}

// ---------------------------------------------------------------------------
// Purity: identical input, identical output; no aliasing of req.ExtraArgs
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_IdenticalInputYieldsIdenticalOutput(t *testing.T) {
	req := harness.SpawnRequest{
		Agent:       ordinaryAgent(),
		Prompt:      "same-prompt",
		Model:       "same-model",
		ExtraArgs:   []string{"--extra"},
		GHCPCLIMode: harness.GHCPCLIModeBlanket,
	}
	args1, err1 := harness.BuildGHCPCLIArgs(req)
	args2, err2 := harness.BuildGHCPCLIArgs(req)
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if !reflect.DeepEqual(args1, args2) {
		t.Errorf("want identical output for identical input, got\n  first:  %v\n  second: %v", args1, args2)
	}
}

func TestBuildGHCPCLIArgs_ReturnedSliceDoesNotAliasExtraArgs(t *testing.T) {
	extra := []string{"--original"}
	req := harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", ExtraArgs: extra, GHCPCLIMode: harness.GHCPCLIModeBlanket}
	args, err := harness.BuildGHCPCLIArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mutate ExtraArgs after the call; the returned slice must be unaffected.
	extra[0] = "--mutated"
	if containsArg(args, "--mutated") {
		t.Errorf("want returned slice to not alias req.ExtraArgs; mutation was visible in %v", args)
	}
}

// ---------------------------------------------------------------------------
// Full exact-order assertion
// ---------------------------------------------------------------------------

func TestBuildGHCPCLIArgs_FullArgumentOrder_AllOptionalFieldsPopulated(t *testing.T) {
	// Verifies the complete positional contract for Blanket mode:
	//   --output-format json --yolo --no-ask-user --agent NAME --model M [ExtraArgs...] -p PROMPT
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

func TestBuildGHCPCLIArgs_FullArgumentOrder_NoOptionalFields(t *testing.T) {
	// Verifies the minimal positional contract when all optional fields are absent
	// (Blanket mode):
	//   --output-format json --yolo --no-ask-user -p PROMPT
	agent := ordinaryAgent()
	agent.Identifier = ""
	req := harness.SpawnRequest{
		Agent:       agent,
		Prompt:      "minimal-prompt",
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
		"-p", "minimal-prompt",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("want exact argument order\n  want: %v\n  got:  %v", want, args)
	}
}
