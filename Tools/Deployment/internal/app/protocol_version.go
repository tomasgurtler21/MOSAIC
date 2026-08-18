package app

import "mosaic-common/docformat"

// extractDeployedProtocolVersion returns the protocol version recorded in the version
// attribute on the deployed file's CommunicationProtocol region's opening tag.
//
// Returns "" when the region is absent, when it carries no version attribute, or when
// the file is malformed. Extraction uses the parser rather than byte search, so any
// leniently formatted open tag is handled correctly. Like the workflow extractor, this
// function degrades gracefully and never fails the scan.
func extractDeployedProtocolVersion(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return ""
	}
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		return ""
	}
	return node.Version()
}
