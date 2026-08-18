package screens

import "fmt"

// Window describes the visible slice of a scrollable list, in item units.
type Window struct {
	// Offset is the index of the first visible item. Always >= 0.
	Offset int
	// Count is how many items are visible. Always >= 0 and <= itemCount.
	Count int
}

// End returns the exclusive end index of the visible slice (Offset + Count),
// safe to use directly as a slice bound.
func (w Window) End() int { return w.Offset + w.Count }

// HasAbove reports whether items exist before the window.
func (w Window) HasAbove() bool { return w.Offset > 0 }

// HasBelow reports whether items exist after the window, given the total count.
func (w Window) HasBelow(itemCount int) bool { return w.Offset+w.Count < itemCount }

// ScrollWindow computes the visible slice of a list of items.
//
//   prevOffset   the offset carried from the previous render/keypress
//   cursor       the currently selected item index (use -1 or any out-of-range
//                value when the list has no cursor; the offset is then only clamped)
//   itemCount    total number of items in the list
//   visibleItems how many items fit in the space available
//
// The returned Window always satisfies:
//   - 0 <= Offset <= max(0, itemCount-Count)
//   - Count == min(max(visibleItems,0), max(itemCount,0))
//   - when 0 <= cursor < itemCount and Count > 0:
//     Offset <= cursor < Offset+Count   (the cursor is always visible)
//   - the offset moves by the minimum amount needed to satisfy the above
//
// Never panics and never returns negative values, including for
// visibleItems <= 0, itemCount == 0, or a negative prevOffset.
func ScrollWindow(prevOffset, cursor, itemCount, visibleItems int) Window {
	if visibleItems < 0 {
		visibleItems = 0
	}
	if itemCount < 0 {
		itemCount = 0
	}

	count := visibleItems
	if count > itemCount {
		count = itemCount
	}

	maxOffset := itemCount - count
	if maxOffset < 0 {
		maxOffset = 0
	}

	offset := prevOffset
	if offset < 0 {
		offset = 0
	}

	// Cursor visibility: adjust offset by the minimum amount so cursor is in [offset, offset+count).
	if cursor >= 0 && cursor < itemCount && count > 0 {
		if cursor < offset {
			offset = cursor
		} else if cursor >= offset+count {
			offset = cursor - count + 1
		}
	}

	// Clamp offset into valid range.
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	return Window{Offset: offset, Count: count}
}

// VisibleItems converts a line budget into a whole number of items.
// Returns 0 when availableLines <= 0 or linesPerItem <= 0.
//
//	VisibleItems(available, perItem) == max(0, available/perItem)
func VisibleItems(availableLines, linesPerItem int) int {
	if availableLines <= 0 || linesPerItem <= 0 {
		return 0
	}
	return availableLines / linesPerItem
}

// FormatScrollHint renders the position indicator appended to a screen's help
// bar so the user can tell that content continues beyond the visible window.
// Returns "" when the whole list is visible (w.Count >= itemCount) or when
// itemCount == 0 or w.Count == 0, so a screen with nothing to scroll shows no hint.
// Otherwise returns a 1-based inclusive range, e.g. "3-8 of 24".
func FormatScrollHint(w Window, itemCount int) string {
	if itemCount == 0 || w.Count == 0 || w.Count >= itemCount {
		return ""
	}
	first := w.Offset + 1
	last := w.Offset + w.Count
	return fmt.Sprintf("%d-%d of %d", first, last, itemCount)
}
