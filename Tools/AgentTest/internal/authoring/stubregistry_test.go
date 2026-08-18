package authoring_test

// Tests for ParseStubRegistry: parsing and schema-validating a stub-registry
// document (`*.stubs.json`) into domain.StubRegistry.
//
// Coverage follows T4.2: entries keyed by the composite collaborator
// identity plus a per-identity invocation ordinal (with an empty agent
// identity meaning a plain tool call), the stub response payload, declared
// side effects (literal content or a $ref), and the unmatched-invocation
// policy.

import (
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
)

const wellFormedRegistry = `
{
  "schema_version": 1,
  "test_id": "happy-path",
  "on_unmatched": "halt",
  "stubs": [
    {
      "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": {
        "agent_instance_id": "researcher#2",
        "run_id": "{run_id}",
        "status_code": "SUCCESS",
        "status_message": "Research complete."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/Research.md", "content": "# Research\n" },
          { "path": "Orchestration-{run_id}/LibraryResearch.md", "ref": "fixtures/library-research.md" }
        ]
      }
    }
  ]
}
`

func TestParseStubRegistry_WellFormed_PopulatesFields(t *testing.T) {
	src := authoring.Source{Path: "happy-path.stubs.json", Data: []byte(wellFormedRegistry)}

	reg, report := authoring.ParseStubRegistry(src)

	if report.HasErrors() {
		t.Fatalf("expected a well-formed registry to parse without errors, got: %v", report.Diagnostics)
	}
	if reg.TestID != "happy-path" {
		t.Errorf("TestID = %q, want %q", reg.TestID, "happy-path")
	}
	if reg.OnUnmatched != domain.UnmatchedHalt {
		t.Errorf("OnUnmatched = %q, want %q", reg.OnUnmatched, domain.UnmatchedHalt)
	}
	if len(reg.Stubs) != 1 {
		t.Fatalf("len(Stubs) = %d, want 1", len(reg.Stubs))
	}
}

func TestParseStubRegistry_CompositeIdentityAndOrdinalKeyTheMatch(t *testing.T) {
	src := authoring.Source{Path: "happy-path.stubs.json", Data: []byte(wellFormedRegistry)}

	reg, report := authoring.ParseStubRegistry(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}

	match := reg.Stubs[0].Match
	wantIdentity := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	if match.Identity != wantIdentity {
		t.Errorf("Match.Identity = %+v, want %+v", match.Identity, wantIdentity)
	}
	if match.Invocation != 1 {
		t.Errorf("Match.Invocation = %d, want 1 (1-based per-identity ordinal)", match.Invocation)
	}
}

// TestParseStubRegistry_EmptyAgentIdentity_ParsesAsPlainToolCall is AC4.4's
// explicit case: a registry entry with no "agent" field is a plain tool
// call, not an error and not a zero-valued identity treated as absent.
func TestParseStubRegistry_EmptyAgentIdentity_ParsesAsPlainToolCall(t *testing.T) {
	src := authoring.Source{
		Path: "plain-tool.stubs.json",
		Data: []byte(`
{
  "schema_version": 1,
  "test_id": "plain-tool",
  "on_unmatched": "halt",
  "stubs": [
    {
      "match": { "tool": "Read", "invocation": 1 },
      "response": { "content": "stubbed file content" }
    }
  ]
}
`),
	}

	reg, report := authoring.ParseStubRegistry(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if len(reg.Stubs) != 1 {
		t.Fatalf("len(Stubs) = %d, want 1", len(reg.Stubs))
	}

	identity := reg.Stubs[0].Match.Identity
	if identity.ToolName != "Read" {
		t.Errorf("Identity.ToolName = %q, want %q", identity.ToolName, "Read")
	}
	if identity.AgentIdentity != "" {
		t.Errorf("Identity.AgentIdentity = %q, want empty (a plain tool call)", identity.AgentIdentity)
	}
	if identity.IsZero() {
		t.Error("Identity.IsZero() = true, want false: a tool name alone is a valid, non-absent identity")
	}
}

func TestParseStubRegistry_SideEffects_LiteralContentAndRef(t *testing.T) {
	src := authoring.Source{Path: "happy-path.stubs.json", Data: []byte(wellFormedRegistry)}

	reg, report := authoring.ParseStubRegistry(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}

	effects := reg.Stubs[0].SideEffects
	if len(effects) != 2 {
		t.Fatalf("len(SideEffects) = %d, want 2", len(effects))
	}

	literal := effects[0]
	if literal.Path != "Orchestration-{run_id}/Research.md" {
		t.Errorf("SideEffects[0].Path = %q, want %q", literal.Path, "Orchestration-{run_id}/Research.md")
	}
	if literal.Content != "# Research\n" {
		t.Errorf("SideEffects[0].Content = %q, want %q", literal.Content, "# Research\n")
	}
	if literal.Ref != "" {
		t.Errorf("SideEffects[0].Ref = %q, want empty (Content and Ref are mutually exclusive)", literal.Ref)
	}

	referenced := effects[1]
	if referenced.Ref != "fixtures/library-research.md" {
		t.Errorf("SideEffects[1].Ref = %q, want %q", referenced.Ref, "fixtures/library-research.md")
	}
	if referenced.Content != "" {
		t.Errorf("SideEffects[1].Content = %q, want empty (Content and Ref are mutually exclusive)", referenced.Content)
	}
}

func TestParseStubRegistry_UnmatchedPolicy_Parses(t *testing.T) {
	cases := []struct {
		declared string
		want     domain.UnmatchedPolicy
	}{
		{"halt", domain.UnmatchedHalt},
		{"generic-response", domain.UnmatchedGenericResponse},
		{"passthrough", domain.UnmatchedPassthrough},
	}

	for _, tc := range cases {
		t.Run(tc.declared, func(t *testing.T) {
			data := `{
  "schema_version": 1,
  "test_id": "t",
  "on_unmatched": "` + tc.declared + `",
  "stubs": []
}`
			if tc.want == domain.UnmatchedGenericResponse {
				data = `{
  "schema_version": 1,
  "test_id": "t",
  "on_unmatched": "generic-response",
  "generic_response": { "status_code": "SUCCESS" },
  "stubs": []
}`
			}

			src := authoring.Source{Path: "t.stubs.json", Data: []byte(data)}
			reg, report := authoring.ParseStubRegistry(src)
			if report.HasErrors() {
				t.Fatalf("unexpected errors: %v", report.Diagnostics)
			}
			if reg.OnUnmatched != tc.want {
				t.Errorf("OnUnmatched = %q, want %q", reg.OnUnmatched, tc.want)
			}
		})
	}
}

func TestParseStubRegistry_GenericResponsePolicyWithoutResponse_ReportsDiagnostic(t *testing.T) {
	src := authoring.Source{
		Path: "t.stubs.json",
		Data: []byte(`{
  "schema_version": 1,
  "test_id": "t",
  "on_unmatched": "generic-response",
  "stubs": []
}`),
	}

	_, report := authoring.ParseStubRegistry(src)

	if !hasDiagnosticCode(report, "missing-generic-response") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "missing-generic-response", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected 'generic-response' policy without a generic_response payload to be reported as a diagnostic")
	}
}

func TestParseStubRegistry_UnknownField_ReportsDiagnostic(t *testing.T) {
	src := authoring.Source{
		Path: "t.stubs.json",
		Data: []byte(`{
  "schema_version": 1,
  "test_id": "t",
  "on_unmatched": "halt",
  "not_a_real_field": true,
  "stubs": []
}`),
	}

	_, report := authoring.ParseStubRegistry(src)

	if !hasDiagnosticCode(report, "unknown-field") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "unknown-field", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected an unknown field to be reported as a diagnostic")
	}
}

func TestParseStubRegistry_MissingRequiredTestID_ReportsDiagnostic(t *testing.T) {
	src := authoring.Source{
		Path: "t.stubs.json",
		Data: []byte(`{
  "schema_version": 1,
  "on_unmatched": "halt",
  "stubs": []
}`),
	}

	_, report := authoring.ParseStubRegistry(src)

	if !hasDiagnosticCode(report, "missing-required-field") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "missing-required-field", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected a missing required 'test_id' field to be reported as a diagnostic")
	}
}
