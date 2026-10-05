"""Tests for read_last_assistant_text in mosaic_logger_transcript (ghcp-cli variant).

Covers:
  Text of the LAST assistant record, for the recorded GHCP CLI event shape
  (assistant.message with data.content) and for the type/role assistant shapes
  the existing fact reader already understands
  Degradation to None: None/empty path, missing file, directory, empty file,
  malformed lines, no assistant record, assistant record without text
  Never raises
"""

import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_transcript as transcript

from tests.runner_mode_support import FIXTURES, FINAL_RESPONSE, SUBAGENT_TRANSCRIPT


class _TranscriptCase(unittest.TestCase):

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.tmp_path = pathlib.Path(self._tmp.name)

    def write(self, records, name="t.jsonl") -> str:
        path = self.tmp_path / name
        path.write_text("\n".join(json.dumps(r) for r in records) + "\n", encoding="utf-8")
        return str(path)


class TestReadLastAssistantText(_TranscriptCase):

    def test_recorded_ghcp_event_fixture_returns_final_assistant_message(self):
        path = FIXTURES / "runner_ghcp_events_transcript.jsonl"
        self.assertEqual("Final answer text", transcript.read_last_assistant_text(str(path)))

    def test_type_assistant_fixture_returns_final_text_block(self):
        self.assertEqual(FINAL_RESPONSE,
                         transcript.read_last_assistant_text(str(SUBAGENT_TRANSCRIPT)))

    def test_role_assistant_with_string_content(self):
        path = self.write([{"role": "user", "content": "hi"},
                           {"role": "assistant", "content": "Hello there"}])
        self.assertEqual("Hello there", transcript.read_last_assistant_text(path))

    def test_type_assistant_with_string_message_content(self):
        path = self.write([{"type": "assistant", "message": {"content": "plain text"}}])
        self.assertEqual("plain text", transcript.read_last_assistant_text(path))

    def test_last_assistant_record_wins(self):
        path = self.write([{"role": "assistant", "content": "first"},
                           {"role": "user", "content": "more"},
                           {"role": "assistant", "content": "second"}])
        self.assertEqual("second", transcript.read_last_assistant_text(path))

    def test_user_text_after_last_assistant_is_not_returned(self):
        path = self.write([{"role": "assistant", "content": "answer"},
                           {"role": "user", "content": "follow-up"}])
        self.assertEqual("answer", transcript.read_last_assistant_text(path))

    def test_tool_use_blocks_are_not_part_of_the_text(self):
        path = self.write([{"type": "assistant", "message": {"content": [
            {"type": "text", "text": "the answer"},
            {"type": "tool_use", "name": "bash", "input": {"command": "SECRET_TOOL_INPUT"}}]}}])
        result = transcript.read_last_assistant_text(path)
        self.assertIn("the answer", result)
        self.assertNotIn("SECRET_TOOL_INPUT", result)

    def test_malformed_lines_are_skipped(self):
        path = self.tmp_path / "mixed.jsonl"
        path.write_text("not json\n" + json.dumps({"role": "assistant", "content": "ok"}) + "\n{bad\n",
                        encoding="utf-8")
        self.assertEqual("ok", transcript.read_last_assistant_text(str(path)))


class TestReadLastAssistantTextDegradation(_TranscriptCase):

    def test_none_path(self):
        self.assertIsNone(transcript.read_last_assistant_text(None))

    def test_empty_path(self):
        self.assertIsNone(transcript.read_last_assistant_text(""))

    def test_missing_file(self):
        self.assertIsNone(transcript.read_last_assistant_text(str(self.tmp_path / "nope.jsonl")))

    def test_directory_path(self):
        self.assertIsNone(transcript.read_last_assistant_text(str(self.tmp_path)))

    def test_empty_file(self):
        path = self.tmp_path / "empty.jsonl"
        path.write_bytes(b"")
        self.assertIsNone(transcript.read_last_assistant_text(str(path)))

    def test_all_malformed(self):
        path = self.tmp_path / "bad.jsonl"
        path.write_text("{bad\n{also bad\n", encoding="utf-8")
        self.assertIsNone(transcript.read_last_assistant_text(str(path)))

    def test_undecodable_bytes(self):
        path = self.tmp_path / "bin.jsonl"
        path.write_bytes(b"\xff\xfe\x00\x80\n\xc3\x28\n")
        self.assertIsNone(transcript.read_last_assistant_text(str(path)))

    def test_no_assistant_record(self):
        path = self.write([{"role": "user", "content": "hi"}, {"type": "system", "content": "s"}])
        self.assertIsNone(transcript.read_last_assistant_text(path))

    def test_assistant_record_without_text(self):
        path = self.write([{"role": "assistant", "model": "m"}])
        self.assertIsNone(transcript.read_last_assistant_text(path))

    def test_non_string_path_does_not_raise(self):
        try:
            result = transcript.read_last_assistant_text(12345)
        except Exception as exc:  # noqa: BLE001
            self.fail(f"read_last_assistant_text raised: {exc}")
        self.assertIsNone(result)


if __name__ == "__main__":
    unittest.main()
