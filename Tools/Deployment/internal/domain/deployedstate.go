package domain

// DeployedArtifactState describes what is actually present on disk at one target path.
// It is produced by the app layer (which owns all I/O) and consumed by the planner, which
// uses it as the sole source of truth for version comparison and on-disk presence.
//
// The zero value represents a valid "absent" state, so map lookups for unknown paths behave
// correctly without a separate absence sentinel.
type DeployedArtifactState struct {
	Present                       bool
	ParseFailed                   bool              // true when the file exists on disk and is identifiable as MOSAIC-managed
	                                                // but its frontmatter could not be parsed. When true, Present is also true.
	                                                // ContentHash is still set (computed from raw bytes), but all version fields are empty.
	ContentHash                   string            // "sha256:<hex>"; empty when !Present
	Version                       string            // frontmatter `version` scalar, verbatim; "" when absent or unparseable
	HarnessVersion                string            // frontmatter `mosaic_harness_version` scalar (or legacy `mosaic_transform_version` fallback), verbatim
	InjectionsVersion             string            // injection version from InjectionHarness-class region tag attribute (or legacy frontmatter fallback)
	OrchestratorInjectionsVersion string            // reserved; always empty after Stage 3 — role-conditional comparison is in AgentStaleness
	ToolMappingsVersion           string            // frontmatter `tool_mappings_version` scalar, verbatim; "" when absent
	ModelID                       string            // frontmatter model scalar under the harness's ModelKey, verbatim; "" when absent, unparseable, or the harness emits no model
	Workflows                     DeployedWorkflows // non-nil only when the file carries workflow section markers
	ProtocolVersion               string            // version from the `version` attribute on the deployed <CommunicationProtocol type="managed"> region's opening tag; "" when absent, region missing, or file unparseable
	BundleVersion                 string            // frontmatter `bundle_version` scalar, verbatim; "" when absent, unparseable, or the file received no bundle region
}

// HasVersionInfo reports whether the deployed file carries at least one readable version stamp.
// A present file with no version info cannot be proven current and is treated as stale.
func (s DeployedArtifactState) HasVersionInfo() bool {
	return s.Version != "" || s.HarnessVersion != "" || s.InjectionsVersion != "" || s.OrchestratorInjectionsVersion != ""
}

// DeployedWorkflow is one workflow block found in a deployed orchestrator, paired with the
// version found in that block's `version` attribute on its opening tag.
type DeployedWorkflow struct {
	ID      string // from <Workflow type="core" name="<id>">
	Version string // from the `version` attribute on the section's opening tag; empty when absent
}

// DeployedWorkflows is the ordered, deduplicated list of workflow blocks in a deployed file,
// in first-occurrence order.
type DeployedWorkflows []DeployedWorkflow

// IDs returns the workflow IDs in order.
func (w DeployedWorkflows) IDs() []string {
	if len(w) == 0 {
		return nil
	}
	ids := make([]string, len(w))
	for i, wf := range w {
		ids[i] = wf.ID
	}
	return ids
}

// Version returns the deployed version for id; the second result is false when id is not
// present in the deployed file.
func (w DeployedWorkflows) Version(id string) (string, bool) {
	for _, wf := range w {
		if wf.ID == id {
			return wf.Version, true
		}
	}
	return "", false
}
