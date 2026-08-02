package tui

// render_test.go verifies the rendering distinctions that carry meaning:
//
//   T10.3  A report containing an absent category, an unpriced model, a
//          provisional run and an unattributable bucket renders each one
//          distinguishably from a zero/priced/final/named counterpart.
//
//   T10.4  The no-data and unusable-path states render without error.
//
// These tests assert that the symbolic constants differ from numeric defaults
// and that rendered strings contain the expected markers, so a collapse at
// the rendering layer is caught here.

import (
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// T10.3: Rendering distinctions
// ---------------------------------------------------------------------------

// TestFormatTokens_AbsentVsPresentZero verifies that an absent TokenCount
// renders as AbsentMarker and never as "0".
func TestFormatTokens_AbsentVsPresentZero(t *testing.T) {
	absent := FormatTokens(domain.AbsentTokens())
	presentZero := FormatTokens(domain.Tokens(0))
	presentNonZero := FormatTokens(domain.Tokens(1234))

	if absent == "0" {
		t.Errorf("AbsentTokens renders as %q — must not equal \"0\"", absent)
	}
	if absent != AbsentMarker {
		t.Errorf("AbsentTokens = %q, want %q", absent, AbsentMarker)
	}
	if presentZero != "0" {
		t.Errorf("Tokens(0) = %q, want \"0\"", presentZero)
	}
	if absent == presentZero {
		t.Errorf("absent %q == present-zero %q — must be distinguishable", absent, presentZero)
	}
	if presentNonZero != "1,234" {
		t.Errorf("Tokens(1234) = %q, want \"1,234\"", presentNonZero)
	}
}

// TestFormatMoney_NoDataVsKnownZeroVsUnpriced verifies the three MoneyValue
// states render distinctly and correctly.
func TestFormatMoney_NoDataVsKnownZeroVsUnpriced(t *testing.T) {
	noData := FormatMoney(domain.NoMoneyData())
	unpriced := FormatMoney(domain.UnpricedMoney())
	knownZero := FormatMoney(domain.KnownMoney(domain.Money(0)))
	knownOne := FormatMoney(domain.KnownMoney(domain.Money(1_000_000_000))) // $1.00

	// No-data must not equal "$0.00".
	if noData == "$0.00" {
		t.Errorf("NoMoneyData renders as %q — must not equal \"$0.00\"", noData)
	}
	if noData != AbsentMarker {
		t.Errorf("NoMoneyData = %q, want AbsentMarker %q", noData, AbsentMarker)
	}

	// Unpriced must not equal "$0.00".
	if unpriced == "$0.00" {
		t.Errorf("UnpricedMoney renders as %q — must not equal \"$0.00\"", unpriced)
	}
	if unpriced != UnpricedMarker {
		t.Errorf("UnpricedMoney = %q, want UnpricedMarker %q", unpriced, UnpricedMarker)
	}

	// KnownMoney(0) IS "$0.00" — it is priced but costs nothing.
	if !strings.HasPrefix(knownZero, "$") {
		t.Errorf("KnownMoney(0) = %q — should start with \"$\"", knownZero)
	}

	// All three must be distinguishable from each other.
	if noData == unpriced {
		t.Errorf("NoMoneyData %q == UnpricedMoney %q — must be distinguishable", noData, unpriced)
	}
	if noData == knownZero {
		t.Errorf("NoMoneyData %q == KnownMoney(0) %q — must be distinguishable", noData, knownZero)
	}
	if unpriced == knownZero {
		t.Errorf("UnpricedMoney %q == KnownMoney(0) %q — must be distinguishable", unpriced, knownZero)
	}

	// $1.00 renders with dollar sign and two decimal places.
	if !strings.HasPrefix(knownOne, "$") {
		t.Errorf("KnownMoney($1) = %q — should start with \"$\"", knownOne)
	}
}

// TestFormatTotal_PartialIndicator verifies that an incomplete CategoryMoney
// appends a visible indicator distinct from a complete total.
func TestFormatTotal_PartialIndicator(t *testing.T) {
	complete := domain.CategoryMoney{
		Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
		Complete: true,
	}
	partial := domain.CategoryMoney{
		Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
		Complete: false,
	}

	completeStr := FormatTotal(complete)
	partialStr := FormatTotal(partial)

	if completeStr == partialStr {
		t.Errorf("complete %q == partial %q — must be distinguishable", completeStr, partialStr)
	}
	if !strings.Contains(partialStr, "partial") {
		t.Errorf("partial total %q does not contain \"partial\"", partialStr)
	}
}

// TestRunsScreen_ProvisionalLabel verifies that provisional runs are labelled
// and distinguishable from final runs.
func TestRunsScreen_ProvisionalLabel(t *testing.T) {
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:         domain.NamedRun("20260101T120000Z-abcd"),
				Provisional: true,
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
					Money:  domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(1_000_000_000)), Complete: true},
				},
				Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
			},
			{
				Run:         domain.NamedRun("20260101T130000Z-bcde"),
				Provisional: false,
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(200)},
					Money:  domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(2_000_000_000)), Complete: true},
				},
				Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
			},
		},
		AllRuns:  domain.Totals{},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary(nil),
	}

	styles := plainStyles()
	s := screens.NewRunsScreen(report, 100, 30, styles)
	view := s.View(100, 30)

	if !strings.Contains(view, ProvisionalLabel) {
		t.Errorf("runs screen view does not contain ProvisionalLabel %q", ProvisionalLabel)
	}
	// The provisional run ID should appear labelled.
	if !strings.Contains(view, "20260101T120000Z-abcd") {
		t.Error("provisional run id not found in runs screen view")
	}
}

// TestRunsScreen_UnattributableSection verifies the unattributable bucket
// is shown in its own section and the label matches UnattributableLabel.
func TestRunsScreen_UnattributableSection(t *testing.T) {
	unattrib := &domain.RunReport{
		Run: domain.UnattributableRun(),
		Totals: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(50)},
			Money:  domain.CategoryMoney{Total: domain.UnpricedMoney()},
		},
		Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
	}
	report := domain.Report{
		Runs:           nil,
		Unattributable: unattrib,
		AllRuns:        domain.Totals{},
		Currency:       domain.Currency,
		Quality:        domain.NewQualitySummary(nil),
	}

	styles := plainStyles()
	s := screens.NewRunsScreen(report, 100, 30, styles)
	view := s.View(100, 30)

	if !strings.Contains(view, UnattributableLabel) {
		t.Errorf("runs screen view does not contain UnattributableLabel %q", UnattributableLabel)
	}
	// Unpriced money should show UnpricedMarker.
	if !strings.Contains(view, UnpricedMarker) {
		t.Errorf("runs screen view does not contain UnpricedMarker %q", UnpricedMarker)
	}
}

// TestRunsScreen_QualityFindingsVisible verifies data-quality findings appear
// in the run-list view when present.
func TestRunsScreen_QualityFindingsVisible(t *testing.T) {
	findings := []domain.Finding{
		{
			Kind:     domain.FindingMalformedLine,
			Severity: domain.SeverityWarning,
			Path:     "/logs/run/events.jsonl",
			Line:     42,
			Detail:   "unexpected token",
		},
	}
	report := domain.Report{
		Runs:     nil,
		AllRuns:  domain.Totals{},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary(findings),
	}

	styles := plainStyles()
	s := screens.NewRunsScreen(report, 100, 30, styles)
	view := s.View(100, 30)

	if !strings.Contains(view, "finding") {
		t.Errorf("runs screen view does not mention data-quality findings; got:\n%s", view)
	}
}

// TestRunsScreen_AbsentTokensShowMarker verifies absent token categories
// render as AbsentMarker and not as "0" in the run list.
func TestRunsScreen_AbsentTokensShowMarker(t *testing.T) {
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun("20260101T120000Z-abcd"),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{
						Input:         domain.Tokens(100),
						CacheRead:     domain.AbsentTokens(), // absent — no cache reads
						CacheCreation: domain.AbsentTokens(),
						Output:        domain.Tokens(50),
					},
					Money: domain.CategoryMoney{
						Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
						Complete: true,
					},
				},
				Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
			},
		},
		AllRuns:  domain.Totals{},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary(nil),
	}

	styles := plainStyles()
	s := screens.NewRunsScreen(report, 100, 30, styles)
	view := s.View(100, 30)

	if !strings.Contains(view, AbsentMarker) {
		t.Errorf("runs screen view does not contain AbsentMarker %q for absent token categories; got:\n%s",
			AbsentMarker, view)
	}
}

// ---------------------------------------------------------------------------
// T10.4: No-data and unusable-path states render without error
// ---------------------------------------------------------------------------

func TestNoDataScreen_RendersWithoutError(t *testing.T) {
	src := domain.Source{
		Kind: domain.SourceLogsRoot,
		Path: "/test/OrchestrationLogs",
	}
	styles := plainStyles()
	s := screens.NewNoDataScreen(src, 80, 24, styles)
	view := s.View(80, 24)

	if view == "" {
		t.Error("NoDataScreen.View() returned empty string")
	}
	if !strings.Contains(view, src.Path) {
		t.Errorf("no-data view does not contain source path %q; got:\n%s", src.Path, view)
	}
}

func TestNoDataScreen_NeverShowsErrorState(t *testing.T) {
	// A valid but empty source must never look like an error or show "0".
	src := domain.Source{
		Kind: domain.SourceLogsRoot,
		Path: "/valid/but/empty",
	}
	styles := plainStyles()
	s := screens.NewNoDataScreen(src, 80, 24, styles)
	view := s.View(80, 24)

	// The word "error" (case-insensitive) should not dominate the message.
	// The screen is a calm informational state, not an error state.
	if strings.Contains(strings.ToLower(view), "error") {
		t.Errorf("no-data view should not contain 'error'; got:\n%s", view)
	}
}

func TestSourceScreen_RendersWithoutError(t *testing.T) {
	styles := plainStyles()
	s := screens.NewSourceScreen(80, 24, styles)
	view := s.View(80, 24)

	if view == "" {
		t.Error("SourceScreen.View() returned empty string")
	}
}

// ---------------------------------------------------------------------------
// T10.3 continued: RunDetailScreen rendering
// ---------------------------------------------------------------------------

// TestRunDetailScreen_OrchestratorLineIsDistinctFromAgentLines verifies that
// the orchestrator has its own labelled section and appears separately from
// the per-agent breakdown, per the binding display requirement.
func TestRunDetailScreen_OrchestratorLineIsDistinctFromAgentLines(t *testing.T) {
	run := domain.RunReport{
		Run: domain.NamedRun("20260101T120000Z-abcd"),
		Totals: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(500)},
			Money:  domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(5_000_000_000)), Complete: true},
		},
		Orchestrator: domain.ActorReport{
			Actor: domain.Orchestrator(),
			Totals: domain.Totals{
				Tokens: domain.TokenUsage{Input: domain.Tokens(200)},
				Money:  domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(2_000_000_000)), Complete: true},
			},
		},
		Agents: []domain.ActorReport{
			{
				Actor: domain.AgentInstance("TestAgent#1"),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(300)},
					Money:  domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(3_000_000_000)), Complete: true},
				},
			},
		},
	}

	styles := plainStyles()
	s := screens.NewRunDetailScreen(run, 100, 30, styles)
	view := s.View(100, 30)

	// The orchestrator section header must appear.
	if !strings.Contains(view, "Orchestrator") {
		t.Errorf("run-detail view missing orchestrator section header; got:\n%s", view)
	}
	// The orchestrator actor label ("orchestrator") must appear as a detail line.
	if !strings.Contains(view, "orchestrator") {
		t.Errorf("run-detail view missing orchestrator actor label; got:\n%s", view)
	}
	// The agent label must appear separately under its own section.
	if !strings.Contains(view, "TestAgent#1") {
		t.Errorf("run-detail view missing agent label 'TestAgent#1'; got:\n%s", view)
	}
	// The agents section header must appear when agents are present.
	if !strings.Contains(view, "Agents") {
		t.Errorf("run-detail view missing 'Agents' section header; got:\n%s", view)
	}
}

// TestRunDetailScreen_PerAgentTotalsAppear verifies that each agent in run.Agents
// has its label visible in the rendered output.
func TestRunDetailScreen_PerAgentTotalsAppear(t *testing.T) {
	run := domain.RunReport{
		Run: domain.NamedRun("20260101T120000Z-abcd"),
		Totals: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(1000)},
			Money:  domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(10_000_000_000)), Complete: true},
		},
		Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
		Agents: []domain.ActorReport{
			{
				Actor: domain.AgentInstance("Alpha#1"),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(400)},
					Money:  domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(4_000_000_000)), Complete: true},
				},
			},
			{
				Actor: domain.AgentInstance("Beta#2"),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(600)},
					Money:  domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(6_000_000_000)), Complete: true},
				},
			},
		},
	}

	styles := plainStyles()
	s := screens.NewRunDetailScreen(run, 100, 30, styles)
	view := s.View(100, 30)

	if !strings.Contains(view, "Alpha#1") {
		t.Errorf("run-detail view missing agent label 'Alpha#1'; got:\n%s", view)
	}
	if !strings.Contains(view, "Beta#2") {
		t.Errorf("run-detail view missing agent label 'Beta#2'; got:\n%s", view)
	}
}

// TestRunDetailScreen_UnpricedModelsListed verifies that unpriced models for
// the run are listed when present, so pricing gaps are always visible.
func TestRunDetailScreen_UnpricedModelsListed(t *testing.T) {
	run := domain.RunReport{
		Run: domain.NamedRun("20260101T120000Z-abcd"),
		Totals: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
			Money:  domain.CategoryMoney{Total: domain.UnpricedMoney()},
		},
		Orchestrator:   domain.ActorReport{Actor: domain.Orchestrator()},
		UnpricedModels: []domain.ModelID{"claude-mystery-model"},
	}

	styles := plainStyles()
	s := screens.NewRunDetailScreen(run, 100, 30, styles)
	view := s.View(100, 30)

	if !strings.Contains(view, "claude-mystery-model") {
		t.Errorf("run-detail view does not list unpriced model 'claude-mystery-model'; got:\n%s", view)
	}
	// The label for the unpriced section must also be present.
	if !strings.Contains(view, "Unpriced") {
		t.Errorf("run-detail view missing 'Unpriced' label when unpriced models present; got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T10.3 continued: PricingScreen rendering
// ---------------------------------------------------------------------------

// TestPricingScreen_ShowsUnpricedModelList verifies that each unpriced model
// name appears in the pricing screen's rendered output.
func TestPricingScreen_ShowsUnpricedModelList(t *testing.T) {
	models := []domain.ModelID{"model-alpha", "model-beta"}
	styles := plainStyles()
	s := screens.NewPricingScreen(models, "/config/pricing.yaml", 100, 30, styles)
	view := s.View(100, 30)

	for _, m := range models {
		if !strings.Contains(view, string(m)) {
			t.Errorf("pricing screen view does not contain model %q; got:\n%s", m, view)
		}
	}
}

// TestPricingScreen_ShowsConfigFilePath verifies that the pricing store's file
// path is visible in the rendered output, satisfying the binding requirement
// that the written path is displayed to the user.
func TestPricingScreen_ShowsConfigFilePath(t *testing.T) {
	models := []domain.ModelID{"some-model"}
	configPath := "/home/user/.mosaic/MosaicLogAnalyzer/config/pricing.yaml"
	styles := plainStyles()
	s := screens.NewPricingScreen(models, configPath, 100, 30, styles)
	view := s.View(100, 30)

	if !strings.Contains(view, configPath) {
		t.Errorf("pricing screen view does not contain config path %q; got:\n%s", configPath, view)
	}
}

// TestPricingScreen_EmptyModelListRendersWithoutPanic verifies that the screen
// renders without error even when no unpriced models are provided.
func TestPricingScreen_EmptyModelListRendersWithoutPanic(t *testing.T) {
	styles := plainStyles()
	s := screens.NewPricingScreen(nil, "/config/pricing.yaml", 100, 30, styles)
	view := s.View(100, 30)

	if view == "" {
		t.Error("PricingScreen.View() returned empty string for nil model list")
	}
}

// ---------------------------------------------------------------------------
// Test helper: plainStyles returns a plain (no-colour) Styles for unit tests.
// ---------------------------------------------------------------------------

func plainStyles() screens.Styles {
	return screens.Styles{} // zero lipgloss.Style values are safe no-ops
}
