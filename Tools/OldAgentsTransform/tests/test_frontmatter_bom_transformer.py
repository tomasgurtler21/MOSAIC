"""Transformer, CLI and read_generic_id behavior for BOM-prefixed, variant-fence
and non-UTF-8 input."""
from __future__ import annotations

import pathlib
import subprocess
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))
sys.path.insert(0, str(pathlib.Path(__file__).parent))

import boundary_transformer as bt  # noqa: E402
import generic_lookup  # noqa: E402
from boundary_transformer import transform_file  # noqa: E402
from frontmatter_build import read_generic_id  # noqa: E402
from frontmatter_bom_helpers import (  # noqa: E402
    BOM, FENCE_NEAR_MISSES, FENCE_VARIANTS, LATIN1_LINE3, SMALL_VALID, UTF16_FILE,
    fixture_bytes, to_crlf, with_bom, with_fences, write,
)

_CLI = _TOOLS_DIR / "boundary_transformer.py"
_GENERIC = "generic_standard_input.md"
_HARNESS = "harness_identity_regions_input.md"
_HARNESS_REF = "harness_identity_regions_generic_ref.md"
_TRANSFORMED = "harness_only_transformed_input.md"


def _run(tmp_path, tag, data, ref_data=None):
    """Transform `data` (written as <tag>/agent.md); return (result, output bytes or None)."""
    d = tmp_path / tag
    src = write(d / "agent.md", data)
    ref = write(d / "ref.md", ref_data) if ref_data is not None else None
    out = d / "out.md"
    result = transform_file(src, out, ref)
    return result, (out.read_bytes() if out.exists() else None)


def _baseline(tmp_path, data, ref_data=None):
    result, out = _run(tmp_path, "baseline", data, ref_data)
    assert result.success, result.errors
    assert not out.startswith(BOM)
    return result, out


class TestBomInput:
    @pytest.mark.parametrize("crlf", [False, True], ids=["lf", "crlf"])
    def test_generic_bom_input_output_matches_bomless(self, tmp_path, crlf):
        data = fixture_bytes(_GENERIC)
        _, expected = _baseline(tmp_path, data)
        variant = to_crlf(data) if crlf else data

        result, out = _run(tmp_path, "bom", with_bom(variant))

        assert result.success, result.errors
        assert out == expected
        assert not out.startswith(BOM)

    @pytest.mark.parametrize("crlf", [False, True], ids=["lf", "crlf"])
    def test_harness_with_ref_bom_input_output_matches_bomless(self, tmp_path, crlf):
        data, ref = fixture_bytes(_HARNESS), fixture_bytes(_HARNESS_REF)
        base_result, expected = _baseline(tmp_path, data, ref)
        assert not base_result.degraded
        variant = to_crlf(data) if crlf else data

        result, out = _run(tmp_path, "bom", with_bom(variant), ref)

        assert result.success, result.errors
        assert not result.degraded
        assert out == expected

    @pytest.mark.parametrize("crlf", [False, True], ids=["lf", "crlf"])
    def test_bom_generic_ref_is_parsed_and_used(self, tmp_path, crlf):
        data, ref = fixture_bytes(_HARNESS), fixture_bytes(_HARNESS_REF)
        _, expected = _baseline(tmp_path, data, ref)
        ref_variant = with_bom(to_crlf(ref) if crlf else ref)

        result, out = _run(tmp_path, "bomref", data, ref_variant)

        assert result.success, result.errors
        assert not result.degraded
        assert out == expected


class TestVariantFences:
    @pytest.mark.parametrize("variant", FENCE_VARIANTS)
    @pytest.mark.parametrize("position", ["opening", "closing"])
    def test_variant_fence_output_matches_canonical(self, tmp_path, variant, position):
        data = fixture_bytes(_GENERIC)
        _, expected = _baseline(tmp_path, data)
        edited = with_fences(data, **{position: variant})

        result, out = _run(tmp_path, "variant", edited)

        assert result.success, result.errors
        assert out == expected
        assert not out.startswith(BOM)

    @pytest.mark.parametrize("variant", FENCE_VARIANTS)
    def test_variant_fence_generic_ref_matches_canonical(self, tmp_path, variant):
        data, ref = fixture_bytes(_HARNESS), fixture_bytes(_HARNESS_REF)
        _, expected = _baseline(tmp_path, data, ref)

        result, out = _run(tmp_path, "variantref", data, with_fences(ref, variant, variant))

        assert result.success, result.errors
        assert not result.degraded
        assert out == expected

    @pytest.mark.parametrize("variant", FENCE_NEAR_MISSES)
    def test_near_miss_opening_fence_is_rejected_at_line_1(self, tmp_path, variant):
        result, out = _run(tmp_path, "nm", with_fences(fixture_bytes(_GENERIC), opening=variant))
        assert not result.success
        assert result.errors[0].line_number == 1
        assert out is None


class TestReadGenericId:
    def test_bom_on_first_line(self):
        assert read_generic_id(["\ufeff---\n", "id: 7\n", "name: x\n", "---\n"]) == "7"

    @pytest.mark.parametrize("variant", FENCE_VARIANTS)
    @pytest.mark.parametrize("terminator", ["", "\n", "\r\n"])
    def test_variant_fences(self, variant, terminator):
        lines = [variant + terminator, "id: 12" + terminator, variant + terminator]
        assert read_generic_id(lines) == "12"

    def test_id_after_variant_closing_fence_is_not_read(self):
        lines = ["---", "name: x", "\u200b---", "id: 5"]
        assert read_generic_id(lines) is None


class TestCliPreChecks:
    def test_resolve_cli_generic_ref_finds_ref_for_bom_harness(self, tmp_path, monkeypatch):
        root = tmp_path / "generic_root"
        ref = write(root / "sub" / "my-agent.md", fixture_bytes(_HARNESS_REF))
        monkeypatch.setattr(generic_lookup, "GENERIC_AGENTS_ROOT", root)
        monkeypatch.setattr(generic_lookup, "GENERIC_ORCHESTRATOR_PATH", tmp_path / "none.md")
        harness = write(tmp_path / "my-agent.md", with_bom(fixture_bytes(_HARNESS)))

        assert bt.resolve_cli_generic_ref(harness, None) == ref

    def test_cli_would_skip_true_for_bom_already_transformed(self, tmp_path):
        p = write(tmp_path / "agent.md", with_bom(fixture_bytes(_TRANSFORMED)))
        assert bt._cli_would_skip(p, None) is True

    def test_main_skips_bom_already_transformed(
        self, tmp_path, monkeypatch, capsys
    ):
        root = tmp_path / "generic_root"
        write(root / "sub" / "my-agent.md", fixture_bytes(_HARNESS_REF))
        monkeypatch.setattr(generic_lookup, "GENERIC_AGENTS_ROOT", root)
        monkeypatch.setattr(generic_lookup, "GENERIC_ORCHESTRATOR_PATH", tmp_path / "none.md")
        src = write(tmp_path / "my-agent.md", with_bom(fixture_bytes(_TRANSFORMED)))
        out = tmp_path / "out.md"
        monkeypatch.setattr(sys, "argv", ["bt", str(src), "--output", str(out)])

        code = bt._main()

        assert code == bt.EXIT_OK
        assert "already carries canonical boundary tags" in capsys.readouterr().err
        assert not out.exists()

    def test_cli_skips_bom_already_transformed_file(self, tmp_path):
        src = write(tmp_path / "agent.md", with_bom(fixture_bytes(_TRANSFORMED)))
        out = tmp_path / "out.md"

        proc = subprocess.run([sys.executable, str(_CLI), str(src), "--output", str(out)],
                              capture_output=True, text=True)

        assert proc.returncode == 0, proc.stderr
        assert "already carries canonical boundary tags" in proc.stderr
        assert not out.exists()

    def test_cli_transforms_bom_input_with_explicit_bom_ref(self, tmp_path):
        _, expected = _baseline(tmp_path, fixture_bytes(_HARNESS), fixture_bytes(_HARNESS_REF))
        src = write(tmp_path / "c" / "agent.md", with_bom(fixture_bytes(_HARNESS)))
        ref = write(tmp_path / "c" / "ref.md", with_bom(fixture_bytes(_HARNESS_REF)))
        out = tmp_path / "c" / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_CLI), str(src), "--output", str(out),
             "--generic-ref", str(ref), "--force"],
            capture_output=True, text=True)

        assert proc.returncode == 0, proc.stderr
        assert out.read_bytes() == expected


class TestOpeningAndClosingFenceErrors:
    @pytest.mark.parametrize(
        ("first_line", "token"),
        [
            (b"# Title", "# Title"),
            ("\xad---".encode("utf-8"), "\\xad"),
            (b"", "''"),
            (b"   ", "'   '"),
            ("\u200b".encode("utf-8"), "\\u200b"),
        ],
        ids=["visible-text", "soft-hyphen", "blank", "whitespace-only", "invisible-only"],
    )
    def test_bad_opening_line_reports_line_1_and_rendering(self, tmp_path, first_line, token):
        data = first_line + b"\n---\nid: 1\nversion: 1.0.0\nname: x\n---\n# T\n"

        result, out = _run(tmp_path, "bad", data)

        assert not result.success
        assert result.errors[0].line_number == 1
        assert token in result.errors[0].message
        assert out is None

    def test_empty_file_reports_line_1_and_empty_rendering(self, tmp_path):
        result, _ = _run(tmp_path, "empty", b"")
        assert not result.success
        assert result.errors[0].line_number == 1
        assert "''" in result.errors[0].message

    def test_missing_closing_fence_reports_line_1(self, tmp_path):
        result, _ = _run(tmp_path, "open", b"---\nid: 1\nversion: 1.0.0\nname: x\n\n# T\nbody\n")
        assert not result.success
        assert result.errors[0].line_number == 1

    def test_bad_generic_ref_opening_reports_line_1_with_rendering(self, tmp_path):
        data = fixture_bytes(_HARNESS)
        ref = "\xad---\n".encode("utf-8") + fixture_bytes(_HARNESS_REF)

        result, _ = _run(tmp_path, "badref", data, ref)

        assert not result.success
        assert result.errors[0].line_number == 1
        assert "\\xad" in result.errors[0].message


class TestNonUtf8:
    def test_utf16_input_is_reported_with_line_1(self, tmp_path):
        result, out = _run(tmp_path, "u16", UTF16_FILE)
        assert not result.success
        assert [e.line_number for e in result.errors] == [1]
        assert "UTF-8" in result.errors[0].message
        assert out is None

    def test_invalid_byte_on_line_3_is_reported_with_line_3(self, tmp_path):
        result, out = _run(tmp_path, "l1", LATIN1_LINE3)
        assert not result.success
        assert [e.line_number for e in result.errors] == [3]
        assert "UTF-8" in result.errors[0].message
        assert out is None

    def test_utf16_generic_ref_is_reported_naming_the_ref(self, tmp_path):
        result, out = _run(tmp_path, "u16ref", fixture_bytes(_HARNESS), UTF16_FILE)
        assert not result.success
        assert [e.line_number for e in result.errors] == [1]
        assert "UTF-8" in result.errors[0].message
        assert "ref.md" in result.errors[0].message
        assert out is None

    def test_invalid_byte_in_generic_ref_reports_its_line(self, tmp_path):
        result, _ = _run(tmp_path, "l1ref", fixture_bytes(_HARNESS), LATIN1_LINE3)
        assert not result.success
        assert result.errors[0].line_number == 3
        assert "UTF-8" in result.errors[0].message

    def test_cli_on_utf16_file_exits_nonzero_with_path_and_line(self, tmp_path):
        src = write(tmp_path / "agent.md", UTF16_FILE)

        proc = subprocess.run(
            [sys.executable, str(_CLI), str(src), "--output", str(tmp_path / "o.md")],
            capture_output=True, text=True)

        assert proc.returncode != 0
        assert f"{src}:1:" in proc.stderr
        assert "UTF-8" in proc.stderr
        assert "Traceback" not in proc.stderr

    def test_valid_utf8_input_is_not_reported_as_non_utf8(self, tmp_path):
        result, _ = _run(tmp_path, "ok", SMALL_VALID)
        assert all("UTF-8" not in e.message for e in result.errors)
