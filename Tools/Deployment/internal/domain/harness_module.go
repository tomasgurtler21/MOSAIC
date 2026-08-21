package domain

import "errors"

// HarnessModule is everything that differs between harnesses. Implementations must be
// deterministic and free of hidden state: identical arguments produce identical results.
// Only the registry constructs one; only transform, plan, and deploy consume one.
// Built-in, descriptor-only, and external implementations are indistinguishable to every consumer.
type HarnessModule interface {
	// Ref returns identity and provenance. Never fails.
	Ref() HarnessRef

	// Descriptor returns the declarative half. The returned value must be treated as read-only.
	Descriptor() *HarnessDescriptor

	// Tools maps and renders an agent's generic tool list into harness frontmatter fields.
	// Every entry in req.Generic must appear exactly once in the returned Resolutions.
	Tools(req ToolRequest) (ToolResult, error)

	// Frontmatter returns the ordered field operations to apply to a parsed generic frontmatter.
	// The module decides adds, drops, and final key order; it never edits a document itself.
	Frontmatter(req FrontmatterRequest) (FrontmatterPlan, error)

	// TargetPath returns the deployment path for one artifact, relative to the deployment root.
	// It includes the filename and the harness's extension rule. Returns ErrArtifactUnsupported
	// when the harness has no deployment path for the requested artifact kind. Returns
	// ErrUnsupportedScope when req.Scope is not ScopeProject.
	TargetPath(req TargetPathRequest) (string, error)

	// Injection returns the harness-level content for a canonical injection name.
	// When req.AgentKey is "orchestrator", the returned content is the shared
	// (subagent-level) content merged with any orchestrator-only content for the
	// same injection name — shared content first, then a blank-line separator,
	// then orchestrator-only content. When only one source has content, no
	// separator is added. When neither source has content for the name, ok is false.
	// For all other agent keys, only shared content is returned.
	// ok is false for injections this harness does not fill (which are left empty).
	Injection(req InjectionRequest) (content string, ok bool)

	// HookPlan resolves a hook bundle for this harness, including variant reuse and registration.
	HookPlan(req HookPlanRequest) (HookPlan, error)

	// Close releases any resource the provision tier holds (a child process, for the external
	// tier). Built-in and descriptor-only implementations return nil.
	Close() error
}

// InjectionRequest is the input to HarnessModule.Injection. It mirrors the
// FrontmatterRequest pattern: a struct that carries context about the requesting
// agent alongside the query parameter, enabling future extension without
// signature changes.
type InjectionRequest struct {
	Name     string // canonical injection name, e.g. "HarnessConstraints"
	AgentKey string // artifact slug of the requesting agent, e.g. "orchestrator"
}

// FrontmatterRequest is the input to HarnessModule.Frontmatter.
type FrontmatterRequest struct {
	Kind       ArtifactKind
	AgentKey   string
	Source     []FrontmatterField // the generic frontmatter, in source order
	Model      ModelSelection
	ToolFields []FrontmatterField // output of Tools
	Versions   VersionStamps
	Role       AgentRole // deploying agent's role; zero value triggers fallback (omit role-conditional fields)
}

// VersionStamps carries the version fields stamped into every deployed agent.
type VersionStamps struct {
	Version                       string // carried through from the source, unchanged
	HarnessVersion                string // transform engine version (agents only); written as mosaic_harness_version
	InjectionsVersion             string // role-selected injection version; for orchestrators, populated with desc.OrchestratorInjectionsVersion
	OrchestratorInjectionsVersion string // reserved; no longer populated after Stage 3 — role-conditional comparison uses InjectionsVersion
	ToolMappingsVersion           string // hash of the effective tool-destination mapping set; empty when no config mappings
}

// FrontmatterPlan is a declarative edit list that transform applies via docformat. The module
// never touches a Document, keeping the port implementable over a process boundary.
type FrontmatterPlan struct {
	Set      []FrontmatterField // add or overwrite, in this order
	Remove   []string
	KeyOrder []string // full desired order; unlisted keys keep relative source order
}

// TargetPathRequest is the input to HarnessModule.TargetPath.
type TargetPathRequest struct {
	Kind     ArtifactKind
	Key      string // artifact slug
	FileName string // source file name, for artifacts whose name is not derived from Key
	Scope    Scope
	GOOS     string // passed explicitly so path resolution stays testable on any platform
}

// ErrArtifactUnsupported is returned by HarnessModule.TargetPath when the harness has no
// deployment path for the requested artifact kind.
var ErrArtifactUnsupported = errors.New("artifact kind not supported by this harness")

// ErrDescriptorCapability is returned when a descriptor declares behaviour a provision tier
// cannot provide (e.g. a built-in-only method requested of a descriptor-only harness).
var ErrDescriptorCapability = errors.New("descriptor declares behaviour this tier cannot provide")
