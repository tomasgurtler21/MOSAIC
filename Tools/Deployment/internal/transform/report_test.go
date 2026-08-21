package transform_test

// report_test.go tests the transformation report returned by Apply (T8.6). The report is
// the input to per-item update logging and gap collection (AC8.5, AC15.2). Every field
// that changed must appear with a non-empty Reason. The report is also the input to the
// TODO checklist, so gaps reported here must carry enough context to render a useful item.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// reportSource is a source agent that triggers all observable field changes:
//   - Drops:   name, recommended_tier, tier_rationale
//   - Adds:    mode (descriptor Add), transform_version, injections_version
//   - Modifies: model (set from selection), tools (mapped from generic list)
//   - Carries: id, version, description, required_skills unchanged
const reportSource = `---
id: 77
version: 5.0.0
name: report-agent
description: Agent for report tests
model: {model-identifier}
tools: [file_read, file_write, file_edit]
recommended_tier: MEDIUM
tier_rationale: moderate task
required_skills: []
---

<Identity type="core">
Body for report tests.
</Identity>
`

// TestReport_ChangedFieldsNamesEveryModifiedField asserts that Report.Fields contains at
// least one entry for every field that changed in the transformation. The report must be
// complete (AC8.5): a consumer must be able to reproduce the full change log from it.
func TestReport_ChangedFieldsNamesEveryModifiedField(t *testing.T) {
	req := transform.Request{
		Source: []byte(reportSource),
		Kind:   domain.ArtifactAgent,
		Key:    "report-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err) // RED: fails here
	}

	// Build an index of reported changes by key.
	reportedKeys := make(map[string]transform.FieldChange, len(result.Report.Fields))
	for _, fc := range result.Report.Fields {
		reportedKeys[fc.Key] = fc
	}

	// Fields that the fixture descriptor drops must appear in the report with After == "".
	droppedFields := []string{"name", "recommended_tier", "tier_rationale"}
	for _, key := range droppedFields {
		fc, ok := reportedKeys[key]
		if !ok {
			t.Errorf("dropped field %q not reported in Report.Fields", key)
			continue
		}
		if fc.After != "" {
			t.Errorf("dropped field %q: Report.Fields entry has non-empty After %q; want empty", key, fc.After)
		}
		if fc.Before == "" {
			t.Errorf("dropped field %q: Report.Fields entry has empty Before; want the source value", key)
		}
	}

	// Fields that the fixture descriptor adds or renames must appear in the report with Before == "".
	// Version stamps use mosaic_-prefixed deployed names; the id and version renames produce
	// mosaic_id and mosaic_version. mosaic_harness_version replaces mosaic_transform_version.
	// mosaic_injections_version is written then stripped (not an "added" field in the final output).
	addedFields := []string{"mode", "mosaic_harness_version", "mosaic_id", "mosaic_version"}
	for _, key := range addedFields {
		fc, ok := reportedKeys[key]
		if !ok {
			t.Errorf("added field %q not reported in Report.Fields", key)
			continue
		}
		if fc.Before != "" {
			t.Errorf("added field %q: Report.Fields entry has non-empty Before %q; want empty", key, fc.Before)
		}
		if fc.After == "" {
			t.Errorf("added field %q: Report.Fields entry has empty After; want the new value", key)
		}
	}

	// The model field is overwritten; Before and After must both be non-empty.
	if fc, ok := reportedKeys["model"]; !ok {
		t.Error("modified field \"model\" not reported in Report.Fields")
	} else {
		if fc.Before == "" {
			t.Error("model field report: Before must be non-empty (the source placeholder)")
		}
		if fc.After == "" {
			t.Error("model field report: After must be non-empty (the selected model ID)")
		}
		if fc.After != "claude/claude-sonnet" {
			t.Errorf("model field report After: want %q, got %q", "claude/claude-sonnet", fc.After)
		}
	}
}

// TestReport_EachFieldChangeHasNonEmptyReason asserts that every FieldChange in the report
// carries a non-empty Reason. A blank reason is an incomplete audit trail (AC8.5).
func TestReport_EachFieldChangeHasNonEmptyReason(t *testing.T) {
	req := transform.Request{
		Source: []byte(reportSource),
		Kind:   domain.ArtifactAgent,
		Key:    "report-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, fc := range result.Report.Fields {
		if fc.Reason == "" {
			t.Errorf("FieldChange for key %q has an empty Reason; every change must document its cause", fc.Key)
		}
	}
}

// TestReport_ToolResolutionsOnePerGenericTool asserts that Report.Tools contains exactly
// one ToolResolution per generic tool declared in the source agent, in the same order.
// This mirrors the HarnessModule.Tools contract (AC9.3, AC9.4) at the report level.
func TestReport_ToolResolutionsOnePerGenericTool(t *testing.T) {
	// reportSource declares: tools: [file_read, file_write, file_edit]
	wantGeneric := []string{"file_read", "file_write", "file_edit"}

	req := transform.Request{
		Source: []byte(reportSource),
		Kind:   domain.ArtifactAgent,
		Key:    "report-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(result.Report.Tools) != len(wantGeneric) {
		t.Fatalf("Report.Tools length: want %d (one per generic tool), got %d: %v",
			len(wantGeneric), len(result.Report.Tools),
			genericToolNames(result.Report.Tools))
	}
	for i, want := range wantGeneric {
		got := result.Report.Tools[i].Generic
		if got != want {
			t.Errorf("Report.Tools[%d].Generic: want %q, got %q", i, want, got)
		}
	}
}

// TestReport_ToolResolutionsHaveOutcomes asserts that every ToolResolution in the report
// has a non-empty Outcome. An empty outcome is invalid and callers (logging, gap) cannot
// act on it.
func TestReport_ToolResolutionsHaveOutcomes(t *testing.T) {
	req := transform.Request{
		Source: []byte(reportSource),
		Kind:   domain.ArtifactAgent,
		Key:    "report-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, tr := range result.Report.Tools {
		if tr.Outcome == "" {
			t.Errorf("ToolResolution for generic tool %q has empty Outcome", tr.Generic)
		}
	}
}

// TestReport_OutputBytesEqualsOutputLength asserts that Report.OutputBytes equals the byte
// length of Result.Output. These must agree: the report value is consumed by log formatting
// and must not lie about the deployed file size.
func TestReport_OutputBytesEqualsOutputLength(t *testing.T) {
	req := transform.Request{
		Source: []byte(reportSource),
		Kind:   domain.ArtifactAgent,
		Key:    "report-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if result.Report.OutputBytes != len(result.Output) {
		t.Errorf("Report.OutputBytes = %d, len(Output) = %d; they must be equal",
			result.Report.OutputBytes, len(result.Output))
	}
}

// TestReport_InjectionOutcomesPresent asserts that the report lists at least one
// RegionOutcome for each region present in the source document. The outcome
// entry allows logging to explain what happened to each region.
func TestReport_InjectionOutcomesPresent(t *testing.T) {
	const sourceWithInjections = `---
id: 10
version: 1.0.0
name: injection-report-agent
description: Agent with injection regions for report testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: simple
required_skills: []
---

<Identity type="core">
Body.

<HarnessConstraints type="managed">
</HarnessConstraints>

</Identity>
`

	req := transform.Request{
		Source: []byte(sourceWithInjections),
		Kind:   domain.ArtifactAgent,
		Key:    "injection-report-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The source has one managed region (HarnessConstraints). The report must account for it.
	if len(result.Report.Regions) == 0 {
		t.Error("Report.Regions is empty; expected at least one entry for the HarnessConstraints region")
	}
	for _, r := range result.Report.Regions {
		if r.Name == "" {
			t.Error("RegionOutcome.Name must not be empty")
		}
		if r.Action == "" {
			t.Error("RegionOutcome.Action must not be empty")
		}
	}
}

// ---------------------------------------------------------------------------
// T4.5: Transform report — protocol region as a distinct tool-managed outcome
// ---------------------------------------------------------------------------

// TestProtocol_Report_RegionOutcomePresentInReport verifies that the transform report
// includes a RegionOutcome entry for the CommunicationProtocol region when the source
// declares one. Report.Regions must account for every managed region in the source.
func TestProtocol_Report_RegionOutcomePresentInReport(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcome(result.Report.Regions, "CommunicationProtocol")
	if outcome == nil {
		t.Fatalf("Report.Regions does not contain an entry for CommunicationProtocol; got: %v",
			regionNames(result.Report.Regions))
	}
}

// TestProtocol_Report_OutcomeIsToolManaged verifies that the CommunicationProtocol
// RegionOutcome has Marker == NodeDeployed and ToolManaged() == true, distinguishing it
// from user-owned injection regions in the same report.
func TestProtocol_Report_OutcomeIsToolManaged(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcome(result.Report.Regions, "CommunicationProtocol")
	if outcome == nil {
		t.Fatalf("Report.Regions does not contain CommunicationProtocol; got: %v", regionNames(result.Report.Regions))
	}

	if !outcome.ToolManaged() {
		t.Errorf("CommunicationProtocol outcome: ToolManaged() = false; want true (Marker = %q)", outcome.Marker)
	}
}

// TestProtocol_Report_OutcomeActionIsProtocolFilled verifies that the RegionOutcome for
// the CommunicationProtocol region carries Action == RegionProtocolFilled, distinguishing
// the protocol fill from harness, workflow, and infrastructure fills.
func TestProtocol_Report_OutcomeActionIsProtocolFilled(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleOrchestrator,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcome(result.Report.Regions, "CommunicationProtocol")
	if outcome == nil {
		t.Fatalf("Report.Regions does not contain CommunicationProtocol; got: %v", regionNames(result.Report.Regions))
	}

	if outcome.Action != transform.RegionProtocolFilled {
		t.Errorf("CommunicationProtocol outcome Action: want %q, got %q",
			transform.RegionProtocolFilled, outcome.Action)
	}
}

// TestProtocol_Report_OutcomeBytesPositive verifies that the RegionOutcome for the
// CommunicationProtocol region carries a positive Bytes count matching the length of the
// written content. A zero byte count would indicate that an empty region was emitted.
func TestProtocol_Report_OutcomeBytesPositive(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcome(result.Report.Regions, "CommunicationProtocol")
	if outcome == nil {
		t.Fatalf("Report.Regions does not contain CommunicationProtocol; got: %v", regionNames(result.Report.Regions))
	}

	if outcome.Bytes <= 0 {
		t.Errorf("CommunicationProtocol outcome Bytes = %d; want > 0 (region must have content)", outcome.Bytes)
	}
}

// TestProtocol_Report_BytesMatchActualRegionLength verifies that RegionOutcome.Bytes
// equals the byte length of the content placed in the <CommunicationProtocol type="managed">
// region. The report value and the serialised document must agree on region size.
func TestProtocol_Report_BytesMatchActualRegionLength(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcome(result.Report.Regions, "CommunicationProtocol")
	if outcome == nil {
		t.Fatalf("Report.Regions does not contain CommunicationProtocol")
	}

	regionContent := extractProtocolRegionContent(t, result.Output)
	if outcome.Bytes != len(regionContent) {
		t.Errorf("RegionOutcome.Bytes = %d, actual region content length = %d; they must be equal",
			outcome.Bytes, len(regionContent))
	}
}

// ---------------------------------------------------------------------------
// report helpers
// ---------------------------------------------------------------------------

// findRegionOutcome returns the first RegionOutcome with the given name, or nil.
func findRegionOutcome(outcomes []transform.RegionOutcome, name string) *transform.RegionOutcome {
	for i := range outcomes {
		if outcomes[i].Name == name {
			return &outcomes[i]
		}
	}
	return nil
}

// regionNames returns the Name field of each RegionOutcome for readable test diagnostics.
func regionNames(outcomes []transform.RegionOutcome) []string {
	names := make([]string, len(outcomes))
	for i, o := range outcomes {
		names[i] = o.Name
	}
	return names
}

// genericToolNames returns the Generic field of each ToolResolution for readable test output.
func genericToolNames(resolutions []domain.ToolResolution) []string {
	names := make([]string, len(resolutions))
	for i, r := range resolutions {
		names[i] = r.Generic
	}
	return names
}
