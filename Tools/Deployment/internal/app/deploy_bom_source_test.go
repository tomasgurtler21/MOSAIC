package app_test

// deploy_bom_source_test.go verifies deploying an agent from a catalog source that starts with
// a UTF-8 BOM, end to end: a temp-dir catalog loaded by the real loader, the real planner and
// executor, and a temp workspace. The manifest entry must record the source's version, and the
// written agent must carry resolved placeholders, complete frontmatter and no BOM.
//
// No test reads the live Catalog/ directory.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/catalog/catalogpaths"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

const bomAgentKey = "bom-agent"

const bomAgentSourceLF = `---
id: bom-agent-001
version: 1.2.3
name: bom-agent
description: Agent whose source starts with a BOM
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

# BOM agent

Some prose.
`

// bomFixtureModule is a harness module driven by the shared fixture descriptor, so the
// deployed frontmatter is shaped by real descriptor algorithms.
type bomFixtureModule struct{ desc *domain.HarnessDescriptor }

func newBOMFixtureModule(t *testing.T) *bomFixtureModule {
	t.Helper()
	const path = "../../testdata/transform/fixture.yaml"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture descriptor: %v", err)
	}
	desc, err := descriptor.Parse(src, path)
	if err != nil {
		t.Fatalf("parse fixture descriptor: %v", err)
	}
	return &bomFixtureModule{desc: desc}
}

func (m *bomFixtureModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: m.desc.ID, DisplayName: m.desc.DisplayName, Tier: domain.TierDescriptor, Usable: true}
}
func (m *bomFixtureModule) Descriptor() *domain.HarnessDescriptor { return m.desc }
func (m *bomFixtureModule) Close() error                           { return nil }
func (m *bomFixtureModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return descriptor.MapTools(m.desc, req)
}
func (m *bomFixtureModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return descriptor.ApplyFrontmatterSpec(m.desc, req)
}
func (m *bomFixtureModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return descriptor.ResolveTargetPath(m.desc, req)
}
func (m *bomFixtureModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *bomFixtureModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "fixture harness declares no hook support"}, nil
}

// writeBOMCatalog creates a temp MOSAIC root whose catalog holds one subagent source with the
// given bytes, and returns the root.
func writeBOMCatalog(t *testing.T, agentSource []byte) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, data []byte) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write(catalogpaths.MosaicRelSourceFilesFormatFile, []byte("# CatalogFilesFormat\n"))
	write(filepath.Join("Workflows", "Index.md"), []byte("# Workflows Index\n\n| ID | Category | Version | Name | Description | Hint | File |\n|----|----------|---------|------|-------------|------|------|\n"))
	write(filepath.Join("Catalog", "Subagents", "Execution", bomAgentKey+".md"), agentSource)
	return root
}

// deployBOMAgent deploys the catalog's agent into a fresh workspace with the real planner,
// executor and manifest store, and returns the workspace and the manifest entry written.
func deployBOMAgent(t *testing.T, root string) (string, domain.ManifestEntry) {
	t.Helper()
	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	mod := newBOMFixtureModule(t)
	store := manifest.NewStore()
	deps.Catalog = cat
	deps.MosaicRoot = root
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{mod.Ref()},
		modules: map[string]domain.HarnessModule{mod.desc.ID: mod},
	}
	deps.Planner = plan.New()
	deps.Manifest = store
	deps.Executor = deploy.NewExecutor(store, deps.Logger, deps.Todo)

	_, err = app.New(deps).DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              mod.desc.ID,
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		SubagentIDs:            []string{bomAgentKey},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		StandaloneAgentIDs:     []string{},
		AgentModels:            map[string]string{bomAgentKey: "claude/claude-sonnet"},
		AutoConfirmPlan:        true,
	})
	if err != nil {
		t.Fatalf("DeployAgents: %v", err)
	}
	snap, err := store.Load(workspace)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for _, e := range snap.Manifest.Entries {
		if e.Ref.Key == bomAgentKey {
			return workspace, e
		}
	}
	t.Fatalf("manifest has no entry for %q; entries: %+v", bomAgentKey, snap.Manifest.Entries)
	return "", domain.ManifestEntry{}
}

// TestDeploy_BOMCatalogSource_RecordsVersionAndWritesCompleteAgent verifies the manifest entry
// carries the source's version and the written agent is complete and BOM-less, for LF and
// CRLF catalog sources.
func TestDeploy_BOMCatalogSource_RecordsVersionAndWritesCompleteAgent(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"LF", bomAgentSourceLF},
		{"CRLF", strings.ReplaceAll(bomAgentSourceLF, "\n", "\r\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeBOMCatalog(t, []byte("\xEF\xBB\xBF"+tc.src))

			workspace, entry := deployBOMAgent(t, root)

			if entry.Version != "1.2.3" {
				t.Errorf("manifest entry version = %q, want the source version 1.2.3", entry.Version)
			}
			path := entry.TargetPath
			if !filepath.IsAbs(path) {
				path = filepath.Join(workspace, path)
			}
			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read deployed agent: %v", err)
			}
			if strings.HasPrefix(string(written), "\xEF\xBB\xBF") {
				t.Error("deployed agent starts with a BOM; the output must be BOM-less")
			}
			if strings.Contains(string(written), "{model-identifier}") {
				t.Error("model placeholder was not resolved in the deployed agent")
			}
			doc, err := docformat.Parse(written)
			if err != nil {
				t.Fatalf("parse deployed agent: %v", err)
			}
			fm := doc.Frontmatter()
			for key, want := range map[string]string{
				"mosaic_version": "1.2.3",
				"model":          "claude/claude-sonnet",
			} {
				if v, ok := fm.Get(key); !ok || v.Scalar != want {
					t.Errorf("deployed frontmatter %s = %q (present=%v), want %q", key, v.Scalar, ok, want)
				}
			}
			if _, ok := fm.Get("mosaic_harness_version"); !ok {
				t.Error("deployed frontmatter lacks the mosaic_harness_version stamp")
			}
		})
	}
}
