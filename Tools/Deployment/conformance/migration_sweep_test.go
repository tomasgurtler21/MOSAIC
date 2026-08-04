package conformance_test

// migration_sweep_test.go verifies that the four-batch Generic agent migration (Stages 8-11)
// produced a consistent result across the whole Agents/Generic/ tree, with no file left behind
// and no batch having applied the naming rules differently from the others.
//
// Three invariants are swept for every .md file under Agents/Generic/:
//
//   (1) No tool-managed name under [[INJECTION:]].
//       Tool-managed names (CommunicationProtocol, AvailableWorkflows, InfrastructureAgents,
//       LanguagePatterns, HarnessConstraints, CustomConstraints) are owned by the deployment
//       tool and must appear under [[DEPLOYED:]], never [[INJECTION:]].
//
//   (2) No user-owned name under [[DEPLOYED:]].
//       User-owned names (IdentityExtension, ArtifactProvenanceExtension, CodebaseContext,
//       OutputArtifactTemplate, SeverityThresholds, SeverityDefinitions, ErrorHandlingExtension,
//       ContextLimits) are owned by each project and must appear under [[INJECTION:]], never
//       [[DEPLOYED:]].
//
//   (3) No ProtocolExtension.
//       ProtocolExtension is removed from the vocabulary; its presence in any file is an error.
//
//   (4) No leftover hardcoded protocol section.
//       The old [[SECTION:CommunicationProtocol]] marker is replaced by
//       [[DEPLOYED:CommunicationProtocol]] in all migrated files. Any remaining
//       [[SECTION:CommunicationProtocol]] tag means a file was not migrated.
//
//   (5) Protocol boundary is empty in source files.
//       Source files must have an empty [[DEPLOYED:CommunicationProtocol]] boundary — the
//       deployment tool fills it at deploy time. Non-empty content means either (a) a deployed
//       file was committed as a source file, or (b) a pre-migration hardcoded block was
//       incompletely converted.

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Canonical vocabulary
// ---------------------------------------------------------------------------

// toolManagedNames are the deployment-tool-owned region names. They must appear under
// [[DEPLOYED:]] and must never appear under [[INJECTION:]] in agent source files.
var toolManagedNames = []string{
	"CommunicationProtocol",
	"AvailableWorkflows",
	"InfrastructureAgents",
	"LanguagePatterns",
	"HarnessConstraints",
	"CustomConstraints",
}

// userOwnedNames are the project-owner names. They must appear under [[INJECTION:]] and
// must never appear under [[DEPLOYED:]] in agent source files.
var userOwnedNames = []string{
	"IdentityExtension",
	"ArtifactProvenanceExtension",
	"CodebaseContext",
	"OutputArtifactTemplate",
	"SeverityThresholds",
	"SeverityDefinitions",
	"ErrorHandlingExtension",
	"ContextLimits",
}

// ---------------------------------------------------------------------------
// Corpus helper
// ---------------------------------------------------------------------------

// genericAgentPaths returns all .md file paths under Agents/Generic/ (excluding README files).
// The test is skipped if the directory is not found so the suite remains runnable in isolated
// checkouts that contain only Tools/.
func genericAgentPaths(t *testing.T) []string {
	t.Helper()
	repoRoot := findRepoRoot(t)
	genericDir := filepath.Join(repoRoot, "Agents", "Generic")

	if _, err := os.Stat(genericDir); err != nil {
		t.Skipf("Agents/Generic/ not found at %s: %v", genericDir, err)
	}

	var paths []string
	err := filepath.WalkDir(genericDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		if strings.EqualFold(filepath.Base(p), "readme.md") {
			return nil // README files are not agent definitions
		}
		if strings.EqualFold(filepath.Base(p), "source-format.md") {
			return nil // format documentation is not an agent definition
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		t.Fatalf("walk Agents/Generic/: %v", err)
	}
	if len(paths) == 0 {
		t.Skip("no .md files found under Agents/Generic/")
	}
	return paths
}

// ---------------------------------------------------------------------------
// (1) No tool-managed name under [[INJECTION:]]
// ---------------------------------------------------------------------------

// TestMigrationSweep_NoToolManagedNameUnderInjection verifies that none of the tool-managed
// region names appear under [[INJECTION:]] in any Agents/Generic/ file. Before migration,
// HarnessConstraints, LanguagePatterns, and others were declared as [[INJECTION:]] regions
// in generic agents; they must now all be [[DEPLOYED:]].
func TestMigrationSweep_NoToolManagedNameUnderInjection(t *testing.T) {
	paths := genericAgentPaths(t)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		rel := relPath(t, findRepoRoot(t), p)
		for _, name := range toolManagedNames {
			forbidden := []byte("[[INJECTION:" + name + "]]")
			if bytes.Contains(data, forbidden) {
				t.Errorf("%s: tool-managed name %q found under [[INJECTION:]]; "+
					"tool-managed names must use [[DEPLOYED:]] (migration Stage 8-11 incomplete for this file)",
					rel, name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// (2) No user-owned name under [[DEPLOYED:]]
// ---------------------------------------------------------------------------

// TestMigrationSweep_NoUserOwnedNameUnderDeployed verifies that none of the user-owned region
// names appear under [[DEPLOYED:]] in any Agents/Generic/ file. User-owned names are project
// customisation points that must be declared with [[INJECTION:]] so the update path preserves
// them byte-identically.
func TestMigrationSweep_NoUserOwnedNameUnderDeployed(t *testing.T) {
	paths := genericAgentPaths(t)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		rel := relPath(t, findRepoRoot(t), p)
		for _, name := range userOwnedNames {
			forbidden := []byte("[[DEPLOYED:" + name + "]]")
			if bytes.Contains(data, forbidden) {
				t.Errorf("%s: user-owned name %q found under [[DEPLOYED:]]; "+
					"user-owned names must use [[INJECTION:]] to be preserved across updates",
					rel, name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// (3) No ProtocolExtension
// ---------------------------------------------------------------------------

// TestMigrationSweep_NoProtocolExtension verifies that the removed ProtocolExtension name
// does not appear in any Agents/Generic/ file. ProtocolExtension was removed from the
// canonical vocabulary; its presence indicates an unmigrated or incorrectly migrated file.
func TestMigrationSweep_NoProtocolExtension(t *testing.T) {
	paths := genericAgentPaths(t)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		rel := relPath(t, findRepoRoot(t), p)
		if bytes.Contains(data, []byte("ProtocolExtension")) {
			t.Errorf("%s: removed name 'ProtocolExtension' found; "+
				"this name was eliminated from the canonical vocabulary and must not appear in any file",
				rel)
		}
	}
}

// ---------------------------------------------------------------------------
// (4) No leftover hardcoded protocol section
// ---------------------------------------------------------------------------

// TestMigrationSweep_NoLegacyProtocolSection verifies that the old [[SECTION:CommunicationProtocol]]
// marker does not appear in any Agents/Generic/ file. Before migration, agents hardcoded protocol
// prose inside a named section; after migration the section is replaced by the empty
// [[DEPLOYED:CommunicationProtocol]] boundary.
func TestMigrationSweep_NoLegacyProtocolSection(t *testing.T) {
	paths := genericAgentPaths(t)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		rel := relPath(t, findRepoRoot(t), p)
		if bytes.Contains(data, []byte("[[SECTION:CommunicationProtocol]]")) {
			t.Errorf("%s: legacy [[SECTION:CommunicationProtocol]] found; "+
				"pre-migration protocol sections must be replaced by the empty "+
				"[[DEPLOYED:CommunicationProtocol]] boundary (migration Stage 8-11 incomplete)",
				rel)
		}
	}
}

// ---------------------------------------------------------------------------
// (5) Protocol boundary is empty in source files
// ---------------------------------------------------------------------------

// TestMigrationSweep_ProtocolBoundaryIsEmptyInSources verifies that every
// [[DEPLOYED:CommunicationProtocol]] boundary in a source file contains no non-whitespace
// content. Source files must have an empty protocol boundary because the deployment tool fills
// it at deploy time. A non-empty source boundary means either a deployed artifact was committed
// as a source file or a pre-migration hardcoded block was incompletely converted.
func TestMigrationSweep_ProtocolBoundaryIsEmptyInSources(t *testing.T) {
	const openTag = "[[DEPLOYED:CommunicationProtocol]]"
	const closeTag = "[[/DEPLOYED:CommunicationProtocol]]"

	paths := genericAgentPaths(t)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		rel := relPath(t, findRepoRoot(t), p)

		openIdx := bytes.Index(data, []byte(openTag))
		if openIdx < 0 {
			continue // file has no protocol boundary — skip
		}

		afterOpen := data[openIdx+len(openTag):]
		closeIdx := bytes.Index(afterOpen, []byte(closeTag))
		if closeIdx < 0 {
			t.Errorf("%s: [[DEPLOYED:CommunicationProtocol]] is opened but never closed", rel)
			continue
		}

		between := afterOpen[:closeIdx]
		if len(bytes.TrimSpace(between)) > 0 {
			preview := between
			if len(preview) > 120 {
				preview = preview[:120]
			}
			t.Errorf("%s: [[DEPLOYED:CommunicationProtocol]] boundary has non-empty content in source file;\n"+
				"source files must have an empty protocol boundary (content between tags, first 120 bytes):\n%q",
				rel, preview)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// relPath returns p relative to base, falling back to p on error.
func relPath(t *testing.T, base, p string) string {
	t.Helper()
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return rel
}
