package transform_test

// retired_region_test.go covers agents that used to carry the retired
// ProtocolConstraints managed region:
//
//   - A fresh deploy of a source that no longer declares the region succeeds and the
//     output contains no such region.
//   - A workspace update, where the previously deployed file still carries the region,
//     succeeds, drops the region from the output, and reports no recovery gap for it.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

const sourceWithoutRetiredRegion = `---
id: 70
version: 2.0.0
name: retired-region-test
description: Agent whose source no longer declares the retired region
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: retired region testing
required_skills: []
---

<Identity type="core">
# RetiredRegionTest Agent

You are the RetiredRegionTest agent.
</Identity>

<Constraints type="core">
## Constraints

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

const deployedWithRetiredRegion = `---
id: 70
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent whose source no longer declares the retired region
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# RetiredRegionTest Agent

You are the RetiredRegionTest agent.
</Identity>

<Constraints type="core">
## Constraints

<ProtocolConstraints type="managed">
Stale protocol constraints content from an earlier deploy.
</ProtocolConstraints>

<HarnessConstraints type="managed">
Stale harness content.
</HarnessConstraints>
</Constraints>
`

func retiredRegionRequest(t *testing.T, deployed []byte) transform.Request {
	t.Helper()
	return transform.Request{
		Source:   []byte(sourceWithoutRetiredRegion),
		Deployed: deployed,
		Kind:     domain.ArtifactAgent,
		Key:      "retired-region-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}
}

func TestRetiredRegion_FreshDeploy_OutputHasNoRetiredRegion(t *testing.T) {
	result, err := transform.Apply(retiredRegionRequest(t, nil))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if strings.Contains(string(result.Output), "ProtocolConstraints") {
		t.Errorf("fresh deploy output must not contain the retired region:\n%s", result.Output)
	}
}

func TestRetiredRegion_WorkspaceUpdate_DropsRegionFromOutput(t *testing.T) {
	result, err := transform.Apply(retiredRegionRequest(t, []byte(deployedWithRetiredRegion)))
	if err != nil {
		t.Fatalf("Apply on an update whose deployed file carries the retired region: %v", err)
	}

	if strings.Contains(string(result.Output), "ProtocolConstraints") {
		t.Errorf("update output must not contain the retired region:\n%s", result.Output)
	}
	if strings.Contains(string(result.Output), "Stale protocol constraints content") {
		t.Errorf("update output must not carry the stale retired-region content:\n%s", result.Output)
	}
}

func TestRetiredRegion_WorkspaceUpdate_ReportsNoRecoveryGapForRetiredRegion(t *testing.T) {
	result, err := transform.Apply(retiredRegionRequest(t, []byte(deployedWithRetiredRegion)))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, g := range result.Report.Gaps {
		if g.Subject == "ProtocolConstraints" {
			t.Errorf("tool-managed content is regenerated, never recovered; unexpected gap: %+v", g)
		}
	}
}
