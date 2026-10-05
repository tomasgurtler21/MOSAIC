package docformat_test

// Tests for fence detection variants: accepted trailing-whitespace fences, rejected
// malformed fences (typed error with line and visible excerpt), indented content lines
// inside frontmatter, and unchanged behaviour for canonical and frontmatter-less input.

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
)

func TestSplitFrontmatter_TrailingWhitespaceFences_AcceptedSymmetrically(t *testing.T) {
	cases := map[string]string{
		"opening trailing space":  "--- \nname: demo\n---\nBody.\n",
		"opening trailing tab":    "---\t\nname: demo\n---\nBody.\n",
		"closing trailing space":  "---\nname: demo\n--- \nBody.\n",
		"closing trailing tab":    "---\nname: demo\n---\t \nBody.\n",
		"both, CRLF":              "--- \r\nname: demo\r\n---  \r\nBody.\r\n",
		"closing at EOF, spaced":  "---\nname: demo\n--- ",
		"closing at EOF, no term": "---\nname: demo\n---",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fm, _, err := docformat.SplitFrontmatter([]byte(src))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fm == nil || !strings.HasPrefix(string(fm), "name: demo") {
				t.Errorf("frontmatter must be found, got %q", fm)
			}
			doc, err := docformat.Parse([]byte(src))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if !doc.Frontmatter().Present() {
				t.Error("Present must be true")
			}
			if v, ok := doc.Frontmatter().Get("name"); !ok || v.Scalar != "demo" {
				t.Errorf("name = %+v ok=%v", v, ok)
			}
		})
	}
}

func TestSplitFrontmatter_TrailingWhitespaceClosingFence_BodyStartsAfterFenceLine(t *testing.T) {
	_, body, err := docformat.SplitFrontmatter([]byte("---\nname: demo\n--- \nBody.\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "Body.\n" {
		t.Errorf("body = %q, want %q", body, "Body.\n")
	}
}

func TestBytes_TrailingWhitespaceOpeningFence_EditedEntryUsesFenceLineEnding(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"CRLF", "--- \r\nname: demo\r\n---\r\nBody.\r\n", "---\r\nname: changed\r\n---\r\nBody.\r\n"},
		{"LF", "--- \nname: demo\n---\nBody.\n", "---\nname: changed\n---\nBody.\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := docformat.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err := doc.Frontmatter().Set("name", mosaic.ScalarValue("changed", mosaic.QuotePlain)); err != nil {
				t.Fatalf("Set: %v", err)
			}
			if got := string(doc.Bytes()); got != tc.want {
				t.Errorf("Bytes = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBytes_TrailingWhitespaceOpeningFence_UnmodifiedDropsWhitespaceKeepingTerminator(t *testing.T) {
	doc, err := docformat.Parse([]byte("--- \r\nname: demo\r\n---\r\nBody.\r\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "---\r\nname: demo\r\n---\r\nBody.\r\n"
	if got := string(doc.Bytes()); got != want {
		t.Errorf("Bytes = %q, want %q", got, want)
	}
}

func TestSplitFrontmatter_RejectedOpeningVariants_TypedErrorWithLineAndExcerpt(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		line    int
		excerpt string // substring expected in Excerpt
	}{
		{"leading blank line", "\n---\nname: demo\n---\nBody.\n", 2, "---"},
		{"leading whitespace-only line", "  \n---\nname: demo\n---\n", 2, "---"},
		{"leading spaces before fence", "  ---\nname: demo\n---\nBody.\n", 1, "  ---"},
		{"leading tab before fence", "\t---\nname: demo\n---\nBody.\n", 1, "<TAB>---"},
		{"zero-width space before fence", "\u200B---\nname: demo\n---\nBody.\n", 1, "<U+200B>---"},
		{"zero-width space after fence", "---\u200B\nname: demo\n---\nBody.\n", 1, "---<U+200B>"},
		{"non-leading BOM char after BOM", bomPrefix + "\uFEFF---\nname: demo\n---\n", 1, "<U+FEFF>---"},
		{"non-breaking space before fence", "\u00A0---\nname: demo\n---\n", 1, "<U+00A0>---"},
		{"word joiner before fence", "\u2060---\nname: demo\n---\n", 1, "<U+2060>---"},
		{"CRLF blank line then fence", "\r\n---\r\nname: demo\r\n---\r\n", 2, "---"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fe := requireFormatProblem(t, tc.src, docformat.FormatProblemMalformedFence, tc.line)
			if !strings.Contains(fe.Excerpt, tc.excerpt) {
				t.Errorf("excerpt = %q, want it to contain %q", fe.Excerpt, tc.excerpt)
			}
			requireASCII(t, fe.Excerpt)
		})
	}
}

func TestSplitFrontmatter_RejectedClosingVariant_MalformedNotUnclosed(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		line    int
		excerpt string
	}{
		{"zero-width space after closing", "---\nname: demo\n---\u200B\nBody.\n", 3, "---<U+200B>"},
		{"zero-width space before closing", "---\nname: demo\n\u200B---\nBody.\n", 3, "<U+200B>---"},
		{"non-breaking space before closing", "---\nname: demo\n\u00A0---\n", 3, "<U+00A0>---"},
		{"closing with CRLF and zero-width", "---\r\nname: demo\r\n---\u200B\r\nBody.\r\n", 3, "---<U+200B>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fe := requireFormatProblem(t, tc.src, docformat.FormatProblemMalformedFence, tc.line)
			if !strings.Contains(fe.Excerpt, tc.excerpt) {
				t.Errorf("excerpt = %q, want it to contain %q", fe.Excerpt, tc.excerpt)
			}
		})
	}
}

func TestSplitFrontmatter_ExcerptRendersControlCharactersVisibly(t *testing.T) {
	// The zero-width space makes the opening line fence-like but malformed. The CR of the
	// CRLF terminator is rendered as <CR>; the LF is never part of the excerpt.
	fe := requireFormatProblem(t, "\u200B---\r\nname: demo\r\n---\r\n", docformat.FormatProblemMalformedFence, 1)

	if strings.ContainsAny(fe.Excerpt, "\r\n\t") {
		t.Errorf("excerpt must not contain raw control characters: %q", fe.Excerpt)
	}
	if !strings.Contains(fe.Excerpt, "<CR>") {
		t.Errorf("excerpt must show the CR as <CR>: %q", fe.Excerpt)
	}
}

func TestSplitFrontmatter_LongOffendingLine_ExcerptIsBoundedAndMarkedTruncated(t *testing.T) {
	// Malformed closing line made long by trailing invisible characters. The full visible
	// rendering would be 3 + 100*len("<U+200B>") characters; the excerpt must be shorter,
	// still start with the fence, and end with "..." to mark the truncation.
	src := "---\nname: demo\n---" + strings.Repeat("\u200B", 100) + "\n"
	fullRendering := "---" + strings.Repeat("<U+200B>", 100)

	fe := requireFormatProblem(t, src, docformat.FormatProblemMalformedFence, 3)

	if len(fe.Excerpt) >= len(fullRendering) {
		t.Errorf("excerpt must be truncated, got %d chars (full rendering is %d)", len(fe.Excerpt), len(fullRendering))
	}
	if !strings.HasPrefix(fe.Excerpt, "---<U+200B>") {
		t.Errorf("excerpt must start with the offending line's beginning: %q", fe.Excerpt)
	}
	if !strings.HasSuffix(fe.Excerpt, "...") {
		t.Errorf("truncated excerpt must end with \"...\": %q", fe.Excerpt)
	}
}

func TestSplitFrontmatter_CROnlyOpeningFence_RejectedAtLineOne(t *testing.T) {
	cases := []string{
		"---\rname: demo\r---\rBody.\r",
		"--- \rname: demo\r",
		"---\r",
	}
	for _, src := range cases {
		t.Run(strings.ReplaceAll(src, "\r", "<CR>"), func(t *testing.T) {
			requireFormatProblem(t, src, docformat.FormatProblemCROnlyLineEndings, 1)
		})
	}
}

func TestSplitFrontmatter_UnclosedFrontmatter_TypedErrorAtOpeningFenceLine(t *testing.T) {
	cases := map[string]string{
		"missing closing":      "---\nname: demo\n\nNo closing delimiter.\n",
		"CRLF missing closing": "---\r\nname: demo\r\n",
		"only opening fence":   "---",
		"only spaced opening":  "---  ",
		"opening with newline": "---\n",
		"indented dashes only": "---\nname: |\n  ---\n",
		"BOM missing closing":  bomPrefix + "---\nname: demo\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			requireFormatProblem(t, src, docformat.FormatProblemUnclosedFrontmatter, 1)
		})
	}
}

func TestParse_IndentedDashesInsideBlockScalar_AreContentNotFence(t *testing.T) {
	src := "---\nname: demo\nnotes: |\n  ---\n  more text\n---\nBody.\n"

	doc, err := docformat.Parse([]byte(src))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !doc.Frontmatter().Present() {
		t.Fatal("frontmatter must be present")
	}
	if got := string(doc.Bytes()); got != src {
		t.Errorf("round-trip mismatch: %q", got)
	}
	_, body, err := docformat.SplitFrontmatter([]byte(src))
	if err != nil || string(body) != "Body.\n" {
		t.Errorf("SplitFrontmatter body = %q err=%v", body, err)
	}
}

func TestSplitFrontmatter_UnchangedResultsForCanonicalAndNonFrontmatterInput(t *testing.T) {
	t.Run("canonical LF", func(t *testing.T) {
		fm, body, err := docformat.SplitFrontmatter([]byte("---\na: 1\n---\nBody\n"))
		if err != nil || string(fm) != "a: 1\n" || string(body) != "Body\n" {
			t.Errorf("fm=%q body=%q err=%v", fm, body, err)
		}
	})
	t.Run("canonical CRLF", func(t *testing.T) {
		fm, body, err := docformat.SplitFrontmatter([]byte("---\r\na: 1\r\n---\r\nBody\r\n"))
		if err != nil || string(fm) != "a: 1\r\n" || string(body) != "Body\r\n" {
			t.Errorf("fm=%q body=%q err=%v", fm, body, err)
		}
	})
	t.Run("empty frontmatter block has length zero", func(t *testing.T) {
		fm, body, err := docformat.SplitFrontmatter([]byte("---\n---\nBody\n"))
		if err != nil || len(fm) != 0 || string(body) != "Body\n" {
			t.Errorf("fm=%q body=%q err=%v", fm, body, err)
		}
	})
	t.Run("first non-blank line is a heading, later rule", func(t *testing.T) {
		src := "# Heading\n\n---\n\nText\n"
		fm, body, err := docformat.SplitFrontmatter([]byte(src))
		if err != nil || fm != nil || string(body) != src {
			t.Errorf("fm=%q body=%q err=%v", fm, body, err)
		}
	})
	t.Run("blank lines then heading", func(t *testing.T) {
		src := "\n\n# Heading\n---\n"
		fm, body, err := docformat.SplitFrontmatter([]byte(src))
		if err != nil || fm != nil || string(body) != src {
			t.Errorf("fm=%q body=%q err=%v", fm, body, err)
		}
	})
	t.Run("four dashes is not a fence", func(t *testing.T) {
		src := "----\ntext\n"
		fm, body, err := docformat.SplitFrontmatter([]byte(src))
		if err != nil || fm != nil || string(body) != src {
			t.Errorf("fm=%q body=%q err=%v", fm, body, err)
		}
	})
}
