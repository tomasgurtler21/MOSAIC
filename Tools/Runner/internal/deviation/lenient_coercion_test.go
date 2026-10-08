package deviation_test

// Tests for lenient type coercion in routing responses — bare-string and
// string-bool coercion for the three polymorphic fields.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
)

// ===== Lenient routing coercion tests =====

// TestConsultRouting_CoercesInputArtifactsFromBareString verifies that when
// the routing response carries input_artifacts as a bare JSON string (not an
// array), ConsultRouting coerces it to a single-element []string, returns a
// non-nil pointer, and does not fail. A non-nil pointer means the value was
// explicitly supplied — not a fallback to the table row.
func TestConsultRouting_CoercesInputArtifactsFromBareString(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{
		"action": "dispatch",
		"agent": "agent-a",
"row": 1,
		"task_description": "do the thing",
		"input_artifacts": "single-file.md"
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when input_artifacts is a bare string (lenient coercion), got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.InputArtifacts == nil {
		t.Fatal("want non-nil InputArtifacts pointer for coerced bare string (explicitly supplied, not fallback), got nil")
	}
	if len(*d.InputArtifacts) != 1 {
		t.Fatalf("want single-element InputArtifacts slice, got %d elements: %v", len(*d.InputArtifacts), *d.InputArtifacts)
	}
	if (*d.InputArtifacts)[0] != "single-file.md" {
		t.Errorf("want InputArtifacts[0]=%q, got %q", "single-file.md", (*d.InputArtifacts)[0])
	}
}

// TestConsultRouting_CoercesOutputArtifactsFromBareString verifies that when
// the routing response carries output_artifacts as a bare JSON string (not an
// array), ConsultRouting coerces it to a single-element []string, returns a
// non-nil pointer, and does not fail. A non-nil pointer means the value was
// explicitly supplied — not a fallback to the table row.
func TestConsultRouting_CoercesOutputArtifactsFromBareString(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{
		"action": "dispatch",
		"agent": "agent-a",
"row": 1,
		"task_description": "do the thing",
		"output_artifacts": "result.md"
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when output_artifacts is a bare string (lenient coercion), got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.OutputArtifacts == nil {
		t.Fatal("want non-nil OutputArtifacts pointer for coerced bare string (explicitly supplied, not fallback), got nil")
	}
	if len(*d.OutputArtifacts) != 1 {
		t.Fatalf("want single-element OutputArtifacts slice, got %d elements: %v", len(*d.OutputArtifacts), *d.OutputArtifacts)
	}
	if (*d.OutputArtifacts)[0] != "result.md" {
		t.Errorf("want OutputArtifacts[0]=%q, got %q", "result.md", (*d.OutputArtifacts)[0])
	}
}

// TestConsultRouting_CoercesHITLOverrideFromStringTrue verifies that when the
// routing response carries hitl_override as the JSON string "true" (not a boolean),
// ConsultRouting coerces it to bool true, returns a non-nil pointer, and does
// not fail. A non-nil pointer means the value was explicitly supplied.
func TestConsultRouting_CoercesHITLOverrideFromStringTrue(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{
		"action": "dispatch",
		"agent": "agent-a",
"row": 1,
		"task_description": "do the thing",
		"hitl_override": "true"
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when hitl_override is string %q (lenient coercion), got %v", "true", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.HITLOverride == nil {
		t.Fatal("want non-nil HITLOverride pointer for coerced string \"true\" (explicitly supplied, not fallback), got nil")
	}
	if !*d.HITLOverride {
		t.Error("want *HITLOverride=true after coercing string \"true\", got false")
	}
}

// TestConsultRouting_CoercesHITLOverrideFromStringFalse verifies that when the
// routing response carries hitl_override as the JSON string "false" (not a
// boolean), ConsultRouting coerces it to bool false, returns a non-nil pointer,
// and does not fail. A non-nil pointer means the value was explicitly supplied —
// false-with-a-non-nil-pointer is distinct from nil (which means "fall back").
func TestConsultRouting_CoercesHITLOverrideFromStringFalse(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{
		"action": "dispatch",
		"agent": "agent-a",
"row": 1,
		"task_description": "do the thing",
		"hitl_override": "false"
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when hitl_override is string %q (lenient coercion), got %v", "false", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.HITLOverride == nil {
		t.Fatal("want non-nil HITLOverride pointer for coerced string \"false\" (explicitly supplied, not fallback), got nil")
	}
	if *d.HITLOverride {
		t.Error("want *HITLOverride=false after coercing string \"false\", got true")
	}
}

// TestConsultRouting_CoercedFieldsAreAllNonNilPointers verifies that when all
// three coercible fields are supplied as their mismatched-but-coercible types in
// a single response, all three coerced values carry non-nil pointers. Non-nil
// means "explicitly supplied" — the coercion must preserve the
// present-vs-absent distinction used by downstream dispatch logic.
func TestConsultRouting_CoercedFieldsAreAllNonNilPointers(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{
		"action": "dispatch",
		"agent": "agent-a",
"row": 1,
		"task_description": "do the thing",
		"input_artifacts": "in.md",
		"output_artifacts": "out.md",
		"hitl_override": "true"
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when all three coercible fields are supplied as coercible types, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.InputArtifacts == nil {
		t.Error("want non-nil InputArtifacts pointer after coercion from bare string, got nil")
	}
	if d.OutputArtifacts == nil {
		t.Error("want non-nil OutputArtifacts pointer after coercion from bare string, got nil")
	}
	if d.HITLOverride == nil {
		t.Error("want non-nil HITLOverride pointer after coercion from string \"true\", got nil")
	}
}

// TestConsultRouting_UncoercibleValues_FailWithMalformedJSON verifies that values
// which cannot be unambiguously coerced to the target type — a JSON number where
// an array is expected, a JSON object where an array is expected, and a
// non-boolean string where a bool is expected — all produce a *ConsultationError
// with ConsultFailMalformedJSON. The object was found (ExtractJSONObject succeeded)
// but the field values are genuinely invalid.
func TestConsultRouting_UncoercibleValues_FailWithMalformedJSON(t *testing.T) {
	table := mustParseTable(t)

	cases := []struct {
		name  string
		reply []byte
	}{
		{
			name: "input_artifacts as JSON number",
			reply: []byte(`{
				"action": "dispatch",
				"agent": "agent-a",
"row": 1,
				"task_description": "do the thing",
				"input_artifacts": 42
			}`),
		},
		{
			name: "input_artifacts as JSON object",
			reply: []byte(`{
				"action": "dispatch",
				"agent": "agent-a",
"row": 1,
				"task_description": "do the thing",
				"input_artifacts": {"key": "value"}
			}`),
		},
		{
			name: "output_artifacts as JSON number",
			reply: []byte(`{
				"action": "dispatch",
				"agent": "agent-a",
"row": 1,
				"task_description": "do the thing",
				"output_artifacts": 99
			}`),
		},
		{
			name: "output_artifacts as JSON object",
			reply: []byte(`{
				"action": "dispatch",
				"agent": "agent-a",
"row": 1,
				"task_description": "do the thing",
				"output_artifacts": {"key": "value"}
			}`),
		},
		{
			name: "hitl_override as non-boolean string",
			reply: []byte(`{
				"action": "dispatch",
				"agent": "agent-a",
"row": 1,
				"task_description": "do the thing",
				"hitl_override": "yes"
			}`),
		},
		{
			name: "hitl_override as JSON number",
			reply: []byte(`{
				"action": "dispatch",
				"agent": "agent-a",
"row": 1,
				"task_description": "do the thing",
				"hitl_override": 1
			}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: tc.reply}
			c := newTestOrchestratorConsultant(fake, table)

			_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
		})
	}
}
