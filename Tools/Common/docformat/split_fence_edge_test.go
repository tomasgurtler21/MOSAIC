package docformat_test

// Edge-case tests for fence detection and encoding checks: BOM combined with fence
// variants, valid multi-byte UTF-8, invalid UTF-8 in CRLF input, content lines that
// resemble fences, and malformed fences that are followed by a valid one.

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
)

func TestSplitFrontmatter_BOMThenBlankLineThenFence_RejectedAtLineTwo(t *testing.T) {
	src := bomPrefix + "\n---\nname: demo\n---\nBody.\n"

	fe := requireFormatProblem(t, src, docformat.FormatProblemMalformedFence, 2)

	if !strings.Contains(fe.Excerpt, "---") {
		t.Errorf("excerpt = %q, want it to contain the fence", fe.Excerpt)
	}
}

func TestSplitFrontmatter_BOMThenBlankLineThenHeading_NoFrontmatterNoError(t *testing.T) {
	src := bomPrefix + "\n# Title\n"

	fm, _, err := docformat.SplitFrontmatter([]byte(src))
	doc, parseErr := docformat.Parse([]byte(src))

	if err != nil || parseErr != nil {
		t.Fatalf("unexpected errors: split=%v parse=%v", err, parseErr)
	}
	if fm != nil || doc.Frontmatter().Present() {
		t.Errorf("no frontmatter expected, got fm=%q", fm)
	}
	if !doc.HasBOM() {
		t.Error("HasBOM must be true")
	}
}

func TestBytes_BOMWithTrailingWhitespaceCRLFOpeningFence_EditedEntryUsesCRLF(t *testing.T) {
	doc, err := docformat.Parse([]byte(bomPrefix + "--- \r\nname: demo\r\n---\r\nBody.\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !doc.HasBOM() || !doc.Frontmatter().Present() {
		t.Fatalf("BOM and frontmatter must both be recognised (HasBOM=%v)", doc.HasBOM())
	}
	if err := doc.Frontmatter().Set("name", mosaic.ScalarValue("changed", mosaic.QuotePlain)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	want := bomPrefix + "---\r\nname: changed\r\n---\r\nBody.\r\n"
	if got := string(doc.Bytes()); got != want {
		t.Errorf("Bytes = %q, want %q", got, want)
	}
}

func TestParse_ValidMultiByteUTF8_AcceptedAndRoundTrips(t *testing.T) {
	cases := map[string]string{
		"accented":    "---\nname: caf\u00E9\n---\nBody \u00E9.\n",
		"CJK":         "---\nname: \u65E5\u672C\u8A9E\n---\n\u672C\u6587\n",
		"emoji":       "---\nname: demo\n---\nParty \U0001F389\n",
		"BOM and CJK": bomPrefix + "---\nname: \u65E5\u672C\n---\nBody\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			doc, err := docformat.Parse([]byte(src))
			if err != nil {
				t.Fatalf("valid UTF-8 must be accepted: %v", err)
			}
			if got := string(doc.Bytes()); got != src {
				t.Errorf("round-trip mismatch: %q", got)
			}
			if _, _, err := docformat.SplitFrontmatter([]byte(src)); err != nil {
				t.Errorf("SplitFrontmatter rejected valid UTF-8: %v", err)
			}
		})
	}
}

func TestParse_InvalidUTF8InCRLFInput_LineCountedByLF(t *testing.T) {
	src := "---\r\nname: demo\r\ndescription: caf\xE9\r\n---\r\nBody.\r\n"

	requireFormatProblem(t, src, docformat.FormatProblemInvalidUTF8, 3)
}

func TestParse_InvalidUTF8OnLineOne_ReportsLineOne(t *testing.T) {
	requireFormatProblem(t, "\xFF# Title\n", docformat.FormatProblemInvalidUTF8, 1)
}

func TestSplitFrontmatter_IndentedFenceLikeLinesInsideFrontmatter_AreContent(t *testing.T) {
	cases := map[string]struct{ src, fm string }{
		"tab-indented in block scalar": {"---\nname: |\n\t---\n---\nBody.\n", "name: |\n\t---\n"},
		"space-indented plain content": {"---\nname: demo\n  ---\n---\nBody.\n", "name: demo\n  ---\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fm, body, err := docformat.SplitFrontmatter([]byte(tc.src))

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(fm) != tc.fm || string(body) != "Body.\n" {
				t.Errorf("fm=%q body=%q, want fm=%q body=%q", fm, body, tc.fm, "Body.\n")
			}
		})
	}
}

func TestSplitFrontmatter_NearFenceLinesInsideFrontmatter_DoNotClose(t *testing.T) {
	src := "---\nname: demo\n----\n--- x\n---\nBody.\n"

	fm, body, err := docformat.SplitFrontmatter([]byte(src))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(fm) != "name: demo\n----\n--- x\n" || string(body) != "Body.\n" {
		t.Errorf("fm=%q body=%q", fm, body)
	}
}

func TestSplitFrontmatter_MalformedClosingBeforeValidClosing_ReportedAtFirstOffender(t *testing.T) {
	src := "---\nname: demo\n---\u200B\nmore: x\n---\nBody.\n"

	requireFormatProblem(t, src, docformat.FormatProblemMalformedFence, 3)
}

func TestSplitFrontmatter_EmptyBlockWithTrailingWhitespaceFences_Accepted(t *testing.T) {
	cases := map[string]string{
		"spaced opening": "--- \n---\nBody.\n",
		"spaced closing": "---\n--- \nBody.\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fm, body, err := docformat.SplitFrontmatter([]byte(src))

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(fm) != 0 || string(body) != "Body.\n" {
				t.Errorf("fm=%q body=%q", fm, body)
			}
		})
	}
}
