// Tests for HarnessDisplayOrder.
package resolve_test

import (
	"testing"

	"mosaic-run/internal/testrun/resolve"
)

// TestHarnessDisplayOrder_EmptySlice_ReturnsEmpty verifies that a nil or
// empty slice returns an empty (or nil) slice with no entries.
func TestHarnessDisplayOrder_EmptySlice_ReturnsEmpty(t *testing.T) {
	result := resolve.HarnessDisplayOrder(nil)
	if len(result) != 0 {
		t.Errorf("HarnessDisplayOrder(nil) returned %v, want empty", result)
	}
}

// TestHarnessDisplayOrder_FakeOnly_ReturnsEmpty verifies that a slice
// containing only "fake" entries yields an empty result.
func TestHarnessDisplayOrder_FakeOnly_ReturnsEmpty(t *testing.T) {
	result := resolve.HarnessDisplayOrder([]string{"fake", "fake"})
	if len(result) != 0 {
		t.Errorf("HarnessDisplayOrder([fake, fake]) = %v, want empty", result)
	}
}

// TestHarnessDisplayOrder_SkipsFakeEntries verifies that "fake" entries are
// excluded from the result.
func TestHarnessDisplayOrder_SkipsFakeEntries(t *testing.T) {
	input := []string{"fake", "claude-code", "fake", "opencode"}
	result := resolve.HarnessDisplayOrder(input)
	for _, id := range result {
		if id == "fake" {
			t.Errorf("HarnessDisplayOrder result contains 'fake': %v", result)
		}
	}
}

// TestHarnessDisplayOrder_DeduplicatesEntries verifies that duplicate harness
// IDs appear at most once in the result.
func TestHarnessDisplayOrder_DeduplicatesEntries(t *testing.T) {
	input := []string{"claude-code", "opencode", "claude-code"}
	result := resolve.HarnessDisplayOrder(input)
	seen := make(map[string]int)
	for _, id := range result {
		seen[id]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("HarnessDisplayOrder duplicated %q (%d times); want each ID at most once", id, count)
		}
	}
}

// TestHarnessDisplayOrder_PreservesInputOrder verifies that the output slice
// preserves the order of first occurrence from the input slice.
func TestHarnessDisplayOrder_PreservesInputOrder(t *testing.T) {
	input := []string{"opencode", "claude-code", "ghcp-cli"}
	result := resolve.HarnessDisplayOrder(input)
	if len(result) != 3 {
		t.Fatalf("HarnessDisplayOrder(%v) returned %d entries, want 3", input, len(result))
	}
	want := []string{"opencode", "claude-code", "ghcp-cli"}
	for i, id := range result {
		if id != want[i] {
			t.Errorf("result[%d] = %q, want %q (order must be preserved)", i, id, want[i])
		}
	}
}

// TestHarnessDisplayOrder_MixedFakeAndReal_ReturnsOnlyRealInOrder verifies
// that mixed "fake" and real entries yield only the real IDs in input order.
func TestHarnessDisplayOrder_MixedFakeAndReal_ReturnsOnlyRealInOrder(t *testing.T) {
	input := []string{"claude-code", "fake", "opencode"}
	result := resolve.HarnessDisplayOrder(input)
	if len(result) != 2 {
		t.Fatalf("HarnessDisplayOrder(%v) returned %d entries, want 2", input, len(result))
	}
	if result[0] != "claude-code" || result[1] != "opencode" {
		t.Errorf("result = %v, want [claude-code opencode]", result)
	}
}
