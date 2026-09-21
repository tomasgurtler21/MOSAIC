package app_test

// parse_generic_source_test.go verifies that ParseGenericSource is behaviour-identical
// to the raw docformat.Parse it replaced for generic-source documents.
//
// This test was written after I11.1 (which created ParseGenericSource) and I11.2
// (which migrated the call sites). Its purpose is to prove that the migration changed
// no observable behaviour: a caller that previously called docformat.Parse on a
// generic source and now calls ParseGenericSource gets the same result.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/app"
)

// TestParseGenericSource_BehaviourIdentical asserts that ParseGenericSource produces
// output that is byte-identical to docformat.Parse for the same representative
// generic-source input. The test exercises a document with YAML frontmatter and a
// non-trivial body, covering the full parse path.
//
// The test uses docformat.Parse in a test file, which is explicitly excluded from the
// read-boundary guard (only non-test production files are checked).
func TestParseGenericSource_BehaviourIdentical(t *testing.T) {
	// A representative generic-source document: YAML frontmatter with several fields
	// and a multi-paragraph body. This is the typical shape of a catalog source file
	// or a render input.
	src := []byte(`---
role: subagent
version: 1.2.3
name: Example Agent
---

# Example

This is a representative generic-source document used to verify that
ParseGenericSource is behaviour-identical to docformat.Parse.

It has multiple paragraphs and a non-trivial body.
`)

	// Parse with the raw entry point (docformat.Parse).
	rawDoc, rawErr := docformat.Parse(src)

	// Parse with the named generic-source operation.
	gDoc, gErr := app.ParseGenericSource(src)

	// Both must succeed or both must fail with the same error shape.
	if (rawErr == nil) != (gErr == nil) {
		t.Fatalf("error mismatch: docformat.Parse error = %v, ParseGenericSource error = %v",
			rawErr, gErr)
	}
	if rawErr != nil {
		// Both failed; the test is satisfied: errors are equal in presence.
		return
	}

	// Both succeeded. Compare the rendered output: re-serialise both documents and
	// assert byte identity. docformat.Document.Bytes() is the canonical serialisation.
	rawBytes := rawDoc.Bytes()
	gBytes := gDoc.Bytes()

	if !bytes.Equal(rawBytes, gBytes) {
		t.Fatalf("ParseGenericSource output differs from docformat.Parse output\n"+
			"docformat.Parse bytes (%d):\n%s\n\nParseGenericSource bytes (%d):\n%s",
			len(rawBytes), rawBytes, len(gBytes), gBytes)
	}

	// Cross-check frontmatter: both must have the same key set.
	rawFm := rawDoc.Frontmatter()
	gFm := gDoc.Frontmatter()

	if rawFm.Present() != gFm.Present() {
		t.Fatalf("frontmatter presence mismatch: docformat.Parse=%v, ParseGenericSource=%v",
			rawFm.Present(), gFm.Present())
	}

	rawKeys := rawFm.Keys()
	gKeys := gFm.Keys()
	if len(rawKeys) != len(gKeys) {
		t.Fatalf("frontmatter key count mismatch: docformat.Parse=%d keys, ParseGenericSource=%d keys",
			len(rawKeys), len(gKeys))
	}
	for i, k := range rawKeys {
		if k != gKeys[i] {
			t.Fatalf("frontmatter key[%d] mismatch: docformat.Parse=%q, ParseGenericSource=%q",
				i, k, gKeys[i])
		}
		rawVal, _ := rawFm.Get(k)
		gVal, _ := gFm.Get(k)
		if rawVal.Scalar != gVal.Scalar {
			t.Fatalf("frontmatter value for key %q mismatch: docformat.Parse=%q, ParseGenericSource=%q",
				k, rawVal.Scalar, gVal.Scalar)
		}
	}

}

// TestParseGenericSource_ErrorPassthrough asserts that ParseGenericSource passes
// through parse errors from docformat.Parse unchanged. A corrupt input that
// docformat.Parse rejects must also be rejected by ParseGenericSource.
func TestParseGenericSource_ErrorPassthrough(t *testing.T) {
	// Deliberately malformed frontmatter: unclosed delimiter.
	malformed := []byte(`---
key: value
`)

	rawDoc, rawErr := docformat.Parse(malformed)
	gDoc, gErr := app.ParseGenericSource(malformed)

	// Both must agree on whether the input was accepted or rejected.
	if (rawErr == nil) != (gErr == nil) {
		t.Fatalf("error presence mismatch for malformed input: docformat.Parse=%v, ParseGenericSource=%v",
			rawErr, gErr)
	}

	// If both accepted (docformat treats unclosed frontmatter as body-only), both
	// documents must still be equal.
	if rawErr == nil && gErr == nil {
		rawBytes := rawDoc.Bytes()
		gBytes := gDoc.Bytes()
		if !bytes.Equal(rawBytes, gBytes) {
			t.Fatalf("document bytes differ for borderline input:\ndocformat.Parse: %q\nParseGenericSource: %q",
				rawBytes, gBytes)
		}
	}
}
