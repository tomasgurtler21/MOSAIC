package app

import (
	"context"

	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

// readAllStreams reads every event file discovered in inv and returns the
// decoded streams together with any findings raised during reading. Reading
// stops and an error is returned only when the context is cancelled or the
// visitor callback returns an error.
func (s *Service) readAllStreams(ctx context.Context, inv domain.Inventory) ([]analysis.Stream, []domain.Finding, error) {
	var streams []analysis.Stream
	var findings []domain.Finding

	addStream := func(ref domain.StreamRef, path string) error {
		events, rf, err := s.readFile(ctx, path)
		findings = append(findings, rf...)
		if err != nil {
			return err
		}
		streams = append(streams, analysis.Stream{Ref: ref, Events: events})
		return nil
	}

	for _, run := range inv.Runs {
		if run.OrchestratorFile != "" {
			ref := domain.StreamRef{
				Run:  run.Run,
				Kind: domain.StreamOrchestrator,
				Path: run.OrchestratorFile,
			}
			if err := addStream(ref, run.OrchestratorFile); err != nil {
				return streams, findings, err
			}
		}
		for _, agent := range run.Agents {
			if agent.EventFile != "" {
				ref := domain.StreamRef{
					Run:          run.Run,
					Kind:         domain.StreamAgentInstance,
					InstanceHint: agent.InstanceHint,
					Path:         agent.EventFile,
				}
				if err := addStream(ref, agent.EventFile); err != nil {
					return streams, findings, err
				}
			}
		}
	}

	if inv.Unattributable != nil {
		unattr := inv.Unattributable
		if unattr.OrchestratorFile != "" {
			ref := domain.StreamRef{
				Run:  unattr.Run,
				Kind: domain.StreamOrchestrator,
				Path: unattr.OrchestratorFile,
			}
			if err := addStream(ref, unattr.OrchestratorFile); err != nil {
				return streams, findings, err
			}
		}
		for _, agent := range unattr.Agents {
			if agent.EventFile != "" {
				ref := domain.StreamRef{
					Run:          unattr.Run,
					Kind:         domain.StreamAgentInstance,
					InstanceHint: agent.InstanceHint,
					Path:         agent.EventFile,
				}
				if err := addStream(ref, agent.EventFile); err != nil {
					return streams, findings, err
				}
			}
		}
	}

	return streams, findings, nil
}

// readFile reads one JSONL event file using the injected EventReader, collecting
// decoded events into a slice. It is the only place in the app layer where file
// I/O is performed.
func (s *Service) readFile(ctx context.Context, path string) ([]domain.Event, []domain.Finding, error) {
	var events []domain.Event
	findings, err := s.deps.Reader.Read(ctx, path, func(ev domain.Event) error {
		events = append(events, ev)
		return nil
	})
	return events, findings, err
}
