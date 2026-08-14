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
// T1.1 — Model ID list migration (all three affected harnesses)
// ─────────────────────────────────────────────────────────────────

// TestDescriptorConsolidation_ModelIDs verifies two post-migration invariants for each
// affected embedded descriptor: no duplicate model IDs appear in Models.IDs, and no
// stale embedded-only model ID survives.
func TestDescriptorConsolidation_ModelIDs(t *testing.T) {
	cases := []struct {
		harness  string
		dir      string
		file     string
		staleIDs []string // IDs present in the old embedded copy but not the public copy
	}{
		{
			harness:  "claude-code",
			dir:      "claudecode",
			file:     "claude-code.yaml",
			staleIDs: []string{"claude-haiku-4-6"},
		},
		{
			harness:  "ghcp-cli",
			dir:      "ghcpcli",
			file:     "ghcp-cli.yaml",
			staleIDs: []string{"claude-haiku-4-6", "gpt-5", "gpt-5-mini"},
		},
		{
			harness:  "vscode-ghcp",
			dir:      "vscodeghcp",
			file:     "vscode-ghcp.yaml",
			staleIDs: []string{"Claude Haiku 4.6", "GPT-5", "GPT-5 mini"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.harness, func(t *testing.T) {
			d := loadEmbeddedDescriptor(t, tc.dir, tc.file)

			// Assert each ID appears at most once.
			seen := make(map[string]int)
			for _, id := range d.Models.IDs {
				seen[id]++
			}
			for id, count := range seen {
				if count > 1 {
					t.Errorf("%s: model ID %q appears %d times in Models.IDs; each ID must appear exactly once",
						tc.harness, id, count)
				}
			}

			// Assert stale embedded-only IDs are absent.
			for _, stale := range tc.staleIDs {
				if containsStr(d.Models.IDs, stale) {
					t.Errorf("%s: stale embedded-only model ID %q is still present; "+
						"it must not survive the migration to the public list",
						tc.harness, stale)
				}
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────
// T1.2 — orchestrator_injections_version preservation
// ─────────────────────────────────────────────────────────────────

// TestDescriptorConsolidation_MosaicOrchestratorInjectionsVersionInKeyOrder verifies that
// "mosaic_orchestrator_injections_version" is present in each affected embedded descriptor's
// frontmatter key_order after the Stage 4 mosaic_ prefix migration.
//
// The embedded descriptors are the authoritative copies. Both the model list migration
// (Stage 1) and the prefix rename (Stage 4/I4.4) update the key_order entries. This test
// guards that the renamed entry survives both migrations.
//
// RED: FAILS until I4.4 renames the key_order entry from "orchestrator_injections_version"
// to "mosaic_orchestrator_injections_version" in all three embedded descriptor YAML files.
func TestDescriptorConsolidation_MosaicOrchestratorInjectionsVersionInKeyOrder(t *testing.T) {
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
			if !containsStr(d.Frontmatter.KeyOrder, "mosaic_orchestrator_injections_version") {
				t.Errorf("%s: Frontmatter.KeyOrder does not contain \"mosaic_orchestrator_injections_version\"; "+
					"the prefixed name must be present after Stage 4 descriptor update\n  KeyOrder: %v",
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
