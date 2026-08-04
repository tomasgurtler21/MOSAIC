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
)

// RegionOutcome records what happened to one managed region in the document body.
// It covers both [[INJECTION:]] (user-owned) and [[DEPLOYED:]] (tool-managed) regions,
// with the Marker field distinguishing them.
type RegionOutcome struct {
	Name   string
	Marker docformat.NodeKind // NodeInjection (user-owned) or NodeDeployed (tool-managed)
	Class  domain.InjectionClass
	Action RegionAction
	Bytes  int // byte length of the content placed in the region
}

// ToolManaged reports whether this outcome describes a tool-managed region.
func (o RegionOutcome) ToolManaged() bool { return o.Marker == docformat.NodeDeployed }

// ErrMarkerMismatch wraps docformat.ErrMarkerMismatch for a source document region whose
// canonical name is declared under the wrong boundary marker.
var ErrMarkerMismatch = docformat.ErrMarkerMismatch

// ErrProtocolContentMissing reports that no protocol block was supplied for the agent's role,
// or that the supplied block is empty (whitespace-only counts as empty).
var ErrProtocolContentMissing = errors.New("no protocol content for agent role")

// ProtocolVersionComment renders the version marker line written as the first line of the
// deployed protocol region: "<!-- protocol-version: 1.9 -->\n".
func ProtocolVersionComment(version string) string {
	return fmt.Sprintf("<!-- protocol-version: %s -->\n", version)
}
