package transform_test

// owned_key_diff_test.go tests the owned-key difference reporting on
// unconfirmed-origin artifacts at the transform level. These tests exercise
// transform.Apply directly; no pipeline or application-layer fixtures are involved.
//
// Test tasks covered here (transform level; pipeline-level tests are separate):
//
//   T17.2 - Negative: OriginConfirmed (including the zero value) with differing
//           owned keys produces no OwnedKeyDifference entry. Separate sub-cases:
//           (a) written model differs from deployed model
//           (b) written description or name differs from deployed
//           (c) a version or stamp value differs
//           Zero-value safety: an unset Origin field behaves identically to
//           OriginConfirmed, pinning the zero-value-is-confirmed invariant.
//
//   T17.3 - Stamp exclusion: with Origin=OriginUnconfirmed, a differing stamp key
//           (derived from agentfields.All()'s Deployed names) produces no entry while
//           a differing non-stamp owned key on the same request does. The exclusion
//           is stated as its own rule, not as a side effect of the conflict gate.
//
//   T17.3a - Legacy-name test: with Origin=OriginUnconfirmed, a differing "role"
//            (the Legacy name of a registry entry) produces an entry. An exclusion
//            set built from IsMosaicOnlyDeployedKey or from Deployed+Legacy names
//            would silently swallow "role"; this test catches that.
//
//   T17.5 - Positive: with Origin=OriginUnconfirmed and a differing non-stamp owned
//           key, exactly one entry is produced naming the key, the prior deployed
//           value and the incoming value, with a non-empty Reason.
//
// TDD notes:
//   T17.2 cases and T17.3's stamp-key half assert zero entries. These pass trivially
//   without any emission code and act as guards against a "values differ" implementation
//   that would emit on every ordinary run.
//   T17.5, T17.3's non-stamp half, and T17.3a assert non-zero entries. These FAIL
//   until I17.2 implements the emission logic. They are the tests that stop the wrong
//   implementation from being written.

import (
	"testing"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Inline harness descriptors
// ---------------------------------------------------------------------------

// ownedKeyDiffDescriptorYAML is a minimal harness descriptor used by the T17.2 and
// T17.5 tests. It declares a model_key so that the model selection is emitted as the
// "model" frontmatter key in the output. It declares no Drop, Add or key_order rules
// so that source keys pass through without renaming, making the deployed-vs-incoming
// comparison easy to reason about.
const ownedKeyDiffDescriptorYAML = `schema_version: "1"
id: "owned-key-diff-harness"
display_name: "Owned Key Diff Test Harness"
frontmatter:
  model_key: "model"
`

// ownedKeyDiffStampDescriptorYAML adds transform_version: "2.0.0" so that the
// transform output carries mosaic_harness_version: "2.0.0". The T17.3 stamp-exclusion
// test uses this to produce a deployed form with mosaic_harness_version: "1.0.0" that
// differs from the incoming "2.0.0", while also carrying a non-stamp key difference.
const ownedKeyDiffStampDescriptorYAML = `schema_version: "1"
id: "owned-key-diff-stamp-harness"
display_name: "Owned Key Diff Stamp Test Harness"
transform_version: "2.0.0"
frontmatter:
  model_key: "model"
`

// ---------------------------------------------------------------------------
// Source documents (generic form - what the transform receives as input)
// ---------------------------------------------------------------------------

// ownedKeyDiffSource is a minimal generic source. The description ("New description")
// differs from the value in ownedKeyDiffDeployedDescDiffers, making it the primary
// vehicle for tests that assert a description difference.
const ownedKeyDiffSource = `---
id: diff-test-agent
version: 1.0.0
description: New description
model: {model-identifier}
---

<Identity type="core">
Body for owned-key diff tests.
</Identity>
`

// ownedKeyDiffSourceWithRole is a source carrying role: orchestrator. After the
// transform the output carries mosaic_role: orchestrator (bare "role" key is renamed).
// The T17.3a test uses this together with a deployed form that carries the Legacy
// bare key role: subagent, so that "role" appears in deployed but not in incoming.
const ownedKeyDiffSourceWithRole = `---
id: diff-test-agent
version: 1.0.0
description: Same description
model: {model-identifier}
role: orchestrator
---

<Identity type="core">
Body for owned-key diff role test.
</Identity>
`

// ownedKeyDiffSourceSameDesc carries description: "Same description". Used in T17.2(c)
// together with ownedKeyDiffDeployedStampDiffers (which also has "Same description"),
// so that description is not an active variable and only the stamp key differs.
// Used in T17.3 together with ownedKeyDiffDeployedStampAndDescDiffer (which has
// "Old description"), so that description IS the non-stamp key that differs.
const ownedKeyDiffSourceSameDesc = `---
id: diff-test-agent
version: 1.0.0
description: Same description
model: {model-identifier}
---

<Identity type="core">
Body for owned-key diff tests.
</Identity>
`

// ---------------------------------------------------------------------------
// Deployed forms (canonical form - what the deployed file looks like)
// ---------------------------------------------------------------------------

// ownedKeyDiffDeployedDescDiffers is a canonical deployed form where description
// differs from ownedKeyDiffSource ("Old description" vs "New description") and model
// matches the test model selection so model is not an extra variable.
const ownedKeyDiffDeployedDescDiffers = `---
mosaic_id: diff-test-agent
mosaic_version: 1.0.0
description: Old description
model: test/same-model
---

Body for owned-key diff tests.
`

// ownedKeyDiffDeployedModelDiffers is a canonical deployed form where model differs
// from the incoming selection (old-model/v1) but description matches the source
// ("New description") so only model is an active difference.
const ownedKeyDiffDeployedModelDiffers = `---
mosaic_id: diff-test-agent
mosaic_version: 1.0.0
description: New description
model: old-model/v1
---

Body for owned-key diff tests.
`

// ownedKeyDiffDeployedNameDiffers is a canonical deployed form where name differs
// from the source. Used by T17.2(b) to verify a name difference with OriginConfirmed
// produces no entry.
const ownedKeyDiffDeployedNameDiffers = `---
mosaic_id: diff-test-agent
mosaic_version: 1.0.0
name: old-agent-name
description: New description
model: test/same-model
---

Body for owned-key diff tests.
`

// ownedKeyDiffDeployedStampDiffers is a canonical deployed form where
// mosaic_harness_version (a stamp key) differs from the incoming value that
// ownedKeyDiffStampDescriptorYAML would produce ("2.0.0"). Description is
// the same on both sides so the only candidate difference is the stamp key.
const ownedKeyDiffDeployedStampDiffers = `---
mosaic_id: diff-test-agent
mosaic_version: 1.0.0
mosaic_harness_version: 1.0.0
description: Same description
model: test/same-model
---

Body for owned-key diff tests.
`

// ownedKeyDiffDeployedStampAndDescDiffer is a canonical deployed form where both
// mosaic_harness_version (stamp) and description (non-stamp) differ from incoming.
// T17.3 uses this to assert: no entry for the stamp, one entry for description.
const ownedKeyDiffDeployedStampAndDescDiffer = `---
mosaic_id: diff-test-agent
mosaic_version: 1.0.0
mosaic_harness_version: 1.0.0
description: Old description
model: test/same-model
---

Body for owned-key diff tests.
`

// ownedKeyDiffDeployedWithCarriage is a canonical deployed form that carries the
// mosaic_carriage carriage container key alongside a differing description.
// Used by the carriage key exclusion tests: the mosaic_carriage key should produce no
// OwnedKeyDifference entry while the differing description should produce one, proving
// that carriage key exclusion is selective rather than blanket suppression.
const ownedKeyDiffDeployedWithCarriage = `---
mosaic_id: diff-test-agent
mosaic_version: 1.0.0
description: Old description
model: test/same-model
mosaic_carriage: {user_key: some_value}
---

Body for owned-key diff tests.
`

// ownedKeyDiffDeployedWithLegacyRole is a canonical deployed form using the bare
// (Legacy) "role" key rather than the deployed "mosaic_role" form. T17.3a uses this
// to verify that "role" is not swallowed by an exclusion set built from Deployed names,
// since "role" is a Legacy name and only "mosaic_role" is in the Deployed-name set.
const ownedKeyDiffDeployedWithLegacyRole = `---
mosaic_id: diff-test-agent
mosaic_version: 1.0.0
description: Same description
role: subagent
model: test/same-model
---

Body for owned-key diff role test.
`

// ---------------------------------------------------------------------------
// Model selections
// ---------------------------------------------------------------------------

// ownedKeyDiffModelSame returns a model selection whose ID matches the "model" value
// in the deployed forms that carry model: test/same-model. Using the same model on
// both sides eliminates model as a variable in tests that only want to examine another key.
func ownedKeyDiffModelSame() domain.ModelSelection {
	return domain.ModelSelection{
		ModelID: "test/same-model",
		Origin:  domain.OriginHarnessList,
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// findOwnedKeyDiff returns the first OwnedKeyDifference with the given key, or nil.
func findOwnedKeyDiff(diffs []transform.OwnedKeyDifference, key string) *transform.OwnedKeyDifference {
	for i := range diffs {
		if diffs[i].Key == key {
			return &diffs[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// T17.2 - Zero-value safety: unset Origin behaves as OriginConfirmed
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_ZeroValue_NoEntry pins the zero-value-is-OriginConfirmed invariant.
// A Request with Origin not explicitly set and differing owned keys must produce no
// OwnedKeyDifference entries. If the zero value were ever changed to a non-OriginConfirmed
// constant, every existing transform.Request literal that omits the field would silently
// activate difference reporting on all runs.
func TestOwnedKeyDiff_ZeroValue_NoEntry(t *testing.T) {
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedDescDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		// Origin intentionally not set; zero value must equal OriginConfirmed.
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Report.OwnedKeyDifferences) != 0 {
		t.Errorf("zero-value Origin with differing description: want 0 OwnedKeyDifferences, got %d: %v",
			len(result.Report.OwnedKeyDifferences), result.Report.OwnedKeyDifferences)
	}
}

// ---------------------------------------------------------------------------
// T17.2(a) - OriginConfirmed: differing model produces no entry
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_OriginConfirmed_DifferingModel_NoEntry asserts that a model change
// in configuration (a normal run event for an untouched deployed file) produces no
// OwnedKeyDifference entry when Origin is OriginConfirmed.
func TestOwnedKeyDiff_OriginConfirmed_DifferingModel_NoEntry(t *testing.T) {
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedModelDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginConfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Report.OwnedKeyDifferences) != 0 {
		t.Errorf("Origin=OriginConfirmed with differing model: want 0 OwnedKeyDifferences, got %d: %v",
			len(result.Report.OwnedKeyDifferences), result.Report.OwnedKeyDifferences)
	}
}

// ---------------------------------------------------------------------------
// T17.2(b) - OriginConfirmed: differing description or name produces no entry
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_OriginConfirmed_DifferingDescription_NoEntry asserts that an edited
// catalog description (a normal run event) produces no OwnedKeyDifference entry when
// Origin is OriginConfirmed.
func TestOwnedKeyDiff_OriginConfirmed_DifferingDescription_NoEntry(t *testing.T) {
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedDescDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginConfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Report.OwnedKeyDifferences) != 0 {
		t.Errorf("Origin=OriginConfirmed with differing description: want 0 OwnedKeyDifferences, got %d: %v",
			len(result.Report.OwnedKeyDifferences), result.Report.OwnedKeyDifferences)
	}
}

// TestOwnedKeyDiff_OriginConfirmed_DifferingName_NoEntry asserts that a changed source
// agent name (a normal run event) produces no OwnedKeyDifference entry when Origin is
// OriginConfirmed.
func TestOwnedKeyDiff_OriginConfirmed_DifferingName_NoEntry(t *testing.T) {
	// Source carries no "name" field; deployed form carries name: old-agent-name.
	// The incoming form will not carry "name" (minimal descriptor, source has no name).
	// With OriginConfirmed the absence-vs-presence difference must not produce an entry.
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedNameDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginConfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Report.OwnedKeyDifferences) != 0 {
		t.Errorf("Origin=OriginConfirmed with differing name: want 0 OwnedKeyDifferences, got %d: %v",
			len(result.Report.OwnedKeyDifferences), result.Report.OwnedKeyDifferences)
	}
}

// ---------------------------------------------------------------------------
// T17.2(c) - OriginConfirmed: differing stamp value produces no entry
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_OriginConfirmed_DifferingStamp_NoEntry asserts that a bumped source
// version or harness version stamp (both normal run events) produces no OwnedKeyDifference
// entry when Origin is OriginConfirmed. Stamp keys are always excluded from difference
// reporting regardless of the Origin value; this test pins the OriginConfirmed gate on
// top of that.
func TestOwnedKeyDiff_OriginConfirmed_DifferingStamp_NoEntry(t *testing.T) {
	// ownedKeyDiffStampDescriptorYAML produces mosaic_harness_version: 2.0.0 in the output.
	// The deployed form carries mosaic_harness_version: 1.0.0 -- a stamp value difference.
	// With OriginConfirmed no entry must be produced.
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSourceSameDesc),
		Deployed: []byte(ownedKeyDiffDeployedStampDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffStampDescriptorYAML, "inline:owned-key-diff-stamp"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginConfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Report.OwnedKeyDifferences) != 0 {
		t.Errorf("Origin=OriginConfirmed with differing stamp: want 0 OwnedKeyDifferences, got %d: %v",
			len(result.Report.OwnedKeyDifferences), result.Report.OwnedKeyDifferences)
	}
}

// ---------------------------------------------------------------------------
// T17.3 - Stamp exclusion with OriginUnconfirmed
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_StampExclusion_StampKeyProducesNoEntry asserts that a differing
// stamp key (mosaic_harness_version) produces no OwnedKeyDifference entry even when
// Origin is OriginUnconfirmed. The excluded set is derived from agentfields.All()
// Deployed names, not from a literal list.
//
// This test is paired with TestOwnedKeyDiff_StampExclusion_NonStampKeyProducesEntry:
// both use the same request so the stamp exclusion is demonstrated as its own rule,
// not as a side effect of some other gate.
func TestOwnedKeyDiff_StampExclusion_StampKeyProducesNoEntry(t *testing.T) {
	// Build the stamp exclusion set from agentfields.All() Deployed names.
	stampNames := make(map[string]bool)
	for _, f := range agentfields.All() {
		stampNames[f.Deployed] = true
	}

	req := transform.Request{
		Source:   []byte(ownedKeyDiffSourceSameDesc),
		Deployed: []byte(ownedKeyDiffDeployedStampAndDescDiffer),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffStampDescriptorYAML, "inline:owned-key-diff-stamp"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// No OwnedKeyDifference entry may have a stamp key.
	for _, diff := range result.Report.OwnedKeyDifferences {
		if stampNames[diff.Key] {
			t.Errorf("OwnedKeyDifference entry produced for stamp key %q; "+
				"stamp keys must be excluded regardless of Origin value", diff.Key)
		}
	}
}

// TestOwnedKeyDiff_StampExclusion_NonStampKeyProducesEntry asserts that a differing
// non-stamp owned key (description) produces an OwnedKeyDifference entry when Origin
// is OriginUnconfirmed and the deployed form differs from the incoming value.
//
// This test FAILS until I17.2 implements the emission logic. It is the counterpart to
// TestOwnedKeyDiff_StampExclusion_StampKeyProducesNoEntry and together the two tests
// prove that stamp exclusion is a selective filter, not a blanket suppression.
func TestOwnedKeyDiff_StampExclusion_NonStampKeyProducesEntry(t *testing.T) {
	// ownedKeyDiffDeployedStampAndDescDiffer has:
	//   mosaic_harness_version: 1.0.0 (stamp, excluded from reporting)
	//   description: Old description   (non-stamp, should produce entry)
	// ownedKeyDiffSourceSameDesc produces description: "Same description" after transform.
	// "Old description" (deployed) != "Same description" (incoming) so an entry is expected.
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSourceSameDesc),
		Deployed: []byte(ownedKeyDiffDeployedStampAndDescDiffer),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffStampDescriptorYAML, "inline:owned-key-diff-stamp"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entry := findOwnedKeyDiff(result.Report.OwnedKeyDifferences, "description")
	if entry == nil {
		t.Errorf("Origin=OriginUnconfirmed with differing description: "+
			"want an OwnedKeyDifference entry for \"description\", got none; "+
			"all entries: %v", result.Report.OwnedKeyDifferences)
	}
}

// ---------------------------------------------------------------------------
// T17.3a - Legacy name "role" is not in the exclusion set
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_LegacyRole_ProducesEntry asserts that a differing "role" key (the
// Legacy name of the role registry entry) produces an OwnedKeyDifference entry when
// Origin is OriginUnconfirmed.
//
// "role" is the Legacy name of the entry whose Deployed name is "mosaic_role". An
// exclusion set built from IsMosaicOnlyDeployedKey or from Deployed+Legacy names would
// include "role" and silently suppress the entry. The correct implementation uses only
// Deployed names, so "role" remains in scope.
//
// Setup: deployed form carries role: subagent (bare Legacy key). The source carries
// role: orchestrator, which the transform renames to mosaic_role: orchestrator in the
// output. The comparison therefore finds "role" in deployed but absent in incoming,
// which is a difference on a key not in the Deployed-names exclusion set.
//
// This test FAILS until I17.2 implements the emission logic with the correct exclusion set.
func TestOwnedKeyDiff_LegacyRole_ProducesEntry(t *testing.T) {
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSourceWithRole),
		Deployed: []byte(ownedKeyDiffDeployedWithLegacyRole),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entry := findOwnedKeyDiff(result.Report.OwnedKeyDifferences, "role")
	if entry == nil {
		t.Errorf("Origin=OriginUnconfirmed with role in deployed but absent in incoming: "+
			"want an OwnedKeyDifference entry for \"role\", got none; "+
			"all entries: %v", result.Report.OwnedKeyDifferences)
	}
}

// TestOwnedKeyDiff_LegacyRole_DeployedPresentIncomingAbsent verifies the presence flags
// on the entry produced for the "role" key. The deployed side has role: subagent (present)
// and the incoming side does not carry "role" after the rename (absent).
//
// This test FAILS until I17.2 implements the emission logic.
func TestOwnedKeyDiff_LegacyRole_DeployedPresentIncomingAbsent(t *testing.T) {
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSourceWithRole),
		Deployed: []byte(ownedKeyDiffDeployedWithLegacyRole),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entry := findOwnedKeyDiff(result.Report.OwnedKeyDifferences, "role")
	if entry == nil {
		t.Fatalf("no OwnedKeyDifference entry for \"role\"; cannot check presence flags")
	}
	if !entry.DeployedPresent {
		t.Errorf("role entry: DeployedPresent = false; deployed form carries role: subagent so it must be present")
	}
	if entry.IncomingPresent {
		t.Errorf("role entry: IncomingPresent = true; incoming form renames role to mosaic_role so role must be absent")
	}
}

// ---------------------------------------------------------------------------
// Carriage key exclusion: mosaic_carriage and mosaic_carriage_markers produce no entry
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_CarriageKeyExclusion_NoEntry asserts that the carriage container
// keys (mosaic_carriage and mosaic_carriage_markers) produce no OwnedKeyDifference
// entry when Origin is OriginUnconfirmed, even when the deployed form carries those
// keys and the incoming form does not.
//
// The carriage keys are reserved frontmatter containers that are never written into
// any format's encoded output, so their presence in a deployed file but absence in
// the incoming form would produce a spurious entry for every deployed file that
// carries user-owned TOML keys. The IsCarriageKey exclusion guard (added in
// implementation-tdd#204) prevents this.
//
// The exclusion set is derived from agentformat.CarriageKeys() so that this test
// remains accurate if new carriage container keys are added in the future.
func TestOwnedKeyDiff_CarriageKeyExclusion_NoEntry(t *testing.T) {
	// Build the carriage key set from agentformat.CarriageKeys().
	carriageKeySet := make(map[string]bool)
	for _, k := range agentformat.CarriageKeys() {
		carriageKeySet[k] = true
	}

	// Deployed form carries mosaic_carriage (a carriage container key) that the
	// incoming form does not carry. With OriginUnconfirmed and the IsCarriageKey
	// guard in place, no entry must be produced for mosaic_carriage.
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedWithCarriage),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// No OwnedKeyDifference entry may have a carriage container key.
	for _, diff := range result.Report.OwnedKeyDifferences {
		if carriageKeySet[diff.Key] {
			t.Errorf("OwnedKeyDifference entry produced for carriage container key %q; "+
				"carriage keys must be excluded from owned-key difference reporting", diff.Key)
		}
	}
}

// TestOwnedKeyDiff_CarriageKeyExclusion_NonCarriageKeyProducesEntry verifies that the
// carriage key exclusion is selective: a differing non-carriage owned key (description)
// on the same request produces an entry. This prevents the test from being satisfied by
// an implementation that emits nothing at all.
//
// Together with TestOwnedKeyDiff_CarriageKeyExclusion_NoEntry, these two tests prove
// that carriage key exclusion is a targeted filter, not a blanket suppression -- the
// same relationship TestOwnedKeyDiff_StampExclusion_StampKeyProducesNoEntry and
// TestOwnedKeyDiff_StampExclusion_NonStampKeyProducesEntry hold for stamp exclusion.
func TestOwnedKeyDiff_CarriageKeyExclusion_NonCarriageKeyProducesEntry(t *testing.T) {
	// ownedKeyDiffDeployedWithCarriage carries:
	//   mosaic_carriage: {user_key: some_value}  (carriage key, excluded)
	//   description: Old description              (non-carriage key, should produce entry)
	// ownedKeyDiffSource has description: New description, so the difference is active.
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedWithCarriage),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entry := findOwnedKeyDiff(result.Report.OwnedKeyDifferences, "description")
	if entry == nil {
		t.Errorf("Origin=OriginUnconfirmed with differing description on a form that also "+
			"carries mosaic_carriage: want an OwnedKeyDifference entry for \"description\", "+
			"got none; all entries: %v", result.Report.OwnedKeyDifferences)
	}
}

// ---------------------------------------------------------------------------
// T17.5 - Positive: OriginUnconfirmed with differing non-stamp key emits one entry
// ---------------------------------------------------------------------------

// TestOwnedKeyDiff_OriginUnconfirmed_DifferingDescription_ProducesEntry is the
// positive emission test. With Origin=OriginUnconfirmed and description differing
// between deployed and incoming, exactly one OwnedKeyDifference entry must be
// produced for "description".
//
// This test FAILS until I17.2 implements the emission logic. It is the counterpart
// that stops T17.2 and T17.3 from being satisfiable by emitting nothing ever.
func TestOwnedKeyDiff_OriginUnconfirmed_DifferingDescription_ProducesEntry(t *testing.T) {
	// Deployed: description: Old description
	// Incoming: description: New description (from ownedKeyDiffSource)
	// Model same on both sides so model does not contribute additional entries.
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedDescDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entry := findOwnedKeyDiff(result.Report.OwnedKeyDifferences, "description")
	if entry == nil {
		t.Fatalf("Origin=OriginUnconfirmed with description differing: "+
			"want an OwnedKeyDifference entry for \"description\", got none; "+
			"all entries: %v", result.Report.OwnedKeyDifferences)
	}
}

// TestOwnedKeyDiff_OriginUnconfirmed_EntryNamesKeyAndBothValues asserts that the
// OwnedKeyDifference entry for a differing description carries the correct key name,
// the prior deployed value ("Old description") and the incoming value ("New description").
// Values are compared as decoded canonical scalars (unquoted scalar text), per the
// value-level relation.
//
// This test FAILS until I17.2 implements the emission logic.
func TestOwnedKeyDiff_OriginUnconfirmed_EntryNamesKeyAndBothValues(t *testing.T) {
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedDescDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entry := findOwnedKeyDiff(result.Report.OwnedKeyDifferences, "description")
	if entry == nil {
		t.Fatalf("no OwnedKeyDifference entry for \"description\"; cannot check values")
	}
	if entry.Key != "description" {
		t.Errorf("entry.Key = %q, want \"description\"", entry.Key)
	}
	if !entry.DeployedPresent {
		t.Errorf("entry.DeployedPresent = false; deployed form carries description so it must be present")
	}
	if entry.Deployed != "Old description" {
		t.Errorf("entry.Deployed = %q, want \"Old description\" (unescaped scalar text)", entry.Deployed)
	}
	if !entry.IncomingPresent {
		t.Errorf("entry.IncomingPresent = false; incoming form carries description so it must be present")
	}
	if entry.Incoming != "New description" {
		t.Errorf("entry.Incoming = %q, want \"New description\" (unescaped scalar text)", entry.Incoming)
	}
}

// TestOwnedKeyDiff_OriginUnconfirmed_EntryHasNonEmptyReason asserts that the
// OwnedKeyDifference entry carries a non-empty Reason field. The Reason is surfaced
// in the run report and must identify the nature of the difference.
//
// This test FAILS until I17.2 implements the emission logic.
func TestOwnedKeyDiff_OriginUnconfirmed_EntryHasNonEmptyReason(t *testing.T) {
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedDescDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entry := findOwnedKeyDiff(result.Report.OwnedKeyDifferences, "description")
	if entry == nil {
		t.Fatalf("no OwnedKeyDifference entry for \"description\"; cannot check Reason")
	}
	if entry.Reason == "" {
		t.Errorf("OwnedKeyDifference.Reason is empty; every entry must document the reason for the difference")
	}
}

// TestOwnedKeyDiff_OriginUnconfirmedUnparseable_NoEntry asserts that Origin=
// OriginUnconfirmedUnparseable produces no OwnedKeyDifference entries even when keys
// differ. Rule 2: when the deployed file could not be parsed, there is no decoded
// canonical form to compare against, so nothing is fabricated.
func TestOwnedKeyDiff_OriginUnconfirmedUnparseable_NoEntry(t *testing.T) {
	req := transform.Request{
		Source:   []byte(ownedKeyDiffSource),
		Deployed: []byte(ownedKeyDiffDeployedDescDiffers),
		Kind:     domain.ArtifactAgent,
		Key:      "diff-test-agent",
		Module:   newDescriptorModule(t, ownedKeyDiffDescriptorYAML, "inline:owned-key-diff"),
		Model:    ownedKeyDiffModelSame(),
		Scope:    domain.ScopeProject,
		Origin:   transform.OriginUnconfirmedUnparseable,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Report.OwnedKeyDifferences) != 0 {
		t.Errorf("Origin=OriginUnconfirmedUnparseable: want 0 OwnedKeyDifferences, got %d: %v",
			len(result.Report.OwnedKeyDifferences), result.Report.OwnedKeyDifferences)
	}
}
