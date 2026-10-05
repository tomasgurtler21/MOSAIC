package docformat_test

// Tests for byte-exact round-trip of documents whose opening and closing fences use different
// line endings or whose closing fence ends the file without a terminator, and for the line
// ending of frontmatter synthesised for a document that had none.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
)

func plainValue(s string) mosaic.FieldValue {
	return mosaic.ScalarValue(s, mosaic.QuotePlain)
}

func mustParse(t *testing.T, src string) *docformat.Document {
	t.Helper()
	doc, err := docformat.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return doc
}

// fenceEndingFixtures are documents whose fences must be written back exactly as found.
var fenceEndingFixtures = map[string]string{
	"crlf opening, lf closing":           "---\r\nname: a\r\nversion: 1.0.0\r\n---\nBody line.\n",
	"lf opening, crlf closing":           "---\nname: a\nversion: 1.0.0\n---\r\nBody line.\r\n",
	"crlf opening, lf closing, no body":  "---\r\nname: a\r\n---\n",
	"lf opening, crlf closing, no body":  "---\nname: a\n---\r\n",
	"empty frontmatter, mixed fences":    "---\r\n---\nBody.\n",
	"lf closing fence at eof":            "---\nname: a\n---",
	"crlf opening, closing fence at eof": "---\r\nname: a\r\n---",
	"closing fence at eof, empty block":  "---\n---",
	"mixed, closing fence at eof, list":  "---\r\ntools:\r\n  - a\r\n  - b\r\n---",
}

func TestBytes_FenceEndingVariants_RoundTripByteIdentical(t *testing.T) {
	for name, src := range fenceEndingFixtures {
		t.Run(name, func(t *testing.T) {
			doc := mustParse(t, src)

			got := doc.Bytes()

			if !bytes.Equal(got, []byte(src)) {
				t.Errorf("Bytes() = %q, want %q", got, src)
			}
		})
	}
}

func TestClone_FenceEndingVariants_RoundTripByteIdentical(t *testing.T) {
	for name, src := range fenceEndingFixtures {
		t.Run(name, func(t *testing.T) {
			clone := mustParse(t, src).Clone()

			got := clone.Bytes()

			if !bytes.Equal(got, []byte(src)) {
				t.Errorf("clone Bytes() = %q, want %q", got, src)
			}
		})
	}
}

func TestBytes_FenceEndingVariants_EditKeepsOriginalFenceBytes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "crlf opening, lf closing",
			src:  "---\r\nname: a\r\nversion: 1.0.0\r\n---\nBody.\n",
			want: "---\r\nname: a\r\nversion: 2.0.0\r\n---\nBody.\n",
		},
		{
			name: "lf opening, crlf closing",
			src:  "---\nname: a\nversion: 1.0.0\n---\r\nBody.\r\n",
			want: "---\nname: a\nversion: 2.0.0\n---\r\nBody.\r\n",
		},
		{
			name: "crlf opening, closing fence at eof",
			src:  "---\r\nname: a\r\nversion: 1.0.0\r\n---",
			want: "---\r\nname: a\r\nversion: 2.0.0\r\n---",
		},
		{
			name: "lf opening, closing fence at eof",
			src:  "---\nname: a\nversion: 1.0.0\n---",
			want: "---\nname: a\nversion: 2.0.0\n---",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParse(t, tt.src)

			if err := doc.Frontmatter().Set("version", plainValue("2.0.0")); err != nil {
				t.Fatalf("Set: %v", err)
			}
			got := doc.Bytes()

			if string(got) != tt.want {
				t.Errorf("Bytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBytes_AddedKeyOnMixedFenceDocument_FollowsOpeningFenceEnding(t *testing.T) {
	doc := mustParse(t, "---\r\nname: a\r\n---\nBody.\n")

	if err := doc.Frontmatter().Set("extra", plainValue("x")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	want := "---\r\nname: a\r\nextra: x\r\n---\nBody.\n"
	if got := string(doc.Bytes()); got != want {
		t.Errorf("Bytes() = %q, want %q", got, want)
	}
}

func TestClone_EditedClone_KeepsFenceBytesAndLeavesOriginalUntouched(t *testing.T) {
	src := "---\r\nname: a\r\n---"
	doc := mustParse(t, src)
	clone := doc.Clone()

	if err := clone.Frontmatter().Set("name", plainValue("b")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got, want := string(clone.Bytes()), "---\r\nname: b\r\n---"; got != want {
		t.Errorf("clone Bytes() = %q, want %q", got, want)
	}
	if got := string(doc.Bytes()); got != src {
		t.Errorf("original Bytes() = %q, want %q", got, src)
	}
}

func TestBodyLineOffset_ClosingFenceAtEOF_CountsFenceAsOneLine(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"lf", "---\nname: a\nversion: 1.0.0\n---", 4},
		{"crlf", "---\r\nname: a\r\nversion: 1.0.0\r\n---", 4},
		{"mixed", "---\r\nname: a\r\n---", 3},
		{"empty block", "---\n---", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParse(t, tt.src)

			if got := doc.BodyLineOffset(); got != tt.want {
				t.Errorf("BodyLineOffset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBytes_SynthesisedFrontmatter_FollowsBodyLineEnding(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"crlf body", "# Title\r\n\r\nText.\r\n", "---\r\nname: x\r\n---\r\n# Title\r\n\r\nText.\r\n"},
		{"lf body", "# Title\n\nText.\n", "---\nname: x\n---\n# Title\n\nText.\n"},
		{"no terminator in body", "just text", "---\nname: x\n---\njust text"},
		{"empty body", "", "---\nname: x\n---\n"},
		{"first terminator crlf, later lf", "a\r\nb\nc\n", "---\r\nname: x\r\n---\r\na\r\nb\nc\n"},
		{"first terminator lf, later crlf", "a\nb\r\nc\r\n", "---\nname: x\n---\na\nb\r\nc\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParse(t, tt.body)

			if err := doc.Frontmatter().Set("name", plainValue("x")); err != nil {
				t.Fatalf("Set: %v", err)
			}
			got := doc.Bytes()

			if string(got) != tt.want {
				t.Errorf("Bytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBytes_SynthesisedFrontmatterOnCRLFBody_ReparsesToSameContent(t *testing.T) {
	doc := mustParse(t, "Body.\r\nMore.\r\n")
	if err := doc.Frontmatter().Set("name", plainValue("x")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	reparsed := mustParse(t, string(doc.Bytes()))

	v, ok := reparsed.Frontmatter().Get("name")
	if !ok || v.Scalar != "x" {
		t.Errorf("reparsed name = %+v (found=%v), want scalar x", v, ok)
	}
	if got := doc.BodyLineOffset(); got != 3 {
		t.Errorf("BodyLineOffset() = %d, want 3", got)
	}
}

func TestBytes_SynthesisedFrontmatterOnBOMCRLFBody_BOMFirstThenCRLFBlock(t *testing.T) {
	doc := mustParse(t, "\xEF\xBB\xBFBody.\r\nMore.\r\n")
	if err := doc.Frontmatter().Set("name", plainValue("x")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := doc.Bytes()

	want := "\xEF\xBB\xBF---\r\nname: x\r\n---\r\nBody.\r\nMore.\r\n"
	if string(got) != want {
		t.Errorf("Bytes() = %q, want %q", got, want)
	}
}

func TestBytes_SynthesisedFrontmatterOnBOMCRLFBody_StripBOMStartsWithCRLFBlock(t *testing.T) {
	doc := mustParse(t, "\xEF\xBB\xBFBody.\r\nMore.\r\n")
	if err := doc.Frontmatter().Set("name", plainValue("x")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	doc.StripBOM()

	got := doc.Bytes()

	want := "---\r\nname: x\r\n---\r\nBody.\r\nMore.\r\n"
	if string(got) != want {
		t.Errorf("Bytes() = %q, want %q", got, want)
	}
}

func TestBytes_SynthesisedFrontmatterOnBOMLFBody_UsesLF(t *testing.T) {
	doc := mustParse(t, "\xEF\xBB\xBFBody.\nMore.\n")
	if err := doc.Frontmatter().Set("name", plainValue("x")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := doc.Bytes()

	want := "\xEF\xBB\xBF---\nname: x\n---\nBody.\nMore.\n"
	if string(got) != want {
		t.Errorf("Bytes() = %q, want %q", got, want)
	}
}
