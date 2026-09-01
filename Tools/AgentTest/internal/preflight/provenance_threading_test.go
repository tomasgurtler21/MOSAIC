package preflight_test

// Tests for Stage 3: provenance threading through preflight call sites.
//
// These tests verify that when MosaicRootProvenance is supplied in
// preflight.Input, the resulting diagnostics from each affected call site
// include:
//   - The operation context (describing what was being attempted)
//   - The configuration provenance (which resolution tier produced MosaicRoot)
//   - A non-empty actionable Hint
//   - The expected diagnostic Code
//
// Three call sites are tested at integration level via preflight.Validate:
//   - checkSubjectRender default branch (unrecognized/fallback reason codes)
//   - checkSubjectDeploy (catalogue-path deploy failure)
//   - The stub-agent render loop (stub definition render failure)
//
// All tests use the fakeDeployer double defined in deployment_validation_test.go
// and the writeTree helper defined in preflight_test.go.
//
// Tests also verify the existing dedicated branches (agent-not-in-catalog,
// unknown-workflow) are unaffected by this change.
//
// These tests are in the TDD RED phase: they will fail until I3.1 and I3.2
// add MosaicRootProvenance to preflight.Input and wire it through each call site.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/agentdeploy"
	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// --- provenance helpers ---

// provenanceDefaultTier is the expected provenance string when MosaicRoot is
// resolved from the default tier. Tests that set this provenance in Input are
// asserting that the value flows verbatim through WiringConfig and Input to
// each diagnostic helper call.
const provenanceDefaultTier = "--mosaic-root default: selfDir/../../.."

// provenanceFlagTier is the expected provenance string when MosaicRoot is
// supplied via the --mosaic-root CLI flag.
const provenanceFlagTier = "--mosaic-root flag"

// provenanceEnvTier is the expected provenance string when MosaicRoot is
// resolved from the environment variable.
const provenanceEnvTier = "--mosaic-root env: MOSAIC_AGENT_TEST_MOSAIC_ROOT"

// findDiagnosticByCode returns the first diagnostic with the given code and
// whether it was found. This helper is used when a test needs to inspect a
// specific diagnostic's fields (Message, Hint) rather than just its presence.
func findDiagnosticByCode(report authoring.Report, code string) (authoring.Diagnostic, bool) {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return d, true
		}
	}
	return authoring.Diagnostic{}, false
}

// --- checkSubjectRender default branch: provenance threading ---

// TestValidate_SubjectRenderDefaultBranch_MessageContainsProvenance verifies
// that when checkSubjectRender's default branch (fallback for unrecognized
// reason codes) fires, the resulting unrenderable-subject-declaration diagnostic
// message contains the MosaicRootProvenance supplied in Input. This is the core
// provenance threading contract: the call site must forward Input.MosaicRootProvenance
// to DelegateToolDiagnostic as FailureContext.Provenance.
func TestValidate_SubjectRenderDefaultBranch_MessageContainsProvenance(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderError("harness-unresolvable", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q when Deploy returns reason %q, got: %v",
			"unrenderable-subject-declaration", "harness-unresolvable", report.Diagnostics)
	}
	if !strings.Contains(d.Message, provenanceDefaultTier) {
		t.Errorf("Message does not contain MosaicRootProvenance %q; got: %q",
			provenanceDefaultTier, d.Message)
	}
}

// TestValidate_SubjectRenderDefaultBranch_MessageContainsOperationContext
// verifies that the unrenderable-subject-declaration diagnostic produced by
// checkSubjectRender's default branch contains the operation context describing
// what was being attempted, so the author understands the failure without
// knowing the internal call-site name.
func TestValidate_SubjectRenderDefaultBranch_MessageContainsOperationContext(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderError("harness-unresolvable", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q, got: %v", "unrenderable-subject-declaration", report.Diagnostics)
	}
	// The operation context must communicate that a render was being attempted
	// for the subject declaration, not merely quote the raw tool error.
	// Both words must appear together; a message containing only one of them
	// would not adequately describe the operation context.
	if !strings.Contains(d.Message, "render") {
		t.Errorf("Message does not contain operation context word 'render'; got: %q", d.Message)
	}
	if !strings.Contains(d.Message, "subject") {
		t.Errorf("Message does not contain operation context word 'subject'; got: %q", d.Message)
	}
}

// TestValidate_SubjectRenderDefaultBranch_HintNonEmpty verifies that the
// unrenderable-subject-declaration diagnostic produced by checkSubjectRender's
// default branch carries a non-empty Hint. The Hint is the primary remediation
// signal for an author reading the diagnostic output.
func TestValidate_SubjectRenderDefaultBranch_HintNonEmpty(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderError("harness-unresolvable", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q, got: %v", "unrenderable-subject-declaration", report.Diagnostics)
	}
	if d.Hint == "" {
		t.Errorf("Hint must not be empty for unrenderable-subject-declaration; "+
			"the hint is the primary remediation signal for the author; got empty Hint")
	}
}

// TestValidate_SubjectRenderDefaultBranch_KnownReasonCode_HintReferencesRoot
// verifies that when the render fails with the known reason code
// "harness-unresolvable", the hint specifically references --mosaic-root so
// the author knows which configuration to change.
func TestValidate_SubjectRenderDefaultBranch_KnownReasonCode_HintReferencesRoot(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderError("harness-unresolvable", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q, got: %v", "unrenderable-subject-declaration", report.Diagnostics)
	}
	if !strings.Contains(d.Hint, "mosaic-root") {
		t.Errorf("Hint for 'harness-unresolvable' must reference --mosaic-root; got: %q", d.Hint)
	}
}

// TestValidate_SubjectRenderDefaultBranch_ToolMessageNotSoleContent verifies
// that the unrenderable-subject-declaration message contains additional context
// beyond the raw ToolMessage — the message must never equal the ToolMessage alone.
func TestValidate_SubjectRenderDefaultBranch_ToolMessageNotSoleContent(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderError("harness-unresolvable", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q, got: %v", "unrenderable-subject-declaration", report.Diagnostics)
	}
	// The ToolMessage produced by returningRenderError for this reason code.
	toolMsg := `fake tool message for reason "harness-unresolvable"`
	if d.Message == toolMsg {
		t.Errorf("Message must not be the sole ToolMessage content; got exactly %q", d.Message)
	}
}

// TestValidate_SubjectRenderDefaultBranch_FutureReasonCode_ProvenanceInMessage
// verifies that the forward-compatibility case (an unrecognized reason code
// from a newer tool version) still produces a structured diagnostic containing
// provenance. The message must never degrade to raw ToolMessage-only output
// even for codes this version does not model.
func TestValidate_SubjectRenderDefaultBranch_FutureReasonCode_ProvenanceInMessage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderError("some-future-reason-code-v99", ""),
		MosaicRootProvenance: provenanceFlagTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-subject-declaration")
	if !ok {
		t.Fatalf("forward-compatibility: expected diagnostic %q for unrecognized reason code, got: %v",
			"unrenderable-subject-declaration", report.Diagnostics)
	}
	if !strings.Contains(d.Message, provenanceFlagTier) {
		t.Errorf("forward-compatibility: Message does not contain provenance %q for unrecognized reason code; got: %q",
			provenanceFlagTier, d.Message)
	}
	if d.Hint == "" {
		t.Error("forward-compatibility: Hint must not be empty for an unrecognized reason code")
	}
}

// TestValidate_SubjectRenderDefaultBranch_AllProvenanceTiers verifies that
// provenance strings from all three resolution tiers appear verbatim in the
// diagnostic message. The provenance string flows as-is; no normalization or
// truncation must occur.
func TestValidate_SubjectRenderDefaultBranch_AllProvenanceTiers(t *testing.T) {
	cases := []struct {
		name       string
		provenance string
	}{
		{"flag-tier", provenanceFlagTier},
		{"env-tier", provenanceEnvTier},
		{"default-tier", provenanceDefaultTier},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{
				"suite.suite.yaml":   validSuiteWithOneTest,
				"subject.test.yaml":  definitionWithSubjectAgent,
				"subject.stubs.json": validRegistryForDeployTests,
			})

			_, report := preflight.Validate(preflight.Input{
				SuitePath:            filepath.Join(root, "suite.suite.yaml"),
				FixtureRoot:          filepath.Join(root, "fixtures"),
				HarnessID:            "claude-code",
				Deploy:               returningRenderError("harness-unresolvable", ""),
				MosaicRootProvenance: tc.provenance,
			})

			d, ok := findDiagnosticByCode(report, "unrenderable-subject-declaration")
			if !ok {
				t.Fatalf("expected diagnostic %q, got: %v", "unrenderable-subject-declaration", report.Diagnostics)
			}
			if !strings.Contains(d.Message, tc.provenance) {
				t.Errorf("provenance tier %q: Message does not contain provenance %q; got: %q",
					tc.name, tc.provenance, d.Message)
			}
		})
	}
}

// --- checkSubjectDeploy: provenance threading ---

// returningDeployError returns a fakeDeployer whose Deploy call returns a
// *agentdeploy.DeployError with the given tool message. This exercises
// checkSubjectDeploy's diagnostic path.
func returningDeployError(toolMessage string) *fakeDeployer {
	return &fakeDeployer{
		deployFn: func(_ context.Context, _ domain.DeployRequest) (domain.DeployResult, error) {
			return domain.DeployResult{}, &agentdeploy.DeployError{
				// Reason and Detail are empty per the current deploy subcommand
				// contract (see agentdeploy.DeployError: both fields are reserved
				// for future use and currently always empty).
				ToolMessage: toolMessage,
				ExitCode:    1,
			}
		},
	}
}

// TestValidate_SubjectDeploy_MessageContainsProvenance verifies that when
// checkSubjectDeploy fires, the resulting undeployable-subject-declaration
// diagnostic message contains the MosaicRootProvenance from Input.
func TestValidate_SubjectDeploy_MessageContainsProvenance(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		DeployScratchRoot:    t.TempDir(),
		Deploy:               returningDeployError("deployment failed: catalogue root not found"),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "undeployable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q when Deploy fails, got: %v",
			"undeployable-subject-declaration", report.Diagnostics)
	}
	if !strings.Contains(d.Message, provenanceDefaultTier) {
		t.Errorf("Message does not contain MosaicRootProvenance %q; got: %q",
			provenanceDefaultTier, d.Message)
	}
}

// TestValidate_SubjectDeploy_MessageContainsOperationContext verifies that
// the undeployable-subject-declaration diagnostic message contains operation
// context describing what was being attempted (a deploy, not a render).
func TestValidate_SubjectDeploy_MessageContainsOperationContext(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		DeployScratchRoot:    t.TempDir(),
		Deploy:               returningDeployError("deployment failed: catalogue root not found"),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "undeployable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q when Deploy fails, got: %v",
			"undeployable-subject-declaration", report.Diagnostics)
	}
	// The message must convey that a deploy was being attempted.
	if !strings.Contains(d.Message, "deploy") {
		t.Errorf("Message does not contain operation context ('deploy'); got: %q", d.Message)
	}
}

// TestValidate_SubjectDeploy_HintNonEmpty verifies that the
// undeployable-subject-declaration diagnostic carries a non-empty Hint.
// DeployError currently has empty Reason/Detail, so the helper uses the
// empty-reason fallback branch — but the fallback must still produce a
// non-empty hint directing the author toward tool configuration.
func TestValidate_SubjectDeploy_HintNonEmpty(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		DeployScratchRoot:    t.TempDir(),
		Deploy:               returningDeployError("deployment failed: catalogue root not found"),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "undeployable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q when Deploy fails, got: %v",
			"undeployable-subject-declaration", report.Diagnostics)
	}
	if d.Hint == "" {
		t.Errorf("Hint must not be empty for undeployable-subject-declaration; "+
			"the hint guides the author to the remediation action; got empty Hint")
	}
}

// TestValidate_SubjectDeploy_ToolMessageNotSoleContent verifies that the
// diagnostic message for a deploy failure contains additional context beyond
// the raw tool message, satisfying the FR-8 requirement.
func TestValidate_SubjectDeploy_ToolMessageNotSoleContent(t *testing.T) {
	const toolMsg = "deployment failed: catalogue root not found"
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		DeployScratchRoot:    t.TempDir(),
		Deploy:               returningDeployError(toolMsg),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "undeployable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q when Deploy fails, got: %v",
			"undeployable-subject-declaration", report.Diagnostics)
	}
	if d.Message == toolMsg {
		t.Errorf("Message must not be the sole ToolMessage content; got exactly %q", d.Message)
	}
}

// TestValidate_SubjectDeploy_FlagTierProvenance verifies that flag-tier
// provenance flows correctly to the deploy-path diagnostic, confirming the
// provenance value is not hardcoded to the default-tier form.
func TestValidate_SubjectDeploy_FlagTierProvenance(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		DeployScratchRoot:    t.TempDir(),
		Deploy:               returningDeployError("deployment failed"),
		MosaicRootProvenance: provenanceFlagTier,
	})

	d, ok := findDiagnosticByCode(report, "undeployable-subject-declaration")
	if !ok {
		t.Fatalf("expected diagnostic %q, got: %v", "undeployable-subject-declaration", report.Diagnostics)
	}
	if !strings.Contains(d.Message, provenanceFlagTier) {
		t.Errorf("Message does not contain flag-tier provenance %q; got: %q",
			provenanceFlagTier, d.Message)
	}
}

// --- stub-agent render loop: provenance threading ---

// returningRenderErrorForStubs returns a fakeDeployer that succeeds for
// subject renders (no SourcePath set) and returns a *agentdeploy.RenderError
// for stub renders (SourcePath set), with the given reason code. This exercises
// the stub-agent render loop diagnostic path in isolation.
func returningRenderErrorForStubs(reason, detail string) *fakeDeployer {
	return &fakeDeployer{
		renderFn: func(_ context.Context, req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
			if req.SourcePath != "" {
				return domain.RenderAgentResult{}, &agentdeploy.RenderError{
					Reason:      reason,
					Detail:      detail,
					ToolMessage: "fake stub render failure: " + reason,
					ExitCode:    1,
				}
			}
			// Subject render succeeds.
			return domain.RenderAgentResult{DestinationPath: "/fake/dest/orchestrator.md"}, nil
		},
	}
}

// definitionWithStubAgent is a test definition that declares a stub_agents
// entry whose source file will be supplied by writeTree. Used to exercise the
// stub-agent render loop in checkSubjectDeploy/preflight.
const definitionWithStubAgent = `
schema_version: 1
name: subject-test
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  infrastructure_agents: []
stub_registry: subject.stubs.json
stub_agents:
  - identity:
      tool: dispatch
      agent: codebase-research
    source: agents/codebase-research.md
`

// TestValidate_StubRenderLoop_MessageContainsProvenance verifies that when
// the stub-agent render loop produces an unrenderable-stub-definition diagnostic,
// the message contains the MosaicRootProvenance from Input.
func TestValidate_StubRenderLoop_MessageContainsProvenance(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":            validSuiteWithOneTest,
		"subject.test.yaml":           definitionWithStubAgent,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition — exists on disk",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderErrorForStubs("source-not-generic", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-stub-definition")
	if !ok {
		t.Fatalf("expected diagnostic %q when stub render fails, got: %v",
			"unrenderable-stub-definition", report.Diagnostics)
	}
	if !strings.Contains(d.Message, provenanceDefaultTier) {
		t.Errorf("Message does not contain MosaicRootProvenance %q; got: %q",
			provenanceDefaultTier, d.Message)
	}
}

// TestValidate_StubRenderLoop_MessageContainsOperationContext verifies that
// the unrenderable-stub-definition diagnostic message contains operation context
// distinguishing it from a subject-declaration failure. The operation context
// identifies this as a stub definition render, not a subject render.
func TestValidate_StubRenderLoop_MessageContainsOperationContext(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":            validSuiteWithOneTest,
		"subject.test.yaml":           definitionWithStubAgent,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition — exists on disk",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderErrorForStubs("source-not-generic", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-stub-definition")
	if !ok {
		t.Fatalf("expected diagnostic %q when stub render fails, got: %v",
			"unrenderable-stub-definition", report.Diagnostics)
	}
	// The message must distinguish this as a stub definition operation, not a
	// subject declaration operation.
	if !strings.Contains(d.Message, "stub") {
		t.Errorf("Message does not contain operation context ('stub'); got: %q", d.Message)
	}
}

// TestValidate_StubRenderLoop_HintNonEmpty verifies that the
// unrenderable-stub-definition diagnostic carries a non-empty Hint.
func TestValidate_StubRenderLoop_HintNonEmpty(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":            validSuiteWithOneTest,
		"subject.test.yaml":           definitionWithStubAgent,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition — exists on disk",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderErrorForStubs("source-not-generic", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-stub-definition")
	if !ok {
		t.Fatalf("expected diagnostic %q when stub render fails, got: %v",
			"unrenderable-stub-definition", report.Diagnostics)
	}
	if d.Hint == "" {
		t.Errorf("Hint must not be empty for unrenderable-stub-definition; got empty Hint")
	}
}

// TestValidate_StubRenderLoop_ToolMessageNotSoleContent verifies that the
// stub-definition diagnostic message contains additional context beyond the raw
// tool message, satisfying the FR-8 requirement.
func TestValidate_StubRenderLoop_ToolMessageNotSoleContent(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":            validSuiteWithOneTest,
		"subject.test.yaml":           definitionWithStubAgent,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition — exists on disk",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderErrorForStubs("source-not-generic", ""),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-stub-definition")
	if !ok {
		t.Fatalf("expected diagnostic %q when stub render fails, got: %v",
			"unrenderable-stub-definition", report.Diagnostics)
	}
	toolMsg := "fake stub render failure: source-not-generic"
	if d.Message == toolMsg {
		t.Errorf("Message must not be the sole ToolMessage content; got exactly %q", d.Message)
	}
}

// TestValidate_StubRenderLoop_FutureReasonCode_ProvenanceInMessage verifies
// the forward-compatibility case for the stub-agent render loop: an
// unrecognized reason code still produces an unrenderable-stub-definition
// diagnostic with provenance in the message and a non-empty hint.
func TestValidate_StubRenderLoop_FutureReasonCode_ProvenanceInMessage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":            validSuiteWithOneTest,
		"subject.test.yaml":           definitionWithStubAgent,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition — exists on disk",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderErrorForStubs("some-future-reason-v99", ""),
		MosaicRootProvenance: provenanceEnvTier,
	})

	d, ok := findDiagnosticByCode(report, "unrenderable-stub-definition")
	if !ok {
		t.Fatalf("forward-compatibility: expected diagnostic %q for unrecognized reason code, got: %v",
			"unrenderable-stub-definition", report.Diagnostics)
	}
	if !strings.Contains(d.Message, provenanceEnvTier) {
		t.Errorf("forward-compatibility: Message does not contain provenance %q; got: %q",
			provenanceEnvTier, d.Message)
	}
	if d.Hint == "" {
		t.Error("forward-compatibility: Hint must not be empty for unrecognized reason code in stub render loop")
	}
}

// --- dedicated early-return branches are unaffected ---

// TestValidate_AgentNotInCatalog_UnchangedByProvenanceThreading verifies that
// the agent-not-in-catalog early-return branch in checkSubjectRender continues
// to produce unknown-subject-agent, not unrenderable-subject-declaration, even
// when MosaicRootProvenance is set. The dedicated branches are NOT being changed
// by Stage 3.
func TestValidate_AgentNotInCatalog_UnchangedByProvenanceThreading(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderError("agent-not-in-catalog", "orchestrator"),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	if !hasDiagnosticCode(report, "unknown-subject-agent") {
		t.Errorf("agent-not-in-catalog branch must still produce unknown-subject-agent "+
			"even when MosaicRootProvenance is set; got: %v", report.Diagnostics)
	}
	// The dedicated branch must not be replaced by the fallback branch.
	if hasDiagnosticCode(report, "unrenderable-subject-declaration") {
		t.Error("agent-not-in-catalog must not produce unrenderable-subject-declaration; " +
			"the dedicated early-return branch must remain in place")
	}
}

// TestValidate_UnknownWorkflow_UnchangedByProvenanceThreading verifies that
// the unknown-workflow early-return branch in checkSubjectRender continues to
// produce unknown-workflow when MosaicRootProvenance is set.
func TestValidate_UnknownWorkflow_UnchangedByProvenanceThreading(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithBadWorkflows,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:            filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:          filepath.Join(root, "fixtures"),
		HarnessID:            "claude-code",
		Deploy:               returningRenderError("unknown-workflow", "nonexistent-workflow"),
		MosaicRootProvenance: provenanceDefaultTier,
	})

	if !hasDiagnosticCode(report, "unknown-workflow") {
		t.Errorf("unknown-workflow branch must still produce unknown-workflow "+
			"even when MosaicRootProvenance is set; got: %v", report.Diagnostics)
	}
	if hasDiagnosticCode(report, "unrenderable-subject-declaration") {
		t.Error("unknown-workflow must not produce unrenderable-subject-declaration; " +
			"the dedicated early-return branch must remain in place")
	}
}

// --- compile-time interface verification ---

// Ensure the authoring.Diagnostic Hint field is available. This is a compile-
// time guard: if authoring.Diagnostic loses the Hint field, this file fails to
// build before any tests run.
var _ = authoring.Diagnostic{}.Hint
