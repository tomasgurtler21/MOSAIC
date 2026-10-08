package domain_test

// Tests for artifact path normalisation: stripping the run prefix and
// de-duplicating a path list by its normalised form.

import (
	"reflect"
	"testing"

	"mosaic-run/internal/domain"
)

const pathRunID = "20261007T174011Z-4f54"

func TestStripRunPrefix(t *testing.T) {
	cases := []struct {
		name  string
		runID string
		in    string
		want  string
	}{
		{"prefixed path loses the run folder", pathRunID, "Orchestration-" + pathRunID + "/Stage-2/Plan.md", "Stage-2/Plan.md"},
		{"unprefixed path is unchanged", pathRunID, "Stage-2/Plan.md", "Stage-2/Plan.md"},
		{"backslash prefixed path is read as slashes", pathRunID, "Orchestration-" + pathRunID + `\Stage-2\Plan.md`, "Stage-2/Plan.md"},
		{"other run prefix is kept", pathRunID, "Orchestration-other-run/Plan.md", "Orchestration-other-run/Plan.md"},
		{"empty run id only converts slashes", "", `docs\Plan.md`, "docs/Plan.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.StripRunPrefix(tc.runID, tc.in)

			if got != tc.want {
				t.Errorf("StripRunPrefix(%q, %q) = %q, want %q", tc.runID, tc.in, got, tc.want)
			}
		})
	}
}

func TestDedupArtifactPaths(t *testing.T) {
	prefix := "Orchestration-" + pathRunID + "/"
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil in nil out", nil, nil},
		{"exact duplicate removed", []string{"a.md", "b.md", "a.md"}, []string{"a.md", "b.md"}},
		{"prefixed duplicate of unprefixed entry removed, first form kept",
			[]string{"Stage-1/Plan.md", prefix + "Stage-1/Plan.md", "c.md"}, []string{"Stage-1/Plan.md", "c.md"}},
		{"unprefixed duplicate of prefixed entry removed, first form kept",
			[]string{prefix + "Stage-1/Plan.md", "Stage-1/Plan.md"}, []string{prefix + "Stage-1/Plan.md"}},
		{"distinct paths keep their order", []string{"b.md", "a.md"}, []string{"b.md", "a.md"}},
		{"wildcard is not expanded or merged with a concrete path",
			[]string{"Stage-*/Plan.md", "Stage-1/Plan.md"}, []string{"Stage-*/Plan.md", "Stage-1/Plan.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.DedupArtifactPaths(pathRunID, tc.in)

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DedupArtifactPaths = %v, want %v", got, tc.want)
			}
		})
	}
}
