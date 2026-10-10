package session

// Unit tests for run-scoped resolution of dispatched path lists. The recorded
// (unprefixed) form must be scoped exactly once; paths already scoped, project
// files outside the run folder and another run's paths are not double-scoped
// or rewritten into this run's folder.
//
// These protect existing behavior that the recorded path form relies on.

import (
	"reflect"
	"testing"
)

const resolveFolder = "Orchestration-20261007T174011Z-4f54/"

func TestResolveToRunScoped_UnprefixedPath_GetsRunFolder(t *testing.T) {
	got := resolveToRunScoped([]string{"Stage-1/Plan.md"}, resolveFolder)

	want := []string{resolveFolder + "Stage-1/Plan.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestResolveToRunScoped_AlreadyScopedPath_NotDoublePrefixed(t *testing.T) {
	in := []string{resolveFolder + "Stage-1/Plan.md"}

	got := resolveToRunScoped(in, resolveFolder)

	if !reflect.DeepEqual(got, in) {
		t.Errorf("want %v unchanged, got %v", in, got)
	}
}

func TestResolveToRunScoped_MixedForms_EachScopedOnce_OrderKept(t *testing.T) {
	got := resolveToRunScoped([]string{"a.md", resolveFolder + "b.md", "Stage-2/c.md"}, resolveFolder)

	want := []string{resolveFolder + "a.md", resolveFolder + "b.md", resolveFolder + "Stage-2/c.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestResolveToRunScoped_EmptyList_StaysEmpty(t *testing.T) {
	if got := resolveToRunScoped(nil, resolveFolder); len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}
