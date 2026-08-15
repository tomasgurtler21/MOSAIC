package app

import (
	"bytes"
	"fmt"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// bundleConformance checks the deployed-file set against the current bundle and returns any
// conformance issues. It is the implementation of rules 21 and 22.
//
// Rule 21 (bundle-version-mismatch, SeverityWarning): a deployed file's bundle_version
// frontmatter stamp must match bundle.Version. A mismatching stamp means the file was last
// deployed from an older bundle and must be re-deployed.
//
// Rule 22 (bundle-region-drift, SeverityWarning): a bundle-sourced managed region in
// a deployed file must carry exactly the same content as the bundle block. A drift means the
// region has been hand-edited or deployed from a different bundle version.
//
// Both rules are skipped for files whose role is not covered by any bundle block
// (bundle.AppliesToRole returns false for that role).
func bundleConformance(
	bundle domain.BundleContent,
	states map[string]domain.DeployedArtifactState,
	deployedBodies map[string][]byte,
) []catalog.Issue {
	var issues []catalog.Issue

	for targetPath, state := range states {
		rawBody, ok := deployedBodies[targetPath]
		if !ok {
			continue
		}

		// Parse the deployed file to read the role from frontmatter.
		doc, err := docformat.Parse(rawBody)
		if err != nil {
			continue // unparseable deployed file — skip silently
		}

		var role domain.AgentRole
		var roleFound bool
		roleField, _ := agentfields.ByGeneric("role")
		for _, key := range agentfields.ReadOrder(roleField) {
			if fv, ok := doc.Frontmatter().Get(key); ok && fv.Kind == domain.KindScalar {
				r, valid := domain.ParseAgentRole(fv.Scalar)
				if !valid {
					break // unrecognised role — not subject to bundle checks
				}
				role = r
				roleFound = true
				break
			}
		}
		if !roleFound {
			continue // no role field or unrecognised value — skip
		}

		// Skip files whose role the bundle does not cover.
		if !bundle.AppliesToRole(role) {
			continue
		}

		// Rule 21: bundle_version stamp must match bundle.Version.
		// Files with no stamp (BundleVersion == "") predate bundle tracking and are skipped.
		if state.BundleVersion != "" && state.BundleVersion != bundle.Version {
			issues = append(issues, catalog.Issue{
				Severity: docformat.SeverityWarning,
				Code:     "bundle-version-mismatch",
				Subject:  targetPath,
				Message: fmt.Sprintf(
					"deployed file %q carries bundle_version %q but the current bundle is at version %q",
					targetPath, state.BundleVersion, bundle.Version,
				),
				Path: targetPath,
			})
		}

		// Rule 22: for each bundle block that applies to this file's role, verify that the
		// managed region in the deployed file carries the expected content.
		body := doc.Body()
		for _, blk := range bundle.Blocks {
			if domain.AgentRole(blk.AppliesTo) != role {
				continue // this block does not apply to this file
			}

			regionNode, regionFound := body.Deployed(blk.Target)

			if !regionFound {
				// The region is declared by the bundle but absent from the deployed file.
				issues = append(issues, catalog.Issue{
					Severity: docformat.SeverityWarning,
					Code:     "bundle-region-drift",
					Subject:  targetPath,
					Node:     blk.Target,
					Message: fmt.Sprintf(
						"deployed file %q is missing managed region %q which the current bundle declares",
						targetPath, blk.Target,
					),
					Path: targetPath,
				})
				continue
			}

			// Compare the deployed region content with the bundle block content.
			// Normalise trailing whitespace on both sides: the document serialiser adds
			// a trailing newline after the last line of a region, but the bundle block
			// content may or may not carry one.
			deployedContent := bytes.TrimRight(regionNode.Content(), "\r\n\t ")
			bundleContent := bytes.TrimRight(blk.Content, "\r\n\t ")

			if !bytes.Equal(deployedContent, bundleContent) {
				issues = append(issues, catalog.Issue{
					Severity: docformat.SeverityWarning,
					Code:     "bundle-region-drift",
					Subject:  targetPath,
					Node:     blk.Target,
					Message: fmt.Sprintf(
						"deployed file %q: managed region %q content differs from the current bundle block",
						targetPath, blk.Target,
					),
					Path: targetPath,
				})
			}
		}
	}

	return issues
}
