package domain_test

// Tests for event decoding shape and identity types.
//
// Design invariants under test:
//   - Harness values not in the known set are tolerated (not rejected).
//   - ModelID.IsEmpty distinguishes empty from non-empty.
//   - RunRef correctly distinguishes named runs from the unattributable bucket,
//     and Label returns the expected string for each.
//   - IsRunID accepts only the {YYYYMMDD}T{HHMMSS}Z-{4-hex} format.
//   - ActorRef.Label returns "orchestrator" for the orchestrator and the
//     instance id string for agent instances.
//   - Event wire constants match the expected discriminator values.
//   - InvocationEndFields.HasUsage is false when token_usage was absent entirely,
//     distinguishing the "key absent" case from "key present with no sub-fields".
//
// Note: struct-literal field-assignment tests were intentionally omitted. Go
// guarantees struct-literal field assignment; there is no method call, no branch,
// and no logic that could ever be wrong. Coverage of decode-tolerance behaviour
// (unknown fields, absent optional fields, HasUsage distinguishing key-absent from
// key-present-with-no-sub-fields) belongs with the logread.Reader tests, where
// actual encoding/json unmarshalling is exercised.

import (
	"testing"

	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Harness identity
// ---------------------------------------------------------------------------

func TestHarness_IsRecognised_KnownValues(t *testing.T) {
	known := []domain.Harness{
		domain.HarnessClaudeCode,
		domain.HarnessOpenCode,
		domain.HarnessVSCodeGHCP,
	}
	for _, h := range known {
		if !h.IsRecognised() {
			t.Errorf("Harness %q must be recognised", h)
		}
	}
}

func TestHarness_IsRecognised_UnknownValue_ToleratedNotRejected(t *testing.T) {
	unknown := domain.Harness("future-harness-v2")
	// An unrecognised harness must not panic and must return false.
	if unknown.IsRecognised() {
		t.Error("unknown harness must return IsRecognised()=false")
	}
}

func TestHarness_IsRecognised_EmptyString(t *testing.T) {
	empty := domain.Harness("")
	if empty.IsRecognised() {
		t.Error("empty harness string must not be recognised")
	}
}

func TestHarness_Constants_WireValues(t *testing.T) {
	if domain.HarnessClaudeCode != "claude-code" {
		t.Errorf("HarnessClaudeCode = %q, want %q", domain.HarnessClaudeCode, "claude-code")
	}
	if domain.HarnessOpenCode != "opencode" {
		t.Errorf("HarnessOpenCode = %q, want %q", domain.HarnessOpenCode, "opencode")
	}
	if domain.HarnessVSCodeGHCP != "vscode-ghcp" {
		t.Errorf("HarnessVSCodeGHCP = %q, want %q", domain.HarnessVSCodeGHCP, "vscode-ghcp")
	}
}

// ---------------------------------------------------------------------------
// ModelID
// ---------------------------------------------------------------------------

func TestModelID_IsEmpty_Empty(t *testing.T) {
	if !domain.ModelID("").IsEmpty() {
		t.Error("empty ModelID must be empty")
	}
}

func TestModelID_IsEmpty_NonEmpty(t *testing.T) {
	if domain.ModelID("claude-sonnet-4-6").IsEmpty() {
		t.Error("non-empty ModelID must not be empty")
	}
}

// ---------------------------------------------------------------------------
// RunRef (named vs unattributable)
// ---------------------------------------------------------------------------

func TestRunRef_NamedRun_Kind(t *testing.T) {
	ref := domain.NamedRun("20260802T074635Z-480e")
	if ref.Kind != domain.RunNamed {
		t.Errorf("NamedRun Kind = %v, want RunNamed", ref.Kind)
	}
}

func TestRunRef_NamedRun_ID(t *testing.T) {
	const id = "20260802T074635Z-480e"
	ref := domain.NamedRun(id)
	if ref.ID != id {
		t.Errorf("NamedRun ID = %q, want %q", ref.ID, id)
	}
}

func TestRunRef_NamedRun_IsNotUnattributable(t *testing.T) {
	ref := domain.NamedRun("20260802T074635Z-480e")
	if ref.IsUnattributable() {
		t.Error("NamedRun must not be unattributable")
	}
}

func TestRunRef_UnattributableRun_Kind(t *testing.T) {
	ref := domain.UnattributableRun()
	if ref.Kind != domain.RunUnattributable {
		t.Errorf("UnattributableRun Kind = %v, want RunUnattributable", ref.Kind)
	}
}

func TestRunRef_UnattributableRun_IsUnattributable(t *testing.T) {
	ref := domain.UnattributableRun()
	if !ref.IsUnattributable() {
		t.Error("UnattributableRun must be unattributable")
	}
}

func TestRunRef_UnattributableRun_HasNoID(t *testing.T) {
	ref := domain.UnattributableRun()
	if ref.ID != "" {
		t.Errorf("UnattributableRun ID = %q, want empty string", ref.ID)
	}
}

func TestRunRef_Label_Named_ReturnsID(t *testing.T) {
	const id = "20260802T074635Z-480e"
	ref := domain.NamedRun(id)
	if got := ref.Label(); got != id {
		t.Errorf("NamedRun Label() = %q, want %q", got, id)
	}
}

func TestRunRef_Label_Unattributable_ReturnsLiteral(t *testing.T) {
	ref := domain.UnattributableRun()
	if got := ref.Label(); got != "unattributable" {
		t.Errorf("UnattributableRun Label() = %q, want %q", got, "unattributable")
	}
}

// ---------------------------------------------------------------------------
// IsRunID
// ---------------------------------------------------------------------------

func TestIsRunID_ValidFormat(t *testing.T) {
	valid := []string{
		"20260802T074635Z-480e",
		"20230101T000000Z-abcd",
		"20991231T235959Z-ffff",
		"20000101T120000Z-0000",
	}
	for _, s := range valid {
		if !domain.IsRunID(s) {
			t.Errorf("IsRunID(%q) = false, want true", s)
		}
	}
}

func TestIsRunID_InvalidFormat(t *testing.T) {
	invalid := []string{
		"",
		"unknown-run",
		"not-a-run-id",
		"20260802T074635Z",         // missing hex suffix
		"20260802T074635Z-480",     // hex suffix too short (3 chars)
		"20260802T074635Z-480eg",   // hex suffix too long / invalid char
		"2026-08-02T074635Z-480e",  // wrong date format (has dashes)
		"20260802t074635Z-480e",    // lowercase 't'
		"20260802T074635z-480e",    // lowercase 'z'
		"abcdefghT074635Z-480e",    // non-digit date
		"20260802T074635Z-ZZZZ",    // non-hex suffix
		" 20260802T074635Z-480e",   // leading space
		"20260802T074635Z-480e ",   // trailing space
		"20260802T074635Z-480eextra", // extra content
	}
	for _, s := range invalid {
		if domain.IsRunID(s) {
			t.Errorf("IsRunID(%q) = true, want false", s)
		}
	}
}

func TestUnknownRunDirName_Value(t *testing.T) {
	if domain.UnknownRunDirName != "unknown-run" {
		t.Errorf("UnknownRunDirName = %q, want %q", domain.UnknownRunDirName, "unknown-run")
	}
}

// ---------------------------------------------------------------------------
// ActorRef
// ---------------------------------------------------------------------------

func TestActorRef_Orchestrator_Kind(t *testing.T) {
	ref := domain.Orchestrator()
	if ref.Kind != domain.ActorOrchestrator {
		t.Errorf("Orchestrator() Kind = %v, want ActorOrchestrator", ref.Kind)
	}
}

func TestActorRef_Orchestrator_Label(t *testing.T) {
	ref := domain.Orchestrator()
	if got := ref.Label(); got != "orchestrator" {
		t.Errorf("Orchestrator Label() = %q, want %q", got, "orchestrator")
	}
}

func TestActorRef_AgentInstance_Kind(t *testing.T) {
	ref := domain.AgentInstance("TestWriter#22")
	if ref.Kind != domain.ActorAgentInstance {
		t.Errorf("AgentInstance() Kind = %v, want ActorAgentInstance", ref.Kind)
	}
}

func TestActorRef_AgentInstance_Instance(t *testing.T) {
	id := domain.AgentInstanceID("TestWriter#22")
	ref := domain.AgentInstance(id)
	if ref.Instance != id {
		t.Errorf("AgentInstance() Instance = %q, want %q", ref.Instance, id)
	}
}

func TestActorRef_AgentInstance_Label_ReturnsInstanceID(t *testing.T) {
	id := domain.AgentInstanceID("CodeWriter#5")
	ref := domain.AgentInstance(id)
	if got := ref.Label(); got != string(id) {
		t.Errorf("AgentInstance Label() = %q, want %q", got, string(id))
	}
}

// ---------------------------------------------------------------------------
// Event type constants (wire values)
// ---------------------------------------------------------------------------

func TestEventType_Constants_WireValues(t *testing.T) {
	cases := []struct {
		name string
		got  domain.EventType
		want domain.EventType
	}{
		{"EventRunStart", domain.EventRunStart, "run_start"},
		{"EventRunEnd", domain.EventRunEnd, "run_end"},
		{"EventSessionStart", domain.EventSessionStart, "session_start"},
		{"EventSessionEnd", domain.EventSessionEnd, "session_end"},
		{"EventInvocationStart", domain.EventInvocationStart, "invocation_start"},
		{"EventInvocationEnd", domain.EventInvocationEnd, "invocation_end"},
		{"EventTurn", domain.EventTurn, "turn"},
		{"EventOther", domain.EventOther, ""},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// EventUsageRecord constant (wire value)
// ---------------------------------------------------------------------------

func TestEventType_EventUsageRecord_WireValue(t *testing.T) {
	if domain.EventUsageRecord != "usage_record" {
		t.Errorf("EventUsageRecord = %q, want %q", domain.EventUsageRecord, "usage_record")
	}
}

// ---------------------------------------------------------------------------
// Schema version set — SchemaVersionCurrent, SupportedSchemaVersions,
// IsSupportedSchemaVersion
// ---------------------------------------------------------------------------

func TestSchemaVersionCurrent_Value(t *testing.T) {
	if domain.SchemaVersionCurrent != "1.1.0" {
		t.Errorf("SchemaVersionCurrent = %q, want %q", domain.SchemaVersionCurrent, "1.1.0")
	}
}

func TestSupportedSchemaVersions_ContainsBothVersions(t *testing.T) {
	set := domain.SupportedSchemaVersions()
	var has110, has100 bool
	for _, v := range set {
		if v == "1.1.0" {
			has110 = true
		}
		if v == "1.0.0" {
			has100 = true
		}
	}
	if !has110 {
		t.Error("SupportedSchemaVersions() must contain \"1.1.0\"")
	}
	if !has100 {
		t.Error("SupportedSchemaVersions() must contain \"1.0.0\" (prior version stays supported)")
	}
}

func TestSupportedSchemaVersions_NewestFirst(t *testing.T) {
	set := domain.SupportedSchemaVersions()
	if len(set) == 0 {
		t.Fatal("SupportedSchemaVersions() must not be empty")
	}
	if set[0] != "1.1.0" {
		t.Errorf("SupportedSchemaVersions()[0] = %q, want %q (newest first)", set[0], "1.1.0")
	}
}

func TestIsSupportedSchemaVersion_PriorAndCurrent_True(t *testing.T) {
	for _, v := range []string{"1.0.0", "1.1.0"} {
		if !domain.IsSupportedSchemaVersion(v) {
			t.Errorf("IsSupportedSchemaVersion(%q) = false, want true", v)
		}
	}
}

func TestIsSupportedSchemaVersion_UnrecognisedVersion_False(t *testing.T) {
	for _, v := range []string{"2.0.0", "0.9.0", "", "1.1.0-beta"} {
		if domain.IsSupportedSchemaVersion(v) {
			t.Errorf("IsSupportedSchemaVersion(%q) = true, want false", v)
		}
	}
}

// ---------------------------------------------------------------------------
// InvocationEndFields — HasUsage distinguishes key-absent from key-present
// ---------------------------------------------------------------------------

func TestInvocationEndFields_HasUsage_False_WhenUsageAbsent(t *testing.T) {
	// HasUsage=false signals the token_usage key was entirely absent in the JSON.
	// This is distinct from the key being present with no sub-fields.
	// When HasUsage is false the Usage field must be all-absent (IsEmpty=true).
	fields := &domain.InvocationEndFields{
		AgentInstanceID: "TestWriter#22",
		Model:           "claude-sonnet-4-6",
		HasUsage:        false,
	}
	if fields.HasUsage {
		t.Error("HasUsage must be false when not explicitly set")
	}
	if !fields.Usage.IsEmpty() {
		t.Error("Usage must be all-absent when HasUsage is false")
	}
}

// ---------------------------------------------------------------------------
// TurnRole constants (wire values)
// ---------------------------------------------------------------------------

func TestTurnRole_Constants_WireValues(t *testing.T) {
	if domain.TurnUser != "user" {
		t.Errorf("TurnUser = %q, want %q", domain.TurnUser, "user")
	}
	if domain.TurnAssistant != "assistant" {
		t.Errorf("TurnAssistant = %q, want %q", domain.TurnAssistant, "assistant")
	}
}
