package domain_test

// role_test.go covers domain.ParseAgentRole, the AgentRole constants, and VariantForRole.
//
// The frontmatter role vocabulary has four accepted values:
//
//	RoleSubagent     = "subagent"     — worker agents
//	RoleOrchestrator = "orchestrator" — orchestrator agent
//	RoleUtility      = "utility"      — shared utility agents
//	RoleStandalone   = "standalone"   — standalone-deployed agents
//
// ParseAgentRole is the single entry point for converting a frontmatter `role` scalar
// to an AgentRole value. It accepts exactly those four values; every other input
// (including "worker", the empty string, and arbitrary strings) is rejected with
// ok = false so the caller can record an issue rather than silently coerce an agent's role.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// AgentRole constant values
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
// existing value.
func TestRoleOrchestrator_Value_IsOrchestrator(t *testing.T) {
	if string(domain.RoleOrchestrator) != "orchestrator" {
		t.Errorf("RoleOrchestrator = %q, want %q", domain.RoleOrchestrator, "orchestrator")
	}
}

// TestRoleUtility_Value_IsUtility verifies that RoleUtility has the correct string value.
func TestRoleUtility_Value_IsUtility(t *testing.T) {
	if string(domain.RoleUtility) != "utility" {
		t.Errorf("RoleUtility = %q, want %q", domain.RoleUtility, "utility")
	}
}

// TestRoleStandalone_Value_IsStandalone verifies that RoleStandalone has the string value
// "standalone", which is the canonical frontmatter vocabulary for standalone-deployed agents.
func TestRoleStandalone_Value_IsStandalone(t *testing.T) {
	if string(domain.RoleStandalone) != "standalone" {
		t.Errorf("RoleStandalone = %q, want %q", domain.RoleStandalone, "standalone")
	}
}

// TestRoleWorker_AliasEqualsRoleSubagent verifies that RoleWorker is exactly equal to
// RoleSubagent. RoleWorker is a backward-compatible alias declared in artifacts.go; new code
// must use RoleSubagent, but existing call sites that still reference RoleWorker must not
// silently receive a different value if the alias drifts. This test pins the identity.
func TestRoleWorker_AliasEqualsRoleSubagent(t *testing.T) {
	if domain.RoleWorker != domain.RoleSubagent {
		t.Errorf("RoleWorker = %q, want %q (RoleSubagent); "+
			"RoleWorker is a backward-compatible alias and must equal RoleSubagent exactly",
			domain.RoleWorker, domain.RoleSubagent)
	}
}

// ---------------------------------------------------------------------------
// ParseAgentRole — accepted values
// ---------------------------------------------------------------------------

// TestParseAgentRole_Subagent_ReturnsRoleSubagentAndTrue verifies that "subagent" is
// accepted and maps to RoleSubagent.
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

// TestParseAgentRole_Utility_ReturnsRoleUtilityAndTrue verifies that "utility" is accepted
// and maps to RoleUtility. Utility is a first-class frontmatter role value: agents may
// declare it explicitly and must be parsed as RoleUtility, not rejected.
func TestParseAgentRole_Utility_ReturnsRoleUtilityAndTrue(t *testing.T) {
	role, ok := domain.ParseAgentRole("utility")
	if !ok {
		t.Fatal("ParseAgentRole(\"utility\") returned ok = false; want ok = true; " +
			"\"utility\" is a valid frontmatter role value and must be accepted")
	}
	if role != domain.RoleUtility {
		t.Errorf("ParseAgentRole(\"utility\") = %q, want RoleUtility (%q)",
			role, domain.RoleUtility)
	}
}

// TestParseAgentRole_Standalone_ReturnsRoleStandaloneAndTrue verifies that "standalone" is
// accepted and maps to RoleStandalone. Standalone agents are deployed on their own, outside
// any workflow; the parser must recognise the value without requiring a separate code path.
func TestParseAgentRole_Standalone_ReturnsRoleStandaloneAndTrue(t *testing.T) {
	role, ok := domain.ParseAgentRole("standalone")
	if !ok {
		t.Fatal("ParseAgentRole(\"standalone\") returned ok = false; want ok = true; " +
			"\"standalone\" is a valid frontmatter role value and must be accepted")
	}
	if role != domain.RoleStandalone {
		t.Errorf("ParseAgentRole(\"standalone\") = %q, want RoleStandalone (%q)",
			role, domain.RoleStandalone)
	}
}

// ---------------------------------------------------------------------------
// ParseAgentRole — rejected values
// ---------------------------------------------------------------------------

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

// TestParseAgentRole_Mixed_TableDriven runs accepted and rejected cases as a table to
// catch unintended expansion or contraction of the vocabulary in future changes. A row
// with wantOk = true has a non-empty wantVal; a rejected row has wantVal == "".
func TestParseAgentRole_Mixed_TableDriven(t *testing.T) {
	cases := []struct {
		input   string
		wantOk  bool
		wantVal domain.AgentRole
	}{
		// Accepted: the four first-class frontmatter role values.
		{"subagent", true, domain.RoleSubagent},
		{"orchestrator", true, domain.RoleOrchestrator},
		{"utility", true, domain.RoleUtility},
		{"standalone", true, domain.RoleStandalone},

		// Rejected: legacy, empty, whitespace-padded, and case-variant forms.
		{"worker", false, ""},
		{"", false, ""},
		{"SUBAGENT", false, ""},
		{"Subagent", false, ""},
		{"orchestrator ", false, ""}, // trailing space
		{" subagent", false, ""},    // leading space
		{"analyst", false, ""},
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
// VariantForRole — role-to-protocol-variant mapping
// ---------------------------------------------------------------------------

// TestVariantForRole_RoleSubagent_ReturnsProtocolSubagent verifies that the renamed
// RoleSubagent constant still maps to ProtocolSubagent.
func TestVariantForRole_RoleSubagent_ReturnsProtocolSubagent(t *testing.T) {
	variant := domain.VariantForRole(domain.RoleSubagent)
	if variant != domain.ProtocolSubagent {
		t.Errorf("VariantForRole(RoleSubagent) = %q, want ProtocolSubagent (%q)",
			variant, domain.ProtocolSubagent)
	}
}

// TestVariantForRole_RoleOrchestrator_ReturnsProtocolOrchestrator verifies that the
// orchestrator mapping is unchanged.
func TestVariantForRole_RoleOrchestrator_ReturnsProtocolOrchestrator(t *testing.T) {
	variant := domain.VariantForRole(domain.RoleOrchestrator)
	if variant != domain.ProtocolOrchestrator {
		t.Errorf("VariantForRole(RoleOrchestrator) = %q, want ProtocolOrchestrator (%q)",
			variant, domain.ProtocolOrchestrator)
	}
}

// TestVariantForRole_RoleUtility_ReturnsProtocolSubagent verifies that the utility
// role maps to ProtocolSubagent (the "everything else" branch). Utility agents receive
// the subagent-variant protocol content when they receive any protocol at all.
func TestVariantForRole_RoleUtility_ReturnsProtocolSubagent(t *testing.T) {
	variant := domain.VariantForRole(domain.RoleUtility)
	if variant != domain.ProtocolSubagent {
		t.Errorf("VariantForRole(RoleUtility) = %q, want ProtocolSubagent (%q)",
			variant, domain.ProtocolSubagent)
	}
}

// TestVariantForRole_RoleStandalone_ReturnsProtocolSubagent verifies that the standalone
// role maps to ProtocolSubagent. Standalone agents are deployed without a protocol marker,
// but the variant mapping itself routes them to the subagent variant — the absence of
// protocol injection is the responsibility of the deployment path, not the mapping.
func TestVariantForRole_RoleStandalone_ReturnsProtocolSubagent(t *testing.T) {
	variant := domain.VariantForRole(domain.RoleStandalone)
	if variant != domain.ProtocolSubagent {
		t.Errorf("VariantForRole(RoleStandalone) = %q, want ProtocolSubagent (%q)",
			variant, domain.ProtocolSubagent)
	}
}

// TestVariantForRole_AllRoles_TableDriven runs all role-to-variant mappings as a table
// to catch any unintended change in the mapping when the vocabulary expands.
func TestVariantForRole_AllRoles_TableDriven(t *testing.T) {
	cases := []struct {
		role domain.AgentRole
		want domain.ProtocolVariant
	}{
		{domain.RoleSubagent, domain.ProtocolSubagent},
		{domain.RoleOrchestrator, domain.ProtocolOrchestrator},
		{domain.RoleUtility, domain.ProtocolSubagent},
		{domain.RoleStandalone, domain.ProtocolSubagent},
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
