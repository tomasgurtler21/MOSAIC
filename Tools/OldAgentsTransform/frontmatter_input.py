r"""Tolerant input reading and frontmatter-fence recognition for MOSAIC boundary tools.

Shared by boundary_transformer, frontmatter_build, boundary_validator and
deployed_blocks. Imports only the standard library (no first-party imports),
so it can be imported by any of those modules without creating a cycle.
"""
from __future__ import annotations

import pathlib

_BOM = b"\xef\xbb\xbf"
_MAX_RENDERED_CHARS = 80
_INVISIBLE_FENCE_CHARS = "﻿​‌‍⁠"


class FrontmatterDecodeError(ValueError):
    """Input bytes are not valid UTF-8.

    Attributes:
        line_number: 1-based line containing the first undecodable byte
            (1 + count of LF bytes before that byte).
        reason: Human-readable, path-free explanation containing "UTF-8".
    """

    line_number: int
    reason: str

    def __init__(self, line_number: int, reason: str) -> None:
        super().__init__(reason)
        self.line_number = line_number
        self.reason = reason


def read_frontmatter_text(path: pathlib.Path) -> str:
    r"""Read an agent/bundle file as UTF-8 text, tolerating one leading BOM.

    Equivalent to path.read_text(encoding="utf-8") for every valid UTF-8 file
    (same universal-newline translation: "\r\n" and "\r" become "\n"), except
    that a single leading UTF-8 BOM (EF BB BF) is removed.

    Raises:
        OSError: unchanged from path.read_bytes() (missing file, permission
            denied, directory, ...). Not wrapped.
        FrontmatterDecodeError: the content is not valid UTF-8.
    """
    return decode_frontmatter_bytes(path.read_bytes())


def decode_frontmatter_bytes(data: bytes) -> str:
    r"""Decode file bytes per read_frontmatter_text's contract (file-free core).

    Raises:
        FrontmatterDecodeError: data (after removing one leading EF BB BF) is
            not valid UTF-8.
    """
    if data.startswith(_BOM):
        data = data[len(_BOM):]
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as exc:
        line_number = 1 + data.count(b"\n", 0, exc.start)
        raise FrontmatterDecodeError(
            line_number,
            f"File is not valid UTF-8 (undecodable byte 0x{data[exc.start]:02X} "
            f"at byte offset {exc.start}); re-save the file as UTF-8",
        ) from exc
    return text.replace("\r\n", "\n").replace("\r", "\n")


def is_frontmatter_fence(line: str) -> bool:
    """Return True when line is a YAML frontmatter fence ('---'), tolerating
    invisible/non-semantic variants per the accepted set."""
    return _strip_padding(_strip_terminator(line)) == "---"


def render_line_for_error(line: str) -> str:
    """Render one source line for inclusion in an error message, with
    invisible/non-printable characters made visible."""
    content = _strip_terminator(line)
    if len(content) > _MAX_RENDERED_CHARS:
        return repr(content[:_MAX_RENDERED_CHARS]) + "..."
    return repr(content)


def _strip_terminator(line: str) -> str:
    if line.endswith("\r\n"):
        return line[:-2]
    if line.endswith(("\n", "\r")):
        return line[:-1]
    return line


def _is_padding(char: str) -> bool:
    return char.isspace() or char in _INVISIBLE_FENCE_CHARS


def _strip_padding(content: str) -> str:
    """Strip whitespace and the accepted invisible characters from both ends."""
    start, end = 0, len(content)
    while start < end and _is_padding(content[start]):
        start += 1
    while end > start and _is_padding(content[end - 1]):
        end -= 1
    return content[start:end]
