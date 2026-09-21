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
	doc, err := ParseGenericSource(src)
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

// finalSuffix returns the final dotted component of ext. This is the single file
// extension used to construct the orchestrator exclusion set and the agent-file filter.
//
//	finalSuffix(".md")       == ".md"
//	finalSuffix(".agent.md") == ".md"
//	finalSuffix(".toml")     == ".toml"
func finalSuffix(ext string) string {
	i := strings.LastIndex(ext, ".")
	if i <= 0 {
		return ext
	}
	return ext[i:]
}

// agentFileName returns the on-disk file name for an agent given its key and the
// harness's declared agent extension.
func agentFileName(key, agentExt string) string {
	return key + agentExt
}

// isAgentFile reports whether name ends in the final suffix of agentExt and therefore
// belongs to the harness's agent file set.
//
// The filter uses finalSuffix so that a harness declaring ".agent.md" (GHCP CLI)
// continues to accept plain ".md" files alongside ".agent.md" files, exactly as
// the previous hard-coded ".md" filter did.
func isAgentFile(name, agentExt string) bool {
	return strings.HasSuffix(name, finalSuffix(agentExt))
}

// agentKeyFromFileName derives an agent key from a deployed file name by stripping the
// declared agent extension and its ".agent.<suffix>" variant, matching how the catalog
// derives a key from a generic source file name.
//
// The derivation uses finalSuffix(agentExt) as the single extension component:
//
//   - For agentExt ".md" or ".agent.md": strips ".agent.md" then ".md" (existing behaviour).
//   - For agentExt ".toml": strips ".agent.toml" then ".toml".
//
// No extension component leaks into the derived key.
func agentKeyFromFileName(name, agentExt string) string {
	suffix := finalSuffix(agentExt)
	agentSuffix := ".agent" + suffix
	if strings.HasSuffix(name, agentSuffix) {
		return strings.TrimSuffix(name, agentSuffix)
	}
	return strings.TrimSuffix(name, suffix)
}

// mosaicOrchestratorFileNames returns the set of lower-case file names that identify
// MOSAIC's own orchestrator agents for the given declared agent extension.
//
// Per CD-5, the derivation uses finalSuffix(agentExt). For agentExt ".md" and ".agent.md"
// this reproduces the four existing names exactly; for ".toml" it yields the four .toml
// names. Nothing is removed from any existing harness's set.
//
// The returned set always has exactly four members.
func mosaicOrchestratorFileNames(agentExt string) map[string]bool {
	suffix := finalSuffix(agentExt)
	return map[string]bool{
		"orchestrator" + suffix:              true,
		"orchestrator.agent" + suffix:        true,
		"orchestrator-script" + suffix:       true,
		"orchestrator-script.agent" + suffix: true,
	}
}

// isOrchestratorFileName reports whether name is one of MOSAIC's orchestrator agent
// file names for the harness declared in src. Matching is whole-name, case-insensitive.
//
// When src is omitted or nil, ".md" is used as the declared extension, which reproduces
// today's four Markdown names:
//
//	"orchestrator.md", "orchestrator.agent.md",
//	"orchestrator-script.md", "orchestrator-script.agent.md"
//
// Frontmatter is never consulted: a `role: orchestrator` value in a differently-named
// file does not make it an orchestrator for this purpose. Matching is whole-name
// equality, never a prefix or substring test — a file named "orchestrator-script-custom.md"
// is not excluded.
func isOrchestratorFileName(name string, src ...*domain.HarnessDescriptor) bool {
	agentExt := ".md"
	if len(src) > 0 && src[0] != nil {
		if ext, ok := src[0].Extensions[domain.ArtifactAgent]; ok && ext != "" {
			agentExt = ext
		}
	}
	lower := strings.ToLower(name)
	return mosaicOrchestratorFileNames(agentExt)[lower]
}

// catalogAgentKeys returns the set of every agent key the generic catalog knows about:
// worker agents (Agents), utility agents (UtilityAgents), the orchestrator (Orchestrator),
// and the script orchestrator (OrchestratorScript). A deployed file whose derived key is in
// this set has a generic counterpart and is therefore NOT harness-only.
//
// OrchestratorScript's absence from a catalog is legitimate, not an error: the (agent, ok)
// pair is consulted and a false ok contributes no key, exactly as an empty Orchestrator key
// contributes none.
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
	if scriptOrch, ok := c.OrchestratorScript(); ok && scriptOrch.Key != "" {
		keys[scriptOrch.Key] = true
	}
	return keys
}

// scanHarnessOnlyAgents walks the workspace's deployed-agent directory (agentsDir,
// relative to workspace) and returns every eligible harness-only agent found.
//
// A file is returned only when ALL hold:
//   - it matches the declared agent extension (isAgentFile);
//   - !isOrchestratorFileName(name, src);
//   - its derived key is not in catalogKeys;
//   - eligibleHarnessOnly(canonicalBytes).Eligible is true.
//
// The optional src parameter supplies the harness descriptor used to determine the
// declared agent extension and to decode file bytes through the funnel. When src is
// omitted or nil, ".md" is used as the declared extension and the identity decoder is
// applied (Markdown harness behaviour, identical to the previous implementation).
//
// Tolerance is the contract: a file that cannot be read or decoded is skipped silently.
// The walk is non-recursive. The function opens no file for writing and leaves every
// scanned file byte-identical. Results are sorted by TargetPath.
//
// An empty agentsDir yields an empty result rather than scanning the workspace root.
//
// The second return value carries decode-report notices for every file that decoded
// through the funnel. Callers surface these at their own named merge point.
func scanHarnessOnlyAgents(workspace, agentsDir string, catalogKeys map[string]bool, srcs ...*domain.HarnessDescriptor) ([]HarnessOnlyAgent, []string) {
	var src *domain.HarnessDescriptor
	if len(srcs) > 0 {
		src = srcs[0]
	}

	result := []HarnessOnlyAgent{}
	var notices []string

	// Guard: an empty agentsDir must yield an empty result without scanning workspace root.
	if agentsDir == "" {
		return result, notices
	}

	// Determine the declared agent extension. Default to ".md" when undeclared.
	agentExt := ".md"
	if src != nil {
		if ext, ok := src.Extensions[domain.ArtifactAgent]; ok && ext != "" {
			agentExt = ext
		}
	}

	dir := filepath.Join(workspace, agentsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory does not exist or is unreadable — return empty result.
		return result, notices
	}

	for _, entry := range entries {
		// Non-recursive: skip directories.
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Only files matching the declared agent extension (using finalSuffix for the
		// GHCP CLI ".agent.md" case — plain ".md" files are still accepted).
		if !isAgentFile(name, agentExt) {
			continue
		}

		// Exclude orchestrator-named files by canonical filename check.
		if isOrchestratorFileName(name, src) {
			continue
		}

		// Derive agent key and check against catalog.
		key := agentKeyFromFileName(name, agentExt)
		if catalogKeys[key] {
			// This file has a generic catalog counterpart; not harness-only.
			continue
		}

		// Read and decode through the funnel (Layer 2 — PlanItem-free path).
		// Discovery never holds a plan item, so readDeployedArtifact is the entry point.
		filePath := filepath.Join(dir, name)
		dr := readDeployedArtifact(filePath, domain.ArtifactAgent, src)

		// Surface the decode report at this named merge point.
		notices = append(notices, decodeReportNotices(name, dr.Report)...)

		// Skip unreadable, missing, or undecodable files silently.
		if !dr.Present || dr.ReadErr != nil || dr.DecodeErr != nil || dr.Canonical == nil {
			continue
		}

		// Apply two-signal eligibility check on the canonical (decoded) bytes.
		verdict := eligibleHarnessOnly(dr.Canonical)
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

	return result, notices
}
