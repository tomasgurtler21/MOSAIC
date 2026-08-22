package preflight_test

// Tests for DelegateToolDiagnostic and the reason-code-to-hint mapping.
//
// DelegateToolDiagnostic is a pure function that accepts a FailureContext
// (AgentTest-side context) and an authoring.StructuredFailure (delegate tool
// failure fields) and returns a single authoring.Diagnostic. These tests
// verify the following properties:
//
//   - Known reason codes produce specific, actionable hint text.
//   - Empty reason codes produce a distinct generic hint (not conflated with
//     unknown codes).
//   - Unrecognized reason codes produce a generic hint that interpolates the
//     unrecognized code so the user can see what was received.
//   - "agent-not-in-catalog" and "unknown-workflow" are treated as
//     unrecognized by the hint mapping (they are handled by dedicated early-
//     return branches in checkSubjectRender, never reaching DelegateToolDiagnostic
//     in production, but the function itself must not give them special treatment).
//   - The returned Diagnostic always has Severity SeverityError.
//   - The Code, Path, and Pointer fields are taken verbatim from FailureContext.
//   - The Message contains the operation description from FailureContext.
//   - The Message contains the configuration provenance from FailureContext when
//     Provenance is non-empty.
//   - The Message contains the ToolMessage as supplementary detail, but the
//     ToolMessage is never the sole content of the Message (FR-8 compliance).
//   - When Provenance is empty, DelegateToolDiagnostic omits the provenance
//     clause rather than embedding an empty string.
//   - The Hint is always non-empty, regardless of reason code.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// fakeStructuredFailure is a test double satisfying authoring.StructuredFailure.
// It allows full control over every structured failure field in isolation.
type fakeStructuredFailure struct {
	reason      string
	detail      string
	toolMessage string
	exitCode    int
}

func (f *fakeStructuredFailure) FailureReason() string      { return f.reason }
func (f *fakeStructuredFailure) FailureDetail() string      { return f.detail }
func (f *fakeStructuredFailure) FailureToolMessage() string { return f.toolMessage }
func (f *fakeStructuredFailure) FailureExitCode() int       { return f.exitCode }

var _ authoring.StructuredFailure = (*fakeStructuredFailure)(nil)

// baseFC is a FailureContext with all fields populated for use as a baseline
// in tests that care only about hint/message behaviour, not about specific
// field forwarding.
func baseFC() preflight.FailureContext {
	return preflight.FailureContext{
		Operation:      "subject declaration dry-run render",
		Provenance:     "--mosaic-root default: selfDir/../../..",
		DiagnosticCode: "unrenderable-subject-declaration",
		Path:           "/suite/subject.test.yaml",
		Pointer:        "subject",
	}
}

// --- Diagnostic field forwarding ---

// TestDelegateToolDiagnostic_SeverityIsAlwaysError verifies that the returned
// Diagnostic always carries SeverityError, regardless of the reason code or
// failure fields.
func TestDelegateToolDiagnostic_SeverityIsAlwaysError(t *testing.T) {
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(baseFC(), sf)
	if got.Severity != authoring.SeverityError {
		t.Errorf("Severity = %q, want %q", got.Severity, authoring.SeverityError)
	}
}

// TestDelegateToolDiagnostic_CodeFromFailureContext verifies that the Code
// field is taken verbatim from FailureContext.DiagnosticCode.
func TestDelegateToolDiagnostic_CodeFromFailureContext(t *testing.T) {
	fc := baseFC()
	fc.DiagnosticCode = "unrenderable-stub-definition"
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(fc, sf)
	if got.Code != "unrenderable-stub-definition" {
		t.Errorf("Code = %q, want %q", got.Code, "unrenderable-stub-definition")
	}
}

// TestDelegateToolDiagnostic_PathFromFailureContext verifies that the Path
// field is taken verbatim from FailureContext.Path.
func TestDelegateToolDiagnostic_PathFromFailureContext(t *testing.T) {
	fc := baseFC()
	fc.Path = "/my/suite/test.test.yaml"
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(fc, sf)
	if got.Path != "/my/suite/test.test.yaml" {
		t.Errorf("Path = %q, want %q", got.Path, "/my/suite/test.test.yaml")
	}
}

// TestDelegateToolDiagnostic_PointerFromFailureContext verifies that the
// Pointer field is taken verbatim from FailureContext.Pointer.
func TestDelegateToolDiagnostic_PointerFromFailureContext(t *testing.T) {
	fc := baseFC()
	fc.Pointer = "stub_agents[0].source"
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(fc, sf)
	if got.Pointer != "stub_agents[0].source" {
		t.Errorf("Pointer = %q, want %q", got.Pointer, "stub_agents[0].source")
	}
}

// --- Message content ---

// TestDelegateToolDiagnostic_MessageContainsOperation verifies that the
// Diagnostic Message contains the operation description from FailureContext.
// The operation context tells the user what was being attempted so they can
// understand the failure in context.
func TestDelegateToolDiagnostic_MessageContainsOperation(t *testing.T) {
	fc := baseFC()
	fc.Operation = "subject declaration dry-run render"
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(fc, sf)
	if !strings.Contains(got.Message, "subject declaration dry-run render") {
		t.Errorf("Message does not contain operation %q; got: %q", "subject declaration dry-run render", got.Message)
	}
}

// TestDelegateToolDiagnostic_MessageContainsProvenance verifies that the
// Diagnostic Message contains the configuration provenance when Provenance
// is non-empty. Provenance tells the user which configuration tier produced
// the failing path so they know what to change.
func TestDelegateToolDiagnostic_MessageContainsProvenance(t *testing.T) {
	fc := baseFC()
	fc.Provenance = "--mosaic-root default: selfDir/../../.."
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(fc, sf)
	if !strings.Contains(got.Message, "--mosaic-root default: selfDir/../../..") {
		t.Errorf("Message does not contain provenance %q; got: %q",
			"--mosaic-root default: selfDir/../../..", got.Message)
	}
}

// TestDelegateToolDiagnostic_MessageContainsToolMessage verifies that the
// ToolMessage from the delegate tool appears in the Diagnostic Message as
// supplementary detail so the author can see the raw tool output.
func TestDelegateToolDiagnostic_MessageContainsToolMessage(t *testing.T) {
	sf := &fakeStructuredFailure{
		reason:      "harness-unresolvable",
		toolMessage: "the harness binary could not be resolved",
	}
	got := preflight.DelegateToolDiagnostic(baseFC(), sf)
	if !strings.Contains(got.Message, "the harness binary could not be resolved") {
		t.Errorf("Message does not contain ToolMessage; got: %q", got.Message)
	}
}

// TestDelegateToolDiagnostic_ToolMessageNotSoleContent verifies FR-8: the
// ToolMessage must never be the sole content of the Diagnostic Message. The
// Message must contain additional context (operation, provenance, or other
// framing) beyond the raw tool output.
func TestDelegateToolDiagnostic_ToolMessageNotSoleContent(t *testing.T) {
	const toolMsg = "unique-tool-output-XYZ"
	fc := baseFC()
	fc.Operation = "subject declaration dry-run render"
	sf := &fakeStructuredFailure{
		reason:      "harness-unresolvable",
		toolMessage: toolMsg,
	}
	got := preflight.DelegateToolDiagnostic(fc, sf)
	if got.Message == toolMsg {
		t.Errorf("Message must not be the sole ToolMessage content; got exactly %q", got.Message)
	}
	if !strings.Contains(got.Message, toolMsg) {
		t.Errorf("Message does not contain ToolMessage at all; got: %q", got.Message)
	}
}

// TestDelegateToolDiagnostic_EmptyProvenanceOmitsProvenanceClause verifies
// that when FailureContext.Provenance is empty, DelegateToolDiagnostic omits
// the provenance clause from the Message rather than embedding an empty string
// or a placeholder. The Message must still be non-empty and contain the
// operation context.
func TestDelegateToolDiagnostic_EmptyProvenanceOmitsProvenanceClause(t *testing.T) {
	fc := baseFC()
	fc.Provenance = ""
	fc.Operation = "subject declaration dry-run render"
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(fc, sf)
	if got.Message == "" {
		t.Error("Message must not be empty even when Provenance is empty")
	}
	// The message must still contain operation context.
	if !strings.Contains(got.Message, "subject declaration dry-run render") {
		t.Errorf("Message must still contain operation context when Provenance is empty; got: %q", got.Message)
	}
	// The message must not embed a bare empty-string provenance clause.
	// When Provenance is empty the clause must be omitted entirely, not
	// rendered as an empty value. Any occurrence of "provenance: " in the
	// message indicates the clause was not omitted as required.
	if strings.Contains(got.Message, "provenance: ") {
		t.Errorf("Message must not embed any provenance clause when Provenance is empty; got: %q", got.Message)
	}
}

// --- Hint: known reason codes ---

// TestDelegateToolDiagnostic_HarnessUnresolvable_SpecificHint verifies that
// the known reason code "harness-unresolvable" produces a specific, actionable
// hint directing the user to check --mosaic-root. This is the minimum required
// known code per the design.
func TestDelegateToolDiagnostic_HarnessUnresolvable_SpecificHint(t *testing.T) {
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(baseFC(), sf)
	if got.Hint == "" {
		t.Fatal("Hint must not be empty for known reason code 'harness-unresolvable'")
	}
	if !strings.Contains(got.Hint, "mosaic-root") {
		t.Errorf("Hint for 'harness-unresolvable' should reference --mosaic-root; got: %q", got.Hint)
	}
}

// --- Hint: empty reason code ---

// TestDelegateToolDiagnostic_EmptyReason_SpecificGenericHint verifies that an
// empty reason code (meaning "undifferentiated failure: the tool reported no
// structured reason") produces a non-empty hint that is distinct from the hint
// produced for unrecognized codes. Empty must be handled explicitly, not
// conflated with unknown.
func TestDelegateToolDiagnostic_EmptyReason_SpecificGenericHint(t *testing.T) {
	sf := &fakeStructuredFailure{reason: "", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(baseFC(), sf)
	if got.Hint == "" {
		t.Fatal("Hint must not be empty even when reason is empty")
	}
	// The hint for an empty reason should communicate that no structured reason
	// was provided, directing the user to check tool configuration.
	if !strings.Contains(strings.ToLower(got.Hint), "no") && !strings.Contains(strings.ToLower(got.Hint), "check") {
		t.Errorf("Hint for empty reason should communicate absence of structured reason; got: %q", got.Hint)
	}
}

// --- Hint: unrecognized reason codes ---

// TestDelegateToolDiagnostic_UnrecognizedReason_GenericHintWithCode verifies
// that an unrecognized reason code (one this version of the tool does not know)
// produces a non-empty generic hint that interpolates the received code so the
// user knows what was received. This is the forward-compatibility requirement:
// a future tool version's reason code must not result in an empty or misleading
// hint.
func TestDelegateToolDiagnostic_UnrecognizedReason_GenericHintWithCode(t *testing.T) {
	const unknownCode = "some-future-reason-code-v99"
	sf := &fakeStructuredFailure{reason: unknownCode, toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(baseFC(), sf)
	if got.Hint == "" {
		t.Fatal("Hint must not be empty for an unrecognized reason code")
	}
	if !strings.Contains(got.Hint, unknownCode) {
		t.Errorf("Hint for unrecognized reason should interpolate the received code %q; got: %q",
			unknownCode, got.Hint)
	}
}

// TestDelegateToolDiagnostic_AgentNotInCatalog_GenericHint verifies that
// "agent-not-in-catalog" falls into the unrecognized branch and receives a
// generic hint. This code is handled by a dedicated early-return branch in
// checkSubjectRender and never reaches DelegateToolDiagnostic in production,
// so the function itself must not give it special treatment.
func TestDelegateToolDiagnostic_AgentNotInCatalog_GenericHint(t *testing.T) {
	sf := &fakeStructuredFailure{reason: "agent-not-in-catalog", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(baseFC(), sf)
	if got.Hint == "" {
		t.Fatal("Hint must not be empty for reason 'agent-not-in-catalog'")
	}
	// Must receive generic (unrecognized) treatment, not a specific hint.
	// We verify this by checking the hint contains the reason code itself,
	// which is the fingerprint of the unrecognized-code branch.
	if !strings.Contains(got.Hint, "agent-not-in-catalog") {
		t.Errorf("'agent-not-in-catalog' should fall into the unrecognized branch "+
			"(hint should interpolate the code); got: %q", got.Hint)
	}
}

// TestDelegateToolDiagnostic_UnknownWorkflow_GenericHint verifies that
// "unknown-workflow" falls into the unrecognized branch and receives a generic
// hint. Like "agent-not-in-catalog", this code is handled by a dedicated early-
// return branch in checkSubjectRender and must not be given special treatment
// by DelegateToolDiagnostic itself.
func TestDelegateToolDiagnostic_UnknownWorkflow_GenericHint(t *testing.T) {
	sf := &fakeStructuredFailure{reason: "unknown-workflow", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(baseFC(), sf)
	if got.Hint == "" {
		t.Fatal("Hint must not be empty for reason 'unknown-workflow'")
	}
	// Must receive generic (unrecognized) treatment.
	if !strings.Contains(got.Hint, "unknown-workflow") {
		t.Errorf("'unknown-workflow' should fall into the unrecognized branch "+
			"(hint should interpolate the code); got: %q", got.Hint)
	}
}

// --- Hint always non-empty ---

// TestDelegateToolDiagnostic_HintAlwaysNonEmpty is a table-driven check
// that confirms every reason code -- known, unknown, and empty -- produces a
// non-empty Hint. The Hint is the primary remediation signal for an author
// reading a diagnostic; it must never be blank.
func TestDelegateToolDiagnostic_HintAlwaysNonEmpty(t *testing.T) {
	cases := []struct {
		name   string
		reason string
	}{
		{"known-harness-unresolvable", "harness-unresolvable"},
		{"empty-reason", ""},
		{"unrecognized-future-code", "future-reason-from-newer-tool"},
		{"agent-not-in-catalog-via-helper", "agent-not-in-catalog"},
		{"unknown-workflow-via-helper", "unknown-workflow"},
		{"internal-error-code", "internal"},
		{"source-not-found-code", "source-not-found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sf := &fakeStructuredFailure{reason: tc.reason, toolMessage: "some tool output"}
			got := preflight.DelegateToolDiagnostic(baseFC(), sf)
			if got.Hint == "" {
				t.Errorf("reason %q: Hint must not be empty; got empty Hint", tc.reason)
			}
		})
	}
}

// --- Message is non-empty ---

// TestDelegateToolDiagnostic_MessageAlwaysNonEmpty verifies that the Message
// field is never empty regardless of input. A diagnostic with no message is
// not useful to an author trying to diagnose their authoring error.
func TestDelegateToolDiagnostic_MessageAlwaysNonEmpty(t *testing.T) {
	cases := []struct {
		name        string
		reason      string
		toolMessage string
	}{
		{"known-reason-with-tool-message", "harness-unresolvable", "tool said something"},
		{"empty-reason-with-tool-message", "", "tool said something"},
		{"unrecognized-with-tool-message", "future-code", "tool said something"},
		{"known-reason-empty-tool-message", "harness-unresolvable", ""},
		{"empty-reason-empty-tool-message", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sf := &fakeStructuredFailure{reason: tc.reason, toolMessage: tc.toolMessage}
			got := preflight.DelegateToolDiagnostic(baseFC(), sf)
			if got.Message == "" {
				t.Errorf("reason %q, toolMessage %q: Message must not be empty", tc.reason, tc.toolMessage)
			}
		})
	}
}

// --- Provenance is a required parameter ---

// TestDelegateToolDiagnostic_ProvenanceFlagTierAppearsInMessage verifies that
// provenance strings from different resolution tiers all appear verbatim in
// the Message. Provenance is a required parameter (not optional context), so
// the call site's string must flow through unchanged.
func TestDelegateToolDiagnostic_ProvenanceFlagTierAppearsInMessage(t *testing.T) {
	cases := []struct {
		name       string
		provenance string
	}{
		{"flag-tier", "--mosaic-root flag"},
		{"env-tier", "--mosaic-root env: MOSAIC_AGENT_TEST_MOSAIC_ROOT"},
		{"default-tier", "--mosaic-root default: selfDir/../../.."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := baseFC()
			fc.Provenance = tc.provenance
			sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
			got := preflight.DelegateToolDiagnostic(fc, sf)
			if !strings.Contains(got.Message, tc.provenance) {
				t.Errorf("Message does not contain provenance %q; got: %q", tc.provenance, got.Message)
			}
		})
	}
}

// TestDelegateToolDiagnostic_HarnessUnresolvable_HintReferencesRepo verifies
// that the hint for "harness-unresolvable" directs the user to verify that
// --mosaic-root points to a valid MOSAIC repository root, which is the
// specific, actionable fix for this failure.
func TestDelegateToolDiagnostic_HarnessUnresolvable_HintReferencesRepo(t *testing.T) {
	sf := &fakeStructuredFailure{reason: "harness-unresolvable", toolMessage: "tool output"}
	got := preflight.DelegateToolDiagnostic(baseFC(), sf)
	lowerHint := strings.ToLower(got.Hint)
	if !strings.Contains(lowerHint, "mosaic-root") {
		t.Errorf("hint for 'harness-unresolvable' must reference --mosaic-root; got: %q", got.Hint)
	}
	if !strings.Contains(lowerHint, "valid") && !strings.Contains(lowerHint, "check") && !strings.Contains(lowerHint, "point") {
		t.Errorf("hint for 'harness-unresolvable' must be actionable (contain 'check', 'valid', or 'point'); got: %q", got.Hint)
	}
}

// TestDelegateToolDiagnostic_EmptyAndUnrecognizedHintsDiffer verifies that
// empty reason codes and unrecognized reason codes produce different hints.
// An empty reason means "the tool reported no structured reason", which is a
// different condition from "the tool reported a reason this version doesn't
// know". Conflating them would give misleading guidance.
func TestDelegateToolDiagnostic_EmptyAndUnrecognizedHintsDiffer(t *testing.T) {
	sfEmpty := &fakeStructuredFailure{reason: "", toolMessage: "tool output"}
	sfUnknown := &fakeStructuredFailure{reason: "some-future-code", toolMessage: "tool output"}

	hintEmpty := preflight.DelegateToolDiagnostic(baseFC(), sfEmpty).Hint
	hintUnknown := preflight.DelegateToolDiagnostic(baseFC(), sfUnknown).Hint

	if hintEmpty == hintUnknown {
		t.Errorf("empty reason and unrecognized reason must produce different hints; both produced: %q", hintEmpty)
	}
}
