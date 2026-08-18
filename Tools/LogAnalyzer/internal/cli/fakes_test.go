package cli_test

// Fake implementations of all domain ports used by the CLI-layer tests.
// Hand-written without a mocking framework; the port interfaces are small
// enough to implement directly.

import (
	"context"
	"time"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Test constants and run-id helpers
// ---------------------------------------------------------------------------

const (
	cliTestRunID      = "20260101T000000Z-abcd"
	cliTestOrchFile   = "/logs/20260101T000000Z-abcd/00_orchestrator_events.jsonl"
	cliTestAgent1File = "/logs/20260101T000000Z-abcd/Agent#1/03_events.jsonl"
)

var cliTestRunRef = domain.NamedRun(cliTestRunID)

// ---------------------------------------------------------------------------
// fakeLogSource implements domain.LogSource.
// ---------------------------------------------------------------------------

type fakeLogSource struct {
	classifyFunc  func(path string) domain.Source
	defaultFunc   func(workDir string) domain.Source
	enumerateFunc func(src domain.Source) (domain.Inventory, []domain.Finding)
}

func (f *fakeLogSource) Classify(path string) domain.Source {
	if f.classifyFunc != nil {
		return f.classifyFunc(path)
	}
	return domain.Source{Kind: domain.SourceNotFound}
}

func (f *fakeLogSource) Default(workDir string) domain.Source {
	if f.defaultFunc != nil {
		return f.defaultFunc(workDir)
	}
	return domain.Source{Kind: domain.SourceNotFound}
}

func (f *fakeLogSource) Enumerate(src domain.Source) (domain.Inventory, []domain.Finding) {
	if f.enumerateFunc != nil {
		return f.enumerateFunc(src)
	}
	return domain.Inventory{Source: src}, nil
}

// ---------------------------------------------------------------------------
// fakeEventReader implements domain.EventReader.
// ---------------------------------------------------------------------------

type fakeEventReader struct {
	events   map[string][]domain.Event
	findings map[string][]domain.Finding
}

func newFakeEventReader() *fakeEventReader {
	return &fakeEventReader{
		events:   make(map[string][]domain.Event),
		findings: make(map[string][]domain.Finding),
	}
}

func (r *fakeEventReader) addEvents(path string, events ...domain.Event) {
	r.events[path] = append(r.events[path], events...)
}

func (r *fakeEventReader) Read(ctx context.Context, path string, visit func(domain.Event) error) ([]domain.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, ev := range r.events[path] {
		if err := visit(ev); err != nil {
			return r.findings[path], err
		}
	}
	return r.findings[path], nil
}

// ---------------------------------------------------------------------------
// fakePricingStore implements domain.PricingStore.
// ---------------------------------------------------------------------------

type fakePricingStore struct {
	table    domain.PricingTable
	filePath string
	loadErr  error
}

func newFakePricingStore() *fakePricingStore {
	return &fakePricingStore{
		table:    domain.EmptyPricingTable(),
		filePath: "/test/pricing.yaml",
	}
}

func (s *fakePricingStore) Path() string { return s.filePath }

func (s *fakePricingStore) Load(_ context.Context) (domain.PricingTable, []domain.Finding, error) {
	return s.table, nil, s.loadErr
}

func (s *fakePricingStore) Put(_ context.Context, entry domain.ModelPricing) error {
	s.table = s.table.With(entry)
	return nil
}

// ---------------------------------------------------------------------------
// fakeClock implements domain.Clock.
// ---------------------------------------------------------------------------

type fakeClock struct{ fixed time.Time }

func newFakeClock() *fakeClock {
	return &fakeClock{fixed: time.Date(2026, 8, 2, 7, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.fixed }

// ---------------------------------------------------------------------------
// alwaysSkippingInteraction implements domain.Interaction.
// ---------------------------------------------------------------------------

// alwaysSkippingInteraction returns SkippedOne for every question and never
// blocks. It is used to verify non-blocking behaviour across every flow.
type alwaysSkippingInteraction struct{}

func (i *alwaysSkippingInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.SkippedOne}, nil
}

func (i *alwaysSkippingInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.SkippedOne}, nil
}

func (i *alwaysSkippingInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.SkippedOne}, nil
}

func (i *alwaysSkippingInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.SkippedOne}, nil
}

func (i *alwaysSkippingInteraction) Notify(_ context.Context, _ interaction.Notice)         {}
func (i *alwaysSkippingInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// ---------------------------------------------------------------------------
// Event builders
// ---------------------------------------------------------------------------

func cliTestInvEndEvent(id domain.AgentInstanceID, model domain.ModelID, usage domain.TokenUsage) domain.Event {
	return domain.Event{
		Type: domain.EventInvocationEnd,
		InvocationEnd: &domain.InvocationEndFields{
			AgentInstanceID: id,
			Model:           model,
			Usage:           usage,
			HasUsage:        !usage.IsEmpty(),
		},
	}
}

func cliTestTurnEvent(model domain.ModelID, usage domain.TokenUsage) domain.Event {
	return domain.Event{
		Type: domain.EventTurn,
		Turn: &domain.TurnFields{
			Role:     domain.TurnAssistant,
			Model:    model,
			Usage:    usage,
			HasUsage: !usage.IsEmpty(),
		},
	}
}

func cliTestRunEndEvent() domain.Event {
	return domain.Event{Type: domain.EventRunEnd}
}

// mustParseRate parses a rate string; panics on error. For use in test setup only.
func mustParseRate(s string) domain.Rate {
	r, err := domain.ParseRate(s)
	if err != nil {
		panic("mustParseRate: " + err.Error())
	}
	return r
}

// flatPricing returns a ModelPricing with no long-context tier.
func flatPricing(model domain.ModelID) domain.ModelPricing {
	return domain.ModelPricing{
		Model:                model,
		Input:                mustParseRate("3.00"),
		CachedInput:          mustParseRate("0.30"),
		CacheWrite:           mustParseRate("3.75"),
		OutputUnderThreshold: mustParseRate("15.00"),
		HasThreshold:         false,
	}
}
