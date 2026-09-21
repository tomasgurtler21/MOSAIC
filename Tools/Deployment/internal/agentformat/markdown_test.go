package agentformat_test

// markdown_test.go covers the Markdown identity translator: both Encode and Decode
// are identity functions on canonical bytes for agent artifacts.

import (
	"bytes"
	"testing"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"
)

// canonicalAgentDoc builds a minimal canonical MOSAIC Markdown document with the
// given frontmatter YAML and body text.
func canonicalAgentDoc(frontmatterYAML, body string) []byte {
	return []byte("---\n" + frontmatterYAML + "---\n" + body)
}

// markdownCtx returns a minimal ArtifactContext for Markdown tests. Markdown ignores
// all context fields, so this is just a valid, non-zero value.
func markdownCtx() agentformat.ArtifactContext {
	return agentformat.ArtifactContext{
		AgentKey: "test-agent",
		Op:       agentformat.OpCreate,
	}
}

// TestMarkdownTranslator_Encode_ReturnsInputBytesUnchanged verifies that the Markdown
// translator's Encode returns bytes that are byte-identical to the canonical input,
// for a well-formed canonical document with no carriage container.
func TestMarkdownTranslator_Encode_ReturnsInputBytesUnchanged(t *testing.T) {
	input := canonicalAgentDoc(
		"name: test-agent\ndescription: A test agent.\nmodel: gpt-4\n",
		"This is the agent body.\n",
	)

	tr, err := agentformat.Lookup(formatid.Markdown)
	if err != nil {
		t.Fatalf("Lookup Markdown translator: %v", err)
	}

	got, _, encErr := tr.Encode(input, markdownCtx())
	if encErr != nil {
		t.Fatalf("Encode returned unexpected error: %v", encErr)
	}
	if !bytes.Equal(got, input) {
		t.Errorf("Markdown Encode modified the input bytes:\ngot:  %q\nwant: %q", got, input)
	}
}

// TestMarkdownTranslator_Encode_ReturnsEmptyReport verifies that the Markdown
// translator's Encode returns an empty Report for input with no carriage container.
func TestMarkdownTranslator_Encode_ReturnsEmptyReport(t *testing.T) {
	input := canonicalAgentDoc(
		"name: test-agent\ndescription: A test agent.\n",
		"Body text.\n",
	)

	tr, _ := agentformat.Lookup(formatid.Markdown)
	_, report, err := tr.Encode(input, markdownCtx())
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}
	if len(report.Entries) != 0 {
		t.Errorf("Markdown Encode returned %d report entries for input with no container; want 0", len(report.Entries))
	}
}

// TestMarkdownTranslator_Decode_ReturnsInputBytesUnchanged verifies that the Markdown
// translator's Decode returns bytes that are byte-identical to the deployed input.
func TestMarkdownTranslator_Decode_ReturnsInputBytesUnchanged(t *testing.T) {
	input := canonicalAgentDoc(
		"name: test-agent\ndescription: A test agent.\n",
		"This is the body.\n",
	)

	tr, err := agentformat.Lookup(formatid.Markdown)
	if err != nil {
		t.Fatalf("Lookup Markdown translator: %v", err)
	}

	got, _, decErr := tr.Decode(input, markdownCtx())
	if decErr != nil {
		t.Fatalf("Decode returned unexpected error: %v", decErr)
	}
	if !bytes.Equal(got, input) {
		t.Errorf("Markdown Decode modified the input bytes:\ngot:  %q\nwant: %q", got, input)
	}
}

// TestMarkdownTranslator_Decode_ReturnsEmptyReport verifies that the Markdown
// translator's Decode always returns an empty Report.
func TestMarkdownTranslator_Decode_ReturnsEmptyReport(t *testing.T) {
	input := canonicalAgentDoc(
		"name: test-agent\ndescription: A test agent.\n",
		"Body text.\n",
	)

	tr, _ := agentformat.Lookup(formatid.Markdown)
	_, report, err := tr.Decode(input, markdownCtx())
	if err != nil {
		t.Fatalf("Decode returned unexpected error: %v", err)
	}
	if len(report.Entries) != 0 {
		t.Errorf("Markdown Decode returned %d report entries; want 0", len(report.Entries))
	}
}

// TestMarkdownTranslator_EncodeDecodeAreIdentity_VariousBodies verifies the identity
// property across several canonical document shapes.
func TestMarkdownTranslator_EncodeDecodeAreIdentity_VariousBodies(t *testing.T) {
	cases := []struct {
		name string
		doc  []byte
	}{
		{
			name: "empty_body",
			doc:  canonicalAgentDoc("name: a\n", ""),
		},
		{
			name: "multiline_body",
			doc:  canonicalAgentDoc("name: b\ndescription: d\n", "Line one.\nLine two.\n"),
		},
		{
			name: "no_frontmatter_keys_beyond_name",
			doc:  canonicalAgentDoc("name: c\n", "Only body.\n"),
		},
		{
			name: "body_with_markdown",
			doc:  canonicalAgentDoc("name: d\n", "# Heading\n\nParagraph.\n\n- item 1\n- item 2\n"),
		},
	}

	tr, err := agentformat.Lookup(formatid.Markdown)
	if err != nil {
		t.Fatalf("Lookup Markdown translator: %v", err)
	}

	ctx := markdownCtx()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, _, err := tr.Encode(tc.doc, ctx)
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			if !bytes.Equal(encoded, tc.doc) {
				t.Errorf("Encode: got %q, want %q", encoded, tc.doc)
			}

			decoded, _, err := tr.Decode(tc.doc, ctx)
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if !bytes.Equal(decoded, tc.doc) {
				t.Errorf("Decode: got %q, want %q", decoded, tc.doc)
			}
		})
	}
}

// TestMarkdownTranslator_Encode_AcceptsAnyOpValue verifies that the Markdown identity
// translator does not raise ErrUnspecifiedOperation or ErrMissingPriorBytes for any Op
// value, including OpUnspecified, OpCreate, and OpUpdate. This is the mirror test that
// proves the Op strictness did not leak onto the Markdown path.
//
// The ContractsDesign mandates four Op contexts. All four are tested here:
//   - OpUnspecified, nil PriorDeployed
//   - OpCreate, nil PriorDeployed
//   - OpUpdate, nil PriorDeployed
//   - OpCreate, non-nil PriorDeployed  (Codex rule 2 rejects this; Markdown must accept it)
func TestMarkdownTranslator_Encode_AcceptsAnyOpValue(t *testing.T) {
	doc := canonicalAgentDoc("name: test\n", "body\n")
	tr, _ := agentformat.Lookup(formatid.Markdown)

	ops := []agentformat.Operation{
		agentformat.OpUnspecified,
		agentformat.OpCreate,
		agentformat.OpUpdate,
	}
	for _, op := range ops {
		ctx := agentformat.ArtifactContext{
			AgentKey: "test",
			Op:       op,
		}
		_, _, err := tr.Encode(doc, ctx)
		if err != nil {
			t.Errorf("Markdown Encode with Op=%q returned error %v; want nil (Markdown ignores Op)", op, err)
		}
	}

	// Fourth mandatory context: OpCreate with non-nil PriorDeployed. Codex rule 2
	// rejects this combination (OpCreate must not have prior bytes), but the Markdown
	// path is an identity translator and must not apply Codex Op enforcement rules.
	// An implementation that accidentally copies Codex rule 2 onto the Markdown path
	// would fail here while passing the three nil-PriorDeployed cases above.
	priorBytes := canonicalAgentDoc("name: test\n", "prior body\n")
	createWithPrior := agentformat.ArtifactContext{
		AgentKey:      "test",
		Op:            agentformat.OpCreate,
		PriorDeployed: priorBytes,
	}
	if _, _, err := tr.Encode(doc, createWithPrior); err != nil {
		t.Errorf("Markdown Encode with Op=OpCreate and non-nil PriorDeployed returned error %v; "+
			"want nil (Markdown accepts all Op contexts regardless of PriorDeployed)", err)
	}
}
