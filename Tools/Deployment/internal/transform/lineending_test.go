package transform_test

// lineending_test.go covers the line-ending-aware helper contract (Stage 10):
//
//   - DetectLineEnding: CRLF, LF, and fallback (no newline / empty / nil).
//   - ProtocolVersionComment (revised): marker line terminator matches the content argument's
//     own line ending.
//   - Full-pipeline CRLF assertion: a protocol region assembled from a CRLF block carries a
//     CRLF-terminated marker and contains no lone LF anywhere in the region.
//   - Full-pipeline LF assertion: a protocol region assembled from an LF block yields a
//     marker that is byte-identical to what the pre-change code produced, so the fix does
//     not regress the Linux path.

import (
	"bytes"
	"strings"
	"testing"

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
// ProtocolVersionComment (revised) — line-ending-aware contract
// ---------------------------------------------------------------------------

// TestProtocolVersionComment_LFContent_YieldsLFTerminatedMarker verifies that LF content
// causes the marker line to carry an LF terminator. This is also the LF-invariance check:
// the output is byte-identical to what the pre-change single-argument form produced.
func TestProtocolVersionComment_LFContent_YieldsLFTerminatedMarker(t *testing.T) {
	content := []byte("## Communication Protocol\n\nSome content.\n")
	got := transform.ProtocolVersionComment("1.10", content)
	want := "<!-- protocol-version: 1.10 -->\n"
	if got != want {
		t.Errorf("ProtocolVersionComment(LF content): want %q, got %q", want, got)
	}
}

// TestProtocolVersionComment_CRLFContent_YieldsCRLFTerminatedMarker verifies that CRLF
// content causes the marker line to carry a CRLF terminator. This is the core Stage 10
// fix: the deployed region on a CRLF checkout must not contain a lone LF.
func TestProtocolVersionComment_CRLFContent_YieldsCRLFTerminatedMarker(t *testing.T) {
	content := []byte("## Communication Protocol\r\n\r\nSome content.\r\n")
	got := transform.ProtocolVersionComment("1.10", content)
	want := "<!-- protocol-version: 1.10 -->\r\n"
	if got != want {
		t.Errorf("ProtocolVersionComment(CRLF content): want %q, got %q", want, got)
	}
}

// TestProtocolVersionComment_NilContent_YieldsLFTerminatedMarker verifies that nil content
// yields the LF form, consistent with the defined fallback of DetectLineEnding.
func TestProtocolVersionComment_NilContent_YieldsLFTerminatedMarker(t *testing.T) {
	got := transform.ProtocolVersionComment("1.9", nil)
	want := "<!-- protocol-version: 1.9 -->\n"
	if got != want {
		t.Errorf("ProtocolVersionComment(nil content): want %q (LF fallback), got %q", want, got)
	}
}

// TestProtocolVersionComment_LFInvariance_ByteIdenticalToPreChangeForm verifies that for LF
// content, ProtocolVersionComment produces output that is byte-identical to what the
// pre-change single-argument form produced. This is the regression guard for the Linux path
// and for all 15 golden fixtures that already hold the correct bytes.
func TestProtocolVersionComment_LFInvariance_ByteIdenticalToPreChangeForm(t *testing.T) {
	lfContent := []byte("## Protocol\n\nContent.\n")
	versions := []string{"1.9", "1.10", "2.0"}
	for _, version := range versions {
		t.Run("version="+version, func(t *testing.T) {
			got := transform.ProtocolVersionComment(version, lfContent)
			expected := "<!-- protocol-version: " + version + " -->\n"
			if got != expected {
				t.Errorf("ProtocolVersionComment(%q, LF content): want %q, got %q",
					version, expected, got)
			}
		})
	}
}

// TestProtocolVersionComment_ResultContainsExactlyOneTerminator verifies that the returned
// string ends in a line terminator and contains exactly one line terminator, at the end.
// This upholds the contract that the result is a complete single line with no embedded
// newlines and no lone '\r'.
func TestProtocolVersionComment_ResultContainsExactlyOneTerminator(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
		wantEnd string
	}{
		{"LF content", []byte("content\n"), "\n"},
		{"CRLF content", []byte("content\r\n"), "\r\n"},
		{"nil content", nil, "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := transform.ProtocolVersionComment("1.9", tc.content)

			// Must end with the expected terminator.
			if !strings.HasSuffix(got, tc.wantEnd) {
				t.Errorf("ProtocolVersionComment result does not end with %q; got %q",
					tc.wantEnd, got)
			}

			// Must contain exactly one '\n' (at the end).
			newlineCount := strings.Count(got, "\n")
			if newlineCount != 1 {
				t.Errorf("ProtocolVersionComment result contains %d '\\n' bytes; want exactly 1; got %q",
					newlineCount, got)
			}

			// Must not contain a lone '\r' (a '\r' not followed by '\n').
			for i, b := range []byte(got) {
				if b == '\r' && (i+1 >= len(got) || got[i+1] != '\n') {
					t.Errorf("ProtocolVersionComment result contains lone '\\r' at index %d; got %q",
						i, got)
				}
			}
		})
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

// crlfSubagentBlock is the CRLF-normalised form of subagentBlockContent, precomputed so
// tests can pass it as the content argument to ProtocolVersionComment when building the
// expected marker.
var crlfSubagentBlock = []byte(strings.ReplaceAll(subagentBlockContent, "\n", "\r\n"))

// ---------------------------------------------------------------------------
// T10.1 — Full-pipeline CRLF: no lone LF in the assembled region
// ---------------------------------------------------------------------------

// TestProtocol_CRLFBlock_RegionContainsNoLoneLF verifies that when the protocol block
// carries CRLF line endings, the assembled CommunicationProtocol region contains no lone
// LF: every '\n' in the region is immediately preceded by '\r'. This covers both the
// protocol-version marker line (which previously emitted a hardcoded lone LF) and the
// block content itself.
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

// TestProtocol_CRLFBlock_MarkerLineIsCRLFTerminated verifies that the protocol-version
// marker line is CRLF-terminated when the protocol block uses CRLF line endings.
// The marker must be the first content of the region and must end in "\r\n".
func TestProtocol_CRLFBlock_MarkerLineIsCRLFTerminated(t *testing.T) {
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

	regionContent := extractProtocolRegionContent(t, result.Output)
	// Hardcode the expected marker literal so this test fails in RED phase.
	// Deriving expectedMarker via transform.ProtocolVersionComment would produce the same
	// stub output as the production path (both return "\n"), masking the CRLF requirement.
	expectedMarker := "<!-- protocol-version: " + version + " -->\r\n"

	if !bytes.HasPrefix(regionContent, []byte(expectedMarker)) {
		t.Errorf("CRLF block: protocol-version marker is not CRLF-terminated or is not the first "+
			"content of the region;\nwant prefix: %q\ngot region (first 200 bytes): %q",
			expectedMarker, truncateBytes(regionContent, 200))
	}
}

// ---------------------------------------------------------------------------
// T10.2 — Full-pipeline LF: byte-identical to pre-change output
// ---------------------------------------------------------------------------

// TestProtocol_LFBlock_MarkerLineIsLFTerminated verifies that when the protocol block
// carries LF line endings, the protocol-version marker line is LF-terminated and
// byte-identical to what the pre-change code produced. This is the regression guard for
// the Linux path and for the 15 golden fixtures that already hold the correct bytes.
func TestProtocol_LFBlock_MarkerLineIsLFTerminated(t *testing.T) {
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

	regionContent := extractProtocolRegionContent(t, result.Output)

	// The marker line must be byte-identical to what the pre-change single-argument form
	// produced: "<!-- protocol-version: X -->\n".
	expectedMarker := "<!-- protocol-version: " + version + " -->\n"
	if !bytes.HasPrefix(regionContent, []byte(expectedMarker)) {
		t.Errorf("LF block: protocol-version marker is not LF-terminated or does not match "+
			"the pre-change form;\nwant prefix: %q\ngot region (first 200 bytes): %q",
			expectedMarker, truncateBytes(regionContent, 200))
	}
}

// TestProtocol_LFBlock_RegionContainsNoCR verifies that an LF protocol block does not
// introduce stray '\r' bytes anywhere in the assembled protocol region.
// The LF path must remain purely LF.
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
