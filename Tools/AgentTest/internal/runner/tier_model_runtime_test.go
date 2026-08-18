package runner_test

// Tests for runtime model selection reaching the tier-model map (Stage 10).
//
// Stage 10 makes the subject and stub models runtime-supplied values: a
// frontend sets them on preflight.ResolvedTest.Models and the runner uses
// them to build the tier-model map sent on the Deploy call, taking precedence
// over the authoring-layer fields on SubjectUnderTest.
//
// Coverage (T10.1):
//   - A runtime subject model (ResolvedTest.Models.Subject) reaches the
//     subject tier of the Deploy call's TierModels, taking precedence over
//     the definition's Subject.Model.
//   - A runtime stub model (ResolvedTest.Models.Stub) reaches the stub tier
//     of the Deploy call's TierModels, taking precedence over the
//     definition's Subject.StubModel.
//
// Coverage (T10.2):
//   - A runtime subject model with no runtime stub model puts the subject
//     model on both tiers (because the stub tier must fall back to the
//     resolved subject-tier value, not to the authored Subject.StubModel).
//   - Supplying neither runtime model leaves today's behaviour intact: the
//     subject tier comes from Subject.Model and the stub tier from
//     Subject.StubModel, with the same Subject.Model fallback when
//     StubModel is empty.
//
// All new assertions in this file are in the TDD RED phase: they compile
// against the current implementation and fail because buildTierModelMap does
// not yet consult ResolvedTest.Models.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

// catalogueRequestWithModels builds a catalogue-path request with explicit
// authored model fields and runtime model selection, so each test controls
// both sources independently.
func catalogueRequestWithModels(testID, authoredSubjectModel, authoredStubModel string, sel domain.ModelSelection) runner.Request {
	req := catalogueRequest(testID)
	req.Test.Definition.Subject.Model = authoredSubjectModel
	req.Test.Definition.Subject.StubModel = authoredStubModel
	req.Test.Models = sel
	return req
}

// --- T10.1: runtime values take precedence ---

// TestSetup_RuntimeSubjectModelTakesPrecedenceOverAuthoredModel asserts that
// when ResolvedTest.Models.Subject is non-empty it is used as the subject
// tier's model, regardless of what Subject.Model carries. The runtime value
// is the higher-priority source: a frontend-supplied model must win over an
// authored default.
func TestSetup_RuntimeSubjectModelTakesPrecedenceOverAuthoredModel(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequestWithModels(
		"runtime-subject-model-precedence",
		"authored-subject-model",  // Subject.Model — must be overridden
		"",                        // Subject.StubModel — absent
		domain.ModelSelection{Subject: "runtime-subject-model"},
	)

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for both tiers")
	}

	got, ok := tm[domain.TierTestSubject]
	if !ok {
		t.Fatalf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestSubject)
	}
	if got != "runtime-subject-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"a runtime-supplied subject model must take precedence over the authored Subject.Model — "+
			"the frontier selects the model for this run and the definition must not override that choice",
			domain.TierTestSubject, got, "runtime-subject-model")
	}
}

// TestSetup_RuntimeStubModelTakesPrecedenceOverAuthoredStubModel asserts that
// when ResolvedTest.Models.Stub is non-empty it is used as the stub tier's
// model, taking precedence over Subject.StubModel. A runtime selection from
// the frontend must not be silently discarded in favour of an authored field.
func TestSetup_RuntimeStubModelTakesPrecedenceOverAuthoredStubModel(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequestWithModels(
		"runtime-stub-model-precedence",
		"authored-subject-model",
		"authored-stub-model",   // Subject.StubModel — must be overridden
		domain.ModelSelection{
			Subject: "runtime-subject-model",
			Stub:    "runtime-stub-model",
		},
	)

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for both tiers")
	}

	got, ok := tm[domain.TierTestStub]
	if !ok {
		t.Fatalf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestStub)
	}
	if got != "runtime-stub-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"a runtime-supplied stub model must take precedence over the authored Subject.StubModel — "+
			"the frontend selects the model for this run and the definition must not override it",
			domain.TierTestStub, got, "runtime-stub-model")
	}
}

// TestSetup_BothRuntimeModelsReachTheirTiers asserts that when both
// ResolvedTest.Models.Subject and ResolvedTest.Models.Stub are non-empty,
// each reaches its respective tier and neither is applied to the wrong tier.
func TestSetup_BothRuntimeModelsReachTheirTiers(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequestWithModels(
		"both-runtime-models",
		"authored-subject-model",
		"authored-stub-model",
		domain.ModelSelection{
			Subject: "runtime-subject-model",
			Stub:    "runtime-stub-model",
		},
	)

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for both tiers")
	}

	subjectGot, subjectOK := tm[domain.TierTestSubject]
	if !subjectOK {
		t.Errorf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestSubject)
	} else if subjectGot != "runtime-subject-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"subject tier must carry the runtime subject model",
			domain.TierTestSubject, subjectGot, "runtime-subject-model")
	}

	stubGot, stubOK := tm[domain.TierTestStub]
	if !stubOK {
		t.Errorf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestStub)
	} else if stubGot != "runtime-stub-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"stub tier must carry the runtime stub model",
			domain.TierTestStub, stubGot, "runtime-stub-model")
	}
}

// --- T10.2: fallback and empty cases ---

// TestSetup_RuntimeSubjectModelWithNoRuntimeStubModel_StubFallsBackToSubjectModel
// asserts that when Models.Subject is set but Models.Stub is empty, and
// Subject.StubModel is also empty, the stub tier falls back to the resolved
// subject-tier value — which is Models.Subject, not Subject.Model. This is
// the deliberate "working but expensive" fallback: every stub agent runs on
// the same model as the subject rather than with an unresolved placeholder.
func TestSetup_RuntimeSubjectModelWithNoRuntimeStubModel_StubFallsBackToSubjectModel(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequestWithModels(
		"runtime-subject-stub-fallback",
		"authored-subject-model", // subject tier should come from runtime, not this
		"",                       // Subject.StubModel empty → must fall back to resolved subject tier
		domain.ModelSelection{Subject: "runtime-subject-model"},
	)

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for both tiers")
	}

	stubGot, stubOK := tm[domain.TierTestStub]
	if !stubOK {
		t.Errorf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestStub)
	} else if stubGot != "runtime-subject-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"when Models.Stub is empty, the stub tier must fall back to the resolved subject-tier "+
			"value (Models.Subject = %q), not to Subject.Model (%q); "+
			"this ensures the run is working-but-expensive, not broken",
			domain.TierTestStub, stubGot, "runtime-subject-model",
			"runtime-subject-model", "authored-subject-model")
	}
}

// TestSetup_RuntimeSubjectModelWithNoRuntimeStubModel_AuthoredStubModelWins
// asserts the intermediate level of the three-level stub-tier fallback chain:
// sel.Stub → subject.StubModel → resolved-subject-tier.
//
// When sel.Subject is non-empty (changing the subject tier) and sel.Stub is
// empty, the stub tier must use subject.StubModel rather than falling all the
// way through to the resolved subject-tier value. The authored stub model is
// the middle level of the chain and must not be skipped just because a runtime
// subject model is present.
func TestSetup_RuntimeSubjectModelWithNoRuntimeStubModel_AuthoredStubModelWins(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequestWithModels(
		"runtime-subject-authored-stub-wins",
		"authored-subject-model",  // Subject.Model — overridden by runtime subject model
		"authored-stub-model",     // Subject.StubModel — must win; it is the middle fallback level
		domain.ModelSelection{Subject: "runtime-subject-model"}, // sel.Stub is deliberately absent
	)

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for both tiers")
	}

	stubGot, stubOK := tm[domain.TierTestStub]
	if !stubOK {
		t.Errorf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestStub)
	} else if stubGot != "authored-stub-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"the stub-tier fallback chain is sel.Stub → subject.StubModel → resolved-subject-tier; "+
			"when sel.Stub is empty the middle level (subject.StubModel=%q) must be used, "+
			"not the resolved subject-tier value (sel.Subject=%q); "+
			"an implementation that skips subject.StubModel whenever a runtime subject model is present "+
			"is incorrect and this test guards against that regression",
			domain.TierTestStub, stubGot, "authored-stub-model",
			"authored-stub-model", "runtime-subject-model")
	}
}

// TestSetup_NoRuntimeModels_SubjectTierUsesAuthoredSubjectModel asserts that
// when both Models.Subject and Models.Stub are empty, the subject tier falls
// through to Subject.Model — today's behaviour, preserved intact. An absent
// runtime model must not alter a run that was already working.
func TestSetup_NoRuntimeModels_SubjectTierUsesAuthoredSubjectModel(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequestWithModels(
		"no-runtime-subject-tier",
		"authored-subject-model",
		"authored-stub-model",
		domain.ModelSelection{}, // neither runtime model supplied
	)

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for both tiers")
	}

	subjectGot, subjectOK := tm[domain.TierTestSubject]
	if !subjectOK {
		t.Errorf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestSubject)
	} else if subjectGot != "authored-subject-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"when no runtime subject model is supplied, the subject tier must fall through to "+
			"Subject.Model — today's behaviour must be preserved intact",
			domain.TierTestSubject, subjectGot, "authored-subject-model")
	}
}

// TestSetup_NoRuntimeModels_StubTierUsesAuthoredStubModel asserts that when
// both runtime models are absent, the stub tier uses Subject.StubModel —
// today's behaviour. The existing authored-stub-model fallback must survive
// the Stage 10 change.
func TestSetup_NoRuntimeModels_StubTierUsesAuthoredStubModel(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequestWithModels(
		"no-runtime-stub-tier",
		"authored-subject-model",
		"authored-stub-model",
		domain.ModelSelection{}, // neither runtime model supplied
	)

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for both tiers")
	}

	stubGot, stubOK := tm[domain.TierTestStub]
	if !stubOK {
		t.Errorf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestStub)
	} else if stubGot != "authored-stub-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"when no runtime stub model is supplied, the stub tier must fall through to "+
			"Subject.StubModel — today's authored-stub-model behaviour must be preserved",
			domain.TierTestStub, stubGot, "authored-stub-model")
	}
}

// TestSetup_NoRuntimeModels_StubFallsBackToSubjectModelWhenStubModelAbsent
// asserts that when both runtime models are empty and Subject.StubModel is
// also empty, the stub tier falls back to Subject.Model. This is the existing
// "absent StubModel yields working run" guarantee and must not regress.
func TestSetup_NoRuntimeModels_StubFallsBackToSubjectModelWhenStubModelAbsent(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequestWithModels(
		"no-runtime-stub-subject-fallback",
		"authored-subject-model",
		"", // Subject.StubModel empty — must fall back to Subject.Model
		domain.ModelSelection{}, // neither runtime model supplied
	)

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for both tiers")
	}

	stubGot, stubOK := tm[domain.TierTestStub]
	if !stubOK {
		t.Errorf("Deploy TierModels missing key %q; want both tiers always present", domain.TierTestStub)
	} else if stubGot != "authored-subject-model" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"when neither runtime model nor Subject.StubModel is set, the stub tier must fall back "+
			"to Subject.Model so the run degrades to expensive-but-working rather than broken",
			domain.TierTestStub, stubGot, "authored-subject-model")
	}
}
