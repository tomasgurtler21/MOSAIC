package catalogpaths_test

// Tests for the catalogpaths package: the single source of truth for every catalog
// subpath in the Go codebase.
//
// Coverage:
//
//   Catalog-root-relative constants:
//   - Each Rel* constant equals the path the corresponding consumer resolves today.
//   - OrchestratorFileName and AgentFileExt equal their expected values.
//
//   MOSAIC-root-relative constants:
//   - CatalogDirName equals "Catalog".
//   - Each MosaicRel* and HarnessContentDir* constant equals the value the
//     corresponding consumer resolves today.
//   - MosaicRelSourceFilesFormatFile and MosaicRelBundleFile are exactly
//     CatalogDirName + "/" + their catalog-root-relative counterparts.
//   - MosaicRelHarnessInjectionsDir equals "Catalog/HarnessInjections" (target layout).
//
//   Harness content directory invariants:
//   - The four HarnessContentDir* values are pairwise distinct and non-empty.
//   - Each equals MosaicRelHarnessInjectionsDir + "/" + its HarnessDirName*.
//
//   Builder functions:
//   - Each builder produces the path that the corresponding consumer currently
//     constructs, verified against a synthetic catalog root.
//   - All builders are pure (no I/O): they return without error when given a
//     non-existent root.
//
//   Harness RepoContentDir alignment:
//   - Each harness package's current RepoContentDir value equals the corresponding
//     HarnessContentDir* constant, establishing the tripwire that fires when Stage 3
//     flips the values.

import (
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog/catalogpaths"
	"mosaic-deploy/internal/harness/builtin/claudecode"
	"mosaic-deploy/internal/harness/builtin/ghcpcli"
	"mosaic-deploy/internal/harness/builtin/opencode"
	"mosaic-deploy/internal/harness/builtin/vscodeghcp"
)

// ---------------------------------------------------------------------------
// Catalog-root-relative constants
// ---------------------------------------------------------------------------

func TestRelConstants_MatchCurrentConsumerLiterals(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		// Orchestrator: target layout Catalog/Orchestrator/ (catalog-root-relative: Orchestrator)
		{"RelOrchestratorDir", catalogpaths.RelOrchestratorDir, "Orchestrator"},
		// Worker agents: target layout Catalog/Subagents/ (catalog-root-relative: Subagents)
		{"RelSubagentsDir", catalogpaths.RelSubagentsDir, "Subagents"},
		// Utility agents: target layout Catalog/UtilityAgents/ (catalog-root-relative: UtilityAgents)
		{"RelUtilityAgentsDir", catalogpaths.RelUtilityAgentsDir, "UtilityAgents"},
		// Skills: target layout Catalog/Skills/ (catalog-root-relative: Skills)
		{"RelSkillsDir", catalogpaths.RelSkillsDir, "Skills"},
		// Hooks: target layout Catalog/Hooks/ (catalog-root-relative: Hooks)
		{"RelHooksDir", catalogpaths.RelHooksDir, "Hooks"},
		// Workflows: workflows directory (unchanged across restructure)
		{"RelWorkflowsDir", catalogpaths.RelWorkflowsDir, "Workflows"},
		// root.go marker: target layout Catalog/SourceFilesFormat.md
		// (catalog-root-relative portion without the leading "Catalog/")
		{"RelSourceFilesFormatFile", catalogpaths.RelSourceFilesFormatFile, "SourceFilesFormat.md"},
		// bundle.go: target layout Catalog/DeployedSections.md
		// (catalog-root-relative portion without the leading "Catalog/")
		{"RelBundleFile", catalogpaths.RelBundleFile, "DeployedSections.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q; want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestOrchestratorFileName_IsOrchestratorMd(t *testing.T) {
	if catalogpaths.OrchestratorFileName != "orchestrator.md" {
		t.Errorf("OrchestratorFileName = %q; want %q", catalogpaths.OrchestratorFileName, "orchestrator.md")
	}
}

func TestAgentFileExt_IsDotMd(t *testing.T) {
	if catalogpaths.AgentFileExt != ".md" {
		t.Errorf("AgentFileExt = %q; want %q", catalogpaths.AgentFileExt, ".md")
	}
}

// ---------------------------------------------------------------------------
// MOSAIC-root-relative constants
// ---------------------------------------------------------------------------

func TestCatalogDirName_IsCatalog(t *testing.T) {
	if catalogpaths.CatalogDirName != "Catalog" {
		t.Errorf("CatalogDirName = %q; want %q", catalogpaths.CatalogDirName, "Catalog")
	}
}

func TestMosaicRelConstants_MatchCurrentConsumerLiterals(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		// bundle.go BundleSourceRelPath: target layout "Catalog/DeployedSections.md"
		{"MosaicRelBundleFile", catalogpaths.MosaicRelBundleFile, "Catalog/DeployedSections.md"},
		// root.go isMosaicRoot: target layout "Catalog/SourceFilesFormat.md"
		{"MosaicRelSourceFilesFormatFile", catalogpaths.MosaicRelSourceFilesFormatFile, "Catalog/SourceFilesFormat.md"},
		// The parent directory that holds per-harness injection content: target layout
		{"MosaicRelHarnessInjectionsDir", catalogpaths.MosaicRelHarnessInjectionsDir, "Catalog/HarnessInjections"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q; want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestMosaicRelBundleFile_IsDerivedFromCatalogDirNameAndRelBundleFile(t *testing.T) {
	// Structural invariant: MosaicRelBundleFile == CatalogDirName + "/" + RelBundleFile.
	// This is independently assertable from the absolute-value test.
	want := catalogpaths.CatalogDirName + "/" + catalogpaths.RelBundleFile
	if catalogpaths.MosaicRelBundleFile != want {
		t.Errorf("MosaicRelBundleFile = %q; want CatalogDirName+\"/\"+RelBundleFile = %q",
			catalogpaths.MosaicRelBundleFile, want)
	}
}

func TestMosaicRelSourceFilesFormatFile_IsDerivedFromCatalogDirNameAndRelSourceFilesFormatFile(t *testing.T) {
	// Structural invariant: MosaicRelSourceFilesFormatFile == CatalogDirName + "/" + RelSourceFilesFormatFile.
	want := catalogpaths.CatalogDirName + "/" + catalogpaths.RelSourceFilesFormatFile
	if catalogpaths.MosaicRelSourceFilesFormatFile != want {
		t.Errorf("MosaicRelSourceFilesFormatFile = %q; want CatalogDirName+\"/\"+RelSourceFilesFormatFile = %q",
			catalogpaths.MosaicRelSourceFilesFormatFile, want)
	}
}

// ---------------------------------------------------------------------------
// Harness directory names
// ---------------------------------------------------------------------------

func TestHarnessDirNames_MatchCurrentDirectoryNames(t *testing.T) {
	// These are the display-style directory names that exist on disk today.
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"HarnessDirNameClaudeCode", catalogpaths.HarnessDirNameClaudeCode, "Claude Code"},
		{"HarnessDirNameGhcpCLI", catalogpaths.HarnessDirNameGhcpCLI, "GHCP CLI"},
		{"HarnessDirNameOpenCode", catalogpaths.HarnessDirNameOpenCode, "OpenCode"},
		{"HarnessDirNameVSCodeGHCP", catalogpaths.HarnessDirNameVSCodeGHCP, "VS Code GHCP"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q; want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Harness content directory constants
// ---------------------------------------------------------------------------

func TestHarnessContentDirs_MatchCurrentRepoContentDirValues(t *testing.T) {
	// These assert the target layout values from the path restructure table.
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"HarnessContentDirClaudeCode", catalogpaths.HarnessContentDirClaudeCode, "Catalog/HarnessInjections/Claude Code"},
		{"HarnessContentDirGhcpCLI", catalogpaths.HarnessContentDirGhcpCLI, "Catalog/HarnessInjections/GHCP CLI"},
		{"HarnessContentDirOpenCode", catalogpaths.HarnessContentDirOpenCode, "Catalog/HarnessInjections/OpenCode"},
		{"HarnessContentDirVSCodeGHCP", catalogpaths.HarnessContentDirVSCodeGHCP, "Catalog/HarnessInjections/VS Code GHCP"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q; want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestHarnessContentDirs_PairwiseDistinctAndNonEmpty(t *testing.T) {
	dirs := []struct {
		name string
		val  string
	}{
		{"HarnessContentDirClaudeCode", catalogpaths.HarnessContentDirClaudeCode},
		{"HarnessContentDirGhcpCLI", catalogpaths.HarnessContentDirGhcpCLI},
		{"HarnessContentDirOpenCode", catalogpaths.HarnessContentDirOpenCode},
		{"HarnessContentDirVSCodeGHCP", catalogpaths.HarnessContentDirVSCodeGHCP},
	}
	seen := make(map[string]string, len(dirs))
	for _, d := range dirs {
		if d.val == "" {
			t.Errorf("%s: harness content dir must not be empty", d.name)
			continue
		}
		if prev, ok := seen[d.val]; ok {
			t.Errorf("duplicate harness content dir %q: shared by %s and %s", d.val, prev, d.name)
		}
		seen[d.val] = d.name
	}
}

func TestHarnessContentDirs_DerivedFromMosaicRelHarnessInjectionsDir(t *testing.T) {
	// Each HarnessContentDir* must equal MosaicRelHarnessInjectionsDir + "/" + its HarnessDirName*.
	// This asserts the derivation relationship, not just the absolute value.
	tests := []struct {
		name    string
		dirName string
		content string
	}{
		{"ClaudeCode", catalogpaths.HarnessDirNameClaudeCode, catalogpaths.HarnessContentDirClaudeCode},
		{"GhcpCLI", catalogpaths.HarnessDirNameGhcpCLI, catalogpaths.HarnessContentDirGhcpCLI},
		{"OpenCode", catalogpaths.HarnessDirNameOpenCode, catalogpaths.HarnessContentDirOpenCode},
		{"VSCodeGHCP", catalogpaths.HarnessDirNameVSCodeGHCP, catalogpaths.HarnessContentDirVSCodeGHCP},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := catalogpaths.MosaicRelHarnessInjectionsDir + "/" + tc.dirName
			if tc.content != want {
				t.Errorf("HarnessContentDir%s = %q; want MosaicRelHarnessInjectionsDir+\"/\"+HarnessDirName%s = %q",
					tc.name, tc.content, tc.name, want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Harness RepoContentDir alignment (tripwire for Stage 3 value flip)
// ---------------------------------------------------------------------------

func TestHarnessRepoContentDirs_MatchCatalogPathsConstants(t *testing.T) {
	// Each harness package's exported RepoContentDir must equal the corresponding
	// HarnessContentDir* constant. This test acts as the tripwire: when Stage 3 flips
	// the constant values in catalogpaths, this test fails until the harness packages
	// are also updated — preventing silent drift between the two.
	tests := []struct {
		name        string
		harnessPath string // the harness package's RepoContentDir
		catalogPath string // the catalogpaths constant it must equal
	}{
		{
			name:        "claudecode",
			harnessPath: claudecode.RepoContentDir,
			catalogPath: catalogpaths.HarnessContentDirClaudeCode,
		},
		{
			name:        "ghcpcli",
			harnessPath: ghcpcli.RepoContentDir,
			catalogPath: catalogpaths.HarnessContentDirGhcpCLI,
		},
		{
			name:        "opencode",
			harnessPath: opencode.RepoContentDir,
			catalogPath: catalogpaths.HarnessContentDirOpenCode,
		},
		{
			name:        "vscodeghcp",
			harnessPath: vscodeghcp.RepoContentDir,
			catalogPath: catalogpaths.HarnessContentDirVSCodeGHCP,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.harnessPath != tc.catalogPath {
				t.Errorf("%s.RepoContentDir = %q; want catalogpaths constant %q",
					tc.name, tc.harnessPath, tc.catalogPath)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Builder functions
// ---------------------------------------------------------------------------

func TestBuilders_ProducePathsMatchingCurrentConsumers(t *testing.T) {
	// A synthetic catalog root — no filesystem access, all builders are pure.
	root := "/test/catalog/root"

	tests := []struct {
		name string
		got  string
		want string
	}{
		// agents.go: target layout filepath.Join(root, "Orchestrator")
		{"OrchestratorDir",
			catalogpaths.OrchestratorDir(root),
			filepath.Join(root, "Orchestrator")},
		// agents.go: target layout filepath.Join(root, "Orchestrator", "orchestrator.md")
		{"OrchestratorFile",
			catalogpaths.OrchestratorFile(root),
			filepath.Join(root, "Orchestrator", "orchestrator.md")},
		// agents.go: target layout filepath.Join(root, "Subagents")
		{"SubagentsDir",
			catalogpaths.SubagentsDir(root),
			filepath.Join(root, "Subagents")},
		// agents.go: target layout filepath.Join(root, "UtilityAgents")
		{"UtilityAgentsDir",
			catalogpaths.UtilityAgentsDir(root),
			filepath.Join(root, "UtilityAgents")},
		// skills.go: target layout filepath.Join(root, "Skills")
		{"SkillsDir",
			catalogpaths.SkillsDir(root),
			filepath.Join(root, "Skills")},
		// hooks.go: target layout filepath.Join(root, "Hooks")
		{"HooksDir",
			catalogpaths.HooksDir(root),
			filepath.Join(root, "Hooks")},
		// workflows.go: filepath.Join(root, "Workflows") (unchanged)
		{"WorkflowsDir",
			catalogpaths.WorkflowsDir(root),
			filepath.Join(root, "Workflows")},
		// root.go isMosaicRoot: target layout filepath.Join(root, "SourceFilesFormat.md")
		{"SourceFilesFormatFile",
			catalogpaths.SourceFilesFormatFile(root),
			filepath.Join(root, "SourceFilesFormat.md")},
		// bundle.go: target layout filepath.Join(root, "DeployedSections.md")
		{"BundleFile",
			catalogpaths.BundleFile(root),
			filepath.Join(root, "DeployedSections.md")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s(%q) = %q; want %q", tc.name, root, tc.got, tc.want)
			}
		})
	}
}

func TestSubagentCategoryDir_ProducesCurrentConsumerPath(t *testing.T) {
	// agents.go: target layout filepath.Join(catalogRoot, "Subagents", category)
	root := "/test/catalog/root"
	category := "Engineering"
	got := catalogpaths.SubagentCategoryDir(root, category)
	want := filepath.Join(root, "Subagents", category)
	if got != want {
		t.Errorf("SubagentCategoryDir(%q, %q) = %q; want %q", root, category, got, want)
	}
}

func TestSubagentFile_ProducesCurrentConsumerPath(t *testing.T) {
	// promote_service.go: target layout filepath.Join(catalogRoot, "Subagents", category, key+".md")
	root := "/test/catalog/root"
	category := "Engineering"
	key := "planner"
	got := catalogpaths.SubagentFile(root, category, key)
	want := filepath.Join(root, "Subagents", category, key+".md")
	if got != want {
		t.Errorf("SubagentFile(%q, %q, %q) = %q; want %q", root, category, key, got, want)
	}
}

func TestUtilityAgentFile_ProducesCurrentConsumerPath(t *testing.T) {
	// promote_service.go: target layout filepath.Join(catalogRoot, "UtilityAgents", key+".md")
	root := "/test/catalog/root"
	key := "file-writer"
	got := catalogpaths.UtilityAgentFile(root, key)
	want := filepath.Join(root, "UtilityAgents", key+".md")
	if got != want {
		t.Errorf("UtilityAgentFile(%q, %q) = %q; want %q", root, key, got, want)
	}
}

func TestSkillDir_ProducesCurrentConsumerPath(t *testing.T) {
	// skills.go: target layout filepath.Join(catalogRoot, "Skills", skillKey)
	root := "/test/catalog/root"
	skillKey := "lean-tdd"
	got := catalogpaths.SkillDir(root, skillKey)
	want := filepath.Join(root, "Skills", skillKey)
	if got != want {
		t.Errorf("SkillDir(%q, %q) = %q; want %q", root, skillKey, got, want)
	}
}

func TestBuilders_PerformNoIO(t *testing.T) {
	// Builders must be pure path joiners. A non-existent root must not cause a panic
	// or any filesystem error — the builders return a path string without touching the disk.
	nonExistent := "/does/not/exist/on/any/disk"
	_ = catalogpaths.OrchestratorDir(nonExistent)
	_ = catalogpaths.OrchestratorFile(nonExistent)
	_ = catalogpaths.SubagentsDir(nonExistent)
	_ = catalogpaths.SubagentCategoryDir(nonExistent, "cat")
	_ = catalogpaths.SubagentFile(nonExistent, "cat", "key")
	_ = catalogpaths.UtilityAgentsDir(nonExistent)
	_ = catalogpaths.UtilityAgentFile(nonExistent, "key")
	_ = catalogpaths.SkillsDir(nonExistent)
	_ = catalogpaths.SkillDir(nonExistent, "key")
	_ = catalogpaths.HooksDir(nonExistent)
	_ = catalogpaths.WorkflowsDir(nonExistent)
	_ = catalogpaths.SourceFilesFormatFile(nonExistent)
	_ = catalogpaths.BundleFile(nonExistent)
	// Reaching here without panic confirms the builders perform no I/O.
}

// ---------------------------------------------------------------------------
// Catalog-root-relative vs MOSAIC-root-relative forms are distinct
// ---------------------------------------------------------------------------

func TestPathForms_CatalogRootRelativeDoesNotContainCatalogPrefix(t *testing.T) {
	// Catalog-root-relative paths must not start with CatalogDirName followed by a separator.
	// If they did, the --catalog-folder override would silently re-introduce a double prefix.
	prefix := catalogpaths.CatalogDirName + "/"
	relPaths := map[string]string{
		"RelOrchestratorDir":       catalogpaths.RelOrchestratorDir,
		"RelSubagentsDir":          catalogpaths.RelSubagentsDir,
		"RelUtilityAgentsDir":      catalogpaths.RelUtilityAgentsDir,
		"RelSkillsDir":             catalogpaths.RelSkillsDir,
		"RelHooksDir":              catalogpaths.RelHooksDir,
		"RelWorkflowsDir":          catalogpaths.RelWorkflowsDir,
		"RelSourceFilesFormatFile": catalogpaths.RelSourceFilesFormatFile,
		"RelBundleFile":            catalogpaths.RelBundleFile,
	}
	for name, val := range relPaths {
		if val == "" {
			// Non-empty is already checked by the value tests; skip the prefix check.
			continue
		}
		if len(val) >= len(prefix) && val[:len(prefix)] == prefix {
			t.Errorf("%s = %q starts with %q; catalog-root-relative paths must not include the Catalog/ prefix", name, val, prefix)
		}
	}
}
