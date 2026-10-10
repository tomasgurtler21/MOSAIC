package deviation_test

// Tests for ConsultRouting and PreConsult reply shape acceptance and failure
// classification (T4.2, T4.3).

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
)

// ===== T4.2: Routing consultation — reply shape acceptance and distinguishable failure classes =====

// TestConsultRouting_AcceptsVariousReplyShapes verifies that ConsultRouting
// accepts a dispatch reply in any of the supported formats — bare object,
// prose-surrounded, fenced with language tag, fenced without language tag —
// and yields the same RoutingInstruction from all of them.
func TestConsultRouting_AcceptsVariousReplyShapes(t *testing.T) {
	table := mustParseTable(t)

	const bareJSON = `{"action":"stop","reason":"orchestration complete"}`

	cases := []struct {
		name  string
		reply string
	}{
		{
			name:  "bare object",
			reply: bareJSON,
		},
		{
			name:  "object surrounded by prose",
			reply: "Based on my analysis of the current run state:\n\n" + bareJSON + "\n\nThis is my final recommendation.",
		},
		{
			name:  "object in fenced block with language tag",
			reply: "My routing decision:\n\n```json\n" + bareJSON + "\n```\n",
		},
		{
			name:  "object in fenced block without language tag",
			reply: "My routing decision:\n\n```\n" + bareJSON + "\n```\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: []byte(tc.reply)}
			c := newTestOrchestratorConsultant(fake, table)

			instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			if err != nil {
				t.Fatalf("want no error for %s, got %v — AC4.2 violated", tc.name, err)
			}
			if instr.Stop == nil {
				t.Fatalf("want Stop instruction for %s, got nil Stop", tc.name)
			}
			if instr.Stop.Reason != "orchestration complete" {
				t.Errorf("want Reason=%q for %s, got %q", "orchestration complete", tc.name, instr.Stop.Reason)
			}
		})
	}
}

// TestConsultRouting_DispatchReplyShapesYieldSameInstruction verifies that a
// dispatch reply is parsed correctly from all supported formats, yielding the
// same DispatchInstruction regardless of wrapping.
func TestConsultRouting_DispatchReplyShapesYieldSameInstruction(t *testing.T) {
	table := mustParseTable(t)

	const bareJSON = `{"action":"dispatch","agent":"agent-a","row":1,"task_description":"implement the plan"}`

	cases := []struct {
		name  string
		reply string
	}{
		{
			name:  "bare dispatch object",
			reply: bareJSON,
		},
		{
			name:  "dispatch in prose",
			reply: "After reviewing the orchestration artifact, I recommend dispatching:\n\n" + bareJSON + "\n\nPlease proceed.",
		},
		{
			name:  "dispatch in fenced block",
			reply: "```json\n" + bareJSON + "\n```",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: []byte(tc.reply)}
			c := newTestOrchestratorConsultant(fake, table)

			instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			if err != nil {
				t.Fatalf("want no error for %s, got %v", tc.name, err)
			}
			if instr.Dispatch == nil {
				t.Fatalf("want Dispatch instruction for %s, got nil Dispatch", tc.name)
			}
			if instr.Dispatch.Agent != "agent-a" {
				t.Errorf("want Agent=%q for %s, got %q", "agent-a", tc.name, instr.Dispatch.Agent)
			}
			if instr.Dispatch.TaskDescription != "implement the plan" {
				t.Errorf("want TaskDescription=%q for %s, got %q", "implement the plan", tc.name, instr.Dispatch.TaskDescription)
			}
			// agent-a is row 0 in the two-row fixture.
			if instr.Dispatch.RowIndex != 0 {
				t.Errorf("want RowIndex=0 for agent-a for %s, got %d", tc.name, instr.Dispatch.RowIndex)
			}
		})
	}
}

// TestConsultRouting_NoObjectReply_FailsWithConsultFailNoInstruction verifies
// that when the orchestrator's reply contains no JSON object at all, ConsultRouting
// returns *ConsultationError with ConsultFailNoInstruction — not ConsultFailMalformedJSON.
// This is the "no instruction was found" class; AC4.3 requires it to be distinct
// from "instruction present but invalid".
func TestConsultRouting_NoObjectReply_FailsWithConsultFailNoInstruction(t *testing.T) {
	table := mustParseTable(t)
	// Pure prose, no JSON object anywhere in the reply.
	fake := &fakeRawInvoker{reply: []byte("The run is progressing well. I will advise you to continue to the next agent.")}
	c := newTestOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	// Must be ConsultFailNoInstruction, not ConsultFailMalformedJSON — AC4.3.
	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestConsultRouting_FailureClassesAreDistinguishable verifies that each distinct
// failure condition produces a different ConsultationFailure value, so a caller
// switching on ConsultationError.Failure can always identify the condition without
// matching on message text.
func TestConsultRouting_FailureClassesAreDistinguishable(t *testing.T) {
	table := mustParseTable(t)

	cases := []struct {
		name        string
		reply       []byte
		wantFailure domain.ConsultationFailure
	}{
		{
			name:        "no object in reply",
			reply:       []byte("Pure prose, no JSON object anywhere."),
			wantFailure: domain.ConsultFailNoInstruction,
		},
		{
			name:        "object with unknown action",
			reply:       []byte(`{"action":"proceed"}`),
			wantFailure: domain.ConsultFailUnknownAction,
		},
		{
			name:        "dispatch with missing agent field",
			reply:       []byte(`{"action":"dispatch","task_description":"do the thing"}`),
			wantFailure: domain.ConsultFailMissingField,
		},
		{
			name:        "dispatch with unknown agent",
			reply:       []byte(`{"action":"dispatch","agent":"no-such-agent","task_description":"do the thing"}`),
			wantFailure: domain.ConsultFailUnknownAgent,
		},
	}

	// Collect the failure class each condition produces, then assert they are all distinct.
	seen := make(map[domain.ConsultationFailure]string)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: tc.reply}
			c := newTestOrchestratorConsultant(fake, table)

			_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			ce := assertConsultationError(t, err, tc.wantFailure)
			if existing, conflict := seen[ce.Failure]; conflict {
				t.Errorf("failure class %q is shared by conditions %q and %q — failure classes must be distinguishable", ce.Failure, existing, tc.name)
			}
			seen[ce.Failure] = tc.name
		})
	}
}

// TestConsultRouting_ObjectFoundButFailsUnmarshal_FailsWithMalformedJSON verifies
// that when an object IS found in the reply but does not unmarshal into a valid
// routing response (wrong schema), the failure is ConsultFailMalformedJSON — not
// ConsultFailNoInstruction. This is the "found but invalid" class, distinct from
// "not found", satisfying AC4.3.
//
// Note: "wrong schema" here means a JSON array (not an object) at the top level;
// a plain extra-field object is tolerated by the routing response (AC3.4 from
// Stage 3). A true schema mismatch that json.Unmarshal cannot survive is needed.
func TestConsultRouting_ObjectFoundButFailsUnmarshal_FailsWithMalformedJSON(t *testing.T) {
	table := mustParseTable(t)
	// Valid JSON but a JSON array, not an object: ExtractJSONObject will not
	// find a JSON object, so this actually tests the NoInstruction path.
	// To test "object found but schema-invalid", we need the per-context
	// unmarshal to fail after extraction. Since routing's wireRoutingResponse
	// is permissive (ignores unknown fields), the only true mismatch is an
	// object whose required top-level value is the wrong type — e.g. action
	// as a non-string. This verifies that a located-but-unmarshalable object
	// produces ConsultFailMalformedJSON, not ConsultFailNoInstruction.
	fake := &fakeRawInvoker{reply: []byte(`{"action": ["not","a","string"]}`)}
	c := newTestOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	// The object was located (valid JSON object found) but the schema is violated
	// (action is an array, not a string). This must be ConsultFailMalformedJSON.
	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
}

// ===== T4.3: Pre-consultation — reply shape acceptance =====

// TestPreConsult_AcceptsVariousReplyShapes verifies that PreConsult extracts
// the JSON object using the same mechanism as ConsultRouting, accepting bare,
// prose-surrounded, and fenced reply formats.
func TestPreConsult_AcceptsVariousReplyShapes(t *testing.T) {
	table := mustParseTable(t)

	const bareJSON = `{"task_description":"run stage 1 first","constraints":"use python only"}`

	cases := []struct {
		name  string
		reply string
	}{
		{
			name:  "bare object",
			reply: bareJSON,
		},
		{
			name:  "object surrounded by prose",
			reply: "Before the run starts, please note:\n\n" + bareJSON + "\n\nGood luck with the execution.",
		},
		{
			name:  "object in fenced block with language tag",
			reply: "Pre-consultation advice:\n\n```json\n" + bareJSON + "\n```\n",
		},
		{
			name:  "object in fenced block without language tag",
			reply: "Pre-consultation advice:\n\n```\n" + bareJSON + "\n```\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: []byte(tc.reply)}
			c := newTestOrchestratorConsultant(fake, table)

			req := domain.ConsultationRequest{
				OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
				Context:               domain.ConsultContextPreConsultation,
			}
			advice, err := c.PreConsult(context.Background(), req)

			if err != nil {
				t.Fatalf("want no error for %s, got %v", tc.name, err)
			}
			if advice.TaskDescription != "run stage 1 first" {
				t.Errorf("want TaskDescription=%q for %s, got %q", "run stage 1 first", tc.name, advice.TaskDescription)
			}
			if advice.Constraints != "use python only" {
				t.Errorf("want Constraints=%q for %s, got %q", "use python only", tc.name, advice.Constraints)
			}
		})
	}
}

// TestPreConsult_NoObjectReply_FailsWithConsultFailNoInstruction verifies that
// when the orchestrator's reply contains no JSON object, PreConsult returns
// *ConsultationError with ConsultFailNoInstruction. The pre-consultation context
// uses the same extraction mechanism as routing (AC4.4).
func TestPreConsult_NoObjectReply_FailsWithConsultFailNoInstruction(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte("The run environment is healthy. No specific advice at this time.")}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	_, err := c.PreConsult(context.Background(), req)

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestPreConsult_ValidatesOwnSchema verifies that pre-consultation still applies
// its own schema validation after extraction: an object that is extracted
// successfully but does not match the pre-consultation wire schema (e.g. because
// a field has the wrong type) produces ConsultFailMalformedJSON — not
// ConsultFailNoInstruction. This confirms that extraction and schema validation
// are separate steps (AC4.4), and that schema validation still occurs.
func TestPreConsult_ValidatesOwnSchema(t *testing.T) {
	table := mustParseTable(t)
	// task_description must be a string; passing an integer causes schema failure.
	fake := &fakeRawInvoker{reply: []byte(`{"task_description": 42}`)}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	_, err := c.PreConsult(context.Background(), req)

	// The object was found but its field type is wrong for the pre-consultation schema.
	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
}
