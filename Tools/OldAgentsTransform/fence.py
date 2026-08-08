"""Fence-awareness utilities for MOSAIC boundary tools.

Provides per-line masking for fenced code blocks, so insertion and deletion
logic in region_insertion can skip lines that are inside or adjacent to fences.
"""
from __future__ import annotations

from typing import Callable, Sequence


def fence_mask(lines: Sequence[str]) -> list[bool]:
    """Return a per-line mask: True when the line is a fence delimiter or lies
    inside a fenced code block.

    A fence delimiter is a line whose lstrip() starts with '```' or '~~~'.
    Opening and closing delimiters are both masked True, so an insertion point
    can never be chosen adjacent-inside a fence. An unterminated fence masks
    True to end of input.

    len(fence_mask(lines)) == len(lines) for all inputs.
    """
    mask = []
    in_fence = False
    for line in lines:
        stripped = line.lstrip()
        is_delimiter = stripped.startswith("```") or stripped.startswith("~~~")
        if is_delimiter:
            if not in_fence:
                in_fence = True
                mask.append(True)
            else:
                in_fence = False
                mask.append(True)
        else:
            mask.append(in_fence)
    return mask


def first_unfenced(
    lines: Sequence[str],
    start: int,
    stop: int,
    predicate: Callable[[str], bool],
) -> int | None:
    """Index of the first line i in [start, stop) where predicate(lines[i]) is
    True and fence_mask(lines)[i] is False. None when there is no such line.
    """
    mask = fence_mask(lines)
    for i in range(start, stop):
        if not mask[i] and predicate(lines[i]):
            return i
    return None


def safe_insertion_index(lines: Sequence[str], desired: int) -> int:
    """Return `desired` when lines[desired] is not inside a fence; otherwise the
    index of the line immediately after the fence containing `desired`.
    Guarantees the returned index never places content inside a fence.
    """
    mask = fence_mask(lines)
    if not mask[desired]:
        return desired
    # Find the end of the fence containing desired
    for i in range(desired + 1, len(mask)):
        if not mask[i]:
            return i
    # Fence runs to end of input; return len(lines)
    return len(lines)
