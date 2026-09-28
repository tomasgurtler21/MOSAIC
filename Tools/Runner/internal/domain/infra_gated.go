package domain

// IsGatedInfraClass reports whether class is one of the gated infrastructure
// classes (checkpoint, commit, restore), the only classes that may appear in
// the infrastructure_selections map.
func IsGatedInfraClass(class string) bool {
	switch class {
	case "checkpoint", "commit", "restore":
		return true
	}
	return false
}
