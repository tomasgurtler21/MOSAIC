package transform_test

// lineending_test.go covers the line-ending-aware helper contract (Stage 10):
//
//   - DetectLineEnding: CRLF, LF, and fallback (no newline / empty / nil).
//   - Full-pipeline CRLF assertion: a protocol region assembled from a CRLF block contains
//     no lone LF anywhere in the region content; the version is a tag attribute, not a
//     comment line inside the region.
//   - Full-pipeline LF assertion: a protocol region assembled from an LF block contains no
//     CR bytes; the version is a tag attribute and is NOT embedded as an inline comment.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// DetectLineEnding — three-case contract
// ---------------------------------------------------------------------------

// TestDetectLineEnding_CRLFContent_ReturnsCRLF verifies that content whose first newline
// is immediately preceded by CR causes DetectLineEnding to return "\r\n".
func TestDetectLineEnding_CRLFContent_ReturnsCRLF(t *testing.T) {
	input := []byte("line one\r\nline two\r\n")
	got := transform.DetectLineEnding(input)
	if got != "\r\n" {
		t.Errorf("DetectLineEnding(CRLF content): want %q, got %q", "\r\n", got)
	}
}

// TestDetectLineEnding_LFContent_ReturnsLF verifies that content whose first newline is not
// preceded by CR causes DetectLineEnding to return "\n".
func TestDetectLineEnding_LFContent_ReturnsLF(t *testing.T) {
	input := []byte("line one\nline two\n")
	got := transform.DetectLineEnding(input)
	if got != "\n" {
		t.Errorf("DetectLineEnding(LF content): want %q, got %q", "\n", got)
	}
}

// TestDetectLineEnding_FallbackCases_ReturnLF verifies that content with no '\n' byte —
// including empty, nil, and lone-CR content — causes DetectLineEnding to return "\n".
// The LF fallback is the defined answer for these inputs, not an error condition.
func TestDetectLineEnding_FallbackCases_ReturnLF(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
	}{
		{"no newline", []byte("no newline here")},
		{"empty slice", []byte{}},
		{"nil", nil},
		{"lone CR (not CRLF)", []byte("just\ra CR")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := transform.DetectLineEnding(tc.input)
			if got != "\n" {
				t.Errorf("DetectLineEnding(%q): want %q (LF fallback), got %q",
					tc.input, "\n", got)
			}
		})
	}
}

// TestDetectLineEnding_FirstTerminatorWins verifies the first-terminator-wins rule: only
// the first '\n' in the content determines the return value. Later newlines of a different
// kind do not override the first.
func TestDetectLineEnding_FirstTerminatorWins(t *testing.T) {
	// First newline is CRLF; later newlines are bare LF.
	crlfFirst := []byte("first\r\nsecond\nthird\n")
	if got := transform.DetectLineEnding(crlfFirst); got != "\r\n" {
		t.Errorf("DetectLineEnding(CRLF first then LF): want %q (first wins), got %q",
			"\r\n", got)
	}

	// First newline is bare LF; later newlines are CRLF.
	lfFirst := []byte("first\nsecond\r\nthird\r\n")
	if got := transform.DetectLineEnding(lfFirst); got != "\n" {
		t.Errorf("DetectLineEnding(LF first then CRLF): want %q (first wins), got %q",
			"\n", got)
	}
}

// TestDetectLineEnding_LFAtIndexZero_ReturnsLF verifies that a '\n' at the very first byte
// position is treated as a bare LF, not as the end of a CRLF pair.
func TestDetectLineEnding_LFAtIndexZero_ReturnsLF(t *testing.T) {
	input := []byte("\ncontent after initial newline\n")
	got := transform.DetectLineEnding(input)
	if got != "\n" {
		t.Errorf("DetectLineEnding(LF at index 0): want %q, got %q", "\n", got)
	}
}

// ---------------------------------------------------------------------------
// Version attribute — full-pipeline contract for LF and CRLF blocks
// ---------------------------------------------------------------------------

// TestProtocol_VersionAttribute_LFBlock_PresentOnTag verifies that when the protocol block
// has LF line endings, the version appears as a tag attribute on the CommunicationProtocol
// region's opening tag and is NOT embedded as an inline comment inside the region body.
func TestProtocol_VersionAttribute_LFBlock_PresentOnTag(t *testing.T) {
	const version = "1.9"
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol(version),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("<CommunicationProtocol type=\"managed\"> region absent from output")
	}

	if got := node.Version(); got != version {
		t.Errorf("CommunicationProtocol version attribute: want %q, got %q", version, got)
	}

	// Version must NOT appear as a legacy comment inside the region body.
	legacyComment := "<!-- protocol-version: " + version + " -->"
	if bytes.Contains(node.Content(), []byte(legacyComment)) {
		t.Errorf("region body must not contain legacy version comment %q; version belongs on the tag attribute", legacyComment)
	}
}

// TestProtocol_VersionAttribute_CRLFBlock_PresentOnTag verifies that when the protocol block
// has CRLF line endings, the version still appears correctly as a tag attribute (not in the
// region body). CRLF content does not change where the version is stored.
func TestProtocol_VersionAttribute_CRLFBlock_PresentOnTag(t *testing.T) {
	const version = "1.10"
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocolCRLF(version),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("<CommunicationProtocol type=\"managed\"> region absent from output")
	}

	if got := node.Version(); got != version {
		t.Errorf("CommunicationProtocol version attribute (CRLF block): want %q, got %q", version, got)
	}

	// Version must NOT appear as a legacy comment inside the region body.
	legacyComment := "<!-- protocol-version: " + version + " -->"
	if bytes.Contains(node.Content(), []byte(legacyComment)) {
		t.Errorf("region body must not contain legacy version comment %q; version belongs on the tag attribute", legacyComment)
	}
}

// ---------------------------------------------------------------------------
// crlfProtocol — shared CRLF fixture for full-pipeline tests
// ---------------------------------------------------------------------------

// fixtureProtocolCRLF returns a ProtocolContent whose blocks carry CRLF line endings,
// simulating the protocol blocks as they appear on a Windows checkout with
// core.autocrlf=true. The block strings are derived from the LF constants shared with
// the existing protocol tests, normalised to CRLF so assertions can compare against a
// known, deterministic byte sequence.
func fixtureProtocolCRLF(version string) domain.ProtocolContent {
	return domain.ProtocolContent{
		Version: version,
		Blocks: map[domain.ProtocolVariant][]byte{
			domain.ProtocolOrchestrator: []byte(strings.ReplaceAll(orchestratorBlockContent, "\n", "\r\n")),
			domain.ProtocolSubagent:     []byte(strings.ReplaceAll(subagentBlockContent, "\n", "\r\n")),
		},
	}
}

// ---------------------------------------------------------------------------
// T10.1 — Full-pipeline CRLF: no lone LF in the assembled region
// ---------------------------------------------------------------------------

// TestProtocol_CRLFBlock_RegionContainsNoLoneLF verifies that when the protocol block
// carries CRLF line endings, the assembled CommunicationProtocol region content contains
// no lone LF: every '\n' in the region content is immediately preceded by '\r'. Because
// the version is now a tag attribute (not an inline comment), this check is exclusively
// about the block content bytes — no version marker line can introduce a lone LF.
func TestProtocol_CRLFBlock_RegionContainsNoLoneLF(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocolCRLF("1.10"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	regionContent := extractProtocolRegionContent(t, result.Output)

	// Every '\n' in the region must be immediately preceded by '\r'. A '\n' at index 0
	// is also a violation (there is no preceding byte that could be '\r').
	for i, b := range regionContent {
		if b == '\n' {
			if i == 0 || regionContent[i-1] != '\r' {
				t.Errorf("lone LF at byte offset %d in CommunicationProtocol region;\n"+
					"a CRLF protocol block must produce a region with no lone LF, "+
					"including on the protocol-version marker line;\n"+
					"region (first 200 bytes): %q",
					i, truncateBytes(regionContent, 200))
				return
			}
		}
	}
}

// TestProtocol_CRLFBlock_RegionContentStartsWithBlock verifies that when the protocol block
// carries CRLF line endings, the assembled CommunicationProtocol region content begins with
// the CRLF block content directly — no legacy version comment prefixes the body. The
// version is stored as a tag attribute, not as the first line of the region body.
func TestProtocol_CRLFBlock_RegionContentStartsWithBlock(t *testing.T) {
	const version = "1.10"
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocolCRLF(version),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("<CommunicationProtocol type=\"managed\"> region absent from output")
	}

	// Version must be on the tag, not inside the body.
	if got := node.Version(); got != version {
		t.Errorf("CRLF block: version attribute want %q, got %q", version, got)
	}

	regionContent := node.Content()

	// No legacy version comment must appear in the region body.
	legacyMarker := "<!-- protocol-version: "
	if bytes.Contains(regionContent, []byte(legacyMarker)) {
		t.Errorf("CRLF block: region body must not contain a legacy version comment;\n"+
			"region content (first 200 bytes): %q", truncateBytes(regionContent, 200))
	}
}

// ---------------------------------------------------------------------------
// T10.2 — Full-pipeline LF: version on tag, no comment in body
// ---------------------------------------------------------------------------

// TestProtocol_LFBlock_VersionAttributePresentNot InBody verifies that when the protocol
// block carries LF line endings, the region's opening tag carries the version attribute
// and the region body does NOT begin with a legacy version comment. This is the regression
// guard for the LF path: version belongs on the tag, not as the first line of the body.
func TestProtocol_LFBlock_VersionAttributePresentNotInBody(t *testing.T) {
	const version = "1.9"
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol(version),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("<CommunicationProtocol type=\"managed\"> region absent from output")
	}

	if got := node.Version(); got != version {
		t.Errorf("LF block: version attribute want %q, got %q", version, got)
	}

	// No legacy version comment must appear in the region body.
	legacyMarker := "<!-- protocol-version: "
	if bytes.Contains(node.Content(), []byte(legacyMarker)) {
		t.Errorf("LF block: region body must not contain a legacy version comment;\n"+
			"region content (first 200 bytes): %q", truncateBytes(node.Content(), 200))
	}
}

// TestProtocol_LFBlock_RegionContainsNoCR verifies that an LF protocol block does not
// introduce stray '\r' bytes anywhere in the assembled protocol region content.
// The LF path must remain purely LF — no CR bytes from the formatting logic.
func TestProtocol_LFBlock_RegionContainsNoCR(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	regionContent := extractProtocolRegionContent(t, result.Output)
	if bytes.Contains(regionContent, []byte{'\r'}) {
		t.Errorf("LF block: protocol region contains unexpected '\\r' bytes;\n"+
			"the LF path must not introduce CR into an otherwise LF region;\n"+
			"region (first 200 bytes): %q", truncateBytes(regionContent, 200))
	}
}
