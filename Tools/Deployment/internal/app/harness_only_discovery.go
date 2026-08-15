package app

// harness_only_discovery.go implements the two-signal harness-only agent eligibility
// check and the workspace directory scan that uses it to find agents with no generic
// catalog counterpart.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// EligibilityVerdict is the outcome of the two-signal harness-only detection rule.
type EligibilityVerdict struct {
	// Eligible reports whether the file is an already-transformed MOSAIC agent.
	Eligible bool
	// Reason explains a negative verdict in human-readable form. Empty when Eligible
	// is true.
	Reason string
	// Meta carries the frontmatter values the caller needs when Eligible is true.
	// Zero-valued when Eligible is false.
	Meta HarnessOnlyMeta
}

// HarnessOnlyMeta is the frontmatter context an eligible file yields.
type HarnessOnlyMeta struct {
	NumericID        string
	Version          string
	TransformVersion string
	Role             domain.AgentRole
}

// blockingValidationCodes are the validation issue codes that make an agent ineligible.
// Non-blocking codes ("content-outside-boundary", "out-of-order-section", "wrong-parent")
// are deliberately excluded — a hand-authored agent is not required to be fully
// canonical, only recognisably tagged.
var blockingValidationCodes = map[string]bool{
	"unbalanced-tag":   true,
	"mismatched-tag":   true,
	"duplicate-name":   true,
	"unknown-deployed": true,
	"wrong-marker":     true,
}

// eligibleHarnessOnly applies the two-signal harness-only detection rule to file bytes.
//
// Signal one: parsed frontmatter is present and carries a non-empty `transform_version`
// scalar — the marker Tools/OldAgentsTransform stamps onto any file it boundary-tags.
//
// Signal two: the body carries a structurally valid set of canonical MOSAIC boundary
// tags. Concretely, ALL of:
//   - at least one <Name type="core"> node whose Name is in docformat.CanonicalSections;
//   - docformat.Validate(doc, docformat.ValidateOptions{}) reports no issue whose Code
//     is one of: "unbalanced-tag", "mismatched-tag", "duplicate-name",
//     "unknown-deployed", "wrong-marker".
//
// The function is pure: no filesystem access, no mutation of src. Unparseable bytes
// yield an ineligible verdict with a Reason, never an error return.
func eligibleHarnessOnly(src []byte) EligibilityVerdict {
	// Parse the document. Unparseable bytes are ineligible.
	doc, err := docformat.Parse(src)
	if err != nil {
		return EligibilityVerdict{
			Eligible: false,
			Reason:   "document could not be parsed: " + err.Error(),
		}
	}

	fm := doc.Frontmatter()
	if !fm.Present() {
		return EligibilityVerdict{
			Eligible: false,
			Reason:   "document has no frontmatter block",
		}
	}

	// Signal one: non-empty transform_version scalar in frontmatter. Both the prefixed
	// (mosaic_-prefixed) and legacy (unprefixed) names are accepted via agentfields.ReadOrder,
	// with the prefixed name preferred when both are present.
	tvField, _ := agentfields.ByDeployedName("transform_version")
	var transformVersion string
	for _, key := range agentfields.ReadOrder(tvField) {
		if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
			transformVersion = v.Scalar
			break
		}
	}
	if transformVersion == "" {
		return EligibilityVerdict{
			Eligible: false,
			Reason:   "frontmatter does not carry a non-empty transform_version scalar",
		}
	}

	// Signal two: body carries at least one canonical section AND no blocking issues.

	// Check for at least one canonical section.
	canonicalSectionSet := make(map[string]bool, len(docformat.CanonicalSections))
	for _, s := range docformat.CanonicalSections {
		canonicalSectionSet[s] = true
	}

	foundCanonical := false
	for _, sec := range doc.Body().Sections() {
		if canonicalSectionSet[sec.Name()] {
			foundCanonical = true
			break
		}
	}
	if !foundCanonical {
		return EligibilityVerdict{
			Eligible: false,
			Reason:   "body contains no canonical MOSAIC section (SECTION tags must match vocabulary names)",
		}
	}

	// Run structural validation with zero-value options (non-strict, no canonical section
	// requirement, no injection-parent requirement). Check only for blocking issue codes.
	issues := docformat.Validate(doc, docformat.ValidateOptions{})
	for _, issue := range issues {
		if blockingValidationCodes[issue.Code] {
			return EligibilityVerdict{
				Eligible: false,
				Reason:   "body has blocking structural issue (" + issue.Code + "): " + issue.Message,
			}
		}
	}

	// Both signals passed. Extract metadata from frontmatter.
	meta := HarnessOnlyMeta{
		TransformVersion: transformVersion,
	}

	if v, ok := fm.Get("version"); ok && v.Kind == domain.KindScalar {
		meta.Version = v.Scalar
	}
	// Read the numeric ID from the deployed file, accepting both the prefixed (mosaic_-prefixed)
	// and legacy (unprefixed) forms via agentfields.ReadOrder.
	idField, _ := agentfields.ByGeneric("id")
	for _, key := range agentfields.ReadOrder(idField) {
		if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar {
			meta.NumericID = v.Scalar
			break
		}
	}

	// Parse role; default to RoleSubagent when absent or unrecognised. Accept both the
	// prefixed deployed form (mosaic_role) and the legacy bare form (role) via ReadOrder,
	// preferring the prefixed form when both are present.
	meta.Role = domain.RoleSubagent
	roleField, _ := agentfields.ByGeneric("role")
	for _, key := range agentfields.ReadOrder(roleField) {
		if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar {
			if role, ok := domain.ParseAgentRole(v.Scalar); ok {
				meta.Role = role
			}
			break
		}
	}

	return EligibilityVerdict{
		Eligible: true,
		Meta:     meta,
	}
}

// isOrchestratorFileName reports whether name is the orchestrator agent's file name.
// True if and only if name equals "orchestrator.md" or "orchestrator.agent.md",
// compared case-insensitively. Frontmatter is never consulted: a `role: orchestrator`
// value in a differently-named file does not make it an orchestrator for this purpose.
func isOrchestratorFileName(name string) bool {
	lower := strings.ToLower(name)
	return lower == "orchestrator.md" || lower == "orchestrator.agent.md"
}

// agentKeyFromFileName derives an agent key from a deployed file name by stripping the
// trailing ".agent.md" or ".md" suffix, matching how the catalog derives a key from a
// generic source file name.
func agentKeyFromFileName(name string) string {
	if strings.HasSuffix(name, ".agent.md") {
		return strings.TrimSuffix(name, ".agent.md")
	}
	return strings.TrimSuffix(name, ".md")
}

// catalogAgentKeys returns the set of every agent key the generic catalog knows about —
// worker agents, utility agents, and the orchestrator. A deployed file whose derived key
// is in this set has a generic counterpart and is therefore NOT harness-only; it is
// handled by the existing catalog-driven path with no behaviour change.
func catalogAgentKeys(c catalog.Catalog) map[string]bool {
	keys := make(map[string]bool)
	for _, a := range c.Agents() {
		if a.Key != "" {
			keys[a.Key] = true
		}
	}
	for _, a := range c.UtilityAgents() {
		if a.Key != "" {
			keys[a.Key] = true
		}
	}
	if orch := c.Orchestrator(); orch.Key != "" {
		keys[orch.Key] = true
	}
	return keys
}

// scanHarnessOnlyAgents walks the workspace's deployed-agent directory (agentsDir,
// relative to workspace) and returns every eligible harness-only agent found.
//
// A file is returned only when ALL hold:
//   - it is a regular file whose name ends in ".md";
//   - !isOrchestratorFileName(name);
//   - its derived key is not in catalogKeys;
//   - eligibleHarnessOnly(bytes).Eligible is true.
//
// Tolerance is the contract: a file that cannot be read or parsed is skipped silently.
// The walk is non-recursive. The function opens no file for writing and leaves every
// scanned file byte-identical. Results are sorted by TargetPath.
//
// An empty agentsDir yields an empty result rather than scanning the workspace root.
func scanHarnessOnlyAgents(workspace, agentsDir string, catalogKeys map[string]bool) []HarnessOnlyAgent {
	result := []HarnessOnlyAgent{}

	// Guard: an empty agentsDir must yield an empty result without scanning workspace root.
	if agentsDir == "" {
		return result
	}

	dir := filepath.Join(workspace, agentsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory does not exist or is unreadable — return empty result.
		return result
	}

	for _, entry := range entries {
		// Non-recursive: skip directories.
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Only .md files.
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		// Exclude orchestrator-named files by canonical filename check.
		if isOrchestratorFileName(name) {
			continue
		}

		// Derive agent key and check against catalog.
		key := agentKeyFromFileName(name)
		if catalogKeys[key] {
			// This file has a generic catalog counterpart; not harness-only.
			continue
		}

		// Read file bytes tolerantly — skip silently on error.
		filePath := filepath.Join(dir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			// Unreadable — skip silently.
			continue
		}

		// Apply two-signal eligibility check.
		verdict := eligibleHarnessOnly(data)
		if !verdict.Eligible {
			// Not an eligible harness-only agent — skip silently.
			continue
		}

		targetPath := filepath.Join(agentsDir, name)
		result = append(result, HarnessOnlyAgent{
			TargetPath:       targetPath,
			FileName:         name,
			Key:              key,
			NumericID:        verdict.Meta.NumericID,
			Version:          verdict.Meta.Version,
			TransformVersion: verdict.Meta.TransformVersion,
			Role:             verdict.Meta.Role,
		})
	}

	// Sort by TargetPath for deterministic results.
	sort.Slice(result, func(i, j int) bool {
		return result[i].TargetPath < result[j].TargetPath
	})

	return result
}
