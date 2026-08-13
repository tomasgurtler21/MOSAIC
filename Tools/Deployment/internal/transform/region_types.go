package transform

import (
	"errors"
	"fmt"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// RegionAction classifies what happened to one managed region in the document body.
type RegionAction string

const (
	// RegionFilled means the harness module supplied content and it was written to the region.
	RegionFilled RegionAction = "filled-from-harness"
	// RegionPreserved means content from the deployed file was copied into the region.
	RegionPreserved RegionAction = "preserved-from-deployed"
	// RegionEmptied means the region was left empty.
	RegionEmptied RegionAction = "left-empty"
	// RegionAssembled means the AvailableWorkflows region was assembled from req.Workflows.
	RegionAssembled RegionAction = "assembled-workflows"
	// RegionAssembledInfra means the InfrastructureAgents region was assembled from
	// selected infrastructure agents.
	RegionAssembledInfra RegionAction = "assembled-infrastructure"
	// RegionProtocolFilled means the CommunicationProtocol region was filled with the
	// role-matched protocol block plus its version marker.
	RegionProtocolFilled RegionAction = "filled-from-protocol"
	// RegionOrphaned means content existed in the deployed file at a user-owned injection
	// point that no longer exists in the source.
	RegionOrphaned RegionAction = "orphaned"
	// RegionAdded means an injection point present in the source was absent from the
	// deployed file; it starts empty.
	RegionAdded RegionAction = "added"

	// RegionBundleFilled means a bundle-sourced region was filled with its role-matched
	// block content, verbatim.
	RegionBundleFilled RegionAction = "filled-from-bundle"

	// RegionCustomPreserved means a [[CUSTOM:]] region found in the deployed file was
	// carried into the output at its anchored position. The content is byte-identical to
	// the deployed file; no content is regenerated or reformatted.
	RegionCustomPreserved RegionAction = "preserved-custom"

	// RegionParked means a [[CUSTOM:]] region could not be anchored in the output after
	// a schema reorder and was appended at the end of the file with content intact. The
	// user must move it to the correct section.
	RegionParked RegionAction = "parked-at-end-of-file"

	// RegionMigrated means content stored in the deployed file under a renamed injection's
	// old name was carried into the region under its new name.
	RegionMigrated RegionAction = "migrated-from-renamed"
)

// RegionOutcome records what happened to one managed region in the document body.
// It covers [[INJECTION:]] (user-owned), [[DEPLOYED:]] (tool-managed), and [[CUSTOM:]]
// (project-invented, deployed-file only) regions, with the Marker field distinguishing them.
type RegionOutcome struct {
	Name   string
	Marker docformat.NodeKind // NodeInjection (user-owned), NodeDeployed (tool-managed), or NodeCustom (project-invented)
	Class  domain.InjectionClass
	Action RegionAction
	Bytes  int // byte length of the content placed in the region
}

// ToolManaged reports whether this outcome describes a tool-managed region.
func (o RegionOutcome) ToolManaged() bool { return o.Marker == docformat.NodeDeployed }

// ErrMarkerMismatch wraps docformat.ErrMarkerMismatch for a source document region whose
// canonical name is declared under the wrong boundary marker.
var ErrMarkerMismatch = docformat.ErrMarkerMismatch

// ErrUnknownDeployedName wraps docformat.ErrUnknownDeployedName for a source document
// region that declares a [[DEPLOYED:]] boundary whose name is not in the tool-managed
// registry. An unrecognised name has no generator and cannot be filled.
var ErrUnknownDeployedName = docformat.ErrUnknownDeployedName

// ErrProtocolContentMissing reports that no protocol block was supplied for the agent's role,
// or that the supplied block is empty (whitespace-only counts as empty).
var ErrProtocolContentMissing = errors.New("no protocol content for agent role")

// ErrBundleBlockMissingForRole reports a bundle-sourced region present in the document for
// which the bundle declares no block matching the file's role. Deploying it empty would ship
// an agent that appears complete and instructs nothing, so this is a hard error.
var ErrBundleBlockMissingForRole = errors.New("no bundle block for region and role")

// ErrSourceDeployedRegionNotEmpty reports a [[DEPLOYED:]] region carrying content in a
// source file. Source regions are empty by definition; content there is either a hand-edit
// about to be discarded or a deployed file mistakenly committed as source.
var ErrSourceDeployedRegionNotEmpty = errors.New("source file carries a populated deployed region")

// ErrRegionNameCollision reports a name that is claimed by two user-owned regions at once:
// either the same name appears twice as [[CUSTOM:]] in the deployed file, or the same name
// appears as [[CUSTOM:]] in the deployed file and [[INJECTION:]] in the source. Preserving
// both would break the unique-name-per-file invariant; preserving one would silently lose
// the other's content. The transform never guesses which copy to keep.
//
// The error message names the colliding name and where each occurrence lives.
var ErrRegionNameCollision = errors.New("region name claimed by two user-owned regions")

// DetectLineEnding reports the line terminator that content uses: "\r\n" or "\n".
//
// The first line terminator in content decides. Content with no line terminator, and
// empty content, yield "\n".
//
// Detection is first-terminator-wins by deliberate choice: the transform never rewrites
// existing bytes, so mixed-ending content keeps whatever it already has and this function
// only answers what a newly emitted line should use to match its neighbours.
func DetectLineEnding(content []byte) string {
	for i, b := range content {
		if b == '\n' {
			if i > 0 && content[i-1] == '\r' {
				return "\r\n"
			}
			return "\n"
		}
	}
	return "\n"
}

// ProtocolVersionComment renders the version marker line written as the first line of the
// deployed protocol region, terminated to match content's own line ending:
// "<!-- protocol-version: 1.10 -->\r\n" for CRLF content, "<!-- protocol-version: 1.10 -->\n"
// for LF content and for content with no line ending at all.
//
// content is the protocol block the marker line will be prepended to. Passing nil is legal
// and yields the LF form.
func ProtocolVersionComment(version string, content []byte) string {
	return fmt.Sprintf("<!-- protocol-version: %s -->%s", version, DetectLineEnding(content))
}
