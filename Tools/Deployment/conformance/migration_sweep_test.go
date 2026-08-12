package conformance_test

// migration_sweep_test.go verifies that the four-batch Generic agent migration (Stages 8-11)
// produced a consistent result across the whole Agents/Generic/ tree, with no file left behind
// and no batch having applied the naming rules differently from the others.
//
// Invariants swept for every .md file under Agents/Generic/:
//
//   (1) No tool-managed name under [[INJECTION:]].
//       Tool-managed names (all nine CanonicalDeployed names) are owned by the deployment
//       tool and must appear under [[DEPLOYED:]], never [[INJECTION:]].
//
//   (2) No user-owned name under [[DEPLOYED:]].
//       Injection names are open as of Stage 2. Names that happen to be injection-only
//       (IdentityExtension, CodebaseContext, LanguagePatterns, OutputArtifactTemplate,
//       SeverityThresholds, SeverityDefinitions, ErrorHandlingExtension, ContextLimits)
//       must never appear under [[DEPLOYED:]] — they have no tool generator.
//       LanguagePatterns is a catalogued, project-authored injection name (parent
//       Capabilities) — it was never a tool-managed name and belongs here, not above.
//
//   (3) No [[INJECTION:ProtocolExtension]] in any file.
//       ProtocolExtension was briefly catalogued in the advisory InjectionParent map, then
//       removed again in Stage 2: projects should use [[CUSTOM:ProtocolExtension]] instead
//       of [[INJECTION:ProtocolExtension]]. No current agent file should declare the injection.
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

// toolManagedNames are the deployment-tool-owned region names (all CanonicalDeployed names).
// They must appear under [[DEPLOYED:]] and must never appear under [[INJECTION:]] in agent
// source files.
var toolManagedNames = []string{
	"CommunicationProtocol",
	"AuthorityHierarchy",
	"ClosingProcedure",
	"AvailableWorkflows",
	"InfrastructureAgents",
	"ProtocolConstraints",
	"HarnessConstraints",
	"ErrorHandlingCommon",
	"ExecutionPhilosophyCommon",
}

// userOwnedNames are the project-owner customisation names. They must never appear under
// [[DEPLOYED:]] in agent source files — they have no tool generator and must be preserved
// as-is via [[INJECTION:]]. LanguagePatterns is a catalogued, project-authored injection
// name (advisory parent Capabilities), not a tool-managed one.
var userOwnedNames = []string{
	"IdentityExtension",
	"CodebaseContext",
	"LanguagePatterns",
	"OutputArtifactTemplate",
	"SeverityThresholds",
	"SeverityDefinitions",
	"ErrorHandlingExtension",
	"ContextLimits",
}

// deprecatedInjectionNames are names that are migrating out of all agent files entirely.
// They must never appear under [[DEPLOYED:]] (no tool generator exists for them) and are
// expected to disappear from [[INJECTION:]] use as well as migration proceeds.
// CustomConstraints has no generator and is deleted outright — it is not re-catalogued
// as an advisory injection name, unlike LanguagePatterns above.
var deprecatedInjectionNames = []string{
	"ArtifactProvenanceExtension",
	"CustomConstraints",
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

// TestMigrationSweep_NoDeprecatedNameUnderDeployed verifies that deprecated injection names
// (those migrating out of files entirely) never appear under [[DEPLOYED:]] in any
// Agents/Generic/ file. These names have no tool generator and must not be tool-managed.
func TestMigrationSweep_NoDeprecatedNameUnderDeployed(t *testing.T) {
	paths := genericAgentPaths(t)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		rel := relPath(t, findRepoRoot(t), p)
		for _, name := range deprecatedInjectionNames {
			forbidden := []byte("[[DEPLOYED:" + name + "]]")
			if bytes.Contains(data, forbidden) {
				t.Errorf("%s: deprecated name %q found under [[DEPLOYED:]]; "+
					"deprecated names have no tool generator and must not be tool-managed",
					rel, name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// (3) No [[INJECTION:ProtocolExtension]]
// ---------------------------------------------------------------------------

// TestMigrationSweep_NoProtocolExtension verifies that no Agents/Generic/ file declares
// [[INJECTION:ProtocolExtension]]. ProtocolExtension is removed from the advisory
// InjectionParent map in Stage 2; projects use [[CUSTOM:ProtocolExtension]] instead.
// The assertion is retained as a safety net: no current agent file should declare this
// injection.
func TestMigrationSweep_NoProtocolExtension(t *testing.T) {
	paths := genericAgentPaths(t)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		rel := relPath(t, findRepoRoot(t), p)
		if bytes.Contains(data, []byte("[[INJECTION:ProtocolExtension]]")) {
			t.Errorf("%s: [[INJECTION:ProtocolExtension]] found; "+
				"no agent file should declare this injection before explicit stage authorisation",
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
// (Formerly numbered (4) before sweep (3) was restored above.)

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
// (6) No old marker filename string in source, test, or documentation files
// ---------------------------------------------------------------------------

// TestNoLegacySourceFormatReference verifies that no source, test, or documentation
// file under Tools/, Agents/, or Development/ contains the old marker filename string.
// The marker file was renamed to SourceFilesFormat.md; all references to the old name
// must be eliminated so that a future edit cannot silently reintroduce a stale reference.
// This test directly operationalises AC1.3 (no old marker string in source/test/docs).
//
// Exclusions:
//   - Files ending in .exe (build outputs / binary artifacts)
//   - Directories whose name begins with "Orchestration-" (prior orchestration and log folders)
//
// The forbidden string is constructed at runtime so that this source file does not
// itself contain the literal and falsely trigger the check.
func TestNoLegacySourceFormatReference(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Construct the forbidden string at runtime. Joining two parts avoids having the
	// literal contiguous in this source file, which would cause the test to flag itself.
	forbidden := []byte("SOURCE" + "-" + "FORMAT")

	treeDirs := []string{"Tools", "Agents", "Development"}

	for _, tree := range treeDirs {
		treePath := filepath.Join(repoRoot, tree)
		if _, err := os.Stat(treePath); err != nil {
			t.Logf("directory %s not found at %s, skipping: %v", tree, treePath, err)
			continue
		}

		err := filepath.WalkDir(treePath, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Skip prior orchestration and log folders.
				if strings.HasPrefix(d.Name(), "Orchestration-") {
					return filepath.SkipDir
				}
				return nil
			}
			// Skip binary build artifacts.
			if strings.HasSuffix(p, ".exe") {
				return nil
			}

			data, err := os.ReadFile(p)
			if err != nil {
				t.Errorf("read %s: %v", relPath(t, repoRoot, p), err)
				return nil
			}

			if bytes.Contains(data, forbidden) {
				t.Errorf("%s: contains old marker filename string %q; "+
					"rename all occurrences to SourceFilesFormat.md "+
					"(the old marker name must not appear in any source, test, or documentation file — AC1.3)",
					relPath(t, repoRoot, p), string(forbidden))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s/: %v", tree, err)
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
