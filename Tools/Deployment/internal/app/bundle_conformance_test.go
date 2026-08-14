package app

// bundle_conformance_test.go covers Stage 7 bundle conformance rules 21 and 22
// tested directly via the package-internal bundleConformance function.
//
// This file uses package app (not package app_test) to access the unexported
// bundleConformance function. It defines its own fixtures and does not rely on
// the stubCatalog or other helpers defined in testhelpers_test.go (which is in
// package app_test and therefore inaccessible from this file).
//
// Coverage (T7.5 — rules 21 and 22 via bundleConformance):
//
//   Rule 21 (bundle-version-mismatch, warning severity): a deployed file's
//   `bundle_version` frontmatter stamp disagrees with bundle.Version.
//     - When a deployed file carries a bundle_version that matches bundle.Version,
//       no "bundle-version-mismatch" issue is produced for that file.
//     - When a deployed file carries a bundle_version that does not match bundle.Version,
//       a "bundle-version-mismatch" issue at SeverityWarning is produced. Issue.Subject
//       is the target path.
//     - When a deployed file has no bundle_version stamp (BundleVersion == ""), no
//       "bundle-version-mismatch" issue is produced — absent stamp means the file
//       predates bundle tracking and is handled by the planner's staleness logic.
//     - When no agent in the bundle applies to the file's role, rule 21 is skipped for
//       that file (the bundle does not concern it).
//
//   Rule 22 (bundle-region-drift, warning severity): a bundle-sourced managed region
//   region in the deployed file carries content that does not match the bundle block
//   byte-for-byte.
//     - When a deployed file's bundle region content matches the bundle block content
//       exactly, no "bundle-region-drift" issue is produced.
//     - When a deployed file's bundle region content differs from the bundle block
//       content, a "bundle-region-drift" issue at SeverityWarning is produced.
//       Issue.Subject is the target path; Issue.Node is the region name.
//     - When the bundle has no block for a given region, that region is not checked and
//       no "bundle-region-drift" issue is produced.
//     - When the deployed file does not contain a particular bundle region at all, a
//       "bundle-region-drift" issue is produced (the region is absent but the bundle
//       declares it should be there).
//
//   Both rules are applied per file. A file that violates both rule 21 and rule 22
//   produces both issues.

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// bcIssueWithCode returns the first catalog.Issue with the given code from a slice.
func bcIssueWithCode(issues []catalog.Issue, code string) *catalog.Issue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}

// bcHasIssueWithCode reports whether any issue in the slice has the given code.
func bcHasIssueWithCode(issues []catalog.Issue, code string) bool {
	return bcIssueWithCode(issues, code) != nil
}

// makeSubagentBundle returns a BundleContent with the given version and one subagent
// block for the given target region name, carrying the given content.
func makeSubagentBundle(version, targetRegion string, blockContent []byte) domain.BundleContent {
	return domain.BundleContent{
		Version: version,
		Blocks: []domain.BundleBlock{
			{
				Name:      targetRegion + ":Subagent",
				Target:    targetRegion,
				AppliesTo: "subagent",
				Content:   blockContent,
			},
		},
	}
}

// deployedFileWithRegion builds the raw bytes of a minimal deployed subagent file that
// has `bundle_version: <bundleVersion>` in frontmatter and one managed region (type="managed")
// containing regionContent.
func deployedFileWithRegion(bundleVer, regionName string, regionContent []byte) []byte {
	front := "---\nrole: subagent\nbundle_version: " + bundleVer + "\n---\n\n"
	region := "<" + regionName + " type=\"managed\">\n" + string(regionContent) + "\n</" + regionName + ">\n"
	return []byte(front + region)
}

// deployedFileNoBundleVersion builds a deployed file with no bundle_version frontmatter field.
func deployedFileNoBundleVersion(regionName string, regionContent []byte) []byte {
	front := "---\nrole: subagent\n---\n\n"
	region := "<" + regionName + " type=\"managed\">\n" + string(regionContent) + "\n</" + regionName + ">\n"
	return []byte(front + region)
}

// deployedFileWithoutRegion builds a deployed file that has bundle_version in frontmatter
// but does NOT contain the given region in the body.
func deployedFileWithoutRegion(bundleVer string) []byte {
	return []byte("---\nrole: subagent\nbundle_version: " + bundleVer + "\n---\n\nContent with no deployed region.\n")
}

// ---------------------------------------------------------------------------
// T7.5 — Rule 21: bundle-version-mismatch
// ---------------------------------------------------------------------------

func TestBundleConformance_Rule21_MatchingBundleVersion_NoBundleVersionMismatchIssue(t *testing.T) {
	// A deployed file whose bundle_version frontmatter stamp matches bundle.Version must
	// produce no "bundle-version-mismatch" issue. The bundle content is current for this
	// file and no re-deployment is needed.
	const targetPath = "path/to/agent.md"
	bundle := makeSubagentBundle("2.0.0", "AuthorityHierarchy", []byte("Content."))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {
			Present:       true,
			BundleVersion: "2.0.0", // matches bundle.Version
		},
	}
	bodies := map[string][]byte{
		targetPath: deployedFileWithRegion("2.0.0", "AuthorityHierarchy", []byte("Content.")),
	}

	issues := bundleConformance(bundle, states, bodies)

	if bcHasIssueWithCode(issues, "bundle-version-mismatch") {
		t.Errorf("matching bundle version produced unexpected \"bundle-version-mismatch\" issue; issues: %v", issues)
	}
}

func TestBundleConformance_Rule21_MismatchedBundleVersion_ReportsBundleVersionMismatch(t *testing.T) {
	// A deployed file whose bundle_version stamp (1.0.0) differs from bundle.Version (2.0.0)
	// must produce a "bundle-version-mismatch" issue at SeverityWarning. The file is stale
	// and must be re-deployed to receive the current bundle content.
	const targetPath = "Catalog/Subagents/Creation/test-writer-tdd.md"
	bundle := makeSubagentBundle("2.0.0", "AuthorityHierarchy", []byte("New content."))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {
			Present:       true,
			BundleVersion: "1.0.0", // does NOT match bundle.Version "2.0.0"
		},
	}
	bodies := map[string][]byte{
		targetPath: deployedFileWithRegion("1.0.0", "AuthorityHierarchy", []byte("Old content.")),
	}

	issues := bundleConformance(bundle, states, bodies)

	iss := bcIssueWithCode(issues, "bundle-version-mismatch")
	if iss == nil {
		t.Error("expected a \"bundle-version-mismatch\" issue when deployed bundle_version differs from bundle.Version; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("bundle-version-mismatch issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestBundleConformance_Rule21_MismatchedBundleVersion_SubjectIsTargetPath(t *testing.T) {
	// The "bundle-version-mismatch" issue's Subject must be the target path of the stale
	// deployed file so that the planner can identify which file to re-deploy.
	const targetPath = "Catalog/Subagents/Creation/test-writer-tdd.md"
	bundle := makeSubagentBundle("2.0.0", "AuthorityHierarchy", []byte("New content."))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := map[string][]byte{
		targetPath: deployedFileWithRegion("1.0.0", "AuthorityHierarchy", []byte("Old content.")),
	}

	issues := bundleConformance(bundle, states, bodies)

	iss := bcIssueWithCode(issues, "bundle-version-mismatch")
	if iss == nil {
		t.Fatal("expected \"bundle-version-mismatch\" issue; cannot verify Subject")
	}
	if iss.Subject != targetPath {
		t.Errorf("bundle-version-mismatch Subject = %q, want %q", iss.Subject, targetPath)
	}
}

func TestBundleConformance_Rule21_NoBundleVersionInDeployedFile_NoBundleVersionMismatchIssue(t *testing.T) {
	// A deployed file with no bundle_version stamp (BundleVersion == "") pre-dates bundle
	// tracking. Rule 21 must not fire for such files — the planner's staleness logic handles
	// them via other version fields.
	const targetPath = "path/to/old-agent.md"
	bundle := makeSubagentBundle("2.0.0", "AuthorityHierarchy", []byte("Content."))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {
			Present:       true,
			BundleVersion: "", // no bundle_version stamp
		},
	}
	bodies := map[string][]byte{
		targetPath: deployedFileNoBundleVersion("AuthorityHierarchy", []byte("Old content.")),
	}

	issues := bundleConformance(bundle, states, bodies)

	if bcHasIssueWithCode(issues, "bundle-version-mismatch") {
		t.Error("deployed file with no bundle_version stamp produced unexpected \"bundle-version-mismatch\" issue; " +
			"rule 21 must skip files without a bundle_version stamp")
	}
}

func TestBundleConformance_Rule21_FilesNotCoveredByBundle_Skipped(t *testing.T) {
	// A deployed file for a role that no bundle block applies to must be skipped by rule 21.
	// When bundle.AppliesToRole(role) is false, the file is not subject to bundle checks.
	// The bundle here covers only "subagent"; there are no orchestrator blocks.
	const orchestratorPath = "path/to/orchestrator.md"
	bundle := makeSubagentBundle("2.0.0", "AuthorityHierarchy", []byte("Content."))
	states := map[string]domain.DeployedArtifactState{
		orchestratorPath: {
			Present:       true,
			BundleVersion: "1.0.0", // old version, but orchestrator isn't covered
		},
	}
	bodies := map[string][]byte{
		// An orchestrator deployed file — no bundle block applies to it.
		orchestratorPath: []byte("---\nrole: orchestrator\nbundle_version: 1.0.0\n---\nContent.\n"),
	}

	issues := bundleConformance(bundle, states, bodies)

	if bcHasIssueWithCode(issues, "bundle-version-mismatch") {
		t.Error("orchestrator file (not covered by subagent-only bundle) produced unexpected \"bundle-version-mismatch\" issue")
	}
}

func TestBundleConformance_Rule21_MultipleFiles_OneMismatch_ReportsOnlyStaleFile(t *testing.T) {
	// When multiple deployed files are checked and only one has a stale bundle_version,
	// only that file must produce a "bundle-version-mismatch" issue. The current file
	// must not be flagged.
	const (
		stalePath   = "path/to/stale.md"
		currentPath = "path/to/current.md"
	)
	bundle := makeSubagentBundle("2.0.0", "AuthorityHierarchy", []byte("Content."))
	states := map[string]domain.DeployedArtifactState{
		stalePath:   {Present: true, BundleVersion: "1.0.0"}, // stale
		currentPath: {Present: true, BundleVersion: "2.0.0"}, // current
	}
	bodies := map[string][]byte{
		stalePath:   deployedFileWithRegion("1.0.0", "AuthorityHierarchy", []byte("Old content.")),
		currentPath: deployedFileWithRegion("2.0.0", "AuthorityHierarchy", []byte("Content.")),
	}

	issues := bundleConformance(bundle, states, bodies)

	// At least one "bundle-version-mismatch" issue must be present (for the stale file).
	found := false
	for _, iss := range issues {
		if iss.Code == "bundle-version-mismatch" {
			if iss.Subject == stalePath {
				found = true
			} else if iss.Subject == currentPath {
				t.Errorf("unexpected \"bundle-version-mismatch\" for current file %q", currentPath)
			}
		}
	}
	if !found {
		t.Errorf("expected a \"bundle-version-mismatch\" issue for stale file %q; not found", stalePath)
	}
}

// ---------------------------------------------------------------------------
// T7.5 — Rule 22: bundle-region-drift
// ---------------------------------------------------------------------------

func TestBundleConformance_Rule22_MatchingRegionContent_NoBundleRegionDriftIssue(t *testing.T) {
	// A deployed file whose <AuthorityHierarchy type="managed"> region content matches the
	// bundle block content byte-for-byte must produce no "bundle-region-drift" issue.
	const targetPath = "path/to/agent.md"
	const blockContent = "Verbatim authority hierarchy content.\n"
	bundle := makeSubagentBundle("1.0.0", "AuthorityHierarchy", []byte(blockContent))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := map[string][]byte{
		targetPath: deployedFileWithRegion("1.0.0", "AuthorityHierarchy", []byte(blockContent)),
	}

	issues := bundleConformance(bundle, states, bodies)

	if bcHasIssueWithCode(issues, "bundle-region-drift") {
		t.Errorf("matching region content produced unexpected \"bundle-region-drift\" issue; issues: %v", issues)
	}
}

func TestBundleConformance_Rule22_DriftingRegionContent_ReportsBundleRegionDrift(t *testing.T) {
	// A deployed file whose <AuthorityHierarchy type="managed"> region carries content that
	// differs from the bundle block's content must produce a "bundle-region-drift" issue
	// at SeverityWarning. The region has been hand-edited or deployed from an outdated
	// bundle version.
	const targetPath = "path/to/agent.md"
	bundle := makeSubagentBundle("2.0.0", "AuthorityHierarchy", []byte("New authority hierarchy content.\n"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "2.0.0"},
	}
	bodies := map[string][]byte{
		// The deployed region carries the OLD content — different from the bundle block.
		targetPath: deployedFileWithRegion("2.0.0", "AuthorityHierarchy", []byte("Old authority hierarchy content.\n")),
	}

	issues := bundleConformance(bundle, states, bodies)

	iss := bcIssueWithCode(issues, "bundle-region-drift")
	if iss == nil {
		t.Error("expected a \"bundle-region-drift\" issue when deployed region content differs from bundle block; got none")
	} else if iss.Severity != docformat.SeverityWarning {
		t.Errorf("bundle-region-drift issue severity: want SeverityWarning, got %q", iss.Severity)
	}
}

func TestBundleConformance_Rule22_DriftingRegion_SubjectIsTargetPath(t *testing.T) {
	// The "bundle-region-drift" issue's Subject must be the target path of the drifted
	// deployed file so that the planner can identify which file to re-deploy.
	const targetPath = "Catalog/Subagents/Creation/test-writer-tdd.md"
	bundle := makeSubagentBundle("1.0.0", "AuthorityHierarchy", []byte("Bundle content.\n"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := map[string][]byte{
		targetPath: deployedFileWithRegion("1.0.0", "AuthorityHierarchy", []byte("Different content.\n")),
	}

	issues := bundleConformance(bundle, states, bodies)

	iss := bcIssueWithCode(issues, "bundle-region-drift")
	if iss == nil {
		t.Fatal("expected \"bundle-region-drift\" issue; cannot verify Subject")
	}
	if iss.Subject != targetPath {
		t.Errorf("bundle-region-drift Subject = %q, want %q", iss.Subject, targetPath)
	}
}

func TestBundleConformance_Rule22_DriftingRegion_NodeIsRegionName(t *testing.T) {
	// The "bundle-region-drift" issue's Node must be the region name
	// (e.g. "AuthorityHierarchy") so that the operator can identify which managed region
	// block within the file has drifted.
	const targetPath = "path/to/agent.md"
	bundle := makeSubagentBundle("1.0.0", "AuthorityHierarchy", []byte("Bundle content.\n"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := map[string][]byte{
		targetPath: deployedFileWithRegion("1.0.0", "AuthorityHierarchy", []byte("Different content.\n")),
	}

	issues := bundleConformance(bundle, states, bodies)

	iss := bcIssueWithCode(issues, "bundle-region-drift")
	if iss == nil {
		t.Fatal("expected \"bundle-region-drift\" issue; cannot verify Node")
	}
	if iss.Code == "bundle-region-drift" && iss.Node != "AuthorityHierarchy" {
		t.Errorf("bundle-region-drift Node = %q, want %q", iss.Node, "AuthorityHierarchy")
	}
}

func TestBundleConformance_Rule22_RegionAbsentFromDeployedFile_ReportsBundleRegionDrift(t *testing.T) {
	// When the bundle declares a block for a region that is entirely absent from the
	// deployed file, a "bundle-region-drift" issue must be produced. A missing region
	// means the deployed file is missing content that the bundle intends to inject.
	const targetPath = "path/to/agent.md"
	bundle := makeSubagentBundle("1.0.0", "AuthorityHierarchy", []byte("Bundle content.\n"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := map[string][]byte{
		// The deployed file has NO <AuthorityHierarchy type="managed"> region at all.
		targetPath: deployedFileWithoutRegion("1.0.0"),
	}

	issues := bundleConformance(bundle, states, bodies)

	if !bcHasIssueWithCode(issues, "bundle-region-drift") {
		t.Error("expected a \"bundle-region-drift\" issue when the bundle region is absent from the deployed file; got none")
	}
}

func TestBundleConformance_Rule22_NoBundleBlockForRegion_NoRegionDriftIssue(t *testing.T) {
	// If the bundle has no block for a given region name, that region is not subject to
	// drift checking. A deployed file may carry content in <ClosingProcedure type="managed">
	// without triggering "bundle-region-drift" if the bundle defines no block for
	// ClosingProcedure.
	const targetPath = "path/to/agent.md"
	// Bundle only has an AuthorityHierarchy block — not a ClosingProcedure block.
	bundle := makeSubagentBundle("1.0.0", "AuthorityHierarchy", []byte("Authority content."))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	// The deployed file has a ClosingProcedure region (not covered by the bundle) and the
	// correct AuthorityHierarchy region.
	bodies := map[string][]byte{
		targetPath: []byte("---\nrole: subagent\nbundle_version: 1.0.0\n---\n\n" +
			"<AuthorityHierarchy type=\"managed\">\nAuthority content.\n</AuthorityHierarchy>\n\n" +
			"<ClosingProcedure type=\"managed\">\nSome closing procedure content.\n</ClosingProcedure>\n"),
	}

	issues := bundleConformance(bundle, states, bodies)

	if bcHasIssueWithCode(issues, "bundle-region-drift") {
		t.Error("unexpected \"bundle-region-drift\": only regions covered by a bundle block should be checked")
	}
}

// ---------------------------------------------------------------------------
// T7.5 — Rules 21 and 22 combined
// ---------------------------------------------------------------------------

func TestBundleConformance_Rules21And22_BothViolated_ReportsBothIssues(t *testing.T) {
	// A deployed file that violates both rule 21 (stale bundle version) and rule 22
	// (drifted region content) must produce both issues. Rules 21 and 22 are independent
	// and must not short-circuit on the first violation.
	const targetPath = "path/to/agent.md"
	bundle := makeSubagentBundle("2.0.0", "AuthorityHierarchy", []byte("New content.\n"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {
			Present:       true,
			BundleVersion: "1.0.0", // stale (rule 21 violation)
		},
	}
	bodies := map[string][]byte{
		// Old bundle_version in frontmatter AND old content in the region (rule 22 violation).
		targetPath: deployedFileWithRegion("1.0.0", "AuthorityHierarchy", []byte("Old content.\n")),
	}

	issues := bundleConformance(bundle, states, bodies)

	if !bcHasIssueWithCode(issues, "bundle-version-mismatch") {
		t.Error("expected \"bundle-version-mismatch\" (rule 21 violation); got none")
	}
	if !bcHasIssueWithCode(issues, "bundle-region-drift") {
		t.Error("expected \"bundle-region-drift\" (rule 22 violation); got none")
	}
}

func TestBundleConformance_EmptyInputs_ReturnsNoIssues(t *testing.T) {
	// bundleConformance called with empty states and bodies must return no issues.
	// There are no deployed files to check.
	bundle := makeSubagentBundle("1.0.0", "AuthorityHierarchy", []byte("Content."))
	states := map[string]domain.DeployedArtifactState{}
	bodies := map[string][]byte{}

	issues := bundleConformance(bundle, states, bodies)

	if len(issues) != 0 {
		t.Errorf("expected no issues for empty states and bodies; got %d: %v", len(issues), issues)
	}
}
