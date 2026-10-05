package app

// deployedstate_infra_test.go covers how probeDeployedArtifact reports the infrastructure
// agent declarations of a deployed file in DeployedArtifactState.InfrastructureDeclarations.
//
//   - A deployed orchestrator with assembled declarations lists each key and version, with the
//     parsed class, triggers and on-failure.
//   - A file with no InfrastructureAgents region, or an empty one, yields none.
//   - A hand-written section that is not the assembler's table shape is listed as unparsed.
//   - Files without declarations (ordinary agents) are unaffected.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

const probeInfraFrontmatter = "---\nversion: \"1.0\"\ntransform_version: \"1.0\"\n---\n\n"

const probeHandWrittenSection = "<InfrastructureAgent type=\"core\" name=\"hand-made\" version=\"0.1\">\n\n" +
	"Added by hand, runs whenever the lead asks.\n\n</InfrastructureAgent>\n"

func probeInfraBlock(key, version, class, onFailure string, triggers ...domain.InfrastructureTrigger) transform.InfrastructureBlock {
	return transform.InfrastructureBlock{
		Key: key, Version: version, Class: class, OnFailure: onFailure,
		Description: "Checks for " + key + ".", Triggers: triggers,
	}
}

// deployedOrchestratorWithInfra returns a deployed orchestrator whose InfrastructureAgents
// region holds inner.
func deployedOrchestratorWithInfra(inner string) []byte {
	return []byte(probeInfraFrontmatter +
		"<Identity type=\"core\">\n# Orchestrator\n\n" +
		"<InfrastructureAgents type=\"managed\">\n" + inner + "</InfrastructureAgents>\n\n" +
		"<AvailableWorkflows type=\"managed\">\n</AvailableWorkflows>\n</Identity>\n")
}

func probeInfra(t *testing.T, content []byte) domain.DeployedInfrastructureDeclarations {
	t.Helper()
	ws := t.TempDir()
	writeFile(t, ws, "orchestrator.md", content)
	return probeDeployedArtifact(ws, "orchestrator.md", "").InfrastructureDeclarations
}

func TestProbeDeployedArtifact_AssembledDeclarations_ListsKeysVersionsAndParsedFields(t *testing.T) {
	assembled, _ := transform.AssembleInfrastructureBlocks([]transform.InfrastructureBlock{
		probeInfraBlock("code-review", "1.2", "review", "continue",
			domain.InfrastructureTrigger{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"}),
		probeInfraBlock("checkpoint-agent", "2.0", "checkpoint", "halt",
			domain.InfrastructureTrigger{Trigger: "STAGE_END"},
			domain.InfrastructureTrigger{Trigger: "PHASE_END"}),
	})

	got := probeInfra(t, deployedOrchestratorWithInfra(string(assembled)))

	if len(got) != 2 {
		t.Fatalf("InfrastructureDeclarations = %+v, want 2 entries", got)
	}
	first, second := got[0], got[1]
	if first.Key != "code-review" || first.Version != "1.2" || !first.Parsed ||
		first.Class != "review" || first.OnFailure != "continue" ||
		len(first.Triggers) != 1 || first.Triggers[0] != (domain.InfrastructureTrigger{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"}) {
		t.Errorf("first declaration = %+v, want parsed code-review 1.2 review/continue with one interval trigger", first)
	}
	if second.Key != "checkpoint-agent" || second.Version != "2.0" || !second.Parsed ||
		second.Class != "checkpoint" || second.OnFailure != "halt" ||
		len(second.Triggers) != 2 || second.Triggers[0].Trigger != "STAGE_END" ||
		second.Triggers[0].TriggerParam != "" || second.Triggers[1].Trigger != "PHASE_END" {
		t.Errorf("second declaration = %+v, want parsed checkpoint-agent 2.0 checkpoint/halt with two triggers", second)
	}
}

func TestProbeDeployedArtifact_NoInfrastructureRegion_NoDeclarations(t *testing.T) {
	content := []byte(probeInfraFrontmatter + "<Identity type=\"core\">\n# Orchestrator\n</Identity>\n")

	if got := probeInfra(t, content); len(got) != 0 {
		t.Errorf("InfrastructureDeclarations = %+v, want none when the file has no InfrastructureAgents region", got)
	}
}

func TestProbeDeployedArtifact_EmptyInfrastructureRegion_NoDeclarations(t *testing.T) {
	if got := probeInfra(t, deployedOrchestratorWithInfra("")); len(got) != 0 {
		t.Errorf("InfrastructureDeclarations = %+v, want none for an empty region", got)
	}
}

func TestProbeDeployedArtifact_HandWrittenSection_ListedAsUnparsed(t *testing.T) {
	assembled, _ := transform.AssembleInfrastructureBlocks([]transform.InfrastructureBlock{
		probeInfraBlock("code-review", "1.0", "review", "continue",
			domain.InfrastructureTrigger{Trigger: "STAGE_END"}),
	})

	got := probeInfra(t, deployedOrchestratorWithInfra(string(assembled)+probeHandWrittenSection))

	if len(got) != 2 {
		t.Fatalf("InfrastructureDeclarations = %+v, want 2 entries", got)
	}
	if !got[0].Parsed || got[0].Key != "code-review" {
		t.Errorf("first declaration = %+v, want parsed code-review", got[0])
	}
	hand := got[1]
	if hand.Key != "hand-made" || hand.Version != "0.1" || hand.Parsed {
		t.Errorf("second declaration = %+v, want unparsed hand-made version 0.1", hand)
	}
}

func TestProbeDeployedArtifact_OrdinaryAgentFile_NoDeclarations(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, ws, "worker.md", []byte("---\nversion: \"1.0\"\n---\n\nA plain worker agent.\n"))

	state := probeDeployedArtifact(ws, "worker.md", "")

	if !state.Present || state.Version != "1.0" {
		t.Errorf("state = %+v, want present with version 1.0 (probe otherwise unchanged)", state)
	}
	if len(state.InfrastructureDeclarations) != 0 {
		t.Errorf("InfrastructureDeclarations = %+v, want none for a file without declarations", state.InfrastructureDeclarations)
	}
}

func TestProbeDeployedArtifact_AbsentFile_NoDeclarations(t *testing.T) {
	state := probeDeployedArtifact(t.TempDir(), "missing.md", "")

	if len(state.InfrastructureDeclarations) != 0 {
		t.Errorf("InfrastructureDeclarations = %+v, want none for an absent file", state.InfrastructureDeclarations)
	}
}
