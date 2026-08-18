package deviation

import "mosaic-run/internal/domain"

// BindRunContext implements domain.RunContextBinder on OrchestratorConsultant.
//
// The session calls this exactly once per run, after the routing table is
// parsed and the orchestrator is resolved, and before the artifact is created
// or any consultation is issued. It supplies the two facts the composition
// root could not provide at construction time.
func (c *OrchestratorConsultant) BindRunContext(rc domain.RunContext) {
	c.Orchestrator = rc.Orchestrator
	c.Table = rc.Table
}

// BindRunContext implements domain.RunContextBinder on ManualResolver.
//
// The session calls this exactly once per run, after the routing table is
// parsed, and before any consultation is issued. It supplies the routing
// table that ManualResolver uses to populate the option list.
func (m *ManualResolver) BindRunContext(rc domain.RunContext) {
	m.Table = rc.Table
}
