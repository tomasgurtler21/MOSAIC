package app

// harness_only_discovery_test.go covers the two-signal harness-only eligibility function
// (eligibleHarnessOnly), the orchestrator filename classifier (isOrchestratorFileName),
// and the workspace directory scanner (scanHarnessOnlyAgents).
//
// All tested functions are package-internal, so this file belongs to package app (not
// package app_test), giving it direct access to unexported symbols — the same convention
// established in deployed_agent_index_test.go and bundle_conformance_test.go.
//
// Verified behaviours:
//
// eligibleHarnessOnly — two-signal eligibility decision (T4.1):
//   - Both signals present (transform_version + valid canonical section) → Eligible true.
//   - transform_version absent from frontmatter → Eligible false, Reason non-empty.
//   - Body carries no boundary tags → Eligible false (no canonical section found).
//   - Body carries only incidental [[...]]-shaped strings embedded mid-line (not whole-line
//     tags) → Eligible false (no whole-line canonical section found).
//   - Body carries an unclosed section region tag (unbalanced) → Eligible false (blocking
//     "unbalanced-tag" issue).
//   - Body carries mismatched open/close tag names → Eligible false (blocking
//     "mismatched-tag" issue).
//   - Body carries a non-canonical section name (no CanonicalSections member present) →
//     Eligible false (requirement 1 of signal two fails).
//   - Body carries an unknown managed region region name → Eligible false (blocking
//     "unknown-deployed" issue).
//   - Body carries a duplicate section region name → Eligible false (blocking
//     "duplicate-name" issue).
//   - Completely unparseable bytes (invalid YAML) → Eligible false, no panic or error return.
//   - Eligible verdict carries correct Meta (TransformVersion, Version, NumericID).
//   - Explicit `role: "orchestrator"` frontmatter field → Meta.Role == RoleOrchestrator.
//   - Absent `role` frontmatter field → Meta.Role defaults to RoleSubagent.
//   - Unrecognised `role` frontmatter value → Meta.Role defaults to RoleSubagent.
//   - Content outside boundary (e.g. a heading before the first section) is non-blocking.
//   - Empty transform_version value → Eligible false (non-empty scalar required).
//
// isOrchestratorFileName — canonical orchestrator classification (T4.1 / T4.2):
//   - "orchestrator.md" → true.
//   - "orchestrator.agent.md" → true.
//   - "ORCHESTRATOR.MD" (upper-case) → true (case-insensitive).
//   - "ORCHESTRATOR.AGENT.MD" (upper-case) → true.
//   - "my-agent.md" → false.
//   - "orchestrator-helper.md" → false (prefix match is not enough).
//   - "" (empty string) → false.
//
// catalogAgentKeys — key set extraction from the generic catalog (T4.2):
//   - A catalog with workers + utilities + orchestrator → all three key groups present.
//   - An empty catalog → non-nil empty map (no panic on in-operator lookup).
//
// scanHarnessOnlyAgents — directory walk with exclusions (T4.2):
//   - An eligible harness-only file in the agents directory is included in the result.
//   - The result entry carries TargetPath relative to the workspace root.
//   - The result entry Key is derived from FileName by stripping ".agent.md" or ".md".
//   - The result entry carries TransformVersion from the file's frontmatter.
//   - The result entry carries FileName, NumericID, and Version from the file's frontmatter.
//   - The result entry carries Role propagated from the eligibility verdict's Meta.Role.
//   - A file whose derived key matches a catalogKeys entry is excluded.
//   - A file named "orchestrator.md" is excluded regardless of its eligibility.
//   - A file named "orchestrator.agent.md" is excluded regardless of its eligibility.
//   - A non-.md file in the agents directory is ignored.
//   - A directory entry named "*.md" (simulating an unreadable regular file) is skipped
//     silently.
//   - An unparseable (ineligible) file is skipped silently; no error is returned.
//   - An empty agentsDir parameter yields an empty result without scanning the workspace root.
//   - When the agents directory does not exist the result is empty (non-nil).
//   - Multiple eligible files are returned sorted by TargetPath (deterministic).
//   - A realistic mixed directory yields only eligible non-catalog non-orchestrator files.
//
// No-write guarantee (T4.3):
//   - After scanHarnessOnlyAgents returns, every eligible file's bytes are byte-identical
//     to what they were before the call.
//   - After scanHarnessOnlyAgents returns, every ineligible file's bytes are also
//     byte-identical (no writes for any file, not just eligible ones).
//
// Test helpers (T4.4):
//   - miniCatalog provides a minimal catalog.Catalog stub for catalogAgentKeys tests.
//   - minimalEligibleAgentBytes builds fixture bytes satisfying both eligibility signals.
//   - eligibleAgentWithRoleBytes builds eligible fixture bytes with a given role value.
//   - writeHarnessOnlyAgentFile writes a minimal eligible agent file to a temp directory.
//   - writeIneligibleParseable writes a parseable but ineligible agent file.

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T4.4 — Test helpers and temporary-workspace fixtures
// ---------------------------------------------------------------------------

// minimalEligibleAgentBytes returns document bytes that satisfy both eligibility signals:
// frontmatter carries transform_version and the body carries a properly paired canonical
// <Identity type="core"> boundary. No content appears outside the section so no advisory
// issues are produced. These bytes should cause eligibleHarnessOnly to return Eligible true.
func minimalEligibleAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"version: \"2.1.0\"\n" +
		"id: \"42\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"This agent provides identity content.\n" +
		"</Identity>\n")
}

// eligibleAgentWithRoleBytes returns eligible agent bytes that include a `role` field in
// the frontmatter, used to verify that Role is parsed and carried into HarnessOnlyMeta.
func eligibleAgentWithRoleBytes(role string) []byte {
	return []byte("---\n" +
		"transform_version: \"1.5.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"7\"\n" +
		"role: \"" + role + "\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Identity section.\n" +
		"</Identity>\n")
}

// miniCatalog is a minimal catalog.Catalog stub used only by catalogAgentKeys tests.
// Only Agents(), Orchestrator(), OrchestratorScript(), and UtilityAgents() are implemented;
// every other method panics to make misuse of the stub visible immediately.
//
// Set scriptOrchestratorOK to true and populate scriptOrchestrator to simulate a catalog
// that exposes a script orchestrator via OrchestratorScript(). When scriptOrchestratorOK
// is false (the zero value), OrchestratorScript() returns (Agent{}, false), modelling
// a catalog whose script orchestrator is legitimately absent.
type miniCatalog struct {
	workers              []domain.Agent
	orchestrator         domain.Agent
	utilities            []domain.Agent
	scriptOrchestrator   domain.Agent
	scriptOrchestratorOK bool
}

func (c *miniCatalog) Root() string                                  { return "" }
func (c *miniCatalog) CatalogRoot() string                           { return "" }
func (c *miniCatalog) Agents() []domain.Agent                        { return c.workers }
func (c *miniCatalog) Orchestrator() domain.Agent                    { return c.orchestrator }
func (c *miniCatalog) OrchestratorScript() (domain.Agent, bool)      { return c.scriptOrchestrator, c.scriptOrchestratorOK }
func (c *miniCatalog) UtilityAgents() []domain.Agent                 { return c.utilities }
func (c *miniCatalog) StandaloneAgents() []domain.Agent              { return nil }
func (c *miniCatalog) Agent(key string) (domain.Agent, bool)         { panic("miniCatalog: Agent not used") }
func (c *miniCatalog) InfrastructureAgents() []domain.Agent          { panic("miniCatalog: InfrastructureAgents not used") }
func (c *miniCatalog) Skills() []domain.Skill                        { panic("miniCatalog: Skills not used") }
func (c *miniCatalog) Skill(key string) (domain.Skill, bool)         { panic("miniCatalog: Skill not used") }
func (c *miniCatalog) Hooks() []domain.HookBundle                    { panic("miniCatalog: Hooks not used") }
func (c *miniCatalog) Hook(key string) (domain.HookBundle, bool)     { panic("miniCatalog: Hook not used") }
func (c *miniCatalog) Workflows() []domain.Workflow                  { panic("miniCatalog: Workflows not used") }
func (c *miniCatalog) Workflow(id string) (domain.Workflow, bool)    { panic("miniCatalog: Workflow not used") }
func (c *miniCatalog) WorkflowCategories() []domain.WorkflowCategory { panic("miniCatalog: WorkflowCategories not used") }
func (c *miniCatalog) Tiers() []domain.TierInfo                      { panic("miniCatalog: Tiers not used") }
func (c *miniCatalog) WorkflowSection(id string) ([]byte, error)     { panic("miniCatalog: WorkflowSection not used") }
func (c *miniCatalog) ReadSource(path string) ([]byte, error)        { panic("miniCatalog: ReadSource not used") }
func (c *miniCatalog) Issues() []catalog.Issue                       { return nil }
func (c *miniCatalog) AgentByNumericID(id string) (domain.Agent, bool) { panic("miniCatalog: AgentByNumericID not used") }

// writeHarnessOnlyAgentFile writes the given content to filepath.Join(dir, filename) and
// creates the directory if needed. It is used to set up eligible harness-only agent files
// for scanHarnessOnlyAgents tests.
func writeHarnessOnlyAgentFile(t *testing.T, dir, filename string, content []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeHarnessOnlyAgentFile MkdirAll(%q): %v", dir, err)
	}
	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		t.Fatalf("writeHarnessOnlyAgentFile WriteFile(%q): %v", fullPath, err)
	}
	return fullPath
}

// writeIneligibleParseable writes a .md file whose YAML parses correctly but is not an
// eligible harness-only agent (no transform_version). The scan must skip it silently.
func writeIneligibleParseable(t *testing.T, dir, filename string) {
	t.Helper()
	content := []byte("---\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Generic-style agent with no transform_version.\n" +
		"</Identity>\n")
	writeHarnessOnlyAgentFile(t, dir, filename, content)
}

// ---------------------------------------------------------------------------
// T4.1 — eligibleHarnessOnly: two-signal eligibility decision
// ---------------------------------------------------------------------------

// TestEligibleHarnessOnly_BothSignalsPresent_ReturnsEligible verifies that a document
// carrying both eligibility signals — a non-empty transform_version scalar in frontmatter
// and a properly paired canonical section in the body — is reported as eligible.
func TestEligibleHarnessOnly_BothSignalsPresent_ReturnsEligible(t *testing.T) {
	// Arrange
	src := minimalEligibleAgentBytes()

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if !verdict.Eligible {
		t.Errorf("eligibleHarnessOnly returned Eligible false for valid two-signal document; "+
			"want Eligible true. Reason: %q", verdict.Reason)
	}
	if verdict.Reason != "" {
		t.Errorf("eligibleHarnessOnly set Reason = %q on an eligible verdict; "+
			"Reason must be empty when Eligible is true", verdict.Reason)
	}
}

// TestEligibleHarnessOnly_MetaCarriedCorrectly verifies that an eligible verdict populates
// HarnessOnlyMeta with the frontmatter values from the source document.
func TestEligibleHarnessOnly_MetaCarriedCorrectly(t *testing.T) {
	// Arrange — document with known frontmatter values.
	src := minimalEligibleAgentBytes()
	// minimalEligibleAgentBytes sets: transform_version="1.0.0", version="2.1.0", id="42"

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if !verdict.Eligible {
		t.Fatalf("eligibleHarnessOnly returned Eligible false; cannot check Meta. Reason: %q", verdict.Reason)
	}
	if verdict.Meta.TransformVersion != "1.0.0" {
		t.Errorf("verdict.Meta.TransformVersion = %q, want %q", verdict.Meta.TransformVersion, "1.0.0")
	}
	if verdict.Meta.Version != "2.1.0" {
		t.Errorf("verdict.Meta.Version = %q, want %q", verdict.Meta.Version, "2.1.0")
	}
	if verdict.Meta.NumericID != "42" {
		t.Errorf("verdict.Meta.NumericID = %q, want %q", verdict.Meta.NumericID, "42")
	}
}

// TestEligibleHarnessOnly_MissingTransformVersion_ReturnsIneligible verifies that signal
// one failing (no transform_version in frontmatter) makes the verdict ineligible. A file
// without this marker is not one OldAgentsTransform has processed.
func TestEligibleHarnessOnly_MissingTransformVersion_ReturnsIneligible(t *testing.T) {
	// Arrange — frontmatter has no transform_version; body has a canonical section.
	src := []byte("---\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document missing " +
			"transform_version; want Eligible false (signal one fails)")
	}
	if verdict.Reason == "" {
		t.Error("eligibleHarnessOnly returned empty Reason for an ineligible verdict; " +
			"a negative verdict must carry a human-readable Reason")
	}
}

// TestEligibleHarnessOnly_NoBoundaryTags_ReturnsIneligible verifies that signal two failing
// (no boundary tags in the body) makes the verdict ineligible even when signal one passes.
func TestEligibleHarnessOnly_NoBoundaryTags_ReturnsIneligible(t *testing.T) {
	// Arrange — transform_version present, but body is plain prose with no tags.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"This file has no boundary tags whatsoever.\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document with no boundary " +
			"tags; want Eligible false (no canonical section present)")
	}
	if verdict.Reason == "" {
		t.Error("eligibleHarnessOnly returned empty Reason for an ineligible verdict")
	}
}

// TestEligibleHarnessOnly_IncidentalBracketString_ReturnsIneligible verifies that an
// incidental tag-shaped string embedded mid-line (not a whole-line tag) does not
// satisfy signal two. Tags are only recognised when they occupy a complete line.
func TestEligibleHarnessOnly_IncidentalBracketString_ReturnsIneligible(t *testing.T) {
	// Arrange — the <Identity type="core"> strings appear inside prose sentences, not as
	// whole lines. The docformat lexer does not recognise them as boundary tags.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"See the <Identity type=\"core\"> marker for details and </Identity> close.\n" +
		"These are mid-line and must not be treated as real tags.\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document whose [[...]] " +
			"strings are embedded mid-line and are not whole-line boundary tags; " +
			"want Eligible false")
	}
}

// TestEligibleHarnessOnly_UnpairedOpenTag_ReturnsIneligible verifies that an unclosed
// section tag (producing an "unbalanced-tag" validation issue) makes the verdict
// ineligible. Structural errors in the boundary tag set are blocking.
func TestEligibleHarnessOnly_UnpairedOpenTag_ReturnsIneligible(t *testing.T) {
	// Arrange — <Identity type="core"> is opened but never closed.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content with no closing tag.\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document with an unclosed " +
			"section region tag; want Eligible false (blocking \"unbalanced-tag\" issue)")
	}
}

// TestEligibleHarnessOnly_MismatchedTags_ReturnsIneligible verifies that a mismatched
// open/close tag pair (producing a "mismatched-tag" validation issue) makes the verdict
// ineligible.
func TestEligibleHarnessOnly_MismatchedTags_ReturnsIneligible(t *testing.T) {
	// Arrange — open tag says Identity but close tag says Capabilities.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Capabilities>\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document with mismatched " +
			"open/close section tags; want Eligible false (blocking \"mismatched-tag\" issue)")
	}
}

// TestEligibleHarnessOnly_NonCanonicalSectionName_ReturnsIneligible verifies that a
// properly paired section region whose name is not in docformat.CanonicalSections does not
// satisfy signal two's first requirement (at least one canonical section must be present).
func TestEligibleHarnessOnly_NonCanonicalSectionName_ReturnsIneligible(t *testing.T) {
	// Arrange — section name "MyCustomSection" is not in CanonicalSections.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"<MyCustomSection type=\"core\">\n" +
		"Custom content.\n" +
		"</MyCustomSection>\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document whose only section " +
			"name (\"MyCustomSection\") is not in CanonicalSections; " +
			"want Eligible false (no canonical section present)")
	}
}

// TestEligibleHarnessOnly_UnknownDeployedRegion_ReturnsIneligible verifies that a
// managed region tag whose name is not in docformat.CanonicalDeployed produces an
// "unknown-deployed" validation issue, which is a blocking signal-two failure.
func TestEligibleHarnessOnly_UnknownDeployedRegion_ReturnsIneligible(t *testing.T) {
	// Arrange — <SomeUnknownRegion type="managed"> is not in CanonicalDeployed.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"<SomeUnknownRegion type=\"managed\">\n" +
		"</SomeUnknownRegion>\n" +
		"</Identity>\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document containing an " +
			"unknown managed region region name; " +
			"want Eligible false (blocking \"unknown-deployed\" validation issue)")
	}
}

// TestEligibleHarnessOnly_UnparseableBytes_ReturnsIneligibleNoPanic verifies that
// completely unparseable bytes (invalid YAML frontmatter) yield an ineligible verdict
// without panicking or returning an error. "Cannot parse" and "not one of ours" are
// the same outcome for a detection function.
func TestEligibleHarnessOnly_UnparseableBytes_ReturnsIneligibleNoPanic(t *testing.T) {
	// Arrange — invalid YAML: duplicate key makes docformat.Parse fail.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"transform_version: \"2.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	// Act — must not panic.
	verdict := eligibleHarnessOnly(src)

	// Assert — unparseable bytes are ineligible.
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for unparseable bytes; " +
			"want Eligible false (\"cannot parse\" equals \"not one of ours\")")
	}
}

// TestEligibleHarnessOnly_ContentOutsideBoundary_IsNotBlocking verifies that content
// appearing outside all boundary tags (e.g. a title heading above the first section)
// does NOT make the verdict ineligible. The "content-outside-boundary" advisory is
// explicitly non-blocking for harness-only agents.
func TestEligibleHarnessOnly_ContentOutsideBoundary_IsNotBlocking(t *testing.T) {
	// Arrange — a heading line appears before the first section region tag.
	// This triggers a "content-outside-boundary" advisory, which must be ignored.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"# My Agent Heading\n" +
		"\n" +
		"<Identity type=\"core\">\n" +
		"Identity section.\n" +
		"</Identity>\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert — the heading outside the section must not block eligibility.
	if !verdict.Eligible {
		t.Errorf("eligibleHarnessOnly returned Eligible false for a document with a heading "+
			"before the first section; want Eligible true. "+
			"\"content-outside-boundary\" is explicitly non-blocking. Reason: %q", verdict.Reason)
	}
}

// TestEligibleHarnessOnly_EmptyTransformVersion_ReturnsIneligible verifies that a
// transform_version field that is present but has an empty value does not satisfy
// signal one ("non-empty scalar" is required).
func TestEligibleHarnessOnly_EmptyTransformVersion_ReturnsIneligible(t *testing.T) {
	// Arrange — transform_version is present but its scalar value is empty.
	src := []byte("---\n" +
		"transform_version: \"\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document with an empty " +
			"transform_version value; want Eligible false (signal one requires a non-empty scalar)")
	}
}

// TestEligibleHarnessOnly_ExplicitRole_ParsedIntoMeta verifies that a known `role` value
// in the frontmatter is parsed through domain.ParseAgentRole and reflected in
// verdict.Meta.Role. The frontmatter value "orchestrator" must map to domain.RoleOrchestrator.
func TestEligibleHarnessOnly_ExplicitRole_ParsedIntoMeta(t *testing.T) {
	// Arrange — eligible document with role: "orchestrator".
	src := eligibleAgentWithRoleBytes("orchestrator")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if !verdict.Eligible {
		t.Fatalf("eligibleHarnessOnly returned Eligible false for a valid document; "+
			"cannot verify Meta.Role. Reason: %q", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleOrchestrator {
		t.Errorf("verdict.Meta.Role = %q, want RoleOrchestrator (%q); "+
			"an explicit 'role: orchestrator' frontmatter value must be parsed via "+
			"domain.ParseAgentRole and carried into Meta.Role",
			verdict.Meta.Role, domain.RoleOrchestrator)
	}
}

// TestEligibleHarnessOnly_AbsentRole_DefaultsToSubagent verifies that a document whose
// frontmatter contains no `role` field yields verdict.Meta.Role == domain.RoleSubagent.
// The design contract requires: "Defaults to domain.RoleSubagent when the field is absent
// or unrecognised."
func TestEligibleHarnessOnly_AbsentRole_DefaultsToSubagent(t *testing.T) {
	// Arrange — minimalEligibleAgentBytes() has no `role` field.
	src := minimalEligibleAgentBytes()

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if !verdict.Eligible {
		t.Fatalf("eligibleHarnessOnly returned Eligible false for a valid document; "+
			"cannot verify default Meta.Role. Reason: %q", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleSubagent {
		t.Errorf("verdict.Meta.Role = %q, want RoleSubagent (%q) as the default when no "+
			"`role` field is present in frontmatter",
			verdict.Meta.Role, domain.RoleSubagent)
	}
}

// TestEligibleHarnessOnly_UnrecognisedRole_DefaultsToSubagent verifies that a frontmatter
// `role` field carrying an unrecognised value resolves to domain.RoleSubagent. This is the
// "unrecognised" branch of "absent or unrecognised defaults to domain.RoleSubagent" in the
// design contract, distinct from the absent-field case tested above.
func TestEligibleHarnessOnly_UnrecognisedRole_DefaultsToSubagent(t *testing.T) {
	// Arrange — role value "not-a-real-role" is not accepted by domain.ParseAgentRole.
	src := eligibleAgentWithRoleBytes("not-a-real-role")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if !verdict.Eligible {
		t.Fatalf("eligibleHarnessOnly returned Eligible false for a valid document with an "+
			"unrecognised role field; want Eligible true (unrecognised role does not block "+
			"eligibility). Reason: %q", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleSubagent {
		t.Errorf("verdict.Meta.Role = %q, want RoleSubagent (%q) as the default when the "+
			"`role` field carries a value not recognised by domain.ParseAgentRole",
			verdict.Meta.Role, domain.RoleSubagent)
	}
}

// TestEligibleHarnessOnly_DuplicateSectionName_ReturnsIneligible verifies that a document
// containing two section region tags with the same name (producing a "duplicate-name"
// validation issue) is treated as ineligible. "duplicate-name" is one of the five blocking
// validation codes the eligibility function filters on.
func TestEligibleHarnessOnly_DuplicateSectionName_ReturnsIneligible(t *testing.T) {
	// Arrange — <Identity type="core"> appears twice; docformat.Validate produces
	// a "duplicate-name" issue on the second occurrence.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"First identity block.\n" +
		"</Identity>\n" +
		"<Identity type=\"core\">\n" +
		"Duplicate identity block — same name used twice.\n" +
		"</Identity>\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document with a duplicate " +
			"<Identity type=\"core\"> name; want Eligible false (blocking \"duplicate-name\" issue)")
	}
	if verdict.Reason == "" {
		t.Error("eligibleHarnessOnly returned empty Reason for an ineligible verdict; " +
			"a negative verdict must carry a human-readable Reason")
	}
}

// TestEligibleHarnessOnly_CompoundNamedSection_NowEligible verifies that a document
// containing a compound-named section (<Workflow type="core" name="brownfield-tdd">
// ... </Workflow>) alongside a well-formed canonical section is treated as eligible
// once the mismatched-tag false positive is fixed.
//
// Before the Stage 1 fix: parseBoundaryTag assembles the opening name as
// "Workflow:brownfield-tdd" but the closing tag supplies the bare name "Workflow";
// the validator's direct comparison ("Workflow:brownfield-tdd" != "Workflow") fires a
// blocking "mismatched-tag" issue, making the verdict ineligible.
//
// After the Stage 1 fix: the comparison uses the tag-name prefix
// (TagNamePrefix("Workflow:brownfield-tdd") == "Workflow"), which matches the closing
// tag name, so no "mismatched-tag" issue is raised. The document then satisfies both
// eligibility signals and the verdict is Eligible true.
func TestEligibleHarnessOnly_CompoundNamedSection_NowEligible(t *testing.T) {
	// Arrange — a document satisfying both eligibility signals:
	//   Signal 1: transform_version is present.
	//   Signal 2: at least one canonical section (<Identity type="core">) is present and
	//             the document has no blocking validation issue.
	// The compound-named section (<Workflow type="core" name="brownfield-tdd">) uses the
	// real-world pattern that triggered the mismatched-tag false positive reported in the
	// Stage 1 bug description.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"version: \"2.1.0\"\n" +
		"id: \"99\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Agent identity content.\n" +
		"</Identity>\n" +
		"<Workflow type=\"core\" name=\"brownfield-tdd\">\n" +
		"Workflow section content.\n" +
		"</Workflow>\n")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert — the compound-named section must no longer block eligibility.
	if !verdict.Eligible {
		t.Errorf("eligibleHarnessOnly returned Eligible false for a document with a "+
			"compound-named section; want Eligible true after the Stage 1 fix removes "+
			"the false-positive 'mismatched-tag' issue for compound open/close name pairs. "+
			"Reason: %q", verdict.Reason)
	}
	if verdict.Reason != "" {
		t.Errorf("eligibleHarnessOnly set Reason = %q on an eligible verdict; "+
			"Reason must be empty when Eligible is true", verdict.Reason)
	}
}

// ---------------------------------------------------------------------------
// T4.1 — isOrchestratorFileName: orchestrator filename classification
// ---------------------------------------------------------------------------

// TestIsOrchestratorFileName_OrchestratorMd_ReturnsTrue verifies that "orchestrator.md"
// is classified as the orchestrator agent's file name.
func TestIsOrchestratorFileName_OrchestratorMd_ReturnsTrue(t *testing.T) {
	if !isOrchestratorFileName("orchestrator.md") {
		t.Error("isOrchestratorFileName(\"orchestrator.md\") returned false; want true")
	}
}

// TestIsOrchestratorFileName_OrchestratorAgentMd_ReturnsTrue verifies that the alternate
// form "orchestrator.agent.md" is also classified as the orchestrator file name.
func TestIsOrchestratorFileName_OrchestratorAgentMd_ReturnsTrue(t *testing.T) {
	if !isOrchestratorFileName("orchestrator.agent.md") {
		t.Error("isOrchestratorFileName(\"orchestrator.agent.md\") returned false; want true")
	}
}

// TestIsOrchestratorFileName_UpperCase_ReturnsTrue verifies that the comparison is
// case-insensitive — "ORCHESTRATOR.MD" must also return true.
func TestIsOrchestratorFileName_UpperCase_ReturnsTrue(t *testing.T) {
	if !isOrchestratorFileName("ORCHESTRATOR.MD") {
		t.Error("isOrchestratorFileName(\"ORCHESTRATOR.MD\") returned false; " +
			"want true — classification must be case-insensitive")
	}
}

// TestIsOrchestratorFileName_UpperCaseAgentMd_ReturnsTrue verifies case-insensitivity for
// the "orchestrator.agent.md" form as well.
func TestIsOrchestratorFileName_UpperCaseAgentMd_ReturnsTrue(t *testing.T) {
	if !isOrchestratorFileName("ORCHESTRATOR.AGENT.MD") {
		t.Error("isOrchestratorFileName(\"ORCHESTRATOR.AGENT.MD\") returned false; " +
			"want true — classification must be case-insensitive")
	}
}

// TestIsOrchestratorFileName_RegularAgent_ReturnsFalse verifies that an ordinary agent
// file name is not classified as the orchestrator.
func TestIsOrchestratorFileName_RegularAgent_ReturnsFalse(t *testing.T) {
	if isOrchestratorFileName("my-agent.md") {
		t.Error("isOrchestratorFileName(\"my-agent.md\") returned true; want false")
	}
}

// TestIsOrchestratorFileName_OrchestratorPrefix_ReturnsFalse verifies that a file whose
// name starts with "orchestrator" but is not one of the exact two forms returns false.
// Prefix matching is not sufficient; the entire name must match.
func TestIsOrchestratorFileName_OrchestratorPrefix_ReturnsFalse(t *testing.T) {
	if isOrchestratorFileName("orchestrator-helper.md") {
		t.Error("isOrchestratorFileName(\"orchestrator-helper.md\") returned true; " +
			"want false — prefix match is not sufficient, the name must be exact")
	}
}

// TestIsOrchestratorFileName_Empty_ReturnsFalse verifies that an empty string returns false
// rather than panicking or returning true.
func TestIsOrchestratorFileName_Empty_ReturnsFalse(t *testing.T) {
	if isOrchestratorFileName("") {
		t.Error("isOrchestratorFileName(\"\") returned true; want false")
	}
}

// ---------------------------------------------------------------------------
// T4.2 — catalogAgentKeys: key set extraction from the generic catalog
// ---------------------------------------------------------------------------

// TestCatalogAgentKeys_FullCatalog_ReturnsAllAgentKeys verifies that catalogAgentKeys
// returns a map containing keys for all three agent groups the generic catalog knows about:
// worker agents, utility agents, and the orchestrator. Using the full catalog (not just the
// workflow-resolved artifact set) is the documented contract: an agent not selected by any
// workflow still has a generic counterpart and must not be treated as harness-only.
func TestCatalogAgentKeys_FullCatalog_ReturnsAllAgentKeys(t *testing.T) {
	// Arrange — catalog with one worker, one utility, and one orchestrator.
	cat := &miniCatalog{
		workers:      []domain.Agent{{Key: "worker-agent"}},
		utilities:    []domain.Agent{{Key: "utility-agent"}},
		orchestrator: domain.Agent{Key: "orchestrator"},
	}

	// Act
	keys := catalogAgentKeys(cat)

	// Assert — all three keys must appear in the returned set.
	for _, want := range []string{"worker-agent", "utility-agent", "orchestrator"} {
		if !keys[want] {
			t.Errorf("catalogAgentKeys returned a map missing key %q; "+
				"worker agents, utility agents, and the orchestrator must all be included",
				want)
		}
	}
}

// TestCatalogAgentKeys_EmptyCatalog_ReturnsNonNilEmptyMap verifies that catalogAgentKeys
// returns a non-nil empty map when the catalog contains no agents (no workers, no utilities,
// no orchestrator). A nil return would panic on the caller's map-key lookup.
func TestCatalogAgentKeys_EmptyCatalog_ReturnsNonNilEmptyMap(t *testing.T) {
	// Arrange — catalog with no agents of any kind.
	cat := &miniCatalog{
		workers:   []domain.Agent{},
		utilities: []domain.Agent{},
		// orchestrator is zero-valued (Key == ""); must not be added to the key set.
	}

	// Act
	keys := catalogAgentKeys(cat)

	// Assert
	if keys == nil {
		t.Error("catalogAgentKeys returned nil for an empty catalog; " +
			"an empty non-nil map is required so callers can safely use the in-operator")
	}
	if len(keys) != 0 {
		t.Errorf("catalogAgentKeys returned %d entries for an empty catalog, want 0; "+
			"entries: %v", len(keys), keys)
	}
}

// ---------------------------------------------------------------------------
// T4.2 — scanHarnessOnlyAgents: directory walk and exclusions
// ---------------------------------------------------------------------------

// TestScanHarnessOnlyAgents_EligibleFile_IsReturned verifies that an eligible harness-only
// file in the agents directory appears in the scan result.
func TestScanHarnessOnlyAgents_EligibleFile_IsReturned(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.md", minimalEligibleAgentBytes())

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents returned empty result; " +
			"an eligible harness-only file must be included")
	}
	if len(result) != 1 {
		t.Errorf("scanHarnessOnlyAgents returned %d results, want 1", len(result))
	}
}

// TestScanHarnessOnlyAgents_EligibleFile_TargetPathIsRelative verifies that the returned
// HarnessOnlyAgent carries a TargetPath relative to the workspace root (not absolute),
// in the same form as plan items and the executor use.
func TestScanHarnessOnlyAgents_EligibleFile_TargetPathIsRelative(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.md", minimalEligibleAgentBytes())

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents returned empty result; cannot verify TargetPath")
	}
	want := filepath.Join(agentsDir, "my-agent.md")
	if result[0].TargetPath != want {
		t.Errorf("result[0].TargetPath = %q, want %q; "+
			"TargetPath must be relative to the workspace root", result[0].TargetPath, want)
	}
}

// TestScanHarnessOnlyAgents_EligibleFile_KeyIsDerivedFromFileName verifies that the Key
// field in the returned entry is the base name with ".md" stripped, matching how the
// catalog derives a key from a source file name.
func TestScanHarnessOnlyAgents_EligibleFile_KeyIsDerivedFromFileName(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.md", minimalEligibleAgentBytes())

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents returned empty result; cannot verify Key")
	}
	if result[0].Key != "my-agent" {
		t.Errorf("result[0].Key = %q, want %q; "+
			"Key must be the base name with .md stripped", result[0].Key, "my-agent")
	}
}

// TestScanHarnessOnlyAgents_AgentMdExtension_KeyStripsAgentMd verifies that when the
// file uses the ".agent.md" double extension, the Key strips the entire ".agent.md" suffix.
func TestScanHarnessOnlyAgents_AgentMdExtension_KeyStripsAgentMd(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.agent.md", minimalEligibleAgentBytes())

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents returned empty result; cannot verify Key for .agent.md file")
	}
	if result[0].Key != "my-agent" {
		t.Errorf("result[0].Key = %q, want %q; "+
			"Key must strip the full \".agent.md\" suffix", result[0].Key, "my-agent")
	}
}

// TestScanHarnessOnlyAgents_EligibleFile_TransformVersionCarried verifies that the
// TransformVersion field in the returned entry matches the frontmatter value from the file.
func TestScanHarnessOnlyAgents_EligibleFile_TransformVersionCarried(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.md", minimalEligibleAgentBytes())
	// minimalEligibleAgentBytes sets transform_version: "1.0.0"

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents returned empty result; cannot verify TransformVersion")
	}
	if result[0].TransformVersion != "1.0.0" {
		t.Errorf("result[0].TransformVersion = %q, want %q",
			result[0].TransformVersion, "1.0.0")
	}
}

// TestScanHarnessOnlyAgents_CatalogCounterpartKey_IsExcluded verifies that a file whose
// derived key appears in the catalogKeys map is excluded from the result. A file with a
// generic catalog counterpart is NOT harness-only.
func TestScanHarnessOnlyAgents_CatalogCounterpartKey_IsExcluded(t *testing.T) {
	// Arrange — "my-agent" appears in the catalog; the file is eligible but has a counterpart.
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.md", minimalEligibleAgentBytes())
	catalogKeys := map[string]bool{"my-agent": true}

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, catalogKeys)

	// Assert
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for a file whose key %q is "+
			"in the catalog; want 0 — catalog-backed agents are NOT harness-only",
			len(result), "my-agent")
	}
}

// TestScanHarnessOnlyAgents_OrchestratorNamedFile_IsExcluded verifies that a file named
// "orchestrator.md" is never returned, regardless of its eligibility signals. Orchestrator
// exclusion is canonical by filename; frontmatter is not consulted.
func TestScanHarnessOnlyAgents_OrchestratorNamedFile_IsExcluded(t *testing.T) {
	// Arrange — "orchestrator.md" satisfies both eligibility signals but must be excluded.
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator.md", minimalEligibleAgentBytes())

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for \"orchestrator.md\"; "+
			"want 0 — orchestrator-named files must always be excluded", len(result))
	}
}

// TestScanHarnessOnlyAgents_OrchestratorAgentMdFile_IsExcluded verifies that the alternate
// orchestrator form "orchestrator.agent.md" is also excluded.
func TestScanHarnessOnlyAgents_OrchestratorAgentMdFile_IsExcluded(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator.agent.md", minimalEligibleAgentBytes())

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for \"orchestrator.agent.md\"; "+
			"want 0 — orchestrator-named files (both forms) must always be excluded", len(result))
	}
}

// TestScanHarnessOnlyAgents_NonMdFile_IsIgnored verifies that a file whose name does not
// end in ".md" is not examined by the scan.
func TestScanHarnessOnlyAgents_NonMdFile_IsIgnored(t *testing.T) {
	// Arrange — write an eligible file with a .txt extension; it should be ignored.
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.txt", minimalEligibleAgentBytes())

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for a non-.md file; "+
			"want 0 — only .md files are scanned", len(result))
	}
}

// TestScanHarnessOnlyAgents_IneligibleFile_IsSkippedSilently verifies that a parseable but
// ineligible .md file (one missing transform_version) is silently skipped. The result is
// empty and no error or panic occurs.
func TestScanHarnessOnlyAgents_IneligibleFile_IsSkippedSilently(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeIneligibleParseable(t, fullDir, "generic-agent.md")

	// Act — must not panic.
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for an ineligible file; "+
			"want 0 — files failing either signal must be skipped silently", len(result))
	}
}

// TestScanHarnessOnlyAgents_DirectoryEntryNamedMd_IsSkippedSilently verifies that when a
// directory entry happens to have a ".md" name, the scan skips it silently. The
// entry.IsDir() guard in scanHarnessOnlyAgents fires before any os.ReadFile call, so the
// directory is excluded at the directory-classification step, not the read step.
func TestScanHarnessOnlyAgents_DirectoryEntryNamedMd_IsSkippedSilently(t *testing.T) {
	// Arrange — create a subdirectory named "bad.md". The entry.IsDir() guard catches it
	// before os.ReadFile is reached, exercising the non-recursive walk contract.
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(filepath.Join(fullDir, "bad.md"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Act — must not panic or return an error.
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert — the directory must produce no result entry.
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) after encountering a "+
			"directory named \"bad.md\"; want 0 — unreadable entries must be skipped silently",
			len(result))
	}
}

// TestScanHarnessOnlyAgents_EmptyAgentsDir_ReturnsEmpty verifies that an empty agentsDir
// parameter ("") yields an empty result without scanning the workspace root. The guard
// lives inside scanHarnessOnlyAgents; an empty string must not expand to the workspace.
func TestScanHarnessOnlyAgents_EmptyAgentsDir_ReturnsEmpty(t *testing.T) {
	// Arrange — put an eligible file directly in the workspace root. With agentsDir=="",
	// the scan must not reach it.
	workspace := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workspace, "my-agent.md"), minimalEligibleAgentBytes(), 0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Act
	result := scanHarnessOnlyAgents(workspace, "", map[string]bool{})

	// Assert
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents with empty agentsDir returned %d result(s); "+
			"want 0 — an empty agentsDir must yield an empty result, never the workspace root",
			len(result))
	}
}

// TestScanHarnessOnlyAgents_AgentsDirDoesNotExist_ReturnsEmpty verifies that when the
// agents directory does not exist on disk, the scan returns an empty result without error.
// This mirrors the tolerance contract of buildDeployedAgentIndex.
func TestScanHarnessOnlyAgents_AgentsDirDoesNotExist_ReturnsEmpty(t *testing.T) {
	// Arrange — workspace exists but the agents subdirectory does not.
	workspace := t.TempDir()
	agentsDir := ".claude/agents" // not created

	// Act — must not panic.
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if result == nil {
		t.Error("scanHarnessOnlyAgents returned nil for a non-existent agents directory; " +
			"want a non-nil empty slice")
	}
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for a non-existent directory; "+
			"want 0", len(result))
	}
}

// TestScanHarnessOnlyAgents_MultipleEligibleFiles_ReturnedSortedByTargetPath verifies that
// multiple eligible files are returned sorted by TargetPath so a run is deterministic and
// does not depend on the file system's walk order.
func TestScanHarnessOnlyAgents_MultipleEligibleFiles_ReturnedSortedByTargetPath(t *testing.T) {
	// Arrange — three eligible files with names chosen so alphabetical TargetPath order
	// differs from the order in which os.ReadDir typically walks them.
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	for _, name := range []string{"zebra.md", "alpha.md", "mango.md"} {
		writeHarnessOnlyAgentFile(t, fullDir, name, minimalEligibleAgentBytes())
	}

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert — must have 3 results.
	if len(result) != 3 {
		t.Fatalf("scanHarnessOnlyAgents returned %d result(s), want 3", len(result))
	}

	// Verify the slice is sorted by TargetPath.
	isSorted := sort.SliceIsSorted(result, func(i, j int) bool {
		return result[i].TargetPath < result[j].TargetPath
	})
	if !isSorted {
		paths := make([]string, len(result))
		for i, r := range result {
			paths[i] = r.TargetPath
		}
		t.Errorf("scanHarnessOnlyAgents results are not sorted by TargetPath; got %v", paths)
	}
}

// TestScanHarnessOnlyAgents_MixedFiles_OnlyEligibleHarnessOnlyReturned verifies a realistic
// scenario where the agents directory contains a mix of eligible harness-only files,
// catalog-backed files, an orchestrator file, and an ineligible file. Only the eligible
// non-catalog, non-orchestrator files appear in the result.
func TestScanHarnessOnlyAgents_MixedFiles_OnlyEligibleHarnessOnlyReturned(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	// harness-only eligible (should be returned)
	writeHarnessOnlyAgentFile(t, fullDir, "harness-agent.md", minimalEligibleAgentBytes())

	// eligible but catalog-backed (should be excluded)
	writeHarnessOnlyAgentFile(t, fullDir, "catalog-agent.md", minimalEligibleAgentBytes())

	// orchestrator-named (should be excluded)
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator.md", minimalEligibleAgentBytes())

	// ineligible (no transform_version — should be skipped)
	writeIneligibleParseable(t, fullDir, "plain-agent.md")

	catalogKeys := map[string]bool{"catalog-agent": true}

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, catalogKeys)

	// Assert — only "harness-agent" should appear.
	if len(result) != 1 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s), want 1 "+
			"(only \"harness-agent\" qualifies)", len(result))
		return
	}
	if result[0].Key != "harness-agent" {
		t.Errorf("result[0].Key = %q, want %q", result[0].Key, "harness-agent")
	}
}

// TestScanHarnessOnlyAgents_EligibleFile_RoleCarried verifies that the Role field parsed
// by eligibleHarnessOnly from the file's `role` frontmatter value is propagated into the
// returned HarnessOnlyAgent.Role. Role selects the protocol and bundle-block variant during
// the refresh path; a silent parsing bug here would surface only in Stage 5+ and be hard to
// trace.
func TestScanHarnessOnlyAgents_EligibleFile_RoleCarried(t *testing.T) {
	// Arrange — agent file with role: "orchestrator". The file name is not an orchestrator
	// name (isOrchestratorFileName is false), so it is not excluded by that check.
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-orchestrator-flavoured.md",
		eligibleAgentWithRoleBytes("orchestrator"))

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents returned empty result; cannot verify Role")
	}
	if result[0].Role != domain.RoleOrchestrator {
		t.Errorf("result[0].Role = %q, want RoleOrchestrator (%q); "+
			"scanHarnessOnlyAgents must propagate Meta.Role from eligibleHarnessOnly "+
			"into the returned HarnessOnlyAgent.Role",
			result[0].Role, domain.RoleOrchestrator)
	}
}

// TestScanHarnessOnlyAgents_EligibleFile_AllFrontmatterFieldsCarried verifies that
// FileName, NumericID, and Version are all propagated from the eligible file's frontmatter
// into the returned HarnessOnlyAgent. TransformVersion propagation is covered by
// TestScanHarnessOnlyAgents_EligibleFile_TransformVersionCarried; this test asserts the
// three remaining fields using the same fixture values that minimalEligibleAgentBytes()
// already provides (version="2.1.0", id="42").
func TestScanHarnessOnlyAgents_EligibleFile_AllFrontmatterFieldsCarried(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.md", minimalEligibleAgentBytes())
	// minimalEligibleAgentBytes sets: transform_version="1.0.0", version="2.1.0", id="42"

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents returned empty result; cannot verify frontmatter fields")
	}
	if result[0].FileName != "my-agent.md" {
		t.Errorf("result[0].FileName = %q, want %q; "+
			"FileName must be the base file name including its extension",
			result[0].FileName, "my-agent.md")
	}
	if result[0].NumericID != "42" {
		t.Errorf("result[0].NumericID = %q, want %q; "+
			"NumericID must be carried from the frontmatter `id` scalar",
			result[0].NumericID, "42")
	}
	if result[0].Version != "2.1.0" {
		t.Errorf("result[0].Version = %q, want %q; "+
			"Version must be carried from the frontmatter `version` scalar",
			result[0].Version, "2.1.0")
	}
}

// ---------------------------------------------------------------------------
// T4.3 — No-write guarantee: scan leaves every file byte-identical
// ---------------------------------------------------------------------------

// TestScanHarnessOnlyAgents_PerformsNoWrites_EligibleFileBytesUnchanged verifies that
// scanHarnessOnlyAgents opens no file for writing and that every eligible file in the
// agents directory is byte-identical before and after the call. "Untouched" is an
// explicit assertable property, not an implication.
func TestScanHarnessOnlyAgents_PerformsNoWrites_EligibleFileBytesUnchanged(t *testing.T) {
	// Arrange — two eligible files.
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	files := []struct {
		name    string
		content []byte
	}{
		{"agent-alpha.md", minimalEligibleAgentBytes()},
		{"agent-beta.md", eligibleAgentWithRoleBytes("subagent")},
	}

	for _, f := range files {
		writeHarnessOnlyAgentFile(t, fullDir, f.name, f.content)
	}

	// Read the original bytes for comparison.
	originalBytes := make(map[string][]byte, len(files))
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(fullDir, f.name))
		if err != nil {
			t.Fatalf("pre-scan ReadFile(%q): %v", f.name, err)
		}
		originalBytes[f.name] = b
	}

	// Act — run the scan.
	_ = scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert — every file must be byte-identical to its pre-scan bytes.
	for _, f := range files {
		after, err := os.ReadFile(filepath.Join(fullDir, f.name))
		if err != nil {
			t.Fatalf("post-scan ReadFile(%q): %v", f.name, err)
		}
		if !bytes.Equal(originalBytes[f.name], after) {
			t.Errorf("file %q was modified by scanHarnessOnlyAgents; "+
				"the scan must perform no writes and leave every file byte-identical",
				f.name)
		}
	}
}

// TestScanHarnessOnlyAgents_PerformsNoWrites_IneligibleFileBytesUnchanged verifies that
// the no-write guarantee holds for ineligible files as well, not only for eligible ones.
// A file that fails either signal must be left completely untouched.
func TestScanHarnessOnlyAgents_PerformsNoWrites_IneligibleFileBytesUnchanged(t *testing.T) {
	// Arrange — one ineligible file (no transform_version).
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	filename := "plain-agent.md"
	writeIneligibleParseable(t, fullDir, filename)

	// Read original bytes.
	original, err := os.ReadFile(filepath.Join(fullDir, filename))
	if err != nil {
		t.Fatalf("pre-scan ReadFile: %v", err)
	}

	// Act
	_ = scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert
	after, err := os.ReadFile(filepath.Join(fullDir, filename))
	if err != nil {
		t.Fatalf("post-scan ReadFile: %v", err)
	}
	if !bytes.Equal(original, after) {
		t.Errorf("ineligible file %q was modified by scanHarnessOnlyAgents; "+
			"files failing either signal must be left completely untouched", filename)
	}
}

// ---------------------------------------------------------------------------
// AC1.6 — Structural parsing checks not skipped for utility and standalone roles
// ---------------------------------------------------------------------------

// eligibleBytesWithRoleAndUnbalancedTag returns document bytes that satisfy signal one
// (transform_version present) and declare a given `role` in frontmatter, but have an
// unbalanced tag (open tag with no matching close tag). These bytes are used to verify
// that structural validation runs regardless of the agent's declared role.
func eligibleBytesWithRoleAndUnbalancedTag(role string) []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"role: \"" + role + "\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content with no closing tag — unbalanced open tag.\n")
}

// TestEligibleHarnessOnly_UtilityRole_UnbalancedTagStillBlocks verifies that a document
// declaring `role: utility` is still subject to structural validation. When the body contains
// an unbalanced section tag (a blocking "unbalanced-tag" issue), the verdict must be
// Eligible: false regardless of the role value. This test locks the requirement from the
// plan that boundary/structural parsing checks must not be gated on role — no role-based
// skip may be added to the validation path.
func TestEligibleHarnessOnly_UtilityRole_UnbalancedTagStillBlocks(t *testing.T) {
	// Arrange — eligible document with role: "utility" and an unclosed section tag.
	// After the vocabulary expansion, ParseAgentRole accepts "utility"; the structural
	// validation path must still execute and block eligibility.
	src := eligibleBytesWithRoleAndUnbalancedTag("utility")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert — the unbalanced tag must block eligibility even though the role is recognised.
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document with role \"utility\" " +
			"and an unclosed section tag; want Eligible false — structural validation " +
			"(blocking \"unbalanced-tag\" issue) must run regardless of role")
	}
	if verdict.Reason == "" {
		t.Error("eligibleHarnessOnly returned empty Reason for an ineligible verdict; " +
			"a negative verdict must carry a human-readable Reason")
	}
}

// TestEligibleHarnessOnly_StandaloneRole_UnbalancedTagStillBlocks verifies that a document
// declaring `role: standalone` is subject to the same structural validation as any other
// role. An unbalanced section tag produces a blocking "unbalanced-tag" issue; the verdict
// must be Eligible: false. A role-based skip at the validation call site would cause this
// test to fail with Eligible: true, which is the precise regression this test is designed
// to detect.
func TestEligibleHarnessOnly_StandaloneRole_UnbalancedTagStillBlocks(t *testing.T) {
	// Arrange — eligible document with role: "standalone" and an unclosed section tag.
	src := eligibleBytesWithRoleAndUnbalancedTag("standalone")

	// Act
	verdict := eligibleHarnessOnly(src)

	// Assert — the unbalanced tag must block eligibility even though the role is recognised.
	if verdict.Eligible {
		t.Error("eligibleHarnessOnly returned Eligible true for a document with role \"standalone\" " +
			"and an unclosed section tag; want Eligible false — structural validation " +
			"(blocking \"unbalanced-tag\" issue) must run regardless of role")
	}
	if verdict.Reason == "" {
		t.Error("eligibleHarnessOnly returned empty Reason for an ineligible verdict; " +
			"a negative verdict must carry a human-readable Reason")
	}
}

// ---------------------------------------------------------------------------
// catalogAgentKeys — script orchestrator key derivation
// ---------------------------------------------------------------------------
//
// Verified behaviours:
//   - A catalog that exposes a script orchestrator via OrchestratorScript() contributes
//     its key to the returned set.
//   - A catalog where OrchestratorScript() returns ok=false contributes no key and
//     does not panic (absence is legitimate, not an error).
//   - A script orchestrator with an empty Key does not insert an empty-string entry
//     (the empty-key guard applies to every accessor, including OrchestratorScript).

// TestCatalogAgentKeys_WithScriptOrchestrator_IncludesKey verifies that when the catalog
// exposes a script orchestrator through OrchestratorScript(), its key is included in the
// returned set. A deployed orchestrator-script.md then has a generic counterpart and must
// not be classified as harness-only.
func TestCatalogAgentKeys_WithScriptOrchestrator_IncludesKey(t *testing.T) {
	// Arrange — catalog that exposes a script orchestrator.
	cat := &miniCatalog{
		workers:              []domain.Agent{{Key: "worker-agent"}},
		orchestrator:         domain.Agent{Key: "orchestrator"},
		scriptOrchestrator:   domain.Agent{Key: "orchestrator-script"},
		scriptOrchestratorOK: true,
	}

	// Act
	keys := catalogAgentKeys(cat)

	// Assert — the script orchestrator's key must be present.
	if !keys["orchestrator-script"] {
		t.Error("catalogAgentKeys returned a map missing key \"orchestrator-script\"; " +
			"when OrchestratorScript() returns ok=true, its key must be included so a " +
			"deployed orchestrator-script.md is not classified as harness-only")
	}
	// Verify the existing keys are still present — the script orchestrator is additive.
	if !keys["worker-agent"] {
		t.Error("catalogAgentKeys missing \"worker-agent\" after adding script orchestrator; " +
			"existing worker agent keys must still be included")
	}
	if !keys["orchestrator"] {
		t.Error("catalogAgentKeys missing \"orchestrator\" after adding script orchestrator; " +
			"orchestrator key must still be included")
	}
}

// TestCatalogAgentKeys_WithoutScriptOrchestrator_NoKeyAdded verifies that when the catalog
// has no script orchestrator (OrchestratorScript returns ok=false), no key is contributed
// and the function neither panics nor inserts a spurious entry. The absence of a script
// orchestrator is a legitimate and expected catalog state, not an error.
func TestCatalogAgentKeys_WithoutScriptOrchestrator_NoKeyAdded(t *testing.T) {
	// Arrange — catalog with no script orchestrator; scriptOrchestratorOK is false (zero value).
	cat := &miniCatalog{
		workers:      []domain.Agent{{Key: "worker-agent"}},
		orchestrator: domain.Agent{Key: "orchestrator"},
	}
	wantLen := 2 // "worker-agent" + "orchestrator" only

	// Act — must not panic.
	keys := catalogAgentKeys(cat)

	// Assert — no "orchestrator-script" entry, and no extra entries of any kind.
	if keys["orchestrator-script"] {
		t.Error("catalogAgentKeys added key \"orchestrator-script\" when OrchestratorScript " +
			"returned ok=false; an absent script orchestrator must contribute no key")
	}
	if len(keys) != wantLen {
		t.Errorf("catalogAgentKeys returned %d entries for a catalog with no script orchestrator, "+
			"want %d; entries: %v", len(keys), wantLen, keys)
	}
}

// TestCatalogAgentKeys_ScriptOrchestratorEmptyKey_NotInserted verifies that when
// OrchestratorScript returns ok=true but the agent's Key is the empty string, no
// empty-string key is inserted. The empty-key guard must apply to the script orchestrator
// branch exactly as it does for worker agents, utility agents, and the plain orchestrator.
func TestCatalogAgentKeys_ScriptOrchestratorEmptyKey_NotInserted(t *testing.T) {
	// Arrange — script orchestrator present but its Key is empty.
	cat := &miniCatalog{
		scriptOrchestrator:   domain.Agent{Key: ""},
		scriptOrchestratorOK: true,
	}

	// Act
	keys := catalogAgentKeys(cat)

	// Assert — the empty string must not appear as a key.
	if keys[""] {
		t.Error("catalogAgentKeys inserted an empty key when the script orchestrator's Key " +
			"field is \"\"; empty keys must never be inserted for any accessor")
	}
}

// ---------------------------------------------------------------------------
// isOrchestratorFileName — script orchestrator filename forms
// ---------------------------------------------------------------------------
//
// Verified behaviours (complement the existing orchestrator.md / orchestrator.agent.md
// tests already in the file):
//   - "orchestrator-script.md" → true
//   - "orchestrator-script.agent.md" → true
//   - "ORCHESTRATOR-SCRIPT.MD" (upper-case) → true (case-insensitive)
//   - "ORCHESTRATOR-SCRIPT.AGENT.MD" (upper-case) → true
//   - "orchestrator-script-custom.md" (prefix match only) → false

// TestIsOrchestratorFileName_OrchestratorScriptMd_ReturnsTrue verifies that
// "orchestrator-script.md" is classified as an orchestrator-role file name.
func TestIsOrchestratorFileName_OrchestratorScriptMd_ReturnsTrue(t *testing.T) {
	if !isOrchestratorFileName("orchestrator-script.md") {
		t.Error("isOrchestratorFileName(\"orchestrator-script.md\") returned false; want true")
	}
}

// TestIsOrchestratorFileName_OrchestratorScriptAgentMd_ReturnsTrue verifies that the
// alternate form "orchestrator-script.agent.md" is also classified as an orchestrator-role
// file name. Both suffix forms must be accepted, matching the existing orchestrator contract.
func TestIsOrchestratorFileName_OrchestratorScriptAgentMd_ReturnsTrue(t *testing.T) {
	if !isOrchestratorFileName("orchestrator-script.agent.md") {
		t.Error("isOrchestratorFileName(\"orchestrator-script.agent.md\") returned false; want true")
	}
}

// TestIsOrchestratorFileName_OrchestratorScriptMdUpperCase_ReturnsTrue verifies that the
// comparison is case-insensitive for the script orchestrator form: "ORCHESTRATOR-SCRIPT.MD"
// must return true.
func TestIsOrchestratorFileName_OrchestratorScriptMdUpperCase_ReturnsTrue(t *testing.T) {
	if !isOrchestratorFileName("ORCHESTRATOR-SCRIPT.MD") {
		t.Error("isOrchestratorFileName(\"ORCHESTRATOR-SCRIPT.MD\") returned false; " +
			"want true — classification must be case-insensitive")
	}
}

// TestIsOrchestratorFileName_OrchestratorScriptAgentMdUpperCase_ReturnsTrue verifies
// case-insensitivity for the "orchestrator-script.agent.md" form.
func TestIsOrchestratorFileName_OrchestratorScriptAgentMdUpperCase_ReturnsTrue(t *testing.T) {
	if !isOrchestratorFileName("ORCHESTRATOR-SCRIPT.AGENT.MD") {
		t.Error("isOrchestratorFileName(\"ORCHESTRATOR-SCRIPT.AGENT.MD\") returned false; " +
			"want true — classification must be case-insensitive")
	}
}

// TestIsOrchestratorFileName_OrchestratorScriptCustomSuffix_ReturnsFalse verifies that a
// file whose name begins with "orchestrator-script" but is not one of the exact two accepted
// forms returns false. Matching is whole-name equality, never a prefix or substring test.
func TestIsOrchestratorFileName_OrchestratorScriptCustomSuffix_ReturnsFalse(t *testing.T) {
	if isOrchestratorFileName("orchestrator-script-custom.md") {
		t.Error("isOrchestratorFileName(\"orchestrator-script-custom.md\") returned true; " +
			"want false — only exact whole-name matches are recognised, " +
			"a file named \"orchestrator-script-custom.md\" is not an orchestrator file")
	}
}

// ---------------------------------------------------------------------------
// scanHarnessOnlyAgents — script orchestrator scan-level exclusion
// ---------------------------------------------------------------------------
//
// Verified behaviours:
//   - A deployed orchestrator-script.md whose catalog exposes the script orchestrator is
//     NOT returned as harness-only (catalog-key exclusion channel).
//   - A deployed orchestrator-script.md is NOT returned even with empty catalogKeys
//     (filename exclusion channel via isOrchestratorFileName).
//   - When orchestrator-script.md is excluded, a co-located genuine harness-only file
//     with no catalog counterpart is still returned (the two-signal rule is unchanged).

// TestScanHarnessOnlyAgents_OrchestratorScriptInCatalog_NotHarnessOnly verifies that a
// deployed orchestrator-script.md file whose catalog exposes the script orchestrator is NOT
// returned by the harness-only scan. The catalog-key exclusion is the primary channel tested
// here: catalogAgentKeys must include "orchestrator-script" so the scan's catalog-key check
// excludes the file.
func TestScanHarnessOnlyAgents_OrchestratorScriptInCatalog_NotHarnessOnly(t *testing.T) {
	// Arrange — catalog that exposes a script orchestrator.
	cat := &miniCatalog{
		workers:              []domain.Agent{{Key: "worker-agent"}},
		orchestrator:         domain.Agent{Key: "orchestrator"},
		scriptOrchestrator:   domain.Agent{Key: "orchestrator-script"},
		scriptOrchestratorOK: true,
	}

	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	// Write an eligible orchestrator-script.md — it passes both eligibility signals.
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator-script.md", minimalEligibleAgentBytes())

	// Derive catalog keys through the production path so both exclusion channels are
	// exercised together: catalogAgentKeys must include "orchestrator-script".
	catalogKeys := catalogAgentKeys(cat)

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, catalogKeys)

	// Assert — the orchestrator-script.md must not appear in the harness-only set.
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for an orchestrator-script.md "+
			"whose catalog exposes the script orchestrator; want 0 — a file with a generic "+
			"catalog counterpart must not be classified as harness-only",
			len(result))
		for _, r := range result {
			t.Logf("  unexpected result: TargetPath=%q Key=%q", r.TargetPath, r.Key)
		}
	}
}

// TestScanHarnessOnlyAgents_OrchestratorScriptFileName_ExcludedByFilename verifies that a
// deployed orchestrator-script.md is excluded from the harness-only result even when the
// catalog key is not present in the supplied catalogKeys map — the filename exclusion via
// isOrchestratorFileName is an independent second channel. This test isolates that channel
// by passing an empty catalogKeys.
func TestScanHarnessOnlyAgents_OrchestratorScriptFileName_ExcludedByFilename(t *testing.T) {
	// Arrange — no catalog keys at all; the exclusion must come from isOrchestratorFileName.
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator-script.md", minimalEligibleAgentBytes())

	// Act — empty catalogKeys: only the filename exclusion channel can exclude this file.
	result := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert — excluded by isOrchestratorFileName before the catalog-key check is reached.
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for \"orchestrator-script.md\" "+
			"with empty catalogKeys; want 0 — isOrchestratorFileName must recognise the "+
			"script-orchestrator filename and exclude it from harness-only detection",
			len(result))
	}
}

// TestScanHarnessOnlyAgents_OrchestratorScriptExcluded_GenuineHarnessOnlyStillDetected
// verifies the negative counterpart: when orchestrator-script.md is present alongside a
// genuine hand-authored harness-only agent with no catalog counterpart, the scan still
// returns the genuine harness-only file. The two-signal eligibility rule is unchanged in
// purpose and behaviour; only the orchestrator-script.md is excluded.
func TestScanHarnessOnlyAgents_OrchestratorScriptExcluded_GenuineHarnessOnlyStillDetected(t *testing.T) {
	// Arrange — catalog exposes the script orchestrator; one separate genuine harness-only file.
	cat := &miniCatalog{
		workers:              []domain.Agent{{Key: "worker-agent"}},
		orchestrator:         domain.Agent{Key: "orchestrator"},
		scriptOrchestrator:   domain.Agent{Key: "orchestrator-script"},
		scriptOrchestratorOK: true,
	}

	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	// The script orchestrator deployed file — must be excluded.
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator-script.md", minimalEligibleAgentBytes())
	// A genuine harness-only file with no catalog counterpart — must still be returned.
	writeHarnessOnlyAgentFile(t, fullDir, "hand-authored-agent.md", minimalEligibleAgentBytes())

	catalogKeys := catalogAgentKeys(cat)

	// Act
	result := scanHarnessOnlyAgents(workspace, agentsDir, catalogKeys)

	// Assert — exactly one result: the genuine harness-only file, not the script orchestrator.
	if len(result) != 1 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s), want 1 — the genuine "+
			"harness-only file must be detected and the script orchestrator must not be",
			len(result))
		for _, r := range result {
			t.Logf("  result: TargetPath=%q Key=%q", r.TargetPath, r.Key)
		}
		return
	}
	if result[0].Key != "hand-authored-agent" {
		t.Errorf("result[0].Key = %q, want \"hand-authored-agent\"; "+
			"the genuine harness-only file must be the only result",
			result[0].Key)
	}
}
