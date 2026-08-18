package app_test

// Tests for the pricing-editor use case.
//
// Coverage:
//   - ReportModels: whole-report model enumeration (all models, sorted,
//     de-duplicated, priced+unpriced, nil-unattributable, empty-report)
//   - ModelPricingStates: per-model priced/unpriced view tied to a pricing table
//   - EditModelPricing: happy path, overwrite, and non-answer paths
//   - EditPricing: frontend-agnostic model-selection entry point
//   - Non-blocking guarantee under an always-skipping Interaction

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared test helpers for the pricing-editor flow
// ---------------------------------------------------------------------------

// buildEditorTestSession runs Analyze with a log source that produces a session
// containing two models:
//   - "model-a": priced at flatPricing rates in the store
//   - "model-x": absent from the store, so it is unpriced
//
// Returns the analysis result, the event reader used during analysis (for
// read-count assertions), and the pricing store.
func buildEditorTestSession(t *testing.T) (app.Result, *fakeEventReader, *fakePricingStore) {
	t.Helper()

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing("model-a")})

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}
	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{{
					Run:              appTestRunRef,
					Dir:              "/test-logs/20260101T000000Z-abcd",
					OrchestratorFile: appTestOrchFile,
					Agents: []domain.AgentEntry{{
						Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
						InstanceHint: "TestAgent#1",
						EventFile:    appTestAgent1File,
					}},
				}},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	reader.addEvents(appTestAgent1File,
		// model-a: 1 M input + 0.5 M output (priced → known money)
		appTestInvEndEvent("TestAgent#1", "model-a", domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(500_000),
		}),
		// model-x: 2 M input (unpriced → unpriced money)
		appTestInvEndEvent("TestAgent#1", "model-x", domain.TokenUsage{
			Input: domain.Tokens(2_000_000),
		}),
	)

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})
	if err != nil {
		t.Fatalf("buildEditorTestSession: Analyze returned error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("buildEditorTestSession: Outcome = %v, want OutcomeReport", result.Outcome)
	}

	return result, reader, store
}

// allRateAnsweringIA returns a scriptedInteraction that answers every
// QuestionPricingRate question with the given rate string and skips everything else.
func allRateAnsweringIA(rate string) *scriptedInteraction {
	return &scriptedInteraction{
		onAskText: func(q interaction.TextQuestion) interaction.TextAnswer {
			if q.ID == app.QuestionPricingRate {
				return interaction.TextAnswer{Status: interaction.Answered, Text: rate}
			}
			return interaction.TextAnswer{Status: interaction.SkippedOne}
		},
	}
}

// newEditService constructs a Service wired with the given Interaction and
// pricing store, using no-op stubs for the other dependencies. Used to call
// the editor methods without repeating the full Deps boilerplate.
func newEditService(ia domain.Interaction, store *fakePricingStore) *app.Service {
	return app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      newFakeEventReader(),
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})
}

// noticeContains reports whether any notice in calls has a Title or Message
// containing substr. Used to assert Interaction.Notify was called with the
// expected context without coupling to exact message wording.
func noticeContains(calls []interaction.Notice, substr string) bool {
	for _, n := range calls {
		if strings.Contains(n.Title, substr) || strings.Contains(n.Message, substr) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ReportModels: whole-report model enumeration
// ---------------------------------------------------------------------------

func TestReportModels_EmptyReport_ReturnsEmpty(t *testing.T) {
	// An empty report — no named runs and no unattributable bucket — has no models
	// anywhere. ReportModels must return an empty slice without panicking.

	// Arrange
	report := domain.Report{}

	// Act
	got := app.ReportModels(report)

	// Assert
	if len(got) != 0 {
		t.Errorf("ReportModels(empty report) = %v, want empty slice", got)
	}
}

func TestReportModels_NilUnattributable_ReturnsNamedRunModels(t *testing.T) {
	// When Unattributable is nil, ReportModels must still return models from
	// named runs without panicking.

	// Arrange
	report := domain.Report{
		Runs: []domain.RunReport{{
			Run: appTestRunRef,
			Orchestrator: domain.ActorReport{
				Actor:  domain.Orchestrator(),
				Models: []domain.ModelID{"model-a"},
			},
		}},
		Unattributable: nil,
	}

	// Act
	got := app.ReportModels(report)

	// Assert
	want := []domain.ModelID{"model-a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReportModels with nil Unattributable = %v, want %v", got, want)
	}
}

func TestReportModels_MultipleRuns_ReturnsSortedDedupedModels(t *testing.T) {
	// Models appearing in multiple actors or runs are deduplicated and the result
	// is sorted by model id.

	// Arrange
	report := domain.Report{
		Runs: []domain.RunReport{{
			Run: appTestRunRef,
			Orchestrator: domain.ActorReport{
				Actor:  domain.Orchestrator(),
				Models: []domain.ModelID{"model-c", "model-a"}, // unsorted
			},
			Agents: []domain.ActorReport{{
				Actor:  domain.AgentInstance("Agent#1"),
				Models: []domain.ModelID{"model-b", "model-a"}, // model-a is a duplicate
			}},
		}},
	}

	// Act
	got := app.ReportModels(report)

	// Assert: model-a appears twice — result must deduplicate and sort.
	want := []domain.ModelID{"model-a", "model-b", "model-c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReportModels (dedup+sort) = %v, want %v", got, want)
	}
}

func TestReportModels_UnattributableBucket_IncludesItsModels(t *testing.T) {
	// Models that appear only in the unattributable bucket (Orchestrator or
	// Agents) must still be included in the result.

	// Arrange
	report := domain.Report{
		Runs: []domain.RunReport{{
			Run: appTestRunRef,
			Orchestrator: domain.ActorReport{
				Actor:  domain.Orchestrator(),
				Models: []domain.ModelID{"model-a"},
			},
		}},
		Unattributable: &domain.RunReport{
			Run: domain.UnattributableRun(),
			Orchestrator: domain.ActorReport{
				Models: []domain.ModelID{"model-b"},
			},
			Agents: []domain.ActorReport{{
				Actor:  domain.AgentInstance("Agent#1"),
				Models: []domain.ModelID{"model-c"},
			}},
		},
	}

	// Act
	got := app.ReportModels(report)

	// Assert
	want := []domain.ModelID{"model-a", "model-b", "model-c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReportModels with unattributable = %v, want %v", got, want)
	}
}

func TestReportModels_PricedModels_IncludedAlongsideUnpriced(t *testing.T) {
	// ReportModels is a superset of Report.UnpricedModels. Models that have a
	// pricing entry (absent from UnpricedModels) must still appear in the result.

	// Arrange
	report := domain.Report{
		Runs: []domain.RunReport{{
			Run: appTestRunRef,
			Orchestrator: domain.ActorReport{
				Actor:  domain.Orchestrator(),
				Models: []domain.ModelID{"model-priced"},
				// UnpricedModels is empty: model-priced has a price.
			},
			Agents: []domain.ActorReport{{
				Actor:          domain.AgentInstance("Agent#1"),
				Models:         []domain.ModelID{"model-unpriced"},
				UnpricedModels: []domain.ModelID{"model-unpriced"},
			}},
		}},
		UnpricedModels: []domain.ModelID{"model-unpriced"},
	}

	// Act
	got := app.ReportModels(report)

	// Assert: both the priced and the unpriced model must appear.
	foundPriced, foundUnpriced := false, false
	for _, m := range got {
		if m == "model-priced" {
			foundPriced = true
		}
		if m == "model-unpriced" {
			foundUnpriced = true
		}
	}
	if !foundPriced {
		t.Errorf("ReportModels must include already-priced models; got %v", got)
	}
	if !foundUnpriced {
		t.Errorf("ReportModels must include unpriced models; got %v", got)
	}
}

// ---------------------------------------------------------------------------
// ModelPricingStates: per-model priced/unpriced view
// ---------------------------------------------------------------------------

func TestModelPricingStates_UnpricedModel_HasPricedFalse(t *testing.T) {
	// A model with no entry in the pricing table must have Priced=false and a
	// zero Pricing value.

	// Arrange
	report := domain.Report{
		Runs: []domain.RunReport{{
			Run: appTestRunRef,
			Orchestrator: domain.ActorReport{
				Actor:          domain.Orchestrator(),
				Models:         []domain.ModelID{"model-x"},
				UnpricedModels: []domain.ModelID{"model-x"},
			},
		}},
	}
	table := domain.EmptyPricingTable()

	// Act
	got := app.ModelPricingStates(report, table)

	// Assert
	if len(got) == 0 {
		t.Fatal("ModelPricingStates must return at least one entry for a report with models")
	}
	if got[0].Model != "model-x" {
		t.Errorf("got[0].Model = %q, want %q", got[0].Model, "model-x")
	}
	if got[0].Priced {
		t.Error("ModelPricingStatus.Priced must be false for a model with no pricing entry")
	}
	if !reflect.DeepEqual(got[0].Pricing, domain.ModelPricing{}) {
		t.Errorf("ModelPricingStatus.Pricing must be the zero value when Priced is false; got %+v", got[0].Pricing)
	}
}

func TestModelPricingStates_PricedModel_HasPricedTrueWithRates(t *testing.T) {
	// A model that has an entry in the pricing table must have Priced=true and
	// Pricing carrying the exact rates from the table.

	// Arrange
	pricing := flatPricing("model-a")
	report := domain.Report{
		Runs: []domain.RunReport{{
			Run: appTestRunRef,
			Orchestrator: domain.ActorReport{
				Actor:  domain.Orchestrator(),
				Models: []domain.ModelID{"model-a"},
				// UnpricedModels empty: model-a is priced.
			},
		}},
	}
	table := domain.NewPricingTable([]domain.ModelPricing{pricing})

	// Act
	got := app.ModelPricingStates(report, table)

	// Assert
	if len(got) == 0 {
		t.Fatal("ModelPricingStates must return at least one entry")
	}
	if got[0].Model != "model-a" {
		t.Errorf("got[0].Model = %q, want %q", got[0].Model, "model-a")
	}
	if !got[0].Priced {
		t.Error("ModelPricingStatus.Priced must be true for a model in the pricing table")
	}
	if !reflect.DeepEqual(got[0].Pricing, pricing) {
		t.Errorf("ModelPricingStatus.Pricing = %+v, want %+v", got[0].Pricing, pricing)
	}
}

func TestModelPricingStates_PreservesReportModelsOrdering(t *testing.T) {
	// ModelPricingStates must follow the same sorted order as ReportModels,
	// so the ordering a frontend sees is consistent.

	// Arrange
	report := domain.Report{
		Runs: []domain.RunReport{{
			Run: appTestRunRef,
			Orchestrator: domain.ActorReport{
				Actor:  domain.Orchestrator(),
				Models: []domain.ModelID{"model-b", "model-a"}, // unsorted
			},
		}},
	}
	table := domain.EmptyPricingTable()

	// Act
	got := app.ModelPricingStates(report, table)

	// Assert: the ordering must follow ReportModels (alphabetical).
	if len(got) < 2 {
		t.Fatalf("ModelPricingStates must return 2 entries; got %d", len(got))
	}
	if got[0].Model != "model-a" || got[1].Model != "model-b" {
		t.Errorf("ModelPricingStates ordering = [%q, %q], want [%q, %q]",
			got[0].Model, got[1].Model, "model-a", "model-b")
	}
}

// ---------------------------------------------------------------------------
// EditModelPricing: happy path — price an unpriced model
// ---------------------------------------------------------------------------

func TestEditModelPricing_ValidModel_AnsweredRates_PersistsEntryAndReprices(t *testing.T) {
	// Providing all four rate answers for an unpriced model must persist the entry
	// through the store and return a re-priced report with Outcome == PricingEditSaved.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	editStore := newFakePricingStore()
	ia := allRateAnsweringIA("3.00")
	editSvc := newEditService(ia, editStore)

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-x")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditSaved {
		t.Errorf("Outcome = %v, want PricingEditSaved", result.Outcome)
	}
	if result.Model != "model-x" {
		t.Errorf("Result.Model = %q, want %q", result.Model, "model-x")
	}
	if len(editStore.putCalls) == 0 {
		t.Error("PricingStore.Put must be called when prices are entered; got 0 Put calls")
	} else if editStore.putCalls[0].Model != "model-x" {
		t.Errorf("Put called with model %q, want %q", editStore.putCalls[0].Model, "model-x")
	}
	if result.Report.IsEmpty() {
		t.Error("the returned report must not be empty after a successful price entry")
	}
	if result.ConfigPath == "" {
		t.Error("PricingEditResult.ConfigPath must be non-empty when Outcome is PricingEditSaved")
	}
	if len(ia.notifyCalls) == 0 {
		t.Error("Interaction.Notify must be called on a successful save")
	}
	if !noticeContains(ia.notifyCalls, "model-x") {
		t.Errorf("at least one Notify call must mention the model id %q; got notices: %v", "model-x", ia.notifyCalls)
	}
}

func TestEditModelPricing_ValidModel_ReportRepricedWithoutRereadingLogs(t *testing.T) {
	// After entering prices, the report must be re-priced from the Session's cached
	// aggregate. The EventReader must never be called during the edit flow.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	// A fresh reader injected into the edit service — any read call is observable.
	editReader := newFakeEventReader()
	editSvc := app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      editReader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: allRateAnsweringIA("3.00"),
		WorkDir:     "/workdir",
	})

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-x")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditSaved {
		t.Fatalf("Outcome = %v, want PricingEditSaved", result.Outcome)
	}
	if len(editReader.readCounts) != 0 {
		t.Errorf("EventReader.Read must not be called during EditModelPricing; "+
			"re-pricing uses Session.Reprice() which is pure. Got reads for: %v", editReader.readCounts)
	}
}

// ---------------------------------------------------------------------------
// EditModelPricing: overwrite an already-priced model
// ---------------------------------------------------------------------------

func TestEditModelPricing_AlreadyPricedModel_NewRates_ReplacesEntryAndMoneyChanges(t *testing.T) {
	// Setting new rates for a model that already has a price must replace the stored
	// entry and produce a report whose money reflects the new rates.
	//
	// Pre-edit: model-a at flatPricing (3.00/M input, 15.00/M output) → ~$10.50 total.
	// Post-edit: model-a at 100.00/M for all → much higher total.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	preEditTotal := session.Report.AllRuns.Money.Total
	if preEditTotal.State != domain.MoneyKnown {
		t.Skip("pre-edit report does not carry known money; cannot verify overwrite changes money")
	}

	editStore := newFakePricingStore()
	editSvc := newEditService(allRateAnsweringIA("100.00"), editStore)

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-a")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditSaved {
		t.Fatalf("Outcome = %v, want PricingEditSaved", result.Outcome)
	}
	if len(editStore.putCalls) == 0 {
		t.Error("PricingStore.Put must be called for an overwrite; got 0 Put calls")
	} else if editStore.putCalls[0].Model != "model-a" {
		t.Errorf("Put called with model %q, want %q", editStore.putCalls[0].Model, "model-a")
	}
	postEditTotal := result.Report.AllRuns.Money.Total
	if postEditTotal.State != domain.MoneyKnown {
		t.Errorf("post-edit AllRuns money state = %v, want MoneyKnown", postEditTotal.State)
	}
	if preEditTotal.Amount == postEditTotal.Amount {
		t.Error("overwriting a price with different rates must change the reported money; " +
			"pre-edit and post-edit amounts are identical")
	}
	if result.ConfigPath == "" {
		t.Error("PricingEditResult.ConfigPath must be non-empty when Outcome is PricingEditSaved")
	}
}

func TestEditModelPricing_AlreadyPricedModel_IncompleteRates_OldPriceUntouched(t *testing.T) {
	// The plan risk register calls out this overwrite scenario:
	// "an overwrite that answers only some rate questions could produce an entry
	// that fails Validate() and is silently dropped, leaving the old price in place."
	// This test exercises that path for an already-priced model and verifies the
	// returned report's money matches the pre-edit total — confirming the existing
	// price is preserved rather than lost from the session table.
	//
	// Pre-edit: model-a at flatPricing rates → AllRuns money reflects those rates.
	// Edit: answer only the input rate for model-a, skip all others → fails Validate().
	// Post-edit: Outcome must be PricingEditNoChange, Put not called, and the
	// returned report's money must equal the pre-edit total (old price intact).

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	preEditTotal := session.Report.AllRuns.Money.Total
	if preEditTotal.State != domain.MoneyKnown {
		t.Skip("pre-edit report does not carry known money; cannot verify overwrite leaves old price intact")
	}

	ia := &scriptedInteraction{
		onAskText: func(q interaction.TextQuestion) interaction.TextAnswer {
			// Answer only the input rate for model-a; skip all others so the
			// collected entry is incomplete and fails ModelPricing.Validate().
			if q.ID == app.QuestionPricingRate &&
				q.Subject == "model-a:"+app.RateFieldInput {
				return interaction.TextAnswer{Status: interaction.Answered, Text: "3.00"}
			}
			return interaction.TextAnswer{Status: interaction.SkippedOne}
		},
	}
	editStore := newFakePricingStore()
	editSvc := newEditService(ia, editStore)

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-a")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditNoChange {
		t.Errorf("Outcome = %v, want PricingEditNoChange when the collected entry fails validation",
			result.Outcome)
	}
	if len(editStore.putCalls) != 0 {
		t.Errorf("PricingStore.Put must not be called for an entry that fails validation; "+
			"got %d Put calls", len(editStore.putCalls))
	}
	// The pre-existing price must still be reflected in the returned report.
	// If the old entry were dropped from the session table, the money state would
	// change from MoneyKnown; if its rates were somehow reset, the amount would differ.
	postEditTotal := result.Report.AllRuns.Money.Total
	if postEditTotal.State != domain.MoneyKnown {
		t.Errorf("post-edit AllRuns money state = %v, want MoneyKnown — "+
			"the old price must remain in effect in the returned report", postEditTotal.State)
	}
	if postEditTotal.Amount != preEditTotal.Amount {
		t.Errorf("post-edit AllRuns money amount = %v, want %v (pre-edit) — "+
			"the old price must remain in effect in the returned report",
			postEditTotal.Amount, preEditTotal.Amount)
	}
}

func TestEditModelPricing_QualityPreservedInRepricedReport(t *testing.T) {
	// The quality summary from the original analysis report must survive the
	// re-price applied by EditModelPricing — it must not be reset or recomputed.
	// This pins the behavior at its true source: EditModelPricing, not just the
	// EditPricing delegation path.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	originalQuality := session.Report.Quality
	ia := allRateAnsweringIA("3.00")
	editSvc := newEditService(ia, newFakePricingStore())

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-x")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditSaved {
		t.Fatalf("Outcome = %v, want PricingEditSaved (prerequisite for quality check)", result.Outcome)
	}
	if result.Report.Quality.IsClean() != originalQuality.IsClean() {
		t.Errorf("Quality.IsClean() changed after re-price: got %v, want %v (original)",
			result.Report.Quality.IsClean(), originalQuality.IsClean())
	}
}

// ---------------------------------------------------------------------------
// Non-answer paths: skip, cancel, invalid/incomplete rates
// ---------------------------------------------------------------------------

func TestEditModelPricing_EmptyModelID_ReturnsCancelled_NoQuestionsAsked(t *testing.T) {
	// An empty model id is an invalid selection. The flow must return
	// PricingEditCancelled without asking any questions and without touching the store.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	ia := &scriptedInteraction{}
	editStore := newFakePricingStore()
	editSvc := newEditService(ia, editStore)

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error for empty model id: %v", err)
	}
	if result.Outcome != app.PricingEditCancelled {
		t.Errorf("Outcome = %v, want PricingEditCancelled for empty model id", result.Outcome)
	}
	if len(ia.askTextCalls) != 0 || len(ia.selectOneCalls) != 0 {
		t.Errorf("no questions must be asked for an empty model id; "+
			"askText=%d selectOne=%d", len(ia.askTextCalls), len(ia.selectOneCalls))
	}
	if len(editStore.putCalls) != 0 {
		t.Errorf("PricingStore.Put must not be called for an empty model id; got %d Put calls", len(editStore.putCalls))
	}
	if !reflect.DeepEqual(result.Report, session.Report) {
		t.Error("result.Report must equal the input report when Outcome is PricingEditCancelled (empty model id)")
	}
}

func TestEditModelPricing_NilSession_ReturnsCancelled_NoPanic(t *testing.T) {
	// A nil Session represents the "no analysis yet" state in the TUI. The flow
	// must return PricingEditCancelled without panicking and without touching the store.

	// Arrange
	ia := &scriptedInteraction{}
	editStore := newFakePricingStore()
	editSvc := newEditService(ia, editStore)

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), nil, domain.Report{}, "model-a")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error for nil session: %v", err)
	}
	if result.Outcome != app.PricingEditCancelled {
		t.Errorf("Outcome = %v, want PricingEditCancelled for nil session", result.Outcome)
	}
	if len(editStore.putCalls) != 0 {
		t.Errorf("PricingStore.Put must not be called for a nil session; got %d Put calls", len(editStore.putCalls))
	}
	if !reflect.DeepEqual(result.Report, (domain.Report{})) {
		t.Error("result.Report must equal the input report when Outcome is PricingEditCancelled (nil session)")
	}
}

func TestEditModelPricing_AllRatesSkipped_ReturnsNoChange_NothingPersisted(t *testing.T) {
	// Skipping every rate question means no valid entry can be built. The flow must
	// return PricingEditNoChange and must not call PricingStore.Put.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	editStore := newFakePricingStore()
	editSvc := newEditService(&alwaysSkippingInteraction{}, editStore)

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-x")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditNoChange {
		t.Errorf("Outcome = %v, want PricingEditNoChange when all rates are skipped", result.Outcome)
	}
	if len(editStore.putCalls) != 0 {
		t.Errorf("PricingStore.Put must not be called when no valid entry was collected; "+
			"got %d Put calls", len(editStore.putCalls))
	}
	if !reflect.DeepEqual(result.Report, session.Report) {
		t.Error("result.Report must equal the input report when Outcome is PricingEditNoChange (all rates skipped)")
	}
}

func TestEditModelPricing_IncompleteRates_FailsValidation_NothingPersisted(t *testing.T) {
	// Answering only the input rate and skipping the others produces an entry with
	// zero CachedInput, CacheWrite and OutputUnderThreshold — which fails
	// ModelPricing.Validate(). The flow must not persist such an entry.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	ia := &scriptedInteraction{
		onAskText: func(q interaction.TextQuestion) interaction.TextAnswer {
			// Answer only the first rate field; skip all others.
			if q.ID == app.QuestionPricingRate &&
				q.Subject == "model-x:"+app.RateFieldInput {
				return interaction.TextAnswer{Status: interaction.Answered, Text: "3.00"}
			}
			return interaction.TextAnswer{Status: interaction.SkippedOne}
		},
	}
	editStore := newFakePricingStore()
	editSvc := newEditService(ia, editStore)

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-x")

	// Assert
	if err != nil {
		t.Fatalf("EditModelPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditNoChange {
		t.Errorf("Outcome = %v, want PricingEditNoChange when the collected entry fails validation",
			result.Outcome)
	}
	if len(editStore.putCalls) != 0 {
		t.Errorf("PricingStore.Put must not be called for an entry that fails validation; "+
			"got %d Put calls", len(editStore.putCalls))
	}
	if !reflect.DeepEqual(result.Report, session.Report) {
		t.Error("result.Report must equal the input report when Outcome is PricingEditNoChange (incomplete rates)")
	}
}

// ---------------------------------------------------------------------------
// EditPricing: frontend-agnostic entry point (model selection)
// ---------------------------------------------------------------------------

func TestEditPricing_ModelSelectionSkipped_ReturnsCancelled_NothingPersisted(t *testing.T) {
	// Skipping or cancelling the model-selection question must return
	// PricingEditCancelled with the original report unchanged and nothing persisted.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	ia := &scriptedInteraction{} // all questions return SkippedOne by default
	editStore := newFakePricingStore()
	editSvc := newEditService(ia, editStore)

	// Act
	result, err := editSvc.EditPricing(context.Background(), session.Session, session.Report)

	// Assert
	if err != nil {
		t.Fatalf("EditPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditCancelled {
		t.Errorf("Outcome = %v, want PricingEditCancelled when model selection is skipped",
			result.Outcome)
	}
	if len(editStore.putCalls) != 0 {
		t.Errorf("PricingStore.Put must not be called on a cancelled selection; "+
			"got %d Put calls", len(editStore.putCalls))
	}
	if !reflect.DeepEqual(result.Report, session.Report) {
		t.Error("result.Report must equal the input report when Outcome is PricingEditCancelled (model selection skipped)")
	}
}

func TestEditPricing_UnrecognisedOptionID_ReturnsCancelled(t *testing.T) {
	// An Answered ChoiceAnswer whose OptionID does not match any model in
	// ReportModels output must be treated as a cancel rather than forwarded to
	// EditModelPricing with a bogus model id. This is a distinct code path from
	// the "skipped/cancelled selection" case tested above.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	ia := &scriptedInteraction{
		onSelectOne: func(q interaction.ChoiceQuestion) interaction.ChoiceAnswer {
			if q.ID == app.QuestionPricingModel {
				return interaction.ChoiceAnswer{
					Status:   interaction.Answered,
					OptionID: "not-a-real-model", // does not appear in the report
				}
			}
			return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
		},
	}
	editStore := newFakePricingStore()
	editSvc := newEditService(ia, editStore)

	// Act
	result, err := editSvc.EditPricing(context.Background(), session.Session, session.Report)

	// Assert
	if err != nil {
		t.Fatalf("EditPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditCancelled {
		t.Errorf("Outcome = %v, want PricingEditCancelled for unrecognised OptionID", result.Outcome)
	}
	if len(editStore.putCalls) != 0 {
		t.Errorf("PricingStore.Put must not be called for an unrecognised OptionID; "+
			"got %d Put calls", len(editStore.putCalls))
	}
	if !reflect.DeepEqual(result.Report, session.Report) {
		t.Error("result.Report must equal the input report when Outcome is PricingEditCancelled (unrecognised OptionID)")
	}
}

func TestEditPricing_NoModelsInReport_ReturnsCancelled_NoQuestionsAsked(t *testing.T) {
	// A report with no models anywhere has nothing to select. EditPricing must
	// return PricingEditCancelled without asking any questions.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	ia := &scriptedInteraction{}
	editSvc := newEditService(ia, newFakePricingStore())
	emptyReport := domain.Report{} // no runs, no models

	// Act
	result, err := editSvc.EditPricing(context.Background(), session.Session, emptyReport)

	// Assert
	if err != nil {
		t.Fatalf("EditPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditCancelled {
		t.Errorf("Outcome = %v, want PricingEditCancelled for a report with no models",
			result.Outcome)
	}
	if len(ia.selectOneCalls) != 0 {
		t.Errorf("no questions must be asked when the report has no models; "+
			"got %d SelectOne calls", len(ia.selectOneCalls))
	}
	if !reflect.DeepEqual(result.Report, emptyReport) {
		t.Error("result.Report must equal the input report when Outcome is PricingEditCancelled (no models in report)")
	}
}

func TestEditPricing_ModelSelected_AnsweredRates_PersistsAndReprices(t *testing.T) {
	// When the user selects a model and provides valid rate answers, EditPricing
	// must persist the entry and return a re-priced report.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	ia := &scriptedInteraction{
		onSelectOne: func(q interaction.ChoiceQuestion) interaction.ChoiceAnswer {
			if q.ID == app.QuestionPricingModel {
				return interaction.ChoiceAnswer{
					Status:   interaction.Answered,
					OptionID: "model-x", // the model-x option is the model id itself
				}
			}
			return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
		},
		onAskText: func(q interaction.TextQuestion) interaction.TextAnswer {
			if q.ID == app.QuestionPricingRate {
				return interaction.TextAnswer{Status: interaction.Answered, Text: "3.00"}
			}
			return interaction.TextAnswer{Status: interaction.SkippedOne}
		},
	}
	editStore := newFakePricingStore()
	editSvc := newEditService(ia, editStore)

	// Act
	result, err := editSvc.EditPricing(context.Background(), session.Session, session.Report)

	// Assert
	if err != nil {
		t.Fatalf("EditPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditSaved {
		t.Errorf("Outcome = %v, want PricingEditSaved", result.Outcome)
	}
	if result.Model != "model-x" {
		t.Errorf("Result.Model = %q, want %q", result.Model, "model-x")
	}
	if len(editStore.putCalls) == 0 {
		t.Error("PricingStore.Put must be called when prices are entered; got 0 Put calls")
	}
	if result.ConfigPath == "" {
		t.Error("PricingEditResult.ConfigPath must be non-empty when Outcome is PricingEditSaved")
	}
	if len(ia.notifyCalls) == 0 {
		t.Error("Interaction.Notify must be called on a successful save")
	}
	if !noticeContains(ia.notifyCalls, "model-x") {
		t.Errorf("at least one Notify call must mention the model id %q; got notices: %v", "model-x", ia.notifyCalls)
	}
}

func TestEditPricing_QualityPreservedInRepricedReport(t *testing.T) {
	// The quality summary from the original analysis must be preserved in the
	// re-priced report returned by EditPricing. Re-pricing from the cached Session
	// aggregate must not reset or recompute the quality state.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	originalQuality := session.Report.Quality

	ia := &scriptedInteraction{
		onSelectOne: func(q interaction.ChoiceQuestion) interaction.ChoiceAnswer {
			if q.ID == app.QuestionPricingModel {
				return interaction.ChoiceAnswer{
					Status:   interaction.Answered,
					OptionID: "model-x",
				}
			}
			return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
		},
		onAskText: func(q interaction.TextQuestion) interaction.TextAnswer {
			if q.ID == app.QuestionPricingRate {
				return interaction.TextAnswer{Status: interaction.Answered, Text: "3.00"}
			}
			return interaction.TextAnswer{Status: interaction.SkippedOne}
		},
	}
	editSvc := newEditService(ia, newFakePricingStore())

	// Act
	result, err := editSvc.EditPricing(context.Background(), session.Session, session.Report)

	// Assert
	if err != nil {
		t.Fatalf("EditPricing returned unexpected error: %v", err)
	}
	if result.Outcome != app.PricingEditSaved {
		t.Fatalf("Outcome = %v, want PricingEditSaved (prerequisite for quality check)", result.Outcome)
	}
	if result.Report.Quality.IsClean() != originalQuality.IsClean() {
		t.Errorf("Quality.IsClean() changed after re-price: got %v, want %v (original)",
			result.Report.Quality.IsClean(), originalQuality.IsClean())
	}
}

// ---------------------------------------------------------------------------
// Non-blocking guarantee
// ---------------------------------------------------------------------------

func TestEditPricing_AlwaysSkipping_Completes(t *testing.T) {
	// EditPricing must return without blocking when given an always-skipping
	// Interaction — the shape required of the CLI implementation.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	editSvc := newEditService(&alwaysSkippingInteraction{}, newFakePricingStore())

	// Act
	_, err := editSvc.EditPricing(context.Background(), session.Session, session.Report)

	// Assert
	if err != nil {
		t.Errorf("EditPricing must not return an error under a skipping Interaction; got: %v", err)
	}
}

func TestEditModelPricing_AlwaysSkipping_Completes(t *testing.T) {
	// EditModelPricing must return without blocking when given an always-skipping
	// Interaction — mirroring the non-blocking guarantee of ResolvePricingGaps.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	editSvc := newEditService(&alwaysSkippingInteraction{}, newFakePricingStore())

	// Act
	_, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-x")

	// Assert
	if err != nil {
		t.Errorf("EditModelPricing must not return an error under a skipping Interaction; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// QuestionPricingModel Options: id, order, and priced/unpriced label distinction
// ---------------------------------------------------------------------------

func TestEditPricing_QuestionPricingModel_OptionsMatchReportModels(t *testing.T) {
	// EditPricing must build the QuestionPricingModel SelectOne question with one
	// option per model in ReportModels order. Each OptionID must equal the model id
	// string so the implementation can dispatch to EditModelPricing directly. The
	// Labels for a priced model and an unpriced model must differ — a user must be
	// able to tell them apart from the rendered text alone.
	//
	// Fixture: session.Report has model-a (priced, has pricing in the session table)
	// and model-x (unpriced, absent from the pricing table). ReportModels order is
	// alphabetical: ["model-a", "model-x"].

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	ia := &scriptedInteraction{} // skips all questions; we only assert what was asked
	editSvc := newEditService(ia, newFakePricingStore())

	// Act
	_, err := editSvc.EditPricing(context.Background(), session.Session, session.Report)
	if err != nil {
		t.Fatalf("EditPricing returned unexpected error: %v", err)
	}

	// Locate the QuestionPricingModel SelectOne call in the recorded calls.
	var modelQ *interaction.ChoiceQuestion
	for i, q := range ia.selectOneCalls {
		if q.ID == app.QuestionPricingModel {
			copied := ia.selectOneCalls[i]
			modelQ = &copied
			break
		}
	}
	if modelQ == nil {
		t.Fatal("EditPricing must call SelectOne with QuestionPricingModel; no such call was recorded")
	}

	// Assert: one option per model in the report, in ReportModels (sorted) order.
	wantOrder := []domain.ModelID{"model-a", "model-x"}
	if len(modelQ.Options) != len(wantOrder) {
		t.Fatalf("QuestionPricingModel Options count = %d, want %d (one per report model)",
			len(modelQ.Options), len(wantOrder))
	}
	for i, want := range wantOrder {
		if got := domain.ModelID(modelQ.Options[i].ID); got != want {
			t.Errorf("Options[%d].ID = %q, want %q (ReportModels order)", i, got, want)
		}
	}

	// Assert: labels distinguish priced from unpriced. model-a is priced (has pricing
	// in the session table); model-x is unpriced. Their labels must not be identical
	// so the user can tell which models already have a price set.
	labelPriced := modelQ.Options[0].Label   // model-a — priced
	labelUnpriced := modelQ.Options[1].Label // model-x — unpriced
	if labelPriced == labelUnpriced {
		t.Errorf("QuestionPricingModel Option labels must distinguish priced from unpriced; "+
			"model-a (priced) label %q == model-x (unpriced) label %q — "+
			"a user cannot tell which model already has a price", labelPriced, labelUnpriced)
	}
}

// ---------------------------------------------------------------------------
// PricingStore.Put error propagation
// ---------------------------------------------------------------------------

func TestEditModelPricing_StorePutError_ReturnsNilResultAndError(t *testing.T) {
	// When PricingStore.Put returns an infrastructure error, EditModelPricing must
	// surface it as a non-nil error return. The returned PricingEditResult must be
	// the zero value — Outcome stays at PricingEditCancelled (iota 0), Model and
	// ConfigPath are empty — so the caller cannot mistake a failed write for a
	// successful save.

	// Arrange
	session, _, _ := buildEditorTestSession(t)
	editStore := newFakePricingStore()
	editStore.putErr = errors.New("simulated disk full")
	editSvc := newEditService(allRateAnsweringIA("3.00"), editStore)

	// Act
	result, err := editSvc.EditModelPricing(context.Background(), session.Session, session.Report, "model-x")

	// Assert
	if err == nil {
		t.Error("EditModelPricing must return a non-nil error when PricingStore.Put fails; got nil")
	}
	if result.Outcome == app.PricingEditSaved {
		t.Error("EditModelPricing must not report PricingEditSaved when Put fails")
	}
	if result.ConfigPath != "" {
		t.Errorf("PricingEditResult.ConfigPath must be empty on a store failure; got %q", result.ConfigPath)
	}
	if result.Model != "" {
		t.Errorf("PricingEditResult.Model must be empty on a store failure; got %q", result.Model)
	}
}
