package domain

import (
	"errors"
	"fmt"
)

// ArtifactKind classifies a catalog artifact by type.
type ArtifactKind string

const (
	ArtifactAgent ArtifactKind = "agent"
	ArtifactSkill ArtifactKind = "skill"
	ArtifactHook  ArtifactKind = "hook"
)

// AgentRole describes the role an agent fulfils within the system.
type AgentRole string

const (
	// RoleSubagent is the role for worker agents. Its value aligns with the frontmatter
	// vocabulary ("subagent") and with ProtocolVariant's existing "subagent" value.
	RoleSubagent     AgentRole = "subagent"
	RoleOrchestrator AgentRole = "orchestrator"
	// RoleUtility marks an agent deployed as a shared utility. It is a valid frontmatter
	// role value; agents carrying this role receive harness transformation but no bundle
	// content and no protocol version marker. RoleUtility is a first-class frontmatter
	// value — any source file may declare role: utility.
	RoleUtility AgentRole = "utility"

	// RoleStandalone marks an agent deployed on its own, outside any workflow. It receives
	// harness transformation but no bundle content, no protocol marker, and no bundle stamp.
	RoleStandalone AgentRole = "standalone"

	// RoleWorker is a backward-compatible alias for RoleSubagent. New code must use
	// RoleSubagent; this alias keeps existing call sites compiling during the transition.
	RoleWorker = RoleSubagent
)

// ParseAgentRole maps a frontmatter `role` scalar to an AgentRole.
// Accepted values: "subagent", "orchestrator", "utility", "standalone".
// ok is false for every other value, including "worker", the empty string, and any
// unrecognised string. An unrecognised value is reported by the caller, never coerced.
func ParseAgentRole(s string) (AgentRole, bool) {
	switch AgentRole(s) {
	case RoleSubagent:
		return RoleSubagent, true
	case RoleOrchestrator:
		return RoleOrchestrator, true
	case RoleUtility:
		return RoleUtility, true
	case RoleStandalone:
		return RoleStandalone, true
	default:
		return "", false
	}
}

// RoleCarriesVersionMarkers reports whether an agent of this role is expected to carry a
// protocol version marker and a bundle version stamp in its deployed form.
//
// False for RoleUtility and RoleStandalone: those roles receive harness transformation only,
// so the absence of either marker is by design and must never be reported as staleness.
// True for every other role, including unrecognised ones (fail-closed: an unknown role is
// treated as a fully-managed agent).
func RoleCarriesVersionMarkers(role AgentRole) bool {
	return role != RoleUtility && role != RoleStandalone
}

// ArtifactRef identifies one catalog artifact. Key is a slug, unique within Kind.
// Agent slugs are the value that referenced_agents fields use (CD-3).
type ArtifactRef struct {
	Kind ArtifactKind
	Key  string
}

// String returns a stable, human-readable representation of the form "agent:test-runner".
// It is suitable for use as a map key and as a log subject.
func (r ArtifactRef) String() string {
	return fmt.Sprintf("%s:%s", r.Kind, r.Key)
}

// Scope selects the deployment root. MOSAIC deploys into the project workspace only.
type Scope string

const (
	ScopeProject Scope = "project"
)

// ErrUnsupportedScope is returned by every target-path resolver when asked for a scope
// other than ScopeProject. Declared here alongside ScopeProject so all resolvers can
// return a single sentinel, enabling conformance tests to assert all five implementations
// identically with errors.Is.
var ErrUnsupportedScope = errors.New("unsupported deployment scope")
