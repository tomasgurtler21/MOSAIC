"""Tests for the raw transcript export primitive in mosaic_logger_export.

Covers: byte-for-byte copy fidelity (including binary content with no parsing
or re-encoding), sidecar placement and required fields, sidecar-not-written when
copy fails, missing/unreadable/None/empty source path degradation, and the
build_sidecar function in isolation.
"""

import json
import os
import pathlib
import re
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_export as export_module


_ISO8601_MS_Z = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")


class TestExportTranscriptByteFidelity(unittest.TestCase):
    """The copy must be byte-identical to the source; no parsing, re-encoding,
    or line-ending translation is permitted."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _source(self, content: bytes) -> str:
        path = self.tmp_path / "source.jsonl"
        path.write_bytes(content)
        return str(path)

    def test_text_content_is_byte_identical(self):
        original = b'{"event": "session_start", "session_id": "abc"}\n'
        target = self.tmp_path / "output" / "04_session.raw"
        export_module.export_transcript(self._source(original), target, "transcript_path")
        self.assertEqual(original, target.read_bytes())

    def test_binary_content_with_null_bytes_is_preserved(self):
        original = b"\x00\xff\x01\x02\r\n" + b"data" * 500
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self._source(original), target, "transcript_path")
        self.assertEqual(original, target.read_bytes())

    def test_content_with_crlf_line_endings_not_translated(self):
        original = b"line one\r\nline two\r\nline three\r\n"
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self._source(original), target, "transcript_path")
        self.assertEqual(original, target.read_bytes())

    def test_large_content_is_fully_copied(self):
        original = b"x" * 5_000_000
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self._source(original), target, "transcript_path")
        self.assertEqual(len(original), len(target.read_bytes()))
        self.assertEqual(original, target.read_bytes())

    def test_export_result_ok_is_true_on_success(self):
        target = self.tmp_path / "04_session.raw"
        result = export_module.export_transcript(
            self._source(b"content"), target, "transcript_path"
        )
        self.assertTrue(result.ok)


class TestExportTranscriptSidecar(unittest.TestCase):
    """The sidecar is written beside the raw file and contains all required fields."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        content = b'{"event": "test"}\n'
        self.source_path = str(self.tmp_path / "source.jsonl")
        pathlib.Path(self.source_path).write_bytes(content)
        self.target = self.tmp_path / "04_session.raw"
        self.result = export_module.export_transcript(
            self.source_path, self.target, "transcript_path"
        )
        self.sidecar_path = self.tmp_path / "04_session.meta.json"
        self.sidecar = json.loads(self.sidecar_path.read_text(encoding="utf-8"))

    def tearDown(self):
        self.tmp.cleanup()

    def test_sidecar_written_beside_raw_file(self):
        self.assertTrue(self.sidecar_path.exists())

    def test_sidecar_harness_is_claude_code(self):
        self.assertEqual("claude-code", self.sidecar["harness"])

    def test_sidecar_native_format_is_claude_code_transcript_jsonl(self):
        self.assertEqual("claude-code-transcript-jsonl", self.sidecar["native_format"])

    def test_sidecar_source_path_matches_actual_source(self):
        self.assertEqual(self.source_path, self.sidecar["source_path"])

    def test_sidecar_source_mechanism_matches_argument(self):
        self.assertEqual("transcript_path", self.sidecar["source_mechanism"])

    def test_sidecar_captured_at_is_iso8601_millisecond_utc(self):
        self.assertRegex(self.sidecar["captured_at"], _ISO8601_MS_Z)

    def test_sidecar_byte_count_matches_source_size(self):
        expected = len(pathlib.Path(self.source_path).read_bytes())
        self.assertEqual(expected, self.sidecar["byte_count"])

    def test_source_mechanism_agent_transcript_path_recorded_correctly(self):
        other_target = self.tmp_path / "sub" / "04_session.raw"
        other_target.parent.mkdir(parents=True, exist_ok=True)
        export_module.export_transcript(
            self.source_path, other_target, "agent_transcript_path"
        )
        sidecar = json.loads(
            (self.tmp_path / "sub" / "04_session.meta.json").read_text(encoding="utf-8")
        )
        self.assertEqual("agent_transcript_path", sidecar["source_mechanism"])


class TestExportTranscriptDegradation(unittest.TestCase):
    """Missing, unreadable, None, or empty source path → ok=False, nothing written."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.target = self.tmp_path / "04_session.raw"
        self.sidecar = self.tmp_path / "04_session.meta.json"

    def tearDown(self):
        self.tmp.cleanup()

    def test_missing_source_returns_failed_result(self):
        result = export_module.export_transcript(
            "/nonexistent_xyz/path.jsonl", self.target, "transcript_path"
        )
        self.assertFalse(result.ok)

    def test_missing_source_raw_file_not_written(self):
        export_module.export_transcript(
            "/nonexistent_xyz/path.jsonl", self.target, "transcript_path"
        )
        self.assertFalse(self.target.exists())

    def test_missing_source_sidecar_not_written(self):
        export_module.export_transcript(
            "/nonexistent_xyz/path.jsonl", self.target, "transcript_path"
        )
        self.assertFalse(self.sidecar.exists())

    def test_none_source_path_returns_failed_result(self):
        result = export_module.export_transcript(None, self.target, "transcript_path")
        self.assertFalse(result.ok)

    def test_none_source_path_writes_nothing(self):
        export_module.export_transcript(None, self.target, "transcript_path")
        self.assertFalse(self.target.exists())
        self.assertFalse(self.sidecar.exists())

    def test_empty_source_path_returns_failed_result(self):
        result = export_module.export_transcript("", self.target, "transcript_path")
        self.assertFalse(result.ok)

    def test_empty_source_path_writes_nothing(self):
        export_module.export_transcript("", self.target, "transcript_path")
        self.assertFalse(self.target.exists())
        self.assertFalse(self.sidecar.exists())

    def test_never_raises_on_none_source(self):
        try:
            export_module.export_transcript(None, self.target, "transcript_path")
        except Exception as e:
            self.fail(f"export_transcript raised on None source: {e}")

    def test_never_raises_on_missing_source(self):
        try:
            export_module.export_transcript(
                "/nonexistent_xyz/file.jsonl", self.target, "transcript_path"
            )
        except Exception as e:
            self.fail(f"export_transcript raised on missing source: {e}")


class TestBuildSidecar(unittest.TestCase):
    """build_sidecar produces the sidecar document without performing I/O.
    All fields are assertable in isolation."""

    def setUp(self):
        self.doc = export_module.build_sidecar(
            "/path/to/session.jsonl", "transcript_path", 184320
        )

    def test_harness_is_claude_code(self):
        self.assertEqual("claude-code", self.doc["harness"])

    def test_native_format_is_claude_code_transcript_jsonl(self):
        self.assertEqual("claude-code-transcript-jsonl", self.doc["native_format"])

    def test_source_path_is_recorded(self):
        self.assertEqual("/path/to/session.jsonl", self.doc["source_path"])

    def test_source_mechanism_is_recorded(self):
        self.assertEqual("transcript_path", self.doc["source_mechanism"])

    def test_byte_count_matches_argument(self):
        self.assertEqual(184320, self.doc["byte_count"])

    def test_captured_at_is_iso8601_millisecond_utc(self):
        self.assertRegex(self.doc["captured_at"], _ISO8601_MS_Z)

    def test_agent_transcript_path_mechanism_recorded(self):
        doc = export_module.build_sidecar(
            "/agent/session.jsonl", "agent_transcript_path", 1024
        )
        self.assertEqual("agent_transcript_path", doc["source_mechanism"])

    def test_sidecar_is_json_serializable(self):
        serialized = json.dumps(self.doc)
        self.assertIsInstance(serialized, str)

    def test_all_required_fields_present(self):
        for field in (
            "harness", "native_format", "source_path",
            "source_mechanism", "captured_at", "byte_count",
        ):
            with self.subTest(field=field):
                self.assertIn(field, self.doc)
