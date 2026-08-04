package docformat_test

// Tests for ArtifactProvenance vocabulary additions (T1.2, T1.3), updated for Stage 2.
//
// Coverage (T1.2 — vocabulary constants, Stage 2 values):
//   - CanonicalSections contains 7 entries after Stage 2 removes CommunicationProtocol.
//   - CanonicalSections[1] is "ArtifactProvenance" (immediately after Identity).
//   - CanonicalSections lists all 7 entries in the precise canonical order.
//   - CanonicalInjections contains 8 entries (user-owned names only) after Stage 2.
//   - CanonicalInjections includes "ArtifactProvenanceExtension".
//   - InjectionParent maps "ArtifactProvenanceExtension" to "ArtifactProvenance".
//
// Coverage (T1.3 — validator behaviour with canonical vocabulary):
//   - A document with all canonical sections and CommunicationProtocol as a top-level
//     DEPLOYED boundary in correct order passes validation (no "out-of-order-section"
//     issue, no "unknown-injection" issue).
//   - A document with ArtifactProvenance placed after Capabilities produces an
//     "out-of-order-section" issue when RequireCanonicalSections is true.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// T1.2 — CanonicalSections
// ---------------------------------------------------------------------------

func TestCanonicalSections_ContainsSevenSections(t *testing.T) {
	// After Stage 2, CommunicationProtocol is removed from CanonicalSections and
	// becomes a tool-managed boundary name declared with [[DEPLOYED:]].
	got := docformat.CanonicalSections
	if len(got) != 7 {
		t.Fatalf("CanonicalSections length: want 7, got %d: %v", len(got), got)
	}
}

func TestCanonicalSections_ArtifactProvenance_IsAtIndexOne(t *testing.T) {
	// After Stage 2 removes CommunicationProtocol from CanonicalSections, ArtifactProvenance
	// shifts from index 2 to index 1 (directly after Identity).
	got := docformat.CanonicalSections
	if len(got) <= 1 {
		t.Fatalf("CanonicalSections too short to have index 1: %v", got)
	}
	if got[1] != "ArtifactProvenance" {
		t.Errorf("CanonicalSections[1]: want %q, got %q", "ArtifactProvenance", got[1])
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

func TestCanonicalSections_FullSevenEntryOrder(t *testing.T) {
	// This is the authoritative lockstep contract with boundary_constants.py after Stage 2.
	// CommunicationProtocol has been removed; the remaining seven sections keep their
	// relative order.
	want := []string{
		"Identity",
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

func TestCanonicalInjections_ContainsEightInjections(t *testing.T) {
	// After Stage 2, tool-managed names (HarnessConstraints, CustomConstraints, etc.)
	// and ProtocolExtension are removed from CanonicalInjections, leaving exactly the
	// 8 user-owned names.
	got := docformat.CanonicalInjections
	if len(got) != 8 {
		t.Fatalf("CanonicalInjections length: want 8, got %d: %v", len(got), got)
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
// T1.3 — Validator: canonical document with DEPLOYED:CommunicationProtocol passes
// ---------------------------------------------------------------------------

func TestValidate_CanonicalDocument_CorrectOrder_ReturnsNoOutOfOrderIssue(t *testing.T) {
	// A document with all canonical sections plus a top-level [[DEPLOYED:CommunicationProtocol]]
	// in the correct second position must not produce an "out-of-order-section" issue.
	//
	// Fixture: all-eight-sections.md layout (Stage 2):
	//   [[SECTION:Identity]]
	//   [[DEPLOYED:CommunicationProtocol]]   ← canonical slot 1 (top-level deployed)
	//   [[SECTION:ArtifactProvenance]]
	//   ... remaining sections in order ...
	doc := parsedBoundaryFixture(t, "all-eight-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
		AllowUnknownInjections:   false,
	})

	if hasIssueWithCode(issues, "out-of-order-section") {
		t.Errorf(
			"expected no out-of-order-section issue for canonical sections in correct order, got issues: %v",
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

func TestValidate_CanonicalDocument_CorrectOrder_ReturnsNoIssues(t *testing.T) {
	// The canonical document must produce zero issues under the strictest validation options.
	doc := parsedBoundaryFixture(t, "all-eight-sections.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
		RequireInjectionParents:  true,
		AllowUnknownInjections:   false,
	})

	if len(issues) != 0 {
		t.Errorf("expected no issues for a well-formed canonical document, got %d:", len(issues))
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
	// canonical section order (ArtifactProvenance belongs at index 2 in CanonicalOrder,
	// Capabilities at index 3). The validator must report an "out-of-order-section" issue.
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
