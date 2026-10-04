package mosaiclog

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"mosaic-run/internal/domain"
)

func isVocabulary(o Outcome) bool {
	switch o {
	case OutcomeCompleted, OutcomeStopped, OutcomeFailed, OutcomeAborted, OutcomeInterrupted:
		return true
	}
	return false
}

func TestOutcomeForRun_PinnedRows(t *testing.T) {
	tests := []struct {
		name   string
		status domain.RunStatus
		err    error
		ctxErr error
		want   Outcome
	}{
		{"cancelled context beats completed", domain.RunCompleted, nil, context.Canceled, OutcomeInterrupted},
		{"cancelled context beats refused", domain.RunRefused, nil, context.Canceled, OutcomeInterrupted},
		{"deadline context is an interruption", domain.RunStopped, nil, context.DeadlineExceeded, OutcomeInterrupted},
		{"returned cancellation error", "", context.Canceled, nil, OutcomeInterrupted},
		{"wrapped cancellation error", "", fmt.Errorf("session: %w", context.Canceled), nil, OutcomeInterrupted},
		{"refusal", domain.RunRefused, nil, nil, OutcomeAborted},
		{"completed", domain.RunCompleted, nil, nil, OutcomeCompleted},
		{"graceful stop", domain.RunStopped, nil, nil, OutcomeStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OutcomeForRun(domain.RunOutcome{Status: tt.status}, tt.err, tt.ctxErr)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOutcomeForRun_NonCancellationErrorIsNotAnInterruption(t *testing.T) {
	got := OutcomeForRun(domain.RunOutcome{}, errors.New("infrastructure"), nil)

	if got == OutcomeInterrupted || !isVocabulary(got) {
		t.Fatalf("got %q, want a non-interrupted vocabulary value", got)
	}
}

func TestOutcomeForRun_EveryStatusYieldsVocabularyValue(t *testing.T) {
	statuses := []domain.RunStatus{
		domain.RunCompleted, domain.RunStopped, domain.RunDeviationUnresolved,
		domain.RunRefused, domain.RunFailed, domain.RunStoppedByConsultant,
		domain.RunStartFailed, "some-future-status", "",
	}
	for _, s := range statuses {
		got := OutcomeForRun(domain.RunOutcome{Status: s}, nil, nil)
		if !isVocabulary(got) {
			t.Errorf("status %q -> %q, want a run_end outcome vocabulary value", s, got)
		}
	}
}
