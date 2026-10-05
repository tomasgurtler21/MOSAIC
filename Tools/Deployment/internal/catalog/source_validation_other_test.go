package catalog_test

// source_validation_other_test.go covers source validation for orchestrator existence
// rules, skills, and workflows. Helpers live in source_validation_test.go.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
)

const legacyOrchestratorRel = "Catalog/Agents/Generic/Orchestrator/orchestrator.md"

func TestLoad_MissingOrchestratorAndScript_LoadsWithoutError(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeFrontmatterSource(t, root, "Catalog/Subagents/Cat/worker.md", validSubagentFM)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	if cat.Orchestrator().Key != "" {
		t.Error("orchestrator unexpectedly loaded")
	}
	if _, ok := cat.OrchestratorScript(); ok {
		t.Error("orchestrator script unexpectedly loaded")
	}
}

func TestLoad_BrokenNewPathOrchestrator_ReportedNotReplacedByLegacy(t *testing.T) {
	root := makeTempMosaicRoot(t)
	p := writeSource(t, root, "Catalog/Orchestrator/orchestrator.md", []byte("\u200b---\nname: o\n---\n"))
	// The legacy file is itself invalid, so any fallback to it would add its path to the error.
	legacy := writeSource(t, root, legacyOrchestratorRel, []byte("no frontmatter\n"))

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
	if strings.Contains(err.Error(), legacyOrchestratorRel) || strings.Contains(err.Error(), filepath.ToSlash(legacy)) {
		t.Errorf("error names the legacy orchestrator, which must not be consulted: %v", err)
	}
}

func TestLoad_MissingNewPathOrchestrator_FallsBackToValidLegacy(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeFrontmatterSource(t, root, legacyOrchestratorRel, "name: Legacy\n")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	if cat.Orchestrator().Key == "" {
		t.Error("legacy orchestrator was not loaded when the new path is absent")
	}
}

func TestLoad_BrokenLegacyOrchestratorWithoutNewPath_Reported(t *testing.T) {
	root := makeTempMosaicRoot(t)
	p := writeSource(t, root, legacyOrchestratorRel, []byte("no frontmatter\n"))

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
}

func TestLoad_BrokenOrchestratorScript_Reported(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeFrontmatterSource(t, root, "Catalog/Orchestrator/orchestrator.md", "name: Orch\n")
	p := writeSource(t, root, "Catalog/Orchestrator/orchestrator-script.md", []byte("\u200b---\nname: s\n---\n"))

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
}

func TestLoad_BrokenSkill_FailsNamingFile(t *testing.T) {
	root := makeTempMosaicRoot(t)
	p := writeSource(t, root, "Catalog/Skills/broken/SKILL.md", []byte("\u200b---\nname: s\n---\n"))

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
}

func TestLoad_BrokenSkillInCustomCatalogRoot_FailsNamingFile(t *testing.T) {
	root := makeTempMosaicRoot(t)
	catalogRoot := makeDistinctCatalogRoot(t)
	p := writeSource(t, catalogRoot, "Skills/broken/SKILL.md", []byte("\u200b---\nname: s\n---\n"))

	_, err := catalog.Load(root, catalogRoot)

	requireInvalidSource(t, err, p)
}

func TestLoad_BrokenLegacySkill_FailsNamingFile(t *testing.T) {
	root := makeTempMosaicRoot(t)
	content := append([]byte("---\nname: s\n---\n"), 0xff, '\n')
	p := writeSource(t, root, "Catalog/Agents/Generic/Skills/broken/SKILL.md", content)

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
}

func TestLoad_SkillWithoutFrontmatter_LoadsWithoutError(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSource(t, root, "Catalog/Skills/plain/SKILL.md", []byte("# Plain skill, no frontmatter\n"))

	if _, err := catalog.Load(root, ""); err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
}

func TestLoad_BrokenWorkflow_FailsNamingFile(t *testing.T) {
	root := makeTempMosaicRoot(t)
	p := writeSource(t, root, "Catalog/Workflows/Cat/flow.md", []byte("\u200b---\nname: w\n---\n"))

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
}

func TestLoad_WorkflowWithoutFrontmatter_LoadsWithoutError(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeSource(t, root, "Catalog/Workflows/Cat/flow.md", []byte("# Flow without frontmatter\n"))

	if _, err := catalog.Load(root, ""); err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
}
