package transform

import (
	"fmt"
	"sort"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// processRegions applies the merge policy to every managed region in the document body.
// It replaces processInjections and covers both [[INJECTION:]] (user-owned) and
// [[DEPLOYED:]] (tool-managed) regions.
//
// Routing is by marker kind first:
//
//	NodeInjection → user-owned (class project): cleared with a GapEmptyInjection on create;
//	                lifted byte-identically from the deployed file on update; cleared with
//	                action added when new in the source.
//
//	NodeDeployed  → tool-managed: regenerated on every transform from the generator selected
//	                by the region's class (harness, workflow, infrastructure, protocol).
//	                The deployed file is never consulted for a tool-managed region.
//
// A region whose name/marker pairing is invalid aborts the transform with an error naming
// the region; the transform never guesses.
//
// Orphan detection covers user-owned regions only: a name present as a [[INJECTION:]]
// region in the deployed file and absent from the source produces an orphaned outcome and a
// GapRemovedInjection gap carrying the content. A tool-managed region removed from the
// source produces no gap. Orphan names are sorted before emission so the report is
// deterministic.
func processRegions(doc *docformat.Document, req Request) (outcomes []RegionOutcome, gaps []domain.Gap, workflowIDs []string, infraAgentKeys []string, err error) {
	// Build a lookup of user-owned injection content from the deployed file (update only).
	// Returns nil when req.Deployed is nil (new deployment). A parse error means the
	// deployed file is structurally unreadable; propagate it rather than silently dropping
	// all the user's injection content.
	deployedContent, err := buildDeployedRegionMap(req.Deployed)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	body := doc.Body()
	sourceRegions := body.Regions() // both NodeInjection and NodeDeployed, document order

	// Track source user-owned injection names for orphan detection.
	sourceInjectionNames := make(map[string]bool, len(sourceRegions))
	for _, node := range sourceRegions {
		if node.Kind() == docformat.NodeInjection {
			sourceInjectionNames[node.Name()] = true
		}
	}

	// Process each region present in the source document.
	for _, node := range sourceRegions {
		name := node.Name()
		class, classErr := docformat.ClassifyRegion(node.Kind(), name)
		if classErr != nil {
			return nil, nil, nil, nil, classErr
		}

		switch class {
		case domain.InjectionHarness:
			outcome := applyHarnessRegion(node, name, class, req)
			outcomes = append(outcomes, outcome)

		case domain.InjectionProject:
			outcome, gap := applyProjectRegion(node, name, class, req, deployedContent)
			outcomes = append(outcomes, outcome)
			if gap != nil {
				gaps = append(gaps, *gap)
			}

		case domain.InjectionWorkflow:
			outcome, ids := applyWorkflowRegion(node, name, class, req)
			outcomes = append(outcomes, outcome)
			workflowIDs = ids

		case domain.InjectionInfrastructure:
			outcome, ids := applyInfrastructureRegion(node, name, class, req)
			outcomes = append(outcomes, outcome)
			infraAgentKeys = ids

		case domain.InjectionProtocol:
			outcome, protoErr := applyProtocolRegion(node, name, req)
			if protoErr != nil {
				return nil, nil, nil, nil, protoErr
			}
			outcomes = append(outcomes, outcome)
		}
	}

	// Detect orphaned user-owned injection points: present in the deployed file but absent
	// from the source. Tool-managed regions removed from the source produce no gap because
	// their content is regenerated and never user-authored.
	//
	// Collect orphaned names into a sorted slice before processing so that Report.Regions
	// and Report.Gaps are produced in a stable, deterministic order across invocations.
	if req.Deployed != nil {
		orphanNames := make([]string, 0, len(deployedContent))
		for name := range deployedContent {
			if !sourceInjectionNames[name] {
				orphanNames = append(orphanNames, name)
			}
		}
		sort.Strings(orphanNames)
		for _, name := range orphanNames {
			content := deployedContent[name]
			outcomes = append(outcomes, RegionOutcome{
				Name:   name,
				Marker: docformat.NodeInjection,
				Class:  domain.InjectionProject,
				Action: RegionOrphaned,
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

// applyHarnessRegion fills a tool-managed harness region from the module's declared content.
// Harness content is refreshed on every transform; the deployed file's content is never
// consulted for tool-managed regions.
func applyHarnessRegion(node *docformat.Node, name string, class domain.InjectionClass, req Request) RegionOutcome {
	content, ok := req.Module.Injection(domain.InjectionRequest{Name: name, AgentKey: req.Key})
	if ok && content != "" {
		// Ensure the content ends with a newline so the closing tag appears on its own line
		// when the document is serialised. Descriptor YAML strings do not always carry a
		// trailing newline; without one, the closing [[/DEPLOYED:...]] tag is concatenated
		// onto the last line of content rather than appearing on a line by itself.
		contentBytes := []byte(content)
		if contentBytes[len(contentBytes)-1] != '\n' {
			contentBytes = append(contentBytes, '\n')
		}
		node.SetContent(contentBytes) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.
		return RegionOutcome{
			Name:   name,
			Marker: node.Kind(),
			Class:  class,
			Action: RegionFilled,
			Bytes:  len(contentBytes),
		}
	}
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  class,
		Action: RegionEmptied,
		Bytes:  0,
	}
}

// applyProjectRegion handles a user-owned [[INJECTION:]] region.
//
// On new deployment (req.Deployed == nil): the region is cleared and a GapEmptyInjection
// gap is recorded, signalling to the user that this region requires their input.
//
// On update (req.Deployed non-nil): the content from the deployed file is lifted verbatim
// into the region (RegionPreserved). When the region is new in the source and absent from
// the deployed file, the region starts empty (RegionAdded).
func applyProjectRegion(node *docformat.Node, name string, class domain.InjectionClass, req Request, deployedContent map[string][]byte) (RegionOutcome, *domain.Gap) {
	if req.Deployed == nil {
		// New deployment: emit empty and record a TODO gap.
		node.Clear() //nolint:errcheck // Node.Clear always returns nil; forward-compatible error return.
		gap := &domain.Gap{
			Kind:    domain.GapEmptyInjection,
			Subject: name,
		}
		return RegionOutcome{
			Name:   name,
			Marker: node.Kind(),
			Class:  class,
			Action: RegionEmptied,
			Bytes:  0,
		}, gap
	}

	// Update: lift from the deployed file when the region was present there.
	if content, present := deployedContent[name]; present {
		node.SetContent(content) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.
		return RegionOutcome{
			Name:   name,
			Marker: node.Kind(),
			Class:  class,
			Action: RegionPreserved,
			Bytes:  len(content),
		}, nil
	}

	// Region added in the new source version — not in the deployed file.
	node.Clear() //nolint:errcheck // Node.Clear always returns nil; forward-compatible error return.
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  class,
		Action: RegionAdded,
		Bytes:  0,
	}, nil
}

// applyWorkflowRegion assembles the AvailableWorkflows region from req.Workflows.
// The blocks are concatenated in selection order; the deployed file's AvailableWorkflows
// content is completely replaced so there is no possibility of duplication.
func applyWorkflowRegion(node *docformat.Node, name string, class domain.InjectionClass, req Request) (RegionOutcome, []string) {
	if len(req.Workflows) == 0 {
		node.Clear() //nolint:errcheck // Node.Clear always returns nil; forward-compatible error return.
		return RegionOutcome{
			Name:   name,
			Marker: node.Kind(),
			Class:  class,
			Action: RegionEmptied,
			Bytes:  0,
		}, nil
	}

	assembled, ids := assembleWorkflowBlocks(req.Workflows)
	node.SetContent(assembled) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  class,
		Action: RegionAssembled,
		Bytes:  len(assembled),
	}, ids
}

// applyInfrastructureRegion assembles the InfrastructureAgents region from
// req.InfrastructureAgents. The blocks are concatenated in selection order; the deployed
// file's InfrastructureAgents content is completely replaced on every transform.
func applyInfrastructureRegion(node *docformat.Node, name string, class domain.InjectionClass, req Request) (RegionOutcome, []string) {
	if len(req.InfrastructureAgents) == 0 {
		node.Clear() //nolint:errcheck // Node.Clear always returns nil; forward-compatible error return.
		return RegionOutcome{
			Name:   name,
			Marker: node.Kind(),
			Class:  class,
			Action: RegionEmptied,
			Bytes:  0,
		}, nil
	}

	assembled, keys := AssembleInfrastructureBlocks(req.InfrastructureAgents)
	node.SetContent(assembled) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  class,
		Action: RegionAssembledInfra,
		Bytes:  len(assembled),
	}, keys
}

// applyProtocolRegion writes the role-matched protocol block into the region, preceded by
// the protocol version marker line. Returns an error wrapping ErrProtocolContentMissing
// when the block for req.Role is absent or empty (whitespace-only counts as empty).
func applyProtocolRegion(node *docformat.Node, name string, req Request) (RegionOutcome, error) {
	block, ok := req.Protocol.For(req.Role)
	if !ok {
		return RegionOutcome{}, fmt.Errorf("region %q: %w", name, ErrProtocolContentMissing)
	}

	versionComment := []byte(ProtocolVersionComment(req.Protocol.Version))
	content := make([]byte, 0, len(versionComment)+len(block))
	content = append(content, versionComment...)
	content = append(content, block...)

	node.SetContent(content) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  domain.InjectionProtocol,
		Action: RegionProtocolFilled,
		Bytes:  len(content),
	}, nil
}

// buildDeployedRegionMap parses the deployed file and returns a map from USER-OWNED region
// name to its inner content bytes. Deployed-marker (NodeDeployed) regions in the deployed
// file are ignored: their content is regenerated on every transform, so preserving or
// orphaning it would be wrong.
// Returns nil, nil when deployed is nil. A parse failure is returned as an error rather
// than silently dropping the user's content.
func buildDeployedRegionMap(deployed []byte) (map[string][]byte, error) {
	if deployed == nil {
		return nil, nil
	}
	depDoc, err := docformat.Parse(deployed)
	if err != nil {
		return nil, fmt.Errorf("deployed file is not a valid MOSAIC document: %w", err)
	}
	// Only NodeInjection nodes are included; NodeDeployed content is regenerated and never
	// recovered.
	depInjections := depDoc.Body().Injections()
	m := make(map[string][]byte, len(depInjections))
	for _, node := range depInjections {
		m[node.Name()] = node.Content()
	}
	return m, nil
}

