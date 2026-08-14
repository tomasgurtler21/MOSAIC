package plan

import "mosaic-deploy/internal/domain"

// ProtocolDeltaField is the VersionDelta.Field name used for protocol staleness. It is
// distinct from every frontmatter stamp field name and from the workflow delta prefix, so it
// can never be mistaken for a manifest version stamp. The executor's resolveVersionStamp
// switch does not handle this field — protocol_version is not a frontmatter stamp.
const ProtocolDeltaField = "protocol_version"

// ProtocolDrift records the difference between the deployed protocol version and the current
// protocol source version. The zero value means no drift.
type ProtocolDrift struct {
	// Deployed is the protocol version read from the deployed file's
	// <CommunicationProtocol type="managed"> opening tag's version attribute. "" when absent.
	Deployed string
	// Source is the current protocol source version from the protocol document frontmatter.
	Source string
}

// ProtocolStaleness compares a deployed artifact's protocol version against the source
// version. An empty deployed version is always stale — agents deployed before the protocol
// marker existed carry no version and must be picked up.
//
// Applies only to agents whose role carries version markers (RoleSubagent and RoleOrchestrator).
// The caller is responsible for gating the call via domain.RoleCarriesVersionMarkers; this
// function is a pure comparison and does not consult the agent role itself. Unlike
// WorkflowStaleness, which is restricted to orchestrators, this function applies to all managed
// roles — but not to RoleUtility or RoleStandalone, whose deployed files carry no protocol
// marker by design.
func ProtocolStaleness(deployed domain.DeployedArtifactState, sourceVersion string) ProtocolDrift {
	return ProtocolDrift{
		Deployed: deployed.ProtocolVersion,
		Source:   sourceVersion,
	}
}

// Stale reports whether the deployed and source protocol versions differ.
// An empty deployed version is always stale — agents deployed before the protocol
// marker existed carry no version and must be picked up.
func (d ProtocolDrift) Stale() bool {
	return d.Deployed != d.Source
}

// Delta returns the VersionDelta for the drift; ok is false when not stale.
// The returned delta's Field is always ProtocolDeltaField.
func (d ProtocolDrift) Delta() (domain.VersionDelta, bool) {
	if !d.Stale() {
		return domain.VersionDelta{}, false
	}
	return domain.VersionDelta{
		Field:    ProtocolDeltaField,
		Deployed: d.Deployed,
		Source:   d.Source,
	}, true
}

// Reason returns a one-line human-readable string naming the protocol and its version change.
// Returns "" when not stale.
//
// Format examples:
//
//	"protocol version changed (1.7 -> 1.9)"
//	"deployed file carries no protocol version marker (source 1.9)"
func (d ProtocolDrift) Reason() string {
	if !d.Stale() {
		return ""
	}
	if d.Deployed == "" {
		return "deployed file carries no protocol version marker (source " + d.Source + ")"
	}
	return "protocol version changed (" + d.Deployed + " -> " + d.Source + ")"
}
