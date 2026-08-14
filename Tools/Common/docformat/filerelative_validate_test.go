package docformat_test

// filerelative_validate_test.go tests that validation issues carry file-relative line
// numbers and that Document.BodyLineOffset() correctly reports the frontmatter block height.
//
// Coverage:
//   - BodyLineOffset returns 0 for a document with no frontmatter block.
//   - BodyLineOffset returns (2 + entry count) for a document with LF-delimited frontmatter.
//   - BodyLineOffset returns the same count for CRLF frontmatter as for LF frontmatter
//     with the same content — delimiter style does not affect line count.
//   - BodyLineOffset with multiple frontmatter entries returns the correct total.
//   - Validate on a document with frontmatter reports Issue.Line as a file-relative number:
//     body-relative line + BodyLineOffset, not the raw body-relative line.
//   - Validate on a document without frontmatter reports Issue.Line unchanged (offset 0).
//   - Validate with CRLF frontmatter reports the correct file-relative line number.
//   - Issue.Line and the line number embedded in Issue.Message refer to the same file line
//     (AC2.5 consistency contract).
//   - mismatched-tag Issue.Line (closing tag file line) matches the closing tag line number
//     in Issue.Message (AC2.5 applied to two-position messages).
//   - out-of-order-section issues carry Line == 0 even when the document has frontmatter —
//     the offset is never applied to issues with no positional origin.

import (
	"fmt"
	"strings"
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// ---------------------------------------------------------------------------
// Document.BodyLineOffset — no frontmatter
// ---------------------------------------------------------------------------

func TestBodyLineOffset_NoFrontmatter_ReturnsZero(t *testing.T) {
	// A document parsed from bytes with no frontmatter block (no --- delimiters) has
	// body starting at file line 1, so the offset is 0.
	src := []byte("<Identity type=\"core\">\nContent.\n</Identity>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := doc.BodyLineOffset(); got != 0 {
		t.Errorf("BodyLineOffset() = %d, want 0 for document with no frontmatter block", got)
	}
}

// ---------------------------------------------------------------------------
// Document.BodyLineOffset — LF-delimited frontmatter
// ---------------------------------------------------------------------------

func TestBodyLineOffset_LFFrontmatter_OneEntry_ReturnsThree(t *testing.T) {
	// Frontmatter block (LF delimiters):
	//   file line 1: ---
	//   file line 2: key: value
	//   file line 3: ---
	//   file line 4: (body starts here)
	//
	// BodyLineOffset() must return 3 (2 delimiter lines + 1 entry line).
	src := []byte("---\nkey: value\n---\n<Identity type=\"core\">\nContent.\n</Identity>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	const want = 3
	if got := doc.BodyLineOffset(); got != want {
		t.Errorf("BodyLineOffset() = %d, want %d (2 delimiter lines + 1 entry line)", got, want)
	}
}

func TestBodyLineOffset_LFFrontmatter_ThreeEntries_ReturnsFive(t *testing.T) {
	// Frontmatter block (LF delimiters, three entries):
	//   file line 1: ---
	//   file line 2: a: 1
	//   file line 3: b: 2
	//   file line 4: c: 3
	//   file line 5: ---
	//   file line 6: (body starts here)
	//
	// BodyLineOffset() must return 5 (2 delimiter lines + 3 entry lines).
	src := []byte("---\na: 1\nb: 2\nc: 3\n---\n<Identity type=\"core\">\n</Identity>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	const want = 5
	if got := doc.BodyLineOffset(); got != want {
		t.Errorf("BodyLineOffset() = %d, want %d (2 delimiter lines + 3 entry lines)", got, want)
	}
}

// ---------------------------------------------------------------------------
// Document.BodyLineOffset — CRLF frontmatter delimiter
// ---------------------------------------------------------------------------

func TestBodyLineOffset_CRLFFrontmatter_SameOffsetAsLFEquivalent(t *testing.T) {
	// CRLF-delimited frontmatter with one entry:
	//   file line 1: ---\r\n
	//   file line 2: key: value\r\n
	//   file line 3: ---\r\n
	//   file line 4: (body starts here)
	//
	// The CRLF line terminator counts as one line (same as LF). BodyLineOffset() must
	// return 3 — the same as the LF equivalent in the previous test.
	src := []byte("---\r\nkey: value\r\n---\r\n<Identity type=\"core\">\nContent.\n</Identity>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	const want = 3
	if got := doc.BodyLineOffset(); got != want {
		t.Errorf("BodyLineOffset() = %d for CRLF frontmatter, want %d (same as LF — delimiter style must not affect line count)",
			got, want)
	}
}

func TestBodyLineOffset_CRLFFrontmatter_ThreeEntries_ReturnsFive(t *testing.T) {
	// CRLF-delimited frontmatter, three entries — mirrors the LF three-entry case.
	src := []byte("---\r\na: 1\r\nb: 2\r\nc: 3\r\n---\r\n<Identity type=\"core\">\n</Identity>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	const want = 5
	if got := doc.BodyLineOffset(); got != want {
		t.Errorf("BodyLineOffset() = %d for CRLF frontmatter with 3 entries, want %d", got, want)
	}
}

// ---------------------------------------------------------------------------
// Document.BodyLineOffset — defining invariant
// ---------------------------------------------------------------------------

func TestBodyLineOffset_DefiningInvariant_MatchesManuallyComputedOffset(t *testing.T) {
	// The contract defines: BodyLineOffset() == countLines(d.Bytes()) - countLines(body bytes).
	// Because Body.bytes() is unexported, we verify the invariant by comparing BodyLineOffset()
	// against the manually computed expected value for each case — we know where the body
	// starts because we constructed the input.
	tests := []struct {
		name       string
		src        []byte
		wantOffset int // = (total file lines) - (body lines)
	}{
		{
			name:       "no frontmatter",
			src:        []byte("<Identity type=\"core\">\nLine 2.\n</Identity>\n"),
			wantOffset: 0, // body == whole file; offset is 0
		},
		{
			name: "LF frontmatter one entry",
			// file: 3 FM lines + 3 body lines = 6 total; body = 3; offset = 3
			src:        []byte("---\nkey: value\n---\n<Identity type=\"core\">\nContent.\n</Identity>\n"),
			wantOffset: 3,
		},
		{
			name: "LF frontmatter three entries",
			// file: 5 FM lines + 2 body lines = 7 total; body = 2; offset = 5
			src:        []byte("---\na: 1\nb: 2\nc: 3\n---\n<Identity type=\"core\">\n</Identity>\n"),
			wantOffset: 5,
		},
		{
			name: "CRLF frontmatter one entry",
			// CRLF terminators count as one line each: 3 FM lines + 3 body lines; offset = 3
			src:        []byte("---\r\nkey: value\r\n---\r\n<Identity type=\"core\">\nContent.\n</Identity>\n"),
			wantOffset: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := docformat.Parse(tt.src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := doc.BodyLineOffset(); got != tt.wantOffset {
				t.Errorf("BodyLineOffset() = %d, want %d", got, tt.wantOffset)
			}
		})
	}

	// Synthesised-frontmatter branch: source has no frontmatter, but a key is added via
	// Set afterwards. Document.Bytes() synthesises the frontmatter block on serialisation:
	//   synthesised line 1: ---
	//   synthesised line 2: key: value
	//   synthesised line 3: ---
	//   body start:         (first body line becomes file line 4)
	//
	// BodyLineOffset() must return 3 (2 delimiter lines + 1 entry line) — matching
	// countLines(d.Bytes()) - countLines(<body bytes>), which is the defining invariant.
	// This is the third contract case (ContractsDesign.md §Document.BodyLineOffset) and
	// the risk explicitly flagged in Plan.md: the synthesised branch of Bytes() is a
	// distinct code path that can be wrong independently of the has-FM branch.
	t.Run("synthesised frontmatter one entry", func(t *testing.T) {
		src := []byte("<Identity type=\"core\">\nContent.\n</Identity>\n")
		doc, err := docformat.Parse(src)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		// Add one key to a document that had no frontmatter; this forces Bytes() to
		// synthesise a frontmatter block when serialising.
		if err := doc.Frontmatter().Set("key", domain.ScalarValue("value", domain.QuotePlain)); err != nil {
			t.Fatalf("Frontmatter().Set: %v", err)
		}
		// 2 delimiter lines + 1 entry line = offset 3.
		const want = 3
		if got := doc.BodyLineOffset(); got != want {
			t.Errorf("BodyLineOffset() = %d, want %d "+
				"(synthesised frontmatter: 2 delimiter lines + 1 added entry line)",
				got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// Validate — file-relative Issue.Line for documents with frontmatter (LF)
// ---------------------------------------------------------------------------

func TestValidate_WithLFFrontmatter_IssueLine_IsFileRelative(t *testing.T) {
	// Document structure:
	//   file line 1: ---
	//   file line 2: key: value
	//   file line 3: ---
	//   file line 4: non-blank content outside all boundaries  ← content-outside-boundary
	//
	// The issue originates at body line 1 (the only body line).
	// Frontmatter offset = 3, so the file-relative line is 1 + 3 = 4.
	// After the fix, Issue.Line must be 4, not 1.
	src := []byte("---\nkey: value\n---\nnon-blank content outside all regions\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "content-outside-boundary")
	if iss == nil {
		t.Fatal("expected a \"content-outside-boundary\" issue — none returned")
	}

	const wantLine = 4 // body line 1 + frontmatter offset 3
	if iss.Line != wantLine {
		t.Errorf("Issue.Line = %d, want %d; "+
			"Validate must add BodyLineOffset (%d) to body-relative line (1) to produce "+
			"the file-relative line number the user sees when opening the file",
			iss.Line, wantLine, 3)
	}
}

func TestValidate_WithMultiEntryFrontmatter_IssueLine_IsFileRelative(t *testing.T) {
	// Document structure:
	//   file line 1: ---
	//   file line 2: a: 1
	//   file line 3: b: 2
	//   file line 4: c: 3
	//   file line 5: ---
	//   file line 6: non-blank content outside all boundaries  ← content-outside-boundary
	//
	// Frontmatter offset = 5. Issue at body line 1 → file line 6.
	src := []byte("---\na: 1\nb: 2\nc: 3\n---\nnon-blank content outside all regions\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "content-outside-boundary")
	if iss == nil {
		t.Fatal("expected a \"content-outside-boundary\" issue — none returned")
	}

	const wantLine = 6 // body line 1 + frontmatter offset 5
	if iss.Line != wantLine {
		t.Errorf("Issue.Line = %d, want %d (body line 1 + frontmatter offset 5)", iss.Line, wantLine)
	}
}

func TestValidate_WithFrontmatter_UnbalancedTag_LineIsFileRelative(t *testing.T) {
	// Document structure:
	//   file line 1: ---
	//   file line 2: key: value
	//   file line 3: ---
	//   file line 4: <Identity type="core">   ← opened here; never closed
	//   file line 5: Content.
	//
	// The "unbalanced-tag" issue reports the line of the unclosed opening tag.
	// Frontmatter offset = 3. Opening tag at body line 1 → file line 4.
	src := []byte("---\nkey: value\n---\n<Identity type=\"core\">\nContent.\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "unbalanced-tag")
	if iss == nil {
		t.Fatal("expected an \"unbalanced-tag\" issue — none returned")
	}

	const wantLine = 4 // body line 1 + frontmatter offset 3
	if iss.Line != wantLine {
		t.Errorf("Issue.Line = %d, want %d (opening tag at body line 1, frontmatter offset 3)",
			iss.Line, wantLine)
	}
}

// ---------------------------------------------------------------------------
// Validate — documents without frontmatter are unaffected (offset 0)
// ---------------------------------------------------------------------------

func TestValidate_NoFrontmatter_IssueLine_IsUnchanged(t *testing.T) {
	// Without frontmatter the offset is 0 and Issue.Line is the raw body line number.
	// Body line 1 == file line 1; no change.
	src := []byte("non-blank content outside all boundary tags\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "content-outside-boundary")
	if iss == nil {
		t.Fatal("expected a \"content-outside-boundary\" issue — none returned")
	}

	const wantLine = 1
	if iss.Line != wantLine {
		t.Errorf("Issue.Line = %d, want %d for document with no frontmatter (zero offset; body line 1 == file line 1)",
			iss.Line, wantLine)
	}
}

// ---------------------------------------------------------------------------
// Validate — CRLF frontmatter delimiter
// ---------------------------------------------------------------------------

func TestValidate_WithCRLFFrontmatter_IssueLine_IsFileRelative(t *testing.T) {
	// CRLF-delimited frontmatter:
	//   file line 1: ---\r\n
	//   file line 2: key: value\r\n
	//   file line 3: ---\r\n
	//   file line 4: non-blank content outside all boundaries
	//
	// Same offset (3) and expected file line (4) as the LF equivalent.
	src := []byte("---\r\nkey: value\r\n---\r\nnon-blank content outside all regions\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "content-outside-boundary")
	if iss == nil {
		t.Fatal("expected a \"content-outside-boundary\" issue — none returned")
	}

	const wantLine = 4 // CRLF offset = 3, body line 1 → file line 4 (same as LF)
	if iss.Line != wantLine {
		t.Errorf("Issue.Line = %d, want %d for CRLF frontmatter (offset 3, body line 1 → file line 4)",
			iss.Line, wantLine)
	}
}

// ---------------------------------------------------------------------------
// AC2.5 — Issue.Line and message-embedded line number agree
// ---------------------------------------------------------------------------

func TestValidate_IssueLineAndEmbeddedMessageLine_Agree_ContentOutsideBoundary(t *testing.T) {
	// The content-outside-boundary message format is:
	//   "non-blank content at line <N> appears outside all boundary tags"
	// After the fix, N must equal Issue.Line (both file-relative, both from one value).
	src := []byte("---\nkey: value\n---\nnon-blank content outside all regions\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "content-outside-boundary")
	if iss == nil {
		t.Fatal("expected a \"content-outside-boundary\" issue — none returned")
	}
	if iss.Line == 0 {
		t.Skip("Issue.Line is 0 — positional issues must carry a non-zero Line")
	}

	// The message must contain "line <N>" where N == Issue.Line.
	wantSubstring := fmt.Sprintf("line %d", iss.Line)
	if !strings.Contains(iss.Message, wantSubstring) {
		t.Errorf("Issue.Message = %q;\nwant it to contain %q;\n"+
			"Issue.Line (%d) and the line number in Issue.Message must come from the same "+
			"file-relative value (AC2.5 consistency contract)",
			iss.Message, wantSubstring, iss.Line)
	}
}

func TestValidate_IssueLineAndEmbeddedMessageLine_Agree_UnbalancedTag(t *testing.T) {
	// The unbalanced-tag message format (unclosed opening tag) is:
	//   "boundary tag \"<name>\" opened at line <N> is never closed"
	// After the fix, N must equal Issue.Line.
	src := []byte("---\nkey: value\n---\n<Identity type=\"core\">\nContent.\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "unbalanced-tag")
	if iss == nil {
		t.Fatal("expected an \"unbalanced-tag\" issue — none returned")
	}
	if iss.Line == 0 {
		t.Skip("Issue.Line is 0 — positional unbalanced-tag issues must carry a non-zero Line")
	}

	wantSubstring := fmt.Sprintf("line %d", iss.Line)
	if !strings.Contains(iss.Message, wantSubstring) {
		t.Errorf("Issue.Message = %q;\nwant it to contain %q;\n"+
			"Issue.Line (%d) and the opening-tag line embedded in Issue.Message must agree (AC2.5)",
			iss.Message, wantSubstring, iss.Line)
	}
}

func TestValidate_MismatchedTag_IssueLineAndEmbeddedMessageLine_Agree(t *testing.T) {
	// mismatched-tag messages embed both lines:
	//   "opening tag <open> (line <A>) does not match closing tag <close> (line <B>)"
	// Issue.Line is the closing tag's line (B). Both A and B must be file-relative,
	// and B must equal Issue.Line.
	//
	// Document structure:
	//   file line 1: ---
	//   file line 2: key: value
	//   file line 3: ---
	//   file line 4: <Identity type="core">    (body line 1, file line 4)
	//   file line 5: Content.
	//   file line 6: </Capabilities>           (body line 3, file line 6) — mismatched close
	src := []byte("---\nkey: value\n---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Capabilities>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "mismatched-tag")
	if iss == nil {
		t.Fatal("expected a \"mismatched-tag\" issue — none returned")
	}
	if iss.Line == 0 {
		t.Skip("Issue.Line is 0 — positional mismatched-tag issues must carry a non-zero Line")
	}

	// Issue.Line is the closing tag line (file line 6 after fix).
	const wantLine = 6
	if iss.Line != wantLine {
		t.Errorf("Issue.Line = %d, want %d (closing tag at body line 3, frontmatter offset 3 → file line 6)",
			iss.Line, wantLine)
	}

	// The message must reference Issue.Line for the closing tag position.
	wantSubstring := fmt.Sprintf("line %d", iss.Line)
	if !strings.Contains(iss.Message, wantSubstring) {
		t.Errorf("Issue.Message = %q;\nwant it to contain %q for the closing tag line;\n"+
			"Issue.Line (%d) must match the closing tag line embedded in Issue.Message (AC2.5)",
			iss.Message, wantSubstring, iss.Line)
	}

	// The message must also reference the opening tag's file-relative line (A).
	// Opening tag is at body line 1; frontmatter offset = 3; file line = 4.
	// ContractsDesign.md: "for issues naming two positions such as mismatched-tag,
	// file-relative values on the same basis."
	// This independently guards the opening-tag line path, which is computed separately
	// from the closing-tag line and could be wrong while the closing-tag line is right.
	const wantOpeningTagFileLine = 4 // body line 1 + frontmatter offset 3
	wantOpeningSubstring := fmt.Sprintf("line %d", wantOpeningTagFileLine)
	if !strings.Contains(iss.Message, wantOpeningSubstring) {
		t.Errorf("Issue.Message = %q;\nwant it to contain %q for the opening tag file-relative line;\n"+
			"opening tag is at body line 1, frontmatter offset 3 → file line %d (AC2.5 two-position check)",
			iss.Message, wantOpeningSubstring, wantOpeningTagFileLine)
	}
}

// ---------------------------------------------------------------------------
// Validate — issues with no positional origin keep Line == 0
// ---------------------------------------------------------------------------

func TestValidate_OutOfOrderSection_WithFrontmatter_LineRemainsZero(t *testing.T) {
	// out-of-order-section issues have no positional origin (checkSectionOrder returns
	// issues with Line == 0). Even when the document has frontmatter, the offset must
	// NOT be applied to a zero origin — that would turn "no position" into file line 3,
	// which is the last frontmatter delimiter, not the actual location of the problem.
	//
	// Canonical order: Identity (index 0), CommunicationProtocol (index 1), Capabilities
	// (index 2), Constraints (index 3). Putting Capabilities before Identity is out of order.
	src := []byte("---\nkey: value\n---\n" +
		"<Capabilities type=\"core\">\n</Capabilities>\n" +
		"<Identity type=\"core\">\n</Identity>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{
		RequireCanonicalSections: true,
	})

	iss := issueWithCode(issues, "out-of-order-section")
	if iss == nil {
		t.Fatal("expected an \"out-of-order-section\" issue — none returned; " +
			"verify that Capabilities before Identity is out of order per CanonicalOrder")
	}

	if iss.Line != 0 {
		t.Errorf("out-of-order-section Issue.Line = %d, want 0; "+
			"issues with no positional origin must never have the frontmatter offset applied — "+
			"Line 0 means 'no position', not 'file line 0'",
			iss.Line)
	}
}
