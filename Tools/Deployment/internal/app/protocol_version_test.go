package app

// protocol_version_test.go covers reading the deployed protocol version from a deployed artifact.
//
//   extractDeployedProtocolVersion — scoped extraction of the <!-- protocol-version: X -->
//   comment from inside the [[DEPLOYED:CommunicationProtocol]] region:
//
//     - A file carrying the region with a version comment returns the version string.
//     - A file carrying the region but no version comment returns "".
//     - A file with no protocol region returns "".
//     - A malformed file degrades gracefully — returns "", never fails.
//     - A version comment appearing outside the region is not captured.
//     - Extra whitespace around the version token is handled by the regex.
//
//   probeDeployedArtifact integration:
//
//     - The ProtocolVersion field of the returned state is populated from the
//       extractDeployedProtocolVersion helper when a version comment is present.
//     - An absent protocol region yields ProtocolVersion == "".
//     - A malformed file yields ProtocolVersion == "".

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// extractDeployedProtocolVersion
// ---------------------------------------------------------------------------

// TestExtractDeployedProtocolVersion_RegionWithVersionComment_ReturnsVersion verifies that
// when the deployed file contains a [[DEPLOYED:CommunicationProtocol]] region whose first
// content line is a <!-- protocol-version: X --> comment, the function returns "X" verbatim.
func TestExtractDeployedProtocolVersion_RegionWithVersionComment_ReturnsVersion(t *testing.T) {
	data := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[DEPLOYED:CommunicationProtocol]]\n" +
			"<!-- protocol-version: 1.9 -->\n" +
			"## Communication Protocol\n\n" +
			"Some protocol content.\n\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")

	got := extractDeployedProtocolVersion(data)

	if got != "1.9" {
		t.Errorf("extractDeployedProtocolVersion with region and version comment: got %q, want %q",
			got, "1.9")
	}
}

// TestExtractDeployedProtocolVersion_RegionAbsent_ReturnsEmpty verifies that when the deployed
// file carries no [[DEPLOYED:CommunicationProtocol]] region, the function returns "". An agent
// deployed before this feature was added carries no such region.
func TestExtractDeployedProtocolVersion_RegionAbsent_ReturnsEmpty(t *testing.T) {
	// A typical agent file with frontmatter and a workflow section but no deployed protocol region.
	data := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[SECTION:Identity]]\n" +
			"Some identity content.\n" +
			"[[/SECTION:Identity]]\n")

	got := extractDeployedProtocolVersion(data)

	if got != "" {
		t.Errorf("extractDeployedProtocolVersion with absent protocol region: got %q, want empty string",
			got)
	}
}

// TestExtractDeployedProtocolVersion_RegionPresentButNoVersionComment_ReturnsEmpty verifies that
// when the [[DEPLOYED:CommunicationProtocol]] region exists but contains no
// <!-- protocol-version: X --> comment, the function returns "". The region being present does
// not guarantee a version comment — a legacy deployment may have been written without one.
func TestExtractDeployedProtocolVersion_RegionPresentButNoVersionComment_ReturnsEmpty(t *testing.T) {
	data := []byte(
		"[[DEPLOYED:CommunicationProtocol]]\n" +
			"## Communication Protocol\n\n" +
			"Content with no version comment inside the region.\n\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")

	got := extractDeployedProtocolVersion(data)

	if got != "" {
		t.Errorf("extractDeployedProtocolVersion with region but no version comment: got %q, want empty string",
			got)
	}
}

// TestExtractDeployedProtocolVersion_MalformedFile_ReturnsEmpty verifies that when the file
// content is malformed (unclosed region marker), the function returns "" and does not panic.
// Like the workflow extractor, this function is used during probing and must never fail the
// scan. The contract says malformed documents degrade to empty — partial version data from
// an unclosed region must not be returned.
func TestExtractDeployedProtocolVersion_MalformedFile_ReturnsEmpty(t *testing.T) {
	// Unclosed protocol region — malformed document.
	data := []byte(
		"[[DEPLOYED:CommunicationProtocol]]\n" +
			"<!-- protocol-version: 1.9 -->\n" +
			"Content that is never closed.\n")

	got := extractDeployedProtocolVersion(data)

	if got != "" {
		t.Errorf("extractDeployedProtocolVersion with malformed (unclosed) region: got %q, want empty string; "+
			"partial version data from an unclosed region must not be returned",
			got)
	}
}

// TestExtractDeployedProtocolVersion_EmptyContent_ReturnsEmpty verifies that passing nil or
// empty byte content returns "" without panicking. An absent or unreadable file will yield
// empty content before the extractor is reached.
func TestExtractDeployedProtocolVersion_EmptyContent_ReturnsEmpty(t *testing.T) {
	got := extractDeployedProtocolVersion(nil)
	if got != "" {
		t.Errorf("extractDeployedProtocolVersion with nil content: got %q, want empty string", got)
	}

	got = extractDeployedProtocolVersion([]byte{})
	if got != "" {
		t.Errorf("extractDeployedProtocolVersion with empty content: got %q, want empty string", got)
	}
}

// TestExtractDeployedProtocolVersion_VersionCommentOutsideRegion_NotCaptured verifies that a
// <!-- protocol-version: X --> comment appearing outside the [[DEPLOYED:CommunicationProtocol]]
// region — for example in the document preamble or in another section — is not captured.
// Extraction must be scoped to the region's own bytes to avoid false version reads.
func TestExtractDeployedProtocolVersion_VersionCommentOutsideRegion_NotCaptured(t *testing.T) {
	// The protocol-version comment appears BEFORE the region opens. It must not be returned.
	data := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"<!-- protocol-version: 9.9 -->\n" + // outside the region — must be ignored
			"[[DEPLOYED:CommunicationProtocol]]\n" +
			"## Communication Protocol\n\n" +
			"No version comment inside this region.\n\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")

	got := extractDeployedProtocolVersion(data)

	if got == "9.9" {
		t.Errorf("extractDeployedProtocolVersion captured a version comment outside the protocol region: "+
			"got %q; extraction must be scoped to the region bytes only", got)
	}
}

// TestExtractDeployedProtocolVersion_VersionCommentWithExtraWhitespace_ReturnsTrimmedVersion
// verifies that the regex correctly handles extra whitespace around the version token in the
// comment body, consistent with the workflowVersionPattern behaviour.
func TestExtractDeployedProtocolVersion_VersionCommentWithExtraWhitespace_ReturnsTrimmedVersion(t *testing.T) {
	data := []byte(
		"[[DEPLOYED:CommunicationProtocol]]\n" +
			"<!--  protocol-version:  2.1  -->\n" + // extra whitespace inside the comment
			"Protocol content.\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")

	got := extractDeployedProtocolVersion(data)

	if got != "2.1" {
		t.Errorf("extractDeployedProtocolVersion with whitespace-padded comment: got %q, want %q; "+
			"whitespace around the version token must not be included in the returned string",
			got, "2.1")
	}
}

// TestExtractDeployedProtocolVersion_MultipleVersionComments_FirstInRegionCaptured verifies that
// when the protocol region contains more than one version comment (which should not happen in
// normal operation), the function returns the first one found, consistent with how other
// extractor functions handle duplicate entries.
func TestExtractDeployedProtocolVersion_MultipleVersionComments_FirstInRegionCaptured(t *testing.T) {
	data := []byte(
		"[[DEPLOYED:CommunicationProtocol]]\n" +
			"<!-- protocol-version: 1.7 -->\n" +
			"Some content.\n" +
			"<!-- protocol-version: 1.9 -->\n" + // second comment — must be ignored
			"More content.\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")

	got := extractDeployedProtocolVersion(data)

	if got != "1.7" {
		t.Errorf("extractDeployedProtocolVersion with two version comments in region: got %q, want %q (first comment)",
			got, "1.7")
	}
}

// ---------------------------------------------------------------------------
// AC10.7 — Reader-side CRLF tolerance for extractDeployedProtocolVersion
// ---------------------------------------------------------------------------

// TestExtractDeployedProtocolVersion_CRLFTerminatedMarker_ReturnsVersionWithoutCR verifies
// that when the [[DEPLOYED:CommunicationProtocol]] region contains a protocol-version marker
// line terminated with CRLF ("\r\n") — as produced by the Stage 10 fix on a Windows checkout —
// the extracted version string does not include the carriage return. The regex captures only
// the version token, which is terminator-independent by design.
func TestExtractDeployedProtocolVersion_CRLFTerminatedMarker_ReturnsVersionWithoutCR(t *testing.T) {
	// Marker line is CRLF-terminated, as it would appear in a CRLF-checked-out file on Windows.
	data := []byte(
		"[[DEPLOYED:CommunicationProtocol]]\r\n" +
			"<!-- protocol-version: 1.10 -->\r\n" +
			"## Communication Protocol\r\n\r\n" +
			"Some protocol content.\r\n\r\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\r\n")

	got := extractDeployedProtocolVersion(data)

	if got != "1.10" {
		t.Errorf("extractDeployedProtocolVersion with CRLF-terminated marker: got %q, want %q; "+
			"the extracted version must not include a trailing \\r",
			got, "1.10")
	}
	if strings.Contains(got, "\r") {
		t.Errorf("extractDeployedProtocolVersion with CRLF-terminated marker: returned version %q contains "+
			"a carriage return; reader must strip or not capture \\r",
			got)
	}
}

// TestExtractDeployedProtocolVersion_CRLFAndLFMarkerYieldEqualVersions verifies that a
// document whose protocol-version marker is CRLF-terminated and a document whose marker is
// LF-terminated both yield the same extracted version string, and that the extracted version
// carries no embedded "\r". This confirms terminator-independence of the reader end-to-end:
// a document whose only change is the marker terminator must not be reported as stale.
//
// This also covers the staleness comparison path: since ProtocolStaleness compares the
// extracted version string against the source version, equal extraction from CRLF and LF
// markers guarantees that neither form is incorrectly reported as stale when the source
// version matches.
func TestExtractDeployedProtocolVersion_CRLFAndLFMarkerYieldEqualVersions(t *testing.T) {
	const version = "1.10"

	crlfDoc := []byte(
		"[[DEPLOYED:CommunicationProtocol]]\r\n" +
			"<!-- protocol-version: " + version + " -->\r\n" +
			"## Communication Protocol\r\n\r\n" +
			"Protocol content.\r\n\r\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\r\n")

	lfDoc := []byte(
		"[[DEPLOYED:CommunicationProtocol]]\n" +
			"<!-- protocol-version: " + version + " -->\n" +
			"## Communication Protocol\n\n" +
			"Protocol content.\n\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")

	crlfExtracted := extractDeployedProtocolVersion(crlfDoc)
	lfExtracted := extractDeployedProtocolVersion(lfDoc)

	// Both must return the same version string.
	if crlfExtracted != lfExtracted {
		t.Errorf("extractDeployedProtocolVersion: CRLF marker returned %q, LF marker returned %q; "+
			"both must return the same version — a document whose only change is the marker terminator "+
			"must not be treated as carrying a different version",
			crlfExtracted, lfExtracted)
	}

	// Neither must contain a carriage return.
	if strings.Contains(crlfExtracted, "\r") {
		t.Errorf("extractDeployedProtocolVersion with CRLF marker: extracted version %q contains \\r; "+
			"the version token must never include the line terminator",
			crlfExtracted)
	}

	// Staleness comparison: a document with CRLF marker must not be stale against the same source
	// version, just as an LF marker document would not be stale.
	crlfState := domain.DeployedArtifactState{Present: true, ProtocolVersion: crlfExtracted}
	lfState := domain.DeployedArtifactState{Present: true, ProtocolVersion: lfExtracted}

	crlfDrift := plan.ProtocolStaleness(crlfState, version)
	lfDrift := plan.ProtocolStaleness(lfState, version)

	if crlfDrift.Stale() {
		t.Errorf("staleness comparison: CRLF-marker document (extracted version %q) is reported as stale "+
			"against source version %q; a CRLF terminator must not cause false staleness",
			crlfExtracted, version)
	}
	if lfDrift.Stale() {
		t.Errorf("staleness comparison: LF-marker document (extracted version %q) is reported as stale "+
			"against source version %q; this is the baseline and must not be stale",
			lfExtracted, version)
	}
}

// ---------------------------------------------------------------------------
// probeDeployedArtifact — ProtocolVersion field population
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_ProtocolRegionWithVersionComment_ProtocolVersionPopulated verifies
// that when the deployed file contains a [[DEPLOYED:CommunicationProtocol]] region with a
// <!-- protocol-version: X --> comment, the returned state's ProtocolVersion carries that version.
func TestProbeDeployedArtifact_ProtocolRegionWithVersionComment_ProtocolVersionPopulated(t *testing.T) {
	ws := t.TempDir()
	content := []byte(
		"---\nversion: \"1.0\"\ntransform_version: \"2.0\"\ninjections_version: \"1.5\"\n---\n\n" +
			"[[DEPLOYED:CommunicationProtocol]]\n" +
			"<!-- protocol-version: 1.9 -->\n" +
			"## Communication Protocol\n\n" +
			"Protocol content here.\n\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true for a readable file")
	}
	if state.ProtocolVersion != "1.9" {
		t.Errorf("state.ProtocolVersion = %q, want %q; probeDeployedArtifact must populate ProtocolVersion "+
			"from the <!-- protocol-version: X --> comment inside the deployed protocol region",
			state.ProtocolVersion, "1.9")
	}
}

// TestProbeDeployedArtifact_NoProtocolRegion_ProtocolVersionEmpty verifies that when the
// deployed file carries no [[DEPLOYED:CommunicationProtocol]] region (e.g. an agent deployed
// before this feature was introduced), ProtocolVersion is "" in the returned state. An empty
// ProtocolVersion signals the planner to treat the agent as protocol-stale.
func TestProbeDeployedArtifact_NoProtocolRegion_ProtocolVersionEmpty(t *testing.T) {
	ws := t.TempDir()
	// A plain agent file with frontmatter only — no deployed protocol region.
	content := []byte(
		"---\nversion: \"1.0\"\ntransform_version: \"2.0\"\ninjections_version: \"1.5\"\n---\n\n" +
			"[[SECTION:Identity]]\nAgent identity.\n[[/SECTION:Identity]]\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true")
	}
	if state.ProtocolVersion != "" {
		t.Errorf("state.ProtocolVersion = %q, want empty string when no protocol region is present; "+
			"absence of the region must be treated as stale by the planner",
			state.ProtocolVersion)
	}
}

// TestProbeDeployedArtifact_ProtocolRegionPresentNoVersionComment_ProtocolVersionEmpty verifies
// that when the [[DEPLOYED:CommunicationProtocol]] region exists but carries no version comment,
// ProtocolVersion is "" in the returned state. This can happen for a file written by a version
// of the tool that emitted the region but not yet the version marker.
func TestProbeDeployedArtifact_ProtocolRegionPresentNoVersionComment_ProtocolVersionEmpty(t *testing.T) {
	ws := t.TempDir()
	content := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[DEPLOYED:CommunicationProtocol]]\n" +
			"## Communication Protocol\n\n" +
			"Content with no version comment.\n\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true")
	}
	if state.ProtocolVersion != "" {
		t.Errorf("state.ProtocolVersion = %q, want empty string when no version comment is in the region",
			state.ProtocolVersion)
	}
}

// TestProbeDeployedArtifact_ProtocolVersionDoesNotAffectOtherFields verifies that extracting
// the protocol version from the deployed file does not perturb the other state fields —
// Version, TransformVersion, InjectionsVersion, and Workflows must be unaffected.
func TestProbeDeployedArtifact_ProtocolVersionDoesNotAffectOtherFields(t *testing.T) {
	ws := t.TempDir()
	content := []byte(
		"---\nversion: \"2.0\"\ntransform_version: \"1.5\"\ninjections_version: \"1.2\"\n---\n\n" +
			"[[DEPLOYED:CommunicationProtocol]]\n" +
			"<!-- protocol-version: 1.9 -->\n" +
			"Protocol content.\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.ProtocolVersion != "1.9" {
		t.Errorf("state.ProtocolVersion = %q, want %q", state.ProtocolVersion, "1.9")
	}
	if state.Version != "2.0" {
		t.Errorf("state.Version = %q, want %q; must be unaffected by protocol version extraction",
			state.Version, "2.0")
	}
	if state.TransformVersion != "1.5" {
		t.Errorf("state.TransformVersion = %q, want %q; must be unaffected", state.TransformVersion, "1.5")
	}
	if state.InjectionsVersion != "1.2" {
		t.Errorf("state.InjectionsVersion = %q, want %q; must be unaffected", state.InjectionsVersion, "1.2")
	}
}

// TestProbeDeployedArtifact_AbsentFile_ProtocolVersionEmpty verifies that an absent file yields
// ProtocolVersion == "" alongside Present: false. The never-error contract must hold.
func TestProbeDeployedArtifact_AbsentFile_ProtocolVersionEmpty(t *testing.T) {
	ws := t.TempDir()

	state := probeDeployedArtifact(ws, "nonexistent.md", "")

	if state.Present {
		t.Fatal("expected Present: false for absent file")
	}
	if state.ProtocolVersion != "" {
		t.Errorf("state.ProtocolVersion = %q, want empty string for absent file; "+
			"absent files must produce no protocol version, consistent with all other scalar fields",
			state.ProtocolVersion)
	}
}

// TestProbeDeployedArtifact_ProtocolVersionNotCountedInHasVersionInfo verifies that
// ProtocolVersion is not included in the HasVersionInfo() signal. HasVersionInfo() indicates
// "this file carries at least one readable frontmatter version stamp"; protocol version is
// tracked through a separate channel (ProtocolDrift) and must not affect the existing
// HasVersionInfo semantics.
func TestProbeDeployedArtifact_ProtocolVersionNotCountedInHasVersionInfo(t *testing.T) {
	ws := t.TempDir()
	// File carries only a protocol region with a version — no frontmatter version stamps.
	content := []byte(
		"[[DEPLOYED:CommunicationProtocol]]\n" +
			"<!-- protocol-version: 1.9 -->\n" +
			"Protocol content.\n" +
			"[[/DEPLOYED:CommunicationProtocol]]\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.ProtocolVersion != "1.9" {
		// If ProtocolVersion is empty here the test is moot — it means probeDeployedArtifact
		// hasn't been implemented yet. That's expected in TDD RED phase.
		t.Logf("state.ProtocolVersion = %q (expected %q; probeDeployedArtifact may not yet populate this field)",
			state.ProtocolVersion, "1.9")
	}
	// Regardless of whether ProtocolVersion is populated, HasVersionInfo must be false when
	// no frontmatter version stamps are present. ProtocolVersion is not a frontmatter stamp.
	if state.HasVersionInfo() {
		t.Error("HasVersionInfo() returned true for a file with no frontmatter version stamps; " +
			"ProtocolVersion must not contribute to HasVersionInfo()")
	}
}
