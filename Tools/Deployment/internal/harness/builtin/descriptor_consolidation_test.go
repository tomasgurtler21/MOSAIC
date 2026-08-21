package builtin_test

// Tests for the descriptor consolidation invariants.
//
// These tests verify post-migration invariants for the three affected embedded
// descriptors (claude-code, ghcp-cli, vscode-ghcp).
//
// Coverage:
//
//   Model list invariants (three harnesses):
//   - No duplicate model ID appears in any harness's Models.IDs list.
//   - No stale embedded-only model ID survives in any harness.
//
//   Key order preservation (three harnesses):
//   - mosaic_orchestrator_injections_version is present in FrontmatterSpec.KeyOrder for every
//     affected embedded descriptor after Stage 4 migration. The field must survive both the
//     model list migration (Stage 1) and the mosaic_ prefix rename (Stage 4).
//
//   Claude Code non-model changes:
//   - The "subagent" mapping resolves to ["Task", "TaskStop"] (both names, in order).
//   - TaskStop is declared in the tool universe immediately after Task, with
//     Unused: deny and ByConvention: false.
//   - TaskStop is declared in placeholder_expansion immediately after Task.
//
//   Source-of-truth cleanup:
//   - The Tools/Deployment/descriptors/ directory no longer exists so duplicate
//     descriptor files cannot silently diverge again.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

// loadEmbeddedDescriptor reads and parses the embedded descriptor YAML for the named
// builtin harness. dir is the subdirectory under internal/harness/builtin/; file is
// the YAML filename inside it.
func loadEmbeddedDescriptor(t *testing.T, dir, file string) *domain.HarnessDescriptor {
	t.Helper()
	root := repoRoot(t) // declared in noinjections_test.go, same package
	path := filepath.Join(root, "Tools", "Deployment", "internal", "harness", "builtin", dir, file)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read embedded descriptor %s: %v", path, err)
	}
	d, err := descriptor.Parse(src, path)
	if err != nil {
		t.Fatalf("parse embedded descriptor %s: %v", path, err)
	}
	return d
}

// containsStr reports whether slice contains s.
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// slicesEqual reports whether a and b contain the same elements in the same order.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// indexInUniverse returns the index of name in the universe slice, or -1.
func indexInUniverse(universe []domain.HarnessTool, name string) int {
	for i, u := range universe {
		if u.Name == name {
			return i
		}
	}
	return -1
}

// indexInSlice returns the index of name in a string slice, or -1.
func indexInSlice(slice []string, name string) int {
	for i, s := range slice {
		if s == name {
			return i
		}
	}
	return -1
}

// ─────────────────────────────────────────────────────────────────
// T1.1 — Model ID list migration (vscode-ghcp) and retirement (CLI harnesses)
// ─────────────────────────────────────────────────────────────────

// TestDescriptorConsolidation_VsCodeGHCP_ModelIDs verifies the post-migration invariants
// for vscode-ghcp's embedded descriptor: no duplicate model IDs, and no stale IDs that
// appeared only in the old public copy. vscode-ghcp is not a CLI-backed harness and keeps
// its own YAML-sourced model catalog unchanged.
func TestDescriptorConsolidation_VsCodeGHCP_ModelIDs(t *testing.T) {
	d := loadEmbeddedDescriptor(t, "vscodeghcp", "vscode-ghcp.yaml")

	// Assert each ID appears at most once.
	seen := make(map[string]int)
	for _, id := range d.Models.IDs {
		seen[id]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("vscode-ghcp: model ID %q appears %d times in Models.IDs; each ID must appear exactly once",
				id, count)
		}
	}

	// Assert stale embedded-only IDs are absent.
	staleIDs := []string{"Claude Haiku 4.6", "GPT-5", "GPT-5 mini"}
	for _, stale := range staleIDs {
		if containsStr(d.Models.IDs, stale) {
			t.Errorf("vscode-ghcp: stale embedded-only model ID %q is still present; "+
				"it must not survive the migration to the public list",
				stale)
		}
	}
}

// TestDescriptorConsolidation_CLIHarnesses_ModelsBlockRetired verifies that the three
// CLI-backed builtin descriptors (claude-code, opencode, ghcp-cli) no longer declare a
// models: block in their YAML. Model data for these harnesses is now sourced exclusively
// from the shared catalog via the registry ModelCatalog hook. An accidentally re-added
// models: block would create a silent disagreement between the YAML and the shared catalog.
func TestDescriptorConsolidation_CLIHarnesses_ModelsBlockRetired(t *testing.T) {
	cases := []struct {
		harness string
		dir     string
		file    string
	}{
		{harness: "claude-code", dir: "claudecode", file: "claude-code.yaml"},
		{harness: "opencode", dir: "opencode", file: "opencode.yaml"},
		{harness: "ghcp-cli", dir: "ghcpcli", file: "ghcp-cli.yaml"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.harness, func(t *testing.T) {
			d := loadEmbeddedDescriptor(t, tc.dir, tc.file)
			if len(d.Models.IDs) != 0 {
				t.Errorf("%s: descriptor Models.IDs is non-empty (%v); "+
					"the models: block must be retired from CLI-backed harness YAML descriptors; "+
					"model data for this harness now comes exclusively from the shared catalog",
					tc.harness, d.Models.IDs)
			}
			if d.Models.FormatHint != "" {
				t.Errorf("%s: descriptor Models.FormatHint = %q; "+
					"the models: block must be retired from CLI-backed harness YAML descriptors",
					tc.harness, d.Models.FormatHint)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────
// T1.2 — orchestrator_injections_version preservation
// ─────────────────────────────────────────────────────────────────

// TestDescriptorConsolidation_MosaicOrchestratorInjectionsVersionAbsentFromKeyOrder verifies
// that "mosaic_orchestrator_injections_version" is NOT present in each affected embedded
// descriptor's frontmatter key_order. Stage 2 relocated this field from frontmatter to
// InjectionHarness-class region tag attributes, so it no longer needs a position in key_order.
func TestDescriptorConsolidation_MosaicOrchestratorInjectionsVersionAbsentFromKeyOrder(t *testing.T) {
	cases := []struct {
		harness string
		dir     string
		file    string
	}{
		{harness: "claude-code", dir: "claudecode", file: "claude-code.yaml"},
		{harness: "ghcp-cli", dir: "ghcpcli", file: "ghcp-cli.yaml"},
		{harness: "vscode-ghcp", dir: "vscodeghcp", file: "vscode-ghcp.yaml"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.harness, func(t *testing.T) {
			d := loadEmbeddedDescriptor(t, tc.dir, tc.file)
			if containsStr(d.Frontmatter.KeyOrder, "mosaic_orchestrator_injections_version") {
				t.Errorf("%s: Frontmatter.KeyOrder contains \"mosaic_orchestrator_injections_version\"; "+
					"Stage 2 relocated this version to region tag attributes — it must not appear in key_order\n  KeyOrder: %v",
					tc.harness, d.Frontmatter.KeyOrder)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────
// T1.3 — Claude Code non-model changes (subagent mapping, TaskStop)
// ─────────────────────────────────────────────────────────────────

// TestDescriptorConsolidation_ClaudeCode_SubagentMapsToTaskAndTaskStop verifies that
// the Claude Code embedded descriptor maps the "subagent" generic tool to both Task
// and TaskStop, in that order. The public copy already declares this; the embedded copy
// currently maps only to Task. This test fails until I1.2 is applied.
func TestDescriptorConsolidation_ClaudeCode_SubagentMapsToTaskAndTaskStop(t *testing.T) {
	d := loadEmbeddedDescriptor(t, "claudecode", "claude-code.yaml")

	var subagentMapping *domain.ToolMapping
	for i, m := range d.Tools.Mappings {
		if m.Generic == "subagent" {
			subagentMapping = &d.Tools.Mappings[i]
			break
		}
	}
	if subagentMapping == nil {
		t.Fatal("claude-code.yaml: no mapping found for generic tool \"subagent\"")
	}

	// The subagent mapping must have a single DestMain destination carrying Task and TaskStop.
	wantNames := []string{"Task", "TaskStop"}
	var mainDest *domain.ToolDestination
	for i := range subagentMapping.Destinations {
		if subagentMapping.Destinations[i].Kind == domain.DestMain {
			mainDest = &subagentMapping.Destinations[i]
			break
		}
	}
	if mainDest == nil {
		t.Errorf("claude-code.yaml: subagent mapping has no DestMain destination; Destinations = %v", subagentMapping.Destinations)
	} else if !slicesEqual(mainDest.Names, wantNames) {
		t.Errorf("claude-code.yaml: subagent DestMain destination Names = %v, want %v",
			mainDest.Names, wantNames)
	}
}

// TestDescriptorConsolidation_ClaudeCode_TaskStopInUniverse verifies that TaskStop
// appears in the Claude Code tool universe immediately after Task, with Unused: deny
// and ByConvention: false. Declaring TaskStop in the universe with this exact position
// and shape makes emission order deterministic and keeps TaskStop adjacent to Task in
// all rendered tool lists. This test fails until I1.2 is applied.
func TestDescriptorConsolidation_ClaudeCode_TaskStopInUniverse(t *testing.T) {
	d := loadEmbeddedDescriptor(t, "claudecode", "claude-code.yaml")

	taskIdx := indexInUniverse(d.Tools.Universe, "Task")
	if taskIdx < 0 {
		t.Fatal("claude-code.yaml: \"Task\" not found in tool universe")
	}

	taskStopIdx := indexInUniverse(d.Tools.Universe, "TaskStop")
	if taskStopIdx < 0 {
		t.Fatal("claude-code.yaml: \"TaskStop\" not found in tool universe; " +
			"it must be added immediately after Task")
	}

	if taskStopIdx != taskIdx+1 {
		t.Errorf("claude-code.yaml: TaskStop is at universe index %d; want %d (immediately after Task at index %d)",
			taskStopIdx, taskIdx+1, taskIdx)
	}

	entry := d.Tools.Universe[taskStopIdx]
	if entry.Unused != domain.Deny {
		t.Errorf("claude-code.yaml: Universe[\"TaskStop\"].Unused = %q, want %q",
			entry.Unused, domain.Deny)
	}
	if entry.ByConvention {
		t.Errorf("claude-code.yaml: Universe[\"TaskStop\"].ByConvention = true, want false")
	}
}

// TestDescriptorConsolidation_ClaudeCode_TaskStopInPlaceholderExpansion verifies that
// TaskStop appears in the Claude Code placeholder_expansion immediately after Task. When
// an orchestrator uses the {tool-permissions} placeholder, the expansion must include
// TaskStop so orchestrator agents receive it. This test fails until I1.2 is applied.
func TestDescriptorConsolidation_ClaudeCode_TaskStopInPlaceholderExpansion(t *testing.T) {
	d := loadEmbeddedDescriptor(t, "claudecode", "claude-code.yaml")

	taskIdx := indexInSlice(d.Tools.PlaceholderExpansion, "Task")
	if taskIdx < 0 {
		t.Fatal("claude-code.yaml: \"Task\" not found in placeholder_expansion")
	}

	taskStopIdx := indexInSlice(d.Tools.PlaceholderExpansion, "TaskStop")
	if taskStopIdx < 0 {
		t.Fatal("claude-code.yaml: \"TaskStop\" not found in placeholder_expansion; " +
			"it must be added immediately after Task so orchestrator agents receive it")
	}

	if taskStopIdx != taskIdx+1 {
		t.Errorf("claude-code.yaml: TaskStop is at placeholder_expansion index %d; "+
			"want %d (immediately after Task at index %d)",
			taskStopIdx, taskIdx+1, taskIdx)
	}
}

// ─────────────────────────────────────────────────────────────────
// T1.4 — No descriptor duplicates under Tools/Deployment/descriptors/
// ─────────────────────────────────────────────────────────────────

// TestDescriptorConsolidation_NoDescriptorsDirectory verifies that the
// Tools/Deployment/descriptors/ directory does not exist. After migration the embedded
// descriptors are the single source of truth; the public copy directory must be deleted
// so the two cannot silently diverge again.
//
// This test fails until I1.3 is applied.
func TestDescriptorConsolidation_NoDescriptorsDirectory(t *testing.T) {
	root := repoRoot(t)
	descriptorsDir := filepath.Join(root, "Tools", "Deployment", "descriptors")

	info, err := os.Stat(descriptorsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory is gone — correct post-migration state.
			return
		}
		t.Fatalf("stat Tools/Deployment/descriptors/: %v", err)
	}

	if !info.IsDir() {
		// Path exists but is not a directory; the invariant is satisfied.
		return
	}

	// Directory still exists. Fail if it contains any YAML descriptor files.
	entries, err := os.ReadDir(descriptorsDir)
	if err != nil {
		t.Fatalf("read Tools/Deployment/descriptors/: %v", err)
	}

	var yamlFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
			yamlFiles = append(yamlFiles, e.Name())
		}
	}

	if len(yamlFiles) > 0 {
		t.Errorf("Tools/Deployment/descriptors/ still exists and contains %d YAML file(s): %v\n"+
			"The directory must be deleted after migration so the embedded descriptors are the single source of truth.",
			len(yamlFiles), yamlFiles)
	}
}
