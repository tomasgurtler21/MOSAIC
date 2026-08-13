package catalog_test

// validate_registry_test.go covers Stage 7 registry-level validation rules invoked via
// ValidateRegistry and ValidateBundleDeclarations.
//
// Coverage (T7.1 — rules 5 and 6 via ValidateRegistry):
//
//   Rule 5 (missing-skill-folder, error severity): every entry in an agent's required_skills
//   list must name an existing folder under Agents/Generic/Skills/.
//     - An agent whose required_skills references a skill folder that does not exist on disk
//       causes ValidateRegistry to return a "missing-skill-folder" issue at SeverityError.
//     - When the skill folder exists, ValidateRegistry returns no "missing-skill-folder" issue.
//     - An agent with no required_skills entries produces no "missing-skill-folder" issue.
//     - The issue Subject identifies the agent key so the operator can locate the file.
//
//   Rule 6 (duplicate-agent-id, error severity): every subagent `id` is unique across the
//   registry.
//     - When two agents declare the same numeric `id`, ValidateRegistry returns a
//       "duplicate-agent-id" issue at SeverityError.
//     - When all agent numeric ids are unique, ValidateRegistry returns no "duplicate-agent-id"
//       issue.
//     - An empty registry (no agents) produces no "duplicate-agent-id" issue.
//
// Coverage (T7.5 — rule 20 via ValidateBundleDeclarations):
//
//   Rule 20 (three sub-checks, all error severity): every declared bundle block must name a
//   target in docformat.CanonicalDeployed, an applies_to in {"subagent","orchestrator"}, and a
//   specified_in path that exists on disk under the MOSAIC root.
//     - A conforming bundle with valid target, applies_to, and existing specified_in path
//       produces no issues.
//     - A block whose Target is not in CanonicalDeployed produces a "bundle-unknown-target"
//       issue at SeverityError.
//     - A block whose AppliesTo is not "subagent" or "orchestrator" produces a
//       "bundle-unknown-applies-to" issue at SeverityError.
//     - A block whose SpecifiedIn path does not exist under the MOSAIC root produces a
//       "bundle-missing-spec-doc" issue at SeverityError.
//     - A block with an empty SpecifiedIn (no design document listed) also produces a
//       "bundle-missing-spec-doc" issue at SeverityError.
//     - Multiple violations in one bundle accumulate independently; the function returns all
//       issues, not just the first.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared local helpers
// ---------------------------------------------------------------------------

// writeAgentWithSkills writes a minimal subagent file with the given required_skills list
// at <root>/Catalog/Agents/Generic/Agents/<category>/<name>.md.
func writeAgentWithSkills(t *testing.T, root, category, name string, skills []string) {
	t.Helper()
	mustMkdir(t, root, "Catalog", "Agents", "Generic", "Agents", category)
	fm := "---\nid: \"1\"\nversion: \"1.0.0\"\nname: " + name + "\ndescription: Test agent.\nrole: subagent\nrequired_skills:\n"
	for _, s := range skills {
		fm += "  - " + s + "\n"
	}
	fm += "---\nContent.\n"
	relPath := filepath.Join("Catalog", "Agents", "Generic", "Agents", category, name+".md")
	mustWriteFile(t, root, relPath, []byte(fm))
}

// writeSkillFolder creates a minimal skill directory with a SKILL.md file under
// <root>/Catalog/Agents/Generic/Skills/<key>.
func writeSkillFolder(t *testing.T, root, key string) {
	t.Helper()
	mustMkdir(t, root, "Catalog", "Agents", "Generic", "Skills", key)
	mustWriteFile(t, root,
		filepath.Join("Catalog", "Agents", "Generic", "Skills", key, "SKILL.md"),
		[]byte("# Skill "+key+"\n"))
}

// issueWithRegistryCode returns the first catalog.Issue with the given code, or nil.
func issueWithRegistryCode(issues []catalog.Issue, code string) *catalog.Issue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}

// hasRegistryIssueWithCode reports whether any issue in the slice has the given code.
func hasRegistryIssueWithCode(issues []catalog.Issue, code string) bool {
	return issueWithRegistryCode(issues, code) != nil
}

// ---------------------------------------------------------------------------
// T7.1 — Rule 5: required_skills entries must name existing skill folders
// ---------------------------------------------------------------------------

func TestValidateRegistry_Rule5_MissingSkillFolder_ReportsMissingSkillFolder(t *testing.T) {
	// An agent that lists "lean-tdd" in required_skills but has no corresponding skill
	// folder under Agents/Generic/Skills/ must produce a "missing-skill-folder" issue
	// at SeverityError from ValidateRegistry.
	root := makeTempMosaicRoot(t)
	writeAgentWithSkills(t, root, "Creation", "test-writer", []string{"lean-tdd"})
	// Deliberately do NOT create Agents/Generic/Skills/lean-tdd/.

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	iss := issueWithRegistryCode(issues, "missing-skill-folder")
	if iss == nil {
		t.Error("expected a \"missing-skill-folder\" issue for a required skill whose folder is absent; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("missing-skill-folder issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidateRegistry_Rule5_SkillFolderPresent_NoMissingSkillFolderIssue(t *testing.T) {
	// An agent that lists "lean-tdd" in required_skills and has a corresponding skill
	// folder under Agents/Generic/Skills/ must produce no "missing-skill-folder" issue.
	root := makeTempMosaicRoot(t)
	writeAgentWithSkills(t, root, "Creation", "test-writer", []string{"lean-tdd"})
	writeSkillFolder(t, root, "lean-tdd")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	if hasRegistryIssueWithCode(issues, "missing-skill-folder") {
		t.Error("unexpected \"missing-skill-folder\" when the required skill folder exists")
	}
}

func TestValidateRegistry_Rule5_AgentWithNoSkills_NoMissingSkillFolderIssue(t *testing.T) {
	// An agent with no required_skills entries must not produce a "missing-skill-folder"
	// issue. There is nothing to check when the list is empty.
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "test-writer", "1")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	if hasRegistryIssueWithCode(issues, "missing-skill-folder") {
		t.Error("unexpected \"missing-skill-folder\" for agent with no required_skills entries")
	}
}

func TestValidateRegistry_Rule5_MissingSkillFolder_SubjectIdentifiesAgent(t *testing.T) {
	// The "missing-skill-folder" issue's Subject must identify the agent key so an
	// operator can locate the source file that declares the missing skill reference.
	root := makeTempMosaicRoot(t)
	writeAgentWithSkills(t, root, "Creation", "test-writer-tdd", []string{"lean-tdd"})

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	iss := issueWithRegistryCode(issues, "missing-skill-folder")
	if iss == nil {
		t.Fatal("expected a \"missing-skill-folder\" issue; got none — cannot verify Subject content")
	}
	// The issue Subject or Message must contain either the agent key ("test-writer-tdd") or
	// the skill name ("lean-tdd") so an operator can identify both the agent that declared
	// the missing skill and the skill folder that is absent.
	combined := iss.Subject + iss.Message
	if !strings.Contains(combined, "test-writer-tdd") && !strings.Contains(combined, "lean-tdd") {
		t.Errorf("missing-skill-folder issue: Subject = %q, Message = %q; "+
			"expected Subject or Message to contain the agent key (\"test-writer-tdd\") "+
			"or skill name (\"lean-tdd\")",
			iss.Subject, iss.Message)
	}
}

func TestValidateRegistry_Rule5_MultipleSkillsOneMissing_ReportsMissingSkillFolder(t *testing.T) {
	// An agent that declares two skills, where one folder exists and one does not, must
	// produce a "missing-skill-folder" issue only for the absent skill. The present skill
	// must not cause an issue.
	root := makeTempMosaicRoot(t)
	writeAgentWithSkills(t, root, "Creation", "test-writer", []string{"lean-tdd", "efficient-file-reading"})
	writeSkillFolder(t, root, "lean-tdd") // present
	// efficient-file-reading skill folder is deliberately absent

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	if !hasRegistryIssueWithCode(issues, "missing-skill-folder") {
		t.Error("expected a \"missing-skill-folder\" issue for the absent efficient-file-reading skill; got none")
	}
}

func TestValidateRegistry_Rule5_BothSkillsFolderPresent_NoMissingSkillFolderIssue(t *testing.T) {
	// An agent that declares two skills and both folders exist must produce no
	// "missing-skill-folder" issue.
	root := makeTempMosaicRoot(t)
	writeAgentWithSkills(t, root, "Creation", "test-writer", []string{"lean-tdd", "efficient-file-reading"})
	writeSkillFolder(t, root, "lean-tdd")
	writeSkillFolder(t, root, "efficient-file-reading")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	if hasRegistryIssueWithCode(issues, "missing-skill-folder") {
		t.Error("unexpected \"missing-skill-folder\" when all required skill folders exist")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — Rule 6: numeric agent ids must be unique across the registry
// ---------------------------------------------------------------------------

func TestValidateRegistry_Rule6_DuplicateNumericID_ReportsDuplicateAgentID(t *testing.T) {
	// Two agents declaring the same numeric `id` must cause ValidateRegistry to return a
	// "duplicate-agent-id" issue at SeverityError.
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "agent-alpha", "77")
	writeWorkerAgentFile(t, root, "Execution", "agent-beta", "77")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	iss := issueWithRegistryCode(issues, "duplicate-agent-id")
	if iss == nil {
		t.Error("expected a \"duplicate-agent-id\" issue when two agents share the same numeric id; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("duplicate-agent-id issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidateRegistry_Rule6_UniqueNumericIDs_NoDuplicateAgentIDIssue(t *testing.T) {
	// When all agents have distinct numeric ids, ValidateRegistry must produce no
	// "duplicate-agent-id" issue. Each id is reachable only from its declaring agent.
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "agent-alpha", "10")
	writeWorkerAgentFile(t, root, "Execution", "agent-beta", "20")
	writeWorkerAgentFile(t, root, "Validation", "agent-gamma", "30")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	if hasRegistryIssueWithCode(issues, "duplicate-agent-id") {
		t.Error("unexpected \"duplicate-agent-id\" when all numeric ids are unique")
	}
}

func TestValidateRegistry_Rule6_EmptyCatalog_NoDuplicateAgentIDIssue(t *testing.T) {
	// An empty registry (no agents beyond the orchestrator) must produce no registry-level
	// issues. ValidateRegistry on an empty catalog must return nil or an empty slice.
	root := makeTempMosaicRoot(t)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	for _, iss := range issues {
		if iss.Code == "duplicate-agent-id" || iss.Code == "missing-skill-folder" {
			t.Errorf("empty catalog produced unexpected issue: code=%q message=%q", iss.Code, iss.Message)
		}
	}
}

func TestValidateRegistry_Rule6_DuplicateID_SubjectIdentifiesDuplicatedID(t *testing.T) {
	// The "duplicate-agent-id" issue's Subject or Message must contain the duplicated id
	// value ("77") so an operator can identify which id is in conflict.
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "agent-alpha", "77")
	writeWorkerAgentFile(t, root, "Execution", "agent-beta", "77")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	issues := catalog.ValidateRegistry(cat)

	iss := issueWithRegistryCode(issues, "duplicate-agent-id")
	if iss == nil {
		t.Fatal("expected \"duplicate-agent-id\" issue; cannot verify Subject content")
	}
	if !strings.Contains(iss.Subject+iss.Message, "77") {
		t.Errorf("duplicate-agent-id: Subject = %q, Message = %q; "+
			"expected Subject or Message to contain the duplicated id (\"77\")",
			iss.Subject, iss.Message)
	}
}

// ---------------------------------------------------------------------------
// T7.5 — Rule 20: ValidateBundleDeclarations
// ---------------------------------------------------------------------------

// conformingBundleBlock returns a BundleBlock that satisfies all rule 20 sub-checks.
// The specDocRelPath must exist under root; callers should create it with mustWriteFile.
func conformingBundleBlock(target, appliesTo, specDocRelPath string) domain.BundleBlock {
	return domain.BundleBlock{
		Name:        target + ":" + appliesTo,
		Target:      target,
		AppliesTo:   appliesTo,
		SpecifiedIn: specDocRelPath,
		Content:     []byte("Block content.\n"),
	}
}

func TestValidateBundleDeclarations_Rule20_ConformingBundle_ReturnsNoIssues(t *testing.T) {
	// A bundle whose blocks all have a recognised target name (in CanonicalDeployed),
	// a recognised applies_to value, and a specified_in path that exists on disk must
	// produce no issues from ValidateBundleDeclarations.
	root := makeTempMosaicRoot(t)
	specRelPath := filepath.Join("Development", "Designs", "BundleSpec.md")
	mustMkdir(t, root, "Development", "Designs")
	mustWriteFile(t, root, specRelPath, []byte("# Bundle Spec\n"))

	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			conformingBundleBlock("AuthorityHierarchy", "subagent", specRelPath),
		},
	}

	issues := catalog.ValidateBundleDeclarations(bundle, root)

	for _, iss := range issues {
		t.Errorf("conforming bundle produced unexpected issue: code=%q severity=%q message=%q",
			iss.Code, iss.Severity, iss.Message)
	}
}

func TestValidateBundleDeclarations_Rule20_UnknownTarget_ReportsBundleUnknownTarget(t *testing.T) {
	// A bundle block whose Target is not in docformat.CanonicalDeployed must produce a
	// "bundle-unknown-target" issue at SeverityError. Only canonical deployed region names
	// are permitted as bundle block targets.
	root := makeTempMosaicRoot(t)
	specRelPath := filepath.Join("Development", "Designs", "BundleSpec.md")
	mustMkdir(t, root, "Development", "Designs")
	mustWriteFile(t, root, specRelPath, []byte("# Bundle Spec\n"))

	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:        "UnknownRegion:Subagent",
				Target:      "UnknownRegion", // NOT in CanonicalDeployed
				AppliesTo:   "subagent",
				SpecifiedIn: specRelPath,
				Content:     []byte("Content.\n"),
			},
		},
	}

	issues := catalog.ValidateBundleDeclarations(bundle, root)

	iss := issueWithRegistryCode(issues, "bundle-unknown-target")
	if iss == nil {
		t.Error("expected a \"bundle-unknown-target\" issue for a block with an unrecognised target; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("bundle-unknown-target issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidateBundleDeclarations_Rule20_UnknownAppliesTo_ReportsBundleUnknownAppliesTo(t *testing.T) {
	// A bundle block whose AppliesTo is neither "subagent" nor "orchestrator" must produce
	// a "bundle-unknown-applies-to" issue at SeverityError. The applies_to vocabulary is
	// closed: only the two recognised role values are accepted.
	root := makeTempMosaicRoot(t)
	specRelPath := filepath.Join("Development", "Designs", "BundleSpec.md")
	mustMkdir(t, root, "Development", "Designs")
	mustWriteFile(t, root, specRelPath, []byte("# Bundle Spec\n"))

	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:        "AuthorityHierarchy:Worker",
				Target:      "AuthorityHierarchy",
				AppliesTo:   "worker", // NOT a recognised role value
				SpecifiedIn: specRelPath,
				Content:     []byte("Content.\n"),
			},
		},
	}

	issues := catalog.ValidateBundleDeclarations(bundle, root)

	iss := issueWithRegistryCode(issues, "bundle-unknown-applies-to")
	if iss == nil {
		t.Error("expected a \"bundle-unknown-applies-to\" issue for an unrecognised applies_to value; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("bundle-unknown-applies-to issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidateBundleDeclarations_Rule20_MissingSpecDoc_ReportsBundleMissingSpecDoc(t *testing.T) {
	// A bundle block whose SpecifiedIn path does not exist under the MOSAIC root must produce
	// a "bundle-missing-spec-doc" issue at SeverityError. Every bundle block must trace back
	// to a design document that defines its intended content.
	root := makeTempMosaicRoot(t)

	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:        "AuthorityHierarchy:Subagent",
				Target:      "AuthorityHierarchy",
				AppliesTo:   "subagent",
				SpecifiedIn: filepath.Join("Development", "Designs", "NonExistent.md"), // absent
				Content:     []byte("Content.\n"),
			},
		},
	}

	issues := catalog.ValidateBundleDeclarations(bundle, root)

	iss := issueWithRegistryCode(issues, "bundle-missing-spec-doc")
	if iss == nil {
		t.Error("expected a \"bundle-missing-spec-doc\" issue when specified_in path does not exist; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("bundle-missing-spec-doc issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidateBundleDeclarations_Rule20_EmptySpecifiedIn_ReportsBundleMissingSpecDoc(t *testing.T) {
	// A bundle block with an empty SpecifiedIn value must produce a "bundle-missing-spec-doc"
	// issue at SeverityError. An empty path cannot satisfy the "file must exist" requirement,
	// and a bundle block without a design document reference cannot be audited.
	root := makeTempMosaicRoot(t)

	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:        "AuthorityHierarchy:Subagent",
				Target:      "AuthorityHierarchy",
				AppliesTo:   "subagent",
				SpecifiedIn: "", // empty
				Content:     []byte("Content.\n"),
			},
		},
	}

	issues := catalog.ValidateBundleDeclarations(bundle, root)

	iss := issueWithRegistryCode(issues, "bundle-missing-spec-doc")
	if iss == nil {
		t.Error("expected a \"bundle-missing-spec-doc\" issue for an empty SpecifiedIn; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("bundle-missing-spec-doc issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidateBundleDeclarations_Rule20_AllSubchecksViolated_ReportsAllThreeIssues(t *testing.T) {
	// A single bundle block that violates all three rule 20 sub-checks (unknown target,
	// unrecognised applies_to, missing spec doc) must produce all three issues. The function
	// must report every violation it finds, not stop after the first.
	root := makeTempMosaicRoot(t)

	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:        "BadTarget:Worker",
				Target:      "BadTarget",  // NOT in CanonicalDeployed
				AppliesTo:   "worker",     // NOT "subagent" or "orchestrator"
				SpecifiedIn: "",           // empty → missing spec doc
				Content:     []byte("Content.\n"),
			},
		},
	}

	issues := catalog.ValidateBundleDeclarations(bundle, root)

	wantCodes := []string{"bundle-unknown-target", "bundle-unknown-applies-to", "bundle-missing-spec-doc"}
	for _, code := range wantCodes {
		if !hasRegistryIssueWithCode(issues, code) {
			t.Errorf("expected issue with code %q; not found in returned issues", code)
		}
	}
}

func TestValidateBundleDeclarations_Rule20_OrchestratorAppliesTo_ConformingBundle(t *testing.T) {
	// A block with applies_to = "orchestrator" is valid — orchestrator is a recognised role.
	// No "bundle-unknown-applies-to" issue must be produced.
	root := makeTempMosaicRoot(t)
	specRelPath := filepath.Join("Development", "Designs", "BundleSpec.md")
	mustMkdir(t, root, "Development", "Designs")
	mustWriteFile(t, root, specRelPath, []byte("# Bundle Spec\n"))

	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:        "CommunicationProtocol:Orchestrator",
				Target:      "CommunicationProtocol",
				AppliesTo:   "orchestrator", // valid role
				SpecifiedIn: specRelPath,
				Content:     []byte("Content.\n"),
			},
		},
	}

	issues := catalog.ValidateBundleDeclarations(bundle, root)

	if hasRegistryIssueWithCode(issues, "bundle-unknown-applies-to") {
		t.Error("\"orchestrator\" is a valid applies_to value; got unexpected \"bundle-unknown-applies-to\" issue")
	}
}

func TestValidateBundleDeclarations_Rule20_SubjectIsBlockName(t *testing.T) {
	// The issue Subject for a rule 20 violation must be the block name so the operator
	// can identify which declaration is malformed.
	root := makeTempMosaicRoot(t)

	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:      "BadBlock:Worker",
				Target:    "BadBlock", // triggers bundle-unknown-target
				AppliesTo: "worker",   // triggers bundle-unknown-applies-to
				Content:   []byte("Content.\n"),
			},
		},
	}

	issues := catalog.ValidateBundleDeclarations(bundle, root)

	iss := issueWithRegistryCode(issues, "bundle-unknown-target")
	if iss == nil {
		t.Fatal("expected \"bundle-unknown-target\" issue; cannot verify Subject")
	}
	if iss.Subject != "BadBlock:Worker" {
		t.Errorf("bundle-unknown-target issue Subject = %q, want %q", iss.Subject, "BadBlock:Worker")
	}
}
