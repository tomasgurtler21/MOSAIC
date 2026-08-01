package transform

import (
	"fmt"
	"sort"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// processInjections applies the injection merge policy to every injection region in the
// document body and returns the outcomes, any gaps generated, the workflow IDs assembled
// into AvailableWorkflows, and the infrastructure agent keys assembled into
// InfrastructureAgents (both in emitted order).
//
// Merge policy by injection class:
//
//   - InjectionHarness: filled from the harness module on every transform (new and update).
//     Never lifted from the deployed file — harness content is always the descriptor's
//     current declaration.
//
//   - InjectionProject: emitted empty on new deployment, with a GapEmptyInjection gap
//     recorded so the user knows what to fill. On update, content is lifted byte-identically
//     from the deployed file when that injection point exists there; when the injection is
//     new in the source (absent from deployed) it starts empty with InjectionAdded action.
//
//   - InjectionWorkflow (AvailableWorkflows): assembled from req.Workflows in selection
//     order. The deployed file's AvailableWorkflows content is completely replaced on every
//     transform to prevent duplication and to reflect the current selection.
//
//   - InjectionInfrastructure (InfrastructureAgents): assembled from req.InfrastructureAgents
//     in selection order, analogous to InjectionWorkflow.
//
// Injection points present in the deployed file but absent from the source produce an
// InjectionOrphaned outcome and a GapRemovedInjection gap whose Fragment carries the
// orphaned content so the user can recover it.
//
// Body bytes outside injection regions are never modified, preserving byte-identity with
// the generic source for all non-injection prose.
func processInjections(doc *docformat.Document, req Request) (outcomes []InjectionOutcome, gaps []domain.Gap, workflowIDs []string, infraAgentKeys []string, err error) {
	// Build a lookup of injection content from the deployed file (update scenario only).
	// Returns nil when req.Deployed is nil (new deployment). A parse error means the
	// deployed file is structurally unreadable; propagate it so the caller can surface
	// it to the user rather than silently dropping all their injection content.
	deployedContent, err := buildDeployedInjectionMap(req.Deployed)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	body := doc.Body()
	sourceInjections := body.Injections()

	// Track source injection names so orphan detection can skip them.
	sourceNames := make(map[string]bool, len(sourceInjections))
	for _, node := range sourceInjections {
		sourceNames[node.Name()] = true
	}

	// Process each injection point present in the source document.
	for _, node := range sourceInjections {
		name := node.Name()
		class := docformat.ClassifyInjection(name)

		switch class {
		case domain.InjectionHarness:
			outcome := applyHarnessInjection(node, name, class, req)
			outcomes = append(outcomes, outcome)

		case domain.InjectionProject:
			outcome, gap := applyProjectInjection(node, name, class, req, deployedContent)
			outcomes = append(outcomes, outcome)
			if gap != nil {
				gaps = append(gaps, *gap)
			}

		case domain.InjectionWorkflow:
			outcome, ids := applyWorkflowInjection(node, name, class, req)
			outcomes = append(outcomes, outcome)
			workflowIDs = ids

		case domain.InjectionInfrastructure:
			outcome, ids := applyInfrastructureInjection(node, name, class, req)
			outcomes = append(outcomes, outcome)
			infraAgentKeys = ids
		}
	}

	// Detect orphaned injection points: present in the deployed file but absent from the
	// source. These must not appear in the output (the source drives the structure), but the
	// user's content must be surfaced in a gap so it can be recovered from the backup.
	//
	// Collect orphaned names into a sorted slice before processing so that Report.Injections
	// and Report.Gaps are produced in a stable, deterministic order across invocations.
	// Go map iteration order is randomised; sorting removes that non-determinism (AC8.3).
	if req.Deployed != nil {
		orphanNames := make([]string, 0, len(deployedContent))
		for name := range deployedContent {
			if !sourceNames[name] {
				orphanNames = append(orphanNames, name)
			}
		}
		sort.Strings(orphanNames)
		for _, name := range orphanNames {
			content := deployedContent[name]
			class := docformat.ClassifyInjection(name)
			outcomes = append(outcomes, InjectionOutcome{
				Name:   name,
				Class:  class,
				Action: InjectionOrphaned,
				Bytes:  0,
			})
			gaps = append(gaps, domain.Gap{
				Kind:     domain.GapRemovedInjection,
				Subject:  name,
				Fragment: string(content),
			})
		}
	}

	return outcomes, gaps, workflowIDs, infraAgentKeys, nil
}

// applyHarnessInjection fills a harness-level injection from the module's declared content.
// Harness content is refreshed on every transform; the deployed file's content is never
// consulted for harness injections.
func applyHarnessInjection(node *docformat.Node, name string, class domain.InjectionClass, req Request) InjectionOutcome {
	content, ok := req.Module.Injection(domain.InjectionRequest{Name: name, AgentKey: req.Key})
	if ok && content != "" {
		node.SetContent([]byte(content)) //nolint:errcheck // docformat.Node.SetContent always returns nil; the error return is part of the interface for forward compatibility only. TODO: propagate when the interface produces real errors.
		return InjectionOutcome{
			Name:   name,
			Class:  class,
			Action: InjectionFilled,
			Bytes:  len(content),
		}
	}
	return InjectionOutcome{
		Name:   name,
		Class:  class,
		Action: InjectionEmptied,
		Bytes:  0,
	}
}

// applyProjectInjection handles a project-level injection region.
//
// On new deployment (req.Deployed == nil): the injection is cleared and a GapEmptyInjection
// gap is recorded, signalling to the user that this region requires their input.
//
// On update (req.Deployed non-nil): the content from the deployed file is lifted verbatim
// into the region (InjectionPreserved). When the injection point is new in the source and
// absent from the deployed file, the region starts empty (InjectionAdded).
func applyProjectInjection(node *docformat.Node, name string, class domain.InjectionClass, req Request, deployedContent map[string][]byte) (InjectionOutcome, *domain.Gap) {
	if req.Deployed == nil {
		// New deployment: emit empty and record a TODO gap.
		node.Clear() //nolint:errcheck // docformat.Node.Clear always returns nil; the error return is part of the interface for forward compatibility only. TODO: propagate when the interface produces real errors.
		gap := &domain.Gap{
			Kind:    domain.GapEmptyInjection,
			Subject: name,
		}
		return InjectionOutcome{
			Name:   name,
			Class:  class,
			Action: InjectionEmptied,
			Bytes:  0,
		}, gap
	}

	// Update: lift from the deployed file when the injection point was present there.
	if content, present := deployedContent[name]; present {
		node.SetContent(content) //nolint:errcheck // docformat.Node.SetContent always returns nil; the error return is part of the interface for forward compatibility only. TODO: propagate when the interface produces real errors.
		return InjectionOutcome{
			Name:   name,
			Class:  class,
			Action: InjectionPreserved,
			Bytes:  len(content),
		}, nil
	}

	// Injection point added in the new source version — not in the deployed file.
	node.Clear() //nolint:errcheck // docformat.Node.Clear always returns nil; the error return is part of the interface for forward compatibility only. TODO: propagate when the interface produces real errors.
	return InjectionOutcome{
		Name:   name,
		Class:  class,
		Action: InjectionAdded,
		Bytes:  0,
	}, nil
}

// applyWorkflowInjection assembles the AvailableWorkflows injection from req.Workflows.
// The blocks are concatenated in selection order; the deployed file's AvailableWorkflows
// content is completely replaced so there is no possibility of duplication.
func applyWorkflowInjection(node *docformat.Node, name string, class domain.InjectionClass, req Request) (InjectionOutcome, []string) {
	if len(req.Workflows) == 0 {
		node.Clear() //nolint:errcheck // docformat.Node.Clear always returns nil; the error return is part of the interface for forward compatibility only. TODO: propagate when the interface produces real errors.
		return InjectionOutcome{
			Name:   name,
			Class:  class,
			Action: InjectionEmptied,
			Bytes:  0,
		}, nil
	}

	assembled, ids := assembleWorkflowBlocks(req.Workflows)
	node.SetContent(assembled) //nolint:errcheck // docformat.Node.SetContent always returns nil; the error return is part of the interface for forward compatibility only. TODO: propagate when the interface produces real errors.
	return InjectionOutcome{
		Name:   name,
		Class:  class,
		Action: InjectionAssembled,
		Bytes:  len(assembled),
	}, ids
}

// applyInfrastructureInjection assembles the InfrastructureAgents injection from
// req.InfrastructureAgents. The blocks are concatenated in selection order; the deployed
// file's InfrastructureAgents content is completely replaced on every transform to prevent
// duplication and to reflect the current selection. This is analogous to applyWorkflowInjection.
func applyInfrastructureInjection(node *docformat.Node, name string, class domain.InjectionClass, req Request) (InjectionOutcome, []string) {
	if len(req.InfrastructureAgents) == 0 {
		node.Clear() //nolint:errcheck // docformat.Node.Clear always returns nil; the error return is part of the interface for forward compatibility only. TODO: propagate when the interface produces real errors.
		return InjectionOutcome{
			Name:   name,
			Class:  class,
			Action: InjectionEmptied,
			Bytes:  0,
		}, nil
	}

	assembled, keys := AssembleInfrastructureBlocks(req.InfrastructureAgents)
	node.SetContent(assembled) //nolint:errcheck // docformat.Node.SetContent always returns nil; the error return is part of the interface for forward compatibility only. TODO: propagate when the interface produces real errors.
	return InjectionOutcome{
		Name:   name,
		Class:  class,
		Action: InjectionAssembledInfra,
		Bytes:  len(assembled),
	}, keys
}

// buildDeployedInjectionMap parses the deployed file and returns a map from injection name
// to its inner content bytes (as returned by Node.Content). Returns nil, nil when deployed
// is nil (new deployment).
//
// A malformed deployed file is an unrecoverable input problem: silently treating it as
// empty would cause every project injection to be classified as InjectionAdded, losing all
// of the user's prior content with no diagnostic. The error is therefore returned so the
// caller can surface it to the user.
func buildDeployedInjectionMap(deployed []byte) (map[string][]byte, error) {
	if deployed == nil {
		return nil, nil
	}
	depDoc, err := docformat.Parse(deployed)
	if err != nil {
		return nil, fmt.Errorf("deployed file is not a valid MOSAIC document: %w", err)
	}
	depInjections := depDoc.Body().Injections()
	m := make(map[string][]byte, len(depInjections))
	for _, node := range depInjections {
		m[node.Name()] = node.Content()
	}
	return m, nil
}
