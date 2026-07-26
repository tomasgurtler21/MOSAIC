package domain

// GapKind identifies the type of unresolved decision or manual step a gap represents.
type GapKind string

const (
	GapNoModel             GapKind = "no-model"
	GapUnmappedTool        GapKind = "unmapped-tool"
	GapEmptyInjection      GapKind = "empty-project-injection"
	GapRemovedInjection    GapKind = "removed-injection"    // injection point gone from source; content preserved in backup only
	GapSkippedFile         GapKind = "skipped-file"
	GapHookRegistration    GapKind = "hook-registration"
	GapManualStep          GapKind = "manual-step"
	GapFallbackLocation    GapKind = "fallback-location"
	GapUnsupportedArtifact GapKind = "unsupported-artifact" // e.g. hooks requested for a harness with none
)

// Gap is produced by transform, plan, and deploy when a decision could not be made automatically.
// It is the only input to TodoItem, so a component that reports a gap never needs to know how the
// checklist is rendered.
type Gap struct {
	Kind     GapKind
	Subject  string // agent key, tool name, file path, injection name
	Detail   string
	Fragment string // optional literal content the user must paste somewhere
}

// TodoCategory groups todo items for display and for the generated checklist file.
type TodoCategory string

const (
	TodoModels       TodoCategory = "Models"
	TodoToolMappings TodoCategory = "Tool mappings"
	TodoInjections   TodoCategory = "Project-specific injections"
	TodoSkippedFiles TodoCategory = "Skipped files"
	TodoRegistration TodoCategory = "Hook registration"
	TodoManual       TodoCategory = "Manual steps"
	TodoEnvironment  TodoCategory = "Deployment location"
)

// TodoItem is one action the user must take after the deployment run completes.
type TodoItem struct {
	Category TodoCategory
	Subject  string
	Detail   string
	Fragment string
}
