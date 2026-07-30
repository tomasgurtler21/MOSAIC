package docformat_test

// Tests for ArtifactProvenance vocabulary additions (T1.2, T1.3).
//
// Coverage (T1.2 — vocabulary constants):
//   - CanonicalSections contains 8 entries after adding ArtifactProvenance.
//   - CanonicalSections[2] is "ArtifactProvenance".
//   - CanonicalSections lists all 8 entries in the precise canonical order.
//   - CanonicalInjections contains 13 entries after adding ArtifactProvenanceExtension.
//   - CanonicalInjections includes "ArtifactProvenanceExtension".
//   - InjectionParent maps "ArtifactProvenanceExtension" to "ArtifactProvenance".
//   - ClassifyInjection("ArtifactProvenanceExtension") returns InjectionProject.
//
// Coverage (T1.3 — validator behaviour with 8-section vocabulary):
//   - A document with all 8 canonical sections in correct order passes validation
//     (no "out-of-order-section" issue, no "unknown-injection" issue).
//   - A document with ArtifactProvenance placed after Capabilities produces an
//     "out-of-order-section" issue when RequireCanonicalSections is true.

import (
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// ---------------------------------------------------------------------------
// T1.2 — CanonicalSections
// ---------------------------------------------------------------------------

func TestCanonicalSections_ContainsEightSections(t *testing.T) {
	// After the ArtifactProvenance addition the slice grows from 7 to 8.
	got := docformat.CanonicalSections
	if len(got) != 8 {
		t.Fatalf("CanonicalSections length: want 8, got %d: %v", len(got), got)
	}
}

func TestCanonicalSections_ArtifactProvenance_IsAtIndexTwo(t *testing.T) {
	got := docformat.CanonicalSections
	if len(got) <= 2 {
		t.Fatalf("CanonicalSections too short to have index 2: %v", got)
	}
	if got[2] != "ArtifactProvenance" {
		t.Errorf("CanonicalSections[2]: want %q, got %q", "ArtifactProvenance", got[2])
	}
}

func TestCanonicalSections_ArtifactProvenance_FollowsCommunicationProtocol(t *testing.T) {
	// ArtifactProvenance must appear after CommunicationProtocol in the slice.
	sections := docformat.CanonicalSections
	cpIdx := -1
	apIdx := -1
	for i, s := range sections {
		if s == "CommunicationProtocol" {
			cpIdx = i
		}
		if s == "ArtifactProvenance" {
			apIdx = i
		}
	}
	if cpIdx < 0 {
		t.Fatal("CommunicationProtocol not found in CanonicalSections")
	}
	if apIdx < 0 {
		t.Fatal("ArtifactProvenance not found in CanonicalSections")
	}
	if cpIdx >= apIdx {
		t.Errorf(
			"ArtifactProvenance (index %d) must appear after CommunicationProtocol (index %d)",
			apIdx, cpIdx,
		)
	}
}

func TestCanonicalSections_ArtifactProvenance_PrecedesCapabilities(t *testing.T) {
	// ArtifactProvenance must appear before Capabilities in the slice.
	sections := docformat.CanonicalSections
	apIdx := -1
	capIdx := -1
	for i, s := range sections {
		if s == "ArtifactProvenance" {
			apIdx = i
		}
		if s == "Capabilities" {
			capIdx = i
		}
	}
	if apIdx < 0 {
		t.Fatal("ArtifactProvenance not found in CanonicalSections")
	}
	if capIdx < 0 {
		t.Fatal("Capabilities not found in CanonicalSections")
	}
	if apIdx >= capIdx {
		t.Errorf(
			"ArtifactProvenance (index %d) must appear before Capabilities (index %d)",
			apIdx, capIdx,
		)
	}
}

func TestCanonicalSections_FullEightEntryOrder(t *testing.T) {
	// This is the authoritative lockstep contract with boundary_constants.py.
	want := []string{
		"Identity",
		"CommunicationProtocol",
		"ArtifactProvenance",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}

	got := docformat.CanonicalSections

	if len(got) != len(want) {
		t.Fatalf("CanonicalSections length: want %d, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("CanonicalSections[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

// ---------------------------------------------------------------------------
// T1.2 — CanonicalInjections
// ---------------------------------------------------------------------------

func TestCanonicalInjections_ContainsThirteenInjections(t *testing.T) {
	// After adding ArtifactProvenanceExtension the total rises from 12 to 13.
	got := docformat.CanonicalInjections
	if len(got) != 13 {
		t.Fatalf("CanonicalInjections length: want 13, got %d: %v", len(got), got)
	}
}

func TestCanonicalInjections_IncludesArtifactProvenanceExtension(t *testing.T) {
	found := false
	for _, inj := range docformat.CanonicalInjections {
		if inj == "ArtifactProvenanceExtension" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf(
			"CanonicalInjections must include \"ArtifactProvenanceExtension\"; got: %v",
			docformat.CanonicalInjections,
		)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — InjectionParent
// ---------------------------------------------------------------------------

func TestInjectionParent_ArtifactProvenanceExtension_MapsToArtifactProvenance(t *testing.T) {
	got := docformat.InjectionParent

	if got == nil {
		t.Fatal("InjectionParent is nil — must be populated before first use")
	}

	parent, ok := got["ArtifactProvenanceExtension"]
	if !ok {
		t.Fatal("InjectionParent missing entry for \"ArtifactProvenanceExtension\"")
	}
	if parent != "ArtifactProvenance" {
		t.Errorf(
			"InjectionParent[\"ArtifactProvenanceExtension\"]: want %q, got %q",
			"ArtifactProvenance", parent,
		)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — ClassifyInjection
// ---------------------------------------------------------------------------

func TestClassifyInjection_ArtifactProvenanceExtension_IsProject(t *testing.T) {
	// ArtifactProvenanceExtension has no special harness role; it is a
	// project-level injection (default case in ClassifyInjection).
	got := docformat.ClassifyInjection("ArtifactProvenanceExtension")
	if got != domain.InjectionProject {
		t.Errorf(
			"ClassifyInjection(\"ArtifactProvenanceExtension\"): want InjectionProject, got %q",
			got,
		)
	}
}

// ---------------------------------------------------------------------------
// T1.3 — Validator: 8 canonical sections in correct order passes
// ---------------------------------------------------------------------------

func TestValidate_EightCanonicalSections_CorrectOrder_ReturnsNoOutOfOrderIssue(t *testing.T) {
	// A document with all eight canonical sections in the correct order (ArtifactProvenance
	// at position 2, between CommunicationProtocol and Capabilities) must not produce an
	// "out-of-order-section" issue.
	//
	// RED note: before vocabulary.go is updated, ArtifactProvenanceExtension is not a
	// canonical injection, so the "unknown-injection" check fires — the test fails.
	// After implementation both the order and injection checks are satisfied.
	doc := parsedBoundaryFixture(t, "all-eight-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
		AllowUnknownInjections:   false,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"expected no out-of-order-section issue for 8 canonical sections in correct order, got issues: %v",
			issues,
		)
	}
	if hasIssueWithCode(issues, "unknown-injection") {
		t.Errorf(
			"expected no unknown-injection issue when ArtifactProvenanceExtension is canonical, got issues: %v",
			issues,
		)
	}
}

func TestValidate_EightCanonicalSections_CorrectOrder_ReturnsNoIssues(t *testing.T) {
	// The fully canonical 8-section document must produce zero issues under the
	// strictest validation options.
	doc := parsedBoundaryFixture(t, "all-eight-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
		AllowUnknownInjections:   false,
	})

	if len(issues) != 0 {
		t.Errorf("expected no issues for a well-formed 8-section document, got %d:", len(issues))
		for _, iss := range issues {
			t.Logf("  [%s] %s (line %d)", iss.Code, iss.Message, iss.Line)
		}
	}
}

// ---------------------------------------------------------------------------
// T1.3 — Validator: ArtifactProvenance after Capabilities is out of order
// ---------------------------------------------------------------------------

func TestValidate_ArtifactProvenance_AfterCapabilities_ReportsOutOfOrderIssue(t *testing.T) {
	// A document where ArtifactProvenance appears after Capabilities violates the
	// canonical section order (ArtifactProvenance belongs at index 2, Capabilities
	// at index 3). The validator must report an "out-of-order-section" issue.
	//
	// RED note: before vocabulary.go is updated, ArtifactProvenance is not a canonical
	// section so canonicalSectionIndex returns -1 and the entry is skipped — no issue
	// is produced and the test fails. After implementation the index comparison fires.
	doc := parsedBoundaryFixture(t, "malformed/artifact-provenance-out-of-order.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	if !hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"expected an \"out-of-order-section\" issue when ArtifactProvenance appears after Capabilities, got issues: %v",
			issues,
		)
	}
}

func TestValidate_ArtifactProvenance_AfterCapabilities_NotReportedWhenOptionDisabled(t *testing.T) {
	// When RequireCanonicalSections is false the ordering check is disabled entirely;
	// no "out-of-order-section" issue must be produced regardless of section order.
	doc := parsedBoundaryFixture(t, "malformed/artifact-provenance-out-of-order.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: false,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Error("out-of-order-section issue reported even though RequireCanonicalSections is false")
	}
}
