package screens_test

// viewport_test.go verifies the pure scroll-window helper functions.
//
// ScrollWindow guarantees: cursor always visible, minimum offset movement,
// non-negative results for all edge inputs (zero items, zero visible, negative
// inputs). VisibleItems is a guarded floor-division. FormatScrollHint produces
// a human-readable 1-based range or empty when the full list is visible.

import (
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// ScrollWindow — cursor tracking
// ---------------------------------------------------------------------------

// TestScrollWindow_CursorBelowWindow_OffsetScrollsDown verifies that when the
// cursor falls below the current window, the offset increases so the cursor
// becomes the last visible item.
func TestScrollWindow_CursorBelowWindow_OffsetScrollsDown(t *testing.T) {
	// prevOffset=0, cursor=5, itemCount=10, visible=3 → cursor is below [0,3)
	// minimum move: offset = cursor - visible + 1 = 5 - 3 + 1 = 3
	w := screens.ScrollWindow(0, 5, 10, 3)

	if w.Offset > 5 {
		t.Errorf("Offset %d > cursor 5: cursor must be in the visible window", w.Offset)
	}
	if w.Offset+w.Count <= 5 {
		t.Errorf("cursor 5 is not inside window [%d, %d): cursor must be visible", w.Offset, w.Offset+w.Count)
	}
}

// TestScrollWindow_CursorBelowWindow_ScrollsByMinimum verifies the offset moves
// by exactly the minimum needed — the cursor becomes the last visible item, not
// the first.
func TestScrollWindow_CursorBelowWindow_ScrollsByMinimum(t *testing.T) {
	// prevOffset=0, cursor=5, visibleItems=3 → expected offset = 5 - 3 + 1 = 3
	w := screens.ScrollWindow(0, 5, 10, 3)

	if w.Offset != 3 {
		t.Errorf("ScrollWindow(0, 5, 10, 3).Offset = %d, want 3 (minimum scroll)", w.Offset)
	}
}

// TestScrollWindow_CursorAboveWindow_OffsetScrollsUp verifies that when the
// cursor falls above the current window, the offset decreases so the cursor
// becomes the first visible item.
func TestScrollWindow_CursorAboveWindow_OffsetScrollsUp(t *testing.T) {
	// prevOffset=5, cursor=2, visible=3 → cursor is above [5,8)
	// expected offset = cursor = 2
	w := screens.ScrollWindow(5, 2, 10, 3)

	if w.Offset != 2 {
		t.Errorf("ScrollWindow(5, 2, 10, 3).Offset = %d, want 2 (scroll up to cursor)", w.Offset)
	}
	if w.Offset+w.Count <= 2 {
		t.Errorf("cursor 2 is not inside window [%d, %d): cursor must be visible", w.Offset, w.Offset+w.Count)
	}
}

// TestScrollWindow_CursorInWindow_OffsetUnchanged verifies that when the
// cursor is already in the window, the offset does not change.
func TestScrollWindow_CursorInWindow_OffsetUnchanged(t *testing.T) {
	// prevOffset=2, cursor=3, visible=3 → window [2,5), cursor 3 is already inside
	w := screens.ScrollWindow(2, 3, 10, 3)

	if w.Offset != 2 {
		t.Errorf("ScrollWindow(2, 3, 10, 3).Offset = %d, want 2 (cursor already in window)", w.Offset)
	}
}

// ---------------------------------------------------------------------------
// ScrollWindow — clamping at end and on resize
// ---------------------------------------------------------------------------

// TestScrollWindow_OffsetClampedWhenShrinks verifies that when the viewport
// shrinks (visibleItems decreases), the offset adjusts so the cursor stays
// visible, rather than falling below the window.
func TestScrollWindow_OffsetClampedWhenShrinks(t *testing.T) {
	// prevOffset=8, cursor=7, itemCount=10, visible=2
	// cursor was in window with previous larger visible, now visible shrank.
	// cursor 7 is above offset 8 → offset must move down to 7.
	w := screens.ScrollWindow(8, 7, 10, 2)

	if w.Offset != 7 {
		t.Errorf("ScrollWindow(8, 7, 10, 2).Offset = %d, want 7 (clamp to include cursor)", w.Offset)
	}
}

// TestScrollWindow_OffsetClampedAtMaxValidOffset verifies that the offset never
// exceeds itemCount-Count, so there are no trailing blank rows.
func TestScrollWindow_OffsetClampedAtMaxValidOffset(t *testing.T) {
	// prevOffset=10 (stale large offset from earlier), cursor=4, itemCount=5, visible=3
	// max valid offset = 5 - 3 = 2; cursor 4 is last item
	// expected: offset = 5 - 3 = 2 (cursor 4 is inside [2,5))
	w := screens.ScrollWindow(10, 4, 5, 3)

	maxOffset := 5 - 3
	if w.Offset > maxOffset {
		t.Errorf("Offset %d > maxOffset %d: trailing blank rows would appear", w.Offset, maxOffset)
	}
	if w.Offset < 0 {
		t.Errorf("Offset %d < 0: must not be negative", w.Offset)
	}
	// cursor must still be visible
	if w.Offset > 4 || w.Offset+w.Count <= 4 {
		t.Errorf("cursor 4 not in window [%d, %d)", w.Offset, w.Offset+w.Count)
	}
}

// TestScrollWindow_AllItemsFitInViewport_OffsetIsZero verifies that when the
// total item count is smaller than or equal to the viewport, the offset stays
// at zero and all items are visible.
func TestScrollWindow_AllItemsFitInViewport_OffsetIsZero(t *testing.T) {
	// 3 items, 10 visible slots → no scrolling needed
	w := screens.ScrollWindow(0, 2, 3, 10)

	if w.Offset != 0 {
		t.Errorf("ScrollWindow(0, 2, 3, 10).Offset = %d, want 0 (all items fit)", w.Offset)
	}
	if w.Count != 3 {
		t.Errorf("ScrollWindow(0, 2, 3, 10).Count = %d, want 3 (all items visible)", w.Count)
	}
}

// ---------------------------------------------------------------------------
// ScrollWindow — Count invariant
// ---------------------------------------------------------------------------

// TestScrollWindow_Count_IsMinOfVisibleAndItemCount verifies the Count field
// equals min(visibleItems, itemCount) in both directions.
func TestScrollWindow_Count_IsMinOfVisibleAndItemCount(t *testing.T) {
	// 3 items, 5 visible → count = 3 (itemCount is the limit)
	small := screens.ScrollWindow(0, 0, 3, 5)
	if small.Count != 3 {
		t.Errorf("ScrollWindow(0, 0, 3, 5).Count = %d, want 3", small.Count)
	}

	// 10 items, 3 visible → count = 3 (visibleItems is the limit)
	large := screens.ScrollWindow(0, 0, 10, 3)
	if large.Count != 3 {
		t.Errorf("ScrollWindow(0, 0, 10, 3).Count = %d, want 3", large.Count)
	}
}

// ---------------------------------------------------------------------------
// ScrollWindow — edge inputs that must not panic or go negative
// ---------------------------------------------------------------------------

// TestScrollWindow_ZeroVisibleItems_DoesNotPanic verifies that a zero viewport
// produces a zero Window without panicking.
func TestScrollWindow_ZeroVisibleItems_DoesNotPanic(t *testing.T) {
	w := screens.ScrollWindow(0, 0, 10, 0)

	if w.Count != 0 {
		t.Errorf("Count = %d, want 0 when visibleItems=0", w.Count)
	}
	if w.Offset < 0 {
		t.Errorf("Offset = %d, must not be negative", w.Offset)
	}
}

// TestScrollWindow_NegativeVisibleItems_DoesNotPanic verifies that a negative
// viewport size is treated as zero and does not panic.
func TestScrollWindow_NegativeVisibleItems_DoesNotPanic(t *testing.T) {
	w := screens.ScrollWindow(0, 0, 10, -5)

	if w.Count != 0 {
		t.Errorf("Count = %d, want 0 when visibleItems<0", w.Count)
	}
	if w.Offset < 0 {
		t.Errorf("Offset = %d, must not be negative", w.Offset)
	}
}

// TestScrollWindow_EmptyItemCount_ZeroWindow verifies that an empty list
// produces a zero Window.
func TestScrollWindow_EmptyItemCount_ZeroWindow(t *testing.T) {
	w := screens.ScrollWindow(0, 0, 0, 5)

	if w.Offset != 0 {
		t.Errorf("Offset = %d, want 0 for empty list", w.Offset)
	}
	if w.Count != 0 {
		t.Errorf("Count = %d, want 0 for empty list", w.Count)
	}
}

// TestScrollWindow_NegativePrevOffset_DoesNotProduceNegativeOffset verifies
// that a stale or invalid negative prevOffset is handled safely.
func TestScrollWindow_NegativePrevOffset_DoesNotProduceNegativeOffset(t *testing.T) {
	w := screens.ScrollWindow(-3, 0, 10, 3)

	if w.Offset < 0 {
		t.Errorf("Offset = %d, must not be negative when prevOffset was negative", w.Offset)
	}
}

// TestScrollWindow_NoCursorMode_OffsetOnlyClamped verifies the documented
// no-cursor mode: when cursor is -1 (or any out-of-range value), the function
// only clamps the offset to keep it within [0, itemCount-Count] without trying
// to satisfy a cursor-visibility guarantee.
//
// The docstring states: "use -1 or any out-of-range value when the list has no
// cursor; the offset is then only clamped."
//
// With prevOffset=3, cursor=-1, itemCount=5, visibleItems=3:
//   maxOffset = 5 - 3 = 2, so Offset should be clamped to 2.
//   No cursor-visibility obligation exists.
func TestScrollWindow_NoCursorMode_OffsetOnlyClamped(t *testing.T) {
	// cursor=-1 signals no-cursor mode.
	w := screens.ScrollWindow(3, -1, 5, 3)

	if w.Offset < 0 {
		t.Errorf("Offset = %d, must not be negative in no-cursor mode", w.Offset)
	}
	maxOffset := 5 - 3
	if w.Offset > maxOffset {
		t.Errorf("Offset = %d, exceeds maxOffset %d: offset should be clamped in no-cursor mode",
			w.Offset, maxOffset)
	}
	if w.Count != 3 {
		t.Errorf("Count = %d, want 3 in no-cursor mode", w.Count)
	}
}

// ---------------------------------------------------------------------------
// Window — helper methods
// ---------------------------------------------------------------------------

// TestWindow_End_ReturnsOffsetPlusCount verifies End() is safe to use as a
// slice upper-bound.
func TestWindow_End_ReturnsOffsetPlusCount(t *testing.T) {
	w := screens.Window{Offset: 3, Count: 5}
	if w.End() != 8 {
		t.Errorf("Window{3,5}.End() = %d, want 8", w.End())
	}
}

// TestWindow_End_ZeroWindow verifies End() is 0 for a zero Window.
func TestWindow_End_ZeroWindow(t *testing.T) {
	var w screens.Window
	if w.End() != 0 {
		t.Errorf("zero Window.End() = %d, want 0", w.End())
	}
}

// TestWindow_HasAbove_TrueWhenOffsetPositive verifies HasAbove is true when
// there are items scrolled out above the window.
func TestWindow_HasAbove_TrueWhenOffsetPositive(t *testing.T) {
	w := screens.Window{Offset: 1, Count: 3}
	if !w.HasAbove() {
		t.Error("Window{Offset:1}.HasAbove() = false, want true")
	}
}

// TestWindow_HasAbove_FalseAtTopOfList verifies HasAbove is false when the
// window starts at the very first item.
func TestWindow_HasAbove_FalseAtTopOfList(t *testing.T) {
	w := screens.Window{Offset: 0, Count: 3}
	if w.HasAbove() {
		t.Error("Window{Offset:0}.HasAbove() = true, want false")
	}
}

// TestWindow_HasBelow_TrueWhenMoreItemsExist verifies HasBelow is true when
// items extend beyond the current window.
func TestWindow_HasBelow_TrueWhenMoreItemsExist(t *testing.T) {
	w := screens.Window{Offset: 0, Count: 3}
	if !w.HasBelow(10) {
		t.Error("Window{0,3}.HasBelow(10) = false, want true (items exist beyond window)")
	}
}

// TestWindow_HasBelow_FalseAtEndOfList verifies HasBelow is false when the
// window covers all remaining items.
func TestWindow_HasBelow_FalseAtEndOfList(t *testing.T) {
	w := screens.Window{Offset: 7, Count: 3}
	if w.HasBelow(10) {
		t.Error("Window{7,3}.HasBelow(10) = true, want false (window covers all items)")
	}
}

// ---------------------------------------------------------------------------
// VisibleItems
// ---------------------------------------------------------------------------

// TestVisibleItems_NormalFloorDivision verifies the floor-division formula.
func TestVisibleItems_NormalFloorDivision(t *testing.T) {
	cases := []struct {
		avail, perItem, want int
	}{
		{10, 2, 5},  // exact division
		{11, 2, 5},  // floor(11/2) = 5
		{12, 3, 4},  // exact division
		{13, 3, 4},  // floor(13/3) = 4
		{2, 2, 1},   // exactly one item
	}
	for _, tc := range cases {
		got := screens.VisibleItems(tc.avail, tc.perItem)
		if got != tc.want {
			t.Errorf("VisibleItems(%d, %d) = %d, want %d", tc.avail, tc.perItem, got, tc.want)
		}
	}
}

// TestVisibleItems_ZeroAvailableLines_ReturnsZero verifies the guard on
// availableLines.
func TestVisibleItems_ZeroAvailableLines_ReturnsZero(t *testing.T) {
	got := screens.VisibleItems(0, 2)
	if got != 0 {
		t.Errorf("VisibleItems(0, 2) = %d, want 0", got)
	}
}

// TestVisibleItems_NegativeAvailableLines_ReturnsZero verifies that a negative
// line budget returns zero rather than a negative item count.
func TestVisibleItems_NegativeAvailableLines_ReturnsZero(t *testing.T) {
	got := screens.VisibleItems(-5, 2)
	if got != 0 {
		t.Errorf("VisibleItems(-5, 2) = %d, want 0", got)
	}
}

// TestVisibleItems_ZeroLinesPerItem_ReturnsZero verifies the guard against
// division by zero.
func TestVisibleItems_ZeroLinesPerItem_ReturnsZero(t *testing.T) {
	got := screens.VisibleItems(10, 0)
	if got != 0 {
		t.Errorf("VisibleItems(10, 0) = %d, want 0", got)
	}
}

// TestVisibleItems_NegativeLinesPerItem_ReturnsZero verifies that a negative
// linesPerItem is treated as invalid and returns zero.
func TestVisibleItems_NegativeLinesPerItem_ReturnsZero(t *testing.T) {
	got := screens.VisibleItems(10, -2)
	if got != 0 {
		t.Errorf("VisibleItems(10, -2) = %d, want 0", got)
	}
}

// TestVisibleItems_LessThanOneItem_ReturnsZero verifies that fewer available
// lines than one full item returns zero, not a fractional value.
func TestVisibleItems_LessThanOneItem_ReturnsZero(t *testing.T) {
	got := screens.VisibleItems(1, 2)
	if got != 0 {
		t.Errorf("VisibleItems(1, 2) = %d, want 0 (1 line < 2 lines per item)", got)
	}
}

// ---------------------------------------------------------------------------
// FormatScrollHint
// ---------------------------------------------------------------------------

// TestFormatScrollHint_WholeListVisible_ReturnsEmpty verifies no hint is shown
// when the window covers the complete list.
func TestFormatScrollHint_WholeListVisible_ReturnsEmpty(t *testing.T) {
	w := screens.Window{Offset: 0, Count: 5}
	hint := screens.FormatScrollHint(w, 5)
	if hint != "" {
		t.Errorf("FormatScrollHint (full coverage) = %q, want empty string", hint)
	}
}

// TestFormatScrollHint_EmptyList_ReturnsEmpty verifies no hint is shown for an
// empty list.
func TestFormatScrollHint_EmptyList_ReturnsEmpty(t *testing.T) {
	w := screens.Window{Offset: 0, Count: 0}
	hint := screens.FormatScrollHint(w, 0)
	if hint != "" {
		t.Errorf("FormatScrollHint (empty list) = %q, want empty string", hint)
	}
}

// TestFormatScrollHint_PartialList_ContainsRange verifies the hint contains the
// 1-based start, end, and total item count as a visible range with a separator
// between start and end (e.g. "3-8 of 24").
//
// Checking only for the bare substrings "3", "8", "24" is insufficient: a string
// like "324 items visible, total 8" would satisfy all three Contains checks while
// clearly not being the intended range format. The separator assertion rules out
// any such false positive.
func TestFormatScrollHint_PartialList_ContainsRange(t *testing.T) {
	// Offset=2, Count=6 → items 3–8 (1-based) of 24
	w := screens.Window{Offset: 2, Count: 6}
	hint := screens.FormatScrollHint(w, 24)

	if hint == "" {
		t.Fatal("FormatScrollHint (partial coverage) = empty, want non-empty range hint")
	}
	// The start and end must appear as a contiguous range (e.g. "3-8"), not as
	// isolated substrings. This catches malformed-but-substring-matching output.
	if !strings.Contains(hint, "3-8") {
		t.Errorf("hint %q does not contain range \"3-8\" (expected format like \"3-8 of 24\")", hint)
	}
	if !strings.Contains(hint, "24") {
		t.Errorf("hint %q does not contain total '24'", hint)
	}
}

// TestFormatScrollHint_FirstPage_ContainsRange verifies the hint appears even
// on the first page when more items exist below, and uses the range-separator
// format (e.g. "1-5 of 24") rather than isolated numerals.
func TestFormatScrollHint_FirstPage_ContainsRange(t *testing.T) {
	// Offset=0, Count=5 → items 1–5 of 24
	w := screens.Window{Offset: 0, Count: 5}
	hint := screens.FormatScrollHint(w, 24)

	if hint == "" {
		t.Fatal("FormatScrollHint (first page, more below) = empty, want range hint")
	}
	// Verify the range shape — start and end joined by a separator — not bare digits.
	if !strings.Contains(hint, "1-5") {
		t.Errorf("hint %q does not contain range \"1-5\" (expected format like \"1-5 of 24\")", hint)
	}
	if !strings.Contains(hint, "24") {
		t.Errorf("hint %q does not contain total '24'", hint)
	}
}

// TestFormatScrollHint_LastPage_ContainsRange verifies the hint appears on the
// last page when items existed above, and uses the range-separator format
// (e.g. "20-24 of 24") rather than isolated numerals.
func TestFormatScrollHint_LastPage_ContainsRange(t *testing.T) {
	// Offset=19, Count=5 → items 20–24 of 24
	w := screens.Window{Offset: 19, Count: 5}
	hint := screens.FormatScrollHint(w, 24)

	if hint == "" {
		t.Fatal("FormatScrollHint (last page, items above) = empty, want range hint")
	}
	// Verify the range shape — start and end joined by a separator.
	if !strings.Contains(hint, "20-24") {
		t.Errorf("hint %q does not contain range \"20-24\" (expected format like \"20-24 of 24\")", hint)
	}
	if !strings.Contains(hint, "24") {
		t.Errorf("hint %q does not contain total '24'", hint)
	}
}

// TestFormatScrollHint_WindowLargerThanList_ReturnsEmpty verifies that a Count
// larger than itemCount (shouldn't normally occur) is treated as full coverage
// and returns an empty hint.
func TestFormatScrollHint_WindowLargerThanList_ReturnsEmpty(t *testing.T) {
	// Count >= itemCount → full coverage, no hint
	w := screens.Window{Offset: 0, Count: 10}
	hint := screens.FormatScrollHint(w, 5)
	if hint != "" {
		t.Errorf("FormatScrollHint (count > itemCount) = %q, want empty", hint)
	}
}
