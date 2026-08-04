package domain

import "mosaic-common/mosaic"

// InjectionClass type alias — domain.InjectionClass IS mosaic.InjectionClass.
type InjectionClass = mosaic.InjectionClass

// Re-declared constants pointing at the mosaic-common originals.
const (
	InjectionHarness        = mosaic.InjectionHarness
	InjectionProject        = mosaic.InjectionProject
	InjectionWorkflow       = mosaic.InjectionWorkflow
	InjectionInfrastructure = mosaic.InjectionInfrastructure
	InjectionProtocol       = mosaic.InjectionProtocol
)

// ProvisionTier indicates how a harness implementation is provided.
type ProvisionTier string

const (
	TierBuiltin    ProvisionTier = "builtin"
	TierDescriptor ProvisionTier = "descriptor"
	TierExternal   ProvisionTier = "external"
)

// HarnessRef is the identity and provenance of a harness, as shown in the TUI and the log.
type HarnessRef struct {
	ID             string
	DisplayName    string
	Tier           ProvisionTier
	SourcePath     string // descriptor/module path; empty for builtin
	ExecutablePath string // external tier only
	RequiresOptIn  bool
	Usable         bool   // false when gated by missing opt-in or a load error
	UnusableReason string // human-readable; empty when Usable
}

// HarnessDescriptor is the declarative half of the harness contract. It carries no struct tags
// (CD-1): the harness/descriptor package maps YAML onto this type using an unexported wire type.
type HarnessDescriptor struct {
	SchemaVersion                 string
	ID                            string
	DisplayName                   string
	TransformVersion              string // stamped into every deployed agent
	InjectionsVersion             string // stamped into every deployed agent
	OrchestratorInjectionsVersion string // stamped into the orchestrator's deployed file only; populated at module construction time from HarnessInjectionsOrchestrator.md frontmatter
	Models                        ModelCatalog
	Tools                         ToolSpec
	Paths                         PathSpec
	Extensions                    map[ArtifactKind]string // e.g. ArtifactAgent -> ".agent.md"
	Frontmatter                   FrontmatterSpec
	Injections                    []InjectionContent
	Hooks                         HookSupport
}

// ModelCatalog lists the models a harness exposes to the user during model selection.
type ModelCatalog struct {
	IDs        []string // presented in declared order; may be empty
	FormatHint string   // e.g. "provider/model-id"; display only, never enforced
}

// PathSpec holds deployment path specifications for each artifact kind.
type PathSpec struct {
	Agents ScopedPaths
	Skills ScopedPaths
	Hooks  ScopedPaths
}

// ScopedPaths holds path templates relative to the deployment root (project scope).
type ScopedPaths struct {
	Supported bool
	Project   string
}

// FrontmatterSpec declares the field operations a harness applies to every agent frontmatter.
type FrontmatterSpec struct {
	ModelKey string             // key the model is written to; empty = model not emitted
	ToolsKey string             // key the rendered tools value is written to; empty = module decides
	Add      []FrontmatterField // fields added with fixed values
	Drop     []string           // keys removed from the generic source
	KeyOrder []string           // output order; unlisted keys keep source-relative order, appended after
}

// InjectionContent is one injection point's harness-level content, keyed by the canonical name.
type InjectionContent struct {
	Name    string // canonical injection name, e.g. "HarnessConstraints"
	Content string // verbatim body to place between the boundary tags; may be empty
}
