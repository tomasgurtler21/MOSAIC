package app

import (
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// extractDeployedInfrastructure reads the infrastructure agent declarations from a deployed
// document through the transform reader. Returns nil when the file declares none, so files
// without an InfrastructureAgents region carry no entries. Duplicate keys keep their first
// occurrence.
func extractDeployedInfrastructure(data []byte) domain.DeployedInfrastructureDeclarations {
	declared := transform.ReadInfrastructureDeclarations(data)
	if len(declared) == 0 {
		return nil
	}
	var result domain.DeployedInfrastructureDeclarations
	seen := make(map[string]bool, len(declared))
	for _, d := range declared {
		if seen[d.Key] {
			continue
		}
		seen[d.Key] = true
		entry := domain.DeployedInfrastructureDeclaration{Key: d.Key, Version: d.Version, Parsed: d.Parsed}
		if d.Parsed {
			entry.Class = d.Block.Class
			entry.Triggers = d.Block.Triggers
			entry.OnFailure = d.Block.OnFailure
		}
		result = append(result, entry)
	}
	return result
}
