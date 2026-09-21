package app

// parse_generic_source.go is the named entry point for parsing documents
// that are NOT deployed agent files.
//
// This file, along with deployed_read.go, is one of exactly two files in the
// application layer permitted to call the raw-bytes docformat entry points
// (docformat.Parse and docformat.SplitFrontmatter). Every other application-layer
// file routes all docformat byte-parsing through ParseGenericSource.
//
// Generic sources include: catalog source files, injection content,
// bundle-conformance inputs, render inputs, and the canonical form of
// already-decoded agent files (e.g. docformat.Parse on dr.Canonical after the
// decode funnel). Deployed agent files in their native format (e.g. Codex TOML)
// must be read through the decode funnel in deployed_read.go instead.
//
// The banned pair for the application layer is docformat.Parse and
// docformat.SplitFrontmatter — both raw-bytes entry points. The rest of the
// docformat package's application-layer surface (Validate, ClassifyRegion,
// CanonicalDeployed, CanonicalSections, CanonicalOrder, NodeDeployed,
// DeployedParent, RenderOpenTagLine, RenderCloseTagLine, RetypeOpenTagLine)
// operates on an already-parsed *docformat.Document or on region vocabulary,
// not on raw bytes, and is not banned.

import "mosaic-common/docformat"

// ParseGenericSource parses a MOSAIC document that is NOT a deployed agent file.
//
// It is one of the two permitted callers of docformat.Parse in the application
// layer; all other application-layer code calls this function rather than
// docformat.Parse directly.
//
// Callers include: bundle-conformance checking (rawBody from deployedBodies),
// the render pipeline (catalog source and path-specified source files),
// harness-only refresh (req.Deployed, which is canonical), deployed-state
// probing and workflow/protocol-version extraction (canonical bytes from the
// funnel), promote and retarget (source bytes), protocol-version extraction,
// source-model extraction (dr.Canonical after decode), workspace-agent scanning
// (canonical bytes), and deployed-agent index building (dr.Canonical).
func ParseGenericSource(src []byte) (*docformat.Document, error) {
	return docformat.Parse(src)
}

// bodyStartPos returns the byte offset in src where the document body begins,
// immediately after the frontmatter closing "---" delimiter line. When src
// carries no frontmatter, 0 is returned (the entire document is body).
//
// src is expected to be in canonical form (Markdown with YAML frontmatter).
// This function is called from the refresh write path on bytes that have already
// been through the decode funnel and are confirmed canonical: insertTopLevelDeployedRegion
// in harness_only_refresh.go passes currentBytes, which came from doc.Bytes() on a
// document parsed from req.Deployed (the funnel-decoded canonical form).
//
// This function calls docformat.SplitFrontmatter, the second raw-bytes docformat
// entry point. It lives here (in the one file besides deployed_read.go that is
// permitted to call both raw-bytes entry points) rather than in
// harness_only_refresh.go so the call-boundary guard is satisfied.
func bodyStartPos(src []byte) int {
	_, body, err := docformat.SplitFrontmatter(src)
	if err != nil || body == nil {
		// No frontmatter or parse error: the body starts at the beginning of src.
		return 0
	}
	return len(src) - len(body)
}
