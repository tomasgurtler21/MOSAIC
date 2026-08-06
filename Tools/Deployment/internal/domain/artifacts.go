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
	// RoleUtility is an internal marker only; it is never a frontmatter role value.
	RoleUtility AgentRole = "utility"

	// RoleWorker is a backward-compatible alias for RoleSubagent. New code must use
	// RoleSubagent; this alias keeps existing call sites compiling during the transition.
	RoleWorker = RoleSubagent
)

// ParseAgentRole maps a frontmatter `role` scalar to an AgentRole.
// ok is false for any value other than "subagent" or "orchestrator" — including
// "utility" (internal marker only), "worker" (pre-Stage 4 internal value), the
// empty string, and any unrecognised string. An unrecognised value is reported by
// the caller rather than silently coerced.
func ParseAgentRole(s string) (AgentRole, bool) {
	switch AgentRole(s) {
	case RoleSubagent:
		return RoleSubagent, true
	case RoleOrchestrator:
		return RoleOrchestrator, true
	default:
		return "", false
	}
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
