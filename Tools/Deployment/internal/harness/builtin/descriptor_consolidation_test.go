package builtin_test

// Tests for the descriptor consolidation invariants.
//
// These tests specify the post-migration state of the three affected embedded
// descriptors (claude-code, ghcp-cli, vscode-ghcp). They are written in the TDD
// RED phase: T1.1, T1.3, and T1.4 fail until the migration is applied; T1.2 already
// passes because the invariant it guards already holds, and its purpose is to catch
// an accidental regression during I1.1.
//
// Coverage:
//
//   Model list migration (three harnesses):
//   - Each embedded descriptor's Models.IDs matches the public copy's model list,
//     deduplicated with first-occurrence winning, in public ordering.
//   - No stale embedded-only model ID survives in any harness.
//
//   Key order preservation (three harnesses):
//   - orchestrator_injections_version remains in FrontmatterSpec.KeyOrder for every
//     affected embedded descriptor after migration. The embedded copies carry it; the
//     public copies do not. A wholesale file copy during migration would drop it.
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

// TestDescriptorConsolidation_ModelIDs verifies that each affected embedded descriptor
// exposes the migrated model ID list from the corresponding public copy. The public
// copy's list is taken verbatim except that duplicate IDs are removed by first-
// occurrence winning. Stale IDs that existed only in the embedded copy must not appear.
//
// These tests fail until I1.1 is applied.
func TestDescriptorConsolidation_ModelIDs(t *testing.T) {
	cases := []struct {
		harness  string
		dir      string
		file     string
		wantIDs  []string
		staleIDs []string // IDs present in the old embedded copy but not the public copy
	}{
		{
			harness: "claude-code",
			dir:     "claudecode",
			file:    "claude-code.yaml",
			// The public copy lists claude-opus-4-6 twice (first and last entry).
			// Deduplication keeps the first occurrence and drops the duplicate.
			wantIDs: []string{
				"claude-opus-4-6",
				"claude-sonnet-4-6",
				"claude-haiku-4-5",
				"claude-fable-5",
				"claude-opus-5",
				"claude-sonnet-5",
				"claude-sonnet-4-5",
				"claude-opus-4-8",
				"claude-opus-4-7",
			},
			// claude-haiku-4-6 appears only in the embedded copy; public uses claude-haiku-4-5.
			staleIDs: []string{"claude-haiku-4-6"},
		},
		{
			harness: "ghcp-cli",
			dir:     "ghcpcli",
			file:    "ghcp-cli.yaml",
			// Public copy has no duplicates; all 22 IDs are taken as-is.
			wantIDs: []string{
				"claude-sonnet-4-6",
				"claude-sonnet-4-5",
				"claude-opus-4-6",
				"claude-opus-4-7",
				"claude-opus-4-8",
				"claude-opus-4-8-fast",
				"claude-opus-5",
				"claude-haiku-4-5",
				"claude-fable-5",
				"gpt-5.4",
				"gpt-5.4-mini",
				"gpt-5.4-nano",
				"gpt-5.3-codex",
				"gpt-5.2-codex",
				"gpt-4.1",
				"gemini-3.5-flash",
				"gemini-3.6-flash",
				"kimi-k2.7-code",
				"gpt-5.6-sol",
				"gpt-5.6-terra",
				"gpt-5.6-luna",
				"mai-code-1-flash",
			},
			// The old embedded list had these IDs that the public copy replaced.
			staleIDs: []string{"claude-haiku-4-6", "gpt-5", "gpt-5-mini"},
		},
		{
			harness: "vscode-ghcp",
			dir:     "vscodeghcp",
			file:    "vscode-ghcp.yaml",
			// Public copy has no duplicates; display-name format, 22 IDs.
			wantIDs: []string{
				"Claude Sonnet 4.6",
				"Claude Sonnet 4.5",
				"Claude Opus 4.6",
				"Claude Opus 4.7",
				"Claude Opus 4.8",
				"Claude Opus 4.8 Fast",
				"Claude Opus 5",
				"Claude Haiku 4.5",
				"Claude Fable 5",
				"Gpt 5.4",
				"Gpt 5.4 Mini",
				"Gpt 5.4 Nano",
				"Gpt 5.3 Codex",
				"Gpt 5.2 Codex",
				"Gpt 4.1",
				"Gemini 3.5 Flash",
				"Gemini 3.6 Flash",
				"Kimi K2.7 Code",
				"Gpt 5.6 Sol",
				"Gpt 5.6 Terra",
				"Gpt 5.6 Luna",
				"Mai Code 1 Flash",
			},
			// The old embedded list had these display-name IDs that the public copy replaced.
			staleIDs: []string{"Claude Haiku 4.6", "GPT-5", "GPT-5 mini"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.harness, func(t *testing.T) {
			d := loadEmbeddedDescriptor(t, tc.dir, tc.file)

			// Assert the full ID list matches the expected (public copy, deduplicated).
			if !slicesEqual(d.Models.IDs, tc.wantIDs) {
				t.Errorf("%s: Models.IDs does not match the migrated list\n  got:  %v\n  want: %v",
					tc.harness, d.Models.IDs, tc.wantIDs)
			}

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

// TestDescriptorConsolidation_OrchestratorInjectionsVersionInKeyOrder verifies that
// orchestrator_injections_version is present in each affected embedded descriptor's
// frontmatter key_order.
//
// All three embedded descriptors already carry this entry; the public copies do not.
// A wholesale copy of a public file into the embedded slot would silently drop it.
// These tests pass in the RED phase and continue passing after a correct migration.
func TestDescriptorConsolidation_OrchestratorInjectionsVersionInKeyOrder(t *testing.T) {
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
			if !containsStr(d.Frontmatter.KeyOrder, "orchestrator_injections_version") {
				t.Errorf("%s: Frontmatter.KeyOrder does not contain \"orchestrator_injections_version\"; "+
					"this entry must survive the model list migration\n  KeyOrder: %v",
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

	wantHarnessTools := []string{"Task", "TaskStop"}
	if !slicesEqual(subagentMapping.HarnessTools, wantHarnessTools) {
		t.Errorf("claude-code.yaml: subagent mapping HarnessTools = %v, want %v",
			subagentMapping.HarnessTools, wantHarnessTools)
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
