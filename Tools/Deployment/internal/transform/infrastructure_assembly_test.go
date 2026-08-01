package transform_test

// infrastructure_assembly_test.go covers infrastructure declaration block assembly
// (AssembleInfrastructureBlocks) and the InfrastructureAgents injection pipeline:
//
//   - Zero agents: the InfrastructureAgents injection is left empty; AssembleInfrastructureBlocks
//     returns nil bytes and nil keys.
//   - Single trigger: the [[SECTION:InfrastructureAgent:{key}]] block is produced with the
//     correct section markers, infra-version comment, table header, and one data row.
//   - Multi-trigger: one table row per trigger entry, Class and On Failure repeated on each.
//   - Null/empty TriggerParam: rendered as "-" in the Param column.
//   - Multiple agents: blocks are composed in the order of the input slice.
//   - Pipeline: Request.InfrastructureAgents is assembled into [[INJECTION:InfrastructureAgents]]
//     by transform.Apply, analogous to Request.Workflows and [[INJECTION:AvailableWorkflows]].

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// orchestratorWithInfrastructureAgents is an orchestrator-like source with both an
// [[INJECTION:InfrastructureAgents]] and an [[INJECTION:AvailableWorkflows]] inside the
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

[[SECTION:Identity]]
# Orchestrator Agent

You are the Orchestrator.

[[INJECTION:InfrastructureAgents]]
[[/INJECTION:InfrastructureAgents]]

[[INJECTION:AvailableWorkflows]]
[[/INJECTION:AvailableWorkflows]]

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
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
// contains the correct [[SECTION:InfrastructureAgent:{key}]] open and close markers.
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

	if !bytes.Contains(assembled, []byte("[[SECTION:InfrastructureAgent:orchestration-review]]")) {
		t.Errorf("output missing section open marker; got:\n%s", assembled)
	}
	if !bytes.Contains(assembled, []byte("[[/SECTION:InfrastructureAgent:orchestration-review]]")) {
		t.Errorf("output missing section close marker; got:\n%s", assembled)
	}
}

// TestAssembleInfrastructureBlocks_SingleTrigger_VersionComment verifies that the
// infra-version comment appears immediately inside the section, carrying the version field
// from the InfrastructureBlock. This comment is used for staleness detection on updates.
func TestAssembleInfrastructureBlocks_SingleTrigger_VersionComment(t *testing.T) {
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

	if !bytes.Contains(assembled, []byte("<!-- infra-version: 1.0.0 -->")) {
		t.Errorf("output missing infra-version comment; got:\n%s", assembled)
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
// two agents are supplied, both produce their [[SECTION:InfrastructureAgent:{key}]] blocks
// in the output.
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

	if !bytes.Contains(assembled, []byte("[[SECTION:InfrastructureAgent:checkpoint-manager-git]]")) {
		t.Errorf("output missing checkpoint-manager-git block; got:\n%s", assembled)
	}
	if !bytes.Contains(assembled, []byte("[[SECTION:InfrastructureAgent:orchestration-review]]")) {
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

	reviewPos := bytes.Index(assembled, []byte("[[SECTION:InfrastructureAgent:orchestration-review]]"))
	checkpointPos := bytes.Index(assembled, []byte("[[SECTION:InfrastructureAgent:checkpoint-manager-git]]"))

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
// [[INJECTION:InfrastructureAgents]] region empty in the output.
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
	node, ok := outDoc.Body().Injection("InfrastructureAgents")
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
// infrastructure agent is supplied, its [[SECTION:InfrastructureAgent:{key}]] block appears
// inside the [[INJECTION:InfrastructureAgents]] region of the output.
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
	node, ok := outDoc.Body().Injection("InfrastructureAgents")
	if !ok {
		t.Fatal("InfrastructureAgents injection absent from output")
	}

	content := node.Content()
	for _, marker := range []string{
		"[[SECTION:InfrastructureAgent:orchestration-review]]",
		"<!-- infra-version: 1.0.0 -->",
		"[[/SECTION:InfrastructureAgent:orchestration-review]]",
	} {
		if !bytes.Contains(content, []byte(marker)) {
			t.Errorf("InfrastructureAgents content missing expected marker %q\ncontent: %q", marker, content)
		}
	}

	// Report.InfrastructureAgents must list the assembled key.
	if len(result.Report.InfrastructureAgents) != 1 || result.Report.InfrastructureAgents[0] != "orchestration-review" {
		t.Errorf("Report.InfrastructureAgents: want [orchestration-review], got %v", result.Report.InfrastructureAgents)
	}
}

// TestInfrastructureAgents_InjectionClass_IsInfrastructure verifies that the
// InjectionOutcome for the InfrastructureAgents region carries
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

			var outcome *transform.InjectionOutcome
			for i := range result.Report.Injections {
				if result.Report.Injections[i].Name == "InfrastructureAgents" {
					outcome = &result.Report.Injections[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("InjectionOutcome for InfrastructureAgents absent from report")
			}
			if outcome.Class != domain.InjectionInfrastructure {
				t.Errorf("InfrastructureAgents Class: want %q, got %q",
					domain.InjectionInfrastructure, outcome.Class)
			}
		})
	}
}

// TestInfrastructureAgents_AssembledAction_WhenAgentsSupplied verifies that when
// infrastructure agents are provided, the InjectionOutcome carries Action ==
// InjectionAssembledInfra, analogous to InjectionAssembled for AvailableWorkflows.
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

	var outcome *transform.InjectionOutcome
	for i := range result.Report.Injections {
		if result.Report.Injections[i].Name == "InfrastructureAgents" {
			outcome = &result.Report.Injections[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("InjectionOutcome for InfrastructureAgents absent from report")
	}
	if outcome.Action != transform.InjectionAssembledInfra {
		t.Errorf("InfrastructureAgents action: want %q, got %q", transform.InjectionAssembledInfra, outcome.Action)
	}
}
