"""Tests for fence-awareness utilities in fence.py.

Covers per-line masking, first-unfenced search, and safe insertion index.
All tests verify fence.py contracts — they will fail until fence.py is implemented.
"""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from fence import fence_mask, first_unfenced, safe_insertion_index  # noqa: E402


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _lines(*texts: str) -> list[str]:
    """Build a line list with newlines, for concise test data."""
    return [t if t.endswith("\n") else t + "\n" for t in texts]


# ---------------------------------------------------------------------------
# fence_mask
# ---------------------------------------------------------------------------

class TestFenceMask:
    """fence_mask returns a per-line boolean mask aligned with the input list."""

    def test_empty_input_returns_empty_list(self):
        """An empty input must produce an empty mask."""
        assert fence_mask([]) == []

    def test_no_fences_all_false(self):
        """Lines with no fence delimiters are all unmasked."""
        lines = _lines("# Hello", "", "Some prose.", "- bullet")
        mask = fence_mask(lines)
        assert mask == [False, False, False, False]

    def test_mask_length_equals_input_length(self):
        """The mask length always matches the number of input lines."""
        for n in (0, 1, 5, 20):
            lines = _lines(*["line"] * n)
            assert len(fence_mask(lines)) == n

    def test_backtick_fence_delimiter_is_true(self):
        """A line whose lstrip starts with '```' is masked True."""
        lines = _lines("```python")
        assert fence_mask(lines)[0] is True

    def test_content_inside_backtick_fence_is_true(self):
        """Lines between opening and closing '```' delimiters are masked True."""
        lines = _lines("```python", "x = 1", "y = 2", "```")
        mask = fence_mask(lines)
        # All four lines (delimiters and content) must be masked
        assert mask == [True, True, True, True]

    def test_content_outside_fence_is_false(self):
        """Lines before and after a fence block are not masked."""
        lines = _lines("prose before", "```", "code", "```", "prose after")
        mask = fence_mask(lines)
        assert mask[0] is False
        assert mask[4] is False

    def test_closing_delimiter_is_true(self):
        """The closing '```' line is itself masked True."""
        lines = _lines("```", "code", "```")
        assert fence_mask(lines)[2] is True

    def test_unterminated_fence_masks_to_end_of_input(self):
        """A fence that is never closed masks all remaining lines True."""
        lines = _lines("before", "```", "code line 1", "code line 2")
        mask = fence_mask(lines)
        assert mask == [False, True, True, True]

    def test_tilde_fence_delimiter_is_true(self):
        """A line whose lstrip starts with '~~~' is also a fence delimiter."""
        lines = _lines("~~~", "code", "~~~")
        mask = fence_mask(lines)
        assert all(mask)

    def test_indented_fence_delimiter_is_masked(self):
        """Indented fence delimiters (lstrip starts with '```') are masked."""
        lines = _lines("    ```python", "    x = 1", "    ```")
        mask = fence_mask(lines)
        assert mask == [True, True, True]

    def test_multiple_fence_blocks_isolated(self):
        """Each fence block is masked independently; lines between blocks are False."""
        lines = _lines("```", "code 1", "```", "prose", "```", "code 2", "```")
        mask = fence_mask(lines)
        assert mask == [True, True, True, False, True, True, True]


# ---------------------------------------------------------------------------
# first_unfenced
# ---------------------------------------------------------------------------

class TestFirstUnfenced:
    """first_unfenced finds the earliest line in a range that matches a predicate
    and is not masked by fence_mask."""

    def test_finds_matching_unfenced_line(self):
        """Returns the index of the first matching line outside a fence."""
        lines = _lines("prose", "target", "other")
        idx = first_unfenced(lines, 0, 3, lambda l: l.startswith("target"))
        assert idx == 1

    def test_skips_matching_line_inside_fence(self):
        """A line that matches but is inside a fence is not returned."""
        lines = _lines("```", "target", "```", "target")
        # 'target' at index 1 is fenced; 'target' at index 3 is not
        idx = first_unfenced(lines, 0, 4, lambda l: l.startswith("target"))
        assert idx == 3

    def test_returns_none_when_no_match_in_range(self):
        """Returns None when no matching unfenced line exists in [start, stop)."""
        lines = _lines("prose", "more prose")
        idx = first_unfenced(lines, 0, 2, lambda l: l.startswith("missing"))
        assert idx is None

    def test_start_is_inclusive(self):
        """Search starts at `start`, so a match at index < start is not returned."""
        lines = _lines("target", "not-target", "target")
        idx = first_unfenced(lines, 1, 3, lambda l: l.startswith("target"))
        assert idx == 2

    def test_stop_is_exclusive(self):
        """Search ends before `stop`, so a match at index >= stop is not returned."""
        lines = _lines("prose", "prose", "target")
        idx = first_unfenced(lines, 0, 2, lambda l: l.startswith("target"))
        assert idx is None

    def test_returns_none_when_all_matches_fenced(self):
        """Returns None when every matching line is inside a fence."""
        lines = _lines("```", "target", "target", "```")
        idx = first_unfenced(lines, 0, 4, lambda l: l.startswith("target"))
        assert idx is None


# ---------------------------------------------------------------------------
# safe_insertion_index
# ---------------------------------------------------------------------------

class TestSafeInsertionIndex:
    """safe_insertion_index ensures the returned index is never inside a fence."""

    def test_unfenced_desired_returned_as_is(self):
        """When the desired index is not inside a fence, it is returned unchanged."""
        lines = _lines("prose", "insert here", "more prose")
        assert safe_insertion_index(lines, 1) == 1

    def test_inside_fence_returns_index_after_fence(self):
        """When desired is inside a fence, the returned index is right after the fence."""
        # Fence spans indices 0-2 (open, content, close).
        lines = _lines("```", "content", "```", "safe here")
        # Index 1 is inside the fence; the first safe index is 3 (after closing ```)
        result = safe_insertion_index(lines, 1)
        assert result == 3

    def test_result_is_never_inside_fence(self):
        """For a range of desired indices, the result is never fence-masked."""
        lines = _lines("before", "```", "code", "```", "after")
        mask = fence_mask(lines)
        for desired in range(len(lines)):
            result = safe_insertion_index(lines, desired)
            assert not mask[result], (
                f"safe_insertion_index({desired}) returned {result}, "
                f"which is fence-masked (True)"
            )

    def test_fence_delimiter_itself_is_avoided(self):
        """Insertion must not land on the fence delimiter line."""
        # Index 0 is a fence delimiter.
        lines = _lines("```", "code", "```", "after")
        result = safe_insertion_index(lines, 0)
        # The fence spans 0-2; safe index is 3
        assert result == 3
