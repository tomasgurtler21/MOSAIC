package agentformat_test

// container_invariant_test.go covers the encode-side container invariant for all
// registered formats. For every format:
//
//   1. Encoding canonical input that contains the carriage container produces output
//      that does not contain either container key.
//
//   2. Container consumption is reported, not silent: the report contains at least one
//      entry with Kind EntryCarriedContainer or EntryStrippedContainer -- never only
//      EntryDroppedForeignKey for the container key.
//
// The test is parameterized over agentformat.RegisteredIDs() so any format added
// later inherits both halves of the invariant automatically.
//
// Why both halves matter:
//   - (1) alone could pass even if the container is silently dropped as a foreign key.
//     Codex already drops unknown keys without writing them to TOML, so assertion (1)
//     is not sufficient to distinguish "properly consumed" from "silently dropped".
//   - (2) catches the silent-drop case by requiring a carriage-specific entry kind.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"

	// Blank-import the wiring package so all registered translators are available.
	// Do NOT replace with _ "mosaic-deploy/internal/agentformat/codextoml" -- that
	// would bypass the wiring package and leave a broken wiring package test green.
	_ "mosaic-deploy/internal/agentformat/all"
)

// buildCanonicalWithCarriageForInvariant builds a canonical document that contains
// the carriage container with a value-carried string key and a body, suitable for
// the per-format invariant test. The key "user_setting" with value "custom-val" is
// the carried user key; the document also includes name and description so the Codex
// encode path can produce output without triggering unrelated fallbacks.
func buildCanonicalWithCarriageForInvariant() []byte {
	pairs := []mosaic.FieldPair{
		{Key: "user_setting", Value: mosaic.ScalarValue("custom-val", mosaic.QuoteDouble)},
	}
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue("invariant-agent", mosaic.QuotePlain)},
		{Key: "description", Value: mosaic.ScalarValue("Invariant test agent.", mosaic.QuotePlain)},
		{Key: agentformat.CarriageValuesKey, Value: mosaic.MappingValue(pairs)},
	}
	fmText := docformat.RenderFrontmatter(fields, "\n")
	return []byte("---\n" + fmText + "---\nTest body.\n")
}

// TestContainerInvariant_PerFormat runs the two-part invariant for every registered
// format. Both assertions must hold after implementation:
//
//   (1) The encoded output contains neither "mosaic_carriage" nor "mosaic_carriage_markers"
//       as a key (the container must never appear in any format's output).
//
//   (2) The report does not contain EntryDroppedForeignKey for the container key
//       (the container must be consumed by the carriage path, not treated as a foreign key).
//
// In RED state this test fails:
//   - Markdown (currently identity): output still contains "mosaic_carriage" -> (1) fails.
//   - Codex (currently drops container as foreign): report has EntryDroppedForeignKey -> (2) fails.
func TestContainerInvariant_PerFormat(t *testing.T) {
	canonical := buildCanonicalWithCarriageForInvariant()

	for _, id := range agentformat.RegisteredIDs() {
		id := id
		t.Run(string(id), func(t *testing.T) {
			tr, err := agentformat.Lookup(id)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", id, err)
			}

			// Construct an artifact context that is valid for every format.
			// Codex uses OpCreate with nil PriorDeployed (standard create path).
			// Markdown ignores Op, so the same context works for both.
			ctx := agentformat.ArtifactContext{
				AgentKey: "invariant-agent",
				Op:       agentformat.OpCreate,
			}

			out, report, encErr := tr.Encode(canonical, ctx)
			if encErr != nil {
				t.Fatalf("Encode(%q) returned error: %v", id, encErr)
			}

			// Assertion (1): output must not contain either carriage key.
			// We check as raw bytes because the encoding varies (TOML vs YAML).
			for _, carriageKey := range agentformat.CarriageKeys() {
				// Check for the key as a TOML key assignment pattern or YAML key pattern.
				// Both formats write keys followed by a separator; checking for the key
				// name with a word boundary is sufficient for this invariant.
				if bytes.Contains(out, []byte(carriageKey+" =")) ||
					bytes.Contains(out, []byte(carriageKey+":")) ||
					bytes.Contains(out, []byte("\n"+carriageKey+"\n")) {
					t.Errorf("format %q: output contains carriage key %q; the container must never appear in any format's encoded output\nOutput:\n%s",
						id, carriageKey, out)
				}
			}

			// Assertion (2): report must not contain EntryDroppedForeignKey for the
			// container key. The container must be consumed by the carriage path.
			for _, entry := range report.Entries {
				if entry.Kind == agentformat.EntryDroppedForeignKey &&
					(entry.Key == agentformat.CarriageValuesKey || entry.Key == agentformat.CarriageMarkersKey) {
					t.Errorf("format %q: report has EntryDroppedForeignKey for %q; the container must be handled by the carriage path (EntryCarriedContainer or EntryStrippedContainer), not as a foreign key",
						id, entry.Key)
				}
			}

			// Assertion (3): report must contain at least one carriage-specific entry
			// (EntryCarriedContainer or EntryStrippedContainer), proving consumption
			// is reported and never silent.
			var hasCarriageReport bool
			for _, entry := range report.Entries {
				if entry.Kind == agentformat.EntryCarriedContainer ||
					entry.Kind == agentformat.EntryStrippedContainer {
					hasCarriageReport = true
					break
				}
			}
			if !hasCarriageReport {
				t.Errorf("format %q: report has no EntryCarriedContainer or EntryStrippedContainer; container consumption must be reported for every format",
					id)
			}
		})
	}
}

// TestContainerInvariant_Markdown_ReportsStrippedContainer verifies specifically that
// the Markdown identity translator reports EntryStrippedContainer for the container
// and its keys, and NOT EntryDroppedForeignKey. Markdown discards (strips) the
// container because it has no place for user TOML keys; the loss is reported.
func TestContainerInvariant_Markdown_ReportsStrippedContainer(t *testing.T) {
	canonical := buildCanonicalWithCarriageForInvariant()

	tr, err := agentformat.Lookup(formatid.Markdown)
	if err != nil {
		t.Fatalf("Lookup(Markdown): %v", err)
	}
	ctx := agentformat.ArtifactContext{AgentKey: "invariant-agent", Op: agentformat.OpCreate}

	out, report, encErr := tr.Encode(canonical, ctx)
	if encErr != nil {
		t.Fatalf("Markdown Encode returned error: %v", encErr)
	}

	// Output must not contain the container key.
	if bytes.Contains(out, []byte(agentformat.CarriageValuesKey)) {
		t.Errorf("Markdown output contains %q; container must be removed", agentformat.CarriageValuesKey)
	}

	// Report must have at least one EntryStrippedContainer entry, and at least one
	// such entry must name a key that was held in the container. The canonical built
	// by buildCanonicalWithCarriageForInvariant holds the key "user_setting", so at
	// least one EntryStrippedContainer entry must carry that key. An implementation
	// that emits a single EntryStrippedContainer with no key detail would fail here.
	var strippedFound bool
	var strippedKnownKeyFound bool
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryStrippedContainer {
			strippedFound = true
			if entry.Key == "user_setting" {
				strippedKnownKeyFound = true
			}
		}
	}
	if !strippedFound {
		t.Error("Markdown report has no EntryStrippedContainer; stripping the container must be reported")
	}
	if !strippedKnownKeyFound {
		t.Error("Markdown report has no EntryStrippedContainer with Key==\"user_setting\"; the report must name each held key, not just emit a bare container-level entry")
	}
}

// TestContainerInvariant_Codex_ReportsCarriedContainer verifies specifically that the
// Codex translator reports EntryCarriedContainer for each re-emitted user key, and NOT
// EntryDroppedForeignKey. Codex re-emits value-carried keys as native TOML keys; the
// preservation is reported.
func TestContainerInvariant_Codex_ReportsCarriedContainer(t *testing.T) {
	canonical := buildCanonicalWithCarriageForInvariant()

	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup(CodexTOML): %v", err)
	}
	ctx := agentformat.ArtifactContext{AgentKey: "invariant-agent", Op: agentformat.OpCreate}

	_, report, encErr := tr.Encode(canonical, ctx)
	if encErr != nil {
		t.Fatalf("Codex Encode returned error: %v", encErr)
	}

	var carriedFound bool
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryCarriedContainer {
			carriedFound = true
			break
		}
	}
	if !carriedFound {
		t.Error("Codex report has no EntryCarriedContainer; re-emitting a value-carried key must be reported as carried")
	}
}

// TestContainerInvariant_Markdown_BytePreserving verifies that Markdown container
// removal is byte-preserving: the output equals the input with only the two carriage
// key lines removed, byte for byte. No other bytes may be changed.
//
// This is the no-re-rendering assertion: a Markdown encode that removes the carriage
// keys by re-rendering the frontmatter would change surrounding bytes and fail this test.
//
// The input is hand-crafted with non-standard YAML formatting that docformat.RenderFrontmatter
// would normalise: the before_key line has two spaces after the colon instead of one.
// The expected output is computed by literal byte deletion of the container lines from
// the input, preserving the non-standard formatting exactly. A re-rendering implementation
// would normalise "before_key:  before-value" to "before_key: before-value" and fail.
func TestContainerInvariant_Markdown_BytePreserving(t *testing.T) {
	// Hand-craft the frontmatter bytes with non-standard formatting.
	// "before_key:  before-value" has two spaces after the colon; docformat.RenderFrontmatter
	// would produce one space, making any re-rendering implementation observable.
	//
	// The carriage container occupies two lines:
	//   mosaic_carriage:\n
	//   "  carried_key: \"carried-value\"\n"
	// Surgical line deletion must remove exactly these lines and nothing else.
	input := []byte(
		"---\n" +
			"name: bp-agent\n" +
			"before_key:  before-value\n" + // two spaces after colon -- non-standard formatting
			agentformat.CarriageValuesKey + ":\n" +
			"  carried_key: \"carried-value\"\n" +
			"after_key: after-value\n" +
			"---\n" +
			"Non-trivial body.\n" +
			"Second line.\n",
	)

	// Expected: the container lines removed by literal byte deletion, leaving the
	// non-standard "before_key:  before-value" formatting intact. This is what a
	// surgical-removal implementation produces. A re-rendering implementation would
	// produce "before_key: before-value" (single space) and fail this assertion.
	expected := []byte(
		"---\n" +
			"name: bp-agent\n" +
			"before_key:  before-value\n" + // same non-standard formatting must be preserved
			"after_key: after-value\n" +
			"---\n" +
			"Non-trivial body.\n" +
			"Second line.\n",
	)

	tr, err := agentformat.Lookup(formatid.Markdown)
	if err != nil {
		t.Fatalf("Lookup(Markdown): %v", err)
	}
	ctx := agentformat.ArtifactContext{AgentKey: "bp-agent", Op: agentformat.OpCreate}

	got, _, encErr := tr.Encode(input, ctx)
	if encErr != nil {
		t.Fatalf("Markdown Encode returned error: %v", encErr)
	}

	if !bytes.Equal(got, expected) {
		t.Errorf("Markdown Encode is not byte-preserving:\ngot:      %q\nexpected: %q", got, expected)
	}
}
