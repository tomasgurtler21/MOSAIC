package docformat

import (
	"fmt"
	"strings"

	"mosaic-common/mosaic"
)

// validateDocumentRules applies the document-level validation rules (rules 1–4, 8a, 8b,
// 8c, 12, 15–19) that require access to both the frontmatter and the parsed body. These
// rules are layered on top of the structural byte-scan performed by validateBytes.
//
// Strict mode promotion is NOT applied here — it is applied once by the caller (Validate)
// after all issues from both validateBytes and validateDocumentRules are combined.
func validateDocumentRules(d *Document, opts ValidateOptions) []Issue {
	var issues []Issue
	fm := d.Frontmatter()
	body := d.Body()

	// Rules 1–4: Source-file frontmatter checks. Skipped when Kind is DocumentUnknown.
	if opts.Kind == DocumentSource {
		issues = append(issues, checkFrontmatterRules(fm, opts)...)
	}

	// Rule 8a: [[DEPLOYED:CommunicationProtocol]] must be present for all roles.
	// Skipped when Role is not set (the validator cannot know what to expect).
	if opts.Role != "" {
		issues = append(issues, checkRule8a(body)...)
	}

	// Rules 8b and 8c: Role-specific conduct regions. Skipped when Role is not set.
	if opts.Role != "" {
		issues = append(issues, checkConductRegions(body, opts.Role)...)
	}

	// Rule 12: In a source file, every [[DEPLOYED:]] region must be empty.
	// Skipped when Kind is DocumentUnknown or DocumentDeployed.
	if opts.Kind == DocumentSource {
		issues = append(issues, checkSourceDeployedRegions(body)...)
	}

	// Rule 15: Identity section must have the required shape.
	// Applied whenever an Identity section is present; no option gate.
	issues = append(issues, checkIdentityShape(body)...)

	// Rule 16: No section outside OutputFormat contains a JSON object with "status_code".
	issues = append(issues, checkNoContractRestated(body)...)

	// Rule 17: OutputFormat section must not contain a JSON code fence.
	issues = append(issues, checkOutputFormatNoJSONFence(body)...)

	// Rule 18: No Process step mentions human-in-the-loop.
	issues = append(issues, checkNoHITLInProcess(body)...)

	// Rule 19: Every skill named in a Process step appears in required_skills, and vice versa.
	issues = append(issues, checkSkillDeclarations(fm, body)...)

	return issues
}

// ---------------------------------------------------------------------------
// Rules 1–4: Frontmatter checks (source files only)
// ---------------------------------------------------------------------------

// checkFrontmatterRules checks rules 1–4 against the document's frontmatter.
// Called only when ValidateOptions.Kind == DocumentSource.
func checkFrontmatterRules(fm *Frontmatter, opts ValidateOptions) []Issue {
	var issues []Issue

	// Rule 1a: Required fields for all source documents.
	requiredFields := []string{"name", "version", "description", "role"}
	for _, field := range requiredFields {
		if _, ok := fm.Get(field); !ok {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "missing-frontmatter-field",
				Message:  fmt.Sprintf("source document is missing required frontmatter field %q", field),
			})
		}
	}

	// Rule 1b: `id` presence depends on role.
	switch opts.Role {
	case "subagent":
		if _, ok := fm.Get("id"); !ok {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "missing-frontmatter-field",
				Message:  `source subagent document is missing required frontmatter field "id"`,
			})
		}
	case "orchestrator":
		if _, ok := fm.Get("id"); ok {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "unexpected-id",
				Message:  `orchestrator source document must not carry an "id" field; orchestrators are identified by key (path)`,
			})
		}
	}

	// Rule 2: `role` must be "subagent" or "orchestrator".
	if v, ok := fm.Get("role"); ok && v.Kind == mosaic.KindScalar {
		if v.Scalar != "subagent" && v.Scalar != "orchestrator" {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "invalid-role",
				Message:  fmt.Sprintf(`frontmatter field "role" has unrecognised value %q; must be "subagent" or "orchestrator"`, v.Scalar),
			})
		}
	}

	// Rule 3: `name` must match the file base name (skipped when FileBaseName is "").
	if opts.FileBaseName != "" {
		if v, ok := fm.Get("name"); ok && v.Kind == mosaic.KindScalar {
			if v.Scalar != opts.FileBaseName {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Code:     "name-path-mismatch",
					Message:  fmt.Sprintf("frontmatter name %q does not match file base name %q", v.Scalar, opts.FileBaseName),
				})
			}
		}
	}

	// Rule 4: `version` must parse as semver.
	if v, ok := fm.Get("version"); ok && v.Kind == mosaic.KindScalar {
		if !isSemver(v.Scalar) {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "invalid-version",
				Message:  fmt.Sprintf("frontmatter version %q is not a valid semantic version (expected MAJOR.MINOR.PATCH)", v.Scalar),
			})
		}
	}

	return issues
}

// isSemver reports whether s is a valid semantic version string (MAJOR.MINOR.PATCH with
// optional pre-release and build-metadata suffixes). Only the three-part numeric core
// is required; any non-empty pre-release or build-metadata suffix is accepted.
func isSemver(s string) bool {
	// Strip build metadata ("+...").
	if idx := strings.Index(s, "+"); idx >= 0 {
		s = s[:idx]
	}
	// Strip pre-release ("-...").
	if idx := strings.Index(s, "-"); idx >= 0 {
		s = s[:idx]
	}
	// The core must be MAJOR.MINOR.PATCH with all-digit components.
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Rules 8a, 8b, 8c: Communication protocol and conduct regions
// ---------------------------------------------------------------------------

// conductRegionNames lists the five bundle-sourced conduct regions that subagents
// must carry. Orchestrators must not carry any of them.
var conductRegionNames = []string{
	"AuthorityHierarchy",
	"ClosingProcedure",
	"ProtocolConstraints",
	"ErrorHandlingCommon",
	"ExecutionPhilosophyCommon",
}

// checkRule8a reports a "missing-contract-region" error when the document lacks a
// [[DEPLOYED:CommunicationProtocol]] region anywhere in the body.
func checkRule8a(body *Body) []Issue {
	if _, ok := body.Deployed("CommunicationProtocol"); !ok {
		return []Issue{{
			Severity: SeverityError,
			Code:     "missing-contract-region",
			Message:  "document is missing [[DEPLOYED:CommunicationProtocol]] which is required for all deployed agents",
		}}
	}
	return nil
}

// checkConductRegions checks rule 8b (subagent must have all five conduct regions) and
// rule 8c (orchestrator must not carry any of them).
func checkConductRegions(body *Body, role string) []Issue {
	var issues []Issue
	switch role {
	case "subagent":
		for _, name := range conductRegionNames {
			if _, ok := body.Deployed(name); !ok {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Code:     "missing-conduct-region",
					Message:  fmt.Sprintf("subagent is missing required conduct region [[DEPLOYED:%s]]", name),
					Node:     name,
				})
			}
		}
	case "orchestrator":
		for _, name := range conductRegionNames {
			if _, ok := body.Deployed(name); ok {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Code:     "role-forbidden-region",
					Message:  fmt.Sprintf("orchestrator carries [[DEPLOYED:%s]] which is reserved for subagents", name),
					Node:     name,
				})
			}
		}
	}
	return issues
}

// ---------------------------------------------------------------------------
// Rule 12: Source deployed regions must be empty
// ---------------------------------------------------------------------------

// checkSourceDeployedRegions reports a "populated-source-region" error for every
// [[DEPLOYED:]] region in a source file that contains non-whitespace content.
func checkSourceDeployedRegions(body *Body) []Issue {
	var issues []Issue
	for _, node := range body.DeployedRegions() {
		if !node.IsEmpty() {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "populated-source-region",
				Message:  fmt.Sprintf("source file has content inside [[DEPLOYED:%s]]; deployed regions in source files must be empty placeholders", node.Name()),
				Node:     node.Name(),
			})
		}
	}
	return issues
}

// ---------------------------------------------------------------------------
// Rule 15: Identity section shape
// ---------------------------------------------------------------------------

// checkIdentityShape reports an "identity-shape" warning when the document has an
// Identity section that is missing one or more required elements: **Goal:**, **Scope:**
// (with both a DO and a DO NOT bullet), **Litmus Test:**, and a ### Process list.
//
// When no Identity section is present the rule is skipped.
func checkIdentityShape(body *Body) []Issue {
	identityNode, ok := body.Section("Identity")
	if !ok {
		return nil
	}

	content := string(identityNode.Content())
	var missing []string

	if !strings.Contains(content, "**Goal:**") {
		missing = append(missing, "**Goal:**")
	}
	if !strings.Contains(content, "**Scope:**") {
		missing = append(missing, "**Scope:**")
	} else {
		// Scope must include both a DO and a DO NOT bullet.
		if !strings.Contains(content, "You DO:") {
			missing = append(missing, "Scope DO bullet (\"You DO:\")")
		}
		if !strings.Contains(content, "You DO NOT:") {
			missing = append(missing, "Scope DO NOT bullet (\"You DO NOT:\")")
		}
	}
	if !strings.Contains(content, "**Litmus Test:**") {
		missing = append(missing, "**Litmus Test:**")
	}
	if !strings.Contains(content, "### Process") {
		missing = append(missing, "### Process")
	}

	if len(missing) > 0 {
		return []Issue{{
			Severity: SeverityWarning,
			Code:     "identity-shape",
			Message:  fmt.Sprintf("Identity section is missing required elements: %s", strings.Join(missing, ", ")),
		}}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Rule 16: No status_code JSON object outside OutputFormat
// ---------------------------------------------------------------------------

// checkNoContractRestated reports a "contract-restated" warning for any non-OutputFormat
// section that contains a line with both "{" and "\"status_code\"", which is a signature
// of a JSON output-format example that restates the Communication Protocol contract.
func checkNoContractRestated(body *Body) []Issue {
	var issues []Issue
	for _, section := range body.SectionsDeep() {
		if section.Name() == "OutputFormat" {
			continue
		}
		content := string(section.Content())
		for _, line := range strings.Split(content, "\n") {
			if strings.Contains(line, "{") && strings.Contains(line, `"status_code"`) {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Code:     "contract-restated",
					Message: fmt.Sprintf(
						"section %q contains a JSON object with a \"status_code\" key; "+
							"output format contracts belong in the Communication Protocol, not restated per-agent",
						section.Name(),
					),
				})
				break // one issue per section
			}
		}
	}
	return issues
}

// ---------------------------------------------------------------------------
// Rule 17: OutputFormat must not contain a JSON code fence
// ---------------------------------------------------------------------------

// checkOutputFormatNoJSONFence reports an "output-format-json-fence" warning when the
// OutputFormat section contains a JSON code fence (```json). The OutputFormat section must
// use a markdown table; JSON examples belong only in the deployed Communication Protocol.
func checkOutputFormatNoJSONFence(body *Body) []Issue {
	outputFormat, ok := body.Section("OutputFormat")
	if !ok {
		return nil
	}
	if strings.Contains(string(outputFormat.Content()), "```json") {
		return []Issue{{
			Severity: SeverityWarning,
			Code:     "output-format-json-fence",
			Message:  "OutputFormat section contains a JSON code fence (```json); use a markdown table instead",
		}}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Rule 18: Process list must not mention human-in-the-loop
// ---------------------------------------------------------------------------

// checkNoHITLInProcess reports a "process-hitl-step" warning when any Process step
// in the Identity section mentions "human-in-the-loop" or "human_in_the_loop".
// The HITL behaviour is deployed via the ClosingProcedure block; the Process list
// must not duplicate it.
func checkNoHITLInProcess(body *Body) []Issue {
	identityNode, ok := body.Section("Identity")
	if !ok {
		return nil
	}
	content := string(identityNode.Content())

	// Find the ### Process subsection.
	processIdx := strings.Index(content, "### Process")
	if processIdx < 0 {
		return nil
	}
	processContent := content[processIdx:]

	if strings.Contains(processContent, "human-in-the-loop") ||
		strings.Contains(processContent, "human_in_the_loop") {
		return []Issue{{
			Severity: SeverityWarning,
			Code:     "process-hitl-step",
			Message:  "Process list contains a step mentioning human-in-the-loop; HITL behaviour is deployed via the ClosingProcedure block",
		}}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Rule 19: Skills in Process steps ↔ required_skills
// ---------------------------------------------------------------------------

// checkSkillDeclarations reports "skill-declaration-mismatch" errors when the set of
// skill names mentioned (in backticks) in the Identity section's Process list does not
// exactly match the required_skills frontmatter list.
//
// A skill name is a backtick-quoted token composed entirely of lowercase letters, digits,
// and hyphens. This matches the naming convention of all MOSAIC skills (e.g. "lean-tdd").
func checkSkillDeclarations(fm *Frontmatter, body *Body) []Issue {
	// Extract required_skills from frontmatter.
	var requiredSkills []string
	if v, ok := fm.Get("required_skills"); ok && v.Kind == mosaic.KindList {
		for _, item := range v.Items {
			if item.Kind == mosaic.KindScalar && item.Scalar != "" {
				requiredSkills = append(requiredSkills, item.Scalar)
			}
		}
	}

	// Find the Identity section and its Process subsection.
	identityNode, ok := body.Section("Identity")
	if !ok {
		if len(requiredSkills) == 0 {
			return nil
		}
		// required_skills is non-empty but there is no Identity section and therefore
		// no Process list to verify the skill declarations against.
		return []Issue{{
			Severity: SeverityError,
			Code:     "skill-declaration-mismatch",
			Message:  "required_skills lists skills but no Identity section with a Process list was found to verify them",
		}}
	}

	content := string(identityNode.Content())
	processIdx := strings.Index(content, "### Process")
	var processContent string
	if processIdx >= 0 {
		processContent = content[processIdx:]
	}

	// Extract skill names from the Process content (backtick-quoted skill-name tokens).
	processSkills := extractSkillNames(processContent)

	requiredSet := stringSet(requiredSkills)
	processSet := stringSet(processSkills)

	var issues []Issue

	// Skills mentioned in Process but absent from required_skills.
	for skill := range processSet {
		if !requiredSet[skill] {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "skill-declaration-mismatch",
				Message:  fmt.Sprintf("Process step mentions skill %q which is not listed in required_skills", skill),
			})
		}
	}

	// Skills listed in required_skills but absent from Process steps.
	for skill := range requiredSet {
		if !processSet[skill] {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "skill-declaration-mismatch",
				Message:  fmt.Sprintf("required_skills lists skill %q which is not mentioned in any Process step", skill),
			})
		}
	}

	return issues
}

// extractSkillNames extracts backtick-quoted tokens from s that look like skill names:
// composed entirely of lowercase letters, digits, and hyphens, starting with a letter.
func extractSkillNames(s string) []string {
	var skills []string
	for {
		start := strings.Index(s, "`")
		if start < 0 {
			break
		}
		s = s[start+1:]
		end := strings.Index(s, "`")
		if end < 0 {
			break
		}
		token := s[:end]
		s = s[end+1:]
		if isSkillNameToken(token) {
			skills = append(skills, token)
		}
	}
	return skills
}

// isSkillNameToken reports whether token looks like a MOSAIC skill name:
// starts with a lowercase letter and contains only lowercase letters, digits, and hyphens.
func isSkillNameToken(token string) bool {
	if len(token) == 0 {
		return false
	}
	for i, c := range token {
		if i == 0 {
			if c < 'a' || c > 'z' {
				return false
			}
		} else {
			ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
			if !ok {
				return false
			}
		}
	}
	return true
}

// stringSet converts a slice of strings into a set (map[string]bool).
func stringSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

