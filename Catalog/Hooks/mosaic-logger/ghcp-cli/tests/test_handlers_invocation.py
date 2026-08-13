"""Tests for invocation-scoped handlers in mosaic_logger_handlers_invocation (ghcp-cli variant).

Key differences from the claude-code variant:
- HookContext.__init__ takes event name as first argument (from sys.argv[1])
- Payload fields are camelCase: agentId, agentName, agentType, agentPrompt, response, transcriptPath
- ctx.agent_id is mapped from payload 'agentId' (camelCase)
- subagentStop reads 'response' (not 'last_assistant_message')
- No 'last_assistant_message' field exists in the GHCP CLI protocol

Covers:
  extract_status_code: closed enum extraction, last-occurrence-wins, None on ambiguity
  handle_subagent_start: agent mapping registration, pending dispatch pop,
    invocation_start event, user turn (when prompt present), 01_input.md artifact
  handle_subagent_stop: invocation_end event, status_code extraction,
    02_output.md artifact, transcript export, unmapped guard
"""

import json
import os
import pathlib
import sys
import tempfile
import types
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_invocation as invocation


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

_RUN_ID = "20260101T170000Z-a3f9"
_SESSION_ID = "test-ghcp-invocation-session"
_AGENT_ID = "ghcp-agent-001"
_INSTANCE_ID = "Research#1"
_TS = "2026-01-01T17:00:00.000Z"


def _make_ctx(event: str, tmp_path: pathlib.Path,
              run_id: str = _RUN_ID,
              session_id: str = _SESSION_ID,
              **payload_extra) -> core.HookContext:
    """Build a HookContext with run_id pre-set, rooted at tmp_path."""
    payload = {"sessionId": session_id, **payload_extra}
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(event, payload, tmp_path, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _make_start_ctx(tmp_path, run_id=_RUN_ID, **kwargs):
    return _make_ctx("subagentStart", tmp_path, run_id=run_id, **kwargs)


def _make_stop_ctx(tmp_path, run_id=_RUN_ID, **kwargs):
    return _make_ctx("subagentStop", tmp_path, run_id=run_id, **kwargs)


def _read_jsonl(path: pathlib.Path) -> list:
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


def _register_mapping(tmp_path, run_id, agent_id, agent_instance_id, agent_type=None):
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_id, agent_instance_id, agent_type)


def _write_transcript(path: pathlib.Path, records: list) -> str:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n",
        encoding="utf-8",
    )
    return str(path)


# ---------------------------------------------------------------------------
# extract_status_code
# ---------------------------------------------------------------------------

class TestExtractStatusCode(unittest.TestCase):

    def test_returns_none_for_none_input(self):
        self.assertIsNone(invocation.extract_status_code(None))

    def test_returns_none_for_empty_string(self):
        self.assertIsNone(invocation.extract_status_code(""))

    def test_extracts_success_from_structured_key_value(self):
        msg = 'The result is:\n{"status_code": "SUCCESS"}'
        self.assertEqual("SUCCESS", invocation.extract_status_code(msg))

    def test_extracts_blocked_from_structured_key_value(self):
        msg = '{"status_code": "BLOCKED"}'
        self.assertEqual("BLOCKED", invocation.extract_status_code(msg))

    def test_extracts_completed_needs_action(self):
        msg = 'status_code: "COMPLETED_NEEDS_ACTION"'
        self.assertEqual("COMPLETED_NEEDS_ACTION", invocation.extract_status_code(msg))

    def test_extracts_partially_done(self):
        msg = 'status_code: "PARTIALLY_DONE"'
        self.assertEqual("PARTIALLY_DONE", invocation.extract_status_code(msg))

    def test_extracts_needs_clarification(self):
        msg = 'status_code: "NEEDS_CLARIFICATION"'
        self.assertEqual("NEEDS_CLARIFICATION", invocation.extract_status_code(msg))

    def test_extracts_capability_exceeded(self):
        msg = 'status_code: "CAPABILITY_EXCEEDED"'
        self.assertEqual("CAPABILITY_EXCEEDED", invocation.extract_status_code(msg))

    def test_last_occurrence_wins(self):
        msg = 'status_code: "SUCCESS"\nstatus_code: "BLOCKED"'
        self.assertEqual("BLOCKED", invocation.extract_status_code(msg))

    def test_returns_none_for_non_enum_value(self):
        msg = 'status_code: "CUSTOM_VALUE"'
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_returns_none_when_no_status_code_key(self):
        msg = "Task completed successfully."
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_never_raises(self):
        for bad_input in [None, "", 12345, [], {}, b"bytes"]:
            with self.subTest(input=bad_input):
                try:
                    invocation.extract_status_code(bad_input)
                except Exception as exc:
                    self.fail(f"extract_status_code raised for {bad_input!r}: {exc}")


# ---------------------------------------------------------------------------
# handle_subagent_start
# ---------------------------------------------------------------------------

class TestHandleSubagentStart(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _put_dispatch(self, run_id=_RUN_ID, agent_instance_id=_INSTANCE_ID,
                      prompt=None, extracted_run_id=None):
        paths = core.build_paths(self.tmp_path)
        runstate.put_pending_dispatch(
            paths, run_id, _SESSION_ID, agent_instance_id, extracted_run_id, prompt=prompt
        )

    def test_returns_early_when_agent_id_absent(self):
        """handler requires agentId; absent agent_id is a no-op."""
        ctx = _make_start_ctx(self.tmp_path, agentName="Research")
        invocation.handle_subagent_start(ctx)
        events_file = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertFalse(events_file.exists())

    def test_emits_invocation_start_event(self):
        self._put_dispatch()
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research",
                               agentPrompt="Do research on X")
        invocation.handle_subagent_start(ctx)
        events_file = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(events_file.exists())
        events = _read_jsonl(events_file)
        types_found = {e["event"] for e in events}
        self.assertIn("invocation_start", types_found)

    def test_invocation_start_has_agent_instance_id(self):
        self._put_dispatch()
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research",
                               agentPrompt="Some task")
        invocation.handle_subagent_start(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        start_event = next(e for e in events if e["event"] == "invocation_start")
        self.assertEqual(_INSTANCE_ID, start_event["agent_instance_id"])

    def test_emits_user_turn_when_prompt_present(self):
        self._put_dispatch(prompt="Dispatch prompt text")
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research",
                               agentPrompt="Fallback prompt")
        invocation.handle_subagent_start(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        turn_events = [e for e in events if e.get("event") == "turn"]
        self.assertTrue(len(turn_events) >= 1)
        self.assertTrue(any(e.get("role") == "user" for e in turn_events))

    def test_no_user_turn_when_prompt_absent(self):
        """When both dispatch and payload agentPrompt are absent, no user turn emitted."""
        paths = core.build_paths(self.tmp_path)
        # Put a dispatch with no prompt
        runstate.put_pending_dispatch(paths, _RUN_ID, _SESSION_ID, _INSTANCE_ID, None, prompt=None)
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research")
        invocation.handle_subagent_start(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        turn_events = [e for e in events if e.get("event") == "turn"]
        self.assertEqual(0, len(turn_events))

    def test_registers_agent_mapping(self):
        self._put_dispatch()
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research")
        invocation.handle_subagent_start(ctx)
        mapping = runstate.get_agent_mapping(ctx.paths, _RUN_ID, _AGENT_ID)
        self.assertIsNotNone(mapping)
        self.assertEqual(_INSTANCE_ID, mapping["agent_instance_id"])

    def test_writes_01_input_md_artifact(self):
        self._put_dispatch(prompt="Task prompt")
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research",
                               agentPrompt="Fallback")
        invocation.handle_subagent_start(ctx)
        input_path = ctx.paths.invocation_input(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(input_path.exists())

    def test_01_input_md_contains_agent_instance_id(self):
        self._put_dispatch(prompt="Task prompt")
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research",
                               agentPrompt="Fallback")
        invocation.handle_subagent_start(ctx)
        content = ctx.paths.invocation_input(_RUN_ID, _INSTANCE_ID).read_text("utf-8")
        self.assertIn(_INSTANCE_ID, content)

    def test_fallback_agent_instance_id_when_no_dispatch(self):
        """Without pending dispatch or parseable prompt, uses 'unknown-agent'."""
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research",
                               agentPrompt="No ID embedded here at all")
        invocation.handle_subagent_start(ctx)
        events_file = ctx.paths.invocation_events(_RUN_ID, "unknown-agent")
        self.assertTrue(events_file.exists())

    def test_reads_agent_instance_id_from_agent_prompt_when_no_dispatch(self):
        """Falls back to extracting from agentPrompt when dispatch queue is empty."""
        prompt_with_id = f'agent_instance_id: "TestWriter#5"\nDo some work.'
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter",
                               agentPrompt=prompt_with_id)
        invocation.handle_subagent_start(ctx)
        events_file = ctx.paths.invocation_events(_RUN_ID, "TestWriter#5")
        self.assertTrue(events_file.exists())

    def test_events_written_to_invocation_stream_not_orchestrator(self):
        self._put_dispatch()
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="Research")
        invocation.handle_subagent_start(ctx)
        orchestrator_file = ctx.paths.orchestrator_events(_RUN_ID)
        # Orchestrator stream may exist from other operations but invocation_start should
        # not be there
        invocation_file = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(invocation_file.exists())
        if orchestrator_file.exists():
            orch_events = _read_jsonl(orchestrator_file)
            orch_types = {e.get("event") for e in orch_events}
            self.assertNotIn("invocation_start", orch_types)

    def test_does_not_raise(self):
        self._put_dispatch()
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID)
        try:
            invocation.handle_subagent_start(ctx)
        except Exception as exc:
            self.fail(f"handle_subagent_start raised: {exc}")


# ---------------------------------------------------------------------------
# handle_subagent_stop
# ---------------------------------------------------------------------------

class TestHandleSubagentStop(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _register(self, agent_id=_AGENT_ID, instance_id=_INSTANCE_ID):
        _register_mapping(self.tmp_path, _RUN_ID, agent_id, instance_id)

    def test_returns_early_when_agent_id_absent(self):
        ctx = _make_stop_ctx(self.tmp_path, response="Done.")
        invocation.handle_subagent_stop(ctx)
        events_file = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertFalse(events_file.exists())

    def test_returns_early_when_agent_id_unmapped(self):
        """Guard: no-op for unmapped_* invocation IDs."""
        ctx = _make_stop_ctx(self.tmp_path, agentId="unmapped-agent-xyz",
                              response="Done.")
        invocation.handle_subagent_stop(ctx)
        self.assertFalse(ctx.paths.invocation_events(
            _RUN_ID, f"unmapped_unmapped-agent-xyz").exists())

    def test_emits_invocation_end_event(self):
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID,
                              response='{"status_code": "SUCCESS"}')
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        event_types = {e["event"] for e in events}
        self.assertIn("invocation_end", event_types)

    def test_invocation_end_has_agent_instance_id(self):
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID, response="Done.")
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        end_event = next(e for e in events if e["event"] == "invocation_end")
        self.assertEqual(_INSTANCE_ID, end_event["agent_instance_id"])

    def test_status_code_extracted_from_response(self):
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID,
                              response='{"status_code": "SUCCESS"}')
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        end_event = next(e for e in events if e["event"] == "invocation_end")
        self.assertEqual("SUCCESS", end_event["status_code"])

    def test_status_code_absent_when_not_extractable(self):
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID,
                              response="Task completed but no structured code.")
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        end_event = next(e for e in events if e["event"] == "invocation_end")
        self.assertNotIn("status_code", end_event)

    def test_reads_response_from_camelcase_field(self):
        """GHCP CLI uses 'response' (not 'last_assistant_message')."""
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID,
                              response="The agent's final response text.")
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        end_event = next(e for e in events if e["event"] == "invocation_end")
        self.assertEqual("The agent's final response text.", end_event["response"])

    def test_emits_assistant_turn_when_response_present(self):
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID, response="Done.")
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        turn_events = [e for e in events if e.get("event") == "turn"
                       and e.get("role") == "assistant"]
        self.assertTrue(len(turn_events) >= 1)

    def test_no_assistant_turn_when_response_absent(self):
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID)
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        turn_events = [e for e in events if e.get("event") == "turn"
                       and e.get("role") == "assistant"]
        self.assertEqual(0, len(turn_events))

    def test_writes_02_output_md_artifact(self):
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID, response="Done.")
        invocation.handle_subagent_stop(ctx)
        output_path = ctx.paths.invocation_output(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(output_path.exists())

    def test_model_from_transcript_in_invocation_end(self):
        self._register()
        transcript_file = self.tmp_path / "sub_transcript.jsonl"
        _write_transcript(transcript_file, [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 100, "output_tokens": 50},
            }},
        ])
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID, response="Done.",
                              transcriptPath=str(transcript_file))
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        end_event = next(e for e in events if e["event"] == "invocation_end")
        self.assertIn("model", end_event)
        self.assertEqual("claude-opus-4-5", end_event["model"])

    def test_exports_subagent_transcript(self):
        self._register()
        transcript_file = self.tmp_path / "sub_transcript.jsonl"
        content = b'{"type": "assistant"}\n'
        transcript_file.write_bytes(content)
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID, response="Done.",
                              transcriptPath=str(transcript_file))
        invocation.handle_subagent_stop(ctx)
        raw = ctx.paths.invocation_raw(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(raw.exists())
        self.assertEqual(content, raw.read_bytes())

    def test_does_not_raise(self):
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID, response="Done.")
        try:
            invocation.handle_subagent_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_subagent_stop raised: {exc}")

    def test_events_written_to_invocation_stream(self):
        """invocation events must be routed to invocation stream, not orchestrator."""
        self._register()
        ctx = _make_stop_ctx(self.tmp_path, agentId=_AGENT_ID, response="Done.")
        invocation.handle_subagent_stop(ctx)
        inv_file = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(inv_file.exists())
        if ctx.paths.orchestrator_events(_RUN_ID).exists():
            orch_events = _read_jsonl(ctx.paths.orchestrator_events(_RUN_ID))
            orch_types = {e.get("event") for e in orch_events}
            self.assertNotIn("invocation_end", orch_types)


if __name__ == "__main__":
    unittest.main()
