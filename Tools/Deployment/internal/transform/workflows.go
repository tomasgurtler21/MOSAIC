package transform

// assembleWorkflowBlocks concatenates the workflow blocks in the order they appear in
// blocks, returning the assembled bytes and the corresponding workflow IDs in emitted
// order. The result is placed verbatim into the <AvailableWorkflows type="project"> region.
//
// Determinism is guaranteed by the caller's slice order, which reflects the user's
// selection order. This function never sorts or deduplicates: callers are responsible for
// providing a deduplicated, ordered list.
func assembleWorkflowBlocks(blocks []WorkflowBlock) (assembled []byte, ids []string) {
	ids = make([]string, 0, len(blocks))
	for _, wf := range blocks {
		assembled = append(assembled, wf.Block...)
		ids = append(ids, wf.ID)
	}
	return assembled, ids
}
