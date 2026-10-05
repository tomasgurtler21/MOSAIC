package plan

import (
	"fmt"
	"slices"
	"strings"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// InfrastructureDeltaFieldPrefix prefixes the Field of every infrastructure-drift VersionDelta.
const InfrastructureDeltaFieldPrefix = "infrastructure:"

// InfrastructureDeclarationIntent is the caller's intent for the infrastructure declarations
// of orchestrator-role items.
type InfrastructureDeclarationIntent struct {
	// EnsureKeys must end up declared and current in every orchestrator-role item.
	EnsureKeys []string
	// RefreshDeclared refreshes the catalog-backed keys each item already declares.
	RefreshDeclared bool
}

// IsZero reports whether no infrastructure drift should be evaluated.
func (i InfrastructureDeclarationIntent) IsZero() bool {
	return len(i.EnsureKeys) == 0 && !i.RefreshDeclared
}

// InfrastructureDeclarationChange describes one stale declared key.
type InfrastructureDeclarationChange struct {
	Key             string
	DeployedVersion string
	SourceVersion   string
	Fields          []string // subset of "version","class","triggers","on_failure", in that order
}

// InfrastructureDrift is the declaration drift of one orchestrator-role file.
type InfrastructureDrift struct {
	Missing         []string
	Stale           []InfrastructureDeclarationChange
	missingVersions map[string]string
}

// InfrastructureStaleness computes declaration drift for ONE orchestrator-role file.
// Declared keys with no infrastructure counterpart in the catalog, and declarations whose
// section did not parse, never produce drift.
func InfrastructureStaleness(declared domain.DeployedInfrastructureDeclarations, c catalog.Catalog, intent InfrastructureDeclarationIntent) InfrastructureDrift {
	var drift InfrastructureDrift
	if intent.IsZero() {
		return drift
	}

	checked := make(map[string]bool)
	checkDeclared := func(decl domain.DeployedInfrastructureDeclaration, src domain.Agent) {
		if checked[decl.Key] {
			return
		}
		checked[decl.Key] = true
		if fields := infrastructureChangedFields(decl, src); len(fields) > 0 {
			drift.Stale = append(drift.Stale, InfrastructureDeclarationChange{
				Key: decl.Key, DeployedVersion: decl.Version, SourceVersion: src.Version, Fields: fields,
			})
		}
	}

	for _, key := range intent.EnsureKeys {
		if checked[key] || slices.Contains(drift.Missing, key) {
			continue
		}
		src, ok := catalogInfrastructureAgent(c, key)
		if !ok {
			continue
		}
		decl, found := declared.Lookup(key)
		if !found {
			drift.Missing = append(drift.Missing, key)
			if drift.missingVersions == nil {
				drift.missingVersions = make(map[string]string)
			}
			drift.missingVersions[key] = src.Version
			continue
		}
		if decl.Parsed {
			checkDeclared(decl, src)
		}
	}

	if intent.RefreshDeclared {
		for _, decl := range declared {
			if !decl.Parsed {
				continue
			}
			if src, ok := catalogInfrastructureAgent(c, decl.Key); ok {
				checkDeclared(decl, src)
			}
		}
	}
	return drift
}

// catalogInfrastructureAgent looks key up in the catalog and requires an infrastructure class.
func catalogInfrastructureAgent(c catalog.Catalog, key string) (domain.Agent, bool) {
	agent, ok := c.Agent(key)
	if !ok || agent.Infrastructure == "" {
		return domain.Agent{}, false
	}
	return agent, true
}

// infrastructureChangedFields names the fields of decl that differ from the catalog agent.
func infrastructureChangedFields(decl domain.DeployedInfrastructureDeclaration, src domain.Agent) []string {
	var fields []string
	if decl.Version != src.Version {
		fields = append(fields, "version")
	}
	if decl.Class != src.Infrastructure {
		fields = append(fields, "class")
	}
	if !slices.Equal(decl.Triggers, src.Triggers) {
		fields = append(fields, "triggers")
	}
	if decl.OnFailure != src.OnFailure {
		fields = append(fields, "on_failure")
	}
	return fields
}

// IsStale reports whether any key drifted.
func (d InfrastructureDrift) IsStale() bool { return len(d.Missing) > 0 || len(d.Stale) > 0 }

// Keys returns Missing keys then Stale keys, without duplicates.
func (d InfrastructureDrift) Keys() []string {
	var keys []string
	keys = append(keys, d.Missing...)
	for _, s := range d.Stale {
		if !slices.Contains(keys, s.Key) {
			keys = append(keys, s.Key)
		}
	}
	return keys
}

// Deltas returns one delta per drifted key, Missing first.
func (d InfrastructureDrift) Deltas() []domain.VersionDelta {
	var deltas []domain.VersionDelta
	for _, key := range d.Missing {
		deltas = append(deltas, domain.VersionDelta{
			Field: InfrastructureDeltaFieldPrefix + key, Deployed: "", Source: d.missingVersions[key],
		})
	}
	for _, s := range d.Stale {
		deltas = append(deltas, domain.VersionDelta{
			Field: InfrastructureDeltaFieldPrefix + s.Key, Deployed: s.DeployedVersion, Source: s.SourceVersion,
		})
	}
	return deltas
}

// Reason returns a human-readable explanation, "" when there is no drift.
func (d InfrastructureDrift) Reason() string {
	var parts []string
	if len(d.Missing) > 0 {
		parts = append(parts, "infrastructure declarations missing: "+strings.Join(d.Missing, ", "))
	}
	if len(d.Stale) > 0 {
		items := make([]string, len(d.Stale))
		for i, s := range d.Stale {
			if slices.Equal(s.Fields, []string{"version"}) {
				items[i] = fmt.Sprintf("%s (version %s -> %s)", s.Key, s.DeployedVersion, s.SourceVersion)
			} else {
				items[i] = fmt.Sprintf("%s (%s)", s.Key, strings.Join(s.Fields, ", "))
			}
		}
		parts = append(parts, "infrastructure declarations stale: "+strings.Join(items, ", "))
	}
	return strings.Join(parts, "; ")
}

// infrastructureInput bundles what classifyAgentItem needs to evaluate declaration drift.
type infrastructureInput struct {
	Catalog catalog.Catalog
	Intent  InfrastructureDeclarationIntent
}

// orchestratorInfrastructureDrift evaluates declaration drift for one item. Only
// orchestrator-role agents are evaluated; every other role reports no drift.
func orchestratorInfrastructureDrift(agent domain.Agent, deployed domain.DeployedArtifactState, in infrastructureInput) InfrastructureDrift {
	if agent.Role != domain.RoleOrchestrator || in.Catalog == nil {
		return InfrastructureDrift{}
	}
	return InfrastructureStaleness(deployed.InfrastructureDeclarations, in.Catalog, in.Intent)
}

// filterEmpty returns the non-empty strings of parts, in order.
func filterEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
