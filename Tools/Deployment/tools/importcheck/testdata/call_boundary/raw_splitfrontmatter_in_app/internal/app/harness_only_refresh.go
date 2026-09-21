package app

// harness_only_refresh.go is a test fixture representing a forbidden raw
// docformat.SplitFrontmatter call in the application layer. The read-boundary
// guard must reject any call to docformat.SplitFrontmatter from an
// application-layer file that is not the decode funnel (deployed_read.go) or
// the named generic-source operation.
//
// This file exists to prove that the guard fires for BOTH banned raw-bytes entry
// points, not only docformat.Parse. A rule banning only Parse would leave a hole:
// SplitFrontmatter also consumes raw document bytes and can misread a format.

import (
	"mosaic-common/docformat"
)

// bodyStartPos calls docformat.SplitFrontmatter directly on raw bytes from the
// application layer. This is the second pattern the read-boundary guard must
// reject. In the production tree this call was made canonical-bytes-safe by the
// refresh stage; this fixture proves the guard fires for calls to SplitFrontmatter
// from unguarded call sites.
func bodyStartPos(src []byte) int {
	_, body, _ := docformat.SplitFrontmatter(src)
	return len(src) - len(body)
}
