package harness_test

// Tests for BuildOpenCodeArgs: argument construction against the `opencode
// run` CLI contract. Mirrors buildargs_test.go's style; reuses its
// ordinaryAgent/orchestratorAgent fixtures and containsArg/containsSequence/
// indexOfArg helpers from helper_test.go since both live in this package.

import (
	"errors"
	"reflect"
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// Required flags, both invocation kinds
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_Ordinary_StartsWithRunSubcommand(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "do the thing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) == 0 || args[0] != "run" {
		t.Errorf("want args to start with %q, got %v", "run", args)
	}
}

func TestBuildOpenCodeArgs_Orchestrator_StartsWithRunSubcommand(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "do the thing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) == 0 || args[0] != "run" {
		t.Errorf("want args to start with %q, got %v", "run", args)
	}
}

func TestBuildOpenCodeArgs_Ordinary_IncludesAgentFlag(t *testing.T) {
	agent := ordinaryAgent()
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: agent, Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args, got %v", agent.Identifier, args)
	}
}

func TestBuildOpenCodeArgs_Orchestrator_IncludesAgentFlag(t *testing.T) {
	agent := orchestratorAgent()
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: agent, Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--agent", agent.Identifier) {
		t.Errorf("want --agent %q in args, got %v", agent.Identifier, args)
	}
}

func TestBuildOpenCodeArgs_IncludesFormatJSON(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--format", "json") {
		t.Errorf("want --format json in args, got %v", args)
	}
}

func TestBuildOpenCodeArgs_FormatJSONEmittedEvenWhenOutputFormatEmpty(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--format", "json") {
		t.Errorf("want --format json in args even with empty OutputFormat, got %v", args)
	}
}

func TestBuildOpenCodeArgs_IncludesAutoFlag(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(args, "--auto") {
		t.Errorf("want --auto in args, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Ephemeral-session guarantee
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_Ordinary_NeverIncludesSessionReuseFlags(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, forbidden := range []string{"--session", "--continue", "--fork"} {
		if containsArg(args, forbidden) {
			t.Errorf("%s must never appear, got %v", forbidden, args)
		}
	}
}

func TestBuildOpenCodeArgs_Orchestrator_NeverIncludesSessionReuseFlags(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: orchestratorAgent(), Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, forbidden := range []string{"--session", "--continue", "--fork"} {
		if containsArg(args, forbidden) {
			t.Errorf("%s must never appear, got %v", forbidden, args)
		}
	}
}

// ---------------------------------------------------------------------------
// ExtraArgs are the final arguments (no positional message follows)
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_ExtraArgsAreFinalArguments(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{
		Agent:     ordinaryAgent(),
		Prompt:    "the-prompt",
		ExtraArgs: []string{"--custom-flag", "custom-value"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n := len(args)
	if n < 2 || args[n-2] != "--custom-flag" || args[n-1] != "custom-value" {
		t.Errorf("want ExtraArgs as the final arguments with nothing after them, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Model flag
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_ModelFlagOmittedWhenEmpty(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", Model: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--model") {
		t.Errorf("want --model absent when Model is empty, got %v", args)
	}
}

func TestBuildOpenCodeArgs_ModelFlagIncludedWhenSet(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", Model: "some-model"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSequence(args, "--model", "some-model") {
		t.Errorf("want --model some-model in args, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Error conditions
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_EmptyAgentIdentifierIsError(t *testing.T) {
	agent := ordinaryAgent()
	agent.Identifier = ""
	_, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: agent, Prompt: "x"})
	if !errors.Is(err, harness.ErrOpenCodeEmptyAgentIdentifier) {
		t.Fatalf("want ErrOpenCodeEmptyAgentIdentifier, got %v", err)
	}
}

func TestBuildOpenCodeArgs_UnsupportedOutputFormatIsError(t *testing.T) {
	_, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "stream-json"})
	if !errors.Is(err, harness.ErrOpenCodeUnsupportedOutputFormat) {
		t.Fatalf("want ErrOpenCodeUnsupportedOutputFormat, got %v", err)
	}
}

func TestBuildOpenCodeArgs_ExplicitJSONOutputFormatIsAccepted(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("unexpected error for explicit OutputFormat \"json\": %v", err)
	}
	if !containsSequence(args, "--format", "json") {
		t.Errorf("want --format json in args, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Purity: DefinitionPath, MaxTurns, AllowedTools are accepted but unused
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_IgnoresDefinitionPathContent(t *testing.T) {
	agent := ordinaryAgent()
	agent.DefinitionPath = "/should/not/appear/anywhere.md"
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: agent, Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, agent.DefinitionPath) {
		t.Errorf("want DefinitionPath never to appear in args, got %v", args)
	}
}

func TestBuildOpenCodeArgs_IgnoresMaxTurns(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", MaxTurns: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range args {
		if a == "5" {
			t.Errorf("want no flag/value emitted for MaxTurns, got %v", args)
		}
	}
	if containsArg(args, "--max-turns") {
		t.Errorf("want no --max-turns flag emitted, got %v", args)
	}
}

func TestBuildOpenCodeArgs_IgnoresAllowedTools(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: "x", AllowedTools: []string{"some-tool"}})
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
// Multiple ExtraArgs entries, preserved verbatim and in order
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_MultipleExtraArgsPreservedVerbatimAndInOrder(t *testing.T) {
	args, _, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{
		Agent:     ordinaryAgent(),
		Prompt:    "the-prompt",
		ExtraArgs: []string{"--foo", "--bar", "baz"},
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
	if bazIdx != len(args)-1 {
		t.Errorf("want the last ExtraArgs entry to be the final argument, got %v", args)
	}
}

// ---------------------------------------------------------------------------
// Boundary: empty prompt
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_EmptyPromptNoSystemPrompt_NoPositionalAndNilStdin(t *testing.T) {
	args, stdin, err := harness.BuildOpenCodeArgs(harness.SpawnRequest{Agent: ordinaryAgent(), Prompt: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) == 0 || args[len(args)-1] != "--auto" {
		t.Errorf("want no positional message (not even an empty string) after --auto, got %q", args)
	}
	if stdin != nil {
		t.Errorf("want nil stdin when there is no content to deliver, got %q", stdin)
	}
}

// ---------------------------------------------------------------------------
// Full exact-order assertion with multiple optional fields populated
// ---------------------------------------------------------------------------

func TestBuildOpenCodeArgs_FullArgumentOrder_AllOptionalFieldsPopulated(t *testing.T) {
	agent := ordinaryAgent()
	req := harness.SpawnRequest{
		Agent:        agent,
		Prompt:       "the-prompt",
		Model:        "some-model",
		SystemPrompt: "the-system-prompt",
		ExtraArgs:    []string{"--custom-flag", "custom-value"},
	}
	args, stdin, err := harness.BuildOpenCodeArgs(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "the-system-prompt\nthe-prompt"; string(stdin) != want {
		t.Errorf("want stdin %q, got %q", want, stdin)
	}
	want := []string{
		"run",
		"--agent", agent.Identifier,
		"--format", "json",
		"--auto",
		"--model", "some-model",
		"--custom-flag", "custom-value",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("want exact argument order %v, got %v", want, args)
	}
}
