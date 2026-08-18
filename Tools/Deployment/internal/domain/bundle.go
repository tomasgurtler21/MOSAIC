package domain

import "bytes"

// BundleBlock is one canonical block of the deployed-sections bundle: its declaration
// fields plus the payload extracted from the document body.
type BundleBlock struct {
	// Name is the compound section name, e.g. "AuthorityHierarchy:Subagent". It is the key
	// used to find the payload in the bundle body and is never a deployed region name.
	Name string
	// Target is the managed region name this block fills, e.g. "AuthorityHierarchy".
	Target string
	// AppliesTo is the role vocabulary value: "subagent" or "orchestrator".
	AppliesTo string
	// SpecifiedIn is the design document path recorded in the declaration, relative to the
	// MOSAIC root. Carried for conformance rule 20; never read during filling.
	SpecifiedIn string
	// Content is the block's inner content, verbatim, excluding its own tag lines.
	Content []byte
}

// BundleContent is the deployable payload of the deployed-sections bundle, loaded once per
// run by the app layer. The zero value is invalid and is rejected by the transform.
type BundleContent struct {
	// Version is the bundle's frontmatter `bundle_version` scalar, verbatim. Stamped into
	// deployed frontmatter and compared by the planner.
	Version string
	// Blocks holds every declared block, in declaration order.
	Blocks []BundleBlock
}

// BlockFor returns the block whose Target equals target and whose AppliesTo matches role.
// ok is false when no block matches, or when the matching block has whitespace-only content.
// Matching is on the declared Target and AppliesTo fields, never on Name.
func (b BundleContent) BlockFor(target string, role AgentRole) (block []byte, ok bool) {
	for _, blk := range b.Blocks {
		if blk.Target == target && AgentRole(blk.AppliesTo) == role {
			if len(bytes.TrimSpace(blk.Content)) == 0 {
				return nil, false
			}
			return blk.Content, true
		}
	}
	return nil, false
}

// AppliesToRole reports whether any block in the bundle applies to role. Used to decide
// whether a deployed file of that role receives a bundle_version stamp.
func (b BundleContent) AppliesToRole(role AgentRole) bool {
	for _, blk := range b.Blocks {
		if AgentRole(blk.AppliesTo) == role {
			return true
		}
	}
	return false
}
