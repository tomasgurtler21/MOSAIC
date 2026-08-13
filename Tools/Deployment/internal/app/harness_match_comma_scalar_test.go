package app

// harness_match_comma_scalar_test.go covers Stage 6 DetectHarnessMatch behaviour for
// harnesses (like claude-code) whose tools key is a MOSAIC generic vocabulary key and whose
// deployed files render the tools field as a comma-separated scalar.
//
// Two independent halves are specified here:
//
// T6.2 — Positive match via comma-scalar tool names:
//   A file deployed for a claude-code-like harness carries its tools as a KindScalar
//   (e.g. "tools: Read, Write, Edit, Bash, Glob, Grep"). Before I6.1 this yields
//   HarnessMatchIndeterminate because ExtractToolEntries returns nil for any non-KindList
//   value; after I6.1 the comma-scalar is parsed, Universe matches are found, and the
//   verdict is HarnessMatchYes.
//
//   Also verified: a file that genuinely carries no distinguishing signal (no tools field,
//   no harness-exclusive frontmatter keys) still returns HarnessMatchIndeterminate, so the
//   fix does not collapse the no-evidence branch.
//
// T6.3 — Revised indeterminate reason text:
//   The no-evidence indeterminate branch of DetectHarnessMatch currently carries a reason
//   that can read as alarming. Stage 6 revises it to be clearly informational: it states
//   that no distinguishing signal was found and that the transform proceeded normally.
//   This test pins the exact new wording; it is RED until I6.2 is implemented.
//
// Both test groups are in package app (not package app_test) to share the retargetTestModule
// and byte-fixture helpers defined in retarget_helpers_test.go.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Claude-code-like harness module
// ---------------------------------------------------------------------------

// newClaudeCodeLikeModule returns a retargetTestModule that mirrors the key structural
// properties of the claude-code built-in harness:
//
//   - ModelKey: "model" — a MOSAIC generic vocabulary key. The field classifier excludes
//     MOSAIC keys from harnessKeys, so no ClassHarness evidence is possible from this key.
//   - ToolsKey: "tools" — also a MOSAIC generic vocabulary key, excluded for the same reason.
//   - No FrontmatterSpec.Add entries with harness-distinctive (non-MOSAIC) keys.
//   - Universe: the standard claude-code tool names, matching the comma-separated list rendered
//     in the golden fixture Tools/Deployment/testdata/golden/claude-code/orchestrator.md.
//
// With no ClassHarness keys, the only path to hasPositive is through Universe tool-name
// matching. Before I6.1 that path is dead for comma-scalar files (ExtractToolEntries returns
// nil). After I6.1 the comma-scalar is parsed and Universe names are matched.
func newClaudeCodeLikeModule() *retargetTestModule {
	return &retargetTestModule{
		d: domain.HarnessDescriptor{
			ID:          "claude-code-like",
			DisplayName: "Claude Code Like",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model", // MOSAIC generic key — excluded from harnessKeys
				ToolsKey: "tools", // MOSAIC generic key — excluded from harnessKeys
				// No Add entries: no harness-distinctive frontmatter fields are declared.
				// This ensures the only positive-evidence path is through Universe tool names.
			},
			Tools: domain.ToolSpec{
				// Universe matches the tools listed in the claude-code golden fixture.
				Universe: []domain.HarnessTool{
					{Name: "Read"},
					{Name: "Write"},
					{Name: "Edit"},
					{Name: "Bash"},
					{Name: "Glob"},
					{Name: "Grep"},
					{Name: "Task"},
					{Name: "TaskStop"},
					{Name: "AskUserQuestion"},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Byte fixtures
// ---------------------------------------------------------------------------

// claudeCodeDeployedAgentBytes returns bytes of a minimal deployed agent where the tools
// field is a comma-separated scalar — the format the claude-code built-in harness renders,
// as confirmed by Tools/Deployment/testdata/golden/claude-code/orchestrator.md line 9:
//
//	tools: Read, Write, Edit, Bash, Glob, Grep, Task, TaskStop, AskUserQuestion
//
// All frontmatter keys in this file are MOSAIC generic vocabulary keys (transform_version,
// injections_version, version, id, name, description, role, model, tools), so the field
// classifier contributes no ClassHarness or ClassUnknown evidence. Before I6.1 the comma-
// scalar tools value is not parsed and hasPositive stays false — verdict: Indeterminate.
// After I6.1 ExtractToolEntries splits the scalar, finds Universe names, and sets
// hasPositive=true — verdict: HarnessMatchYes.
func claudeCodeDeployedAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"11\"\n" +
		"name: claude-code-agent\n" +
		"description: An agent deployed by the claude-code harness\n" +
		"role: subagent\n" +
		"model: claude-sonnet-4-5\n" +
		"tools: Read, Write, Edit, Bash, Glob, Grep\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the ClaudeCodeAgent.\n" +
		"[[/SECTION:Identity]]\n")
}

// ---------------------------------------------------------------------------
// T6.2 — Positive match via comma-scalar tool names
// ---------------------------------------------------------------------------

// TestDetectHarnessMatch_ClaudeCodeCommaScalar_YieldsPositiveMatch verifies that a deployed
// agent file whose tools field is a comma-separated scalar (the claude-code rendering) produces
// HarnessMatchYes when checked against the matching claude-code-like descriptor, once
// ExtractToolEntries is extended to parse comma-scalars (I6.1).
//
// Before I6.1: ExtractToolEntries returns nil for the KindScalar tools value →
// no Universe names matched → hasPositive=false → HarnessMatchIndeterminate (the observed
// alarming warning). This test is RED until I6.1 is implemented.
//
// After I6.1: ExtractToolEntries splits "Read, Write, Edit, Bash, Glob, Grep" on commas →
// each trimmed name is in the Universe → hasPositive=true, hasNegative=false →
// HarnessMatchYes.
func TestDetectHarnessMatch_ClaudeCodeCommaScalar_YieldsPositiveMatch(t *testing.T) {
	src := claudeCodeDeployedAgentBytes()
	ccModule := newClaudeCodeLikeModule()

	verdict := DetectHarnessMatch(src, ccModule, domain.ArtifactAgent, "claude-code-agent")

	if verdict.Status != HarnessMatchYes {
		t.Errorf("DetectHarnessMatch status = %q, want %q; "+
			"a claude-code-deployed file with a comma-scalar tools field should yield "+
			"HarnessMatchYes once ExtractToolEntries parses the scalar (I6.1); "+
			"currently ExtractToolEntries returns nil for KindScalar → hasPositive=false → "+
			"HarnessMatchIndeterminate; Reason: %q",
			verdict.Status, HarnessMatchYes, verdict.Reason)
	}
	if verdict.Reason != "" {
		t.Errorf("HarnessMatchYes Reason must be empty, got %q", verdict.Reason)
	}
}

// TestDetectHarnessMatch_ClaudeCodeCommaScalar_EvidenceContainsToolName verifies that after
// the comma-scalar fix the Evidence list names at least one matched tool name, making the
// detection auditable.
func TestDetectHarnessMatch_ClaudeCodeCommaScalar_EvidenceContainsToolName(t *testing.T) {
	src := claudeCodeDeployedAgentBytes()
	ccModule := newClaudeCodeLikeModule()

	verdict := DetectHarnessMatch(src, ccModule, domain.ArtifactAgent, "claude-code-agent")

	if verdict.Status != HarnessMatchYes {
		t.Fatalf("expected HarnessMatchYes (requires I6.1), got %q; "+
			"cannot assert Evidence without a positive-match verdict", verdict.Status)
	}
	if len(verdict.Evidence) == 0 {
		t.Error("HarnessMatchYes Evidence must be non-empty; " +
			"at least one tool name from the comma-scalar must appear as a matched signal")
	}
}

// TestDetectHarnessMatch_ClaudeCodeNoSignal_StillIndeterminate verifies the guard side of
// T6.2: a file that carries no distinguishing signal for the claude-code-like harness (no
// tools field, no harness-exclusive frontmatter keys) still returns HarnessMatchIndeterminate
// after I6.1. The fix must not collapse the no-evidence branch.
//
// This test passes both before and after I6.1 — it is a regression guard to confirm that the
// comma-scalar widening does not create false positives for tool-less files.
func TestDetectHarnessMatch_ClaudeCodeNoSignal_StillIndeterminate(t *testing.T) {
	// mosaicOnlyAgentBytes() carries only MOSAIC-known generic vocab keys and has no tools
	// field. With the claude-code-like descriptor, there is no path to hasPositive:
	// no ClassHarness keys (both model and tools keys are MOSAIC vocab) and no Universe
	// tool-name matches (tools field absent). hasNegative is also false. → Indeterminate.
	src := mosaicOnlyAgentBytes()
	ccModule := newClaudeCodeLikeModule()

	verdict := DetectHarnessMatch(src, ccModule, domain.ArtifactAgent, "bare-agent")

	if verdict.Status != HarnessMatchIndeterminate {
		t.Errorf("DetectHarnessMatch status = %q, want %q; "+
			"a file with no distinguishing signal must remain Indeterminate after I6.1; "+
			"the comma-scalar widening must only add positive evidence for files that "+
			"actually carry Universe tool names in their tools scalar; Reason: %q",
			verdict.Status, HarnessMatchIndeterminate, verdict.Reason)
	}
}

// ---------------------------------------------------------------------------
// T6.3 — Revised indeterminate reason text
// ---------------------------------------------------------------------------

// indeterminateNoEvidenceReason is the exact reason text the no-evidence indeterminate branch
// of DetectHarnessMatch must carry after I6.2. It is informational in register: it states that
// no distinguishing signal was found and that the transform proceeded normally, without implying
// that anything failed.
//
// This constant is the target contract. The test below is RED until I6.2 revises the wording
// in retarget.go to match this string exactly.
const indeterminateNoEvidenceReason = "no signal distinguishing this harness from another was found; the transform proceeded normally"

// TestDetectHarnessMatch_IndeterminateNoEvidence_ReasonIsInformational pins the exact wording
// of the no-evidence indeterminate reason. The current wording ("file is a transformed MOSAIC
// agent but carries no signal distinguishing this harness from another") can read as alarming;
// the revised wording must clearly communicate that no distinguishing signal was found and that
// the transform proceeded normally, without implying failure.
//
// This test is RED until I6.2 is implemented (the reason text in retarget.go is revised).
func TestDetectHarnessMatch_IndeterminateNoEvidence_ReasonIsInformational(t *testing.T) {
	// mosaicOnlyAgentBytes() + newAlphaHarnessModule() is the canonical no-evidence scenario:
	// the file carries only MOSAIC-known keys, no harness-distinctive keys (alpha_stamp is
	// absent), and no tools field → hasPositive=false, hasNegative=false → Indeterminate.
	src := mosaicOnlyAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "bare-agent")

	if verdict.Status != HarnessMatchIndeterminate {
		t.Fatalf("expected HarnessMatchIndeterminate for a no-evidence file, got %q; "+
			"cannot assert Reason text without the no-evidence indeterminate verdict", verdict.Status)
	}
	if verdict.Reason != indeterminateNoEvidenceReason {
		t.Errorf("DetectHarnessMatch indeterminate Reason:\n  want: %q\n   got: %q\n\n"+
			"The revised reason must state that no distinguishing signal was found and that the "+
			"transform proceeded normally. It must not imply that anything failed.\n"+
			"Implement by updating the fall-through return in retarget.go (I6.2).",
			indeterminateNoEvidenceReason, verdict.Reason)
	}
}

// TestDetectHarnessMatch_IndeterminateNoEvidence_ReasonDoesNotImplyFailure complements the
// exact-wording test by asserting a negative: the no-evidence indeterminate reason must not
// contain language that implies an error or unexpected state. This catches alternative phrasings
// that satisfy the exact-wording check but are still alarming.
//
// Note: this test passes if the exact-wording test passes, since indeterminateNoEvidenceReason
// does not contain alarming language. It is included as an explicit documentation of intent.
func TestDetectHarnessMatch_IndeterminateNoEvidence_ReasonDoesNotImplyFailure(t *testing.T) {
	src := mosaicOnlyAgentBytes()
	alpha := newAlphaHarnessModule()

	verdict := DetectHarnessMatch(src, alpha, domain.ArtifactAgent, "bare-agent")

	if verdict.Status != HarnessMatchIndeterminate {
		t.Fatalf("expected HarnessMatchIndeterminate, got %q", verdict.Status)
	}
	// The reason must contain an affirmative statement about normal operation.
	// The revised wording "the transform proceeded normally" satisfies this.
	mustContain := "proceeded normally"
	if !strings.Contains(verdict.Reason, mustContain) {
		t.Errorf("DetectHarnessMatch indeterminate Reason %q does not affirm normal operation; "+
			"it must contain %q to distinguish an informational no-signal outcome from a failure "+
			"(current wording omits any statement about the transform proceeding)",
			verdict.Reason, mustContain)
	}
}
