package tui

// fakes_test.go provides minimal fake domain port implementations used by
// navigation tests that require a real *app.Service instance.

import (
	"context"
	"time"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// tuiFakePricingStore is a minimal in-memory PricingStore for TUI tests.
type tuiFakePricingStore struct{ path string }

func (f *tuiFakePricingStore) Path() string { return f.path }

func (f *tuiFakePricingStore) Load(_ context.Context) (domain.PricingTable, []domain.Finding, error) {
	return domain.EmptyPricingTable(), nil, nil
}

func (f *tuiFakePricingStore) Put(_ context.Context, _ domain.ModelPricing) error { return nil }

// tuiFakeLogSource is a minimal LogSource that always reports SourceNotFound.
type tuiFakeLogSource struct{}

func (f *tuiFakeLogSource) Default(_ string) domain.Source {
	return domain.Source{Kind: domain.SourceNotFound}
}

func (f *tuiFakeLogSource) Classify(_ string) domain.Source {
	return domain.Source{Kind: domain.SourceNotFound}
}

func (f *tuiFakeLogSource) Enumerate(src domain.Source) (domain.Inventory, []domain.Finding) {
	return domain.Inventory{Source: src}, nil
}

// tuiFakeEventReader is a minimal EventReader that delivers no events.
type tuiFakeEventReader struct{}

func (f *tuiFakeEventReader) Read(_ context.Context, _ string, _ func(domain.Event) error) ([]domain.Finding, error) {
	return nil, nil
}

// tuiFakeClock returns a fixed point in time.
type tuiFakeClock struct{}

func (f *tuiFakeClock) Now() time.Time {
	return time.Date(2026, 8, 2, 7, 46, 35, 0, time.UTC)
}

// tuiFakeInteraction is a minimal Interaction that always skips without blocking.
type tuiFakeInteraction struct{}

func (f *tuiFakeInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.SkippedOne}, nil
}

func (f *tuiFakeInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.SkippedOne}, nil
}

func (f *tuiFakeInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.SkippedOne}, nil
}

func (f *tuiFakeInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.SkippedOne}, nil
}

func (f *tuiFakeInteraction) Notify(_ context.Context, _ interaction.Notice)          {}
func (f *tuiFakeInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// newServiceWithPricingPath returns an *app.Service whose PricingPath() returns
// pricingPath. Intended for navigation tests that need a non-nil Service
// to call PricingPath() without panicking; not intended for full analysis flows.
func newServiceWithPricingPath(pricingPath string) *app.Service {
	return app.New(app.Deps{
		Source:      &tuiFakeLogSource{},
		Reader:      &tuiFakeEventReader{},
		Pricing:     &tuiFakePricingStore{path: pricingPath},
		Clock:       &tuiFakeClock{},
		Interaction: &tuiFakeInteraction{},
	})
}
