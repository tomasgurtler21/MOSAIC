package domain

import "fmt"

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
	RoleWorker       AgentRole = "worker"
	RoleOrchestrator AgentRole = "orchestrator"
	RoleUtility      AgentRole = "utility"
)

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

// Scope selects between project-scoped and user-scoped deployment paths.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeUser    Scope = "user"
)
