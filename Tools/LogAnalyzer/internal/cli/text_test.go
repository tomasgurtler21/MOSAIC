package cli_test

// Tests for the human-readable text encoder (EncodeText).
//
// Coverage:
//   - Output is non-empty for a non-empty report
//   - Per-run figures appear in the output
//   - Per-subagent (agent-instance) figures appear in the output
//   - Unpriced money is clearly marked (not rendered as "$0.00")
//   - Provisional runs are clearly labelled (not rendered as final)
//   - The unattributable bucket appears as a distinct section
//   - The all-runs total is present
//   - Data-quality findings are mentioned when present

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/cli"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Basic validity
// ---------------------------------------------------------------------------

func TestEncodeText_NonEmptyOutputForNonEmptyReport(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{Run: domain.NamedRun(cliTestRunID)},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}
	if buf.Len() == 0 {
		t.Errorf("EncodeText produced empty output for a non-empty report")
	}
}

func TestEncodeText_EmptyReportDoesNotPanic(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error for empty report: %v", err)
	}
	// No panic reached — test passes.
}

// ---------------------------------------------------------------------------
// Per-run figures
// ---------------------------------------------------------------------------

func TestEncodeText_ContainsRunID(t *testing.T) {
	// The run ID must appear in the text output so users can identify each run.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(1000)},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	if !strings.Contains(buf.String(), cliTestRunID) {
		t.Errorf("output does not contain run ID %q; output:\n%s", cliTestRunID, buf.String())
	}
}

func TestEncodeText_ContainsPerRunTokenFigures(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{
						Input:  domain.Tokens(12_345),
						Output: domain.Tokens(6_789),
					},
					Money: domain.CategoryMoney{Total: domain.NoMoneyData()},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()
	// The exact formatting is up to the implementation, but token figures must appear.
	if !strings.Contains(output, "12") || !strings.Contains(output, "345") {
		// "12,345" or "12345" should appear somewhere.
		if !strings.Contains(output, "12345") && !strings.Contains(output, "12,345") {
			t.Errorf("output does not contain input token figure 12345; output:\n%s", output)
		}
	}
}

// ---------------------------------------------------------------------------
// Per-subagent (agent-instance) figures
// ---------------------------------------------------------------------------

func TestEncodeText_ContainsPerSubagentFigures(t *testing.T) {
	const agentID = domain.AgentInstanceID("Implementer#3")

	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Agents: []domain.ActorReport{
					{
						Actor: domain.AgentInstance(agentID),
						Totals: domain.Totals{
							Tokens: domain.TokenUsage{Input: domain.Tokens(500)},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Implementer#3") {
		t.Errorf("output does not contain agent-instance ID %q; output:\n%s", string(agentID), output)
	}
}

// ---------------------------------------------------------------------------
// Unpriced money marker
// ---------------------------------------------------------------------------

func TestEncodeText_UnpricedMoneyMarkedDistinctlyFromDollarZero(t *testing.T) {
	// An unpriced money cell must not appear as "$0.00". The design specifies
	// UnpricedMarker = "unpriced" as the canonical representation.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(1000)},
					Money: domain.CategoryMoney{
						Input:    domain.UnpricedMoney(),
						Total:    domain.UnpricedMoney(),
						Complete: false,
					},
				},
				UnpricedModels: []domain.ModelID{"unknown-model"},
			},
		},
		UnpricedModels: []domain.ModelID{"unknown-model"},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()

	// The output must contain the unpriced marker (exact string may vary, but
	// must not be "$0.00" for the unpriced cell).
	if strings.Contains(output, "$0.00") {
		t.Errorf("output contains %q for an unpriced money cell; unpriced must be rendered distinctly", "$0.00")
	}
}

func TestEncodeText_UnpricedMarkerPresent(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Money: domain.CategoryMoney{
						Total:    domain.UnpricedMoney(),
						Complete: false,
					},
				},
				UnpricedModels: []domain.ModelID{"model-x"},
			},
		},
		UnpricedModels: []domain.ModelID{"model-x"},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()
	// "unpriced" (the canonical UnpricedMarker from the design) must appear somewhere.
	if !strings.Contains(strings.ToLower(output), "unpriced") {
		t.Errorf("output does not contain 'unpriced' marker for an unpriced money cell; output:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Provisional run label
// ---------------------------------------------------------------------------

func TestEncodeText_ProvisionalRunClearlyLabelled(t *testing.T) {
	// A provisional run (no run_end/session_end observed) must be labelled to
	// indicate its cost is not final. The design specifies ProvisionalLabel =
	// "in progress" as the canonical label.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run:         domain.NamedRun(cliTestRunID),
				Provisional: true,
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()
	// The provisional label (or equivalent) must appear somewhere in the output.
	if !strings.Contains(strings.ToLower(output), "progress") &&
		!strings.Contains(strings.ToLower(output), "provisional") {
		t.Errorf("output does not label the provisional run; output:\n%s", output)
	}
}

func TestEncodeText_CompletedRunNotLabelledAsProvisional(t *testing.T) {
	// A non-provisional run must not carry a provisional label.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run:         domain.NamedRun(cliTestRunID),
				Provisional: false,
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()
	if strings.Contains(strings.ToLower(output), "in progress") {
		t.Errorf("completed run is incorrectly labelled as 'in progress'; output:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Unattributable section
// ---------------------------------------------------------------------------

func TestEncodeText_UnattributableSectionPresent(t *testing.T) {
	// When the report contains an unattributable bucket, it must appear as a
	// distinct section (not folded into the named-run list or the all-runs total).
	unattributableRun := &domain.RunReport{
		Run: domain.UnattributableRun(),
		Totals: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(200)},
		},
	}
	report := domain.Report{
		Source:         domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency:       domain.Currency,
		Unattributable: unattributableRun,
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()
	// The design specifies UnattributableLabel = "Unattributable".
	if !strings.Contains(strings.ToLower(output), "unattributable") {
		t.Errorf("output does not contain an unattributable section; output:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// All-runs total
// ---------------------------------------------------------------------------

func TestEncodeText_AllRunsTotalPresent(t *testing.T) {
	// The all-runs total must appear somewhere in the output. Named runs only;
	// the unattributable bucket is excluded.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		AllRuns: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(9_000)},
			Money: domain.CategoryMoney{
				Total:    domain.KnownMoney(domain.Money(27_000_000_000)), // $27
				Complete: true,
			},
		},
		Runs: []domain.RunReport{
			{Run: domain.NamedRun(cliTestRunID)},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()
	// "total" or "all" (case-insensitive) should appear for the all-runs summary.
	if !strings.Contains(strings.ToLower(output), "total") &&
		!strings.Contains(strings.ToLower(output), "all") {
		t.Errorf("output does not appear to contain an all-runs total section; output:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Data-quality findings
// ---------------------------------------------------------------------------

func TestEncodeText_DataQualityFindingsMentioned(t *testing.T) {
	// When the report has quality findings, they must be visible in the text output.
	finding := domain.Finding{
		Kind:     domain.FindingMalformedLine,
		Severity: domain.SeverityWarning,
		Path:     "/logs/run/events.jsonl",
		Line:     7,
		Run:      domain.NamedRun(cliTestRunID),
		Detail:   "unexpected token",
	}
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary([]domain.Finding{finding}),
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(strings.ToLower(output), "malformed") &&
		!strings.Contains(output, "finding") &&
		!strings.Contains(output, "quality") {
		t.Errorf("output does not mention data-quality findings; output:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Shared token-category label vocabulary
// ---------------------------------------------------------------------------

// TestEncodeText_TokenLabelsUseSharedVocabulary verifies the CLI text encoder
// uses the same four shared category labels as the TUI, so the two presentations
// cannot drift apart. The expected labels are the canonical plain-English strings
// specified in the design: "input", "cache read", "cache write", "output".
func TestEncodeText_TokenLabelsUseSharedVocabulary(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{
						Input:         domain.Tokens(100),
						CacheRead:     domain.Tokens(200),
						CacheCreation: domain.Tokens(300),
						Output:        domain.Tokens(400),
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}
	output := strings.ToLower(buf.String())

	// All four shared labels must appear somewhere in the output.
	for _, label := range []string{"input", "cache read", "cache write", "output"} {
		if !strings.Contains(output, label) {
			t.Errorf("EncodeText output does not contain shared label %q; output:\n%s", label, buf.String())
		}
	}
}

// TestEncodeText_TokenLabelsDoNotUseLegacyNames verifies the CLI no longer emits
// the old camel-case identifiers "CacheRead" or "CacheCreation" as category
// labels, since those expose internal domain names rather than human-readable text.
func TestEncodeText_TokenLabelsDoNotUseLegacyNames(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{
						Input:         domain.Tokens(10),
						CacheRead:     domain.Tokens(20),
						CacheCreation: domain.Tokens(30),
						Output:        domain.Tokens(40),
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}
	output := buf.String()

	for _, legacy := range []string{"CacheRead", "CacheCreation"} {
		if strings.Contains(output, legacy) {
			t.Errorf("EncodeText output contains legacy label %q; must use shared domain labels; output:\n%s",
				legacy, output)
		}
	}
}

// TestEncodeText_CacheReadAndCacheWriteLabelsDistinguishable verifies the two
// cache categories appear as distinct labels in the output, so a reader can
// tell cache-read tokens apart from cache-write (creation) tokens.
func TestEncodeText_CacheReadAndCacheWriteLabelsDistinguishable(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{
						CacheRead:     domain.Tokens(111),
						CacheCreation: domain.Tokens(222),
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeText(&buf, report); err != nil {
		t.Fatalf("EncodeText returned error: %v", err)
	}
	output := strings.ToLower(buf.String())

	if !strings.Contains(output, "cache read") {
		t.Errorf("EncodeText output does not contain 'cache read' label; output:\n%s", buf.String())
	}
	if !strings.Contains(output, "cache write") {
		t.Errorf("EncodeText output does not contain 'cache write' label; output:\n%s", buf.String())
	}
}
