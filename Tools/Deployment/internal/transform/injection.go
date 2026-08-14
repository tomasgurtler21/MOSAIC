package transform

import (
	"bytes"
	"fmt"
	"sort"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// regionEntry is one user-owned region lifted from the deployed file: its content and the
// marker kind it was written with. Provenance is required because a name absent from the
// source means "removed injection, discard with a gap" for NodeInjection and "custom
// region, preserve in output" for NodeCustom.
type regionEntry struct {
	Content  []byte             // inner bytes, byte-identical to the deployed file
	Kind     docformat.NodeKind // NodeInjection or NodeCustom; never NodeDeployed
	Migrated bool               // true when content arrived via rename resolution of an old injection name
}

// processRegions applies the merge policy to every managed region in the document body.
// It replaces processInjections and covers both injection (user-owned) and
// managed (tool-managed) regions.
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
// Orphan detection covers user-owned regions only. After the source region loop:
//   - A name present as an injection region in the deployed file and absent from the source
//     produces an orphaned outcome and a GapRemovedInjection gap carrying the content.
//   - A name present as a custom region in the deployed file and absent from the source
//     (custom regions never appear in source files) is preserved in the output at its
//     anchored position. It never produces RegionOrphaned and never routes to
//     GapRemovedInjection.
//   - A tool-managed region removed from the source produces no gap.
//
// Orphan and custom names are sorted before emission so the report is deterministic.
//
// Name-collision errors abort the transform before any document mutation:
//   - Shape A: the same name appears twice as a custom region in the deployed file.
//   - Shape B: the same name appears as a custom region in the deployed file and an injection
//     region in the source.
func processRegions(doc *docformat.Document, req Request) (outcomes []RegionOutcome, gaps []domain.Gap, workflowIDs []string, infraAgentKeys []string, err error) {
	// Select the active rename table: use the per-request override when provided (for tests),
	// falling back to the package-level InjectionRenames table for all real callers.
	renames := InjectionRenames
	if req.InjectionRenames != nil {
		renames = req.InjectionRenames
	}

	// Validate the rename table before any document mutation. An ambiguous table is a hard
	// error: the tool never guesses which destination was intended for the user's content.
	if err := ValidateRenames(renames); err != nil {
		return nil, nil, nil, nil, err
	}

	// Build a lookup of user-owned region content from the deployed file (update only).
	// Returns nil map, nil document, and nil gaps when req.Deployed is nil (new deployment).
	// A parse error or a Shape A collision (duplicate custom region name) is propagated here
	// before any mutation. Rename resolution runs inside: old injection names are translated
	// to their new names so applyProjectRegion finds the content at the right key.
	deployedContent, depDoc, renameGaps, err := buildDeployedRegionMap(req.Deployed, renames)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// Gaps from rename resolution (old-name content when both old and new names are present)
	// are accumulated here and merged with the final gap list at the end.
	gaps = append(gaps, renameGaps...)

	body := doc.Body()
	sourceRegions := body.Regions() // NodeInjection, NodeDeployed, and NodeCustom, document order

	// Track source user-owned injection names for orphan detection and Shape B collision.
	// Nested injection names (inside managed regions) are included so they are not
	// reported as orphans — they are handled by the capture-regenerate-reemit sequence.
	sourceInjectionNames := make(map[string]bool, len(sourceRegions))
	for _, node := range sourceRegions {
		if node.Kind() == docformat.NodeInjection {
			sourceInjectionNames[node.Name()] = true
		}
	}

	// reemittedNames tracks region names that were re-emitted by reemitNestedUserRegions
	// during deployed-region processing. These names must be excluded from the post-loop
	// custom placement so they are not placed twice.
	reemittedNames := make(map[string]bool)

	// Shape B collision: a name that is a custom region in the deployed file and an injection
	// region in the source. Both claim user-authored content; preserving both breaks the unique-name
	// invariant; the tool aborts rather than guessing which to keep.
	// This check runs before the source region loop so no mutation occurs on the error path.
	if req.Deployed != nil {
		for name, entry := range deployedContent {
			if entry.Kind == docformat.NodeCustom && sourceInjectionNames[name] {
				return nil, nil, nil, nil, fmt.Errorf("region name %q: %w", name, ErrRegionNameCollision)
			}
		}
	}

	// Process each region present in the source document.
	for _, node := range sourceRegions {
		name := node.Name()

		// Skip injection nodes nested directly inside a managed region.
		// These are captured before the parent's generator runs and re-emitted after it,
		// inside the parent's regenerated content. Processing them here via applyProjectRegion
		// would write to a detached node (one that SetContent already replaced) and produce
		// a duplicate outcome entry.
		if node.Kind() == docformat.NodeInjection &&
			node.Parent() != nil &&
			node.Parent().Kind() == docformat.NodeDeployed {
			continue
		}

		class, classErr := docformat.ClassifyRegion(node.Kind(), name)
		if classErr != nil {
			return nil, nil, nil, nil, classErr
		}

		// Reject any managed region that carries non-empty content in the source file.
		// Source regions are empty by definition; content here is either a hand-edit about to
		// be discarded or a deployed file mistakenly committed as source.
		// Empty nested injection or custom markers are permitted — they are empty
		// placeholders that the capture-regenerate-reemit sequence will populate from the
		// deployed file.
		if node.Kind() == docformat.NodeDeployed && !docformat.SourceDeployedRegionIsEmpty(node) {
			return nil, nil, nil, nil, fmt.Errorf("region %q: %w", name, ErrSourceDeployedRegionNotEmpty)
		}

		// Capture nested user-owned regions BEFORE the generator runs. The generator calls
		// SetContent or Clear on this node, which replaces the node's item list and detaches
		// any nested child nodes. Capture must happen first so the nested regions survive.
		//
		// Mandatory ordering (violating it reintroduces the bug this fixes):
		//  1. captureNestedUserRegions — records what must survive (BEFORE any mutation)
		//  2. generator — regenerates canonical content (SetContent/Clear)
		//  3. reemitNestedUserRegions — appends captured regions inside the parent (AFTER regeneration)
		var capture []nestedRegionRecord
		if node.Kind() == docformat.NodeDeployed {
			capture = captureNestedUserRegions(node, depDoc)
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

		case domain.InjectionBundle:
			outcome, bundleErr := applyBundleRegion(node, name, req)
			if bundleErr != nil {
				return nil, nil, nil, nil, bundleErr
			}
			outcomes = append(outcomes, outcome)
		}

		// Content-change detection: compare the freshly generated canonical content of this
		// deployed region against its previously deployed canonical content. A gap is emitted
		// when the canonical content changed AND nested user regions are present, so the user
		// knows to review their nested content against the updated canonical text.
		//
		// This runs between step 2 (generator) and step 3 (reemitNestedUserRegions): at this
		// point the node holds only the freshly generated canonical content with no nested user
		// regions re-emitted yet, making the comparison symmetric via CanonicalContent.
		//
		// Conditions for gap emission:
		//   - depDoc != nil: an update run; no previous content exists on a create run.
		//   - len(capture) > 0: nested user regions are present and will survive.
		//   - CanonicalContent differs between old (deployed) and new (generated) sides.
		if len(capture) > 0 && depDoc != nil {
			newCanonical := CanonicalContent(node)
			if depRegion, ok := depDoc.Body().Deployed(name); ok {
				oldCanonical := CanonicalContent(depRegion)
				if !bytes.Equal(newCanonical, oldCanonical) {
					nestedNames := make([]string, 0, len(capture))
					for _, rec := range capture {
						nestedNames = append(nestedNames, rec.Name)
					}
					sort.Strings(nestedNames)
					gaps = append(gaps, domain.Gap{
						Kind:    domain.GapDeployedRegionContentChanged,
						Subject: name,
						Detail:  DeployedRegionContentChangedDetail(name, nestedNames, req.Timestamp),
					})
				}
			}
		}

		// Re-emit nested user-owned regions inside the parent after the generator ran.
		// This is step 3 of the mandatory capture-regenerate-reemit sequence.
		// When capture is empty, reemitNestedUserRegions is a no-op and does not touch the
		// parent — no stray separators, no trailing whitespace change (AC6.6).
		if len(capture) > 0 {
			nestedOutcomes, reemitErr := reemitNestedUserRegions(node, capture, deployedContent)
			if reemitErr != nil {
				return nil, nil, nil, nil, reemitErr
			}
			for _, out := range nestedOutcomes {
				reemittedNames[out.Name] = true
			}
			outcomes = append(outcomes, nestedOutcomes...)
		}
	}

	// Injection re-parenting detection (AD-E, AD-F): walk all source injection nodes at
	// any depth — including those nested inside managed regions, which are skipped by
	// the main source-region loop above. This is a read-only inspection and does not affect
	// placement or content. Runs only on update runs (depDoc != nil).
	if depDoc != nil {
		deployedInjParents := buildDeployedInjectionParents(depDoc, renames)
		for _, node := range body.Injections() {
			name := node.Name()
			deployedParent, inDeployed := deployedInjParents[name]
			if !inDeployed {
				// Injection is new in the source — no previous parent to compare (AD-F).
				continue
			}
			sourceParent := anchorParentName(node)
			if sourceParent != deployedParent {
				gaps = append(gaps, domain.Gap{
					Kind:    domain.GapInjectionReparented,
					Subject: name,
					Detail:  InjectionReparentedDetail(name, deployedParent, sourceParent, req.Timestamp),
				})
			}
		}
	}

	// Post-loop: handle names present in the deployed file but absent from the source.
	// Branch by provenance:
	//   - NodeInjection: the injection was removed from the source → orphan and gap (unchanged).
	//   - NodeCustom: custom regions never appear in source files → preserve in output.
	//
	// Names are collected into sorted slices before processing so the report is deterministic.
	if req.Deployed != nil {
		// Collect custom region records from the deployed document for placement.
		customRecords := collectDeployedCustomRegions(depDoc)
		customRecordByName := make(map[string]customRegionRecord, len(customRecords))
		for _, rec := range customRecords {
			customRecordByName[rec.Name] = rec
		}

		orphanNames := make([]string, 0)
		customNames := make([]string, 0)
		for name, entry := range deployedContent {
			if !sourceInjectionNames[name] {
				switch entry.Kind {
				case docformat.NodeInjection:
					orphanNames = append(orphanNames, name)
				case docformat.NodeCustom:
					// Skip custom regions that were already re-emitted by the
					// capture-regenerate-reemit sequence applied to their deployed parent.
					// Placing them again would fail with ErrDuplicateRegionName and produce
					// a duplicate outcome entry.
					if !reemittedNames[name] {
						customNames = append(customNames, name)
					}
				}
			}
		}

		// Removed injection names: existing orphan-and-gap behavior, unchanged.
		sort.Strings(orphanNames)
		for _, name := range orphanNames {
			entry := deployedContent[name]
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
				Fragment: string(entry.Content),
			})
		}

		// Detect schema reorder: compare the deployed and source structural slot orders.
		// A pure section addition or removal never triggers a reorder on its own.
		reorder := docformat.ReorderDetected(depDoc, doc)

		// Build the source anchor name set only when a reorder is detected, since the
		// anchor check (rec.ParentName in sourceAnchors) only affects routing on reorder.
		var sourceAnchors map[string]bool
		if reorder {
			sourceAnchors = sourceAnchorNames(body)
		}

		// Custom region names: route to anchoring or parking based on reorder detection.
		//
		// Anchoring rule (name-only, no position sensitivity):
		//   anchored = rec.ParentName != "" && sourceAnchors[rec.ParentName]
		//
		//   anchored OR no reorder → place at anchored position (normal path)
		//   !anchored AND reorder  → collect for parking (one gap for the whole set)
		//
		// Fallthrough detection runs on the placement path: when a region's ParentName is
		// non-empty but cannot be resolved in the output body (neither as a section nor as a
		// deployed region), the region falls through to body-level placement. One gap is
		// emitted per transform listing all fallthrough names in sorted order. This is
		// independent of the reorder flag and disjoint from the parking path.
		sort.Strings(customNames)
		var toPark []customRegionRecord
		var fallthroughNames []string
		for _, name := range customNames {
			entry := deployedContent[name]
			rec := customRecordByName[name]
			anchored := rec.ParentName != "" && sourceAnchors[rec.ParentName]
			if anchored || !reorder {
				// Detect fallthrough before calling placeCustomRegion: if the ParentName is
				// non-empty but resolves to neither a section nor a deployed region in the
				// output body, the region will fall through to body-level placement.
				if rec.ParentName != "" {
					_, sectionOK := body.SectionDeep(rec.ParentName)
					_, deployedOK := body.Deployed(rec.ParentName)
					if !sectionOK && !deployedOK {
						fallthroughNames = append(fallthroughNames, name)
					}
				}
				// Anchored or no reorder: place the custom region at its resolved position.
				if _, placeErr := placeCustomRegion(body, rec, entry.Content); placeErr != nil {
					return nil, nil, nil, nil, placeErr
				}
				outcomes = append(outcomes, RegionOutcome{
					Name:   name,
					Marker: docformat.NodeCustom,
					Class:  domain.InjectionProject,
					Action: RegionCustomPreserved,
					Bytes:  len(entry.Content),
				})
			} else {
				// Unanchored on a reorder: collect for end-of-body parking.
				// Content is already present on the record from collectDeployedCustomRegions.
				toPark = append(toPark, rec)
			}
		}

		// Emit one fallthrough gap when one or more custom regions could not be anchored to
		// their recorded parent in the output document and were placed at body level. Names
		// are already in sorted order because customNames was sorted before the loop.
		if len(fallthroughNames) > 0 {
			gaps = append(gaps, domain.Gap{
				Kind:    domain.GapCustomRegionFallthrough,
				Subject: req.Key,
				Detail:  FallthroughCustomRegionsDetail(fallthroughNames, req.Timestamp),
			})
		}

		// Park all unanchored custom regions at end of body and emit a single parking gap
		// listing every parked name in sorted order. Exactly one gap is emitted per transform.
		if len(toPark) > 0 {
			parkedNames, parkedOutcomes, parkErr := parkCustomRegions(body, toPark)
			if parkErr != nil {
				return nil, nil, nil, nil, parkErr
			}
			outcomes = append(outcomes, parkedOutcomes...)
			gaps = append(gaps, domain.Gap{
				Kind:    domain.GapParkedCustomRegion,
				Subject: req.Key,
				Detail:  ParkedCustomRegionsDetail(parkedNames, req.Timestamp),
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
		// trailing newline; without one, the closing </Name> tag is concatenated onto the
		// last line of content rather than appearing on a line by itself.
		//
		// Harness injection content is normalised to LF by the harness loader so that content
		// read from CRLF-checked-out files on Windows matches what LF-checked-out files
		// produce. Adapt the LF content to the source document's prevailing line ending so the
		// injected lines are consistent with the surrounding document.
		lineEnding := DetectLineEnding(req.Source)
		contentBytes := []byte(content)
		if lineEnding == "\r\n" {
			contentBytes = bytes.ReplaceAll(contentBytes, []byte("\n"), []byte("\r\n"))
		}
		if len(contentBytes) == 0 || contentBytes[len(contentBytes)-1] != '\n' {
			contentBytes = append(contentBytes, []byte(lineEnding)...)
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
	// No harness content for this region: clear it so any nested source markers are removed
	// and the capture-regenerate-reemit sequence can re-add them cleanly. This mirrors the
	// explicit Clear() call on the RegionEmptied path of every other deployed-region generator.
	node.Clear() //nolint:errcheck // Node.Clear always returns nil; forward-compatible error return.
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  class,
		Action: RegionEmptied,
		Bytes:  0,
	}
}

// applyProjectRegion handles a user-owned injection region.
//
// On new deployment (req.Deployed == nil): the region is cleared and a GapEmptyInjection
// gap is recorded, signalling to the user that this region requires their input.
//
// On update (req.Deployed non-nil): the content from the deployed file is lifted verbatim
// into the region (RegionPreserved). When the region is new in the source and absent from
// the deployed file, the region starts empty (RegionAdded).
func applyProjectRegion(node *docformat.Node, name string, class domain.InjectionClass, req Request, deployedContent map[string]regionEntry) (RegionOutcome, *domain.Gap) {
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
	if entry, present := deployedContent[name]; present {
		node.SetContent(entry.Content) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.
		action := RegionPreserved
		if entry.Migrated {
			// Content arrived via rename resolution: the deployed file stored it under the
			// injection's old name and the rename table directed it here.
			action = RegionMigrated
		}
		return RegionOutcome{
			Name:   name,
			Marker: node.Kind(),
			Class:  class,
			Action: action,
			Bytes:  len(entry.Content),
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

	// Determine the effective line ending for the protocol region. The primary signal is the
	// block's own line ending (which is CRLF when loaded from a CRLF-checked-out file).
	// When the block is LF but the source document is CRLF, promote to CRLF so the region
	// is consistent with the surrounding document. This handles test fixtures where protocol
	// blocks are Go string literals (always LF) embedded in a real CRLF source file.
	//
	// The block is normalised to LF first to avoid double-converting an already-CRLF block.
	lineEnding := DetectLineEnding(block)
	if lineEnding == "\n" && DetectLineEnding(req.Source) == "\r\n" {
		lineEnding = "\r\n"
	}
	adaptedBlock := bytes.ReplaceAll(block, []byte("\r\n"), []byte("\n"))
	if lineEnding == "\r\n" {
		adaptedBlock = bytes.ReplaceAll(adaptedBlock, []byte("\n"), []byte("\r\n"))
	}
	node.SetContent(adaptedBlock) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.
	// Write the protocol version as a tag attribute on the region's opening tag.
	// The version source is the protocol document's frontmatter; only the destination changes.
	node.SetVersion(req.Protocol.Version) //nolint:errcheck // SetVersion errors only for invalid version strings or nodes without an open tag; neither applies here.
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  domain.InjectionProtocol,
		Action: RegionProtocolFilled,
		Bytes:  len(adaptedBlock),
	}, nil
}

// applyBundleRegion fills a bundle-sourced managed region with the role-matched block
// from req.Bundle.
//
// When req.Bundle is the zero value (not loaded), the region is cleared and recorded as
// emptied. This matches the pre-bundle behavior and keeps pre-migration call sites working:
// the app layer always loads the bundle before calling transform, so a zero-value Bundle
// only appears in tests that do not exercise bundle filling.
//
// When req.Bundle is loaded but has no block matching the region name and the file's role,
// the error ErrBundleBlockMissingForRole is returned. Deploying an empty region would produce
// an agent that appears complete and instructs nothing.
func applyBundleRegion(node *docformat.Node, name string, req Request) (RegionOutcome, error) {
	// Zero-value bundle: not loaded for this run. Leave region empty; no error.
	if req.Bundle.Version == "" && len(req.Bundle.Blocks) == 0 {
		node.Clear() //nolint:errcheck // Node.Clear always returns nil; forward-compatible error return.
		return RegionOutcome{
			Name:   name,
			Marker: node.Kind(),
			Class:  domain.InjectionBundle,
			Action: RegionEmptied,
			Bytes:  0,
		}, nil
	}

	block, ok := req.Bundle.BlockFor(name, req.Role)
	if !ok {
		return RegionOutcome{}, fmt.Errorf("region %q: %w", name, ErrBundleBlockMissingForRole)
	}

	node.SetContent(block) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  domain.InjectionBundle,
		Action: RegionBundleFilled,
		Bytes:  len(block),
	}, nil
}

// captureNestedUserRegions records the user-owned regions that must survive regeneration of
// the given source managed region node. It is called BEFORE the generator mutates the node.
//
// Two provenances are merged into one deterministically ordered result:
//
//  1. Deployed-file-origin: injection and custom direct children of the same-named
//     managed region in depDoc. These take indices 0..n-1 in deployed-file order.
//
//  2. Source-declared only: injection direct children of sourceNode that have no
//     counterpart in the deployed file. These take indices n.. in source declaration order.
//
// A source-declared injection that has a deployed-file counterpart takes the deployed-file
// index — it is one region, not two.
//
// depDoc may be nil (new deployment); only source-declared regions are then returned.
func captureNestedUserRegions(sourceNode *docformat.Node, depDoc *docformat.Document) []nestedRegionRecord {
	// Pass 1: collect user-owned regions from the deployed file's counterpart of sourceNode.
	var deployedUserRegions []nestedRegionRecord
	depIndexByName := make(map[string]int)

	if depDoc != nil {
		if depParent, ok := depDoc.Body().Deployed(sourceNode.Name()); ok {
			idx := 0
			for _, child := range depParent.Children() {
				// Only capture custom children from the deployed file here (deployed-only
				// provenance). Injection children must only enter via Pass 2 (source-declared),
				// because a nested injection absent from the source has been deliberately removed —
				// silently re-emitting it from the deployed tree would resurrect it forever and
				// prevent the user from actually deleting it.
				if child.Kind() == docformat.NodeCustom {
					deployedUserRegions = append(deployedUserRegions, nestedRegionRecord{
						Kind:  child.Kind(),
						Name:  child.Name(),
						Order: idx,
					})
					depIndexByName[child.Name()] = idx
					idx++
				}
			}
		}
	}

	n := len(deployedUserRegions)
	result := make([]nestedRegionRecord, n)
	copy(result, deployedUserRegions)

	// Pass 2: add source-declared injections not present in the deployed file (source-only).
	nextSourceOnly := n
	for _, child := range sourceNode.Children() {
		if child.Kind() != docformat.NodeInjection {
			continue
		}
		if _, inDeployed := depIndexByName[child.Name()]; inDeployed {
			// Already captured at the deployed-file index — one region, not two.
			continue
		}
		result = append(result, nestedRegionRecord{
			Kind:  docformat.NodeInjection,
			Name:  child.Name(),
			Order: nextSourceOnly,
		})
		nextSourceOnly++
	}

	// Sort by Order for deterministic emission (deployed-origin first, then source-only).
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})

	return result
}

// reemitNestedUserRegions appends the captured regions to the parent's regenerated content,
// inside the parent's tags, immediately after the canonical content. It is called AFTER the
// generator has run SetContent or Clear on the parent.
//
// Regions are emitted in nestedRegionRecord.Order order (deterministic; caller sorts before
// passing). Content is resolved by name from deployedContent; a name absent from the map
// produces an empty region.
//
// When captured is empty, the function is a no-op — the parent's content is exactly what
// the generator wrote: no separator, no trailing whitespace change (AC6.6 invariant).
//
// Returns one RegionOutcome per re-emitted region with the region's true marker kind
// (NodeInjection or NodeCustom), never the parent's NodeDeployed.
func reemitNestedUserRegions(parent *docformat.Node, captured []nestedRegionRecord, deployedContent map[string]regionEntry) ([]RegionOutcome, error) {
	if len(captured) == 0 {
		return nil, nil
	}

	var outcomes []RegionOutcome
	for _, rec := range captured {
		// Resolve content from the deployed region map; absent name → empty region.
		var content []byte
		if deployedContent != nil {
			if entry, ok := deployedContent[rec.Name]; ok {
				content = entry.Content
			}
		}

		if _, err := parent.AppendRegion(rec.Kind, rec.Name, content); err != nil {
			return nil, fmt.Errorf("reemit nested region %q: %w", rec.Name, err)
		}

		// Determine action: injections are preserved (content from deployed map) or added
		// (source-only, not in deployed map). Custom regions are always preserved-custom —
		// they exist only in deployed files, so they always come from the deployed map.
		var action RegionAction
		switch rec.Kind {
		case docformat.NodeCustom:
			action = RegionCustomPreserved
		case docformat.NodeInjection:
			if deployedContent != nil {
				if _, ok := deployedContent[rec.Name]; ok {
					action = RegionPreserved
				} else {
					action = RegionAdded
				}
			} else {
				action = RegionAdded
			}
		}

		outcomes = append(outcomes, RegionOutcome{
			Name:   rec.Name,
			Marker: rec.Kind,
			Class:  domain.InjectionProject,
			Action: action,
			Bytes:  len(content),
		})
	}
	return outcomes, nil
}

// anchorParentName returns the anchor parent name for a node: the name of its nearest
// enclosing section or managed region ancestor, or the empty string when the node sits
// at document top level (Parent == nil). This is the canonical identity used for
// re-parenting comparison.
func anchorParentName(node *docformat.Node) string {
	if node.Parent() == nil {
		return ""
	}
	return node.Parent().Name()
}

// buildDeployedInjectionParents returns a map from resolved injection name to anchor parent
// name for every injection node in the deployed document. Rename resolution is applied
// so the map is keyed on post-rename names, matching the keys in the deployedContent map.
//
// Returns nil when depDoc is nil (create run).
func buildDeployedInjectionParents(depDoc *docformat.Document, renames []RenameEntry) map[string]string {
	if depDoc == nil {
		return nil
	}

	injections := depDoc.Body().Injections()

	// Build the raw map: original deployed name → anchor parent name.
	rawParents := make(map[string]string, len(injections))
	for _, node := range injections {
		rawParents[node.Name()] = anchorParentName(node)
	}

	// Apply rename resolution identical to buildDeployedRegionMap: old names become new names
	// when the new name is absent, so the result keys match what applyProjectRegion sees.
	result := make(map[string]string, len(rawParents))
	for name, parent := range rawParents {
		result[name] = parent
	}
	for _, r := range renames {
		if _, oldPresent := rawParents[r.Old]; !oldPresent {
			continue
		}
		// Old name was found in the deployed file. Remove it from the result.
		delete(result, r.Old)
		if _, newPresent := rawParents[r.New]; !newPresent {
			// New name absent: migrate the old name's parent to the new name.
			result[r.New] = rawParents[r.Old]
		}
		// When the new name is also present, the new-name entry wins and the old entry is
		// already removed. No override needed — mirrors the buildDeployedRegionMap policy.
	}

	return result
}

// buildDeployedRegionMap parses the deployed file and returns a map from user-owned region
// name to its content and originating marker kind (NodeInjection or NodeCustom), plus the
// parsed document for anchor extraction and any removal gaps produced by rename resolution.
//
// Both injection and custom regions are included at any nesting depth, keyed by
// name with byte-identical content. Managed regions are excluded: their content is
// regenerated on every transform and is never recovered from the deployed file.
//
// Injection names are resolved through the rename table (renames) before being used as
// keys, so content stored under a renamed injection's old name is reachable at its new
// name. Custom names are never renamed. Resolution is a two-pass operation:
//
//  1. Collect all user regions into a raw map.
//  2. For each rename entry where the old name is present in the raw map:
//     - If the new name is absent: add it with Migrated=true (content migrated from old name).
//     - If the new name is already present: new name wins; old name's content is emitted as
//       a GapRemovedInjection so the user can recover it. This rule is order-independent
//       — it does not depend on which name appeared first in the deployed document.
//
// A Shape A collision (the same name appearing twice as a custom region in the deployed file)
// is detected here and returned as an error wrapping ErrRegionNameCollision, before any
// mutation.
//
// Returns nil, nil, nil, nil when deployed is nil. A parse failure is returned as an error
// rather than silently dropping the user's content.
func buildDeployedRegionMap(deployed []byte, renames []RenameEntry) (map[string]regionEntry, *docformat.Document, []domain.Gap, error) {
	if deployed == nil {
		return nil, nil, nil, nil
	}
	depDoc, err := docformat.Parse(deployed)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("deployed file is not a valid MOSAIC document: %w", err)
	}
	// UserRegions returns NodeInjection and NodeCustom regions at any depth in document order.
	// NodeDeployed content is regenerated on every transform and is never recovered.
	userRegions := depDoc.Body().UserRegions()

	// Pass 1: build the raw map with names exactly as they appear in the deployed file.
	// Shape A collision detection runs here (duplicate custom region names).
	rawMap := make(map[string]regionEntry, len(userRegions))
	for _, node := range userRegions {
		name := node.Name()
		// Detect Shape A collision: the same name appearing twice as a custom region in the
		// deployed file. Whichever entry would be written second would silently overwrite the
		// other's content; the tool aborts rather than guessing which copy to keep.
		if node.Kind() == docformat.NodeCustom {
			if existing, ok := rawMap[name]; ok && existing.Kind == docformat.NodeCustom {
				return nil, nil, nil, fmt.Errorf("region name %q: %w", name, ErrRegionNameCollision)
			}
		}
		rawMap[name] = regionEntry{Content: node.Content(), Kind: node.Kind()}
	}

	// Pass 2: apply rename resolution to produce the final map. This pass is order-independent
	// because it operates on the completed raw map rather than the document traversal order.
	// Custom names are never renamed — rename resolution applies to NodeInjection only.
	finalMap := make(map[string]regionEntry, len(rawMap))
	for name, entry := range rawMap {
		finalMap[name] = entry
	}

	var renameGaps []domain.Gap
	for _, r := range renames {
		oldEntry, oldPresent := rawMap[r.Old]
		if !oldPresent {
			// Old name absent from this deployed file — rename entry is a no-op here.
			continue
		}
		// Old name was found. Remove it from the final map; it must not appear as an orphan.
		delete(finalMap, r.Old)

		_, newPresent := rawMap[r.New]
		if newPresent {
			// Both old and new names are in the deployed file. The new name's own entry wins.
			// The old name's content is emitted as a removal gap so the user can recover it.
			renameGaps = append(renameGaps, domain.Gap{
				Kind:     domain.GapRemovedInjection,
				Subject:  r.Old,
				Fragment: string(oldEntry.Content),
			})
		} else {
			// Only the old name was present. Migrate its content to the new name.
			finalMap[r.New] = regionEntry{
				Content:  oldEntry.Content,
				Kind:     oldEntry.Kind,
				Migrated: true,
			}
		}
	}

	return finalMap, depDoc, renameGaps, nil
}

