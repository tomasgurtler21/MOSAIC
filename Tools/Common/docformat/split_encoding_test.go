package docformat_test

// Tests for encoding handling at the parse entry point: UTF-8 BOM recognition and
// preservation, UTF-16 BOM rejection and invalid UTF-8 rejection.
// Fixtures are built in Go code because .gitattributes forces LF on *.md files.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
)

func TestParse_BOMWithFrontmatter_ReadsFieldsAndReportsBOM(t *testing.T) {
	cases := map[string]string{
		"LF":   bomPrefix + "---\nname: demo\nversion: 1.0.0\n---\nBody.\n",
		"CRLF": bomPrefix + "---\r\nname: demo\r\nversion: 1.0.0\r\n---\r\nBody.\r\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			doc, err := docformat.Parse([]byte(src))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !doc.Frontmatter().Present() {
				t.Fatal("frontmatter must be recognised behind a BOM")
			}
			if v, ok := doc.Frontmatter().Get("name"); !ok || v.Scalar != "demo" {
				t.Errorf("name = %+v ok=%v, want demo", v, ok)
			}
			if v, ok := doc.Frontmatter().Get("version"); !ok || v.Scalar != "1.0.0" {
				t.Errorf("version = %+v ok=%v, want 1.0.0", v, ok)
			}
			if !doc.HasBOM() {
				t.Error("HasBOM must be true for a BOM'd input")
			}
			if bytes.Count(doc.Bytes(), []byte(bomPrefix)) != 1 {
				t.Error("the BOM must be emitted exactly once (not duplicated into the body)")
			}
		})
	}
}

func TestSplitFrontmatter_BOMWithFrontmatter_SplitsWithoutBOM(t *testing.T) {
	src := []byte(bomPrefix + "---\nname: demo\n---\nBody.\n")

	fm, body, err := docformat.SplitFrontmatter(src)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(fm) != "name: demo\n" {
		t.Errorf("frontmatter = %q, want %q", fm, "name: demo\n")
	}
	if string(body) != "Body.\n" {
		t.Errorf("body = %q, want %q", body, "Body.\n")
	}
}

func TestSplitFrontmatter_BOMWithoutFrontmatter_BodyIsInputAsGiven(t *testing.T) {
	src := []byte(bomPrefix + "# Title\n\nText.\n")

	fm, body, err := docformat.SplitFrontmatter(src)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter, got %q", fm)
	}
	if !bytes.Equal(body, src) {
		t.Errorf("body must be src as given (BOM included): got %q", body)
	}
}

func TestParse_BOMRoundTrip_UnmodifiedIsByteIdentical(t *testing.T) {
	cases := map[string]string{
		"LF frontmatter":   bomPrefix + "---\nname: demo\n---\nBody.\n",
		"CRLF frontmatter": bomPrefix + "---\r\nname: demo\r\n---\r\nBody.\r\n",
		"no frontmatter":   bomPrefix + "# Title\n\nText.\n",
		"BOM only":         bomPrefix,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			doc, err := docformat.Parse([]byte(src))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !doc.HasBOM() {
				t.Error("HasBOM must be true")
			}
			if got := doc.Bytes(); string(got) != src {
				t.Errorf("round-trip mismatch\ngot:  %q\nwant: %q", got, src)
			}
		})
	}
}

func TestParse_NoBOM_HasBOMIsFalse(t *testing.T) {
	doc, err := docformat.Parse([]byte("---\nname: demo\n---\nBody.\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.HasBOM() {
		t.Error("HasBOM must be false when the input has no BOM")
	}
}

func TestStripBOM_BOMWithFrontmatter_BytesEqualsBOMLessForm(t *testing.T) {
	plain := "---\nname: demo\n---\nBody.\n"
	doc, err := docformat.Parse([]byte(bomPrefix + plain))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc.StripBOM()

	if doc.HasBOM() {
		t.Error("HasBOM must be false after StripBOM")
	}
	if got := doc.Bytes(); string(got) != plain {
		t.Errorf("Bytes after StripBOM = %q, want %q", got, plain)
	}
	if v, ok := doc.Frontmatter().Get("name"); !ok || v.Scalar != "demo" {
		t.Errorf("frontmatter must be unchanged by StripBOM, got %+v ok=%v", v, ok)
	}
}

func TestStripBOM_NoBOM_IsNoOp(t *testing.T) {
	src := "---\nname: demo\n---\nBody.\n"
	doc, _ := docformat.Parse([]byte(src))

	doc.StripBOM()

	if string(doc.Bytes()) != src {
		t.Errorf("StripBOM on a BOM-less document changed the bytes: %q", doc.Bytes())
	}
}

func TestSet_BOMWithoutFrontmatter_PlacesBOMBeforeSynthesisedBlock(t *testing.T) {
	doc, err := docformat.Parse([]byte(bomPrefix + "# Title\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := doc.Frontmatter().Set("name", mosaic.ScalarValue("demo", mosaic.QuotePlain)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := string(doc.Bytes())

	want := bomPrefix + "---\nname: demo\n---\n# Title\n"
	if got != want {
		t.Errorf("Bytes = %q, want %q", got, want)
	}
	reparsed, err := docformat.Parse([]byte(got))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if v, ok := reparsed.Frontmatter().Get("name"); !ok || v.Scalar != "demo" {
		t.Errorf("re-parsed name = %+v ok=%v", v, ok)
	}
	if _, body, err := docformat.SplitFrontmatter([]byte(got)); err != nil || string(body) != "# Title\n" {
		t.Errorf("re-split body = %q err=%v", body, err)
	}
}

func TestSet_BOMWithoutFrontmatter_BOMTurnedOffStartsWithBlock(t *testing.T) {
	doc, _ := docformat.Parse([]byte(bomPrefix + "# Title\n"))
	_ = doc.Frontmatter().Set("name", mosaic.ScalarValue("demo", mosaic.QuotePlain))

	doc.StripBOM()

	want := "---\nname: demo\n---\n# Title\n"
	if got := string(doc.Bytes()); got != want {
		t.Errorf("Bytes = %q, want %q", got, want)
	}
}

func TestClone_BOMState_SurvivesAndIsIndependent(t *testing.T) {
	doc, _ := docformat.Parse([]byte(bomPrefix + "---\nname: demo\n---\nBody.\n"))

	clone := doc.Clone()

	if !clone.HasBOM() {
		t.Error("clone must carry the BOM state")
	}
	if !bytes.Equal(clone.Bytes(), doc.Bytes()) {
		t.Errorf("clone bytes differ: %q vs %q", clone.Bytes(), doc.Bytes())
	}
	clone.StripBOM()
	if !doc.HasBOM() {
		t.Error("stripping the clone's BOM must not affect the original")
	}

	// A stripped document clones as stripped.
	stripped := doc.Clone()
	stripped.StripBOM()
	if stripped.Clone().HasBOM() {
		t.Error("a clone of a BOM-less document must not have a BOM")
	}
}

func TestBodyLineOffset_BOMDoesNotChangeOffset(t *testing.T) {
	plain := "---\nname: demo\nversion: 1.0.0\n---\nBody.\n"
	withBOM, err := docformat.Parse([]byte(bomPrefix + plain))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	without, _ := docformat.Parse([]byte(plain))

	if withBOM.BodyLineOffset() != 4 || without.BodyLineOffset() != 4 {
		t.Errorf("offsets = %d (BOM) and %d (plain), want 4 for both",
			withBOM.BodyLineOffset(), without.BodyLineOffset())
	}
}

func TestBytes_BOMCRLFDocument_DirtyEntryWrittenWithCRLF(t *testing.T) {
	doc, err := docformat.Parse([]byte(bomPrefix + "---\r\nname: demo\r\n---\r\nBody.\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := doc.Frontmatter().Set("name", mosaic.ScalarValue("changed", mosaic.QuotePlain)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	want := bomPrefix + "---\r\nname: changed\r\n---\r\nBody.\r\n"
	if got := string(doc.Bytes()); got != want {
		t.Errorf("Bytes = %q, want %q", got, want)
	}
}

func TestSplitFrontmatter_UTF16BOM_RejectedWithUTF16Problem(t *testing.T) {
	cases := map[string]string{
		"little endian": "\xFF\xFE-\x00-\x00-\x00\n\x00",
		"big endian":    "\xFE\xFF\x00-\x00-\x00-\x00\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fe := requireFormatProblem(t, src, docformat.FormatProblemUTF16, 1)
			if fe.Excerpt != "" {
				t.Errorf("UTF-16 error excerpt must be empty, got %q", fe.Excerpt)
			}
		})
	}
}

func TestParse_InvalidUTF8InFrontmatterValue_RejectedAtItsLine(t *testing.T) {
	// 0xE9 is Latin-1 e-acute; as a lone byte it is invalid UTF-8.
	src := "---\nname: demo\ndescription: caf\xE9\n---\nBody.\n"

	fe := requireFormatProblem(t, src, docformat.FormatProblemInvalidUTF8, 3)

	if !strings.Contains(fe.Excerpt, "<0xE9>") {
		t.Errorf("excerpt must show the invalid byte as <0xE9>, got %q", fe.Excerpt)
	}
}

func TestParse_InvalidUTF8InBody_RejectedAtItsLine(t *testing.T) {
	src := "---\nname: demo\n---\nline one\ncaf\xE9 au lait\n"

	fe := requireFormatProblem(t, src, docformat.FormatProblemInvalidUTF8, 5)

	if !strings.Contains(fe.Excerpt, "<0xE9>") {
		t.Errorf("excerpt must show the invalid byte as <0xE9>, got %q", fe.Excerpt)
	}
}

func TestParse_InvalidUTF8WithoutFrontmatter_StillRejected(t *testing.T) {
	requireFormatProblem(t, "# Title\n\xFF bad\n", docformat.FormatProblemInvalidUTF8, 2)
}

func TestParse_InvalidUTF8AfterBOM_LineCountsBOMOnLineOne(t *testing.T) {
	requireFormatProblem(t, bomPrefix+"---\nname: a\xE9\n---\n", docformat.FormatProblemInvalidUTF8, 2)
}

func TestFormatError_Message_ContainsLineAndExcerptInASCII(t *testing.T) {
	_, _, err := docformat.SplitFrontmatter([]byte("---\ndescription: caf\xE9\n---\n"))
	fe := requireFormatError(t, err)

	msg := fe.Error()

	requireASCII(t, msg)
	if !strings.Contains(msg, "2") {
		t.Errorf("message must contain the line number 2: %q", msg)
	}
	if fe.Excerpt == "" || !strings.Contains(msg, fe.Excerpt) {
		t.Errorf("message must contain the excerpt %q: %q", fe.Excerpt, msg)
	}
}
