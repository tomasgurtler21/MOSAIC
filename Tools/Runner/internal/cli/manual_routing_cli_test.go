package cli_test

// Manual routing driven by the real non-interactive Runner CLI Interaction
// (manual resolution can be enabled from the CLI): it must end, never block
// and never dispatch.

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/cli"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// countingInteraction counts the question calls that reach the wrapped
// Interaction.
type countingInteraction struct {
	interaction.Interaction
	questions int
}

func (c *countingInteraction) SelectOne(ctx context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	c.questions++
	return c.Interaction.SelectOne(ctx, q)
}

func (c *countingInteraction) SelectMany(ctx context.Context, q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	c.questions++
	return c.Interaction.SelectMany(ctx, q)
}

func (c *countingInteraction) AskText(ctx context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	c.questions++
	return c.Interaction.AskText(ctx, q)
}

// manualRoutingTable is a two-row table: a non-staged row and a staged row.
func manualRoutingTable() domain.RoutingTable {
	return domain.RoutingTable{Rows: []domain.RoutingRow{
		{Index: 0, Phase: "RESEARCH", Agent: "researcher", PhaseParsed: domain.PhaseParsed{Name: "RESEARCH"}},
		{Index: 1, Phase: "EXECUTION.Implementation.[StageNumber]", Agent: "dev",
			PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}},
	}}
}

func TestManualRouting_WithTheRealCLIInteraction_EndsWithoutDispatchAndWithoutBlocking(t *testing.T) {
	var out bytes.Buffer
	in := &countingInteraction{Interaction: cli.NewInteraction(&out)}
	m := &deviation.ManualResolver{Interact: in, Table: manualRoutingTable()}
	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
		Stages:                &domain.StageSet{Entries: []domain.StageEntry{{Number: 1}, {Number: 2}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	instr, err := m.ConsultRouting(ctx, req)

	if ctx.Err() != nil {
		t.Fatalf("manual routing did not end before the deadline: %v", ctx.Err())
	}
	var ce *domain.ConsultationError
	if !errors.As(err, &ce) || ce.Failure != domain.ConsultFailInteractionUnavailable {
		t.Fatalf("want a %q consultation failure, got %v", domain.ConsultFailInteractionUnavailable, err)
	}
	if instr.Dispatch != nil || instr.Stop != nil {
		t.Errorf("want no instruction, got %+v", instr)
	}
	if in.questions < 1 || in.questions > deviation.ManualPromptLimit {
		t.Errorf("want a bounded number (1..%d) of Interaction calls, got %d", deviation.ManualPromptLimit, in.questions)
	}
}
