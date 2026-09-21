package app

// deployed_read.go is a test fixture representing the decode funnel, which is
// one of exactly two files in the application layer permitted to call the
// raw-bytes docformat entry points (docformat.Parse and docformat.SplitFrontmatter).
//
// The read-boundary guard must ACCEPT calls to these entry points from this file.
// The permission is expressed as the filename "deployed_read.go": all three funnel
// layers live in this one file, so permitting the file is the complete expression
// of the permission.
//
// This fixture exists to prove the companion acceptance case: the guard does not
// degrade into rejecting everything, only raw-bytes calls from files that are
// neither the funnel nor the named generic-source operation.

import (
	"mosaic-common/docformat"
)

// decodeDeployedBytes is Layer 1 of the decode funnel. It calls docformat.Parse
// directly because it is the authorised entry point for deployed-artifact decodes.
// The guard must not reject this call.
func decodeDeployedBytes(deployed []byte) {
	_, _ = docformat.Parse(deployed)
}

// bodyStartPos calls docformat.SplitFrontmatter on bytes that have already been
// confirmed canonical by the funnel's own encoding step. The guard must not reject
// this call either: the funnel file is one of the two permitted callers of both
// raw-bytes entry points.
func bodyStartPos(src []byte) int {
	_, body, _ := docformat.SplitFrontmatter(src)
	return len(src) - len(body)
}
