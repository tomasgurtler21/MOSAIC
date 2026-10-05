package roleenv_test

import (
	"reflect"
	"testing"

	"mosaic-run/internal/harness/roleenv"
)

func TestSubagentEnv_WithAllValues_ReturnsRunIDRoleAndInstanceIDInOrder(t *testing.T) {
	got := roleenv.SubagentEnv("20261003T185044Z-07e9", "Research#3")

	want := []string{
		"MOSAIC_RUN_ID=20261003T185044Z-07e9",
		"MOSAIC_ROLE=subagent",
		"MOSAIC_AGENT_INSTANCE_ID=Research#3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubagentEnv = %v, want %v", got, want)
	}
}

func TestSubagentEnv_WithEmptyValues_OmitsThoseEntriesButKeepsRole(t *testing.T) {
	got := roleenv.SubagentEnv("", "")

	want := []string{"MOSAIC_ROLE=subagent"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubagentEnv(empty, empty) = %v, want %v", got, want)
	}
}

func TestSubagentEnv_WithEmptyRunIDOnly_OmitsRunID(t *testing.T) {
	got := roleenv.SubagentEnv("", "Research#3")

	want := []string{"MOSAIC_ROLE=subagent", "MOSAIC_AGENT_INSTANCE_ID=Research#3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubagentEnv(empty run id) = %v, want %v", got, want)
	}
}

func TestOrchestratorEnv_WithRunID_ReturnsRunIDAndRoleWithoutInstanceID(t *testing.T) {
	got := roleenv.OrchestratorEnv("20261003T185044Z-07e9")

	want := []string{"MOSAIC_RUN_ID=20261003T185044Z-07e9", "MOSAIC_ROLE=orchestrator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrchestratorEnv = %v, want %v", got, want)
	}
}

func TestOrchestratorEnv_WithEmptyRunID_ReturnsOnlyRole(t *testing.T) {
	got := roleenv.OrchestratorEnv("")

	want := []string{"MOSAIC_ROLE=orchestrator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrchestratorEnv(empty) = %v, want %v", got, want)
	}
}

func TestNewConfig_AppliesOptionsInOrderAndIgnoresNil(t *testing.T) {
	got := roleenv.NewConfig(roleenv.WithRunID("20261003T185044Z-aaaa"), nil, roleenv.WithRunID("20261003T185044Z-bbbb"))

	if got.RunID != "20261003T185044Z-bbbb" {
		t.Errorf("Config.RunID = %q, want the last option's value", got.RunID)
	}
}

func TestNewConfig_WithoutOptions_LeavesRunIDEmpty(t *testing.T) {
	if got := roleenv.NewConfig(); got.RunID != "" {
		t.Errorf("Config.RunID = %q, want empty", got.RunID)
	}
}
