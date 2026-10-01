package domain

// DeployedInfrastructureDeclaration is one <InfrastructureAgent> section found in a deployed
// orchestrator-role file.
type DeployedInfrastructureDeclaration struct {
	Key       string
	Version   string // tag version attribute; "" when absent
	Parsed    bool   // false: shape not recognised; Class/Triggers/OnFailure empty
	Class     string
	Triggers  []InfrastructureTrigger // TriggerParam "" for "-"
	OnFailure string
}

// DeployedInfrastructureDeclarations is the ordered, first-occurrence-deduplicated list of
// infrastructure agent declarations in a deployed file.
type DeployedInfrastructureDeclarations []DeployedInfrastructureDeclaration

// Keys returns the declared keys in order; nil when empty.
func (d DeployedInfrastructureDeclarations) Keys() []string {
	if len(d) == 0 {
		return nil
	}
	keys := make([]string, len(d))
	for i, decl := range d {
		keys[i] = decl.Key
	}
	return keys
}

// Lookup returns the declaration for key; the second result is false when it is not declared.
func (d DeployedInfrastructureDeclarations) Lookup(key string) (DeployedInfrastructureDeclaration, bool) {
	for _, decl := range d {
		if decl.Key == key {
			return decl, true
		}
	}
	return DeployedInfrastructureDeclaration{}, false
}
