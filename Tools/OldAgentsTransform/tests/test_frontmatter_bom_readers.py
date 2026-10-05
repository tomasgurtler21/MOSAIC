"""Validator and deployed-blocks reader behavior for BOM-prefixed, variant-fence,
malformed-fence and non-UTF-8 input."""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))
sys.path.insert(0, str(pathlib.Path(__file__).parent))

from boundary_validator import validate_file  # noqa: E402
from deployed_blocks import BundleError, load_bundle  # noqa: E402
from frontmatter_bom_helpers import (  # noqa: E402
    FENCE_NEAR_MISSES, FENCE_VARIANTS, LATIN1_LINE3, UTF16_FILE,
    fixture_bytes, to_crlf, with_bom, with_fences, write,
)

_AGENT = "generic_standard_input.md"

_BUNDLE = (
    b'---\nid: deployed-sections\ntype: bundle\nbundle_version: "2.0.0"\n'
    b'name: "Test Bundle"\nblocks:\n'
    b'  - name: "AuthorityHierarchy:Subagent"\n    applies_to: subagent\n'
    b'    target: AuthorityHierarchy\n    specified_in: x.md\n'
    b'---\n\n# Bundle\n\n'
    b'<AuthorityHierarchy type="core" name="Subagent">\nbody text\n</AuthorityHierarchy>\n'
)


def _findings(path):
    return [(e.error_code, e.line_number, e.message) for e in validate_file(path)]


def _bundle_view(path):
    b = load_bundle(path)
    return b.bundle_version, b.blocks


class TestValidatorAcceptsVariants:
    def test_canonical_file_has_no_frontmatter_error(self, tmp_path):
        codes = [c for c, _, _ in _findings(write(tmp_path / "a.md", fixture_bytes(_AGENT)))]
        assert "E000" not in codes

    @pytest.mark.parametrize("crlf", [False, True], ids=["lf", "crlf"])
    def test_bom_file_matches_canonical(self, tmp_path, crlf):
        data = fixture_bytes(_AGENT)
        expected = _findings(write(tmp_path / "canon.md", data))
        variant = with_bom(to_crlf(data) if crlf else data)

        assert _findings(write(tmp_path / "bom.md", variant)) == expected

    @pytest.mark.parametrize("variant", FENCE_VARIANTS)
    @pytest.mark.parametrize("position", ["opening", "closing"])
    def test_variant_fence_matches_canonical(self, tmp_path, variant, position):
        data = fixture_bytes(_AGENT)
        expected = _findings(write(tmp_path / "canon.md", data))
        edited = with_fences(data, **{position: variant})

        assert _findings(write(tmp_path / "var.md", edited)) == expected

    def test_bom_with_variant_closing_fence_matches_canonical(self, tmp_path):
        data = fixture_bytes(_AGENT)
        expected = _findings(write(tmp_path / "canon.md", data))
        edited = with_bom(with_fences(data, closing="--- "))

        assert _findings(write(tmp_path / "both.md", edited)) == expected


class TestValidatorFenceErrors:
    @pytest.mark.parametrize(
        ("first_line", "token"),
        [
            (b"# Title", "# Title"),
            ("\xad---".encode("utf-8"), "\\xad"),
            (b"", "''"),
            (b"  ", "'  '"),
            ("\u200b".encode("utf-8"), "\\u200b"),
        ],
        ids=["visible-text", "soft-hyphen", "blank", "whitespace-only", "invisible-only"],
    )
    def test_bad_opening_line_is_single_e000_at_line_1_with_rendering(
        self, tmp_path, first_line, token
    ):
        p = write(tmp_path / "a.md", first_line + b"\n---\nid: 1\nname: x\n---\n# T\n")

        errors = validate_file(p)

        assert [(e.error_code, e.line_number) for e in errors] == [("E000", 1)]
        assert token in errors[0].message

    @pytest.mark.parametrize("variant", FENCE_NEAR_MISSES)
    def test_near_miss_closing_fence_is_not_a_fence(self, tmp_path, variant):
        head = fixture_bytes(_AGENT).split(b"\n---\n")[0]
        data = head + ("\n" + variant + "\n# T\nbody\n").encode("utf-8")
        errors = validate_file(write(tmp_path / "a.md", data))
        assert [(e.error_code, e.line_number) for e in errors] == [("E000", 1)]

    def test_empty_file_is_e000_line_1_with_empty_rendering(self, tmp_path):
        errors = validate_file(write(tmp_path / "a.md", b""))
        assert [(e.error_code, e.line_number) for e in errors] == [("E000", 1)]
        assert "''" in errors[0].message

    def test_missing_closing_fence_reports_line_1(self, tmp_path):
        p = write(tmp_path / "a.md", b"---\nid: 1\nname: x\n\n# T\nbody\nmore\n")
        errors = validate_file(p)
        assert [(e.error_code, e.line_number) for e in errors] == [("E000", 1)]

    def test_utf16_file_is_single_e000_line_1_mentioning_utf8(self, tmp_path):
        errors = validate_file(write(tmp_path / "a.md", UTF16_FILE))
        assert [(e.error_code, e.line_number) for e in errors] == [("E000", 1)]
        assert "UTF-8" in errors[0].message

    def test_invalid_byte_on_line_3_is_reported_with_line_3(self, tmp_path):
        errors = validate_file(write(tmp_path / "a.md", LATIN1_LINE3))
        assert [(e.error_code, e.line_number) for e in errors] == [("E000", 3)]
        assert "UTF-8" in errors[0].message


class TestLoadBundleAcceptsVariants:
    def test_canonical_bundle_loads(self, tmp_path):
        version, blocks = _bundle_view(write(tmp_path / "b.md", _BUNDLE))
        assert version == "2.0.0"
        assert [b.name for b in blocks] == ["AuthorityHierarchy:Subagent"]
        assert "body text" in blocks[0].body

    @pytest.mark.parametrize("crlf", [False, True], ids=["lf", "crlf"])
    def test_bom_bundle_matches_canonical(self, tmp_path, crlf):
        expected = _bundle_view(write(tmp_path / "canon.md", _BUNDLE))
        variant = with_bom(to_crlf(_BUNDLE) if crlf else _BUNDLE)

        assert _bundle_view(write(tmp_path / "bom.md", variant)) == expected

    @pytest.mark.parametrize("variant", FENCE_VARIANTS)
    @pytest.mark.parametrize("position", ["opening", "closing"])
    def test_variant_fence_matches_canonical(self, tmp_path, variant, position):
        expected = _bundle_view(write(tmp_path / "canon.md", _BUNDLE))
        edited = with_fences(_BUNDLE, **{position: variant})

        assert _bundle_view(write(tmp_path / "var.md", edited)) == expected

    def test_fence_with_trailing_spaces_is_accepted(self, tmp_path):
        expected = _bundle_view(write(tmp_path / "canon.md", _BUNDLE))
        edited = with_fences(_BUNDLE, "---   ", "---  ")
        assert _bundle_view(write(tmp_path / "sp.md", edited)) == expected


class TestLoadBundleErrors:
    @pytest.mark.parametrize(
        ("first_line", "token"),
        [
            (b"# Title", "# Title"),
            ("\xad---".encode("utf-8"), "\\xad"),
            (b"", "''"),
            ("\u200b".encode("utf-8"), "\\u200b"),
        ],
        ids=["visible-text", "soft-hyphen", "blank", "invisible-only"],
    )
    def test_bad_opening_line_names_line_1_and_rendering(self, tmp_path, first_line, token):
        p = write(tmp_path / "b.md", first_line + b"\n" + _BUNDLE)
        with pytest.raises(BundleError) as info:
            load_bundle(p)
        assert "line 1" in str(info.value)
        assert token in str(info.value)

    @pytest.mark.parametrize("variant", FENCE_NEAR_MISSES)
    def test_near_miss_opening_fence_is_rejected(self, tmp_path, variant):
        p = write(tmp_path / "b.md", with_fences(_BUNDLE, opening=variant))
        with pytest.raises(BundleError) as info:
            load_bundle(p)
        assert "line 1" in str(info.value)

    def test_empty_file_names_line_1(self, tmp_path):
        with pytest.raises(BundleError) as info:
            load_bundle(write(tmp_path / "b.md", b""))
        assert "line 1" in str(info.value)
        assert "''" in str(info.value)

    def test_missing_closing_fence_names_line_1(self, tmp_path):
        p = write(tmp_path / "b.md", b"---\nid: x\ntype: bundle\nblocks:\n  - name: A\n")
        with pytest.raises(BundleError) as info:
            load_bundle(p)
        assert "line 1" in str(info.value)

    def test_utf16_bundle_raises_bundle_error_with_line_1_and_utf8(self, tmp_path):
        with pytest.raises(BundleError) as info:
            load_bundle(write(tmp_path / "b.md", UTF16_FILE))
        assert "line 1" in str(info.value)
        assert "UTF-8" in str(info.value)

    def test_invalid_byte_on_line_3_names_line_3(self, tmp_path):
        with pytest.raises(BundleError) as info:
            load_bundle(write(tmp_path / "b.md", LATIN1_LINE3))
        assert "line 3" in str(info.value)
        assert "UTF-8" in str(info.value)
