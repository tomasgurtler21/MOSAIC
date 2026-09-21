package transform

import (
	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentformat"
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
			HarnessVersion:                desc.TransformVersion,
			InjectionsVersion:             desc.InjectionsVersion,
			OrchestratorInjectionsVersion: desc.OrchestratorInjectionsVersion,
		},
		Role: req.Role,
	}
	fmPlan, err := req.Module.Frontmatter(fmReq)
	if err != nil {
		return Result{}, err
	}

	// Apply the frontmatter plan (descriptor drops/adds/order) plus model, version
	// stamps, and tool fields. Returns FieldChange audit entries, gaps, and owned-key
	// differences for conflict-classified artifacts (empty on every ordinary run).
	// Owned-key differences are computed inside applyFrontmatter before the Step 5c
	// preservation pass, so "incoming" reflects source-driven transforms, not deployed
	// values copied back by the preservation step.
	fieldChanges, gaps, ownedKeyDiffs := applyFrontmatter(fm, fmPlan, toolResult, req, desc)

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
	//
	// For agent artifacts, delegate to the translator named by the descriptor's agent
	// format ID. Transform stays pure: it resolves the translator by format ID and passes
	// the full artifact context. Skills and hooks remain Markdown for every harness (AD-4).
	//
	// Transform threads Op and DeployedRaw through to ArtifactContext but does NOT enforce
	// them. Enforcement is format-conditional and belongs to the translator (Codex raises
	// ErrUnspecifiedOperation / ErrMissingPriorBytes; Markdown ignores Op entirely).
	//
	// Precondition: req.Deployed always holds the CANONICAL form. Raw on-disk bytes are
	// in req.DeployedRaw. The application layer decodes before calling Apply.
	var output []byte
	var encodeReport agentformat.Report
	if req.Kind == domain.ArtifactAgent {
		t, lookupErr := agentformat.LookupString(desc.AgentFormatID)
		if lookupErr != nil {
			return Result{}, lookupErr
		}
		ctx := agentformat.ArtifactContext{
			AgentKey:      req.Key,
			Kind:          req.Kind,
			PriorDeployed: req.DeployedRaw,
			Op:            req.Op,
		}
		var encErr error
		output, encodeReport, encErr = t.Encode(doc.Bytes(), ctx)
		if encErr != nil {
			return Result{}, encErr
		}
	} else {
		output = doc.Bytes()
	}

	// Merge the encode report into fieldChanges using the entry-kind mapping table
	// from the transform pipeline contract. This is the single place the encode-side
	// vocabulary is translated into the run's reporting vocabulary.
	for _, entry := range encodeReport.Entries {
		switch entry.Kind {
		case agentformat.EntryDroppedForeignKey:
			fieldChanges = append(fieldChanges, FieldChange{
				Key:    entry.Key,
				Before: entry.Detail,
				After:  "",
				Reason: entry.Reason,
			})
		case agentformat.EntryOverriddenName:
			fieldChanges = append(fieldChanges, FieldChange{
				Key:    entry.Key,
				Before: entry.Detail,
				After:  req.Key,
				Reason: entry.Reason,
			})
		case agentformat.EntryAppliedFallback:
			fieldChanges = append(fieldChanges, FieldChange{
				Key:    entry.Key,
				Before: "",
				After:  entry.Detail,
				Reason: entry.Reason,
			})
		case agentformat.EntryStrippedContainer:
			fieldChanges = append(fieldChanges, FieldChange{
				Key:    entry.Key,
				Before: entry.Detail,
				After:  "",
				Reason: entry.Reason,
			})
		case agentformat.EntryRefusedMarker:
			fieldChanges = append(fieldChanges, FieldChange{
				Key:    entry.Key,
				Before: "",
				After:  entry.Detail,
				Reason: entry.Reason,
			})
		case agentformat.EntryCarriedContainer:
			// Nothing: the key was re-emitted, so no field changed and there is nothing
			// to tell the user. This is what keeps an ordinary Codex redeploy silent
			// about the user's preserved keys.
		case agentformat.EntryDroppedComment:
			// A free-standing comment that did not travel. Not a FieldChange (a comment
			// is not a field and has no key to put in Key). Not a gap either — there is
			// no existing gap kind for a dropped comment. A later stage that adds a
			// user-facing notice channel for comments may map it there.
		}
		// EntryMissingInstructions is decode-only and never appears in an encode report.
	}

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
		OwnedKeyDifferences:  ownedKeyDiffs,
	}

	return Result{Output: output, Report: report}, nil
}
