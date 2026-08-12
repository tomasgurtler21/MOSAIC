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

	// GapParkedCustomRegion reports [[CUSTOM:]] regions relocated to the end of the
	// deployed file because a schema reorder left them without a surviving anchor.
	// The content is in the deployed file, not lost; the user must move it to the
	// correct section. One gap is produced per transform, listing all parked names.
	GapParkedCustomRegion GapKind = "parked-custom-region"

	// GapCustomRegionFallthrough reports [[CUSTOM:]] regions whose recorded parent could not
	// be resolved in the output document — typically because the source removed the parent
	// section — and which were therefore placed at body level. The content is in the deployed
	// file, not lost; the user must move it to the correct section. One gap is produced per
	// transform, listing all fallthrough names in sorted order. Distinct from
	// GapParkedCustomRegion, which covers the schema-reorder case.
	GapCustomRegionFallthrough GapKind = "custom-region-fallthrough"

	// GapInjectionReparented reports a source [[INJECTION:]] region that existed in the
	// previously deployed file and now sits under a different parent. Advisory only: content
	// is preserved verbatim at the new location and the transform is unaffected. One gap is
	// produced per re-parented injection.
	GapInjectionReparented GapKind = "injection-reparented"

	// GapDeployedRegionContentChanged reports a [[DEPLOYED:]] region whose canonical content
	// changed relative to the previously deployed file and which contains nested user-owned
	// regions, whose content survived regeneration untouched and may now contradict or
	// duplicate the updated canonical text. One gap is produced per affected region.
	GapDeployedRegionContentChanged GapKind = "deployed-region-content-changed"
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
