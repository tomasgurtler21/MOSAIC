package app

// harness_only_refresh.go implements refreshHarnessOnly, the narrow content-refresh routine
// for harness-only agents. It regenerates in-scope [[DEPLOYED:]] regions from canonical
// protocol and bundle content, leaving every other byte — injection content, section
// structure, frontmatter, and out-of-scope deployed regions — byte-identical to the input.
//
// The defining constraint for harness-only agents: there is no generic source.
// The file on disk is the source of truth. transform.Apply is built around a generic source
// whose [[DEPLOYED:]] regions are empty placeholders and rejects a populated source region
// with transform.ErrSourceDeployedRegionNotEmpty. This routine therefore takes the dedicated-
// narrow-routine approach: walk the parsed document's regions directly and rewrite only the
// in-scope deployed regions. transform.ErrSourceDeployedRegionNotEmpty is never returned here.

import (
	"bytes"
	"fmt"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// HarnessOnlyRefreshRequest carries every input refreshHarnessOnly needs. Like
// transform.Apply, the routine performs no filesystem, network, clock, or randomness
// operation; all inputs arrive here.
type HarnessOnlyRefreshRequest struct {
	// Deployed is the current file bytes, verbatim. This is the source of truth.
	Deployed []byte
	// Scope selects which [[DEPLOYED:]] regions are regenerated.
	Scope RefreshScope
	// Role selects the protocol variant and the bundle block variant. Taken from the
	// file's own frontmatter `role`, defaulting to domain.RoleSubagent.
	Role domain.AgentRole
	// Protocol carries the role-keyed protocol blocks and the protocol source version,
	// loaded once per run by the Update flow.
	Protocol domain.ProtocolContent
	// Bundle carries the canonical blocks and the bundle version, loaded once per run.
	Bundle domain.BundleContent
	// Subject names the file for error messages returned by refreshHarnessOnly; typically
	// the agent's TargetPath. It never affects output bytes.
	//
	// Subject is used ONLY to decorate returned errors (e.g. a wrapped parse error).
	// refreshHarnessOnly performs no logging of its own and holds no logging port — it is
	// a pure function. All log events for a harness-only refresh are emitted by the
	// Update flow caller. An implementer must not add a logging call inside this function.
	Subject string
}

// HarnessOnlyRefreshResult is the output of a successful refresh.
type HarnessOnlyRefreshResult struct {
	// Output is the refreshed file bytes.
	Output []byte
	// Regions records what happened to each region touched, reusing the transform
	// package's audit type so run logging is uniform across both paths.
	Regions []transform.RegionOutcome
	// Added names the in-scope regions that were absent from the input and were
	// inserted, in canonical order.
	Added []string
	// NotAdded names in-scope regions that were absent and could NOT be inserted
	// because their required parent section is missing from the file. These are
	// reported, never forced.
	NotAdded []string
}

// refreshHarnessOnly regenerates the in-scope tool-managed regions of a harness-only
// agent and returns the refreshed bytes.
//
// A harness-only agent with no `version` field in its frontmatter is always eligible for
// refresh. The version field is not consulted by this routine and an absent version never
// causes a failure — this is the explicit, documented fallback for harness-only agents,
// which commonly carry no version because there is no catalog source to derive one from.
// The sole gate on whether a refresh runs is the user's explicit scope answer, applied by
// the Update flow caller before invoking this function.
func refreshHarnessOnly(req HarnessOnlyRefreshRequest) (HarnessOnlyRefreshResult, error) {
	// Parse the deployed bytes. An unparseable document returns an error; callers reach
	// this routine only for files the discovery scan already parsed, so this is defensive.
	doc, err := docformat.Parse(req.Deployed)
	if err != nil {
		return HarnessOnlyRefreshResult{}, fmt.Errorf("parse deployed document %q: %w", req.Subject, err)
	}

	// Determine which regions are in scope. Regions() returns them in canonical order
	// (matching docformat.CanonicalDeployed), so processing them in this order is
	// deterministic.
	scopeRegions := req.Scope.Regions()

	// Build a fast lookup for in-scope names.
	inScope := make(map[string]bool, len(scopeRegions))
	for _, name := range scopeRegions {
		inScope[name] = true
	}

	// Walk every deployed region currently in the document to find which in-scope regions
	// are present. Pre-compute content only for regions that ARE present in the file — a
	// content error for a present region is a hard error (shipping an empty region to a
	// file that already declares it would produce an agent that appears complete and
	// instructs nothing). Content for absent regions is computed lazily below.
	presentDeployed := make(map[string]bool)
	for _, node := range doc.Body().DeployedRegions() {
		presentDeployed[node.Name()] = true
	}

	canonicalContent := make(map[string][]byte, len(scopeRegions))
	for _, regionName := range scopeRegions {
		if !presentDeployed[regionName] {
			continue // will be computed lazily in the insertion loop
		}
		content, computeErr := harnessOnlyRegionContent(regionName, req)
		if computeErr != nil {
			return HarnessOnlyRefreshResult{}, fmt.Errorf("region %q: %w", regionName, computeErr)
		}
		canonicalContent[regionName] = content
	}

	// Walk every deployed region currently in the document, in document order.
	// In-scope regions are updated via SetContent; out-of-scope regions are left untouched,
	// which preserves their bytes byte-identically when the document is serialised.
	var outcomes []transform.RegionOutcome

	for _, node := range doc.Body().DeployedRegions() {
		name := node.Name()

		if !inScope[name] {
			// Out of scope: do not call SetContent; the node's existing bytes are preserved.
			continue
		}

		content := canonicalContent[name]
		_ = node.SetContent(content) // SetContent always returns nil (forward-compatible signature).

		class, _ := docformat.ClassifyRegion(docformat.NodeDeployed, name)
		outcomes = append(outcomes, transform.RegionOutcome{
			Name:   name,
			Marker: docformat.NodeDeployed,
			Class:  class,
			Action: harnessOnlyRegionAction(class),
			Bytes:  len(content),
		})
	}

	// Serialise the document after updating existing in-scope regions. Missing-region
	// insertions are performed at the byte level on this serialised form, so their content
	// is added without re-parsing and without affecting the bytes the mutation API already
	// produced.
	currentBytes := doc.Bytes()

	// Handle missing in-scope regions. Process them in scopeRegions (canonical) order so
	// the output is deterministic regardless of Go map iteration order.
	var added []string
	var notAdded []string

	for _, regionName := range scopeRegions {
		if presentDeployed[regionName] {
			continue // already present and updated above
		}

		// This in-scope region is absent from the input. Compute content lazily.
		// If content cannot be provided (e.g. no bundle block for this region and role),
		// record as not-added rather than returning an error: the file does not declare the
		// region, so no incomplete or empty region would be shipped. This differs from the
		// hard-error case above, where the region IS in the file.
		content, computeErr := harnessOnlyRegionContent(regionName, req)
		if computeErr != nil {
			notAdded = append(notAdded, regionName)
			continue
		}

		// Attempt to insert it at the position the vocabulary dictates.
		regionBytes := buildDeployedRegionBytes(regionName, content)
		parent := docformat.DeployedParent[regionName]

		if parent == "" {
			// Top-level region (currently only CommunicationProtocol). Insert after the
			// close tag of the last preceding canonical section present in the file,
			// or at the top of the body when no such section is present.
			currentBytes = insertTopLevelDeployedRegion(currentBytes, regionName, regionBytes, doc)
			added = append(added, regionName)
		} else {
			// Parent-section region. Insert before the parent section's close tag if the
			// section is present. If the section is absent, the region cannot be inserted
			// without inventing a [[SECTION:]] tag — which this routine must never do.
			if _, ok := doc.Body().Section(parent); ok {
				currentBytes = insertSectionDeployedRegion(currentBytes, regionBytes, parent)
				added = append(added, regionName)
			} else {
				// Parent section absent: record as not-added and move on.
				notAdded = append(notAdded, regionName)
				continue
			}
		}

		class, _ := docformat.ClassifyRegion(docformat.NodeDeployed, regionName)
		outcomes = append(outcomes, transform.RegionOutcome{
			Name:   regionName,
			Marker: docformat.NodeDeployed,
			Class:  class,
			Action: transform.RegionAdded,
			Bytes:  len(content),
		})
	}

	return HarnessOnlyRefreshResult{
		Output:   currentBytes,
		Regions:  outcomes,
		Added:    added,
		NotAdded: notAdded,
	}, nil
}

// harnessOnlyRegionContent computes the canonical content for one in-scope deployed region.
//
// CommunicationProtocol is filled with the protocol version marker line followed by the
// role-matched protocol block, exactly as the catalog-backed path fills it.
//
// Every other in-scope region is filled with its role-matched bundle block.
func harnessOnlyRegionContent(regionName string, req HarnessOnlyRefreshRequest) ([]byte, error) {
	if regionName == "CommunicationProtocol" {
		block, ok := req.Protocol.For(req.Role)
		if !ok {
			return nil, transform.ErrProtocolContentMissing
		}
		versionComment := []byte(transform.ProtocolVersionComment(req.Protocol.Version))
		content := make([]byte, 0, len(versionComment)+len(block))
		content = append(content, versionComment...)
		content = append(content, block...)
		return content, nil
	}
	// All other in-scope deployed regions are filled from the bundle.
	block, ok := req.Bundle.BlockFor(regionName, req.Role)
	if !ok {
		return nil, transform.ErrBundleBlockMissingForRole
	}
	return block, nil
}

// harnessOnlyRegionAction maps an injection class to the RegionAction recorded in the
// outcome, keeping the audit trail consistent with the catalog-backed path.
func harnessOnlyRegionAction(class domain.InjectionClass) transform.RegionAction {
	switch class {
	case domain.InjectionProtocol:
		return transform.RegionProtocolFilled
	case domain.InjectionBundle:
		return transform.RegionBundleFilled
	default:
		// InjectionHarness, InjectionWorkflow, InjectionInfrastructure — treated as
		// filled-from-harness in the audit trail.
		return transform.RegionFilled
	}
}

// buildDeployedRegionBytes serialises a deployed region as:
//
//	[[DEPLOYED:Name]]\n
//	<content>
//	[[/DEPLOYED:Name]]\n
//
// The open tag, content, and close tag are each on their own line, matching how docformat
// serialises an existing deployed region via bodyBytes().
func buildDeployedRegionBytes(name string, content []byte) []byte {
	openTag := fmt.Sprintf("[[DEPLOYED:%s]]\n", name)
	closeTag := fmt.Sprintf("[[/DEPLOYED:%s]]\n", name)
	var buf bytes.Buffer
	buf.WriteString(openTag)
	buf.Write(content)
	buf.WriteString(closeTag)
	return buf.Bytes()
}

// insertTopLevelDeployedRegion inserts regionBytes into src for a top-level deployed region
// (one whose docformat.DeployedParent value is ""). The insertion point is immediately after
// the close tag of the last preceding canonical slot in docformat.CanonicalOrder that is
// present in the document as a section. When no such preceding section is found, the region
// is inserted at the top of the document body (immediately after the frontmatter delimiter).
//
// A blank line is prepended to the inserted bytes to separate the new region from the
// content that precedes it.
func insertTopLevelDeployedRegion(src []byte, regionName string, regionBytes []byte, doc *docformat.Document) []byte {
	// Locate this region's slot in CanonicalOrder.
	regionIdx := -1
	for i, name := range docformat.CanonicalOrder {
		if name == regionName {
			regionIdx = i
			break
		}
	}

	// Try each preceding slot (in reverse order) to find a section that exists in the
	// document. When found, insert immediately after its close tag.
	if regionIdx > 0 {
		for i := regionIdx - 1; i >= 0; i-- {
			precedingName := docformat.CanonicalOrder[i]
			if _, ok := doc.Body().Section(precedingName); !ok {
				// Section is not present in this document — try the one before it.
				continue
			}

			// Prefer CRLF close tags if present; fall back to LF.
			closeTagCRLF := []byte(fmt.Sprintf("[[/SECTION:%s]]\r\n", precedingName))
			closeTagLF := []byte(fmt.Sprintf("[[/SECTION:%s]]\n", precedingName))

			var closeTag []byte
			if bytes.Contains(src, closeTagCRLF) {
				closeTag = closeTagCRLF
			} else if bytes.Contains(src, closeTagLF) {
				closeTag = closeTagLF
			}

			if len(closeTag) == 0 {
				// Close tag not found in bytes — skip (defensive; should not happen for
				// a document that was just serialised from a parsed, valid structure).
				continue
			}

			idx := bytes.LastIndex(src, closeTag)
			if idx < 0 {
				continue
			}
			insertPos := idx + len(closeTag)
			// Prepend a blank line so the inserted region is visually separated from the
			// section close tag.
			toInsert := append([]byte("\n"), regionBytes...)
			return insertAt(src, insertPos, toInsert)
		}
	}

	// No preceding canonical section found in the document. Insert at the start of the body
	// (immediately after the frontmatter closing delimiter). A trailing blank line is appended
	// to separate the inserted region from whatever follows it in the body.
	bodyStart := bodyStartPos(src)
	toInsert := append(regionBytes, '\n')
	return insertAt(src, bodyStart, toInsert)
}

// insertSectionDeployedRegion inserts regionBytes into src immediately before the close tag
// of the given parent section. The parent section must exist in the document (the caller
// is responsible for verifying this before calling).
func insertSectionDeployedRegion(src []byte, regionBytes []byte, parentSection string) []byte {
	// Prefer CRLF close tags if present; fall back to LF.
	closeTagCRLF := []byte(fmt.Sprintf("[[/SECTION:%s]]\r\n", parentSection))
	closeTagLF := []byte(fmt.Sprintf("[[/SECTION:%s]]\n", parentSection))

	var closeTag []byte
	if bytes.Contains(src, closeTagCRLF) {
		closeTag = closeTagCRLF
	} else {
		closeTag = closeTagLF
	}

	idx := bytes.LastIndex(src, closeTag)
	if idx < 0 {
		// Defensive fallback: close tag not found. Return src unchanged rather than panic.
		return src
	}
	return insertAt(src, idx, regionBytes)
}

// insertAt returns a new byte slice with insert injected into src at byte position pos.
// src is not modified.
func insertAt(src []byte, pos int, insert []byte) []byte {
	result := make([]byte, 0, len(src)+len(insert))
	result = append(result, src[:pos]...)
	result = append(result, insert...)
	result = append(result, src[pos:]...)
	return result
}

// bodyStartPos returns the byte offset in src where the document body begins — immediately
// after the frontmatter closing "---" delimiter line. When src carries no frontmatter,
// 0 is returned (the entire document is body).
func bodyStartPos(src []byte) int {
	_, body, err := docformat.SplitFrontmatter(src)
	if err != nil || body == nil {
		// No frontmatter or parse error: the body starts at the beginning of src.
		return 0
	}
	return len(src) - len(body)
}
