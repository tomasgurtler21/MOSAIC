package docformat_test

// validate_stage7_test.go covers Stage 7 validator rule implementation for docformat-level
// rules: frontmatter checks (rules 1–4), structure checks (8a, 8b, 8c, 12), and content
// checks (15–19). Registry-level rules (5, 6) and bundle conformance rules (20–22) are
// tested in the catalog and app packages respectively.
//
// Every test follows the pattern:
//   - Conforming case: a structurally complete document produces no issue with the given code.
//   - Violating case: a document breaking the rule produces an issue with the expected code
//     and severity.
//
// Test helpers (parsedBoundaryFixture, issueWithCode, hasIssueWithCode) are defined in
// body_validate_test.go and available to all files in the docformat_test package.
//
// Coverage (T7.1 — Frontmatter rules 1 through 4):
//
//   Rule 1: All source fields present; `id` present exactly when role is subagent.
//     - A conforming subagent source document with all required fields produces no
//       "missing-frontmatter-field" or "unexpected-id" issue.
//     - A source document missing the `name` field produces a "missing-frontmatter-field"
//       issue at SeverityError.
//     - A source document missing the `version` field produces a "missing-frontmatter-field"
//       issue at SeverityError.
//     - A source document missing the `description` field produces a "missing-frontmatter-field"
//       issue at SeverityError.
//     - A source document missing the `role` field produces a "missing-frontmatter-field"
//       issue at SeverityError.
//     - A subagent source document missing the `id` field produces a "missing-frontmatter-field"
//       issue at SeverityError.
//     - An orchestrator source document carrying an `id` field produces an "unexpected-id"
//       issue at SeverityError.
//     - A conforming orchestrator source document (no `id`) produces no "unexpected-id" issue.
//
//   Rule 2: `role` is "subagent" or "orchestrator".
//     - A document with a recognised `role` value produces no "invalid-role" issue.
//     - A document with an unrecognised `role` value produces an "invalid-role" issue at
//       SeverityError.
//
//   Rule 3: `name` matches the file's base name.
//     - When FileBaseName matches the `name` field, no "name-path-mismatch" issue is produced.
//     - When FileBaseName differs from the `name` field, a "name-path-mismatch" issue is
//       produced at SeverityError.
//     - When FileBaseName is the empty string (not provided), rule 3 is skipped entirely.
//
//   Rule 4: `version` parses as semver.
//     - A document with a well-formed semver version produces no "invalid-version" issue.
//     - A document with a malformed version string produces an "invalid-version" issue at
//       SeverityError.
//
// Coverage (T7.2 — Structure rules 8a, 8b, 8c):
//
//   Rule 8a: <Name type="managed"> CommunicationProtocol must be present — Error.
//     - A document containing <CommunicationProtocol type="managed"> at body top level produces
//       no "missing-contract-region" issue.
//     - A document without <CommunicationProtocol type="managed"> produces a "missing-contract-region"
//       issue at SeverityError.
//
//   Rule 8b: The five conduct regions must be present for role: subagent — Warning.
//     - A subagent document with all five conduct regions produces no "missing-conduct-region"
//       issue.
//     - A subagent document missing at least one conduct region produces a
//       "missing-conduct-region" issue at SeverityWarning.
//     - An orchestrator document missing all five conduct regions produces no
//       "missing-conduct-region" issue (not required for orchestrators).
//
//   Rule 8c: No region present that the role forbids — Warning.
//     - An orchestrator document without any subagent-only bundle regions produces no
//       "role-forbidden-region" issue.
//     - An orchestrator document containing a subagent-only bundle region produces a
//       "role-forbidden-region" issue at SeverityWarning.
//     - A subagent document with the five bundle regions produces no "role-forbidden-region"
//       issue (the bundle regions are permitted for subagents).
//
// Coverage (T7.3 — Structure rule 12):
//
//   Rule 12: In a source file, every deployed region (<Name type="managed">) is empty — Error.
//     - A source document with empty deployed regions produces no "populated-source-region"
//       issue.
//     - A source document with content inside a deployed region produces a
//       "populated-source-region" issue at SeverityError.
//     - A deployed document (Kind = DocumentDeployed) with content inside <Name type="managed"> produces
//       no "populated-source-region" issue (rule applies only to source files).
//     - When Kind is DocumentUnknown (zero value), rule 12 is skipped.
//
// Coverage (T7.4 — Content rules 15 through 19):
//
//   Rule 15: Identity section shape — Warning.
//     - A document whose Identity section contains **Goal:**, **Scope:** with a DO and
//       a DO NOT bullet, **Litmus Test:**, and a ### Process list produces no "identity-shape"
//       issue.
//     - A document whose Identity section is missing **Goal:** produces an "identity-shape"
//       issue at SeverityWarning.
//     - A document whose Identity section is missing ### Process produces an "identity-shape"
//       issue at SeverityWarning.
//     - A document with no Identity section does not produce an "identity-shape" issue.
//
//   Rule 16: No section outside OutputFormat contains a JSON object with a status_code key —
//   Warning.
//     - A document with a status_code JSON object only inside OutputFormat produces no
//       "contract-restated" issue.
//     - A document with a status_code JSON object inside a non-OutputFormat section produces
//       a "contract-restated" issue at SeverityWarning.
//
//   Rule 17: OutputFormat section contains no JSON code fence — Warning.
//     - An OutputFormat section containing only a markdown table produces no
//       "output-format-json-fence" issue.
//     - An OutputFormat section containing a JSON code fence (```json) produces an
//       "output-format-json-fence" issue at SeverityWarning.
//
//   Rule 18: No Process step mentions human-in-the-loop — Warning.
//     - A Process list with no step mentioning "human-in-the-loop" produces no
//       "process-hitl-step" issue.
//     - A Process list with a step mentioning "human-in-the-loop" produces a
//       "process-hitl-step" issue at SeverityWarning.
//
//   Rule 19: Every skill named in a Process step appears in required_skills, and vice
//   versa — Error.
//     - A document whose Process steps mention exactly the skills in required_skills produces
//       no "skill-declaration-mismatch" issue.
//     - A document whose Process steps mention a skill not in required_skills produces a
//       "skill-declaration-mismatch" issue at SeverityError.
//     - A document with a skill in required_skills not mentioned in any Process step produces
//       a "skill-declaration-mismatch" issue at SeverityError.
//
// Coverage (T7.6 — Strict mode with Stage 7 warning codes):
//
//   - A warning-level finding from a Stage 7 rule (e.g., "missing-conduct-region") carries
//     SeverityWarning when Strict is false.
//   - The same finding carries SeverityError when Strict is true (promoted by strict mode).
//   - Strict mode does not change the severity of SeverityAdvice findings.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Inline document fixtures
// ---------------------------------------------------------------------------

// conformingSubagentDoc is a minimal source document for a subagent with all required
// frontmatter fields present. It satisfies rules 1–4 (no violations).
const conformingSubagentDoc = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent used in Stage 7 unit tests.
role: subagent
required_skills: []
---

<Identity type="core">
**Goal:** Do something useful.

**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** If it involves writing tests, you handle it.

### Process
1. Read the design.
2. Write tests.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// conformingOrchestratorDoc is a minimal source document for an orchestrator. It has no
// `id` field (orchestrators must not carry one) and role: orchestrator.
const conformingOrchestratorDoc = `---
version: 1.0.0
name: orchestrator
description: The orchestrator agent.
role: orchestrator
---

<Identity type="core">
Content.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// subagentWithAllConductRegions has all five conduct regions required for subagents.
const subagentWithAllConductRegions = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<Identity type="core">
<AuthorityHierarchy type="managed">
</AuthorityHierarchy>
<ClosingProcedure type="managed">
</ClosingProcedure>
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Constraints type="core">
<ProtocolConstraints type="managed">
</ProtocolConstraints>
</Constraints>

<ErrorHandling type="core">
<ErrorHandlingCommon type="managed">
</ErrorHandlingCommon>
</ErrorHandling>

<ExecutionPhilosophy type="core">
<ExecutionPhilosophyCommon type="managed">
</ExecutionPhilosophyCommon>
</ExecutionPhilosophy>
`

// subagentMissingOneConductRegion is missing ExecutionPhilosophyCommon.
const subagentMissingOneConductRegion = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<Identity type="core">
<AuthorityHierarchy type="managed">
</AuthorityHierarchy>
<ClosingProcedure type="managed">
</ClosingProcedure>
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Constraints type="core">
<ProtocolConstraints type="managed">
</ProtocolConstraints>
</Constraints>

<ErrorHandling type="core">
<ErrorHandlingCommon type="managed">
</ErrorHandlingCommon>
</ErrorHandling>

<ExecutionPhilosophy type="core">
Content without the ExecutionPhilosophyCommon region.
</ExecutionPhilosophy>
`

// orchestratorConformingDoc is an orchestrator without any subagent-only bundle regions.
const orchestratorConformingDoc = `---
version: 1.0.0
name: orchestrator
description: The orchestrator agent.
role: orchestrator
---

<Identity type="core">
Content.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// orchestratorWithBundleRegion is an orchestrator that carries AuthorityHierarchy — a
// subagent-only bundle region. This violates rule 8c.
const orchestratorWithBundleRegion = `---
version: 1.0.0
name: orchestrator
description: The orchestrator agent.
role: orchestrator
---

<Identity type="core">
<AuthorityHierarchy type="managed">
</AuthorityHierarchy>
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// sourceDocWithEmptyDeployedRegions has a <CommunicationProtocol type="managed"> with no inner
// content. This is the correct source-file form.
const sourceDocWithEmptyDeployedRegions = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// sourceDocWithPopulatedDeployedRegion has content inside <CommunicationProtocol type="managed">.
// This violates rule 12 for source files.
const sourceDocWithPopulatedDeployedRegion = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<CommunicationProtocol type="managed">
This content must not appear in a source file's deployed region.
</CommunicationProtocol>
`

// identityWithFullShape has all required elements of the Identity section (rule 15).
const identityWithFullShape = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<Identity type="core">
**Goal:** Do something useful for the user.

**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** If it involves writing tests, you handle it.

### Process
1. Read the design.
2. Write tests.
3. Return result.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// identityMissingGoal is missing the **Goal:** element from the Identity section.
const identityMissingGoal = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<Identity type="core">
**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** Test.

### Process
1. Do something.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// identityMissingProcess is missing the ### Process subsection.
const identityMissingProcess = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<Identity type="core">
**Goal:** Do something.

**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** If it involves tests, you handle it.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// noIdentitySection is a document without an Identity section.
const noIdentitySection = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Capabilities type="core">
Content.
</Capabilities>
`

// statusCodeInConstraints has a JSON status_code object inside the Constraints section
// rather than inside OutputFormat. This violates rule 16.
const statusCodeInConstraints = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Constraints type="core">
Some constraint prose.

{"status_code": "SUCCESS", "agent_instance_id": "test-writer#1", "run_id": "abc123"}

More content.
</Constraints>
`

// statusCodeOnlyInOutputFormat has a status_code JSON only inside OutputFormat, which is
// the permitted location. No rule 16 violation.
const statusCodeOnlyInOutputFormat = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<OutputFormat type="core">
| Status | error_code | Example status_message |
|--------|------------|------------------------|
| SUCCESS | — | "Done." |
| BLOCKED | E101 | "Input not found." |
</OutputFormat>
`

// outputFormatWithJsonFence has a JSON code fence inside OutputFormat. This violates rule 17.
const outputFormatWithJsonFence = "---\nid: \"1\"\nversion: 1.0.0\nname: test-agent\ndescription: A test agent.\nrole: subagent\nrequired_skills: []\n---\n\n<CommunicationProtocol type=\"managed\">\n</CommunicationProtocol>\n\n<OutputFormat type=\"core\">\nReturn your response as:\n\n```json\n{\"status_code\": \"SUCCESS\"}\n```\n</OutputFormat>\n"

// outputFormatWithTable has only a markdown table in OutputFormat — no JSON fence.
// No rule 17 violation.
const outputFormatWithTable = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<OutputFormat type="core">
Your entire response is the JSON object the Communication Protocol defines. This section
specifies only what your status_message should say.

| Status | error_code | Example status_message |
|--------|------------|------------------------|
| SUCCESS | — | "All tests written." |
</OutputFormat>
`

// processWithHITLStep has a Process step mentioning "human-in-the-loop". Rule 18 violation.
const processWithHITLStep = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<Identity type="core">
**Goal:** Do something.

**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** Test.

### Process
1. Read the design.
2. If human_in_the_loop is true, present output to the user for review.
3. Return the response.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// processWithoutHITLStep has no HITL mention in Process steps. No rule 18 violation.
const processWithoutHITLStep = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<Identity type="core">
**Goal:** Do something.

**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** Test.

### Process
1. Read the design.
2. Write tests.
3. Return the response.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// skillsMatchedInProcessAndRequired has lean-tdd both in required_skills and in a Process
// step. No rule 19 violation.
const skillsMatchedInProcessAndRequired = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills:
  - lean-tdd
---

<Identity type="core">
**Goal:** Do something.

**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** Test.

### Process
1. Load the ` + "`lean-tdd`" + ` skill for test quality principles.
2. Write tests.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// skillInProcessNotInRequired references lean-tdd in a Process step but does not list it
// in required_skills. Violates rule 19.
const skillInProcessNotInRequired = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills: []
---

<Identity type="core">
**Goal:** Do something.

**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** Test.

### Process
1. Load the ` + "`lean-tdd`" + ` skill for test quality principles.
2. Write tests.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// skillInRequiredNotInProcess lists lean-tdd in required_skills but does not mention it in
// any Process step. Violates rule 19.
const skillInRequiredNotInProcess = `---
id: "1"
version: 1.0.0
name: test-agent
description: A test agent.
role: subagent
required_skills:
  - lean-tdd
---

<Identity type="core">
**Goal:** Do something.

**Scope:**
- You DO: Write tests.
- You DO NOT: Write implementation code.

**Litmus Test:** Test.

### Process
1. Read the design.
2. Write tests.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseInlineDoc parses an inline document string. Fails the test if Parse returns an error.
func parseInlineDoc(t *testing.T, src string) *docformat.Document {
	t.Helper()
	doc, err := docformat.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

// ---------------------------------------------------------------------------
// T7.1 — Rule 1: Source fields present; id present iff role is subagent
// ---------------------------------------------------------------------------

func TestValidate_Rule1_ConformingSubagent_NoMissingFieldIssue(t *testing.T) {
	// A source document with all required frontmatter fields and an id field (subagent)
	// must produce no "missing-frontmatter-field" or "unexpected-id" issue.
	doc := parseInlineDoc(t, conformingSubagentDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	for _, iss := range issues {
		if iss.Code == "missing-frontmatter-field" || iss.Code == "unexpected-id" {
			t.Errorf("conforming subagent produced unexpected issue: code=%q severity=%q message=%q",
				iss.Code, iss.Severity, iss.Message)
		}
	}
}

func TestValidate_Rule1_SourceSubagentMissingName_ReportsMissingFrontmatterField(t *testing.T) {
	// A source subagent document missing the `name` frontmatter field must produce a
	// "missing-frontmatter-field" issue at SeverityError.
	src := "---\nid: \"1\"\nversion: 1.0.0\ndescription: A test.\nrole: subagent\nrequired_skills: []\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	iss := issueWithCode(issues, "missing-frontmatter-field")
	if iss == nil {
		t.Error("expected a \"missing-frontmatter-field\" issue for a subagent source document missing `name`; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("missing-frontmatter-field issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule1_SourceSubagentMissingVersion_ReportsMissingFrontmatterField(t *testing.T) {
	// A source subagent document missing the `version` frontmatter field must produce a
	// "missing-frontmatter-field" issue at SeverityError.
	src := "---\nid: \"1\"\nname: test-agent\ndescription: A test.\nrole: subagent\nrequired_skills: []\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	iss := issueWithCode(issues, "missing-frontmatter-field")
	if iss == nil {
		t.Error("expected a \"missing-frontmatter-field\" issue for a subagent source document missing `version`; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("missing-frontmatter-field issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule1_SourceSubagentMissingDescription_ReportsMissingFrontmatterField(t *testing.T) {
	// A source subagent document missing the `description` frontmatter field must produce a
	// "missing-frontmatter-field" issue at SeverityError.
	src := "---\nid: \"1\"\nversion: 1.0.0\nname: test-agent\nrole: subagent\nrequired_skills: []\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	iss := issueWithCode(issues, "missing-frontmatter-field")
	if iss == nil {
		t.Error("expected a \"missing-frontmatter-field\" issue for a subagent source document missing `description`; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("missing-frontmatter-field issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule1_SourceSubagentMissingRole_ReportsMissingFrontmatterField(t *testing.T) {
	// A source subagent document missing the `role` frontmatter field must produce a
	// "missing-frontmatter-field" issue at SeverityError when Kind is DocumentSource.
	src := "---\nid: \"1\"\nversion: 1.0.0\nname: test-agent\ndescription: A test.\nrequired_skills: []\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	iss := issueWithCode(issues, "missing-frontmatter-field")
	if iss == nil {
		t.Error("expected a \"missing-frontmatter-field\" issue for a source document missing `role`; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("missing-frontmatter-field issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule1_SourceSubagentMissingID_ReportsMissingFrontmatterField(t *testing.T) {
	// A subagent source document missing the `id` field must produce a
	// "missing-frontmatter-field" issue at SeverityError. Subagents are identified by numeric
	// id and must always declare one.
	src := "---\nversion: 1.0.0\nname: test-agent\ndescription: A test.\nrole: subagent\nrequired_skills: []\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	iss := issueWithCode(issues, "missing-frontmatter-field")
	if iss == nil {
		t.Error("expected a \"missing-frontmatter-field\" issue for a subagent source document missing `id`; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("missing-frontmatter-field issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule1_OrchestratorWithIDField_ReportsUnexpectedID(t *testing.T) {
	// An orchestrator source document carrying an `id` field must produce an "unexpected-id"
	// issue at SeverityError. Orchestrators are identified by key (path), not by numeric id.
	src := "---\nid: \"0\"\nversion: 1.0.0\nname: orchestrator\ndescription: The orchestrator.\nrole: orchestrator\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "orchestrator",
	})

	iss := issueWithCode(issues, "unexpected-id")
	if iss == nil {
		t.Error("expected an \"unexpected-id\" issue for an orchestrator source document carrying an `id` field; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("unexpected-id issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule1_ConformingOrchestratorNoID_NoUnexpectedIDIssue(t *testing.T) {
	// A conforming orchestrator source document (no `id` field) must produce no "unexpected-id"
	// issue. The orchestrator is identified by key alone.
	doc := parseInlineDoc(t, conformingOrchestratorDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "orchestrator",
	})

	if hasIssueWithCode(issues, "unexpected-id") {
		t.Errorf("conforming orchestrator (no id) produced unexpected \"unexpected-id\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule1_FrontmatterRulesSkippedWhenKindUnknown(t *testing.T) {
	// When Kind is DocumentUnknown (zero value), frontmatter source-field checks (rule 1)
	// are skipped. A document with missing fields must not produce frontmatter issues
	// when the validator does not know it is a source file.
	src := "---\nversion: 1.0.0\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "missing-frontmatter-field") {
		t.Error("frontmatter field check must be skipped when Kind is DocumentUnknown; " +
			"got \"missing-frontmatter-field\" issue")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — Rule 2: Role value must be "subagent" or "orchestrator"
// ---------------------------------------------------------------------------

func TestValidate_Rule2_UnrecognisedRoleValue_ReportsInvalidRole(t *testing.T) {
	// A document whose `role` frontmatter field is neither "subagent" nor "orchestrator"
	// must produce an "invalid-role" issue at SeverityError.
	src := "---\nversion: 1.0.0\nname: test-agent\ndescription: A test.\nrole: worker\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
	})

	iss := issueWithCode(issues, "invalid-role")
	if iss == nil {
		t.Error("expected an \"invalid-role\" issue for role value \"worker\" (unrecognised); got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("invalid-role issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule2_SubagentRole_NoInvalidRoleIssue(t *testing.T) {
	// A document with role: subagent must produce no "invalid-role" issue.
	doc := parseInlineDoc(t, conformingSubagentDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	if hasIssueWithCode(issues, "invalid-role") {
		t.Error("role: subagent produced unexpected \"invalid-role\" issue")
	}
}

func TestValidate_Rule2_OrchestratorRole_NoInvalidRoleIssue(t *testing.T) {
	// A document with role: orchestrator must produce no "invalid-role" issue.
	doc := parseInlineDoc(t, conformingOrchestratorDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "orchestrator",
	})

	if hasIssueWithCode(issues, "invalid-role") {
		t.Error("role: orchestrator produced unexpected \"invalid-role\" issue")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — Rule 3: name matches file base name
// ---------------------------------------------------------------------------

func TestValidate_Rule3_NameMatchesFileBaseName_NoNamePathMismatch(t *testing.T) {
	// When FileBaseName matches the document's frontmatter `name` field, no "name-path-mismatch"
	// issue must be produced.
	doc := parseInlineDoc(t, conformingSubagentDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind:         docformat.DocumentSource,
		Role:         "subagent",
		FileBaseName: "test-agent",
	})

	if hasIssueWithCode(issues, "name-path-mismatch") {
		t.Error("unexpected \"name-path-mismatch\" when FileBaseName matches the frontmatter name")
	}
}

func TestValidate_Rule3_NameDoesNotMatchFileBaseName_ReportsNamePathMismatch(t *testing.T) {
	// When FileBaseName differs from the frontmatter `name` field, a "name-path-mismatch"
	// issue must be produced at SeverityError.
	doc := parseInlineDoc(t, conformingSubagentDoc) // frontmatter name: test-agent

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind:         docformat.DocumentSource,
		Role:         "subagent",
		FileBaseName: "different-name", // deliberately mismatches
	})

	iss := issueWithCode(issues, "name-path-mismatch")
	if iss == nil {
		t.Error("expected a \"name-path-mismatch\" issue when FileBaseName does not match the frontmatter name; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("name-path-mismatch issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule3_EmptyFileBaseName_SkipsNameCheck(t *testing.T) {
	// When FileBaseName is the empty string, rule 3 is skipped. A document whose frontmatter
	// name would not match a non-empty FileBaseName must produce no "name-path-mismatch" issue.
	doc := parseInlineDoc(t, conformingSubagentDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind:         docformat.DocumentSource,
		Role:         "subagent",
		FileBaseName: "", // skips the name check
	})

	if hasIssueWithCode(issues, "name-path-mismatch") {
		t.Error("rule 3 must be skipped when FileBaseName is empty; got \"name-path-mismatch\" issue")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — Rule 4: version parses as semver
// ---------------------------------------------------------------------------

func TestValidate_Rule4_WellFormedSemver_NoInvalidVersionIssue(t *testing.T) {
	// A document with a well-formed semantic version (MAJOR.MINOR.PATCH) must produce no
	// "invalid-version" issue. Standard semver is the expected form.
	doc := parseInlineDoc(t, conformingSubagentDoc) // version: 1.0.0

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	if hasIssueWithCode(issues, "invalid-version") {
		t.Error("well-formed semver produced unexpected \"invalid-version\" issue")
	}
}

func TestValidate_Rule4_MalformedVersion_ReportsInvalidVersion(t *testing.T) {
	// A document with a malformed version string that does not conform to semver must produce
	// an "invalid-version" issue at SeverityError.
	src := "---\nid: \"1\"\nversion: not-semver\nname: test-agent\ndescription: A test.\nrole: subagent\nrequired_skills: []\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	iss := issueWithCode(issues, "invalid-version")
	if iss == nil {
		t.Error("expected an \"invalid-version\" issue for a malformed version string; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("invalid-version issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule4_SemverWithPreRelease_NoInvalidVersionIssue(t *testing.T) {
	// A version string with a pre-release suffix (1.0.0-alpha.1) is a valid semver and must
	// produce no "invalid-version" issue.
	src := "---\nid: \"1\"\nversion: 1.0.0-alpha.1\nname: test-agent\ndescription: A test.\nrole: subagent\nrequired_skills: []\n---\nBody.\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
		Role: "subagent",
	})

	if hasIssueWithCode(issues, "invalid-version") {
		t.Error("valid semver with pre-release suffix produced unexpected \"invalid-version\" issue")
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Rule 8a: CommunicationProtocol region must be present
// ---------------------------------------------------------------------------

func TestValidate_Rule8a_CommunicationProtocolPresent_NoMissingContractRegionIssue(t *testing.T) {
	// A document with <CommunicationProtocol type="managed"> at body top level must produce no
	// "missing-contract-region" issue. Rule 8a requires the contract region to be present.
	doc := parseInlineDoc(t, conformingSubagentDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "subagent",
	})

	if hasIssueWithCode(issues, "missing-contract-region") {
		t.Errorf("document with CommunicationProtocol produced unexpected \"missing-contract-region\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule8a_OrchestratorWithCommunicationProtocol_NoMissingContractRegionIssue(t *testing.T) {
	// An orchestrator document with <CommunicationProtocol type="managed"> must produce no
	// "missing-contract-region" issue. Rule 8a requires the contract region for all roles;
	// this is the conforming case paired with the orchestrator-violating case below.
	doc := parseInlineDoc(t, conformingOrchestratorDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "orchestrator",
	})

	if hasIssueWithCode(issues, "missing-contract-region") {
		t.Errorf("orchestrator with CommunicationProtocol produced unexpected \"missing-contract-region\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule8a_CommunicationProtocolAbsent_ReportsMissingContractRegion(t *testing.T) {
	// A document without <CommunicationProtocol type="managed"> must produce a
	// "missing-contract-region" issue at SeverityError. Without the Communication Protocol
	// the deployed agent has no operational contract.
	src := "---\nid: \"1\"\nversion: 1.0.0\nname: test-agent\ndescription: A test.\nrole: subagent\nrequired_skills: []\n---\n\n<Identity type=\"core\">\nContent.\n</Identity>\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "subagent",
	})

	iss := issueWithCode(issues, "missing-contract-region")
	if iss == nil {
		t.Error("expected a \"missing-contract-region\" issue when CommunicationProtocol is absent; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("missing-contract-region issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule8a_OrchestratorWithoutProtocol_ReportsMissingContractRegion(t *testing.T) {
	// Rule 8a applies to all roles. An orchestrator document without CommunicationProtocol
	// also produces a "missing-contract-region" issue at SeverityError.
	src := "---\nversion: 1.0.0\nname: orchestrator\ndescription: The orchestrator.\nrole: orchestrator\n---\n\n<Identity type=\"core\">\nContent.\n</Identity>\n"
	doc := parseInlineDoc(t, src)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "orchestrator",
	})

	iss := issueWithCode(issues, "missing-contract-region")
	if iss == nil {
		t.Error("expected a \"missing-contract-region\" issue for an orchestrator without CommunicationProtocol; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("missing-contract-region issue severity: want SeverityError, got %q", iss.Severity)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Rule 8b: Five conduct regions required for role: subagent
// ---------------------------------------------------------------------------

func TestValidate_Rule8b_SubagentWithAllConductRegions_NoMissingConductRegionIssue(t *testing.T) {
	// A subagent document with all five conduct regions (AuthorityHierarchy, ClosingProcedure,
	// ProtocolConstraints, ErrorHandlingCommon, ExecutionPhilosophyCommon) must produce no
	// "missing-conduct-region" issue.
	doc := parseInlineDoc(t, subagentWithAllConductRegions)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "subagent",
	})

	if hasIssueWithCode(issues, "missing-conduct-region") {
		t.Errorf("subagent with all five conduct regions produced unexpected \"missing-conduct-region\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule8b_SubagentMissingOneConductRegion_ReportsMissingConductRegion(t *testing.T) {
	// A subagent document missing at least one of the five conduct regions must produce a
	// "missing-conduct-region" issue. The fixture is missing ExecutionPhilosophyCommon.
	doc := parseInlineDoc(t, subagentMissingOneConductRegion)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "subagent",
	})

	iss := issueWithCode(issues, "missing-conduct-region")
	if iss == nil {
		t.Error("expected a \"missing-conduct-region\" issue for subagent missing ExecutionPhilosophyCommon; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("missing-conduct-region issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestValidate_Rule8b_OrchestratorMissingConductRegions_NoMissingConductRegionIssue(t *testing.T) {
	// An orchestrator document is not required to carry the five subagent-only conduct regions.
	// Rule 8b must not produce a "missing-conduct-region" issue for role: orchestrator.
	doc := parseInlineDoc(t, orchestratorConformingDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "orchestrator",
	})

	if hasIssueWithCode(issues, "missing-conduct-region") {
		t.Errorf("orchestrator document produced unexpected \"missing-conduct-region\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule8b_RoleEmptyString_SkipsConductRegionCheck(t *testing.T) {
	// When Role is the empty string, role-specific checks (8b and 8c) are skipped.
	// A subagent document missing conduct regions must produce no "missing-conduct-region"
	// when Role is not set.
	doc := parseInlineDoc(t, subagentMissingOneConductRegion)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "missing-conduct-region") {
		t.Error("rule 8b must be skipped when Role is empty; got \"missing-conduct-region\" issue")
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Rule 8c: No region the role forbids
// ---------------------------------------------------------------------------

func TestValidate_Rule8c_OrchestratorWithoutBundleRegions_NoRoleForbiddenRegionIssue(t *testing.T) {
	// An orchestrator document without any subagent-only bundle regions must produce no
	// "role-forbidden-region" issue.
	doc := parseInlineDoc(t, orchestratorConformingDoc)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "orchestrator",
	})

	if hasIssueWithCode(issues, "role-forbidden-region") {
		t.Errorf("conforming orchestrator produced unexpected \"role-forbidden-region\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule8c_OrchestratorWithSubagentBundleRegion_ReportsRoleForbiddenRegion(t *testing.T) {
	// An orchestrator document carrying a subagent-only bundle region (AuthorityHierarchy) must
	// produce a "role-forbidden-region" issue at SeverityWarning. Orchestrators must not carry
	// bundle regions intended exclusively for subagents.
	doc := parseInlineDoc(t, orchestratorWithBundleRegion)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "orchestrator",
	})

	iss := issueWithCode(issues, "role-forbidden-region")
	if iss == nil {
		t.Error("expected a \"role-forbidden-region\" issue for orchestrator carrying AuthorityHierarchy; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("role-forbidden-region issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestValidate_Rule8c_SubagentWithBundleRegions_NoRoleForbiddenRegionIssue(t *testing.T) {
	// A subagent document with the five bundle conduct regions must produce no
	// "role-forbidden-region" issue. These regions are permitted for subagents.
	doc := parseInlineDoc(t, subagentWithAllConductRegions)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Role: "subagent",
	})

	if hasIssueWithCode(issues, "role-forbidden-region") {
		t.Errorf("subagent with permitted bundle regions produced unexpected \"role-forbidden-region\" issue; issues: %v", issues)
	}
}

// ---------------------------------------------------------------------------
// T7.3 — Rule 12: Source files must have empty deployed regions
// ---------------------------------------------------------------------------

func TestValidate_Rule12_SourceDocWithEmptyDeployedRegion_NoPopulatedSourceRegionIssue(t *testing.T) {
	// A source document with empty <Name type="managed"> regions (the correct form) must produce no
	// "populated-source-region" issue.
	doc := parseInlineDoc(t, sourceDocWithEmptyDeployedRegions)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
	})

	if hasIssueWithCode(issues, "populated-source-region") {
		t.Errorf("source document with empty deployed regions produced unexpected \"populated-source-region\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule12_SourceDocWithPopulatedDeployedRegion_ReportsPopulatedSourceRegion(t *testing.T) {
	// A source document with content inside a <Name type="managed"> region must produce a
	// "populated-source-region" issue at SeverityError. In a source file, deployed regions
	// are empty placeholders; content there is either a hand-edit or a deployed file
	// committed as source.
	doc := parseInlineDoc(t, sourceDocWithPopulatedDeployedRegion)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentSource,
	})

	iss := issueWithCode(issues, "populated-source-region")
	if iss == nil {
		t.Error("expected a \"populated-source-region\" issue for a source document with content inside a deployed region; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("populated-source-region issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule12_DeployedDocWithPopulatedRegion_NoPopulatedSourceRegionIssue(t *testing.T) {
	// A deployed document (Kind = DocumentDeployed) is permitted to carry content inside
	// <Name type="managed"> regions. Rule 12 applies only to source files and must not fire for
	// DocumentDeployed.
	doc := parseInlineDoc(t, sourceDocWithPopulatedDeployedRegion)

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		Kind: docformat.DocumentDeployed,
	})

	if hasIssueWithCode(issues, "populated-source-region") {
		t.Error("rule 12 must not fire for DocumentDeployed; got \"populated-source-region\" issue")
	}
}

func TestValidate_Rule12_DocumentKindUnknown_SkipsPopulatedSourceRegionCheck(t *testing.T) {
	// When Kind is DocumentUnknown (zero value), rule 12 is skipped. A document with content
	// in deployed regions must produce no "populated-source-region" issue.
	doc := parseInlineDoc(t, sourceDocWithPopulatedDeployedRegion)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "populated-source-region") {
		t.Error("rule 12 must be skipped when Kind is DocumentUnknown; got \"populated-source-region\" issue")
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Rule 15: Identity section shape
// ---------------------------------------------------------------------------

func TestValidate_Rule15_IdentityWithFullShape_NoIdentityShapeIssue(t *testing.T) {
	// An Identity section containing **Goal:**, **Scope:** with DO and DO NOT bullets,
	// **Litmus Test:**, and ### Process must produce no "identity-shape" issue.
	doc := parseInlineDoc(t, identityWithFullShape)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "identity-shape") {
		t.Errorf("Identity section with full shape produced unexpected \"identity-shape\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule15_IdentityMissingGoal_ReportsIdentityShape(t *testing.T) {
	// An Identity section missing **Goal:** must produce an "identity-shape" issue at
	// SeverityWarning.
	doc := parseInlineDoc(t, identityMissingGoal)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "identity-shape")
	if iss == nil {
		t.Error("expected an \"identity-shape\" issue for Identity section missing **Goal:**; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("identity-shape issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestValidate_Rule15_IdentityMissingProcess_ReportsIdentityShape(t *testing.T) {
	// An Identity section missing the ### Process subsection must produce an "identity-shape"
	// issue at SeverityWarning.
	doc := parseInlineDoc(t, identityMissingProcess)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "identity-shape")
	if iss == nil {
		t.Error("expected an \"identity-shape\" issue for Identity section missing ### Process; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("identity-shape issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestValidate_Rule15_NoIdentitySection_NoIdentityShapeIssue(t *testing.T) {
	// When the document has no Identity section, rule 15 is skipped. A document without an
	// Identity section must produce no "identity-shape" issue.
	doc := parseInlineDoc(t, noIdentitySection)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "identity-shape") {
		t.Error("document with no Identity section produced unexpected \"identity-shape\" issue; rule 15 must be skipped")
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Rule 16: No status_code JSON object outside OutputFormat
// ---------------------------------------------------------------------------

func TestValidate_Rule16_StatusCodeInConstraints_ReportsContractRestated(t *testing.T) {
	// A section other than OutputFormat containing a JSON object with a "status_code" key
	// must produce a "contract-restated" issue at SeverityWarning. Contract prose belongs
	// in the deployed Communication Protocol, not restated per-agent.
	doc := parseInlineDoc(t, statusCodeInConstraints)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "contract-restated")
	if iss == nil {
		t.Error("expected a \"contract-restated\" issue for a status_code JSON object outside OutputFormat; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("contract-restated issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestValidate_Rule16_StatusCodeOnlyInOutputFormat_NoContractRestatedIssue(t *testing.T) {
	// A document whose OutputFormat section contains status-related prose (as a markdown
	// table) must produce no "contract-restated" issue. The table format does not contain
	// a JSON object with a status_code key.
	doc := parseInlineDoc(t, statusCodeOnlyInOutputFormat)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "contract-restated") {
		t.Errorf("document with status info only in OutputFormat produced unexpected \"contract-restated\" issue; issues: %v", issues)
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Rule 17: OutputFormat contains no JSON code fence
// ---------------------------------------------------------------------------

func TestValidate_Rule17_OutputFormatWithJsonFence_ReportsOutputFormatJsonFence(t *testing.T) {
	// An OutputFormat section containing a JSON code fence (```json) must produce an
	// "output-format-json-fence" issue at SeverityWarning. OutputFormat must use a
	// markdown table, not a JSON example — the Communication Protocol deploys the contract.
	doc := parseInlineDoc(t, outputFormatWithJsonFence)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "output-format-json-fence")
	if iss == nil {
		t.Error("expected an \"output-format-json-fence\" issue for an OutputFormat section with a JSON fence; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("output-format-json-fence issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestValidate_Rule17_OutputFormatWithTableOnly_NoOutputFormatJsonFenceIssue(t *testing.T) {
	// An OutputFormat section containing only a markdown table (no JSON fence) must produce
	// no "output-format-json-fence" issue.
	doc := parseInlineDoc(t, outputFormatWithTable)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "output-format-json-fence") {
		t.Errorf("OutputFormat with table only produced unexpected \"output-format-json-fence\" issue; issues: %v", issues)
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Rule 18: No Process step mentions human-in-the-loop
// ---------------------------------------------------------------------------

func TestValidate_Rule18_ProcessStepMentionsHITL_ReportsProcessHITLStep(t *testing.T) {
	// A Process list step that mentions "human-in-the-loop" must produce a "process-hitl-step"
	// issue at SeverityWarning. HITL behaviour is deployed via the ClosingProcedure block;
	// the Process list must not duplicate it.
	doc := parseInlineDoc(t, processWithHITLStep)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "process-hitl-step")
	if iss == nil {
		t.Error("expected a \"process-hitl-step\" issue for a Process step mentioning human-in-the-loop; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("process-hitl-step issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestValidate_Rule18_ProcessStepsWithoutHITL_NoProcessHITLStepIssue(t *testing.T) {
	// A Process list with no step mentioning "human-in-the-loop" must produce no
	// "process-hitl-step" issue.
	doc := parseInlineDoc(t, processWithoutHITLStep)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "process-hitl-step") {
		t.Errorf("Process list without HITL mention produced unexpected \"process-hitl-step\" issue; issues: %v", issues)
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Rule 19: Skills in Process ↔ required_skills
// ---------------------------------------------------------------------------

func TestValidate_Rule19_SkillsMatchedInProcessAndRequired_NoSkillDeclarationMismatch(t *testing.T) {
	// When every skill named in a Process step appears in required_skills, and every entry in
	// required_skills appears in a Process step, no "skill-declaration-mismatch" issue must
	// be produced.
	doc := parseInlineDoc(t, skillsMatchedInProcessAndRequired)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "skill-declaration-mismatch") {
		t.Errorf("matched skills produced unexpected \"skill-declaration-mismatch\" issue; issues: %v", issues)
	}
}

func TestValidate_Rule19_SkillInProcessNotInRequired_ReportsSkillDeclarationMismatch(t *testing.T) {
	// When a Process step names a skill not listed in required_skills, a
	// "skill-declaration-mismatch" issue must be produced at SeverityError. A skill that is
	// used but not declared cannot be loaded by the runner.
	doc := parseInlineDoc(t, skillInProcessNotInRequired)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "skill-declaration-mismatch")
	if iss == nil {
		t.Error("expected a \"skill-declaration-mismatch\" issue when a Process step names a skill not in required_skills; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("skill-declaration-mismatch issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_Rule19_SkillInRequiredNotInProcess_ReportsSkillDeclarationMismatch(t *testing.T) {
	// When required_skills lists a skill that is not mentioned in any Process step, a
	// "skill-declaration-mismatch" issue must be produced at SeverityError. A declared but
	// unused skill wastes a load step and suggests an out-of-date declaration.
	doc := parseInlineDoc(t, skillInRequiredNotInProcess)

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "skill-declaration-mismatch")
	if iss == nil {
		t.Error("expected a \"skill-declaration-mismatch\" issue when required_skills lists a skill not mentioned in any Process step; got none")
	} else if iss.Severity != docformat.SeverityError {
		t.Errorf("skill-declaration-mismatch issue severity: want SeverityError, got %q", iss.Severity)
	}
}

// ---------------------------------------------------------------------------
// T7.6 — Strict mode escalates Stage 7 warning-level findings to SeverityError
// ---------------------------------------------------------------------------

func TestValidate_StrictMode_EscalatesStage7WarningToError(t *testing.T) {
	// A warning-level finding from a Stage 7 rule (missing-conduct-region for a subagent
	// missing a conduct region) carries SeverityWarning when Strict is false, and must carry
	// SeverityError when Strict is true. Strict mode promotes all warnings; it does not
	// selectively promote only pre-Stage-7 findings.
	doc := parseInlineDoc(t, subagentMissingOneConductRegion)

	issuesLenient := docformat.Validate(doc, docformat.ValidateOptions{
		Role:   "subagent",
		Strict: false,
	})
	issuesStrict := docformat.Validate(doc, docformat.ValidateOptions{
		Role:   "subagent",
		Strict: true,
	})

	lenientIss := issueWithCode(issuesLenient, "missing-conduct-region")
	if lenientIss == nil {
		t.Skip("no missing-conduct-region issue returned in lenient mode — severity escalation cannot be checked")
	}
	if lenientIss.Severity != docformat.SeverityWarning {
		t.Errorf("lenient mode: missing-conduct-region severity = %q, want SeverityWarning", lenientIss.Severity)
	}

	strictIss := issueWithCode(issuesStrict, "missing-conduct-region")
	if strictIss == nil {
		t.Error("strict mode: expected a \"missing-conduct-region\" issue; got none")
	} else if strictIss.Severity != docformat.SeverityError {
		t.Errorf("strict mode: missing-conduct-region severity = %q, want SeverityError (promoted by Strict)", strictIss.Severity)
	}
}

func TestValidate_StrictMode_DoesNotEscalateStage7AdviceToError(t *testing.T) {
	// Strict mode must not promote SeverityAdvice findings — even from Stage 7 rules.
	// A misplaced <Name type="project"> injection region produces "wrong-parent" at SeverityAdvice; that
	// must remain SeverityAdvice under Strict=true.
	// (Fixture reuses malformed/wrong-parent.md from the Stage 3 boundary test suite.)
	doc := parsedBoundaryFixture(t, "malformed/wrong-parent.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireInjectionParents: true,
		Strict:                  true,
	})

	for _, iss := range issues {
		if iss.Code == "wrong-parent" && iss.Severity == docformat.SeverityError {
			t.Errorf("strict mode must not promote SeverityAdvice to SeverityError; "+
				"got wrong-parent at SeverityError: %+v", iss)
		}
	}
}

func TestValidate_StrictMode_NewFields_StructCompiles(t *testing.T) {
	// Verify that the new ValidateOptions fields (Kind, Role, FileBaseName) can be
	// set at compile time, and that the DocumentKind constants have the expected values.
	opts := docformat.ValidateOptions{
		Kind:         docformat.DocumentSource,
		Role:         "subagent",
		FileBaseName: "my-agent",
		Strict:       true,
	}
	_ = opts

	if docformat.DocumentSource != "source" {
		t.Errorf("DocumentSource = %q, want %q", docformat.DocumentSource, "source")
	}
	if docformat.DocumentDeployed != "deployed" {
		t.Errorf("DocumentDeployed = %q, want %q", docformat.DocumentDeployed, "deployed")
	}
	if docformat.DocumentUnknown != "" {
		t.Errorf("DocumentUnknown = %q, want empty string", docformat.DocumentUnknown)
	}
}
