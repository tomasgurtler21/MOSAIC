package domain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
)

func TestRunStateApply_CompleteAgents_AddsIdentifiersToCompletedAgents(t *testing.T) {
	s := domain.RunState{}

	next := s.Apply(domain.StateDelta{CompleteAgents: []string{"a", "b"}})

	if !next.CompletedAgents["a"] || !next.CompletedAgents["b"] || len(next.CompletedAgents) != 2 {
		t.Errorf("CompletedAgents = %v, want a and b", next.CompletedAgents)
	}
}

func TestRunStateApply_CompleteAgents_KeepsExistingEntries(t *testing.T) {
	s := domain.RunState{CompletedAgents: map[string]bool{"a": true}}

	next := s.Apply(domain.StateDelta{CompleteAgents: []string{"b"}})

	if !next.CompletedAgents["a"] || !next.CompletedAgents["b"] {
		t.Errorf("CompletedAgents = %v, want a and b", next.CompletedAgents)
	}
}

func TestRunStateApply_CompleteAgents_DoesNotMutateReceiver(t *testing.T) {
	s := domain.RunState{CompletedAgents: map[string]bool{"a": true}}

	_ = s.Apply(domain.StateDelta{CompleteAgents: []string{"b"}})

	if s.CompletedAgents["b"] || len(s.CompletedAgents) != 1 {
		t.Errorf("receiver CompletedAgents = %v, want unchanged {a}", s.CompletedAgents)
	}
}

func TestRunStateApply_EmptyCompleteAgents_KeepsNilMapNil(t *testing.T) {
	next := domain.RunState{}.Apply(domain.StateDelta{SequenceIncrement: 1})

	if next.CompletedAgents != nil {
		t.Errorf("CompletedAgents = %v, want nil", next.CompletedAgents)
	}
}

func TestRunStateJSON_CompletedAgents_RoundTrips(t *testing.T) {
	s := domain.RunState{CompletedAgents: map[string]bool{"a": true}}

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back domain.RunState
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}

	if !back.CompletedAgents["a"] {
		t.Errorf("round-trip lost completed agent; json=%s", raw)
	}
}

func TestRunStateJSON_NoCompletedAgents_OmitsField(t *testing.T) {
	raw, err := json.Marshal(domain.RunState{SchemaVersion: 1})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(raw), "completed_agents") {
		t.Errorf("empty CompletedAgents must be omitted; json=%s", raw)
	}
}

func TestRunStateJSON_DocumentWithoutCompletedAgents_DecodesToEmptyState(t *testing.T) {
	doc := `{"schema_version":1,"test_id":"t","run_number":1,"agent_dispatch":{"x":"tok"}}`

	var s domain.RunState
	if err := json.Unmarshal([]byte(doc), &s); err != nil {
		t.Fatalf("legacy document must decode: %v", err)
	}

	if s.CompletedAgents["x"] || len(s.CompletedAgents) != 0 {
		t.Errorf("CompletedAgents = %v, want empty", s.CompletedAgents)
	}
}
