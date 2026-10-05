package transform_test

// source_bom_test.go covers transform output for a source that starts with a UTF-8 BOM:
// the output equals the output for the same source without the BOM, for LF and CRLF sources,
// in the create case (no deployed input) and the update case (deployed input present).
// Placeholders must be resolved and version stamps present in both forms.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
)

type bomCase struct {
	name string
	src  []byte
}

func bomCases() []bomCase {
	return []bomCase{
		{"LF", []byte(fullSourceLF)},
		{"CRLF", crlfBytes(fullSourceLF)},
	}
}

// TestApply_BOMSource_Create_OutputEqualsBOMlessOutput verifies that a BOM'd source produces
// byte-identical output to the same source without the BOM when nothing is deployed yet.
func TestApply_BOMSource_Create_OutputEqualsBOMlessOutput(t *testing.T) {
	for _, tc := range bomCases() {
		t.Run(tc.name, func(t *testing.T) {
			want := mustApply(t, fidelityRequest(t, tc.src, nil))
			got := mustApply(t, fidelityRequest(t, withBOM(tc.src), nil))

			if bytes.HasPrefix(got, []byte(utf8BOM)) {
				t.Error("output for a BOM'd source starts with a BOM; the output must be BOM-less")
			}
			if !bytes.Equal(got, want) {
				t.Errorf("BOM'd source output differs from BOM-less source output\nwith BOM (first 300 bytes): %q\nwithout BOM (first 300 bytes): %q",
					truncateBytes(got, 300), truncateBytes(want, 300))
			}
		})
	}
}

// TestApply_BOMSource_Update_OutputEqualsBOMlessOutput verifies the same equality when a
// deployed file is present, so the update path is exercised.
func TestApply_BOMSource_Update_OutputEqualsBOMlessOutput(t *testing.T) {
	for _, tc := range bomCases() {
		t.Run(tc.name, func(t *testing.T) {
			deployed := filledDeployed(t, "User wrote this context.\nSecond line.\n", "\n")
			want := mustApply(t, fidelityRequest(t, tc.src, deployed))
			got := mustApply(t, fidelityRequest(t, withBOM(tc.src), deployed))

			if bytes.HasPrefix(got, []byte(utf8BOM)) {
				t.Error("output for a BOM'd source starts with a BOM; the output must be BOM-less")
			}
			if !bytes.Equal(got, want) {
				t.Errorf("BOM'd source update output differs from BOM-less source update output\nwith BOM (first 300 bytes): %q\nwithout BOM (first 300 bytes): %q",
					truncateBytes(got, 300), truncateBytes(want, 300))
			}
		})
	}
}

// TestApply_BOMSource_ResolvesPlaceholdersAndStampsVersions verifies that a BOM'd source gets
// its model placeholder resolved, its source version carried and the harness version stamps
// written, and that the frontmatter of the output is recognised without a BOM.
func TestApply_BOMSource_ResolvesPlaceholdersAndStampsVersions(t *testing.T) {
	for _, tc := range bomCases() {
		t.Run(tc.name, func(t *testing.T) {
			out := mustApply(t, fidelityRequest(t, withBOM(tc.src), nil))

			if bytes.Contains(out, []byte("{model-identifier}")) {
				t.Error("model placeholder {model-identifier} was not resolved in the output")
			}
			doc, err := docformat.Parse(out)
			if err != nil {
				t.Fatalf("parse output: %v", err)
			}
			if doc.HasBOM() {
				t.Error("parsed output reports a BOM")
			}
			fm := doc.Frontmatter()
			if v, ok := fm.Get("mosaic_version"); !ok || v.Scalar != "2.4.1" {
				t.Errorf("output version = %+v (present=%v), want the source version 2.4.1", v, ok)
			}
			if v, ok := fm.Get("model"); !ok || v.Scalar != "claude/claude-sonnet" {
				t.Errorf("output model = %+v (present=%v), want claude/claude-sonnet", v, ok)
			}
			if v, ok := fm.Get("mosaic_harness_version"); !ok || v.Scalar == "" {
				t.Errorf("output lacks the mosaic_harness_version stamp (present=%v)", ok)
			}
		})
	}
}

// TestApply_BOMSource_NoFrontmatter_OutputEqualsBOMlessOutput covers the synthesised
// frontmatter path: a BOM'd source without frontmatter must also yield BOM-less output
// equal to the BOM-less source's output.
func TestApply_BOMSource_NoFrontmatter_OutputEqualsBOMlessOutput(t *testing.T) {
	src := []byte(noFrontmatterSourceLF)
	want := mustApply(t, fidelityRequest(t, src, nil))
	got := mustApply(t, fidelityRequest(t, withBOM(src), nil))

	if bytes.HasPrefix(got, []byte(utf8BOM)) {
		t.Error("output starts with a BOM; the output must be BOM-less")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("BOM'd no-frontmatter source output differs from BOM-less output\ngot:  %q\nwant: %q", got, want)
	}
}
