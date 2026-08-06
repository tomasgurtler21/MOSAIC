package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// ValidateRegistry checks registry-level invariants that can only be evaluated with access
// to the whole catalog. It returns all issues found; the slice is nil when there are none.
//
// Implemented rules:
//
//   - Rule 5 (missing-skill-folder): every entry in an agent's required_skills list must name
//     an existing folder under Agents/Generic/Skills/.
//
//   - Rule 6 (duplicate-agent-id): every subagent numeric `id` must be unique across the
//     registry. When two agents share an id, one issue is appended per duplicate (the first
//     by key-sort order wins the slot in the numeric-id index).
func ValidateRegistry(c Catalog) []Issue {
	var issues []Issue

	// Rule 5: every required_skills entry must correspond to an existing skill.
	for _, agent := range c.Agents() {
		for _, skillKey := range agent.RequiredSkills {
			if _, ok := c.Skill(skillKey); !ok {
				issues = append(issues, Issue{
					Severity: docformat.SeverityError,
					Code:     "missing-skill-folder",
					Subject:  agent.Key,
					Message: fmt.Sprintf(
						"agent %q requires skill %q but no Agents/Generic/Skills/%s/ folder exists in the registry",
						agent.Key, skillKey, skillKey,
					),
					Path: agent.SourcePath,
				})
			}
		}
	}

	// Rule 6: numeric agent ids must be unique.
	// Walk agents in sorted key order (Agents() guarantees sorted order) and flag every id
	// seen for the second time. One issue is emitted per duplicate occurrence.
	seen := make(map[string]string) // id → first-seen agent key
	for _, agent := range c.Agents() {
		if agent.NumericID == "" {
			continue
		}
		if firstKey, exists := seen[agent.NumericID]; exists {
			issues = append(issues, Issue{
				Severity: docformat.SeverityError,
				Code:     "duplicate-agent-id",
				Subject:  agent.NumericID,
				Message: fmt.Sprintf(
					"agents %q and %q both declare numeric id %q; ids must be unique across the registry",
					firstKey, agent.Key, agent.NumericID,
				),
				Path: agent.SourcePath,
			})
		} else {
			seen[agent.NumericID] = agent.Key
		}
	}

	return issues
}

// ValidateBundleDeclarations checks rule 20: every declared bundle block must name a target
// in docformat.CanonicalDeployed, an applies_to in {"subagent","orchestrator"}, and a
// specified_in path that exists on disk under the MOSAIC root. All violations are
// accumulated; the function returns all issues found.
func ValidateBundleDeclarations(b domain.BundleContent, root string) []Issue {
	var issues []Issue

	for _, blk := range b.Blocks {
		// Rule 20a: Target must be a recognised canonical deployed region name.
		if !isCanonicalDeployedName(blk.Target) {
			issues = append(issues, Issue{
				Severity: docformat.SeverityError,
				Code:     "bundle-unknown-target",
				Subject:  blk.Name,
				Message: fmt.Sprintf(
					"bundle block %q declares target %q which is not in docformat.CanonicalDeployed",
					blk.Name, blk.Target,
				),
			})
		}

		// Rule 20b: AppliesTo must be "subagent" or "orchestrator".
		if blk.AppliesTo != "subagent" && blk.AppliesTo != "orchestrator" {
			issues = append(issues, Issue{
				Severity: docformat.SeverityError,
				Code:     "bundle-unknown-applies-to",
				Subject:  blk.Name,
				Message: fmt.Sprintf(
					"bundle block %q declares applies_to %q which is not a recognised role (expected \"subagent\" or \"orchestrator\")",
					blk.Name, blk.AppliesTo,
				),
			})
		}

		// Rule 20c: SpecifiedIn must be non-empty and the path must exist under root.
		if blk.SpecifiedIn == "" {
			issues = append(issues, Issue{
				Severity: docformat.SeverityError,
				Code:     "bundle-missing-spec-doc",
				Subject:  blk.Name,
				Message: fmt.Sprintf(
					"bundle block %q has an empty specified_in field; every bundle block must reference a design document",
					blk.Name,
				),
			})
		} else {
			fullPath := filepath.Join(root, blk.SpecifiedIn)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				issues = append(issues, Issue{
					Severity: docformat.SeverityError,
					Code:     "bundle-missing-spec-doc",
					Subject:  blk.Name,
					Message: fmt.Sprintf(
						"bundle block %q specified_in path %q does not exist under the MOSAIC root",
						blk.Name, blk.SpecifiedIn,
					),
					Path: fullPath,
				})
			}
		}
	}

	return issues
}

// isCanonicalDeployedName reports whether name is in docformat.CanonicalDeployed.
func isCanonicalDeployedName(name string) bool {
	for _, n := range docformat.CanonicalDeployed {
		if n == name {
			return true
		}
	}
	return false
}
