// Package plan computes a domain.Plan describing every action a deployment run would take,
// without performing any writes. The Planner reads from the catalog and the manifest snapshot,
// delegates artifact resolution and staleness comparisons to exported helper functions, and
// returns a fully renderable plan that both frontends can display for user review before any
// file is touched.
package plan
