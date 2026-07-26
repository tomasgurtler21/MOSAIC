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

[[SECTION:Identity]]
Body for report tests.
[[/SECTION:Identity]]
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

	// Fields that the fixture descriptor adds must appear in the report with Before == "".
	addedFields := []string{"mode", "transform_version", "injections_version"}
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
// InjectionOutcome for each injection region present in the source document. The outcome
// entry allows logging to explain what happened to each injection point.
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

[[SECTION:Identity]]
Body.

[[INJECTION:HarnessConstraints]]
[[/INJECTION:HarnessConstraints]]

[[/SECTION:Identity]]
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

	// The source has one injection region (HarnessConstraints). The report must account for it.
	if len(result.Report.Injections) == 0 {
		t.Error("Report.Injections is empty; expected at least one entry for the HarnessConstraints injection region")
	}
	for _, inj := range result.Report.Injections {
		if inj.Name == "" {
			t.Error("InjectionOutcome.Name must not be empty")
		}
		if inj.Action == "" {
			t.Error("InjectionOutcome.Action must not be empty")
		}
	}
}

// genericToolNames returns the Generic field of each ToolResolution for readable test output.
func genericToolNames(resolutions []domain.ToolResolution) []string {
	names := make([]string, len(resolutions))
	for i, r := range resolutions {
		names[i] = r.Generic
	}
	return names
}
