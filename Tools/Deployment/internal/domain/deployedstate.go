package domain

// DeployedArtifactState describes what is actually present on disk at one target path.
// It is produced by the app layer (which owns all I/O) and consumed by the planner, which
// uses it as the sole source of truth for version comparison and on-disk presence.
//
// The zero value represents a valid "absent" state, so map lookups for unknown paths behave
// correctly without a separate absence sentinel.
type DeployedArtifactState struct {
	Present                       bool
	ContentHash                   string            // "sha256:<hex>"; empty when !Present
	Version                       string            // frontmatter `version` scalar, verbatim; "" when absent or unparseable
	TransformVersion              string            // frontmatter `transform_version` scalar, verbatim
	InjectionsVersion             string            // frontmatter `injections_version` scalar, verbatim
	OrchestratorInjectionsVersion string            // frontmatter `orchestrator_injections_version` scalar, verbatim; "" when absent or subagent
	ModelID                       string            // frontmatter model scalar under the harness's ModelKey, verbatim; "" when absent, unparseable, or the harness emits no model
	Workflows                     DeployedWorkflows // non-nil only when the file carries workflow section markers
}

// HasVersionInfo reports whether the deployed file carries at least one readable version stamp.
// A present file with no version info cannot be proven current and is treated as stale.
func (s DeployedArtifactState) HasVersionInfo() bool {
	return s.Version != "" || s.TransformVersion != "" || s.InjectionsVersion != "" || s.OrchestratorInjectionsVersion != ""
}

// DeployedWorkflow is one workflow block found in a deployed orchestrator, paired with the
// version found in that block's <!-- workflow-version: X --> comment.
type DeployedWorkflow struct {
	ID      string // from [[SECTION:Workflow:<id>]]
	Version string // from <!-- workflow-version: X -->; empty when no such comment is present
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
