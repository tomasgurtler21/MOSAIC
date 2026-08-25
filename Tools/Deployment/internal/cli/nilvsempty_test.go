package cli_test

// nilvsempty_test.go validates the prerequisite assumption that goccy/go-yaml preserves
// the nil-vs-non-nil-empty distinction on []string struct fields.
//
// This distinction underpins the PreAnswersFromSelectionsFile fix: a nil slice (YAML key
// absent) must be distinguishable from a non-nil empty slice (YAML key present as []).
// If any test here fails, the implementation cannot rely on struct-level nil checks and
// must use pointer fields or a custom unmarshaller instead.
//
// Fields covered: infrastructure_agents, utility_agents, hooks.
// (workflows is intentionally excluded; its empty-list behavior differs by design.)

import (
	"testing"

	"github.com/goccy/go-yaml"
)

// parseSelectionsStub is a local struct that mirrors the three fields of selectionsFile
// whose nil-vs-empty behaviour we need to validate. It is declared here rather than using
// selectionsFile directly so the test stays in package cli_test and does not require
// access to the unexported production type.
type parseSelectionsStub struct {
	InfrastructureAgents []string `yaml:"infrastructure_agents"`
	UtilityAgents        []string `yaml:"utility_agents"`
	Hooks                []string `yaml:"hooks"`
}

// unmarshalStub is a helper that unmarshals YAML content into parseSelectionsStub.
func unmarshalStub(t *testing.T, content string) parseSelectionsStub {
	t.Helper()
	var s parseSelectionsStub
	if err := yaml.Unmarshal([]byte(content), &s); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	return s
}

// ---------------------------------------------------------------------------
// infrastructure_agents: absent key → nil
// ---------------------------------------------------------------------------

// TestYAML_InfrastructureAgents_AbsentKey_IsNil verifies that when the
// infrastructure_agents key is absent from the YAML document, the corresponding Go
// field is nil. A nil slice means "key not specified" and must not produce a pre-answer.
func TestYAML_InfrastructureAgents_AbsentKey_IsNil(t *testing.T) {
	s := unmarshalStub(t, "{}\n")

	if s.InfrastructureAgents != nil {
		t.Errorf("InfrastructureAgents = %v (non-nil), want nil; "+
			"an absent YAML key must produce a nil slice so the deploy tool can "+
			"distinguish \"not specified\" from \"explicitly none\"",
			s.InfrastructureAgents)
	}
}

// ---------------------------------------------------------------------------
// infrastructure_agents: [] → non-nil empty slice
// ---------------------------------------------------------------------------

// TestYAML_InfrastructureAgents_ExplicitEmptyList_IsNonNilEmpty verifies that the YAML
// value `infrastructure_agents: []` produces a non-nil empty slice. A non-nil empty
// slice means "explicitly none" and must produce a pre-answer entry (value "") so the
// deploy tool does not ask an interactive question.
func TestYAML_InfrastructureAgents_ExplicitEmptyList_IsNonNilEmpty(t *testing.T) {
	s := unmarshalStub(t, "infrastructure_agents: []\n")

	if s.InfrastructureAgents == nil {
		t.Error("InfrastructureAgents is nil for 'infrastructure_agents: []'; " +
			"goccy/go-yaml must produce a non-nil empty slice for an explicit empty list " +
			"so the nil-check guard can distinguish it from a missing key")
	}
	if len(s.InfrastructureAgents) != 0 {
		t.Errorf("InfrastructureAgents = %v, want empty slice; "+
			"an explicit empty list must contain no elements", s.InfrastructureAgents)
	}
}

// ---------------------------------------------------------------------------
// infrastructure_agents: [values] → populated slice
// ---------------------------------------------------------------------------

// TestYAML_InfrastructureAgents_PopulatedList_ContainsValues verifies that a non-empty
// infrastructure_agents list produces the expected Go string slice.
func TestYAML_InfrastructureAgents_PopulatedList_ContainsValues(t *testing.T) {
	s := unmarshalStub(t, "infrastructure_agents:\n  - checkpoint-manager-git\n  - commit-manager-git\n")

	want := []string{"checkpoint-manager-git", "commit-manager-git"}
	if len(s.InfrastructureAgents) != len(want) {
		t.Fatalf("InfrastructureAgents = %v, want %v", s.InfrastructureAgents, want)
	}
	for i, v := range want {
		if s.InfrastructureAgents[i] != v {
			t.Errorf("InfrastructureAgents[%d] = %q, want %q", i, s.InfrastructureAgents[i], v)
		}
	}
}

// ---------------------------------------------------------------------------
// utility_agents: absent key → nil
// ---------------------------------------------------------------------------

// TestYAML_UtilityAgents_AbsentKey_IsNil verifies that an absent utility_agents key
// produces a nil slice.
func TestYAML_UtilityAgents_AbsentKey_IsNil(t *testing.T) {
	s := unmarshalStub(t, "{}\n")

	if s.UtilityAgents != nil {
		t.Errorf("UtilityAgents = %v (non-nil), want nil; "+
			"an absent YAML key must produce a nil slice", s.UtilityAgents)
	}
}

// ---------------------------------------------------------------------------
// utility_agents: [] → non-nil empty slice
// ---------------------------------------------------------------------------

// TestYAML_UtilityAgents_ExplicitEmptyList_IsNonNilEmpty verifies that
// `utility_agents: []` produces a non-nil empty slice.
func TestYAML_UtilityAgents_ExplicitEmptyList_IsNonNilEmpty(t *testing.T) {
	s := unmarshalStub(t, "utility_agents: []\n")

	if s.UtilityAgents == nil {
		t.Error("UtilityAgents is nil for 'utility_agents: []'; " +
			"goccy/go-yaml must produce a non-nil empty slice for an explicit empty list")
	}
	if len(s.UtilityAgents) != 0 {
		t.Errorf("UtilityAgents = %v, want empty slice", s.UtilityAgents)
	}
}

// ---------------------------------------------------------------------------
// hooks: absent key → nil
// ---------------------------------------------------------------------------

// TestYAML_Hooks_AbsentKey_IsNil verifies that an absent hooks key produces a nil slice.
func TestYAML_Hooks_AbsentKey_IsNil(t *testing.T) {
	s := unmarshalStub(t, "{}\n")

	if s.Hooks != nil {
		t.Errorf("Hooks = %v (non-nil), want nil; "+
			"an absent YAML key must produce a nil slice", s.Hooks)
	}
}

// ---------------------------------------------------------------------------
// hooks: [] → non-nil empty slice
// ---------------------------------------------------------------------------

// TestYAML_Hooks_ExplicitEmptyList_IsNonNilEmpty verifies that `hooks: []` produces
// a non-nil empty slice.
func TestYAML_Hooks_ExplicitEmptyList_IsNonNilEmpty(t *testing.T) {
	s := unmarshalStub(t, "hooks: []\n")

	if s.Hooks == nil {
		t.Error("Hooks is nil for 'hooks: []'; " +
			"goccy/go-yaml must produce a non-nil empty slice for an explicit empty list")
	}
	if len(s.Hooks) != 0 {
		t.Errorf("Hooks = %v, want empty slice", s.Hooks)
	}
}

// ---------------------------------------------------------------------------
// All three fields in one document
// ---------------------------------------------------------------------------

// TestYAML_AllThreeFields_MixedPresence verifies that nil-vs-empty is correctly preserved
// across all three guarded fields simultaneously when some are present and some absent.
func TestYAML_AllThreeFields_MixedPresence(t *testing.T) {
	// infrastructure_agents present as empty; utility_agents present with a value; hooks absent.
	s := unmarshalStub(t, "infrastructure_agents: []\nutility_agents:\n  - agent-a\n")

	if s.InfrastructureAgents == nil {
		t.Error("InfrastructureAgents is nil; 'infrastructure_agents: []' must produce non-nil empty slice")
	}
	if len(s.InfrastructureAgents) != 0 {
		t.Errorf("InfrastructureAgents = %v, want empty", s.InfrastructureAgents)
	}

	if len(s.UtilityAgents) != 1 || s.UtilityAgents[0] != "agent-a" {
		t.Errorf("UtilityAgents = %v, want [agent-a]", s.UtilityAgents)
	}

	if s.Hooks != nil {
		t.Errorf("Hooks = %v (non-nil), want nil for absent hooks key", s.Hooks)
	}
}
