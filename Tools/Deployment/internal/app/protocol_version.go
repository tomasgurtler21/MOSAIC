package app

import (
	"bytes"
	"regexp"
)

// protocolVersionPattern matches a protocol-version HTML comment inside the deployed
// CommunicationProtocol region: <!-- protocol-version: X -->
//
// The shape mirrors workflowVersionPattern and is used by extractDeployedProtocolVersion
// to read back the version marker that transform.applyProtocolRegion writes.
var protocolVersionPattern = regexp.MustCompile(`<!--\s*protocol-version:\s*(\S+)\s*-->`)

// protocolRegionOpen and protocolRegionClose are the boundary tag bytes that delimit the
// deployed protocol region.
var (
	protocolRegionOpen  = []byte("[[DEPLOYED:CommunicationProtocol]]")
	protocolRegionClose = []byte("[[/DEPLOYED:CommunicationProtocol]]")
)

// extractDeployedProtocolVersion returns the protocol version recorded inside the deployed
// file's [[DEPLOYED:CommunicationProtocol]] region's <!-- protocol-version: X --> comment.
//
// Returns "" when the region is absent, when it carries no version comment, or when the file
// is malformed. Extraction is scoped to the region's own bytes so a comment elsewhere in the
// file is never mistaken for the protocol version. Like the workflow extractor, this function
// degrades gracefully and never fails the scan.
func extractDeployedProtocolVersion(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// Find the opening tag.
	openIdx := bytes.Index(data, protocolRegionOpen)
	if openIdx < 0 {
		return ""
	}

	// Content starts just after the opening tag.
	contentStart := openIdx + len(protocolRegionOpen)

	// Find the closing tag within the remaining bytes.
	closeIdx := bytes.Index(data[contentStart:], protocolRegionClose)

	var regionContent []byte
	if closeIdx >= 0 {
		regionContent = data[contentStart : contentStart+closeIdx]
	} else {
		// Unclosed region: degrade gracefully — scan whatever content is present.
		regionContent = data[contentStart:]
	}

	// Extract the first protocol-version comment within the region.
	m := protocolVersionPattern.FindSubmatch(regionContent)
	if m == nil {
		return ""
	}
	return string(m[1])
}
