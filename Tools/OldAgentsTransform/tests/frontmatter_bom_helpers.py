"""Shared byte-level builders for the BOM / variant-fence / non-UTF-8 tests.

Inputs are generated at test time from tool-local fixtures so that the
intended leading bytes can be asserted (committed BOM bytes are fragile).
"""
from __future__ import annotations

import pathlib

FIXTURES_DIR = pathlib.Path(__file__).parent / "fixtures"
BOM = b"\xef\xbb\xbf"

# Accepted fence variants (opening or closing), written as str.
FENCE_VARIANTS = [
    "\ufeff---",
    "--- ",
    "---\t",
    " ---",
    "  ---  ",
    "\u200b---\u2060",
    "\xa0---\xa0",
    "\u200c\u200d---",
]

# Lines that look like fences but are not.
FENCE_NEAR_MISSES = ["----", "--", "-\u200b--", "--- foo", "\xad---"]


def fixture_bytes(name: str) -> bytes:
    """Return a fixture's bytes with LF newlines and no BOM."""
    data = (FIXTURES_DIR / name).read_bytes().replace(b"\r\n", b"\n")
    assert not data.startswith(BOM)
    return data


def to_crlf(data: bytes) -> bytes:
    return data.replace(b"\n", b"\r\n")


def with_bom(data: bytes) -> bytes:
    out = BOM + data
    assert out.startswith(b"\xef\xbb\xbf")
    return out


def with_fences(data: bytes, opening: str | None = None, closing: str | None = None) -> bytes:
    """Replace the first (opening) and second (closing) bare '---' lines."""
    lines = data.decode("utf-8").split("\n")
    seen = 0
    for i, line in enumerate(lines):
        if line == "---":
            if seen == 0 and opening is not None:
                lines[i] = opening
            elif seen == 1 and closing is not None:
                lines[i] = closing
            seen += 1
            if seen == 2:
                break
    return "\n".join(lines).encode("utf-8")


def write(path: pathlib.Path, data: bytes) -> pathlib.Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    return path


# Small frontmatter-bearing inputs for error-reporting tests.
SMALL_VALID = b"---\nid: 1\nversion: 1.0.0\nname: x\n---\n\n# Title\n"
UTF16_FILE = "---\nid: 1\n---\n# T\n".encode("utf-16")
LATIN1_LINE3 = b"---\nid: 1\nname: caf\xe9\n---\n# T\n"
