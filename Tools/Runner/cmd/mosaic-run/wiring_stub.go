package main

import (
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
)

// buildDeps selects and constructs the session's routing consultant and manual
// resolver from the pre-scanned CLI flags. It follows the same composition-root
// pattern as buildAdapter: the flag strings and booleans have already been
// extracted from os.Args before cobra runs, so the correct consultant types
// are in place before the session is constructed.
//
//   - modeStr selects which RoutingConsultant is wired as Deps.Routing.
//   - manualResolution controls whether ManualResolver is wired as Deps.Manual.
//   - preConsult controls whether the OrchestratorConsultant is also wired as
//     Deps.PreConsult (it implements both RoutingConsultant and PreConsultant).
//   - invoker is the raw-JSON transport used by OrchestratorConsultant.
//   - orchestrator is the resolved orchestrator AgentReference.
//   - table is the workflow's routing table.
//   - interact is the Interaction port used by ManualResolver.
func buildDeps(
	modeStr string,
	manualResolution bool,
	preConsult bool,
	invoker domain.RawInvoker,
	orchestrator domain.AgentReference,
	table domain.RoutingTable,
	interact domain.Interaction,
) session.Deps {
	consultant := &deviation.OrchestratorConsultant{
		Invoker:      invoker,
		Orchestrator: orchestrator,
		Table:        table,
	}

	deps := session.Deps{
		Routing: consultant,
	}

	if manualResolution {
		deps.Manual = &deviation.ManualResolver{
			Interact: interact,
			Table:    table,
		}
	}

	if preConsult {
		deps.PreConsult = consultant
	}

	return deps
}
