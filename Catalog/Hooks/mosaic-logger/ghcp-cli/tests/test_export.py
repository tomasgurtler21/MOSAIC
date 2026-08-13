"""Tests for the transcript export module in mosaic_logger_export (ghcp-cli variant).

Key differences from the claude-code variant:
- build_sidecar uses harness='ghcp-cli' and native_format='ghcp-cli-transcript-jsonl'

Covers:
  export_transcript: byte-for-byte copy fidelity, sidecar creation, failure modes
  build_sidecar: correct harness and native_format fields
  ExportResult: ok flag, raw_path, meta_path, byte_count, reason
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


# ---------------------------------------------------------------------------
# build_sidecar
# ---------------------------------------------------------------------------

class TestBuildSidecar(unittest.TestCase):
    """build_sidecar produces the ghcp-cli specific sidecar document."""

    def _sidecar(self, source_path="/tmp/transcript.jsonl",
                 source_mechanism="transcript_path",
                 byte_count=42) -> dict:
        return export_module.build_sidecar(source_path, source_mechanism, byte_count)

    def test_harness_is_ghcp_cli(self):
        s = self._sidecar()
        self.assertEqual("ghcp-cli", s["harness"])

    def test_harness_is_not_claude_code(self):
        s = self._sidecar()
        self.assertNotEqual("claude-code", s["harness"])

    def test_native_format_is_ghcp_cli_transcript_jsonl(self):
        s = self._sidecar()
        self.assertEqual("ghcp-cli-transcript-jsonl", s["native_format"])

    def test_source_path_matches_input(self):
        s = self._sidecar(source_path="/workspace/transcript.jsonl")
        self.assertEqual("/workspace/transcript.jsonl", s["source_path"])

    def test_source_mechanism_matches_input(self):
        s = self._sidecar(source_mechanism="transcriptPath")
        self.assertEqual("transcriptPath", s["source_mechanism"])

    def test_byte_count_matches_input(self):
        s = self._sidecar(byte_count=12345)
        self.assertEqual(12345, s["byte_count"])

    def test_captured_at_is_iso8601_ms_z(self):
        s = self._sidecar()
        self.assertIn("captured_at", s)
        self.assertRegex(s["captured_at"], _ISO8601_MS_Z)

    def test_result_is_json_serializable(self):
        s = self._sidecar()
        serialized = json.dumps(s, ensure_ascii=False)
        self.assertIsInstance(serialized, str)


# ---------------------------------------------------------------------------
# export_transcript -- byte fidelity
# ---------------------------------------------------------------------------

class TestExportTranscriptByteFidelity(unittest.TestCase):

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
        original = b'{"event": "session_start", "harness": "ghcp-cli"}\n'
        target = self.tmp_path / "output" / "04_session.raw"
        export_module.export_transcript(self._source(original), target, "transcript_path")
        self.assertEqual(original, target.read_bytes())

    def test_binary_content_preserved(self):
        original = b"\x00\xff\x01\x02\r\n" + b"ghcp-cli data" * 100
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self._source(original), target, "transcript_path")
        self.assertEqual(original, target.read_bytes())

    def test_crlf_line_endings_not_translated(self):
        original = b"line one\r\nline two\r\n"
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self._source(original), target, "transcript_path")
        self.assertEqual(original, target.read_bytes())

    def test_large_content_fully_copied(self):
        original = b"x" * 2_000_000
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self._source(original), target, "transcript_path")
        self.assertEqual(original, target.read_bytes())

    def test_export_result_ok_true_on_success(self):
        target = self.tmp_path / "04_session.raw"
        result = export_module.export_transcript(
            self._source(b"content"), target, "transcript_path"
        )
        self.assertTrue(result.ok)

    def test_creates_parent_directories(self):
        deep_target = self.tmp_path / "a" / "b" / "c" / "04_session.raw"
        export_module.export_transcript(self._source(b"data"), deep_target, "transcript_path")
        self.assertTrue(deep_target.exists())


# ---------------------------------------------------------------------------
# export_transcript -- sidecar
# ---------------------------------------------------------------------------

class TestExportTranscriptSidecar(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        content = b'{"type": "assistant"}\n'
        src = self.tmp_path / "source.jsonl"
        src.write_bytes(content)
        self.src_str = str(src)

    def tearDown(self):
        self.tmp.cleanup()

    def _export(self, target_name="04_session.raw") -> export_module.ExportResult:
        target = self.tmp_path / target_name
        return export_module.export_transcript(self.src_str, target, "transcript_path")

    def test_sidecar_created_on_success(self):
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self.src_str, target, "transcript_path")
        meta = target.with_suffix("").with_suffix(".meta.json")
        self.assertTrue(meta.exists())

    def test_sidecar_harness_is_ghcp_cli(self):
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self.src_str, target, "transcript_path")
        meta = target.with_suffix("").with_suffix(".meta.json")
        sidecar = json.loads(meta.read_text("utf-8"))
        self.assertEqual("ghcp-cli", sidecar["harness"])

    def test_sidecar_native_format_is_ghcp_cli_transcript_jsonl(self):
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self.src_str, target, "transcript_path")
        meta = target.with_suffix("").with_suffix(".meta.json")
        sidecar = json.loads(meta.read_text("utf-8"))
        self.assertEqual("ghcp-cli-transcript-jsonl", sidecar["native_format"])

    def test_sidecar_source_mechanism_matches_argument(self):
        target = self.tmp_path / "04_session.raw"
        export_module.export_transcript(self.src_str, target, "transcriptPath")
        meta = target.with_suffix("").with_suffix(".meta.json")
        sidecar = json.loads(meta.read_text("utf-8"))
        self.assertEqual("transcriptPath", sidecar["source_mechanism"])

    def test_sidecar_has_byte_count(self):
        target = self.tmp_path / "04_session.raw"
        src_bytes = pathlib.Path(self.src_str).read_bytes()
        export_module.export_transcript(self.src_str, target, "transcript_path")
        meta = target.with_suffix("").with_suffix(".meta.json")
        sidecar = json.loads(meta.read_text("utf-8"))
        self.assertEqual(len(src_bytes), sidecar["byte_count"])

    def test_export_result_has_raw_path(self):
        target = self.tmp_path / "04_session.raw"
        result = export_module.export_transcript(self.src_str, target, "transcript_path")
        self.assertIsNotNone(result.raw_path)

    def test_export_result_has_meta_path(self):
        target = self.tmp_path / "04_session.raw"
        result = export_module.export_transcript(self.src_str, target, "transcript_path")
        self.assertIsNotNone(result.meta_path)

    def test_export_result_has_byte_count(self):
        target = self.tmp_path / "04_session.raw"
        result = export_module.export_transcript(self.src_str, target, "transcript_path")
        expected = len(pathlib.Path(self.src_str).read_bytes())
        self.assertEqual(expected, result.byte_count)


# ---------------------------------------------------------------------------
# export_transcript -- failure modes
# ---------------------------------------------------------------------------

class TestExportTranscriptFailureModes(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _target(self) -> pathlib.Path:
        return self.tmp_path / "output" / "04_session.raw"

    def test_returns_failure_when_source_is_none(self):
        result = export_module.export_transcript(None, self._target(), "transcript_path")
        self.assertFalse(result.ok)

    def test_no_raw_file_written_when_source_is_none(self):
        export_module.export_transcript(None, self._target(), "transcript_path")
        self.assertFalse(self._target().exists())

    def test_returns_failure_when_source_is_empty_string(self):
        result = export_module.export_transcript("", self._target(), "transcript_path")
        self.assertFalse(result.ok)

    def test_returns_failure_when_source_file_missing(self):
        missing = str(self.tmp_path / "does_not_exist.jsonl")
        result = export_module.export_transcript(missing, self._target(), "transcript_path")
        self.assertFalse(result.ok)

    def test_no_raw_file_written_when_source_file_missing(self):
        missing = str(self.tmp_path / "nonexistent.jsonl")
        export_module.export_transcript(missing, self._target(), "transcript_path")
        self.assertFalse(self._target().exists())

    def test_never_raises_for_none_source(self):
        try:
            export_module.export_transcript(None, self._target(), "transcript_path")
        except Exception as exc:
            self.fail(f"export_transcript raised for None source: {exc}")

    def test_never_raises_for_missing_source(self):
        missing = str(self.tmp_path / "missing.jsonl")
        try:
            export_module.export_transcript(missing, self._target(), "transcript_path")
        except Exception as exc:
            self.fail(f"export_transcript raised for missing file: {exc}")

    def test_failure_result_has_reason(self):
        result = export_module.export_transcript(None, self._target(), "transcript_path")
        self.assertIsNotNone(result.reason)
        self.assertIsInstance(result.reason, str)


# ---------------------------------------------------------------------------
# ExportResult
# ---------------------------------------------------------------------------

class TestExportResult(unittest.TestCase):

    def test_ok_true_on_success(self):
        r = export_module.ExportResult(ok=True, byte_count=100)
        self.assertTrue(r.ok)

    def test_ok_false_on_failure(self):
        r = export_module.ExportResult(ok=False, reason="unreadable")
        self.assertFalse(r.ok)

    def test_reason_accessible(self):
        r = export_module.ExportResult(ok=False, reason="test reason")
        self.assertEqual("test reason", r.reason)

    def test_raw_path_none_by_default(self):
        r = export_module.ExportResult(ok=False)
        self.assertIsNone(r.raw_path)

    def test_byte_count_none_by_default(self):
        r = export_module.ExportResult(ok=False)
        self.assertIsNone(r.byte_count)


if __name__ == "__main__":
    unittest.main()
