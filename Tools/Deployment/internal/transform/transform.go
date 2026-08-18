package transform

import (
	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// Apply transforms a generic MOSAIC source file into a harness-specific deployed file.
//
// It is a pure function: no filesystem access, no network calls, no clock reads, and no
// randomness. Every input arrives as a field of req; every output is in the returned Result
// (CD-7, AC8.2, AC8.3).
//
// Guarantees:
//   - Body bytes outside injection regions are byte-identical to req.Source (AC8.1).
//   - All frontmatter shaping is driven by req.Module.Descriptor(); no harness name appears
//     in this package (AC8.4).
//   - The returned Report names every changed field and its reason (AC8.5).
//   - Identical inputs across repeated calls produce byte-identical output (AC8.3).
func Apply(req Request) (Result, error) {
	// Parse the source document.
	doc, err := docformat.Parse(req.Source)
	if err != nil {
		return Result{}, err
	}

	desc := req.Module.Descriptor()
	fm := doc.Frontmatter()
	sourceFields := fm.Fields()

	// Resolve the tool output for this agent.
	toolResult, err := resolveTools(req, fm, desc)
	if err != nil {
		return Result{}, err
	}

	// Extract the source version field value for VersionStamps.
	sourceVersion := ""
	if v, ok := fm.Get("version"); ok && v.Kind == domain.KindScalar {
		sourceVersion = v.Scalar
	}

	// Build the FrontmatterRequest and get the plan from the module.
	fmReq := domain.FrontmatterRequest{
		Kind:       req.Kind,
		AgentKey:   req.Key,
		Source:     sourceFields,
		Model:      req.Model,
		ToolFields: toolResult.Fields,
		Versions: domain.VersionStamps{
			Version:                       sourceVersion,
			TransformVersion:              desc.TransformVersion,
			InjectionsVersion:             desc.InjectionsVersion,
			OrchestratorInjectionsVersion: desc.OrchestratorInjectionsVersion,
		},
	}
	fmPlan, err := req.Module.Frontmatter(fmReq)
	if err != nil {
		return Result{}, err
	}

	// Apply the frontmatter plan (descriptor drops/adds/order) plus model, version
	// stamps, and tool fields. Returns FieldChange audit entries and any gaps.
	fieldChanges, gaps := applyFrontmatter(fm, fmPlan, toolResult, req, desc)

	// Process managed regions in the body, applying the merge policy:
	//   injection regions (user-owned) — preserved from deployed on update, emptied on create.
	//   managed regions (tool-managed) — regenerated every transform from harness/workflows/infra.
	// Orphaned user-owned injection points produce gaps; tool-managed regions removed from
	// the source produce no gap.
	regionOutcomes, regionGaps, workflowIDs, infraAgentKeys, err := processRegions(doc, req)
	if err != nil {
		return Result{}, err
	}

	// Serialise the transformed document to bytes.
	output := doc.Bytes()

	// Merge frontmatter gaps with region gaps into one ordered slice.
	allGaps := append(gaps, regionGaps...)

	report := Report{
		Fields:               fieldChanges,
		Tools:                toolResult.Resolutions,
		Regions:              regionOutcomes,
		Gaps:                 allGaps,
		Workflows:            workflowIDs,
		InfrastructureAgents: infraAgentKeys,
		OutputBytes:          len(output),
	}

	return Result{Output: output, Report: report}, nil
}
