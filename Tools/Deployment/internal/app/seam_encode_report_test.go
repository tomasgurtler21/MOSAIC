package app

// seam_encode_report_test.go verifies T7.3: the encode report emitted by the translator
// reaches the transform result's report structure.
//
// # What is being proved
//
// The Codex TOML encoder returns a Report containing entries such as EntryDroppedForeignKey
// (a frontmatter key that has no place in the TOML vocabulary) and EntryOverriddenName
// (a frontmatter name that diverges from the agent key). Without the merge loop added to
// transform.Apply in I7.4a, both values would be produced and immediately discarded.
// This test asserts that they are consumed and reach res.Report.Fields with the key name
// and reason set.
//
// The test calls transform.Apply directly (not through buildContent) because the point
// is that the encode report merge loop in transform.go is wired and working. buildContent
// passes the report to the gap collector, which is a separate assertion; here we verify
// the report contains what we expect before it reaches any consumer.
//
// # Translator registration
//
// The Codex translator is registered via the blank import below. A direct blank import of
// internal/agentformat/codextoml is forbidden.

import (
	"testing"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"

	_ "mosaic-deploy/internal/agentformat/all"
)

// ---------------------------------------------------------------------------
// Source fixture for encode report tests
// ---------------------------------------------------------------------------

// encodeReportAgentKey is the agent key whose canonical name differs from the name
// in the source, triggering an EntryOverriddenName entry in the encode report.
const encodeReportAgentKey = "actual-agent-key"

// encodeReportSource is a canonical Markdown document that, when encoded by the Codex
// TOML translator, emits a predictable encode report:
//
//   - EntryOverriddenName: "name" key carries "wrong-name", which diverges from
//     encodeReportAgentKey; the agent key wins and the divergence is reported.
//   - EntryDroppedForeignKey: "user_custom_key" is not in the Codex emitted set and
//     is not a MOSAIC stamp; it is dropped and reported.
//   - EntryAppliedFallback x2: description fallback (absent in source) and sandbox_mode
//     fallback (absent in source) are both invented and reported.
//
// The test asserts on the OverriddenName and DroppedForeignKey entries, which are the
// two that prove the "encode report reaches pipeline report" claim. The fallback entries
// are not asserted directly but must not prevent the other entries from appearing.
const encodeReportSource = `---
name: wrong-name
user_custom_key: some-user-value
---

This is the developer_instructions body. The body must be non-empty for the Codex
encoder to produce output rather than ErrEmptyBody.
`

// ---------------------------------------------------------------------------
// Codex stub module for encode report tests
// ---------------------------------------------------------------------------

// encodeReportCodexModule satisfies domain.HarnessModule with the Codex TOML format ID.
// Used only in this file to avoid a global duplicate type.
type encodeReportCodexModule struct{}

func (encodeReportCodexModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: "encode-report-test", Tier: domain.TierBuiltin, Usable: true}
}
func (encodeReportCodexModule) Descriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID:            "encode-report-test",
		AgentFormatID: "codex-toml",
	}
}
func (encodeReportCodexModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (encodeReportCodexModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (encodeReportCodexModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return ".codex/agents/" + req.Key + ".toml", nil
}
func (encodeReportCodexModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (encodeReportCodexModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "test"}, nil
}
func (encodeReportCodexModule) Close() error { return nil }

// ---------------------------------------------------------------------------
// T7.3: encode report entries appear in the transform result's report
// ---------------------------------------------------------------------------

// TestSeamEncodeReport_DroppedForeignKey_AppearsInResultReport verifies that a
// frontmatter key that the Codex translator classifies as foreign (neither a Codex
// native key nor a MOSAIC stamp) appears in the transform result's Report.Fields as a
// FieldChange with a non-empty Key and a non-empty Before value.
//
// This is the observable proof that the encode report merge loop in transform.Apply
// (I7.4a) consumes the translator's return value rather than discarding it.
func TestSeamEncodeReport_DroppedForeignKey_AppearsInResultReport(t *testing.T) {
	res, err := transform.Apply(transform.Request{
		Source:   []byte(encodeReportSource),
		Kind:     domain.ArtifactAgent,
		Key:      encodeReportAgentKey,
		Module:   encodeReportCodexModule{},
		Scope:    domain.ScopeProject,
		Op:       agentformat.OpCreate,
		Protocol: domain.ProtocolContent{},
	})
	if err != nil {
		t.Fatalf("transform.Apply returned error: %v", err)
	}

	// Search the report fields for the dropped foreign key entry.
	const wantKey = "user_custom_key"
	var found *transform.FieldChange
	for i := range res.Report.Fields {
		if res.Report.Fields[i].Key == wantKey {
			found = &res.Report.Fields[i]
			break
		}
	}

	if found == nil {
		t.Errorf("transform result Report.Fields contains no entry for key %q; "+
			"the encode report merge loop must convert EntryDroppedForeignKey entries "+
			"into FieldChange records (I7.4a)\n"+
			"all fields in report: %v", wantKey, res.Report.Fields)
		return
	}

	// Before must carry the dropped value; After must be empty (the key was dropped).
	if found.Before == "" {
		t.Errorf("FieldChange for %q has empty Before; want the dropped value (\"some-user-value\") "+
			"present so the user can see what was lost", wantKey)
	}
	if found.After != "" {
		t.Errorf("FieldChange for %q has non-empty After (%q); a dropped key has no After value",
			wantKey, found.After)
	}
}

// TestSeamEncodeReport_OverriddenName_AppearsInResultReport verifies that when the
// canonical frontmatter carries a name that diverges from the agent key, the translator
// reports it as EntryOverriddenName and that entry appears in the transform result's
// Report.Fields as a FieldChange with Before set to the divergent name and After set to
// the agent key.
func TestSeamEncodeReport_OverriddenName_AppearsInResultReport(t *testing.T) {
	res, err := transform.Apply(transform.Request{
		Source:   []byte(encodeReportSource),
		Kind:     domain.ArtifactAgent,
		Key:      encodeReportAgentKey,
		Module:   encodeReportCodexModule{},
		Scope:    domain.ScopeProject,
		Op:       agentformat.OpCreate,
		Protocol: domain.ProtocolContent{},
	})
	if err != nil {
		t.Fatalf("transform.Apply returned error: %v", err)
	}

	// Search the report fields for the overridden name entry.
	const wantKey = "name"
	const wantBefore = "wrong-name"
	var found *transform.FieldChange
	for i := range res.Report.Fields {
		if res.Report.Fields[i].Key == wantKey && res.Report.Fields[i].Before == wantBefore {
			found = &res.Report.Fields[i]
			break
		}
	}

	if found == nil {
		t.Errorf("transform result Report.Fields contains no entry for key %q with Before=%q; "+
			"the encode report merge loop must convert EntryOverriddenName entries into "+
			"FieldChange records (I7.4a)\n"+
			"all fields in report: %v", wantKey, wantBefore, res.Report.Fields)
		return
	}

	// After must be the agent key (the value that won).
	if found.After != encodeReportAgentKey {
		t.Errorf("FieldChange for %q has After=%q; want %q (the agent key that won the override)",
			wantKey, found.After, encodeReportAgentKey)
	}
	if found.Reason == "" {
		t.Errorf("FieldChange for %q has empty Reason; a reason is required for overridden entries", wantKey)
	}
}
