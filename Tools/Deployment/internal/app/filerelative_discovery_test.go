package app

// filerelative_discovery_test.go tests that the harness-only eligibility discovery
// reason carries file-relative line numbers when a blocking structural issue is found
// in a document that has frontmatter.
//
// Coverage:
//   - An ineligible document with frontmatter produces a discovery reason whose
//     embedded line number is file-relative, not body-relative. The user-facing
//     string "body has blocking structural issue (<code>): <message>" inherits the
//     file-relative number from Issue.Message, which is produced file-relative by
//     docformat.Validate after the Stage 2 fix.
//   - The file-relative line number scales correctly with frontmatter block height:
//     a taller frontmatter block produces a proportionally larger file line for the
//     same body error position.

import (
	"strings"
	"testing"
)

// TestEligibleHarnessOnly_WithFrontmatter_DiscoveryReasonLineIsFileRelative verifies that
// when a document with frontmatter fails eligibility due to a blocking structural issue,
// the discovery reason's embedded line number matches the file-relative position of the
// problem — the line the user would see when opening the file — rather than the
// body-relative position that the pre-fix implementation reported.
//
// Before the fix: docformat.Validate numbers lines from the start of the body, so a
// structural error on the first body line is reported as "line 1" in Issue.Message.
// The discovery reason then reads "…line 1…", but the user's editor shows that line as
// line 4 (three frontmatter lines precede it).
//
// After the fix: docformat.Validate adds BodyLineOffset() to every positional issue, so
// the same error is reported as "line 4". The discovery reason reads "…line 4…", matching
// what the user sees when they open the file.
func TestEligibleHarnessOnly_WithFrontmatter_DiscoveryReasonLineIsFileRelative(t *testing.T) {
	// Document structure:
	//   file line 1: ---
	//   file line 2: transform_version: "1.0.0"
	//   file line 3: ---
	//   file line 4: <Identity type="core">   ← opened here; never closed (body line 1)
	//   file line 5: Content with no closing tag.
	//
	// The blocking "unbalanced-tag" issue reports the opening tag's line. Before the fix
	// that line is body-relative 1. After the fix it is file-relative 4.
	// BodyLineOffset() = 3 (two delimiter lines + one entry line).
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content with no closing tag.\n")

	verdict := eligibleHarnessOnly(src)

	if verdict.Eligible {
		t.Fatal("expected Eligible false for a document with an unclosed section tag; got Eligible true")
	}
	if verdict.Reason == "" {
		t.Fatal("ineligible verdict must carry a non-empty Reason")
	}

	// After the fix: the reason must contain "line 4" (file-relative), not "line 1" (body-relative).
	// The reason format is: "body has blocking structural issue (unbalanced-tag): boundary tag
	// \"Identity\" opened at line <N> is never closed"
	// where <N> must be 4 (body line 1 + frontmatter offset 3).
	const wantLinePhrase = "line 4"
	if !strings.Contains(verdict.Reason, wantLinePhrase) {
		t.Errorf("discovery reason = %q;\n"+
			"want it to contain %q (file-relative line, frontmatter offset 3, body line 1 → file line 4);\n"+
			"the reason currently embeds the body-relative line number — "+
			"this test is RED until docformat.Validate applies BodyLineOffset() (I2.1 + I2.2)",
			verdict.Reason, wantLinePhrase)
	}
}

// TestEligibleHarnessOnly_WithDeepFrontmatter_DiscoveryReasonLineIsFileRelative verifies
// that the file-relative line number scales correctly with frontmatter block height. A
// larger offset shifts the reported line proportionally: body line 1 in a document with a
// 6-line frontmatter block becomes file line 7.
func TestEligibleHarnessOnly_WithDeepFrontmatter_DiscoveryReasonLineIsFileRelative(t *testing.T) {
	// Document structure:
	//   file line 1: ---
	//   file line 2: transform_version: "2.0.0"
	//   file line 3: version: "1.0.0"
	//   file line 4: id: "7"
	//   file line 5: role: "subagent"
	//   file line 6: ---
	//   file line 7: <Identity type="core">   ← opened here; never closed (body line 1)
	//   file line 8: Content with no closing tag.
	//
	// BodyLineOffset() = 6 (two delimiter lines + four entry lines).
	// Body line 1 → file line 7.
	src := []byte("---\n" +
		"transform_version: \"2.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"7\"\n" +
		"role: \"subagent\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content with no closing tag.\n")

	verdict := eligibleHarnessOnly(src)

	if verdict.Eligible {
		t.Fatal("expected Eligible false for a document with an unclosed section tag; got Eligible true")
	}
	if verdict.Reason == "" {
		t.Fatal("ineligible verdict must carry a non-empty Reason")
	}

	// After the fix: offset = 6 (two delimiter lines + four entries), body line 1 → file line 7.
	const wantLinePhrase = "line 7"
	if !strings.Contains(verdict.Reason, wantLinePhrase) {
		t.Errorf("discovery reason = %q;\n"+
			"want it to contain %q (file-relative line, frontmatter offset 6, body line 1 → file line 7);\n"+
			"this test is RED until docformat.Validate applies BodyLineOffset() (I2.1 + I2.2)",
			verdict.Reason, wantLinePhrase)
	}
}

// TestEligibleHarnessOnly_WithFrontmatter_DiscoveryReasonContainsBlockingCode verifies that
// the discovery reason correctly identifies which blocking code caused the ineligibility,
// regardless of line number format. This is a structural sanity check that complements the
// file-relative line tests above.
func TestEligibleHarnessOnly_WithFrontmatter_DiscoveryReasonContainsBlockingCode(t *testing.T) {
	// Unclosed section tag → "unbalanced-tag" blocking issue.
	src := []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content with no closing tag.\n")

	verdict := eligibleHarnessOnly(src)

	if verdict.Eligible {
		t.Fatal("expected Eligible false for a document with an unclosed section tag")
	}

	if !strings.Contains(verdict.Reason, "unbalanced-tag") {
		t.Errorf("discovery reason = %q; want it to contain \"unbalanced-tag\" "+
			"(the blocking validation code produced by an unclosed section tag)",
			verdict.Reason)
	}
}
