package transform_test

// infrastructure_assembly_test.go covers infrastructure declaration block assembly
// (AssembleInfrastructureBlocks) and the InfrastructureAgents injection pipeline:
//
//   - Zero agents: the InfrastructureAgents injection is left empty; AssembleInfrastructureBlocks
//     returns nil bytes and nil keys.
//   - Single trigger: the <InfrastructureAgent type="core" name="{key}" version="..."> block
//     is produced with the correct section markers, version attribute, table header, and one
//     data row.
//   - Multi-trigger: one table row per trigger entry, Class and On Failure repeated on each.
//   - Null/empty TriggerParam: rendered as "-" in the Param column.
//   - Multiple agents: blocks are composed in the order of the input slice.
//   - Pipeline: Request.InfrastructureAgents is assembled into <InfrastructureAgents type="managed">
//     by transform.Apply, analogous to Request.Workflows and <AvailableWorkflows type="managed">.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// orchestratorWithInfrastructureAgents is an orchestrator-like source with both a
// <InfrastructureAgents type="managed"> and a <AvailableWorkflows type="managed"> inside the
// Identity section. This mirrors the canonical shape the real orchestrator will have once
// the InfrastructureAgents injection is wired in.
const orchestratorWithInfrastructureAgents = `---
version: 6.0.0
name: orchestrator
description: Central coordinator that manages multi-agent workflow execution
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: multi-phase coordination
required_skills: []
---

<Identity type="core">
# Orchestrator Agent

You are the Orchestrator.

<InfrastructureAgents type="managed">
</InfrastructureAgents>

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`

// ---------------------------------------------------------------------------
// Unit tests for AssembleInfrastructureBlocks (pure-function, no pipeline)
// ---------------------------------------------------------------------------

// TestAssembleInfrastructureBlocks_EmptyInput_ReturnsNilBytes verifies that when an empty
// (or nil) agent slice is supplied, AssembleInfrastructureBlocks returns nil bytes and nil
// keys. An empty region is valid per the design and must not produce an error.
func TestAssembleInfrastructureBlocks_EmptyInput_ReturnsNilBytes(t *testing.T) {
	assembled, keys := transform.AssembleInfrastructureBlocks(nil)
	if assembled != nil {
		t.Errorf("AssembleInfrastructureBlocks(nil): assembled bytes = %q, want nil", assembled)
	}
	if keys != nil {
		t.Errorf("AssembleInfrastructureBlocks(nil): keys = %v, want nil", keys)
	}
}

// TestAssembleInfrastructureBlocks_EmptySlice_ReturnsNilBytes verifies that an explicitly
// empty (non-nil) slice also returns nil bytes and nil keys, consistent with nil input.
func TestAssembleInfrastructureBlocks_EmptySlice_ReturnsNilBytes(t *testing.T) {
	assembled, keys := transform.AssembleInfrastructureBlocks([]transform.InfrastructureBlock{})
	if assembled != nil {
		t.Errorf("AssembleInfrastructureBlocks([]): assembled bytes = %q, want nil", assembled)
	}
	if keys != nil {
		t.Errorf("AssembleInfrastructureBlocks([]): keys = %v, want nil", keys)
	}
}

// TestAssembleInfrastructureBlocks_SingleTrigger_SectionMarkers verifies that the output
// contains the correct <InfrastructureAgent type="core" name="{key}"> open tag and
// </InfrastructureAgent> close tag.
func TestAssembleInfrastructureBlocks_SingleTrigger_SectionMarkers(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "orchestration-review",
			Version:     "1.0.0",
			Class:       "review",
			Description: "Advisory checks on run bookkeeping.",
			OnFailure:   "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	if !bytes.Contains(assembled, []byte(`<InfrastructureAgent type="core" name="orchestration-review"`)) {
		t.Errorf("output missing section open tag; got:\n%s", assembled)
	}
	if !bytes.Contains(assembled, []byte("</InfrastructureAgent>")) {
		t.Errorf("output missing section close tag; got:\n%s", assembled)
	}
}

// TestAssembleInfrastructureBlocks_SingleTrigger_VersionAttribute verifies that the
// section opening tag carries the version attribute matching the InfrastructureBlock's
// Version field. The version is a tag attribute, not an inline comment.
func TestAssembleInfrastructureBlocks_SingleTrigger_VersionAttribute(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "orchestration-review",
			Version:     "1.0.0",
			Class:       "review",
			Description: "Advisory.",
			OnFailure:   "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	if !bytes.Contains(assembled, []byte(`version="1.0.0"`)) {
		t.Errorf("output missing version attribute on section open tag; got:\n%s", assembled)
	}
	// The version must NOT appear as a legacy infra-version comment.
	if bytes.Contains(assembled, []byte("<!-- infra-version:")) {
		t.Errorf("output must not contain legacy infra-version comment; version belongs on the tag attribute; got:\n%s", assembled)
	}
}

// TestAssembleInfrastructureBlocks_SingleTrigger_TableHeader verifies that the markdown
// table header row and separator row appear inside the section block, with the correct
// column names: Class, Trigger, Param, On Failure, Description.
func TestAssembleInfrastructureBlocks_SingleTrigger_TableHeader(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "orchestration-review",
			Version:     "1.0.0",
			Class:       "review",
			Description: "Advisory.",
			OnFailure:   "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	if !bytes.Contains(assembled, []byte("| Class | Trigger | Param | On Failure | Description |")) {
		t.Errorf("output missing table header row; got:\n%s", assembled)
	}
}

// TestAssembleInfrastructureBlocks_SingleTrigger_DataRow verifies that the single trigger
// produces one data row with Class, Trigger, Param, On Failure, and Description columns
// populated from the InfrastructureBlock.
func TestAssembleInfrastructureBlocks_SingleTrigger_DataRow(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "orchestration-review",
			Version:     "1.0.0",
			Class:       "review",
			Description: "Advisory checks on run bookkeeping.",
			OnFailure:   "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	// Each trigger row must contain all five column values.
	for _, want := range []string{"review", "INVOCATION_INTERVAL", "30", "continue", "Advisory checks on run bookkeeping."} {
		if !bytes.Contains(assembled, []byte(want)) {
			t.Errorf("output missing expected column value %q; got:\n%s", want, assembled)
		}
	}
}

// TestAssembleInfrastructureBlocks_EmptyTriggerParam_RenderedAsDash verifies that when
// TriggerParam is empty (corresponding to a null frontmatter value), the Param column
// renders as "-" rather than an empty cell. This is the canonical representation for
// parameterless triggers (STAGE_END, PHASE_END, MANUAL).
func TestAssembleInfrastructureBlocks_EmptyTriggerParam_RenderedAsDash(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "commit-manager-git",
			Version:     "1.0.0",
			Class:       "commit",
			Description: "Commits completed stage work.",
			OnFailure:   "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "STAGE_END", TriggerParam: ""}, // null trigger_param → empty string → rendered as "-"
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	// The Param column for a parameterless trigger must be "-", not an empty cell.
	// We look for "| STAGE_END | - |" as a substring of the row.
	if !bytes.Contains(assembled, []byte("| STAGE_END | - |")) {
		t.Errorf("empty TriggerParam not rendered as '-'; got:\n%s", assembled)
	}
}

// TestAssembleInfrastructureBlocks_MultiTrigger_TwoRows verifies that a multi-trigger
// agent (like checkpoint-manager-git with STAGE_END and INVOCATION_INTERVAL) produces
// two data rows, with Class and On Failure repeated on each row.
func TestAssembleInfrastructureBlocks_MultiTrigger_TwoRows(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "checkpoint-manager-git",
			Version:     "1.0.0",
			Class:       "checkpoint",
			Description: "Commits a restorable checkpoint of the working tree.",
			OnFailure:   "halt",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "STAGE_END", TriggerParam: ""},
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "10"},
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	// Both trigger names must appear in the output.
	if !bytes.Contains(assembled, []byte("STAGE_END")) {
		t.Errorf("output missing STAGE_END trigger row; got:\n%s", assembled)
	}
	if !bytes.Contains(assembled, []byte("INVOCATION_INTERVAL")) {
		t.Errorf("output missing INVOCATION_INTERVAL trigger row; got:\n%s", assembled)
	}

	// Class and On Failure must appear at least twice (once per row).
	if bytes.Count(assembled, []byte("checkpoint")) < 2 {
		t.Errorf("Class column not repeated on both rows; want at least 2 occurrences of 'checkpoint'; got:\n%s", assembled)
	}
	if bytes.Count(assembled, []byte("halt")) < 2 {
		t.Errorf("On Failure column not repeated on both rows; want at least 2 occurrences of 'halt'; got:\n%s", assembled)
	}
}

// TestAssembleInfrastructureBlocks_MultiTrigger_ParamPopulated verifies that when
// TriggerParam is non-empty (e.g. "10" for INVOCATION_INTERVAL), the value appears in
// the Param column rather than the dash sentinel.
func TestAssembleInfrastructureBlocks_MultiTrigger_ParamPopulated(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "checkpoint-manager-git",
			Version:     "1.0.0",
			Class:       "checkpoint",
			Description: "Commits a restorable checkpoint of the working tree.",
			OnFailure:   "halt",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "STAGE_END", TriggerParam: ""},
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "10"},
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	if !bytes.Contains(assembled, []byte("| INVOCATION_INTERVAL | 10 |")) {
		t.Errorf("TriggerParam '10' not present in Param column; got:\n%s", assembled)
	}
}

// TestAssembleInfrastructureBlocks_MultipleAgents_BothBlocksPresent verifies that when
// two agents are supplied, both produce their <InfrastructureAgent type="core" name="{key}">
// blocks in the output.
func TestAssembleInfrastructureBlocks_MultipleAgents_BothBlocksPresent(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "checkpoint-manager-git",
			Version:     "1.0.0",
			Class:       "checkpoint",
			Description: "Commits a restorable checkpoint.",
			OnFailure:   "halt",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "STAGE_END", TriggerParam: ""},
			},
		},
		{
			Key:         "orchestration-review",
			Version:     "1.0.0",
			Class:       "review",
			Description: "Advisory checks.",
			OnFailure:   "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	if !bytes.Contains(assembled, []byte(`name="checkpoint-manager-git"`)) {
		t.Errorf("output missing checkpoint-manager-git block; got:\n%s", assembled)
	}
	if !bytes.Contains(assembled, []byte(`name="orchestration-review"`)) {
		t.Errorf("output missing orchestration-review block; got:\n%s", assembled)
	}
}

// TestAssembleInfrastructureBlocks_MultipleAgents_InInputOrder verifies that when multiple
// agents are supplied, their blocks appear in the output in exactly the order they appear in
// the input slice. Ordering is the caller's responsibility; AssembleInfrastructureBlocks
// must preserve input order.
func TestAssembleInfrastructureBlocks_MultipleAgents_InInputOrder(t *testing.T) {
	// Supply orchestration-review before checkpoint-manager-git to verify order is not sorted.
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "orchestration-review",
			Version:     "1.0.0",
			Class:       "review",
			Description: "Advisory.",
			OnFailure:   "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
			},
		},
		{
			Key:         "checkpoint-manager-git",
			Version:     "1.0.0",
			Class:       "checkpoint",
			Description: "Commits a restorable checkpoint.",
			OnFailure:   "halt",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "STAGE_END", TriggerParam: ""},
			},
		},
	}

	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)

	reviewPos := bytes.Index(assembled, []byte(`name="orchestration-review"`))
	checkpointPos := bytes.Index(assembled, []byte(`name="checkpoint-manager-git"`))

	if reviewPos < 0 || checkpointPos < 0 {
		t.Fatalf("one or both blocks absent from output; orchestration-review at %d, checkpoint-manager-git at %d", reviewPos, checkpointPos)
	}
	if reviewPos >= checkpointPos {
		t.Errorf("blocks not in input order: orchestration-review at %d, checkpoint-manager-git at %d (expected orchestration-review first)",
			reviewPos, checkpointPos)
	}
}

// TestAssembleInfrastructureBlocks_ReturnedKeys_MatchInputOrder verifies that the returned
// keys slice contains the keys of all input blocks in input order, analogous to how
// assembleWorkflowBlocks returns workflow IDs.
func TestAssembleInfrastructureBlocks_ReturnedKeys_MatchInputOrder(t *testing.T) {
	blocks := []transform.InfrastructureBlock{
		{
			Key:         "orchestration-review",
			Version:     "1.0.0",
			Class:       "review",
			Description: "Advisory.",
			OnFailure:   "continue",
			Triggers:    []domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"}},
		},
		{
			Key:         "checkpoint-manager-git",
			Version:     "1.0.0",
			Class:       "checkpoint",
			Description: "Commits a checkpoint.",
			OnFailure:   "halt",
			Triggers:    []domain.InfrastructureTrigger{{Trigger: "STAGE_END", TriggerParam: ""}},
		},
	}

	_, keys := transform.AssembleInfrastructureBlocks(blocks)

	if len(keys) != 2 {
		t.Fatalf("returned %d keys, want 2; keys: %v", len(keys), keys)
	}
	if keys[0] != "orchestration-review" {
		t.Errorf("keys[0] = %q, want %q", keys[0], "orchestration-review")
	}
	if keys[1] != "checkpoint-manager-git" {
		t.Errorf("keys[1] = %q, want %q", keys[1], "checkpoint-manager-git")
	}
}

// ---------------------------------------------------------------------------
// Pipeline tests: InfrastructureAgents injection through transform.Apply
// ---------------------------------------------------------------------------

// TestInfrastructureAgents_ZeroAgents_InjectionEmpty verifies that when
// Request.InfrastructureAgents is nil (or empty), transform.Apply leaves the
// <InfrastructureAgents type="project"> region empty in the output.
func TestInfrastructureAgents_ZeroAgents_InjectionEmpty(t *testing.T) {
	req := transform.Request{
		Source:               []byte(orchestratorWithInfrastructureAgents),
		Kind:                 domain.ArtifactAgent,
		Key:                  "orchestrator",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		InfrastructureAgents: nil, // no infrastructure agents selected
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Deployed("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents injection absent from output")
	}
	if !node.IsEmpty() {
		t.Errorf("InfrastructureAgents must be empty with zero agents; got content: %q", node.Content())
	}

	// Report.InfrastructureAgents must be empty.
	if len(result.Report.InfrastructureAgents) != 0 {
		t.Errorf("Report.InfrastructureAgents must be empty with zero agents; got: %v", result.Report.InfrastructureAgents)
	}
}

// TestInfrastructureAgents_SingleAgent_BlockPresentInInjection verifies that when one
// infrastructure agent is supplied, its <InfrastructureAgent type="core" name="{key}"> block
// appears inside the <InfrastructureAgents type="project"> region of the output.
func TestInfrastructureAgents_SingleAgent_BlockPresentInInjection(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorWithInfrastructureAgents),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		InfrastructureAgents: []transform.InfrastructureBlock{
			{
				Key:         "orchestration-review",
				Version:     "1.0.0",
				Class:       "review",
				Description: "Advisory checks on run bookkeeping.",
				OnFailure:   "continue",
				Triggers: []domain.InfrastructureTrigger{
					{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
				},
			},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Deployed("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents injection absent from output")
	}

	content := node.Content()
	for _, want := range []string{
		`<InfrastructureAgent type="core" name="orchestration-review"`,
		`version="1.0.0"`,
		"</InfrastructureAgent>",
	} {
		if !bytes.Contains(content, []byte(want)) {
			t.Errorf("InfrastructureAgents content missing expected text %q\ncontent: %q", want, content)
		}
	}

	// Report.InfrastructureAgents must list the assembled key.
	if len(result.Report.InfrastructureAgents) != 1 || result.Report.InfrastructureAgents[0] != "orchestration-review" {
		t.Errorf("Report.InfrastructureAgents: want [orchestration-review], got %v", result.Report.InfrastructureAgents)
	}
}

// TestInfrastructureAgents_InjectionClass_IsInfrastructure verifies that the
// RegionOutcome for the InfrastructureAgents region carries
// Class == domain.InjectionInfrastructure, regardless of whether any agents were selected.
func TestInfrastructureAgents_InjectionClass_IsInfrastructure(t *testing.T) {
	for _, withAgents := range []bool{false, true} {
		withAgents := withAgents
		t.Run(map[bool]string{false: "zero-agents", true: "one-agent"}[withAgents], func(t *testing.T) {
			var agents []transform.InfrastructureBlock
			if withAgents {
				agents = []transform.InfrastructureBlock{
					{
						Key:         "orchestration-review",
						Version:     "1.0.0",
						Class:       "review",
						Description: "Advisory.",
						OnFailure:   "continue",
						Triggers:    []domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"}},
					},
				}
			}

			req := transform.Request{
				Source:               []byte(orchestratorWithInfrastructureAgents),
				Kind:                 domain.ArtifactAgent,
				Key:                  "orchestrator",
				Module:               newFixtureModule(t),
				Model:                fixtureModel(),
				Scope:                domain.ScopeProject,
				InfrastructureAgents: agents,
			}

			result, err := transform.Apply(req)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			var outcome *transform.RegionOutcome
			for i := range result.Report.Regions {
				if result.Report.Regions[i].Name == "InfrastructureAgents" {
					outcome = &result.Report.Regions[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("RegionOutcome for InfrastructureAgents absent from report")
			}
			if outcome.Class != domain.InjectionInfrastructure {
				t.Errorf("InfrastructureAgents Class: want %q, got %q",
					domain.InjectionInfrastructure, outcome.Class)
			}
		})
	}
}

// TestInfrastructureAgents_AssembledAction_WhenAgentsSupplied verifies that when
// infrastructure agents are provided, the RegionOutcome carries Action ==
// RegionAssembledInfra, analogous to RegionAssembled for AvailableWorkflows.
func TestInfrastructureAgents_AssembledAction_WhenAgentsSupplied(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorWithInfrastructureAgents),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		InfrastructureAgents: []transform.InfrastructureBlock{
			{
				Key:         "orchestration-review",
				Version:     "1.0.0",
				Class:       "review",
				Description: "Advisory.",
				OnFailure:   "continue",
				Triggers:    []domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"}},
			},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "InfrastructureAgents" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for InfrastructureAgents absent from report")
	}
	if outcome.Action != transform.RegionAssembledInfra {
		t.Errorf("InfrastructureAgents action: want %q, got %q", transform.RegionAssembledInfra, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// Preservation tests: InfrastructureAgents region preserved on Update
// ---------------------------------------------------------------------------

// orchestratorWithInfrastructureAgentsDeployed is an already-deployed orchestrator
// whose InfrastructureAgents region contains a pre-assembled infrastructure-agent block.
// It is used as the req.Deployed input in Update-scenario tests.
const orchestratorWithInfrastructureAgentsDeployed = `---
version: 6.0.0
name: orchestrator
description: Central coordinator that manages multi-agent workflow execution
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: multi-phase coordination
required_skills: []
---

<Identity type="core">
# Orchestrator Agent

You are the Orchestrator.

<InfrastructureAgents type="managed">
<InfrastructureAgent type="core" name="orchestration-review" version="1.0.0">

| Class | Trigger | Param | On Failure | Description |
| --- | --- | --- | --- | --- |
| review | INVOCATION_INTERVAL | 30 | continue | Advisory checks on run bookkeeping. |
</InfrastructureAgent>
</InfrastructureAgents>

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`

// orchestratorWithoutInfrastructureAgentsDeployed is an already-deployed orchestrator whose
// InfrastructureAgents region is entirely absent — simulating a file deployed before the
// InfrastructureAgents region was introduced. Used to verify the graceful no-op path: when
// req.Deployed is non-nil but contains no InfrastructureAgents region, transform.Apply must
// succeed without error and clear (or empty) the region in the output.
const orchestratorWithoutInfrastructureAgentsDeployed = `---
version: 6.0.0
name: orchestrator
description: Central coordinator that manages multi-agent workflow execution
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: multi-phase coordination
required_skills: []
---

<Identity type="core">
# Orchestrator Agent

You are the Orchestrator.

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`

// orchestratorWithEmptyInfrastructureAgentsDeployed is an already-deployed orchestrator
// whose InfrastructureAgents region is empty. Used to verify that Update preserves the
// empty region rather than introducing content.
const orchestratorWithEmptyInfrastructureAgentsDeployed = `---
version: 6.0.0
name: orchestrator
description: Central coordinator that manages multi-agent workflow execution
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: multi-phase coordination
required_skills: []
---

<Identity type="core">
# Orchestrator Agent

You are the Orchestrator.

<InfrastructureAgents type="managed">
</InfrastructureAgents>

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`

// TestInfrastructureAgents_Update_WithDeployedContent_PreservesContentByteForByte verifies
// that when req.Deployed is non-nil (Update scenario) and req.InfrastructureAgents is empty,
// transform.Apply copies the deployed file's InfrastructureAgents region content into the
// output unchanged — byte-for-byte identical to what was in the deployed file.
func TestInfrastructureAgents_Update_WithDeployedContent_PreservesContentByteForByte(t *testing.T) {
	// Arrange: deployed file has InfrastructureAgents with content. Extract the content that
	// must survive the Update so we can assert against it precisely.
	deployed := []byte(orchestratorWithInfrastructureAgentsDeployed)
	deployedDoc, err := docformat.Parse(deployed)
	if err != nil {
		t.Fatalf("parse deployed fixture: %v", err)
	}
	deployedNode, ok := deployedDoc.Body().Deployed("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents absent from deployed fixture — fixture is malformed")
	}
	wantContent := deployedNode.Content()

	req := transform.Request{
		Source:               []byte(orchestratorWithInfrastructureAgents),
		Kind:                 domain.ArtifactAgent,
		Key:                  "orchestrator",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		Deployed:             deployed,
		InfrastructureAgents: nil, // Update: no new infrastructure agent selections
	}

	// Act
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Assert: output InfrastructureAgents content is byte-identical to the deployed file.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outNode, ok := outDoc.Body().Deployed("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents region absent from output")
	}
	if !bytes.Equal(outNode.Content(), wantContent) {
		t.Errorf("InfrastructureAgents content not preserved on Update:\ngot:  %q\nwant: %q",
			outNode.Content(), wantContent)
	}
}

// TestInfrastructureAgents_Update_WithDeployedContent_ReportsPreservedAction verifies that
// when req.Deployed is non-nil and req.InfrastructureAgents is empty, the RegionOutcome for
// InfrastructureAgents carries Action == RegionPreservedInfra, the dedicated constant for
// infrastructure region preservation (value "preserved-infrastructure"), distinct from
// RegionPreserved which is used for project-class regions.
func TestInfrastructureAgents_Update_WithDeployedContent_ReportsPreservedAction(t *testing.T) {
	req := transform.Request{
		Source:               []byte(orchestratorWithInfrastructureAgents),
		Kind:                 domain.ArtifactAgent,
		Key:                  "orchestrator",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		Deployed:             []byte(orchestratorWithInfrastructureAgentsDeployed),
		InfrastructureAgents: nil, // Update: no new infrastructure agent selections
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "InfrastructureAgents" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatal("RegionOutcome for InfrastructureAgents absent from report")
	}
	if outcome.Action != transform.RegionPreservedInfra {
		t.Errorf("InfrastructureAgents action on Update: want %q, got %q",
			transform.RegionPreservedInfra, outcome.Action)
	}
}

// TestInfrastructureAgents_Update_WithDeployedContent_ReportHasNoAgentKeys verifies that
// when the InfrastructureAgents region is preserved from the deployed file (not assembled),
// Report.InfrastructureAgents is empty — the keys were not re-assembled in this pass.
func TestInfrastructureAgents_Update_WithDeployedContent_ReportHasNoAgentKeys(t *testing.T) {
	req := transform.Request{
		Source:               []byte(orchestratorWithInfrastructureAgents),
		Kind:                 domain.ArtifactAgent,
		Key:                  "orchestrator",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		Deployed:             []byte(orchestratorWithInfrastructureAgentsDeployed),
		InfrastructureAgents: nil, // Update: no new infrastructure agent selections
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Preservation path does not re-assemble keys; report list must be empty.
	if len(result.Report.InfrastructureAgents) != 0 {
		t.Errorf("Report.InfrastructureAgents on Update preserve: want empty, got %v",
			result.Report.InfrastructureAgents)
	}
}

// TestInfrastructureAgents_DeployNew_NoDeployed_NoAgents_RegionCleared verifies that when
// req.Deployed is nil (DeployNew) and req.InfrastructureAgents is empty, the
// InfrastructureAgents region is cleared as before — the DeployNew path is unchanged by
// the new preservation logic.
func TestInfrastructureAgents_DeployNew_NoDeployed_NoAgents_RegionCleared(t *testing.T) {
	req := transform.Request{
		Source:               []byte(orchestratorWithInfrastructureAgents),
		Kind:                 domain.ArtifactAgent,
		Key:                  "orchestrator",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		Deployed:             nil, // DeployNew: no previously-deployed file
		InfrastructureAgents: nil, // no agents selected
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outNode, ok := outDoc.Body().Deployed("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents region absent from output")
	}
	if !outNode.IsEmpty() {
		t.Errorf("DeployNew with no agents: InfrastructureAgents must be empty; got: %q",
			outNode.Content())
	}

	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "InfrastructureAgents" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatal("RegionOutcome for InfrastructureAgents absent from report")
	}
	if outcome.Action != transform.RegionEmptied {
		t.Errorf("DeployNew no-agents action: want %q, got %q", transform.RegionEmptied, outcome.Action)
	}
}

// TestInfrastructureAgents_Update_WithAgentsProvided_AssemblesFromRequest verifies that
// when req.Deployed is non-nil (Update scenario) but req.InfrastructureAgents is non-empty,
// the region is assembled from the supplied blocks rather than lifted from the deployed file.
// Non-empty InfrastructureAgents always triggers assembly regardless of the Update/DeployNew
// path.
func TestInfrastructureAgents_Update_WithAgentsProvided_AssemblesFromRequest(t *testing.T) {
	req := transform.Request{
		Source:  []byte(orchestratorWithInfrastructureAgents),
		Kind:    domain.ArtifactAgent,
		Key:     "orchestrator",
		Module:  newFixtureModule(t),
		Model:   fixtureModel(),
		Scope:   domain.ScopeProject,
		Deployed: []byte(orchestratorWithInfrastructureAgentsDeployed),
		InfrastructureAgents: []transform.InfrastructureBlock{
			{
				Key:         "checkpoint-manager-git",
				Version:     "2.0.0",
				Class:       "checkpoint",
				Description: "Commits a restorable checkpoint of the working tree.",
				OnFailure:   "halt",
				Triggers: []domain.InfrastructureTrigger{
					{Trigger: "STAGE_END", TriggerParam: ""},
				},
			},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outNode, ok := outDoc.Body().Deployed("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents region absent from output")
	}
	content := outNode.Content()

	// The output must contain the newly-assembled block, not the deployed block.
	if !bytes.Contains(content, []byte(`name="checkpoint-manager-git"`)) {
		t.Errorf("assembled block for checkpoint-manager-git absent from output; got: %q", content)
	}
	// The deployed block must NOT be present (assembly replaces it).
	if bytes.Contains(content, []byte(`name="orchestration-review"`)) {
		t.Errorf("deployed block for orchestration-review must not appear when assembly is requested; got: %q", content)
	}

	// RegionOutcome must be RegionAssembledInfra, not RegionPreserved.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "InfrastructureAgents" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatal("RegionOutcome for InfrastructureAgents absent from report")
	}
	if outcome.Action != transform.RegionAssembledInfra {
		t.Errorf("Update with agents action: want %q, got %q",
			transform.RegionAssembledInfra, outcome.Action)
	}
}

// TestInfrastructureAgents_Update_EmptyDeployedRegion_PreservesEmpty verifies that when
// req.Deployed is non-nil (Update scenario), req.InfrastructureAgents is empty, AND the
// deployed file's InfrastructureAgents region is itself empty, the output region is also
// empty. Update must not introduce any content; it preserves what was there, even if that
// content is nothing.
func TestInfrastructureAgents_Update_EmptyDeployedRegion_PreservesEmpty(t *testing.T) {
	req := transform.Request{
		Source:               []byte(orchestratorWithInfrastructureAgents),
		Kind:                 domain.ArtifactAgent,
		Key:                  "orchestrator",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		Deployed:             []byte(orchestratorWithEmptyInfrastructureAgentsDeployed),
		InfrastructureAgents: nil, // Update: no new selections
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outNode, ok := outDoc.Body().Deployed("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents region absent from output")
	}
	if !outNode.IsEmpty() {
		t.Errorf("Update with empty deployed region: InfrastructureAgents must remain empty; got: %q",
			outNode.Content())
	}
}

// TestInfrastructureAgents_Update_DeployedHasNoInfrastructureRegion_GracefulNoOp verifies
// that when req.Deployed is non-nil (Update scenario) but the deployed file contains no
// InfrastructureAgents region at all — as would occur when the source file adds the region
// for the first time while a previous deployed file predates it — transform.Apply succeeds
// without error and the output region is empty. Parse failure on the deployed-side lookup
// must not cause a panic or a transform error; it is a graceful no-op per the Error Handling
// Strategy: no content is introduced from the deployed file.
func TestInfrastructureAgents_Update_DeployedHasNoInfrastructureRegion_GracefulNoOp(t *testing.T) {
	req := transform.Request{
		Source:               []byte(orchestratorWithInfrastructureAgents),
		Kind:                 domain.ArtifactAgent,
		Key:                  "orchestrator",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		Deployed:             []byte(orchestratorWithoutInfrastructureAgentsDeployed),
		InfrastructureAgents: nil, // Update: no new selections
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply must not error when deployed file has no InfrastructureAgents region: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outNode, ok := outDoc.Body().Deployed("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents region absent from output — source defines it, it must be present")
	}
	// No content from the deployed file (it had none); region must be empty.
	if !outNode.IsEmpty() {
		t.Errorf("Update with no deployed region: InfrastructureAgents must be empty (graceful no-op); got: %q",
			outNode.Content())
	}
}
