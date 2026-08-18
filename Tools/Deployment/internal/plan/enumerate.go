package plan

import (
	"fmt"
	"path/filepath"

	"mosaic-deploy/internal/domain"
)

// PlannedPath pairs an artifact with the target path this run would write it to.
type PlannedPath struct {
	Ref        domain.ArtifactRef
	TargetPath string // relative to the deployment root
}

// PlannedPaths is the ordered result of EnumerateTargetPaths: agents, then skills, then hooks,
// each in ArtifactSet order. Hook bundles the harness does not support are omitted.
type PlannedPaths []PlannedPath

// Path returns the target path for ref; the second result is false when ref was not planned.
func (p PlannedPaths) Path(ref domain.ArtifactRef) (string, bool) {
	for _, pp := range p {
		if pp.Ref == ref {
			return pp.TargetPath, true
		}
	}
	return "", false
}

// EnumerateTargetPaths resolves the target path of every artifact in set, in the same order
// and with the same semantics Build uses. It performs no file-system access: it only calls
// module.TargetPath, which is pure string manipulation.
//
// Agents and skills whose path cannot be resolved return a wrapped error, matching Build's
// current behaviour. Hooks whose path cannot be resolved are skipped silently (the harness
// does not support that hook bundle), also matching Build.
func EnumerateTargetPaths(
	set ArtifactSet,
	module domain.HarnessModule,
	scope domain.Scope,
	goos string,
) (PlannedPaths, error) {
	result := make(PlannedPaths, 0, len(set.Agents)+len(set.Skills)+len(set.Hooks))

	for _, agent := range set.Agents {
		targetPath, err := module.TargetPath(domain.TargetPathRequest{
			Kind:     domain.ArtifactAgent,
			Key:      agent.Key,
			FileName: filepath.Base(agent.SourcePath),
			Scope:    scope,
			GOOS:     goos,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve target path for agent %q: %w", agent.Key, err)
		}
		result = append(result, PlannedPath{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key},
			TargetPath: targetPath,
		})
	}

	for _, skill := range set.Skills {
		targetPath, err := module.TargetPath(domain.TargetPathRequest{
			Kind:     domain.ArtifactSkill,
			Key:      skill.Key,
			FileName: skill.EntryFile,
			Scope:    scope,
			GOOS:     goos,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve target path for skill %q: %w", skill.Key, err)
		}
		result = append(result, PlannedPath{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: skill.Key},
			TargetPath: targetPath,
		})
	}

	for _, hook := range set.Hooks {
		targetPath, err := module.TargetPath(domain.TargetPathRequest{
			Kind:  domain.ArtifactHook,
			Key:   hook.Key,
			Scope: scope,
			GOOS:  goos,
		})
		if err != nil {
			// Hook not supported by this harness — skip silently, matching Build's behaviour.
			continue
		}
		result = append(result, PlannedPath{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactHook, Key: hook.Key},
			TargetPath: targetPath,
		})
	}

	return result, nil
}
