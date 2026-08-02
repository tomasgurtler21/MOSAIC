package app_test

// Fake implementations of all domain ports used by the app-layer tests.
// Every fake is hand-written without a mocking framework, as the port
// interfaces are small enough to implement directly.

import (
	"context"
	"time"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// fakeLogSource implements domain.LogSource.
// ---------------------------------------------------------------------------

// fakeLogSource is a scripted implementation of domain.LogSource that records
// which methods were called and returns pre-configured outcomes.
type fakeLogSource struct {
	// classifyFunc is called for each Classify invocation. If nil, returns SourceNotFound.
	classifyFunc func(path string) domain.Source
	// defaultFunc is called for each Default invocation. If nil, returns SourceNotFound.
	defaultFunc func(workDir string) domain.Source
	// enumerateFunc is called for each Enumerate invocation. If nil, returns an empty
	// Inventory carrying the supplied source with no findings.
	enumerateFunc func(src domain.Source) (domain.Inventory, []domain.Finding)

	// Call-tracking slices. Tests inspect these after calling the service.
	classifyCalls []string
	defaultCalls  []string
}

func (f *fakeLogSource) Classify(path string) domain.Source {
	f.classifyCalls = append(f.classifyCalls, path)
	if f.classifyFunc != nil {
		return f.classifyFunc(path)
	}
	return domain.Source{Kind: domain.SourceNotFound}
}

func (f *fakeLogSource) Default(workDir string) domain.Source {
	f.defaultCalls = append(f.defaultCalls, workDir)
	if f.defaultFunc != nil {
		return f.defaultFunc(workDir)
	}
	return domain.Source{Kind: domain.SourceNotFound}
}

func (f *fakeLogSource) Enumerate(src domain.Source) (domain.Inventory, []domain.Finding) {
	if f.enumerateFunc != nil {
		return f.enumerateFunc(src)
	}
	// Default: usable source with no runs (empty inventory).
	return domain.Inventory{Source: src}, nil
}

// ---------------------------------------------------------------------------
// fakeEventReader implements domain.EventReader.
// ---------------------------------------------------------------------------

// fakeEventReader delivers scripted events and findings per file path and
// counts how many times each path was read.
type fakeEventReader struct {
	// events maps file path -> events to deliver via the visitor callback.
	events map[string][]domain.Event
	// findings maps file path -> findings to return alongside events.
	findings map[string][]domain.Finding

	// readCounts records how many times Read was called for each path.
	readCounts map[string]int
}

func newFakeEventReader() *fakeEventReader {
	return &fakeEventReader{
		events:     make(map[string][]domain.Event),
		findings:   make(map[string][]domain.Finding),
		readCounts: make(map[string]int),
	}
}

// addEvents registers events to deliver when Read is called with path.
func (r *fakeEventReader) addEvents(path string, events ...domain.Event) {
	r.events[path] = append(r.events[path], events...)
}

// addFindings registers findings to return when Read is called with path.
func (r *fakeEventReader) addFindings(path string, findings ...domain.Finding) {
	r.findings[path] = append(r.findings[path], findings...)
}

func (r *fakeEventReader) Read(ctx context.Context, path string, visit func(domain.Event) error) ([]domain.Finding, error) {
	r.readCounts[path]++
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

// fakePricingStore is an in-memory implementation of domain.PricingStore.
// Calls to Put are recorded and optionally applied to the in-memory table.
type fakePricingStore struct {
	table    domain.PricingTable
	findings []domain.Finding
	loadErr  error
	putErr   error
	filePath string

	// putCalls records every ModelPricing handed to Put.
	putCalls []domain.ModelPricing
}

func newFakePricingStore() *fakePricingStore {
	return &fakePricingStore{
		table:    domain.EmptyPricingTable(),
		filePath: "/test/pricing.yaml",
	}
}

func (s *fakePricingStore) Path() string { return s.filePath }

func (s *fakePricingStore) Load(ctx context.Context) (domain.PricingTable, []domain.Finding, error) {
	return s.table, s.findings, s.loadErr
}

func (s *fakePricingStore) Put(ctx context.Context, entry domain.ModelPricing) error {
	s.putCalls = append(s.putCalls, entry)
	if s.putErr != nil {
		return s.putErr
	}
	// Persist to in-memory table so subsequent Load reflects the new entry.
	s.table = s.table.With(entry)
	return nil
}

// ---------------------------------------------------------------------------
// fakeClock implements domain.Clock.
// ---------------------------------------------------------------------------

// fakeClock returns a fixed point in time, making tests deterministic.
type fakeClock struct {
	fixed time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{fixed: time.Date(2026, 8, 2, 7, 46, 35, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.fixed }

// ---------------------------------------------------------------------------
// alwaysSkippingInteraction implements domain.Interaction.
// ---------------------------------------------------------------------------

// alwaysSkippingInteraction is the CLI-shape fake: every question returns
// SkippedOne immediately and never blocks. Used to verify non-blocking behaviour
// across every flow.
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
// scriptedInteraction implements domain.Interaction.
// ---------------------------------------------------------------------------

// scriptedInteraction returns pre-configured answers per question ID and
// records which questions were asked, enabling assertions about the interaction
// flow without coupling tests to interaction implementation details.
type scriptedInteraction struct {
	// onSelectOne handles SelectOne calls. If nil, returns SkippedOne.
	onSelectOne func(q interaction.ChoiceQuestion) interaction.ChoiceAnswer
	// onAskText handles AskText calls. If nil, returns SkippedOne.
	onAskText func(q interaction.TextQuestion) interaction.TextAnswer

	// Call recordings (tests inspect these after the service call).
	selectOneCalls []interaction.ChoiceQuestion
	askTextCalls   []interaction.TextQuestion
	notifyCalls    []interaction.Notice
}

func (s *scriptedInteraction) SelectOne(_ context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	s.selectOneCalls = append(s.selectOneCalls, q)
	if s.onSelectOne != nil {
		return s.onSelectOne(q), nil
	}
	return interaction.ChoiceAnswer{Status: interaction.SkippedOne}, nil
}

func (s *scriptedInteraction) SelectMany(_ context.Context, q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.SkippedOne}, nil
}

func (s *scriptedInteraction) AskText(_ context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	s.askTextCalls = append(s.askTextCalls, q)
	if s.onAskText != nil {
		return s.onAskText(q), nil
	}
	return interaction.TextAnswer{Status: interaction.SkippedOne}, nil
}

func (s *scriptedInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.SkippedOne}, nil
}

func (s *scriptedInteraction) Notify(_ context.Context, n interaction.Notice) {
	s.notifyCalls = append(s.notifyCalls, n)
}

func (s *scriptedInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// askTextCalledWith reports whether AskText was called with the given question ID.
func (s *scriptedInteraction) askTextCalledWith(id interaction.QuestionID) bool {
	for _, q := range s.askTextCalls {
		if q.ID == id {
			return true
		}
	}
	return false
}

// selectOneCalledWith reports whether SelectOne was called with the given question ID.
func (s *scriptedInteraction) selectOneCalledWith(id interaction.QuestionID) bool {
	for _, q := range s.selectOneCalls {
		if q.ID == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Shared test constants and event builders.
// ---------------------------------------------------------------------------

const (
	appTestRunID      = "20260101T000000Z-abcd"
	appTestOrchFile   = "/test-logs/20260101T000000Z-abcd/00_orchestrator_events.jsonl"
	appTestAgent1File = "/test-logs/20260101T000000Z-abcd/TestAgent#1/03_events.jsonl"
	appTestAgent2File = "/test-logs/20260101T000000Z-abcd/TestAgent#2/03_events.jsonl"
)

var appTestRunRef = domain.NamedRun(appTestRunID)

// appTestInvEndEvent returns an invocation_end event with the given agent,
// model and token usage.
func appTestInvEndEvent(id domain.AgentInstanceID, model domain.ModelID, usage domain.TokenUsage) domain.Event {
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

// appTestTurnEvent returns an assistant turn event with the given model and usage.
func appTestTurnEvent(model domain.ModelID, usage domain.TokenUsage) domain.Event {
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

// appTestRunEndEvent returns a run_end event.
func appTestRunEndEvent() domain.Event {
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

// flatPricing returns a ModelPricing with no long-context threshold.
// Rates: input="3.00"/M, cachedInput="0.30"/M, cacheWrite="3.75"/M, output="15.00"/M.
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

// oneRunTwoAgentInventory builds an Inventory with one named run containing
// an orchestrator file and two agent-instance files. Used in analysis tests.
func oneRunTwoAgentInventory(src domain.Source) domain.Inventory {
	return domain.Inventory{
		Source: src,
		Runs: []domain.RunEntry{
			{
				Run:              appTestRunRef,
				Dir:              "/test-logs/20260101T000000Z-abcd",
				OrchestratorFile: appTestOrchFile,
				Agents: []domain.AgentEntry{
					{
						Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
						InstanceHint: "TestAgent#1",
						EventFile:    appTestAgent1File,
					},
					{
						Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#2",
						InstanceHint: "TestAgent#2",
						EventFile:    appTestAgent2File,
					},
				},
			},
		},
	}
}
