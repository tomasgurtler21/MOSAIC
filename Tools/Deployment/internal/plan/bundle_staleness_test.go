package plan_test

// bundle_staleness_test.go covers bundle staleness detection: the comparison of a deployed
// artifact's bundle version against the current bundle source version.
//
//   BundleStaleness / BundleDrift (T6.7):
//
//     Staleness comparison:
//       - When applies is false (agent's role receives no bundle block), BundleDrift.Stale()
//         is always false regardless of the deployed or source versions.
//       - A deployed bundle version equal to the source version is not stale (when applies = true).
//       - A deployed bundle version that differs from the source is stale (when applies = true).
//       - An empty deployed bundle version (no stamp in the file) is stale (when applies = true).
//
//     BundleDrift.Delta():
//       - When stale, Delta() returns ok = true and a VersionDelta whose Field is BundleDeltaField.
//       - When stale, the delta's Deployed and Source fields carry the respective versions.
//       - When not stale (applies = false, or equal versions), Delta() returns ok = false.
//
//     BundleDeltaField constant:
//       - Its value is "bundle_version".
//       - It does not collide with any frontmatter stamp field name or with WorkflowDeltaFieldPrefix.
//       - It does not collide with ProtocolDeltaField.
//
//     BundleDrift.Reason():
//       - When deployed and source differ: "bundle version changed (X -> Y)".
//       - When deployed is empty: names the absence of a bundle version stamp.
//       - When not stale (applies = false, or equal versions): returns "".

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// T6.7 — BundleStaleness: staleness comparison
// ---------------------------------------------------------------------------

// TestBundleStaleness_AppliesFalse_NeverStale verifies that when applies is false (the
// agent's role has no bundle blocks — the orchestrator today), BundleDrift.Stale() returns
// false regardless of the deployed or source versions. An agent that receives no bundle
// blocks is never stale for a bundle reason.
func TestBundleStaleness_AppliesFalse_NeverStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}

	drift := plan.BundleStaleness(deployed, "2.0.0", false)

	if drift.Stale() {
		t.Error("BundleStaleness with applies = false: Stale() = true, want false; " +
			"an agent whose role receives no bundle blocks must never be stale for a bundle reason")
	}
}

// TestBundleStaleness_AppliesFalse_EmptyDeployed_NeverStale verifies that when applies is
// false, even an absent deployed version does not make the agent stale.
func TestBundleStaleness_AppliesFalse_EmptyDeployed_NeverStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "", // no stamp in the deployed file
	}

	drift := plan.BundleStaleness(deployed, "1.0.0", false)

	if drift.Stale() {
		t.Error("BundleStaleness with applies = false and empty deployed version: Stale() = true, want false")
	}
}

// TestBundleStaleness_EqualVersions_NotStale verifies that when the deployed and source
// bundle versions are the same, and the bundle applies to this role, Stale() returns false.
func TestBundleStaleness_EqualVersions_NotStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}

	drift := plan.BundleStaleness(deployed, "1.0.0", true)

	if drift.Stale() {
		t.Error("BundleStaleness with equal versions and applies = true: Stale() = true, want false")
	}
}

// TestBundleStaleness_OlderDeployedVersion_IsStale verifies that when the deployed bundle
// version is older than the source, BundleStaleness reports drift. A version change in the
// bundle must trigger re-deployment of all affected agents.
func TestBundleStaleness_OlderDeployedVersion_IsStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}

	drift := plan.BundleStaleness(deployed, "1.1.0", true)

	if !drift.Stale() {
		t.Error("BundleStaleness with older deployed version and applies = true: Stale() = false, want true; " +
			"a deployed bundle version older than the source must be detected as stale")
	}
}

// TestBundleStaleness_EmptyDeployedVersion_IsStale verifies that when the deployed file
// carries no bundle version stamp (BundleVersion == ""), BundleStaleness reports drift.
// Agents deployed before the bundle stamp feature was added carry no version; they must
// be picked up and re-deployed to receive both the stamp and the updated content.
func TestBundleStaleness_EmptyDeployedVersion_IsStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "", // no `bundle_version` frontmatter key in the deployed file
	}

	drift := plan.BundleStaleness(deployed, "1.0.0", true)

	if !drift.Stale() {
		t.Error("BundleStaleness with empty deployed version and applies = true: Stale() = false, want true; " +
			"an absent bundle version stamp must always be treated as stale when the bundle applies")
	}
}

// TestBundleStaleness_DeployedAndSourceFieldsCarriedCorrectly verifies that the returned
// BundleDrift carries the deployed version and source version from the inputs.
func TestBundleStaleness_DeployedAndSourceFieldsCarriedCorrectly(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}

	drift := plan.BundleStaleness(deployed, "1.1.0", true)

	if drift.Deployed != "1.0.0" {
		t.Errorf("BundleDrift.Deployed = %q, want %q (value from the deployed file)",
			drift.Deployed, "1.0.0")
	}
	if drift.Source != "1.1.0" {
		t.Errorf("BundleDrift.Source = %q, want %q (current source version)",
			drift.Source, "1.1.0")
	}
}

// TestBundleStaleness_AppliesFieldCarriedCorrectly verifies that BundleDrift.Applies
// reflects the applies argument passed to BundleStaleness.
func TestBundleStaleness_AppliesFieldCarriedCorrectly(t *testing.T) {
	deployed := domain.DeployedArtifactState{Present: true}

	trueApplies := plan.BundleStaleness(deployed, "1.0.0", true)
	if !trueApplies.Applies {
		t.Error("BundleDrift.Applies = false when applies argument was true")
	}

	falseApplies := plan.BundleStaleness(deployed, "1.0.0", false)
	if falseApplies.Applies {
		t.Error("BundleDrift.Applies = true when applies argument was false")
	}
}

// TestBundleStaleness_VersionDowngrade_IsStale verifies that the direction of the version
// change is irrelevant. A deployed version higher than the source (downgrade scenario) is
// still stale — any difference must be detected.
func TestBundleStaleness_VersionDowngrade_IsStale(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "2.0.0", // deployed is newer than source
	}

	drift := plan.BundleStaleness(deployed, "1.0.0", true)

	if !drift.Stale() {
		t.Error("BundleStaleness with deployed version newer than source (downgrade scenario): " +
			"Stale() = false, want true; any version difference must be detected as stale")
	}
}

// ---------------------------------------------------------------------------
// T6.7 — BundleDrift.Delta(): version delta construction
// ---------------------------------------------------------------------------

// TestBundleDrift_Delta_WhenStale_ReturnsOkAndBundleVersionField verifies that Delta()
// returns ok = true and a VersionDelta whose Field is BundleDeltaField when the drift is stale.
func TestBundleDrift_Delta_WhenStale_ReturnsOkAndBundleVersionField(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}
	drift := plan.BundleStaleness(deployed, "1.1.0", true)

	delta, ok := drift.Delta()

	if !ok {
		t.Fatal("BundleDrift.Delta() returned ok = false for a stale drift; want ok = true")
	}
	if delta.Field != plan.BundleDeltaField {
		t.Errorf("delta.Field = %q, want %q (BundleDeltaField)", delta.Field, plan.BundleDeltaField)
	}
}

// TestBundleDrift_Delta_WhenStale_CarriesDeployedAndSourceValues verifies that the delta
// returned by Delta() carries the deployed version in delta.Deployed and the source version
// in delta.Source.
func TestBundleDrift_Delta_WhenStale_CarriesDeployedAndSourceValues(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}
	drift := plan.BundleStaleness(deployed, "1.1.0", true)

	delta, ok := drift.Delta()

	if !ok {
		t.Fatal("BundleDrift.Delta() returned ok = false for a stale drift")
	}
	if delta.Deployed != "1.0.0" {
		t.Errorf("delta.Deployed = %q, want %q", delta.Deployed, "1.0.0")
	}
	if delta.Source != "1.1.0" {
		t.Errorf("delta.Source = %q, want %q", delta.Source, "1.1.0")
	}
}

// TestBundleDrift_Delta_WhenNotStale_OkIsFalse verifies that Delta() returns ok = false
// when the drift is not stale (equal versions).
func TestBundleDrift_Delta_WhenNotStale_OkIsFalse(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}
	drift := plan.BundleStaleness(deployed, "1.0.0", true)

	_, ok := drift.Delta()

	if ok {
		t.Error("BundleDrift.Delta() returned ok = true for a non-stale drift; want ok = false")
	}
}

// TestBundleDrift_Delta_AppliesFalse_OkIsFalse verifies that Delta() returns ok = false
// when applies is false, even if the versions differ. A non-applicable agent is never stale.
func TestBundleDrift_Delta_AppliesFalse_OkIsFalse(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}
	drift := plan.BundleStaleness(deployed, "1.1.0", false) // applies = false

	_, ok := drift.Delta()

	if ok {
		t.Error("BundleDrift.Delta() returned ok = true when applies = false; " +
			"a non-applicable agent is never stale and must return ok = false")
	}
}

// TestBundleDrift_Delta_EmptyDeployedVersion_FieldIsBundleVersion verifies that when the
// deployed version is "" (absent stamp), Delta() still returns the correct field name.
func TestBundleDrift_Delta_EmptyDeployedVersion_FieldIsBundleVersion(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "",
	}
	drift := plan.BundleStaleness(deployed, "1.0.0", true)

	delta, ok := drift.Delta()

	if !ok {
		t.Fatal("BundleDrift.Delta() returned ok = false for stale drift with empty deployed version")
	}
	if delta.Field != plan.BundleDeltaField {
		t.Errorf("delta.Field = %q, want %q", delta.Field, plan.BundleDeltaField)
	}
	if delta.Deployed != "" {
		t.Errorf("delta.Deployed = %q, want empty string (no stamp in deployed file)", delta.Deployed)
	}
}

// ---------------------------------------------------------------------------
// T6.7 — BundleDeltaField constant value and collision avoidance
// ---------------------------------------------------------------------------

// TestBundleDeltaField_IsBundleVersionString verifies that BundleDeltaField has the exact
// value "bundle_version". This value corresponds to the frontmatter key stamped into deployed
// files and is relied on by the update-reason formatter.
func TestBundleDeltaField_IsBundleVersionString(t *testing.T) {
	if plan.BundleDeltaField != "bundle_version" {
		t.Errorf("plan.BundleDeltaField = %q, want %q", plan.BundleDeltaField, "bundle_version")
	}
}

// TestBundleDeltaField_DoesNotCollideWithFrontmatterStampFieldNames verifies that
// BundleDeltaField is distinct from every frontmatter stamp field name that the executor's
// resolveVersionStamp switch handles. A collision would cause the executor to incorrectly
// attempt to write a "bundle_version" key as a non-frontmatter stamp.
func TestBundleDeltaField_DoesNotCollideWithFrontmatterStampFieldNames(t *testing.T) {
	frontmatterStampFields := []string{
		"version",
		"transform_version",
		"injections_version",
		"orchestrator_injections_version",
		"tool_mappings_version",
	}
	for _, field := range frontmatterStampFields {
		if plan.BundleDeltaField == field {
			t.Errorf("BundleDeltaField %q collides with frontmatter stamp field %q; "+
				"the executor's resolveVersionStamp switch would incorrectly handle it",
				plan.BundleDeltaField, field)
		}
	}
}

// TestBundleDeltaField_DoesNotCollideWithProtocolDeltaField verifies that BundleDeltaField
// is distinct from ProtocolDeltaField. A collision would make the update-reason builder
// unable to distinguish bundle staleness from protocol staleness.
func TestBundleDeltaField_DoesNotCollideWithProtocolDeltaField(t *testing.T) {
	if plan.BundleDeltaField == plan.ProtocolDeltaField {
		t.Errorf("BundleDeltaField %q equals ProtocolDeltaField %q; they must be distinct",
			plan.BundleDeltaField, plan.ProtocolDeltaField)
	}
}

// TestBundleDeltaField_DoesNotCollideWithWorkflowDeltaPrefix verifies that BundleDeltaField
// does not start with the WorkflowDeltaFieldPrefix. A collision would cause the update-reason
// builder to misroute the bundle delta through the workflow-reason formatter.
func TestBundleDeltaField_DoesNotCollideWithWorkflowDeltaPrefix(t *testing.T) {
	if strings.HasPrefix(plan.BundleDeltaField, plan.WorkflowDeltaFieldPrefix) {
		t.Errorf("BundleDeltaField %q starts with WorkflowDeltaFieldPrefix %q; "+
			"these must be disjoint to allow correct reason routing",
			plan.BundleDeltaField, plan.WorkflowDeltaFieldPrefix)
	}
}

// ---------------------------------------------------------------------------
// T6.7 — BundleDrift.Reason(): human-readable staleness description
// ---------------------------------------------------------------------------

// TestBundleDrift_Reason_VersionChanged_NamesVersions verifies that when deployed and source
// versions differ (and deployed is non-empty), Reason() returns a string naming both versions
// in the format "bundle version changed (X -> Y)".
func TestBundleDrift_Reason_VersionChanged_NamesVersions(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}
	drift := plan.BundleStaleness(deployed, "1.1.0", true)

	reason := drift.Reason()

	if reason == "" {
		t.Fatal("BundleDrift.Reason() returned empty string for a stale drift with version change; " +
			"want a non-empty reason naming the bundle and both versions")
	}
	if !strings.Contains(reason, "bundle") {
		t.Errorf("Reason() = %q; want it to contain %q", reason, "bundle")
	}
	if !strings.Contains(reason, "1.0.0") {
		t.Errorf("Reason() = %q; want it to contain the deployed version %q", reason, "1.0.0")
	}
	if !strings.Contains(reason, "1.1.0") {
		t.Errorf("Reason() = %q; want it to contain the source version %q", reason, "1.1.0")
	}
}

// TestBundleDrift_Reason_EmptyDeployedVersion_NamesAbsence verifies that when the deployed
// file carries no bundle version stamp (deployed == ""), Reason() returns a string that names
// the absence of the stamp — not a generic "changed from '' to '1.0.0'" message.
func TestBundleDrift_Reason_EmptyDeployedVersion_NamesAbsence(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "",
	}
	drift := plan.BundleStaleness(deployed, "1.0.0", true)

	reason := drift.Reason()

	if reason == "" {
		t.Fatal("BundleDrift.Reason() returned empty string for stale drift with absent stamp; " +
			"want a non-empty reason describing the absence of the bundle version stamp")
	}
	if !strings.Contains(reason, "1.0.0") {
		t.Errorf("Reason() = %q; want it to reference the source version %q", reason, "1.0.0")
	}
	// The reason must describe the absence, not just a generic "changed" message.
	lowerReason := strings.ToLower(reason)
	if !strings.Contains(lowerReason, "no") && !strings.Contains(lowerReason, "absent") &&
		!strings.Contains(lowerReason, "missing") && !strings.Contains(lowerReason, "carries no") {
		t.Errorf("Reason() = %q; want it to describe the absence of the bundle version stamp",
			reason)
	}
}

// TestBundleDrift_Reason_WhenNotStale_IsEmpty verifies that Reason() returns "" when the
// drift is not stale. An empty string signals the reason-builder to omit the bundle line.
func TestBundleDrift_Reason_WhenNotStale_IsEmpty(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}
	drift := plan.BundleStaleness(deployed, "1.0.0", true)

	reason := drift.Reason()

	if reason != "" {
		t.Errorf("BundleDrift.Reason() = %q for a non-stale drift; want empty string", reason)
	}
}

// TestBundleDrift_Reason_AppliesFalse_IsEmpty verifies that Reason() returns "" when applies
// is false, regardless of the version difference. A non-applicable agent is never stale.
func TestBundleDrift_Reason_AppliesFalse_IsEmpty(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:       true,
		BundleVersion: "1.0.0",
	}
	drift := plan.BundleStaleness(deployed, "1.1.0", false) // applies = false

	reason := drift.Reason()

	if reason != "" {
		t.Errorf("BundleDrift.Reason() = %q when applies = false; want empty string", reason)
	}
}
