package transform

import (
	"mosaic-common/docformat"
)

// assembleWorkflowBlocks concatenates the workflow blocks in the order they appear in
// blocks, returning the assembled bytes and the corresponding workflow IDs in emitted
// order. The result is placed into the <AvailableWorkflows type="managed"> region.
//
// For each block, the first line is examined: if it is an opening boundary tag, its
// type attribute is rewritten to "managed" before emission so that the injected block
// carries type="managed" to match the surrounding AvailableWorkflows region. The source
// WorkflowBlock.Block slices are never mutated; every other byte of each block is emitted
// verbatim. A block whose first line is not an opening boundary tag is emitted entirely
// verbatim.
//
// Determinism is guaranteed by the caller's slice order, which reflects the user's
// selection order. This function never sorts or deduplicates: callers are responsible for
// providing a deduplicated, ordered list.
func assembleWorkflowBlocks(blocks []WorkflowBlock) (assembled []byte, ids []string) {
	ids = make([]string, 0, len(blocks))
	for _, wf := range blocks {
		block := wf.Block
		// Rewrite the opening tag's type attribute to "managed" on the first line.
		// splitLinesPreservingFirst returns the first line and the remainder so that we
		// only touch the opening tag and emit the rest byte-identically.
		if len(block) > 0 {
			firstLineEnd := firstLineLen(block)
			firstLine := block[:firstLineEnd]
			rest := block[firstLineEnd:]
			if retyped, ok := docformat.RetypeOpenTagLine(firstLine, docformat.NodeDeployed); ok {
				assembled = append(assembled, retyped...)
				assembled = append(assembled, rest...)
			} else {
				// First line is not a recognised opening boundary tag — emit verbatim.
				assembled = append(assembled, block...)
			}
		}
		ids = append(ids, wf.ID)
	}
	return assembled, ids
}

// firstLineLen returns the byte length of the first line in data, including any line
// terminator (LF or CRLF). If data contains no newline, its full length is returned.
func firstLineLen(data []byte) int {
	for i, b := range data {
		if b == '\n' {
			return i + 1
		}
	}
	return len(data)
}
