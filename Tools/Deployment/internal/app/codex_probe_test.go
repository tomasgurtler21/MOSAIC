package app

// codex_probe_test.go covers T8.1: probeDeployedArtifact extension behaviour for Codex
// .toml files. These tests verify that the deployed-state probe correctly decodes Codex TOML
// through the funnel and extracts version stamps and model ID from the decoded canonical form.
//
// Verified behaviours:
//   - A valid Codex .toml file with a stamp block yields Present: true with Version,
//     HarnessVersion, and ModelID populated from the decoded canonical frontmatter.
//   - An absent .toml path (not yet deployed) yields Present: false.
//   - A broken .toml file (TOML syntax error) yields Present: true with empty scalar fields
//     (graceful degradation: the file exists but is undecodable).
//   - A Codex descriptor with ArtifactAgent extension ".toml" is treated consistently with
//     a Markdown descriptor: the same probeDeployedArtifact function handles both.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	_ "mosaic-deploy/internal/agentformat/all"
)

// ---------------------------------------------------------------------------
// codexProbeDesc returns a minimal HarnessDescriptor for Codex probing in these tests.
// ---------------------------------------------------------------------------

func codexProbeDesc() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		AgentFormatID: "codex-toml",
		Frontmatter: domain.FrontmatterSpec{
			ModelKey: "model",
		},
		Extensions: map[domain.ArtifactKind]string{
			domain.ArtifactAgent: ".toml",
		},
	}
}

// codexTomlBytes builds a minimal Codex TOML file with the given stamp values and model.
// Stamps are written as leading comment lines in agentfields registry order:
//
//	# mosaic_version: <version>
//	# mosaic_harness_version: <harnessVersion>
//
// The TOML body always includes sandbox_mode and developer_instructions so that the
// Codex decoder produces a valid canonical document.
func codexTomlBytes(version, harnessVersion, model string) []byte {
	var b []byte
	if version != "" {
		b = append(b, ("# mosaic_version: " + version + "\n")...)
	}
	if harnessVersion != "" {
		b = append(b, ("# mosaic_harness_version: " + harnessVersion + "\n")...)
	}
	if len(b) > 0 {
		b = append(b, '\n') // blank separator line after stamp block
	}
	if model != "" {
		b = append(b, ("model = \"" + model + "\"\n")...)
	}
	b = append(b, "sandbox_mode = \"read-only\"\n"...)
	b = append(b, "developer_instructions = \"Agent body.\\n\"\n"...)
	return b
}

// ---------------------------------------------------------------------------
// T8.1 — probeDeployedArtifact for Codex .toml
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_CodexToml_PresentAndVersionsPopulated verifies that a valid
// Codex .toml file with stamp comments is decoded through the funnel and the stamp values
// appear in the returned DeployedArtifactState as Version and HarnessVersion.
func TestProbeDeployedArtifact_CodexToml_PresentAndVersionsPopulated(t *testing.T) {
	ws := t.TempDir()
	content := codexTomlBytes("2.0", "1.5", "gpt-6-astra")
	writeFile(t, ws, "agent.toml", content)

	state := probeDeployedArtifact(ws, "agent.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if !state.Present {
		t.Fatal("expected Present: true for a readable Codex .toml file, got false")
	}
	if state.Version != "2.0" {
		t.Errorf("Version = %q, want %q; mosaic_version stamp must be extracted through the decode funnel",
			state.Version, "2.0")
	}
	if state.HarnessVersion != "1.5" {
		t.Errorf("HarnessVersion = %q, want %q; mosaic_harness_version stamp must be extracted through the decode funnel",
			state.HarnessVersion, "1.5")
	}
}

// TestProbeDeployedArtifact_CodexToml_ModelIDExtracted verifies that the model key declared
// by the Codex harness descriptor is read from the decoded canonical frontmatter and returned
// as ModelID. This confirms the model is carried through the TOML-to-Markdown decode path.
func TestProbeDeployedArtifact_CodexToml_ModelIDExtracted(t *testing.T) {
	ws := t.TempDir()
	content := codexTomlBytes("2.0", "1.5", "gpt-6-astra")
	writeFile(t, ws, "agent.toml", content)

	state := probeDeployedArtifact(ws, "agent.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if state.ModelID != "gpt-6-astra" {
		t.Errorf("ModelID = %q, want %q; model TOML key must be decoded and extracted from canonical frontmatter",
			state.ModelID, "gpt-6-astra")
	}
}

// TestProbeDeployedArtifact_CodexToml_Absent_PresentIsFalse verifies that a .toml path that
// does not exist on disk yields Present: false, identical to the Markdown absent-file contract.
func TestProbeDeployedArtifact_CodexToml_Absent_PresentIsFalse(t *testing.T) {
	ws := t.TempDir()

	state := probeDeployedArtifact(ws, "nonexistent.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if state.Present {
		t.Error("expected Present: false for absent .toml file, got true")
	}
	if state.ContentHash != "" {
		t.Errorf("expected empty ContentHash for absent file, got %q", state.ContentHash)
	}
}

// TestProbeDeployedArtifact_CodexToml_BrokenSyntax_PresentTrueVersionsEmpty verifies that a
// .toml file with a TOML syntax error (broken, undecodable) still reports Present: true with
// a non-empty ContentHash but empty scalar fields. This is the graceful-degradation contract:
// a file that exists but cannot be decoded is not treated as absent.
func TestProbeDeployedArtifact_CodexToml_BrokenSyntax_PresentTrueVersionsEmpty(t *testing.T) {
	ws := t.TempDir()
	// Invalid TOML: duplicate key causes a syntax error on unmarshal.
	content := []byte("model = \"gpt-6-astra\"\nmodel = \"duplicate-key\"\n")
	writeFile(t, ws, "broken.toml", content)

	state := probeDeployedArtifact(ws, "broken.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if !state.Present {
		t.Fatal("broken .toml must still be Present: true (file exists); got Present: false")
	}
	if state.ContentHash == "" {
		t.Error("broken .toml must have a non-empty ContentHash; raw bytes are still hashed even when undecodable")
	}
	if state.Version != "" || state.HarnessVersion != "" {
		t.Errorf("version fields must be empty for undecodable .toml; got Version=%q HarnessVersion=%q",
			state.Version, state.HarnessVersion)
	}
	if state.ModelID != "" {
		t.Errorf("ModelID must be empty for undecodable .toml; got %q", state.ModelID)
	}
}

// TestProbeDeployedArtifact_CodexToml_NoStampBlock_VersionsEmpty verifies that a valid Codex
// .toml file with no leading stamp comments decodes successfully but yields empty Version and
// HarnessVersion, because no stamp values were present in the file.
func TestProbeDeployedArtifact_CodexToml_NoStampBlock_VersionsEmpty(t *testing.T) {
	ws := t.TempDir()
	// No stamp comments, but valid TOML with a model key.
	content := []byte("model = \"gpt-6-astra\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"Body.\\n\"\n")
	writeFile(t, ws, "agent.toml", content)

	state := probeDeployedArtifact(ws, "agent.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if !state.Present {
		t.Fatal("expected Present: true for a valid .toml file without stamp block")
	}
	if state.Version != "" || state.HarnessVersion != "" {
		t.Errorf("Version and HarnessVersion must be empty when no stamp block is present; got Version=%q HarnessVersion=%q",
			state.Version, state.HarnessVersion)
	}
	// Model must still be extracted even without stamps.
	if state.ModelID != "gpt-6-astra" {
		t.Errorf("ModelID = %q, want %q; model must be extracted from TOML body even without stamp block",
			state.ModelID, "gpt-6-astra")
	}
}

// TestProbeDeployedArtifact_CodexToml_WorkflowMarkersExtracted verifies that when a Codex .toml
// deployed agent has a Workflow managed region in its developer_instructions body, the probe
// correctly decodes the TOML through the funnel and populates state.Workflows with the expected
// workflow ID and version. This confirms the workflow-marker probe path works for the .toml
// extension (AC8.1 requires workflow markers to work for both .toml and .md deployed agents).
func TestProbeDeployedArtifact_CodexToml_WorkflowMarkersExtracted(t *testing.T) {
	ws := t.TempDir()
	// A Codex TOML file whose developer_instructions body contains an AvailableWorkflows managed
	// region enclosing a Workflow managed region with name="my-workflow". After TOML decode
	// the body becomes canonical Markdown; extractDeployedWorkflows enumerates DeployedRegions
	// at all nesting levels, finds the Workflow region, and records id "my-workflow" (from the
	// name attribute) and version "2.0" (from the version attribute).
	content := []byte(
		"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"my-workflow\" version=\"2.0\">\n" +
			"Workflow body content.\n" +
			"</Workflow>\n" +
			"</AvailableWorkflows>\n" +
			"\"\"\"\n",
	)
	writeFile(t, ws, "agent.toml", content)

	state := probeDeployedArtifact(ws, "agent.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if !state.Present {
		t.Fatal("expected Present: true for a readable Codex .toml file with workflow region")
	}
	if len(state.Workflows) == 0 {
		t.Fatal("Workflows is empty; a Codex .toml file with a Workflow managed region in " +
			"developer_instructions must yield at least one entry in state.Workflows after " +
			"decode through the funnel")
	}
	if state.Workflows[0].ID != "my-workflow" {
		t.Errorf("Workflows[0].ID = %q, want %q; workflow ID must be extracted from the Workflow: name prefix",
			state.Workflows[0].ID, "my-workflow")
	}
	if state.Workflows[0].Version != "2.0" {
		t.Errorf("Workflows[0].Version = %q, want %q; version attribute must be extracted from the region opening tag",
			state.Workflows[0].Version, "2.0")
	}
}

// TestProbeDeployedArtifact_CodexToml_ProtocolVersionExtracted verifies that when a Codex .toml
// deployed agent has a CommunicationProtocol managed region in its developer_instructions body,
// the probe correctly decodes the TOML through the funnel and populates state.ProtocolVersion
// with the version attribute from the region. This confirms the protocol-version probe path
// works for the .toml extension (AC8.1 requires protocol version to work for both extensions).
func TestProbeDeployedArtifact_CodexToml_ProtocolVersionExtracted(t *testing.T) {
	ws := t.TempDir()
	// A Codex TOML file whose developer_instructions body contains a CommunicationProtocol
	// managed region with a version attribute. After TOML decode the body becomes canonical
	// Markdown; extractDeployedProtocolVersion finds the version in the region tag.
	content := []byte(
		"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<CommunicationProtocol type=\"managed\" version=\"1.11\">\n" +
			"Protocol content here.\n" +
			"</CommunicationProtocol>\n" +
			"\"\"\"\n",
	)
	writeFile(t, ws, "agent.toml", content)

	state := probeDeployedArtifact(ws, "agent.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if !state.Present {
		t.Fatal("expected Present: true for a readable Codex .toml file with CommunicationProtocol region")
	}
	if state.ProtocolVersion != "1.11" {
		t.Errorf("ProtocolVersion = %q, want %q; "+
			"a CommunicationProtocol managed region in developer_instructions must yield "+
			"the version attribute after decode through the funnel",
			state.ProtocolVersion, "1.11")
	}
}

// TestProbeDeployedArtifact_CodexToml_MissingInstructions_DecodeNoticePopulated verifies that
// when a Codex .toml file has no developer_instructions key, the Codex decoder emits an
// EntryMissingInstructions report entry and probeDeployedArtifact surfaces it as a non-empty
// DecodeNotices slice on the returned state. This satisfies AC8.9: decode report surfacing for
// the probe path makes EntryMissingInstructions observable in the run report.
func TestProbeDeployedArtifact_CodexToml_MissingInstructions_DecodeNoticePopulated(t *testing.T) {
	ws := t.TempDir()
	// A Codex TOML file with no developer_instructions key. The Codex decoder records
	// EntryMissingInstructions in the decode report; the probe must surface this as a
	// non-empty DecodeNotices slice rather than silently discarding the report.
	content := []byte(
		"model = \"gpt-6-astra\"\n" +
			"sandbox_mode = \"read-only\"\n",
	)
	writeFile(t, ws, "agent.toml", content)

	state := probeDeployedArtifact(ws, "agent.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if !state.Present {
		t.Fatal("expected Present: true for a readable Codex .toml file without developer_instructions")
	}
	if len(state.DecodeNotices) == 0 {
		t.Error("DecodeNotices is empty; a Codex .toml file with no developer_instructions key " +
			"must produce at least one decode notice (EntryMissingInstructions) when probed, " +
			"making the missing-instructions condition observable in the run report")
	}
}

// TestProbeDeployedArtifact_CodexToml_InjectionsVersionAndHasInjectionRegion verifies that when
// a Codex .toml deployed agent carries an InjectionHarness-class region (HarnessConstraints) in
// its developer_instructions body, the probe correctly decodes the TOML through the funnel,
// parses the canonical body, and populates both InjectionsVersion (from the region's version
// attribute) and HasInjectionRegion = true. This confirms the injection probe path works for
// the .toml extension, not just for Markdown .md deployed files.
func TestProbeDeployedArtifact_CodexToml_InjectionsVersionAndHasInjectionRegion(t *testing.T) {
	ws := t.TempDir()
	// A Codex TOML file whose developer_instructions body contains a HarnessConstraints
	// managed region with a version attribute. After TOML decode the body becomes canonical
	// Markdown, and extractDeployedInjectionVersion finds the version in the region tag.
	content := []byte(
		"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<HarnessConstraints type=\"managed\" version=\"3.1\">\n" +
			"Harness constraint content.\n" +
			"</HarnessConstraints>\n" +
			"\"\"\"\n",
	)
	writeFile(t, ws, "agent.toml", content)

	state := probeDeployedArtifact(ws, "agent.toml", "model", domain.ArtifactAgent, codexProbeDesc())

	if !state.Present {
		t.Fatal("expected Present: true for a readable Codex .toml file with injection region")
	}
	if !state.HasInjectionRegion {
		t.Error("HasInjectionRegion = false, want true; " +
			"a Codex .toml file with an InjectionHarness-class region in developer_instructions " +
			"must yield HasInjectionRegion = true after decode through the funnel")
	}
	if state.InjectionsVersion != "3.1" {
		t.Errorf("InjectionsVersion = %q, want %q; " +
			"the version attribute on the InjectionHarness-class region must be extracted from " +
			"the canonical body produced by decoding the Codex .toml through the funnel",
			state.InjectionsVersion, "3.1")
	}
}
