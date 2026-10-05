package deviation_test

// Tests for the last_error_reason field on the consultation wire request:
// always present, JSON null when absent, a string when the request carries one.

import (
	"context"
	"encoding/json"
	"testing"

	"mosaic-run/internal/domain"
)

// wireKeys unmarshals a captured payload into raw values so absent keys and
// explicit nulls can be told apart.
func wireKeys(t *testing.T, payload []byte) map[string]json.RawMessage {
	t.Helper()
	if len(payload) == 0 {
		t.Fatal("want a payload sent to the orchestrator, got none")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("want valid JSON wire payload, got %v", err)
	}
	return raw
}

func TestOrchestratorConsultant_RoutingRequestAlwaysCarriesAllFourKeys(t *testing.T) {
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := newTestOrchestratorConsultant(fake, mustParseTable(t))

	c.ConsultRouting(context.Background(), domain.ConsultationRequest{ //nolint:errcheck
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
	})

	raw := wireKeys(t, fake.sent)
	for _, key := range []string{"orchestration_artifact", "context", "last_status_message", "last_error_reason"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("want key %q present in routing wire request, got keys %v", key, raw)
		}
	}
}

func TestOrchestratorConsultant_LastErrorReasonIsNullWhenNil(t *testing.T) {
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := newTestOrchestratorConsultant(fake, mustParseTable(t))

	c.ConsultRouting(context.Background(), domain.ConsultationRequest{ //nolint:errcheck
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
		LastStatusMessage:     strptr("agent-a done"),
		LastErrorReason:       nil,
	})

	raw := wireKeys(t, fake.sent)
	got, present := raw["last_error_reason"]
	if !present {
		t.Fatal("want last_error_reason present (as null), got absent")
	}
	if string(got) != "null" {
		t.Errorf("want last_error_reason=null, got %s", got)
	}
}

func TestOrchestratorConsultant_LastErrorReasonIsStringVerbatimWhenSet(t *testing.T) {
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := newTestOrchestratorConsultant(fake, mustParseTable(t))
	reason := "line one\nline \"two\" | tail"

	c.ConsultRouting(context.Background(), domain.ConsultationRequest{ //nolint:errcheck
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
		LastStatusMessage:     strptr("blocked"),
		LastErrorReason:       &reason,
	})

	raw := wireKeys(t, fake.sent)
	var got *string
	if err := json.Unmarshal(raw["last_error_reason"], &got); err != nil {
		t.Fatalf("want last_error_reason to decode as a string, got %s (%v)", raw["last_error_reason"], err)
	}
	if got == nil || *got != reason {
		t.Errorf("want last_error_reason %q verbatim, got %v", reason, got)
	}
}

func TestOrchestratorConsultant_EmptyLastErrorReasonIsEmptyStringNotNull(t *testing.T) {
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := newTestOrchestratorConsultant(fake, mustParseTable(t))
	empty := ""

	c.ConsultRouting(context.Background(), domain.ConsultationRequest{ //nolint:errcheck
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
		LastErrorReason:       &empty,
	})

	raw := wireKeys(t, fake.sent)
	if string(raw["last_error_reason"]) != `""` {
		t.Errorf("want last_error_reason=\"\" for a pointer to an empty string, got %s", raw["last_error_reason"])
	}
}

func TestOrchestratorConsultant_PreConsultSendsNullLastErrorReasonEvenWhenRequestCarriesOne(t *testing.T) {
	fake := &fakeRawInvoker{reply: []byte(`{}`)}
	c := newTestOrchestratorConsultant(fake, mustParseTable(t))
	reason := "should never reach the wire"

	c.PreConsult(context.Background(), domain.ConsultationRequest{ //nolint:errcheck
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
		LastErrorReason:       &reason,
	})

	raw := wireKeys(t, fake.sent)
	got, present := raw["last_error_reason"]
	if !present {
		t.Fatal("want last_error_reason present (as null) in pre-consultation request, got absent")
	}
	if string(got) != "null" {
		t.Errorf("want last_error_reason=null for pre-consultation, got %s", got)
	}
	if lsm := string(raw["last_status_message"]); lsm != "null" {
		t.Errorf("want last_status_message=null for pre-consultation, got %s", lsm)
	}
}
