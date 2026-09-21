package codextoml_test

// decode_test.go covers the Codex TOML decode path:
//   - A valid Codex TOML file decodes to a canonical MOSAIC Markdown document with
//     YAML frontmatter and a body recovered byte-exactly from developer_instructions.
//   - The decoded key order is fixed and total (determinism).
//   - Calling Decode twice on the same input produces identical bytes.
//   - The decoded YAML frontmatter is identical to what the docformat renderer would
//     produce for the same key/value pairs (i.e. the Markdown path produces the same
//     bytes for the same input values).
//   - Stamps present in the leading comment block appear in the decoded frontmatter
//     ahead of the owned keys, in agentfields.All() registry order.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat"
)

// decodeCtx returns an ArtifactContext suitable for Codex decode tests.
func decodeCtx(agentKey string) agentformat.ArtifactContext {
	return agentformat.ArtifactContext{AgentKey: agentKey}
}

// decodeToml calls Decode and returns the canonical bytes. Fails the test on error.
func decodeToml(t *testing.T, tomlBytes []byte, agentKey string) []byte {
	t.Helper()
	tr := codexTranslator(t)
	out, _, err := tr.Decode(tomlBytes, decodeCtx(agentKey))
	if err != nil {
		t.Fatalf("Decode returned error: %v\nInput:\n%s", err, tomlBytes)
	}
	return out
}

// splitCanonical splits canonical bytes into frontmatter bytes and body bytes
// using docformat.SplitFrontmatter. Fails the test on error.
func splitCanonical(t *testing.T, canonical []byte) (frontmatter, body []byte) {
	t.Helper()
	fm, b, err := docformat.SplitFrontmatter(canonical)
	if err != nil {
		t.Fatalf("SplitFrontmatter: %v\nInput:\n%s", err, canonical)
	}
	return fm, b
}

// parseCanonical parses canonical bytes into a Document. Fails the test on error.
func parseCanonical(t *testing.T, canonical []byte) *docformat.Document {
	t.Helper()
	doc, err := docformat.Parse(canonical)
	if err != nil {
		t.Fatalf("docformat.Parse canonical output: %v\nInput:\n%s", err, canonical)
	}
	return doc
}

// checkField asserts that the frontmatter of doc has key with the given unescaped
// scalar text. Fails if the key is absent or not a scalar.
func checkField(t *testing.T, doc *docformat.Document, key, wantText string) {
	t.Helper()
	fv, ok := doc.Frontmatter().Get(key)
	if !ok {
		t.Errorf("key %q absent from decoded frontmatter", key)
		return
	}
	if fv.Kind != mosaic.KindScalar {
		t.Errorf("key %q has kind %q; want scalar", key, fv.Kind)
		return
	}
	if fv.Scalar != wantText {
		t.Errorf("key %q value = %q; want %q", key, fv.Scalar, wantText)
	}
}

// TestCodexDecode_ProducesValidCanonicalDocument verifies that a typical Codex TOML
// file decodes to a valid canonical MOSAIC Markdown document with YAML frontmatter.
func TestCodexDecode_ProducesValidCanonicalDocument(t *testing.T) {
	tomlInput := []byte(`name = "my-agent"
description = "A test agent."
model = "claude-opus-4-5"
sandbox_mode = "read-only"
developer_instructions = "Do useful things.\n"
`)
	canonical := decodeToml(t, tomlInput, "my-agent")

	// The output must begin with the frontmatter opening delimiter.
	if !bytes.HasPrefix(canonical, []byte("---\n")) {
		t.Errorf("decoded output does not begin with frontmatter delimiter; got: %q (first 20 bytes)", canonical[:min(20, len(canonical))])
	}

	// The output must be parseable as a MOSAIC document.
	doc := parseCanonical(t, canonical)
	if !doc.Frontmatter().Present() {
		t.Error("decoded output has no frontmatter block")
	}
}

// TestCodexDecode_BodyRecoveredByteExactly verifies that the body in the decoded
// canonical document equals the developer_instructions value byte for byte.
func TestCodexDecode_BodyRecoveredByteExactly(t *testing.T) {
	const wantBody = "This is the agent body.\nSecond line.\n"
	tomlInput := []byte("name = \"agent\"\ndescription = \"desc\"\ndeveloper_instructions = \"This is the agent body.\\nSecond line.\\n\"\n")

	canonical := decodeToml(t, tomlInput, "agent")
	_, body := splitCanonical(t, canonical)

	if string(body) != wantBody {
		t.Errorf("decoded body = %q; want %q (must equal developer_instructions byte for byte)", body, wantBody)
	}
}

// TestCodexDecode_BodyRecoveredByteExactly_Multiline verifies body recovery when
// developer_instructions uses the multi-line basic string form (""").
func TestCodexDecode_BodyRecoveredByteExactly_Multiline(t *testing.T) {
	const wantBody = "Line one.\nLine two.\n"
	tomlInput := []byte("name = \"agent\"\ndescription = \"desc\"\ndeveloper_instructions = \"\"\"\nLine one.\nLine two.\n\"\"\"\n")

	canonical := decodeToml(t, tomlInput, "agent")
	_, body := splitCanonical(t, canonical)

	if string(body) != wantBody {
		t.Errorf("decoded body = %q; want %q (multi-line form must round-trip exactly)", body, wantBody)
	}
}

// TestCodexDecode_OwnedKeys_PresentInFrontmatter verifies that name, description,
// model and sandbox_mode appear in the decoded frontmatter when present in the TOML.
func TestCodexDecode_OwnedKeys_PresentInFrontmatter(t *testing.T) {
	tomlInput := []byte(`name = "my-agent"
description = "An agent description."
model = "claude-opus-4-5"
sandbox_mode = "read-only"
developer_instructions = "body"
`)
	canonical := decodeToml(t, tomlInput, "my-agent")
	doc := parseCanonical(t, canonical)

	checkField(t, doc, "name", "my-agent")
	checkField(t, doc, "description", "An agent description.")
	checkField(t, doc, "model", "claude-opus-4-5")
	checkField(t, doc, "sandbox_mode", "read-only")
}

// TestCodexDecode_OwnedKey_AbsentWhenMissing verifies that model is absent from
// the decoded frontmatter when it is absent from the TOML file. The Codex format
// optionally carries model; its absence is not an error.
func TestCodexDecode_OwnedKey_AbsentWhenMissing(t *testing.T) {
	tomlInput := []byte(`name = "agent"
description = "desc"
developer_instructions = "body"
`)
	canonical := decodeToml(t, tomlInput, "agent")
	doc := parseCanonical(t, canonical)

	if _, ok := doc.Frontmatter().Get("model"); ok {
		t.Error("model is present in decoded frontmatter but was absent from TOML input; it must be omitted")
	}
}

// TestCodexDecode_KeyOrder_IsFixed verifies that the decoded frontmatter keys appear
// in the fixed total order declared by the decode contract: stamps first (in
// agentfields.All() registry order), then name, description, model, sandbox_mode.
func TestCodexDecode_KeyOrder_IsFixed(t *testing.T) {
	// Build TOML with stamps (using the first two agentfields registry entries)
	// and owned keys supplied in reverse order to confirm the output order is fixed.
	fields := agentfields.All()
	if len(fields) < 2 {
		t.Skip("need at least two stamp entries in agentfields.All()")
	}

	f0, f1 := fields[0], fields[1]
	tomlInput := []byte("# " + f1.Deployed + ": v2\n# " + f0.Deployed + ": v1\nsandbox_mode = \"read-only\"\nmodel = \"m\"\ndescription = \"d\"\nname = \"a\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, tomlInput, "a")
	doc := parseCanonical(t, canonical)
	keys := doc.Frontmatter().Keys()

	// Stamps must come first (in agentfields order), then owned keys.
	if len(keys) < 2 {
		t.Fatalf("decoded frontmatter has only %d keys; want at least 2", len(keys))
	}

	// The first two keys must be f0 then f1 (agentfields order, not TOML input order).
	if keys[0] != f0.Deployed {
		t.Errorf("keys[0] = %q; want %q (stamps first, agentfields order)", keys[0], f0.Deployed)
	}
	if keys[1] != f1.Deployed {
		t.Errorf("keys[1] = %q; want %q (stamps second, agentfields order)", keys[1], f1.Deployed)
	}

	// Among the owned keys, the order must be: name, description, model, sandbox_mode.
	ownedOrder := []string{"name", "description", "model", "sandbox_mode"}
	j := 0
	for _, k := range keys {
		if j < len(ownedOrder) && k == ownedOrder[j] {
			j++
		}
	}
	if j != len(ownedOrder) {
		t.Errorf("owned keys not in fixed order after stamps\nkeys: %v\nwant subsequence: %v", keys, ownedOrder)
	}
}

// TestCodexDecode_IsDeterministic verifies that decoding the same input twice
// produces byte-identical output.
func TestCodexDecode_IsDeterministic(t *testing.T) {
	tomlInput := []byte(`name = "agent"
description = "desc"
model = "m"
sandbox_mode = "read-only"
developer_instructions = "body"
`)
	tr := codexTranslator(t)
	ctx := decodeCtx("agent")

	out1, _, err1 := tr.Decode(tomlInput, ctx)
	if err1 != nil {
		t.Fatalf("first Decode error: %v", err1)
	}
	out2, _, err2 := tr.Decode(tomlInput, ctx)
	if err2 != nil {
		t.Fatalf("second Decode error: %v", err2)
	}

	if !bytes.Equal(out1, out2) {
		t.Errorf("Decode is not deterministic: two calls with identical input produced different output\nfirst:  %q\nsecond: %q", out1, out2)
	}
}

// TestCodexDecode_FrontmatterIdenticalToMarkdownPath verifies that the YAML
// frontmatter produced by decode is identical to what docformat.RenderFrontmatter
// would produce for the same key/value pairs using the same quoting policy.
// This is the assertion that keeps the Codex decode path byte-identical to the
// Markdown path for any value the Markdown path would render.
func TestCodexDecode_FrontmatterIdenticalToMarkdownPath(t *testing.T) {
	const (
		agentKey = "my-agent"
		desc     = "An ordinary description."
		model    = "claude-opus-4-5"
		sandbox  = "read-only"
	)
	tomlInput := []byte("name = \"" + agentKey + "\"\ndescription = \"" + desc + "\"\nmodel = \"" + model + "\"\nsandbox_mode = \"" + sandbox + "\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, tomlInput, agentKey)
	fm, _ := splitCanonical(t, canonical)

	// Build the expected frontmatter using docformat.RenderFrontmatter.
	// The quoting policy for plain (unquoted) values: no leading/trailing space,
	// no leading #, no ": ", no reserved spelling.
	plain := mosaic.QuotePlain
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue(agentKey, plain)},
		{Key: "description", Value: mosaic.ScalarValue(desc, plain)},
		{Key: "model", Value: mosaic.ScalarValue(model, plain)},
		{Key: "sandbox_mode", Value: mosaic.ScalarValue(sandbox, plain)},
	}
	wantFM := docformat.RenderFrontmatter(fields, "\n")

	if string(fm) != wantFM {
		t.Errorf("decoded frontmatter does not match docformat.RenderFrontmatter output\ngot:\n%s\nwant:\n%s", fm, wantFM)
	}
}

// TestCodexDecode_Stamps_PresentInFrontmatter verifies that a stamp comment block
// in the TOML file is decoded into the canonical frontmatter.
func TestCodexDecode_Stamps_PresentInFrontmatter(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}

	f := fields[0]
	const stampVal = "1.2.3"
	tomlInput := []byte("# " + f.Deployed + ": " + stampVal + "\nname = \"a\"\ndescription = \"d\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, tomlInput, "a")
	doc := parseCanonical(t, canonical)

	fv, ok := doc.Frontmatter().Get(f.Deployed)
	if !ok {
		t.Errorf("stamp key %q absent from decoded frontmatter; must be present when in TOML comment block", f.Deployed)
		return
	}
	if fv.Scalar != stampVal {
		t.Errorf("stamp %q value = %q; want %q", f.Deployed, fv.Scalar, stampVal)
	}
}

