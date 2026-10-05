"""Unit tests for the shared frontmatter input helper (BOM-tolerant read,
fence predicate, visible-line rendering, decode-error line numbers)."""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))
sys.path.insert(0, str(pathlib.Path(__file__).parent))

from frontmatter_input import (  # noqa: E402
    FrontmatterDecodeError,
    decode_frontmatter_bytes,
    is_frontmatter_fence,
    read_frontmatter_text,
    render_line_for_error,
)
from frontmatter_bom_helpers import BOM, UTF16_FILE, LATIN1_LINE3  # noqa: E402

_TERMINATORS = ["", "\n", "\r\n", "\r"]
_INVISIBLE_AND_SPACE = ["\ufeff", "\u200b", "\u200c", "\u200d", "\u2060", " ", "\t", "\xa0"]


class TestReadFrontmatterText:
    def test_bom_is_removed_lf(self, tmp_path):
        p = tmp_path / "a.md"
        p.write_bytes(BOM + b"---\nid: 1\n---\n")
        assert read_frontmatter_text(p) == "---\nid: 1\n---\n"

    def test_bom_is_removed_crlf_with_newline_translation(self, tmp_path):
        p = tmp_path / "a.md"
        p.write_bytes(BOM + b"---\r\nid: 1\r\n---\r\n")
        assert read_frontmatter_text(p) == "---\nid: 1\n---\n"

    def test_crlf_without_bom_matches_read_text(self, tmp_path):
        p = tmp_path / "a.md"
        p.write_bytes(b"---\r\nid: 1\r\n---\r\nbody\r\n")
        assert read_frontmatter_text(p) == p.read_text(encoding="utf-8")

    def test_bom_crlf_matches_read_text_minus_bom(self, tmp_path):
        p = tmp_path / "a.md"
        p.write_bytes(BOM + b"---\r\nid: 1\r\n---\r\n")
        assert read_frontmatter_text(p) == p.read_text(encoding="utf-8")[1:]

    def test_text_without_bom_is_unchanged(self, tmp_path):
        p = tmp_path / "a.md"
        p.write_bytes("---\nid: 1\n---\nbody é\n".encode("utf-8"))
        assert read_frontmatter_text(p) == "---\nid: 1\n---\nbody é\n"

    def test_only_one_bom_is_removed(self, tmp_path):
        p = tmp_path / "a.md"
        p.write_bytes(BOM + BOM + b"---\n")
        assert read_frontmatter_text(p) == "\ufeff---\n"

    def test_empty_file_returns_empty_string(self, tmp_path):
        p = tmp_path / "a.md"
        p.write_bytes(b"")
        assert read_frontmatter_text(p) == ""

    def test_missing_file_raises_oserror(self, tmp_path):
        with pytest.raises(OSError):
            read_frontmatter_text(tmp_path / "missing.md")

    def test_invalid_utf8_raises_decode_error_not_unicode_error(self, tmp_path):
        p = tmp_path / "a.md"
        p.write_bytes(LATIN1_LINE3)
        with pytest.raises(FrontmatterDecodeError) as info:
            read_frontmatter_text(p)
        assert not isinstance(info.value, UnicodeDecodeError)
        assert info.value.line_number == 3


class TestDecodeFrontmatterBytes:
    def test_bom_removed(self):
        assert decode_frontmatter_bytes(BOM + b"---\n") == "---\n"

    def test_empty_bytes(self):
        assert decode_frontmatter_bytes(b"") == ""

    def test_utf16_reports_line_1(self):
        with pytest.raises(FrontmatterDecodeError) as info:
            decode_frontmatter_bytes(UTF16_FILE)
        assert info.value.line_number == 1
        assert "UTF-8" in str(info.value)
        assert "UTF-8" in info.value.reason

    def test_invalid_byte_on_line_3_reports_line_3(self):
        with pytest.raises(FrontmatterDecodeError) as info:
            decode_frontmatter_bytes(LATIN1_LINE3)
        assert info.value.line_number == 3
        assert "UTF-8" in str(info.value)

    def test_invalid_byte_after_bom_counts_lines_normally(self):
        with pytest.raises(FrontmatterDecodeError) as info:
            decode_frontmatter_bytes(BOM + LATIN1_LINE3)
        assert info.value.line_number == 3

    def test_crlf_lines_are_counted_by_lf(self):
        with pytest.raises(FrontmatterDecodeError) as info:
            decode_frontmatter_bytes(b"---\r\nid: 1\r\nname: caf\xe9\r\n---\r\n")
        assert info.value.line_number == 3

    def test_decode_error_is_value_error_and_str_is_reason(self):
        with pytest.raises(ValueError) as info:
            decode_frontmatter_bytes(b"\xe9")
        assert str(info.value) == info.value.reason


class TestIsFrontmatterFence:
    @pytest.mark.parametrize("terminator", _TERMINATORS)
    @pytest.mark.parametrize("char", _INVISIBLE_AND_SPACE)
    def test_accepts_char_before_and_after_dashes(self, char, terminator):
        assert is_frontmatter_fence(char + "---" + char + terminator)

    @pytest.mark.parametrize("terminator", _TERMINATORS)
    @pytest.mark.parametrize("char", _INVISIBLE_AND_SPACE)
    def test_accepts_char_before_dashes_only(self, char, terminator):
        assert is_frontmatter_fence(char + "---" + terminator)

    @pytest.mark.parametrize("terminator", _TERMINATORS)
    @pytest.mark.parametrize("char", _INVISIBLE_AND_SPACE)
    def test_accepts_char_after_dashes_only(self, char, terminator):
        assert is_frontmatter_fence("---" + char + terminator)

    @pytest.mark.parametrize("line", ["\u200c\u200d---", "\u200b---\u2060\r\n", "  ---  "])
    def test_accepts_mixed_examples(self, line):
        assert is_frontmatter_fence(line)

    @pytest.mark.parametrize("terminator", _TERMINATORS)
    @pytest.mark.parametrize(
        "line",
        ["----", "--", "-\u200b--", "- --", "--- foo", "\xad---", "", "\u200b", "# Title", "---x", "x---"],
    )
    def test_rejects_near_misses(self, line, terminator):
        assert not is_frontmatter_fence(line + terminator)


class TestRenderLineForError:
    @pytest.mark.parametrize(
        ("line", "expected"),
        [
            ("", "''"),
            ("\n", "''"),
            ("\xad---\r\n", r"'\xad---'"),
            ("\ufeff---", r"'\ufeff---'"),
            ("\u200b", r"'\u200b'"),
            ("\u200b---\u2060", r"'\u200b---\u2060'"),
            ("# Title", "'# Title'"),
            ("a\tb", r"'a\tb'"),
        ],
    )
    def test_design_examples(self, line, expected):
        assert render_line_for_error(line) == expected

    def test_exactly_80_chars_not_truncated(self):
        line = "a" * 80
        assert render_line_for_error(line) == repr(line)

    def test_81_chars_truncated_to_80_with_ellipsis(self):
        assert render_line_for_error("a" * 81) == repr("a" * 80) + "..."

    def test_terminator_not_counted_toward_limit(self):
        assert render_line_for_error("a" * 80 + "\n") == repr("a" * 80)


class TestSourceUsesEscapes:
    def test_invisible_constants_have_expected_code_points(self):
        codes = [ord(c) for c in _INVISIBLE_AND_SPACE[:5]]
        assert codes == [0xFEFF, 0x200B, 0x200C, 0x200D, 0x2060]
