package builtin_test

// Verifies that every builtin harness strips the three source-only infrastructure-agent
// frontmatter keys (infrastructure, triggers, on_failure) from deployed agents, leaves
// non-infrastructure agents unaffected, and leaves the orchestrator's assembled
// InfrastructureAgents region unchanged. Fixtures come from the tool-local testdata only.

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/claudecode"
	"mosaic-deploy/internal/harness/builtin/ghcpcli"
	"mosaic-deploy/internal/harness/builtin/opencode"
	"mosaic-deploy/internal/harness/builtin/vscodeghcp"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

var infraSourceOnlyKeys = []string{"infrastructure", "triggers", "on_failure"}

func deploymentTestdata(t *testing.T, parts ...string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "..", "testdata"}, parts...)...))
	if err != nil {
		t.Fatalf("resolve testdata path: %v", err)
	}
	return abs
}

type harnessCase struct {
	name  string
	model domain.ModelSelection
	build func(registry.BuiltinOptions) (domain.HarnessModule, error)
}

func allBuiltinHarnesses() []harnessCase {
	return []harnessCase{
		{"claude-code", domain.ModelSelection{ModelID: "claude-sonnet-4-6", Origin: domain.OriginHarnessList}, claudecode.New},
		{"opencode", domain.ModelSelection{ModelID: "github-copilot/claude-sonnet-4-6", Origin: domain.OriginHarnessList}, opencode.New},
		{"ghcp-cli", domain.ModelSelection{ModelID: "claude-sonnet-4-6", Origin: domain.OriginHarnessList}, ghcpcli.New},
		{"vscode-ghcp", domain.ModelSelection{ModelID: "Claude Sonnet 4.6", Origin: domain.OriginHarnessList}, vscodeghcp.New},
	}
}

func deployFixture(t *testing.T, h harnessCase, fixture string) *docformat.Document {
	t.Helper()
	frozen := deploymentTestdata(t, "frozen-catalog")
	mod, err := h.build(registry.BuiltinOptions{MosaicRoot: frozen})
	if err != nil {
		t.Fatalf("build module: %v", err)
	}
	protocol, err := catalog.FileProtocolLoader{}.LoadProtocol(frozen)
	if err != nil {
		t.Fatalf("load protocol: %v", err)
	}
	src, err := os.ReadFile(deploymentTestdata(t, "agent-fixtures", fixture))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	res, err := transform.Apply(transform.Request{
		Source: src, Kind: domain.ArtifactAgent, Key: "fixture-agent", Module: mod,
		Model: h.model, Scope: domain.ScopeProject, Role: domain.RoleWorker, Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	doc, err := docformat.Parse(res.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	return doc
}

// A deployed infrastructure agent carries none of the three source-only keys, and the
// drop removes nothing else: its key set equals that of an equivalent non-infrastructure agent.
func TestInfrastructureAgent_DeployedWithoutSourceOnlyKeys(t *testing.T) {
	for _, h := range allBuiltinHarnesses() {
		h := h
		t.Run(h.name, func(t *testing.T) {
			infra := deployFixture(t, h, "checkpoint-infra.md").Frontmatter()
			for _, key := range infraSourceOnlyKeys {
				if _, ok := infra.Get(key); ok {
					t.Errorf("deployed infrastructure agent still carries %q", key)
				}
			}
			plain := deployFixture(t, h, "test-runner.md").Frontmatter()
			if !reflect.DeepEqual(infra.Keys(), plain.Keys()) {
				t.Errorf("infrastructure agent keys %v differ from non-infrastructure agent keys %v",
					infra.Keys(), plain.Keys())
			}
		})
	}
}

const orchestratorWithInfraRegion = `---
version: 6.0.0
name: orchestrator-infra-test
description: Orchestrator carrying the assembled infrastructure region
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: infra region check
required_skills: []
---

<Identity type="core">
# Orchestrator

<InfrastructureAgents type="managed">
</InfrastructureAgents>
</Identity>
`

// The orchestrator's InfrastructureAgents region is assembled from catalog sources and
// must be byte-identical to the assembly output regardless of the frontmatter drop set.
func TestOrchestrator_InfrastructureAgentsRegion_UnchangedByDropSet(t *testing.T) {
	blocks := []transform.InfrastructureBlock{{
		Key: "checkpoint-agent", Version: "1.0.0", Class: "checkpoint",
		Description: "Saves progress after each stage.", OnFailure: "halt",
		Triggers: []domain.InfrastructureTrigger{{Trigger: "STAGE_END"}},
	}}
	want, _ := transform.AssembleInfrastructureBlocks(blocks)
	for _, h := range allBuiltinHarnesses() {
		h := h
		t.Run(h.name, func(t *testing.T) {
			frozen := deploymentTestdata(t, "frozen-catalog")
			mod, err := h.build(registry.BuiltinOptions{MosaicRoot: frozen})
			if err != nil {
				t.Fatalf("build module: %v", err)
			}
			res, err := transform.Apply(transform.Request{
				Source: []byte(orchestratorWithInfraRegion), Kind: domain.ArtifactAgent,
				Key: "orchestrator-infra-test", Module: mod, Model: h.model,
				Scope: domain.ScopeProject, Role: domain.RoleOrchestrator,
				InfrastructureAgents: blocks,
			})
			if err != nil {
				t.Fatalf("transform.Apply: %v", err)
			}
			doc, err := docformat.Parse(res.Output)
			if err != nil {
				t.Fatalf("parse output: %v", err)
			}
			node, ok := doc.Body().Deployed("InfrastructureAgents")
			if !ok {
				t.Fatal("InfrastructureAgents region absent from deployed orchestrator")
			}
			if !bytes.Contains(node.Content(), bytes.TrimSpace(want)) {
				t.Errorf("InfrastructureAgents region content changed;\nwant it to contain %q\ngot %q", want, node.Content())
			}
			for _, key := range infraSourceOnlyKeys {
				if _, present := doc.Frontmatter().Get(key); present {
					t.Errorf("deployed orchestrator frontmatter carries %q", key)
				}
			}
		})
	}
}
