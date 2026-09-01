package session

// Unit tests for the expandStageGlobs pure helper function.
//
// These tests use package session (internal) to access the unexported function.
//
// expandStageGlobs coverage:
//
//   No-glob paths:
//   - Slice containing only non-glob paths passes through unchanged when stages
//     are populated.
//
//   Single-glob expansion:
//   - A single Stage-* path expands to one entry per stage entry in the set.
//   - Verified with a 2-entry StageSet (stages 1 and 2).
//
//   Multiple-glob expansion:
//   - Multiple Stage-* paths each expand per stage, in order: all expansions of
//     the first glob are emitted before all expansions of the second glob.
//
//   Nil and empty StageSet passthrough:
//   - When stages is nil, paths containing Stage-* are returned unchanged.
//   - When stages is an empty StageSet, same passthrough behaviour.
//   - Neither case panics.
//
//   Mixed glob and non-glob:
//   - A slice containing both Stage-* and non-Stage-* paths is handled: non-glob
//     entries appear once in output position, glob entries are expanded in-place.
//
//   Multiple Stage-* occurrences in a single path:
//   - A path with Stage-* appearing more than once (e.g. "Stage-*/Sub/Stage-*/Out.md")
//     gets all occurrences replaced consistently within each expansion, matching
//     the strings.ReplaceAll semantic the engine uses.
//
//   Nil and empty artifact slice:
//   - A nil input slice returns nil.
//   - An empty input slice returns an empty slice.

import (
	"reflect"
	"testing"

	"mosaic-run/internal/domain"
)

func TestExpandStageGlobs(t *testing.T) {
	twoStages := &domain.StageSet{
		Entries: []domain.StageEntry{
			{Number: 1},
			{Number: 2},
		},
	}
	threeStages := &domain.StageSet{
		Entries: []domain.StageEntry{
			{Number: 1},
			{Number: 2},
			{Number: 3},
		},
	}

	tests := []struct {
		name      string
		artifacts []string
		stages    *domain.StageSet
		want      []string
	}{
		{
			name:      "no globs passthrough with populated stages",
			artifacts: []string{"Plan.md", "review.md"},
			stages:    twoStages,
			want:      []string{"Plan.md", "review.md"},
		},
		{
			name:      "single glob expands to one path per stage",
			artifacts: []string{"Stage-*/Plan.md"},
			stages:    twoStages,
			want:      []string{"Stage-1/Plan.md", "Stage-2/Plan.md"},
		},
		{
			name:      "single glob with three-stage set produces three entries",
			artifacts: []string{"Stage-*/Plan.md"},
			stages:    threeStages,
			want:      []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-3/Plan.md"},
		},
		{
			name:      "multiple globs each expand per stage in declaration order",
			artifacts: []string{"Stage-*/Plan.md", "Stage-*/PlanProgress.md"},
			stages:    twoStages,
			want:      []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-1/PlanProgress.md", "Stage-2/PlanProgress.md"},
		},
		{
			name:      "nil stages returns paths unchanged",
			artifacts: []string{"Stage-*/Plan.md"},
			stages:    nil,
			want:      []string{"Stage-*/Plan.md"},
		},
		{
			name:      "empty StageSet returns paths unchanged",
			artifacts: []string{"Stage-*/Plan.md"},
			stages:    &domain.StageSet{},
			want:      []string{"Stage-*/Plan.md"},
		},
		{
			name:      "nil stages does not panic on non-glob path",
			artifacts: []string{"Plan.md"},
			stages:    nil,
			want:      []string{"Plan.md"},
		},
		{
			name:      "mixed glob and non-glob paths",
			artifacts: []string{"Plan.md", "Stage-*/Plan.md", "review.md"},
			stages:    twoStages,
			want:      []string{"Plan.md", "Stage-1/Plan.md", "Stage-2/Plan.md", "review.md"},
		},
		{
			name:      "path with multiple Stage-* occurrences expands all occurrences consistently",
			artifacts: []string{"Stage-*/Sub/Stage-*/Out.md"},
			stages:    twoStages,
			want:      []string{"Stage-1/Sub/Stage-1/Out.md", "Stage-2/Sub/Stage-2/Out.md"},
		},
		{
			name:      "absolute path with Stage-* segment expands correctly",
			artifacts: []string{"/tmp/run/Stage-*/Plan.md"},
			stages:    twoStages,
			want:      []string{"/tmp/run/Stage-1/Plan.md", "/tmp/run/Stage-2/Plan.md"},
		},
		{
			name:      "nil artifact slice returns nil",
			artifacts: nil,
			stages:    twoStages,
			want:      nil,
		},
		{
			name:      "empty artifact slice returns empty slice",
			artifacts: []string{},
			stages:    twoStages,
			want:      []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := expandStageGlobs(tc.artifacts, tc.stages)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("expandStageGlobs(%v, stages)\ngot:  %v\nwant: %v",
					tc.artifacts, got, tc.want)
			}
		})
	}
}
