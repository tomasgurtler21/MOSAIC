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

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-log-analyzer/internal/app"
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

// TestPricingScreen_ShowsAllModelsInReport verifies that every model name
// (priced and unpriced alike) appears in the pricing screen's rendered output.
// The reworked screen lists ALL report models, not only the unpriced ones.
func TestPricingScreen_ShowsAllModelsInReport(t *testing.T) {
	models := []app.ModelPricingStatus{
		{Model: "model-alpha", Priced: false},
		{Model: "model-beta", Priced: true, Pricing: domain.ModelPricing{Model: "model-beta"}},
	}
	styles := plainStyles()
	s := screens.NewPricingScreen(models, "/config/pricing.yaml", 100, 30, styles)
	view := s.View(100, 30)

	for _, m := range models {
		if !strings.Contains(view, string(m.Model)) {
			t.Errorf("pricing screen view does not contain model %q; got:\n%s", m.Model, view)
		}
	}
}

// TestPricingScreen_ShowsConfigFilePath verifies that the pricing store's file
// path is visible in the rendered output, satisfying the binding requirement
// that the written path is displayed to the user.
func TestPricingScreen_ShowsConfigFilePath(t *testing.T) {
	models := []app.ModelPricingStatus{{Model: "some-model", Priced: false}}
	configPath := "/home/user/.mosaic/MosaicLogAnalyzer/config/pricing.yaml"
	styles := plainStyles()
	s := screens.NewPricingScreen(models, configPath, 100, 30, styles)
	view := s.View(100, 30)

	if !strings.Contains(view, configPath) {
		t.Errorf("pricing screen view does not contain config path %q; got:\n%s", configPath, view)
	}
}

// TestPricingScreen_EmptyModelListRendersWithoutPanic verifies that the screen
// renders without error even when no models are provided (nil slice).
func TestPricingScreen_EmptyModelListRendersWithoutPanic(t *testing.T) {
	styles := plainStyles()
	s := screens.NewPricingScreen(nil, "/config/pricing.yaml", 100, 30, styles)
	view := s.View(100, 30)

	if view == "" {
		t.Error("PricingScreen.View() returned empty string for nil model list")
	}
}

// TestPricingScreen_DistinguishesPricedFromUnpriced verifies that the rendered
// output lets a reader distinguish a priced entry from an unpriced one. The
// design requires "a reader must be able to tell them apart from the rendered
// text alone". This test uses neutral model names ("alpha", "beta") that do
// not themselves contain pricing-related words, so the distinction must come
// from the pricing state rendered by the screen.
//
// With the stub View (shows model name only, no priced/unpriced indicator),
// the unpriced model's entry will have no no-price marker, so this test FAILS
// (RED phase).
func TestPricingScreen_DistinguishesPricedFromUnpriced(t *testing.T) {
	models := []app.ModelPricingStatus{
		{Model: "alpha", Priced: true, Pricing: domain.ModelPricing{Model: "alpha"}},
		{Model: "beta", Priced: false},
	}
	styles := plainStyles()
	s := screens.NewPricingScreen(models, "/config/pricing.yaml", 100, 30, styles)
	view := s.View(100, 30)

	lines := strings.Split(view, "\n")

	// Locate the line(s) that contain each model name.
	pricedLine, unpricedLine := "", ""
	for _, line := range lines {
		if strings.Contains(line, "alpha") {
			pricedLine = line
		}
		if strings.Contains(line, "beta") {
			unpricedLine = line
		}
	}
	if pricedLine == "" {
		t.Fatalf("could not find a line containing the priced model 'alpha' in view:\n%s", view)
	}
	if unpricedLine == "" {
		t.Fatalf("could not find a line containing the unpriced model 'beta' in view:\n%s", view)
	}

	// The unpriced entry must show a visible indicator of missing pricing
	// (e.g. AbsentMarker, the word "unpriced", or "no price").
	unpricedIndicator := strings.Contains(unpricedLine, screens.AbsentMarker) ||
		strings.Contains(unpricedLine, "unpriced") ||
		strings.Contains(unpricedLine, "no price")
	if !unpricedIndicator {
		t.Errorf("unpriced model entry does not show a no-price indicator; "+
			"line: %q\nExpect one of: %q, %q, %q",
			unpricedLine, screens.AbsentMarker, "unpriced", "no price")
	}
}

// TestPricingScreen_CursorMovement_SelectsCorrectModel verifies that the cursor
// starts at the first model, moves down on 'j'/down and up on 'k'/up, and that
// SelectedModel() returns the model at the cursor after Enter is pressed.
//
// With the stub (cursor logic present but selection not wired to Done/SelectedModel
// correctly at runtime), pressing Enter should set Done()=true and SelectedModel()
// should return the model at the cursor.
func TestPricingScreen_CursorMovement_SelectsCorrectModel(t *testing.T) {
	models := []app.ModelPricingStatus{
		{Model: "model-first", Priced: true},
		{Model: "model-second", Priced: false},
		{Model: "model-third", Priced: true},
	}
	styles := plainStyles()
	s := screens.NewPricingScreen(models, "/config/pricing.yaml", 100, 30, styles)

	// Move cursor down twice to land on "model-third".
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	// Verify the rendered view highlights "model-third" (cursor at index 2).
	view := s.View(100, 30)
	if !strings.Contains(view, "model-third") {
		t.Errorf("after moving cursor to index 2, view does not contain 'model-third'; got:\n%s", view)
	}

	// Press Enter to confirm selection.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Error("after Enter, Done() must be true")
	}
	if s.SelectedModel() != "model-third" {
		t.Errorf("SelectedModel() = %q, want %q", s.SelectedModel(), "model-third")
	}
}

// ---------------------------------------------------------------------------
// FormatTotalsLine: full-word category label tests
// ---------------------------------------------------------------------------

// TestFormatTotalsLine_UsesFullWordCategoryLabels verifies the compact summary
// line names each token category in plain English rather than short codes like
// "in:", "cr:", "cw:", "out:".
func TestFormatTotalsLine_UsesFullWordCategoryLabels(t *testing.T) {
	totals := domain.Totals{
		Tokens: domain.TokenUsage{
			Input:         domain.Tokens(12_345),
			CacheRead:     domain.Tokens(6_789),
			CacheCreation: domain.Tokens(1_234),
			Output:        domain.Tokens(567),
		},
		Money: domain.CategoryMoney{
			Total:    domain.KnownMoney(domain.Money(1_230_000_000)),
			Complete: true,
		},
	}
	line := screens.FormatTotalsLine(totals)

	// All four full-word labels (with colon separator) must appear.
	for _, want := range []string{"input:", "cache read:", "cache write:", "output:"} {
		if !strings.Contains(line, want) {
			t.Errorf("FormatTotalsLine does not contain %q; got %q", want, line)
		}
	}
}

// TestFormatTotalsLine_DoesNotUseAbbreviatedLabels verifies the old two-letter
// codes "cr:" and "cw:" no longer appear in the rendered line.
func TestFormatTotalsLine_DoesNotUseAbbreviatedLabels(t *testing.T) {
	totals := domain.Totals{
		Tokens: domain.TokenUsage{
			Input:         domain.Tokens(1),
			CacheRead:     domain.Tokens(2),
			CacheCreation: domain.Tokens(3),
			Output:        domain.Tokens(4),
		},
		Money: domain.CategoryMoney{Total: domain.NoMoneyData()},
	}
	line := screens.FormatTotalsLine(totals)

	for _, old := range []string{"in:", "cr:", "cw:", "out:"} {
		if strings.Contains(line, old) {
			t.Errorf("FormatTotalsLine still contains old abbreviated label %q; got %q", old, line)
		}
	}
}

// TestFormatTotalsLine_AbsentCategoryRendersAbsentMarker verifies absent token
// categories still render as AbsentMarker and not as "0" after the label change.
func TestFormatTotalsLine_AbsentCategoryRendersAbsentMarker(t *testing.T) {
	totals := domain.Totals{
		Tokens: domain.TokenUsage{
			Input:         domain.Tokens(100),
			CacheRead:     domain.AbsentTokens(),
			CacheCreation: domain.AbsentTokens(),
			Output:        domain.Tokens(50),
		},
		Money: domain.CategoryMoney{Total: domain.NoMoneyData()},
	}
	line := screens.FormatTotalsLine(totals)

	if !strings.Contains(line, screens.AbsentMarker) {
		t.Errorf("FormatTotalsLine did not render AbsentMarker %q for absent categories; got %q",
			screens.AbsentMarker, line)
	}
}

// TestFormatTotalsLine_PresentZeroRendersAsZero verifies that a present-zero
// token count renders as "0" and does not trigger the AbsentMarker in the token
// fields. Money is set to UnpricedMoney (renders as "unpriced", not AbsentMarker)
// so the only AbsentMarker that could appear would be from absent token categories.
func TestFormatTotalsLine_PresentZeroRendersAsZero(t *testing.T) {
	totals := domain.Totals{
		Tokens: domain.TokenUsage{
			Input:         domain.Tokens(0),
			CacheRead:     domain.Tokens(0),
			CacheCreation: domain.Tokens(0),
			Output:        domain.Tokens(0),
		},
		Money: domain.CategoryMoney{Total: domain.UnpricedMoney()},
	}
	line := screens.FormatTotalsLine(totals)

	if !strings.Contains(line, "0") {
		t.Errorf("FormatTotalsLine did not render '0' for present-zero categories; got %q", line)
	}
	// With all four categories present-zero and money unpriced (not NoData),
	// AbsentMarker must not appear anywhere in the line.
	if strings.Contains(line, screens.AbsentMarker) {
		t.Errorf("FormatTotalsLine rendered AbsentMarker for a present-zero token category; got %q", line)
	}
}

// TestFormatTotalsLine_IsSingleLine verifies the rendered output contains no
// embedded newlines — it is a compact single-line summary.
func TestFormatTotalsLine_IsSingleLine(t *testing.T) {
	totals := domain.Totals{
		Tokens: domain.TokenUsage{Input: domain.Tokens(1)},
		Money:  domain.CategoryMoney{Total: domain.NoMoneyData()},
	}
	line := screens.FormatTotalsLine(totals)

	if strings.Contains(line, "\n") {
		t.Errorf("FormatTotalsLine returned a multi-line string; got %q", line)
	}
}

// TestFormatTotalsLine_UnpricedMoneyPreserved verifies the unpriced money marker
// still appears when a category has no pricing entry.
func TestFormatTotalsLine_UnpricedMoneyPreserved(t *testing.T) {
	totals := domain.Totals{
		Tokens: domain.TokenUsage{Input: domain.Tokens(1)},
		Money:  domain.CategoryMoney{Total: domain.UnpricedMoney()},
	}
	line := screens.FormatTotalsLine(totals)

	if !strings.Contains(line, screens.UnpricedMarker) {
		t.Errorf("FormatTotalsLine with UnpricedMoney does not contain UnpricedMarker %q; got %q",
			screens.UnpricedMarker, line)
	}
}

// TestFormatTotalsLine_PartialMoneyPreserved verifies the "(partial)" suffix
// still appears when the money total is incomplete.
func TestFormatTotalsLine_PartialMoneyPreserved(t *testing.T) {
	totals := domain.Totals{
		Tokens: domain.TokenUsage{Input: domain.Tokens(1)},
		Money: domain.CategoryMoney{
			Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
			Complete: false,
		},
	}
	line := screens.FormatTotalsLine(totals)

	if !strings.Contains(line, "partial") {
		t.Errorf("FormatTotalsLine with incomplete money does not contain 'partial'; got %q", line)
	}
}

// ---------------------------------------------------------------------------
// Runs screen help bar: source-switch key advertising
// ---------------------------------------------------------------------------

// TestRunsScreen_HelpAndView_AdvertiseSourceSwitchKey verifies that both
// RunsScreen.Help() and the help bar rendered by RunsScreen.View() contain
// the source-switch key ('s') as a standalone key binding, satisfying the
// contract that the two sources of key hints must stay in sync.
//
// The current help text ("↑/↓ navigate  enter open  p pricing  esc quit  ctrl+c quit")
// does not contain the pattern "s " (standalone 's' key followed by its label).
// This test therefore fails until the implementation adds the source-switch hint.
func TestRunsScreen_HelpAndView_AdvertiseSourceSwitchKey(t *testing.T) {
	report := domain.Report{
		Runs:     nil,
		AllRuns:  domain.Totals{},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary(nil),
	}
	styles := plainStyles()
	s := screens.NewRunsScreen(report, 100, 30, styles)

	help := s.Help()
	view := s.View(100, 30)

	// Help() must advertise 's' as a standalone key binding.
	// "s " (s followed by its description word) distinguishes the 's' key from
	// substrings of other words like "esc" (e-s-c, no 's' followed by space).
	if !strings.Contains(help, "s ") {
		t.Errorf("Help() does not advertise 's' key binding; got %q", help)
	}
	// View()'s rendered help bar must carry the same key hint so the two sources
	// of key information cannot drift apart.
	if !strings.Contains(view, "s ") {
		t.Errorf("View() help bar does not advertise 's' key binding; view:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T5.5: Runs help bar advertises pricing unconditionally (T5.5)
// ---------------------------------------------------------------------------

// TestRunsScreen_HelpBar_AdvertisesPricingUnconditionally verifies that both
// Help() and the help bar rendered by View() contain 'p' as a key binding even
// when every model in the report is already priced (UnpricedModels is empty).
// The pricing key must be unconditional: the old implementation silently swallowed
// 'p' when nothing was unpriced, making the help bar misleading. This test pins
// the corrected behaviour — the help bar must always advertise pricing truthfully.
func TestRunsScreen_HelpBar_AdvertisesPricingUnconditionally(t *testing.T) {
	// Report with no unpriced models — all models are priced.
	report := domain.Report{
		Runs: []domain.RunReport{{
			Run:    domain.NamedRun("20260101T120000Z-abcd"),
			Totals: domain.Totals{Money: domain.CategoryMoney{Total: domain.KnownMoney(domain.Money(1_000_000_000)), Complete: true}},
		}},
		AllRuns:  domain.Totals{},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary(nil),
		// UnpricedModels is empty: all models are priced.
	}
	styles := plainStyles()
	s := screens.NewRunsScreen(report, 100, 30, styles)

	help := s.Help()
	view := s.View(100, 30)

	// Help() must advertise 'p' as a standalone key binding.
	// "p " (p followed by its description word) distinguishes the 'p' key from
	// substrings like "up" or "open" (which contain 'p' but not as a standalone key).
	if !strings.Contains(help, "p ") {
		t.Errorf("Help() does not advertise 'p' key binding unconditionally; got %q", help)
	}
	// View()'s rendered help bar must carry the same key hint.
	if !strings.Contains(view, "p ") {
		t.Errorf("View() help bar does not advertise 'p' key binding unconditionally; view:\n%s", view)
	}
	// The hint must not imply pricing is conditional (e.g. 'p (if unpriced)').
	if strings.Contains(help, "if unpriced") || strings.Contains(help, "when unpriced") {
		t.Errorf("Help() pricing hint implies conditionality; got %q", help)
	}
}

// ---------------------------------------------------------------------------
// Test helper: plainStyles returns a plain (no-colour) Styles for unit tests.
// ---------------------------------------------------------------------------

func plainStyles() screens.Styles {
	return screens.Styles{} // zero lipgloss.Style values are safe no-ops
}
