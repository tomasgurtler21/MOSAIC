"""Tests for adapter version extraction from hook.yaml in mosaic_logger_core.

Covers: successful extraction (bare and quoted values), indented-key exclusion
(only top-level, zero-indent lines match), missing file, missing key, empty value,
multiple top-level version keys, and the never-raises contract.
"""

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core


def _write_yaml(directory: str, content: str) -> pathlib.Path:
    path = pathlib.Path(directory) / "hook.yaml"
    path.write_text(content, encoding="utf-8")
    return path


class TestReadAdapterVersionHappyPath(unittest.TestCase):
    """A top-level version key with a scalar value is extracted correctly."""

    def test_reads_bare_version_value(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "schema_version: '1.0'\nversion: 2.3.1\ndescription: test\n")
            self.assertEqual("2.3.1", core.read_adapter_version(p))

    def test_strips_double_quotes_from_value(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, 'version: "1.2.3"\n')
            self.assertEqual("1.2.3", core.read_adapter_version(p))

    def test_strips_single_quotes_from_value(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "version: '1.2.3'\n")
            self.assertEqual("1.2.3", core.read_adapter_version(p))

    def test_strips_surrounding_whitespace_from_value(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "version:  3.0.0  \n")
            self.assertEqual("3.0.0", core.read_adapter_version(p))

    def test_works_with_version_key_among_other_top_level_keys(self):
        with tempfile.TemporaryDirectory() as tmp:
            content = (
                "id: mosaic-logger\n"
                "schema_version: '1.0'\n"
                "version: 4.1.0\n"
                "description: logging adapter\n"
            )
            p = _write_yaml(tmp, content)
            self.assertEqual("4.1.0", core.read_adapter_version(p))


class TestReadAdapterVersionDegradationPaths(unittest.TestCase):
    """Any ambiguity or unresolvable case returns None and never raises."""

    def test_missing_file_returns_none(self):
        result = core.read_adapter_version(
            pathlib.Path("/nonexistent_dir_xyz/hook.yaml")
        )
        self.assertIsNone(result)

    def test_no_version_key_returns_none(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "schema_version: '1.0'\ndescription: no version here\n")
            self.assertIsNone(core.read_adapter_version(p))

    def test_empty_value_after_colon_returns_none(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "version:\n")
            self.assertIsNone(core.read_adapter_version(p))

    def test_empty_quoted_value_returns_none(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, 'version: ""\n')
            self.assertIsNone(core.read_adapter_version(p))

    def test_empty_single_quoted_value_returns_none(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "version: ''\n")
            self.assertIsNone(core.read_adapter_version(p))

    def test_indented_version_key_is_not_matched(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "parent:\n  version: 9.9.9\n")
            self.assertIsNone(core.read_adapter_version(p))

    def test_version_key_with_tab_indent_is_not_matched(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "parent:\n\tversion: 9.9.9\n")
            self.assertIsNone(core.read_adapter_version(p))

    def test_multiple_top_level_version_keys_returns_none(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "version: 1.0.0\nversion: 2.0.0\n")
            self.assertIsNone(core.read_adapter_version(p))

    def test_version_key_in_yaml_comment_is_not_matched(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = _write_yaml(tmp, "# version: 1.0.0\nid: test\n")
            self.assertIsNone(core.read_adapter_version(p))

    def test_never_raises_on_missing_file(self):
        try:
            result = core.read_adapter_version(pathlib.Path("/nonexistent_xyz/hook.yaml"))
            self.assertIsNone(result)
        except Exception as e:
            self.fail(f"read_adapter_version raised on missing file: {e}")

    def test_never_raises_on_empty_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = pathlib.Path(tmp) / "hook.yaml"
            p.write_bytes(b"")
            try:
                result = core.read_adapter_version(p)
                self.assertIsNone(result)
            except Exception as e:
                self.fail(f"read_adapter_version raised on empty file: {e}")

    def test_never_raises_on_binary_content(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = pathlib.Path(tmp) / "hook.yaml"
            p.write_bytes(b"\x00\xff\xfe garbage \x01\x02")
            try:
                core.read_adapter_version(p)
            except Exception as e:
                self.fail(f"read_adapter_version raised on binary file: {e}")
