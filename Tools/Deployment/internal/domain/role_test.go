package domain_test

// role_test.go covers domain.ParseAgentRole and the renamed RoleSubagent constant.
//
// After Stage 4 the internal role vocabulary aligns with the frontmatter vocabulary:
//
//	RoleSubagent     = "subagent"     (renamed from RoleWorker / "worker")
//	RoleOrchestrator = "orchestrator" (unchanged)
//	RoleUtility      = "utility"      (internal marker only — not a frontmatter value)
//
// ParseAgentRole is the single entry point for converting a frontmatter `role` scalar
// to an AgentRole value. It accepts only "subagent" and "orchestrator"; every other
// input (including "utility", "worker", the empty string, and arbitrary strings) is
// rejected with ok = false so the caller can record an issue rather than silently
// coerce an agent's role.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// RoleSubagent constant value
// ---------------------------------------------------------------------------

// TestRoleSubagent_Value_IsSubagent verifies that RoleSubagent equals the string
// "subagent", aligning the internal constant with the frontmatter vocabulary and
// with ProtocolVariant's existing "subagent" value.
func TestRoleSubagent_Value_IsSubagent(t *testing.T) {
	if string(domain.RoleSubagent) != "subagent" {
		t.Errorf("RoleSubagent = %q, want %q", domain.RoleSubagent, "subagent")
	}
}

// TestRoleOrchestrator_Value_IsOrchestrator verifies that RoleOrchestrator retains its
// existing value and is unchanged by the Stage 4 rename.
func TestRoleOrchestrator_Value_IsOrchestrator(t *testing.T) {
	if string(domain.RoleOrchestrator) != "orchestrator" {
		t.Errorf("RoleOrchestrator = %q, want %q", domain.RoleOrchestrator, "orchestrator")
	}
}

// TestRoleUtility_Value_IsUtility verifies that RoleUtility retains its existing value.
// Utility agents are never exposed via frontmatter; the constant is an internal marker
// only and is not changed by the vocabulary alignment.
func TestRoleUtility_Value_IsUtility(t *testing.T) {
	if string(domain.RoleUtility) != "utility" {
		t.Errorf("RoleUtility = %q, want %q", domain.RoleUtility, "utility")
	}
}

// ---------------------------------------------------------------------------
// ParseAgentRole — accepted values ("subagent" and "orchestrator")
// ---------------------------------------------------------------------------

// TestParseAgentRole_Subagent_ReturnsRoleSubagentAndTrue verifies that "subagent" is
// accepted and maps to RoleSubagent. This is the canonical frontmatter value for
// worker agents.
func TestParseAgentRole_Subagent_ReturnsRoleSubagentAndTrue(t *testing.T) {
	role, ok := domain.ParseAgentRole("subagent")
	if !ok {
		t.Fatal("ParseAgentRole(\"subagent\") returned ok = false; want ok = true")
	}
	if role != domain.RoleSubagent {
		t.Errorf("ParseAgentRole(\"subagent\") = %q, want RoleSubagent (%q)",
			role, domain.RoleSubagent)
	}
}

// TestParseAgentRole_Orchestrator_ReturnsRoleOrchestratorAndTrue verifies that
// "orchestrator" is accepted and maps to RoleOrchestrator.
func TestParseAgentRole_Orchestrator_ReturnsRoleOrchestratorAndTrue(t *testing.T) {
	role, ok := domain.ParseAgentRole("orchestrator")
	if !ok {
		t.Fatal("ParseAgentRole(\"orchestrator\") returned ok = false; want ok = true")
	}
	if role != domain.RoleOrchestrator {
		t.Errorf("ParseAgentRole(\"orchestrator\") = %q, want RoleOrchestrator (%q)",
			role, domain.RoleOrchestrator)
	}
}

// ---------------------------------------------------------------------------
// ParseAgentRole — rejected values
// ---------------------------------------------------------------------------

// TestParseAgentRole_Utility_ReturnsFalse verifies that "utility" is rejected.
// Utility agents carry no frontmatter role value; the "utility" string is an
// internal marker and must not be accepted via the frontmatter parse path.
func TestParseAgentRole_Utility_ReturnsFalse(t *testing.T) {
	_, ok := domain.ParseAgentRole("utility")
	if ok {
		t.Error("ParseAgentRole(\"utility\") returned ok = true; " +
			"\"utility\" is not a frontmatter role value and must be rejected")
	}
}

// TestParseAgentRole_Empty_ReturnsFalse verifies that the empty string is rejected.
// An absent frontmatter field is handled by the caller (path-derived fallback), not
// by passing the empty string to ParseAgentRole.
func TestParseAgentRole_Empty_ReturnsFalse(t *testing.T) {
	_, ok := domain.ParseAgentRole("")
	if ok {
		t.Error("ParseAgentRole(\"\") returned ok = true; empty string must be rejected")
	}
}

// TestParseAgentRole_OldWorkerConstant_ReturnsFalse verifies that the pre-Stage 4
// internal role value "worker" is rejected. The frontmatter vocabulary never used
// "worker"; agents in source files declare "subagent", so "worker" arriving in a
// frontmatter field is a malformed value.
func TestParseAgentRole_OldWorkerConstant_ReturnsFalse(t *testing.T) {
	_, ok := domain.ParseAgentRole("worker")
	if ok {
		t.Error("ParseAgentRole(\"worker\") returned ok = true; " +
			"\"worker\" is not a valid frontmatter role value and must be rejected")
	}
}

// TestParseAgentRole_UnknownValue_ReturnsFalse verifies that an arbitrary unknown
// string is rejected. The design requires unrecognised role values to be reported
// rather than silently coerced to any role.
func TestParseAgentRole_UnknownValue_ReturnsFalse(t *testing.T) {
	_, ok := domain.ParseAgentRole("unknown-role-value")
	if ok {
		t.Error("ParseAgentRole(\"unknown-role-value\") returned ok = true; " +
			"unknown values must be rejected")
	}
}

// TestParseAgentRole_CaseSensitive_UppercaseSubagent_ReturnsFalse verifies that
// "SUBAGENT" is rejected. Role values in frontmatter are lower-case; ParseAgentRole
// must perform an exact, case-sensitive comparison.
func TestParseAgentRole_CaseSensitive_UppercaseSubagent_ReturnsFalse(t *testing.T) {
	_, ok := domain.ParseAgentRole("SUBAGENT")
	if ok {
		t.Error("ParseAgentRole(\"SUBAGENT\") returned ok = true; " +
			"ParseAgentRole must be case-sensitive — only \"subagent\" is accepted")
	}
}

// TestParseAgentRole_CaseSensitive_UppercaseOrchestrator_ReturnsFalse verifies that
// "ORCHESTRATOR" is rejected for the same reason as "SUBAGENT".
func TestParseAgentRole_CaseSensitive_UppercaseOrchestrator_ReturnsFalse(t *testing.T) {
	_, ok := domain.ParseAgentRole("ORCHESTRATOR")
	if ok {
		t.Error("ParseAgentRole(\"ORCHESTRATOR\") returned ok = true; " +
			"ParseAgentRole must be case-sensitive — only \"orchestrator\" is accepted")
	}
}

// TestParseAgentRole_Mixed_TableDriven runs the accepted/rejected cases as a table
// to catch off-by-one expansion in future changes. A value that should be accepted
// has a non-zero want; a value that should be rejected has want == "".
func TestParseAgentRole_Mixed_TableDriven(t *testing.T) {
	cases := []struct {
		input   string
		wantOk  bool
		wantVal domain.AgentRole
	}{
		{"subagent", true, domain.RoleSubagent},
		{"orchestrator", true, domain.RoleOrchestrator},
		{"utility", false, ""},
		{"worker", false, ""},
		{"", false, ""},
		{"SUBAGENT", false, ""},
		{"Subagent", false, ""},
		{"orchestrator ", false, ""},  // trailing space
		{" subagent", false, ""},     // leading space
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got, ok := domain.ParseAgentRole(tc.input)
			if ok != tc.wantOk {
				t.Errorf("ParseAgentRole(%q) ok = %v, want %v", tc.input, ok, tc.wantOk)
			}
			if tc.wantOk && got != tc.wantVal {
				t.Errorf("ParseAgentRole(%q) = %q, want %q", tc.input, got, tc.wantVal)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VariantForRole — role-to-protocol-variant mapping survives the rename
// ---------------------------------------------------------------------------

// TestVariantForRole_RoleSubagent_ReturnsProtocolSubagent verifies that the renamed
// RoleSubagent constant still maps to ProtocolSubagent. The many-to-one mapping
// (subagent, utility → ProtocolSubagent) must not break when the constant is renamed
// from RoleWorker.
func TestVariantForRole_RoleSubagent_ReturnsProtocolSubagent(t *testing.T) {
	variant := domain.VariantForRole(domain.RoleSubagent)
	if variant != domain.ProtocolSubagent {
		t.Errorf("VariantForRole(RoleSubagent) = %q, want ProtocolSubagent (%q)",
			variant, domain.ProtocolSubagent)
	}
}

// TestVariantForRole_RoleOrchestrator_ReturnsProtocolOrchestrator verifies that the
// orchestrator mapping is unchanged after the vocabulary alignment.
func TestVariantForRole_RoleOrchestrator_ReturnsProtocolOrchestrator(t *testing.T) {
	variant := domain.VariantForRole(domain.RoleOrchestrator)
	if variant != domain.ProtocolOrchestrator {
		t.Errorf("VariantForRole(RoleOrchestrator) = %q, want ProtocolOrchestrator (%q)",
			variant, domain.ProtocolOrchestrator)
	}
}

// TestVariantForRole_RoleUtility_ReturnsProtocolSubagent verifies that the utility
// role still maps to ProtocolSubagent (the "everything else" branch). Utility agents
// receive subagent-variant protocol content if they receive any protocol at all.
func TestVariantForRole_RoleUtility_ReturnsProtocolSubagent(t *testing.T) {
	variant := domain.VariantForRole(domain.RoleUtility)
	if variant != domain.ProtocolSubagent {
		t.Errorf("VariantForRole(RoleUtility) = %q, want ProtocolSubagent (%q)",
			variant, domain.ProtocolSubagent)
	}
}

// TestVariantForRole_AllRoles_TableDriven runs all role-to-variant mappings as a
// table to catch any unintended change in the mapping after the rename.
func TestVariantForRole_AllRoles_TableDriven(t *testing.T) {
	cases := []struct {
		role domain.AgentRole
		want domain.ProtocolVariant
	}{
		{domain.RoleSubagent, domain.ProtocolSubagent},
		{domain.RoleOrchestrator, domain.ProtocolOrchestrator},
		{domain.RoleUtility, domain.ProtocolSubagent},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.role), func(t *testing.T) {
			got := domain.VariantForRole(tc.role)
			if got != tc.want {
				t.Errorf("VariantForRole(%q) = %q, want %q", tc.role, got, tc.want)
			}
		})
	}
}
