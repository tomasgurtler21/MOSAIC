"""Tests for the pure and persisted building blocks of hand-back completion.

Covers the agent-transcript path derivation rule, the exclusive completion
claim (single winner, holder visibility, clearing for a new cycle), the
hand-back message helpers, and the two extra rows render_output can show for a
hand-back completion.
"""

import os
import pathlib
import sys
import tempfile
import threading
import types
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_invocation as invocation
import mosaic_logger_transcript as transcript
import mosaic_logger_artifacts as artifacts


_RUN_ID = "20260101T170000Z-c7d1"
_TS = "2026-01-01T17:00:00.000Z"
_FACTS = types.SimpleNamespace(model="claude-opus-4-5",
                               token_usage={"input_tokens": 1})


def _expected(base_without_ext, agent_id):
    return os.path.join(base_without_ext, "subagents", f"agent-{agent_id}.jsonl")


class TestDeriveAgentTranscriptPath(unittest.TestCase):
    """The agent transcript sits beside the session transcript, in a directory
    named like the session file without its extension."""

    def test_windows_style_session_path(self):
        base = r"C:\Users\u\.claude\projects\C--Proj\d92f807d-1111-2222"
        result = transcript.derive_agent_transcript_path(
            base + ".jsonl", "a24d6c04de2a3dec4")
        self.assertEqual(_expected(base, "a24d6c04de2a3dec4"), result)

    def test_posix_style_session_path(self):
        base = "/home/u/.claude/projects/-proj/d92f807d-1111-2222"
        result = transcript.derive_agent_transcript_path(
            base + ".jsonl", "a24d6c04de2a3dec4")
        self.assertEqual(_expected(base, "a24d6c04de2a3dec4"), result)

    def test_only_the_trailing_extension_is_removed(self):
        base = os.path.join("some", "dir.jsonl.d", "sess")
        result = transcript.derive_agent_transcript_path(base + ".jsonl", "ag1")
        self.assertEqual(_expected(base, "ag1"), result)

    def test_none_when_transcript_path_missing_or_empty(self):
        self.assertIsNone(transcript.derive_agent_transcript_path(None, "ag1"))
        self.assertIsNone(transcript.derive_agent_transcript_path("", "ag1"))

    def test_none_when_agent_id_missing_or_empty(self):
        self.assertIsNone(transcript.derive_agent_transcript_path("/p/s.jsonl", None))
        self.assertIsNone(transcript.derive_agent_transcript_path("/p/s.jsonl", ""))

    def test_none_when_transcript_path_has_other_extension(self):
        self.assertIsNone(transcript.derive_agent_transcript_path("/p/s.json", "ag1"))
        self.assertIsNone(transcript.derive_agent_transcript_path("/p/s", "ag1"))

    def test_none_for_non_string_inputs(self):
        self.assertIsNone(transcript.derive_agent_transcript_path(123, "ag1"))
        self.assertIsNone(transcript.derive_agent_transcript_path("/p/s.jsonl", 7))

    def test_performs_no_io_and_does_not_create_directories(self):
        with tempfile.TemporaryDirectory() as tmp:
            session = os.path.join(tmp, "sess.jsonl")
            before = set(pathlib.Path(tmp).rglob("*"))
            transcript.derive_agent_transcript_path(session, "ag1")
            self.assertEqual(before, set(pathlib.Path(tmp).rglob("*")))


class TestCompletionClaim(unittest.TestCase):
    """claim_completion gives exactly one caller the completion of an
    agent's current cycle and lets everyone else see who holds it."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_first_claim_is_claimed(self):
        result = runstate.claim_completion(
            self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_HANDBACK, _TS)
        self.assertEqual("claimed", result.outcome)

    def test_claim_document_records_source_time_and_transcript(self):
        runstate.claim_completion(
            self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_HANDBACK, _TS,
            agent_transcript_path="/x/subagents/agent-ag1.jsonl")
        doc = runstate.get_completion_claim(self.paths, _RUN_ID, "ag1")
        self.assertEqual("handback", doc["source"])
        self.assertEqual(_TS, doc["claimed_at"])
        self.assertEqual("/x/subagents/agent-ag1.jsonl", doc["agent_transcript_path"])

    def test_claim_file_lives_in_dot_prefixed_completion_dir(self):
        runstate.claim_completion(
            self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_HANDBACK, _TS)
        entry = self.paths.completion_claim_entry(_RUN_ID, "ag1")
        self.assertTrue(entry.exists())
        relative = entry.relative_to(self.paths.run_root(_RUN_ID))
        self.assertTrue(relative.parts[0].startswith("."))

    def test_second_claim_is_held_and_exposes_first_holder(self):
        runstate.claim_completion(
            self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_HANDBACK, _TS)
        result = runstate.claim_completion(
            self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_SUBAGENT_STOP,
            "2026-01-01T17:00:09.000Z")
        self.assertEqual("held", result.outcome)
        self.assertEqual("handback", result.holder["source"])

    def test_claims_for_different_agents_are_independent(self):
        runstate.claim_completion(
            self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_HANDBACK, _TS)
        result = runstate.claim_completion(
            self.paths, _RUN_ID, "ag2", runstate.CLAIM_SOURCE_HANDBACK, _TS)
        self.assertEqual("claimed", result.outcome)

    def test_get_claim_is_none_when_absent(self):
        self.assertIsNone(runstate.get_completion_claim(self.paths, _RUN_ID, "nobody"))

    def test_get_claim_is_none_for_malformed_file(self):
        entry = self.paths.completion_claim_entry(_RUN_ID, "ag1")
        entry.parent.mkdir(parents=True, exist_ok=True)
        entry.write_text("{not json", encoding="utf-8")
        self.assertIsNone(runstate.get_completion_claim(self.paths, _RUN_ID, "ag1"))

    def test_clear_allows_a_new_claim(self):
        runstate.claim_completion(
            self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_HANDBACK, _TS)
        self.assertTrue(runstate.clear_completion_claim(self.paths, _RUN_ID, "ag1"))
        result = runstate.claim_completion(
            self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_SUBAGENT_STOP, _TS)
        self.assertEqual("claimed", result.outcome)

    def test_clear_without_claim_is_true_and_creates_nothing(self):
        self.assertTrue(runstate.clear_completion_claim(self.paths, _RUN_ID, "ag1"))
        self.assertFalse(self.paths.completion_claim_dir(_RUN_ID).exists())

    def test_concurrent_claims_have_exactly_one_winner(self):
        outcomes = []
        lock = threading.Lock()
        barrier = threading.Barrier(8)

        def worker(i):
            barrier.wait()
            source = (runstate.CLAIM_SOURCE_HANDBACK if i % 2 == 0
                      else runstate.CLAIM_SOURCE_SUBAGENT_STOP)
            r = runstate.claim_completion(self.paths, _RUN_ID, "ag-race", source, _TS)
            with lock:
                outcomes.append(r.outcome)

        threads = [threading.Thread(target=worker, args=(i,)) for i in range(8)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=20)
        self.assertEqual(8, len(outcomes))
        self.assertEqual(1, outcomes.count("claimed"))
        self.assertEqual(7, outcomes.count("held"))

    def test_claim_never_raises_when_directory_cannot_be_created(self):
        blocker = pathlib.Path(self.tmp.name) / "OrchestrationLogs"
        blocker.write_bytes(b"")
        try:
            result = runstate.claim_completion(
                self.paths, _RUN_ID, "ag1", runstate.CLAIM_SOURCE_HANDBACK, _TS)
        except Exception as exc:
            self.fail(f"claim_completion raised: {exc}")
        self.assertEqual("error", result.outcome)


class TestHandbackMessageHelpers(unittest.TestCase):

    def _ctx(self, **payload):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            return core.HookContext(
                {"hook_event_name": "PostToolUse", "session_id": "s", **payload},
                root, core.build_paths(root), _TS)

    def test_extract_returns_message_verbatim(self):
        ctx = self._ctx(tool_input={"message": '  {"a": 1}\n'})
        self.assertEqual('  {"a": 1}\n', invocation.extract_handback_message(ctx))

    def test_extract_keeps_empty_string(self):
        ctx = self._ctx(tool_input={"message": ""})
        self.assertEqual("", invocation.extract_handback_message(ctx))

    def test_extract_none_when_tool_input_absent(self):
        self.assertIsNone(invocation.extract_handback_message(self._ctx()))

    def test_extract_none_when_tool_input_not_an_object(self):
        ctx = self._ctx(tool_input="message")
        self.assertIsNone(invocation.extract_handback_message(ctx))

    def test_extract_none_when_message_absent_or_not_a_string(self):
        self.assertIsNone(invocation.extract_handback_message(
            self._ctx(tool_input={})))
        self.assertIsNone(invocation.extract_handback_message(
            self._ctx(tool_input={"message": {"a": 1}})))
        self.assertIsNone(invocation.extract_handback_message(
            self._ctx(tool_input={"message": None})))

    def test_classify_json_object_is_none(self):
        self.assertIsNone(invocation.classify_handback_message(
            '{"status_code": "SUCCESS"}'))

    def test_classify_json_with_surrounding_whitespace_is_none(self):
        self.assertIsNone(invocation.classify_handback_message('  {"a": 1}\n'))

    def test_classify_empty_and_whitespace(self):
        self.assertEqual("empty", invocation.classify_handback_message(""))
        self.assertEqual("empty", invocation.classify_handback_message("  \n\t"))

    def test_classify_plain_text_is_non_json(self):
        self.assertEqual("non_json", invocation.classify_handback_message("All done."))


class TestRenderOutputHandbackRows(unittest.TestCase):

    def _ctx(self, tmp):
        root = pathlib.Path(tmp)
        ctx = core.HookContext(
            {"hook_event_name": "PostToolUse", "session_id": "s", "agent_id": "ag1"},
            root, core.build_paths(root), _TS)
        ctx.run_id = _RUN_ID
        return ctx

    def test_rows_shown_when_supplied(self):
        with tempfile.TemporaryDirectory() as tmp:
            text = artifacts.render_output(
                self._ctx(tmp), "Reviewer#1", "not json", None, _FACTS,
                completion_source="handback", response_format="non_json")
        self.assertIn("Completion Source", text)
        self.assertIn("handback", text)
        self.assertIn("Response Format", text)
        self.assertIn("non_json", text)

    def test_rows_omitted_when_none_and_output_unchanged(self):
        with tempfile.TemporaryDirectory() as tmp:
            ctx = self._ctx(tmp)
            plain = artifacts.render_output(ctx, "Reviewer#1", "resp", "SUCCESS", _FACTS)
            explicit = artifacts.render_output(
                ctx, "Reviewer#1", "resp", "SUCCESS", _FACTS,
                completion_source=None, response_format=None)
        self.assertNotIn("Completion Source", plain)
        self.assertNotIn("Response Format", plain)
        self.assertEqual(plain, explicit)

    def test_empty_response_renders_response_section(self):
        with tempfile.TemporaryDirectory() as tmp:
            text = artifacts.render_output(
                self._ctx(tmp), "Reviewer#1", "", None, _FACTS,
                completion_source="handback", response_format="empty")
        self.assertIn("## Response", text)


if __name__ == "__main__":
    unittest.main()
