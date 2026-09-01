package main

import (
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
)

// buildDeps constructs the session's routing consultant, manual resolver,
// pre-consultation capability and approval reader from the run's settings.
// It is the single dependency builder for both frontends: the non-interactive
// path calls it with settings derived from the pre-scanned CLI flags, the
// interactive path with the settings carried by the completed configuration
// selection. Neither frontend constructs a consultant of its own.
//
// The orchestrator reference and the routing table are deliberately absent:
// neither is known at this point on either frontend. Both reach the
// consultants later, through domain.RunContextBinder, on the session's
// run-start path.
//
//   - settings.Mode selects which consultant is wired as Deps.Routing.
//   - settings.ManualResolution controls whether a ManualResolver is wired
//     as Deps.Manual.
//   - settings.PreConsultation controls whether the OrchestratorConsultant is
//     also wired as Deps.PreConsult (it implements both ports).
//   - invoker is the consultation transport used by OrchestratorConsultant.
//   - interact is the Interaction port used by ManualResolver.
//   - approvals is the HITL approval reader. Both frontends pass the real
//     reader; a nil value is normalised by session.New as it is today.
func buildDeps(
	settings domain.RunSettings,
	invoker domain.RawInvoker,
	interact domain.Interaction,
	approvals domain.ApprovalReader,
	dispLogger domain.DispatchLogger,
) session.Deps {
	consultant := &deviation.OrchestratorConsultant{
		Invoker:        invoker,
		DispatchLogger: dispLogger,
	}

	deps := session.Deps{
		Routing:   consultant,
		Approvals: approvals,
	}

	if settings.ManualResolution {
		deps.Manual = &deviation.ManualResolver{
			Interact: interact,
		}
	}

	if settings.PreConsultation {
		deps.PreConsult = consultant
	}

	return deps
}
