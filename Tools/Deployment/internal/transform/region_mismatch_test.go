package transform_test

// region_mismatch_test.go covers marker/name mismatch rejection (T3.4):
//
//   - A source document that declares a tool-managed canonical name (e.g. HarnessConstraints)
//     under [[INJECTION:]] fails the transform with an error wrapping ErrMarkerMismatch.
//   - A source document that declares a user-owned canonical name (e.g. CodebaseContext)
//     under [[DEPLOYED:]] fails the transform with an error wrapping ErrMarkerMismatch.
//   - The error message names the offending region, so the user can locate and fix it.
//   - A source document with an unrecognised name under [[DEPLOYED:]] fails the transform
//     with an error wrapping ErrUnknownDeployedName (a tool-managed name has no generator).
//
// These tests establish that the transform never guesses: marker/name pairings are validated
// at transform time, not at validation time, so a file that passes validation but is routed
// through the wrong path is still caught.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// T3.4: Tool-managed name under [[INJECTION:]] fails with ErrMarkerMismatch
// ---------------------------------------------------------------------------

// TestMarkerMismatch_ToolManagedNameUnderInjection_Rejected asserts that a source document
// that declares HarnessConstraints with [[INJECTION:]] — the wrong marker for a tool-managed
// name — causes Apply to return an error wrapping ErrMarkerMismatch. The transform never
// produces partial output for a mismatch.
func TestMarkerMismatch_ToolManagedNameUnderInjection_Rejected(t *testing.T) {
	// sourceWithHarnessConstraintsUnderInjection uses [[INJECTION:HarnessConstraints]] for a
	// tool-managed name. This is the wrong marker: HarnessConstraints must be declared with
	// [[DEPLOYED:]].
	const sourceWithHarnessConstraintsUnderInjection = `---
id: 60
version: 1.0.0
name: mismatch-test
description: Agent for marker mismatch testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: mismatch testing
required_skills: []
---

[[SECTION:Constraints]]
## Constraints

[[INJECTION:HarnessConstraints]]
[[/INJECTION:HarnessConstraints]]
[[/SECTION:Constraints]]
`

	req := transform.Request{
		Source:   []byte(sourceWithHarnessConstraintsUnderInjection),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "mismatch-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when a tool-managed name is declared under [[INJECTION:]], got nil")
	}
	if !errors.Is(err, transform.ErrMarkerMismatch) {
		t.Errorf("expected error wrapping transform.ErrMarkerMismatch; got: %v", err)
	}
}

// TestMarkerMismatch_WorkflowNameUnderInjection_Rejected asserts that
// [[INJECTION:AvailableWorkflows]] — a tool-managed name declared under the wrong marker —
// is rejected.
func TestMarkerMismatch_WorkflowNameUnderInjection_Rejected(t *testing.T) {
	const sourceWithAvailableWorkflowsUnderInjection = `---
id: 60
version: 1.0.0
name: mismatch-workflow-test
description: Agent for workflow mismatch testing
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: mismatch testing
required_skills: []
---

[[SECTION:Identity]]
# Mismatch Agent

[[INJECTION:AvailableWorkflows]]
[[/INJECTION:AvailableWorkflows]]
[[/SECTION:Identity]]
`

	req := transform.Request{
		Source:   []byte(sourceWithAvailableWorkflowsUnderInjection),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "mismatch-workflow-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when AvailableWorkflows is declared under [[INJECTION:]], got nil")
	}
	if !errors.Is(err, transform.ErrMarkerMismatch) {
		t.Errorf("expected error wrapping transform.ErrMarkerMismatch; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.4: Name with no tool-managed generator under [[DEPLOYED:]] fails with ErrUnknownDeployedName
// ---------------------------------------------------------------------------
//
// In Stage 2, the user-owned injection registry (CanonicalInjections) is removed.
// Names such as CodebaseContext and IdentityExtension are no longer in any canonical
// registry. [[DEPLOYED:CodebaseContext]] is now an unknown deployed name (not a marker
// mismatch), because there is no user-owned registry to detect the mismatch against.
// The transform still rejects them — only the error type changes.

// TestMarkerMismatch_UserOwnedNameUnderDeployed_Rejected asserts that a source document
// that declares CodebaseContext with [[DEPLOYED:]] — a name with no tool-managed generator —
// causes Apply to return an error wrapping ErrUnknownDeployedName.
func TestMarkerMismatch_UserOwnedNameUnderDeployed_Rejected(t *testing.T) {
	// sourceWithCodebaseContextUnderDeployed uses [[DEPLOYED:CodebaseContext]] for a name
	// that has no tool-managed generator. After Stage 2 there is no user-owned registry,
	// so the name is unknown-deployed rather than a marker mismatch.
	const sourceWithCodebaseContextUnderDeployed = `---
id: 60
version: 1.0.0
name: mismatch-user-owned-test
description: Agent for user-owned mismatch testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: user-owned mismatch testing
required_skills: []
---

[[SECTION:Capabilities]]
## Capabilities

[[DEPLOYED:CodebaseContext]]
[[/DEPLOYED:CodebaseContext]]
[[/SECTION:Capabilities]]
`

	req := transform.Request{
		Source:   []byte(sourceWithCodebaseContextUnderDeployed),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "mismatch-user-owned-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when a name with no tool-managed generator is declared under [[DEPLOYED:]], got nil")
	}
	if !errors.Is(err, transform.ErrUnknownDeployedName) {
		t.Errorf("expected error wrapping transform.ErrUnknownDeployedName; got: %v", err)
	}
}

// TestMarkerMismatch_IdentityExtensionUnderDeployed_Rejected asserts that
// [[DEPLOYED:IdentityExtension]] — a name with no tool-managed generator — is rejected.
func TestMarkerMismatch_IdentityExtensionUnderDeployed_Rejected(t *testing.T) {
	const sourceWithIdentityExtensionUnderDeployed = `---
id: 60
version: 1.0.0
name: mismatch-identity-test
description: Agent for IdentityExtension mismatch testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: identity mismatch testing
required_skills: []
---

[[SECTION:Identity]]
# Mismatch Agent

[[DEPLOYED:IdentityExtension]]
[[/DEPLOYED:IdentityExtension]]
[[/SECTION:Identity]]
`

	req := transform.Request{
		Source:   []byte(sourceWithIdentityExtensionUnderDeployed),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "mismatch-identity-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when IdentityExtension is declared under [[DEPLOYED:]], got nil")
	}
	if !errors.Is(err, transform.ErrUnknownDeployedName) {
		t.Errorf("expected error wrapping transform.ErrUnknownDeployedName; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.4: Error message names the offending region
// ---------------------------------------------------------------------------

// TestMarkerMismatch_ErrorMessageNamesOffendingRegion asserts that the error returned for a
// marker/name mismatch names the offending region in the message, so the user can locate and
// fix it without having to scan the entire source document.
func TestMarkerMismatch_ErrorMessageNamesOffendingRegion(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		regionName string
	}{
		{
			name: "tool-managed under injection",
			source: `---
id: 61
version: 1.0.0
name: error-name-test
description: Error name test
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: error name testing
required_skills: []
---

[[SECTION:Constraints]]
## Constraints

[[INJECTION:CustomConstraints]]
[[/INJECTION:CustomConstraints]]
[[/SECTION:Constraints]]
`,
			regionName: "CustomConstraints",
		},
		{
			name: "user-owned under deployed",
			source: `---
id: 62
version: 1.0.0
name: error-name-test-2
description: Error name test 2
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: error name testing 2
required_skills: []
---

[[SECTION:Capabilities]]
## Capabilities

[[DEPLOYED:OutputArtifactTemplate]]
[[/DEPLOYED:OutputArtifactTemplate]]
[[/SECTION:Capabilities]]
`,
			regionName: "OutputArtifactTemplate",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			req := transform.Request{
				Source:   []byte(c.source),
				Deployed: nil,
				Kind:     domain.ArtifactAgent,
				Key:      "error-name-test",
				Module:   newFixtureModule(t),
				Model:    fixtureModel(),
				Scope:    domain.ScopeProject,
			}

			_, err := transform.Apply(req)
			if err == nil {
				t.Fatalf("Apply must return an error for %s mismatch, got nil", c.name)
			}
			if !strings.Contains(err.Error(), c.regionName) {
				t.Errorf("error message must name the offending region %q;\ngot: %v", c.regionName, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T3.4: Unrecognised [[DEPLOYED:]] name fails with ErrUnknownDeployedName
// ---------------------------------------------------------------------------

// TestMarkerMismatch_UnknownDeployedName_Rejected asserts that a source document that
// declares an unrecognised name under [[DEPLOYED:]] fails the transform with an error
// wrapping docformat.ErrUnknownDeployedName. An unrecognised tool-managed name has no
// generator and cannot be filled; accepting it silently would produce an empty region.
func TestMarkerMismatch_UnknownDeployedName_Rejected(t *testing.T) {
	const sourceWithUnknownDeployedName = `---
id: 60
version: 1.0.0
name: unknown-deployed-test
description: Agent for unknown deployed name testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: unknown deployed testing
required_skills: []
---

[[SECTION:Capabilities]]
## Capabilities

[[DEPLOYED:UnrecognisedToolManagedName]]
[[/DEPLOYED:UnrecognisedToolManagedName]]
[[/SECTION:Capabilities]]
`

	req := transform.Request{
		Source:   []byte(sourceWithUnknownDeployedName),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "unknown-deployed-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error for an unrecognised [[DEPLOYED:]] name, got nil")
	}
	if !errors.Is(err, docformat.ErrUnknownDeployedName) {
		t.Errorf("expected error wrapping docformat.ErrUnknownDeployedName; got: %v", err)
	}
	if !strings.Contains(err.Error(), "UnrecognisedToolManagedName") {
		t.Errorf("error must name the unrecognised region; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.4: Well-formed source with correct markers is not rejected
// ---------------------------------------------------------------------------

// TestMarkerMismatch_CorrectMarkersNotRejected asserts that a source document that correctly
// uses [[DEPLOYED:]] for tool-managed names and [[INJECTION:]] for user-owned names is
// accepted by Apply with no error. This is the positive control for the rejection tests.
func TestMarkerMismatch_CorrectMarkersNotRejected(t *testing.T) {
	const sourceWithCorrectMarkers = `---
id: 60
version: 1.0.0
name: correct-markers-test
description: Agent with correctly marked regions
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: correct markers testing
required_skills: []
---

[[SECTION:Identity]]
# CorrectMarkersTest Agent

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

[[INJECTION:CodebaseContext]]
[[/INJECTION:CodebaseContext]]
[[/SECTION:Capabilities]]

[[SECTION:Constraints]]
## Constraints

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

	req := transform.Request{
		Source:   []byte(sourceWithCorrectMarkers),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "correct-markers-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Errorf("Apply must not return an error for a source with correctly marked regions; got: %v", err)
	}
}
