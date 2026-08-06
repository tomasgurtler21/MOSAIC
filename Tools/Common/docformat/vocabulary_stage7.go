package docformat

// DocumentKind tells the validator whether it is looking at a source file or a deployed
// file. It selects the rules that differ between the two (rule 12: source deployed regions
// must be empty).
type DocumentKind string

const (
	// DocumentUnknown is the zero value. When Kind is DocumentUnknown, kind-specific rules
	// (rule 12) are skipped.
	DocumentUnknown DocumentKind = ""

	// DocumentSource indicates a source agent file. Source files must carry only empty
	// [[DEPLOYED:]] regions (rule 12); populated regions indicate a hand-edit or a deployed
	// file committed as source.
	DocumentSource DocumentKind = "source"

	// DocumentDeployed indicates a file that has been produced by the deployment tool.
	// Deployed files are not subject to rule 12 (they legitimately carry populated regions).
	DocumentDeployed DocumentKind = "deployed"
)
