"""Tests for the SubagentStart and SubagentStop handlers.

Covers agent_instance_id resolution from prompt text, fallback when prompt is
absent or unparseable, invocation_start and invocation_end events, boundary
turns, status_code extraction, token usage and model from subagent transcript,
per-invocation artifact set, agent_id mapping round trip, folder-name
sanitization, and the guarantee that no invocation event reaches the
orchestrator event stream.
"""

import datetime
import json
import os
import pathlib
import re
import sys
import tempfile
import types
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_invocation as invocation
import mosaic_logger_transcript as transcript
import mosaic_logger_artifacts as artifacts


# ---------------------------------------------------------------------------
# Shared constants
# ---------------------------------------------------------------------------

_RUN_ID = "20260101T170000Z-a3f9"
_SESSION_ID = "test-session-invocation"
_TS = "2026-01-01T17:00:00.000Z"
_ISO8601_MS_Z = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")

# A simple namespace that stands in for TurnFacts when the real class is absent.
_MOCK_FACTS = types.SimpleNamespace(
    model="claude-opus-4-5",
    token_usage={"input_tokens": 100, "output_tokens": 50},
)
_MOCK_EMPTY_FACTS = types.SimpleNamespace(model=None, token_usage=None)


# ---------------------------------------------------------------------------
# Helper functions
# ---------------------------------------------------------------------------

def _make_ctx(tmp_path, hook_event_name, run_id=_RUN_ID,
              session_id=_SESSION_ID, **payload_extra):
    """Build a HookContext with run_id pre-set, rooted at tmp_path."""
    payload = {
        "hook_event_name": hook_event_name,
        "session_id": session_id,
        **payload_extra,
    }
    workspace_root = pathlib.Path(tmp_path)
    paths = core.build_paths(workspace_root)
    ctx = core.HookContext(payload, workspace_root, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _make_start_ctx(tmp_path, run_id=_RUN_ID, **kwargs):
    return _make_ctx(tmp_path, "SubagentStart", run_id=run_id, **kwargs)


def _make_stop_ctx(tmp_path, run_id=_RUN_ID, **kwargs):
    return _make_ctx(tmp_path, "SubagentStop", run_id=run_id, **kwargs)


def _read_jsonl(path):
    """Read all non-empty lines of a JSONL file as parsed dicts."""
    return [json.loads(ln) for ln in pathlib.Path(path).read_text("utf-8").splitlines()
            if ln.strip()]


def _write_transcript_file(dir_path, records):
    """Write a minimal Claude Code transcript JSONL and return its path string."""
    dir_path = pathlib.Path(dir_path)
    dir_path.mkdir(parents=True, exist_ok=True)
    path = dir_path / "transcript.jsonl"
    with open(path, "w", encoding="utf-8") as fh:
        for record in records:
            fh.write(json.dumps(record) + "\n")
    return str(path)


def _register_mapping(tmp_path, run_id, agent_id, agent_instance_id, agent_type=None):
    """Pre-register an agent_id -> agent_instance_id mapping on disk."""
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_id, agent_instance_id, agent_type)


# ---------------------------------------------------------------------------
# T4.1  agent_instance_id resolution and fallback
# ---------------------------------------------------------------------------

class TestAgentInstanceIdResolution(unittest.TestCase):
    """handle_subagent_start resolves agent_instance_id from the dispatched prompt,
    falling back to the documented pattern when the prompt is absent or contains
    no extractable ID. The field is NEVER omitted, NEVER substituted by agent_id."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.run_id = _RUN_ID

    def tearDown(self):
        self.tmp.cleanup()

    def _paths(self):
        return core.build_paths(pathlib.Path(self.tmp.name))

    def _invocation_dirs(self):
        run_root = self._paths().run_root(self.run_id)
        if not run_root.exists():
            return []
        return [d for d in run_root.iterdir()
                if d.is_dir() and not d.name.startswith(".")]

    def test_extracts_structured_json_key_from_prompt(self):
        """agent_instance_id extracted from structured JSON key in the prompt."""
        prompt = '{"agent_instance_id": "TestWriter#28", "task": "do something"}'
        ctx = _make_start_ctx(
            self.tmp.name, self.run_id,
            agent_id="harness-a1b2", agent_type="TestWriter",
            agent_prompt=prompt,
        )
        invocation.handle_subagent_start(ctx)
        event_path = self._paths().invocation_events(self.run_id, "TestWriter#28")
        self.assertTrue(event_path.exists())
        event = _read_jsonl(event_path)[0]
        self.assertEqual("TestWriter#28", event["agent_instance_id"])

    def test_extracts_bare_agent_name_hash_number_from_prompt(self):
        """Bare AgentName#Number form is used when no structured key is present."""
        prompt = "Execute the task for DataProcessor#5. Here is the context..."
        ctx = _make_start_ctx(
            self.tmp.name, self.run_id,
            agent_id="harness-b2c3", agent_type="DataProcessor",
            agent_prompt=prompt,
        )
        invocation.handle_subagent_start(ctx)
        event_path = self._paths().invocation_events(self.run_id, "DataProcessor#5")
        self.assertTrue(event_path.exists())

    def test_fallback_uses_unknown_agent_when_prompt_absent(self):
        """When agent_prompt is absent and extraction fails, fallback is 'unknown-agent'."""
        ctx = _make_start_ctx(
            self.tmp.name, self.run_id,
            agent_id="harness-c3d4", agent_type="RequirementsRefinement",
        )
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs()
        self.assertEqual(1, len(dirs))
        self.assertEqual("unknown-agent", dirs[0].name,
                         f"Expected 'unknown-agent', got {dirs[0].name!r}")

    def test_fallback_uses_unknown_agent_when_neither_prompt_nor_type_present(self):
        """When both agent_prompt and agent_type are absent, fallback is 'unknown-agent'."""
        ctx = _make_start_ctx(
            self.tmp.name, self.run_id,
            agent_id="harness-d4e5",
        )
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs()
        self.assertEqual(1, len(dirs))
        self.assertEqual("unknown-agent", dirs[0].name,
                         f"Expected 'unknown-agent', got {dirs[0].name!r}")

    def test_fallback_id_is_unknown_agent_when_extraction_fails(self):
        """When agent_instance_id cannot be extracted, fallback is the literal 'unknown-agent'."""
        ctx = _make_start_ctx(
            self.tmp.name, self.run_id,
            agent_id="harness-e5f6", agent_type="SomeAgent",
        )
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs()
        self.assertEqual(1, len(dirs))
        folder = dirs[0].name
        self.assertEqual("unknown-agent", folder,
                         f"Expected 'unknown-agent', got {folder!r}")

    def test_invocation_start_event_always_has_agent_instance_id(self):
        """invocation_start always carries a non-empty agent_instance_id."""
        ctx = _make_start_ctx(
            self.tmp.name, self.run_id,
            agent_id="harness-f6g7",
            # Worst case: no agent_type, no agent_prompt
        )
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs()
        self.assertEqual(1, len(dirs))
        event_file = dirs[0] / "03_events.jsonl"
        self.assertTrue(event_file.exists())
        event = _read_jsonl(event_file)[0]
        self.assertIn("agent_instance_id", event)
        self.assertTrue(event["agent_instance_id"],
                        "agent_instance_id must be non-empty")

    def test_agent_instance_id_is_never_the_harness_native_agent_id(self):
        """The harness-native agent_id must never appear as agent_instance_id."""
        harness_id = "opaque-harness-native-id-xyz"
        ctx = _make_start_ctx(
            self.tmp.name, self.run_id,
            agent_id=harness_id, agent_type="SomeAgent",
        )
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs()
        self.assertEqual(1, len(dirs))
        event_file = dirs[0] / "03_events.jsonl"
        event = _read_jsonl(event_file)[0]
        self.assertNotEqual(harness_id, event["agent_instance_id"])

    def test_agent_instance_id_from_mapping_used_at_stop(self):
        """SubagentStop resolves agent_instance_id via the mapping, not re-extraction."""
        _register_mapping(
            self.tmp.name, self.run_id, "harness-g7h8", "Researcher#3", "Researcher",
        )
        ctx = _make_stop_ctx(
            self.tmp.name, self.run_id,
            agent_id="harness-g7h8", agent_type="Researcher",
            last_assistant_message='{"status_code": "SUCCESS", "status_message": "Done."}',
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self._paths().invocation_events(self.run_id, "Researcher#3")
        self.assertTrue(event_path.exists(), "invocation_end must land in the mapped folder")


# ---------------------------------------------------------------------------
# T4.2  invocation_start event and initial user turn
# ---------------------------------------------------------------------------

class TestInvocationStartEventAndTurn(unittest.TestCase):
    """handle_subagent_start emits invocation_start and an initial user turn,
    both directed to the invocation's own 03_events.jsonl."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _run(self, **kwargs):
        ctx = _make_start_ctx(self.tmp.name, **kwargs)
        invocation.handle_subagent_start(ctx)
        return ctx

    def _events(self, agent_instance_id):
        return _read_jsonl(
            self.paths.invocation_events(_RUN_ID, agent_instance_id)
        )

    def test_invocation_start_event_is_written(self):
        prompt = '{"agent_instance_id": "PlanWriter#1"}'
        self._run(agent_id="aid-1", agent_type="PlanWriter", agent_prompt=prompt)
        events = self._events("PlanWriter#1")
        names = [e["event"] for e in events]
        self.assertIn("invocation_start", names)

    def test_invocation_start_has_required_agent_instance_id(self):
        prompt = '{"agent_instance_id": "PlanWriter#2"}'
        self._run(agent_id="aid-2", agent_type="PlanWriter", agent_prompt=prompt)
        event = next(e for e in self._events("PlanWriter#2") if e["event"] == "invocation_start")
        self.assertIn("agent_instance_id", event)
        self.assertEqual("PlanWriter#2", event["agent_instance_id"])

    def test_invocation_start_has_required_envelope_fields(self):
        prompt = '{"agent_instance_id": "PlanWriter#3"}'
        self._run(agent_id="aid-3", agent_type="PlanWriter", agent_prompt=prompt)
        event = next(e for e in self._events("PlanWriter#3") if e["event"] == "invocation_start")
        for field in ("schema_version", "event", "timestamp", "harness"):
            self.assertIn(field, event, f"Required envelope field '{field}' absent")

    def test_invocation_start_has_agent_type_when_present(self):
        prompt = '{"agent_instance_id": "ReviewAgent#1"}'
        self._run(agent_id="aid-4", agent_type="ReviewAgent", agent_prompt=prompt)
        event = next(e for e in self._events("ReviewAgent#1") if e["event"] == "invocation_start")
        self.assertEqual("ReviewAgent", event.get("agent_type"))

    def test_invocation_start_has_full_prompt_in_prompt_field(self):
        big_prompt = '{"agent_instance_id": "PlanWriter#5"}\n' + "x" * 50_000
        self._run(agent_id="aid-5", agent_type="PlanWriter", agent_prompt=big_prompt)
        event = next(e for e in self._events("PlanWriter#5") if e["event"] == "invocation_start")
        self.assertEqual(big_prompt, event.get("prompt"),
                         "prompt field must be the full untruncated text")

    def test_initial_user_turn_is_written(self):
        prompt = '{"agent_instance_id": "PlanWriter#6"}'
        self._run(agent_id="aid-6", agent_type="PlanWriter", agent_prompt=prompt)
        events = self._events("PlanWriter#6")
        names = [e["event"] for e in events]
        self.assertIn("turn", names)

    def test_initial_turn_has_role_user(self):
        prompt = '{"agent_instance_id": "PlanWriter#7"}'
        self._run(agent_id="aid-7", agent_type="PlanWriter", agent_prompt=prompt)
        turn = next(e for e in self._events("PlanWriter#7") if e["event"] == "turn")
        self.assertEqual("user", turn["role"])

    def test_initial_turn_content_is_full_prompt(self):
        prompt = '{"agent_instance_id": "PlanWriter#8"}\nFull task instructions here.'
        self._run(agent_id="aid-8", agent_type="PlanWriter", agent_prompt=prompt)
        turn = next(e for e in self._events("PlanWriter#8") if e["event"] == "turn")
        self.assertEqual(prompt, turn["content"],
                         "turn content must be the full untruncated prompt")

    def test_no_initial_turn_when_prompt_absent(self):
        """When agent_prompt is absent, no user turn is written (content is required)."""
        ctx = _make_start_ctx(
            self.tmp.name, agent_id="aid-9", agent_type="SomeAgent"
        )
        invocation.handle_subagent_start(ctx)
        dirs = [
            d for d in self.paths.run_root(_RUN_ID).iterdir()
            if d.is_dir() and not d.name.startswith(".")
        ]
        self.assertEqual(1, len(dirs))
        event_file = dirs[0] / "03_events.jsonl"
        events = _read_jsonl(event_file)
        turn_events = [e for e in events if e.get("event") == "turn"]
        self.assertEqual(0, len(turn_events),
                         "No turn should be emitted when agent_prompt is absent")

    def test_invocation_start_is_written_before_turn(self):
        """invocation_start appears before the initial turn in the event stream."""
        prompt = '{"agent_instance_id": "PlanWriter#10"}'
        self._run(agent_id="aid-10", agent_type="PlanWriter", agent_prompt=prompt)
        events = self._events("PlanWriter#10")
        event_names = [e["event"] for e in events]
        self.assertIn("invocation_start", event_names)
        self.assertIn("turn", event_names)
        start_idx = event_names.index("invocation_start")
        turn_idx = event_names.index("turn")
        self.assertLess(start_idx, turn_idx)

    def test_events_written_to_invocation_stream_not_orchestrator(self):
        """SubagentStart must not write any event to 00_orchestrator_events.jsonl."""
        prompt = '{"agent_instance_id": "PlanWriter#11"}'
        self._run(agent_id="aid-11", agent_type="PlanWriter", agent_prompt=prompt)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertFalse(
            orc_path.exists(),
            "SubagentStart must not write to the orchestrator events stream",
        )

    def test_handler_routes_to_unknown_run_when_run_id_is_none(self):
        """When ctx.run_id is None, events are routed to the unknown-run/ bucket."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": _SESSION_ID,
            "agent_id": "aid-none",
            "agent_type": "SomeAgent",
            "agent_prompt": '{"agent_instance_id": "SomeAgent#1"}',
        }
        workspace_root = pathlib.Path(self.tmp.name)
        paths = core.build_paths(workspace_root)
        ctx = core.HookContext(payload, workspace_root, paths, _TS)
        ctx.run_id = None  # Explicitly unresolved — routes to unknown-run/
        invocation.handle_subagent_start(ctx)
        # effective_run_id returns "unknown-run" when ctx.run_id is None
        unknown_run_dir = paths.run_root("unknown-run")
        self.assertTrue(unknown_run_dir.exists(),
                        "Events must be routed to unknown-run/ when ctx.run_id is None")

    def test_handler_does_nothing_when_agent_id_is_none(self):
        """No files are written when agent_id is absent (not a subagent context)."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": _SESSION_ID,
            # No agent_id
        }
        workspace_root = pathlib.Path(self.tmp.name)
        paths = core.build_paths(workspace_root)
        ctx = core.HookContext(payload, workspace_root, paths, _TS)
        ctx.run_id = _RUN_ID
        invocation.handle_subagent_start(ctx)
        self.assertFalse(
            self.paths.run_root(_RUN_ID).exists(),
            "No files should be created when agent_id is absent",
        )


# ---------------------------------------------------------------------------
# T5.2 / T5.3  handle_subagent_start emits usage_record from the orchestrator
# transcript (ctx.transcript_path), routed to the orchestrator stream.
# ---------------------------------------------------------------------------

class TestSubagentStartUsageEmission(unittest.TestCase):
    """handle_subagent_start reads ctx.transcript_path (the orchestrator
    transcript, not the subagent's) and emits usage_record to the
    orchestrator stream -- never to the invocation stream it also writes to."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _run(self, **kwargs):
        ctx = _make_start_ctx(self.tmp.name, **kwargs)
        invocation.handle_subagent_start(ctx)
        return ctx

    def test_usage_records_written_to_orchestrator_stream(self):
        transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_start", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])
        prompt = '{"agent_instance_id": "PlanWriter#20"}'
        self._run(agent_id="aid-20", agent_type="PlanWriter", agent_prompt=prompt,
                   transcript_path=transcript_path)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertTrue(orc_path.exists(), "usage_record must be written to the orchestrator stream")
        events = _read_jsonl(orc_path)
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))

    def test_usage_records_not_written_to_invocation_stream(self):
        transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_start2", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])
        prompt = '{"agent_instance_id": "PlanWriter#21"}'
        self._run(agent_id="aid-21", agent_type="PlanWriter", agent_prompt=prompt,
                   transcript_path=transcript_path)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "PlanWriter#21"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events))

    def test_no_usage_records_when_transcript_path_absent(self):
        prompt = '{"agent_instance_id": "PlanWriter#22"}'
        self._run(agent_id="aid-22", agent_type="PlanWriter", agent_prompt=prompt)
        # No orchestrator events file at all -- nothing else writes there.
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        if orc_path.exists():
            events = _read_jsonl(orc_path)
            self.assertEqual(0, len([e for e in events if e["event"] == "usage_record"]))


# ---------------------------------------------------------------------------
# T4.3  invocation_end event and final assistant turn
# ---------------------------------------------------------------------------

class TestInvocationEndEventAndTurn(unittest.TestCase):
    """handle_subagent_stop emits invocation_end and a final assistant turn,
    both directed to the invocation's own 03_events.jsonl."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _setup_mapping(self, agent_id, agent_instance_id, agent_type=None):
        _register_mapping(self.tmp.name, _RUN_ID, agent_id, agent_instance_id, agent_type)

    def _run_stop(self, agent_id, **kwargs):
        ctx = _make_stop_ctx(self.tmp.name, agent_id=agent_id, **kwargs)
        invocation.handle_subagent_stop(ctx)
        return ctx

    def _events(self, agent_instance_id):
        return _read_jsonl(
            self.paths.invocation_events(_RUN_ID, agent_instance_id)
        )

    def test_invocation_end_event_is_written(self):
        self._setup_mapping("aid-1", "Reviewer#1", "Reviewer")
        self._run_stop("aid-1", agent_type="Reviewer",
                       last_assistant_message="Response text.")
        events = self._events("Reviewer#1")
        self.assertIn("invocation_end", [e["event"] for e in events])

    def test_invocation_end_has_required_agent_instance_id(self):
        self._setup_mapping("aid-2", "Reviewer#2", "Reviewer")
        self._run_stop("aid-2", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}')
        event = next(e for e in self._events("Reviewer#2") if e["event"] == "invocation_end")
        self.assertIn("agent_instance_id", event)
        self.assertEqual("Reviewer#2", event["agent_instance_id"])

    def test_invocation_end_has_required_envelope_fields(self):
        self._setup_mapping("aid-3", "Reviewer#3", "Reviewer")
        self._run_stop("aid-3", agent_type="Reviewer",
                       last_assistant_message="Done.")
        event = next(e for e in self._events("Reviewer#3") if e["event"] == "invocation_end")
        for field in ("schema_version", "event", "timestamp", "harness"):
            self.assertIn(field, event)

    def test_invocation_end_has_full_response_in_response_field(self):
        """response field must be the full untruncated last_assistant_message."""
        big_response = '{"status_code": "SUCCESS"}\n' + "y" * 50_000
        self._setup_mapping("aid-4", "Reviewer#4", "Reviewer")
        self._run_stop("aid-4", agent_type="Reviewer", last_assistant_message=big_response)
        event = next(e for e in self._events("Reviewer#4") if e["event"] == "invocation_end")
        self.assertEqual(big_response, event.get("response"))

    def test_invocation_end_has_status_code_from_last_assistant_message(self):
        """status_code on invocation_end is extracted from the last_assistant_message."""
        self._setup_mapping("aid-5", "Reviewer#5", "Reviewer")
        self._run_stop(
            "aid-5", agent_type="Reviewer",
            last_assistant_message='{"status_code": "BLOCKED", "error_code": "E101"}',
        )
        event = next(e for e in self._events("Reviewer#5") if e["event"] == "invocation_end")
        self.assertEqual("BLOCKED", event.get("status_code"),
                         "status_code must be extracted from last_assistant_message")

    def test_invocation_end_status_code_absent_when_no_structured_block(self):
        """status_code is omitted when last_assistant_message has no status block."""
        self._setup_mapping("aid-6", "Reviewer#6", "Reviewer")
        self._run_stop("aid-6", agent_type="Reviewer",
                       last_assistant_message="Task is done with SUCCESS in mind.")
        event = next(e for e in self._events("Reviewer#6") if e["event"] == "invocation_end")
        self.assertNotIn("status_code", event,
                         "status_code must be absent when no structured block is found")

    def test_invocation_end_has_model_from_subagent_transcript(self):
        """model field on invocation_end comes from the subagent transcript."""
        transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 100},
            }},
        ])
        self._setup_mapping("aid-7", "Reviewer#7", "Reviewer")
        self._run_stop(
            "aid-7", agent_type="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
            agent_transcript_path=transcript_path,
        )
        event = next(e for e in self._events("Reviewer#7") if e["event"] == "invocation_end")
        self.assertEqual("claude-opus-4-5", event.get("model"),
                         "model must be read from the subagent transcript")

    def test_invocation_end_has_token_usage_from_subagent_transcript(self):
        """token_usage on invocation_end comes from the subagent transcript."""
        transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 200, "output_tokens": 75},
            }},
        ])
        self._setup_mapping("aid-8", "Reviewer#8", "Reviewer")
        self._run_stop(
            "aid-8", agent_type="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
            agent_transcript_path=transcript_path,
        )
        event = next(e for e in self._events("Reviewer#8") if e["event"] == "invocation_end")
        self.assertIn("token_usage", event,
                      "token_usage must be present when the transcript supplies usage data")
        self.assertEqual(200, event["token_usage"].get("input_tokens"))
        self.assertEqual(75, event["token_usage"].get("output_tokens"))

    def test_invocation_end_model_absent_when_transcript_unreadable(self):
        """model is omitted when the transcript path is missing or unreadable."""
        self._setup_mapping("aid-9", "Reviewer#9", "Reviewer")
        self._run_stop(
            "aid-9", agent_type="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
            agent_transcript_path="/nonexistent/transcript.jsonl",
        )
        event = next(e for e in self._events("Reviewer#9") if e["event"] == "invocation_end")
        self.assertNotIn("model", event)

    def test_invocation_end_token_usage_absent_when_transcript_unreadable(self):
        """token_usage is omitted when the transcript path is missing."""
        self._setup_mapping("aid-10", "Reviewer#10", "Reviewer")
        self._run_stop(
            "aid-10", agent_type="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
            agent_transcript_path="/nonexistent/transcript.jsonl",
        )
        event = next(e for e in self._events("Reviewer#10") if e["event"] == "invocation_end")
        self.assertNotIn("token_usage", event)

    def test_final_assistant_turn_is_written(self):
        self._setup_mapping("aid-11", "Reviewer#11", "Reviewer")
        self._run_stop("aid-11", agent_type="Reviewer",
                       last_assistant_message="Final response.")
        events = self._events("Reviewer#11")
        self.assertIn("turn", [e["event"] for e in events])

    def test_final_turn_has_role_assistant(self):
        self._setup_mapping("aid-12", "Reviewer#12", "Reviewer")
        self._run_stop("aid-12", agent_type="Reviewer",
                       last_assistant_message="My final answer.")
        turn = next(e for e in self._events("Reviewer#12") if e["event"] == "turn")
        self.assertEqual("assistant", turn["role"])

    def test_final_turn_content_is_full_last_assistant_message(self):
        """turn content must be the full untruncated last_assistant_message."""
        big_msg = '{"status_code": "SUCCESS"}\n' + "z" * 50_000
        self._setup_mapping("aid-13", "Reviewer#13", "Reviewer")
        self._run_stop("aid-13", agent_type="Reviewer", last_assistant_message=big_msg)
        turn = next(e for e in self._events("Reviewer#13") if e["event"] == "turn")
        self.assertEqual(big_msg, turn["content"])

    def test_final_turn_has_model_from_transcript(self):
        """The final assistant turn also carries model from the subagent transcript."""
        transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "model": "claude-sonnet-4-6",
                "usage": {"input_tokens": 50},
            }},
        ])
        self._setup_mapping("aid-14", "Reviewer#14", "Reviewer")
        self._run_stop(
            "aid-14", agent_type="Reviewer",
            last_assistant_message="Done.",
            agent_transcript_path=transcript_path,
        )
        turn = next(e for e in self._events("Reviewer#14") if e["event"] == "turn")
        self.assertEqual("claude-sonnet-4-6", turn.get("model"))

    def test_no_final_turn_when_last_assistant_message_absent(self):
        """When last_assistant_message is absent, no assistant turn is written."""
        self._setup_mapping("aid-15", "Reviewer#15", "Reviewer")
        ctx = _make_stop_ctx(self.tmp.name, agent_id="aid-15")
        invocation.handle_subagent_stop(ctx)
        events = self._events("Reviewer#15")
        turn_events = [e for e in events if e.get("event") == "turn"]
        self.assertEqual(0, len(turn_events),
                         "No turn should be emitted when last_assistant_message is absent")

    def test_invocation_end_written_before_final_turn(self):
        """invocation_end appears before the final turn in the event stream."""
        self._setup_mapping("aid-16", "Reviewer#16", "Reviewer")
        self._run_stop("aid-16", agent_type="Reviewer",
                       last_assistant_message="Response.")
        events = self._events("Reviewer#16")
        names = [e["event"] for e in events]
        self.assertIn("invocation_end", names)
        self.assertIn("turn", names)
        end_idx = names.index("invocation_end")
        turn_idx = names.index("turn")
        self.assertLess(end_idx, turn_idx)

    def test_stop_events_not_written_to_orchestrator_stream(self):
        """SubagentStop must not write any event to 00_orchestrator_events.jsonl."""
        self._setup_mapping("aid-17", "Reviewer#17", "Reviewer")
        self._run_stop("aid-17", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}')
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertFalse(
            orc_path.exists(),
            "SubagentStop must not write to the orchestrator events stream",
        )

    def test_only_one_turn_emitted_per_stop_regardless_of_transcript_records(self):
        """SubagentStop emits exactly one final assistant turn even when the subagent
        transcript contains multiple assistant records representing intermediate turns.
        Parsing agent_transcript_path for intermediate turn reconstruction is
        explicitly forbidden by design; only last_assistant_message produces the turn."""
        multi_record_path = _write_transcript_file(
            str(pathlib.Path(self.tmp.name) / "multi_rec_transcript"),
            [
                {"type": "assistant", "message": {
                    "model": "m1", "usage": {"input_tokens": 10},
                }},
                {"type": "tool_result", "content": "intermediate tool output"},
                {"type": "assistant", "message": {
                    "model": "m2", "usage": {"input_tokens": 20},
                }},
                {"type": "tool_result", "content": "another tool output"},
                {"type": "assistant", "message": {
                    "model": "m3", "usage": {"input_tokens": 30},
                }},
            ],
        )
        self._setup_mapping("aid-18", "Reviewer#18", "Reviewer")
        self._run_stop(
            "aid-18",
            agent_type="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
            agent_transcript_path=multi_record_path,
        )
        events = self._events("Reviewer#18")
        turn_events = [e for e in events if e.get("event") == "turn"]
        self.assertEqual(
            1, len(turn_events),
            "SubagentStop must emit exactly one final turn, not one per assistant "
            "record in the subagent transcript",
        )


# ---------------------------------------------------------------------------
# T5.2 / T5.3  handle_subagent_stop emits usage_record from BOTH transcripts
# it has access to: the agent transcript (routed to the invocation stream)
# and the orchestrator transcript (routed to the orchestrator stream) --
# each record reaching exactly one of the two.
# ---------------------------------------------------------------------------

class TestSubagentStopUsageEmission(unittest.TestCase):
    """handle_subagent_stop's agent-transcript read lands on the invocation
    stream; its orchestrator-transcript read lands on the orchestrator
    stream. Neither leaks into the other."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _setup_mapping(self, agent_id, agent_instance_id, agent_type=None):
        _register_mapping(self.tmp.name, _RUN_ID, agent_id, agent_instance_id, agent_type)

    def _run_stop(self, agent_id, **kwargs):
        ctx = _make_stop_ctx(self.tmp.name, agent_id=agent_id, **kwargs)
        invocation.handle_subagent_stop(ctx)
        return ctx

    def test_agent_transcript_usage_records_land_on_invocation_stream(self):
        agent_transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_agent", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])
        self._setup_mapping("aid-30", "Reviewer#30", "Reviewer")
        self._run_stop("aid-30", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       agent_transcript_path=agent_transcript_path)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "Reviewer#30"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))
        self.assertEqual("Reviewer#30", usage_events[0].get("agent_instance_id"))
        self.assertEqual("agent_transcript", usage_events[0].get("source"))

    def test_agent_transcript_usage_records_do_not_reach_orchestrator_stream(self):
        agent_transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_agent2", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])
        self._setup_mapping("aid-31", "Reviewer#31", "Reviewer")
        self._run_stop("aid-31", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       agent_transcript_path=agent_transcript_path)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        if orc_path.exists():
            events = _read_jsonl(orc_path)
            self.assertEqual(0, len([e for e in events if e["event"] == "usage_record"]))

    def test_orchestrator_transcript_usage_records_land_on_orchestrator_stream(self):
        orch_transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_orch", "model": "claude-opus-4-5",
                "usage": {"output_tokens": 20},
            }},
        ])
        self._setup_mapping("aid-32", "Reviewer#32", "Reviewer")
        self._run_stop("aid-32", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       transcript_path=orch_transcript_path)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertTrue(orc_path.exists(), "usage_record must be written to the orchestrator stream")
        events = _read_jsonl(orc_path)
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))
        self.assertNotIn("agent_instance_id", usage_events[0])
        self.assertEqual("orchestrator_transcript", usage_events[0].get("source"))

    def test_orchestrator_transcript_usage_records_do_not_reach_invocation_stream(self):
        orch_transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_orch2", "model": "claude-opus-4-5",
                "usage": {"output_tokens": 20},
            }},
        ])
        self._setup_mapping("aid-33", "Reviewer#33", "Reviewer")
        self._run_stop("aid-33", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       transcript_path=orch_transcript_path)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "Reviewer#33"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events))

    def test_both_transcripts_present_emit_to_both_distinct_streams(self):
        """A single SubagentStop firing carrying both transcript paths
        emits its usage records to each stream independently -- never
        crossing over -- satisfying the single-stream-per-record invariant."""
        agent_transcript_path = _write_transcript_file(
            pathlib.Path(self.tmp.name) / "agent", [
                {"type": "assistant", "message": {
                    "id": "msg_both_agent", "model": "claude-opus-4-5",
                    "usage": {"input_tokens": 5},
                }},
            ])
        orch_transcript_path = _write_transcript_file(
            pathlib.Path(self.tmp.name) / "orch", [
                {"type": "assistant", "message": {
                    "id": "msg_both_orch", "model": "claude-opus-4-5",
                    "usage": {"output_tokens": 9},
                }},
            ])
        self._setup_mapping("aid-34", "Reviewer#34", "Reviewer")
        self._run_stop("aid-34", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       agent_transcript_path=agent_transcript_path,
                       transcript_path=orch_transcript_path)
        invocation_events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "Reviewer#34"))
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        orch_events = _read_jsonl(orc_path) if orc_path.exists() else []
        invocation_usage_ids = {e["record_id"] for e in invocation_events
                                 if e["event"] == "usage_record"}
        orch_usage_ids = {e["record_id"] for e in orch_events
                           if e["event"] == "usage_record"}
        self.assertEqual({"msg_both_agent"}, invocation_usage_ids)
        self.assertEqual({"msg_both_orch"}, orch_usage_ids)
        self.assertEqual(set(), invocation_usage_ids & orch_usage_ids)

    def test_repeated_stop_firings_do_not_error_or_duplicate(self):
        """Two SubagentStop firings against the same transcripts are tolerated:
        no exception is raised and the usage record is emitted exactly once,
        not re-emitted on the second identical firing."""
        agent_transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_repeat", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 5},
            }},
        ])
        self._setup_mapping("aid-35", "Reviewer#35", "Reviewer")
        self._run_stop("aid-35", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       agent_transcript_path=agent_transcript_path)
        self._run_stop("aid-35", agent_type="Reviewer",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       agent_transcript_path=agent_transcript_path)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "Reviewer#35"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))


# ---------------------------------------------------------------------------
# T5.2 / A5  Quarantine routing: an unmapped-but-genuine SubagentStop's
# agent-transcript usage records follow the invocation_end/output/raw
# destination to the quarantine location, never the ordinary invocation dir.
# ---------------------------------------------------------------------------

class TestSubagentStopQuarantineUsageEmission(unittest.TestCase):
    """Where SubagentStop is quarantined, its agent-transcript usage_record
    events go to the quarantine events file under the same single-stream
    rule -- the destination changes, the one-stream-per-record property
    does not."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _run_stop(self, agent_id, **kwargs):
        ctx = _make_stop_ctx(self.tmp.name, agent_id=agent_id, **kwargs)
        invocation.handle_subagent_stop(ctx)
        return ctx

    def test_quarantined_agent_transcript_usage_records_land_in_quarantine_events(self):
        agent_transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_quarantine", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])
        self._run_stop("aid-quarantine-usage-1", last_assistant_message="Genuine but unmapped.",
                       agent_transcript_path=agent_transcript_path)
        events = _read_jsonl(self.paths.quarantine_events(_RUN_ID, "aid-quarantine-usage-1"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))

    def test_quarantined_agent_transcript_usage_records_absent_from_ordinary_invocation_dir(self):
        agent_transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_quarantine2", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])
        self._run_stop("aid-quarantine-usage-2", last_assistant_message="Genuine but unmapped.",
                       agent_transcript_path=agent_transcript_path)
        # The unmapped agent_id has no real agent_instance_id to look up an
        # ordinary invocation dir under, so the only place usage_record can
        # legitimately appear is the quarantine destination.
        quarantine_events = _read_jsonl(
            self.paths.quarantine_events(_RUN_ID, "aid-quarantine-usage-2")
        )
        record_ids = {e["record_id"] for e in quarantine_events
                      if e["event"] == "usage_record"}
        self.assertIn("msg_quarantine2", record_ids)


# ---------------------------------------------------------------------------
# T4.4  status_code extraction
# ---------------------------------------------------------------------------

class TestStatusCodeExtraction(unittest.TestCase):
    """extract_status_code parses the MOSAIC status block conservatively, matching
    only structured key-value occurrences against the closed six-value enum.
    Free prose, unrecognized values, and absent blocks all return None."""

    def test_extracts_success(self):
        msg = '{"agent_instance_id": "A#1", "status_code": "SUCCESS", "status_message": "ok"}'
        self.assertEqual("SUCCESS", invocation.extract_status_code(msg))

    def test_extracts_completed_needs_action(self):
        msg = '{"status_code": "COMPLETED_NEEDS_ACTION", "status_message": "found issues"}'
        self.assertEqual("COMPLETED_NEEDS_ACTION", invocation.extract_status_code(msg))

    def test_extracts_partially_done(self):
        msg = '{"status_code": "PARTIALLY_DONE", "status_message": "stopping early"}'
        self.assertEqual("PARTIALLY_DONE", invocation.extract_status_code(msg))

    def test_extracts_needs_clarification(self):
        msg = '{"status_code": "NEEDS_CLARIFICATION", "status_message": "unclear"}'
        self.assertEqual("NEEDS_CLARIFICATION", invocation.extract_status_code(msg))

    def test_extracts_capability_exceeded(self):
        msg = '{"status_code": "CAPABILITY_EXCEEDED", "status_message": "too hard"}'
        self.assertEqual("CAPABILITY_EXCEEDED", invocation.extract_status_code(msg))

    def test_extracts_blocked(self):
        msg = '{"status_code": "BLOCKED", "error_code": "E101"}'
        self.assertEqual("BLOCKED", invocation.extract_status_code(msg))

    def test_last_structured_occurrence_wins(self):
        """When multiple structured status_code occurrences exist, the last wins."""
        msg = (
            'First block: {"status_code": "SUCCESS", "status_message": "early"}\n'
            'Some intervening text.\n'
            'Final block: {"status_code": "BLOCKED", "error_code": "E501"}'
        )
        self.assertEqual("BLOCKED", invocation.extract_status_code(msg))

    def test_free_prose_mention_of_success_is_not_a_match(self):
        """A bare status name in prose must not be extracted."""
        msg = "The operation completed with SUCCESS in every aspect."
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_free_prose_mention_of_blocked_is_not_a_match(self):
        msg = "This request is BLOCKED by the policy."
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_unrecognized_status_value_returns_none(self):
        """A value outside the closed enum must be rejected, not returned."""
        msg = '{"status_code": "CUSTOM_STATUS", "status_message": "something"}'
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_empty_status_code_value_returns_none(self):
        msg = '{"status_code": "", "status_message": "empty"}'
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_no_status_block_returns_none(self):
        msg = "The agent ran the task and produced an output with no status block."
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_none_input_returns_none(self):
        self.assertIsNone(invocation.extract_status_code(None))

    def test_empty_string_returns_none(self):
        self.assertIsNone(invocation.extract_status_code(""))

    def test_never_defaults_to_success_when_no_block(self):
        """The function must never invent SUCCESS as a default."""
        result = invocation.extract_status_code("Task completed.")
        self.assertNotEqual("SUCCESS", result,
                            "extract_status_code must never default to SUCCESS")

    def test_never_raises_on_none(self):
        try:
            invocation.extract_status_code(None)
        except Exception as e:
            self.fail(f"extract_status_code raised on None: {e}")

    def test_never_raises_on_malformed_json(self):
        try:
            invocation.extract_status_code('{"status_code": broken json {{{}')
        except Exception as e:
            self.fail(f"extract_status_code raised on malformed JSON: {e}")

    def test_never_raises_on_very_long_input(self):
        try:
            invocation.extract_status_code("content " * 10_000 + '{"status_code": "SUCCESS"}')
        except Exception as e:
            self.fail(f"extract_status_code raised on long input: {e}")

    def test_never_raises_on_control_characters(self):
        try:
            invocation.extract_status_code("\x00\x01\x02status_code SUCCESS\xff")
        except Exception as e:
            self.fail(f"extract_status_code raised on control chars: {e}")

    def test_status_code_absent_means_key_truly_absent_not_null(self):
        """None return means no key emitted; the serializer will drop it."""
        result = invocation.extract_status_code("No structured block here.")
        # assertIsNone covers both None and the distinction from ""
        self.assertIsNone(result)


# ---------------------------------------------------------------------------
# T4.5  token usage and model from subagent transcript
# ---------------------------------------------------------------------------

class TestTranscriptReading(unittest.TestCase):
    """read_last_assistant_facts reads model and token_usage from the LAST
    assistant record in a Claude Code transcript JSONL, defensively."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _write(self, records):
        return _write_transcript_file(str(self.tmp_path), records)

    def test_returns_model_from_last_assistant_record(self):
        path = self._write([
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("claude-opus-4-5", facts.model)

    def test_returns_model_from_last_of_multiple_assistant_records(self):
        """The LAST assistant record is used, not an earlier one."""
        path = self._write([
            {"type": "assistant", "message": {"model": "first-model", "usage": {}}},
            {"type": "tool_result", "content": "intermediate"},
            {"type": "assistant", "message": {
                "model": "last-model",
                "usage": {"input_tokens": 100},
            }},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("last-model", facts.model)

    def test_returns_input_tokens_from_message_usage(self):
        path = self._write([
            {"type": "assistant", "message": {
                "model": "m", "usage": {"input_tokens": 250},
            }},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual(250, facts.token_usage["input_tokens"])

    def test_returns_output_tokens_from_message_usage(self):
        path = self._write([
            {"type": "assistant", "message": {
                "model": "m", "usage": {"output_tokens": 80},
            }},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual(80, facts.token_usage["output_tokens"])

    def test_maps_cache_read_input_tokens_to_cache_read_tokens(self):
        path = self._write([
            {"type": "assistant", "message": {
                "model": "m", "usage": {"cache_read_input_tokens": 30},
            }},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIn("cache_read_tokens", facts.token_usage)
        self.assertEqual(30, facts.token_usage["cache_read_tokens"])
        self.assertNotIn("cache_read_input_tokens", facts.token_usage)

    def test_maps_cache_creation_input_tokens_to_cache_creation_tokens(self):
        path = self._write([
            {"type": "assistant", "message": {
                "model": "m", "usage": {"cache_creation_input_tokens": 15},
            }},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIn("cache_creation_tokens", facts.token_usage)
        self.assertEqual(15, facts.token_usage["cache_creation_tokens"])
        self.assertNotIn("cache_creation_input_tokens", facts.token_usage)

    def test_original_harness_key_names_do_not_appear_in_output(self):
        """The source key names must not leak into the returned token_usage dict."""
        path = self._write([
            {"type": "assistant", "message": {
                "model": "m",
                "usage": {
                    "input_tokens": 100,
                    "output_tokens": 50,
                    "cache_read_input_tokens": 20,
                    "cache_creation_input_tokens": 10,
                },
            }},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertNotIn("cache_read_input_tokens", facts.token_usage)
        self.assertNotIn("cache_creation_input_tokens", facts.token_usage)

    def test_token_usage_is_none_when_no_sub_field_resolves(self):
        """token_usage must be None (not an empty dict) when no field resolves."""
        path = self._write([
            {"type": "assistant", "message": {"model": "m", "usage": {}}},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.token_usage)

    def test_model_is_none_when_message_model_absent(self):
        path = self._write([
            {"type": "assistant", "message": {"usage": {"input_tokens": 10}}},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.model)

    def test_model_falls_back_to_top_level_model_key(self):
        """When message.model is absent, the top-level model key is used."""
        path = self._write([
            {"type": "assistant", "model": "top-level-model",
             "message": {"usage": {"input_tokens": 10}}},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("top-level-model", facts.model)

    def test_message_model_preferred_over_top_level_model(self):
        path = self._write([
            {"type": "assistant", "model": "top-level",
             "message": {"model": "message-level", "usage": {}}},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("message-level", facts.model)

    def test_unparseable_lines_are_discarded_not_fatal(self):
        """A truncated or invalid line is skipped; the next valid record is used."""
        path = self.tmp_path / "partial.jsonl"
        path.write_text(
            "not valid json at all\n"
            '{"type": "assistant", "message": {"model": "valid-model",'
            ' "usage": {"input_tokens": 5}}}\n',
            encoding="utf-8",
        )
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("valid-model", facts.model)

    def test_missing_file_returns_none_model(self):
        facts = transcript.read_last_assistant_facts("/nonexistent/path/transcript.jsonl")
        self.assertIsNone(facts.model)

    def test_missing_file_returns_none_token_usage(self):
        facts = transcript.read_last_assistant_facts("/nonexistent/path/transcript.jsonl")
        self.assertIsNone(facts.token_usage)

    def test_none_path_returns_none_model(self):
        facts = transcript.read_last_assistant_facts(None)
        self.assertIsNone(facts.model)

    def test_none_path_returns_none_token_usage(self):
        facts = transcript.read_last_assistant_facts(None)
        self.assertIsNone(facts.token_usage)

    def test_file_with_no_assistant_record_returns_none_facts(self):
        path = self._write([
            {"type": "user", "content": "hello"},
            {"type": "tool_result", "content": "done"},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)

    def test_each_token_sub_field_is_independently_optional(self):
        """Only input_tokens is present; other fields must be absent."""
        path = self._write([
            {"type": "assistant", "message": {
                "model": "m", "usage": {"input_tokens": 100},
            }},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIn("input_tokens", facts.token_usage)
        self.assertNotIn("output_tokens", facts.token_usage)
        self.assertNotIn("cache_read_tokens", facts.token_usage)
        self.assertNotIn("cache_creation_tokens", facts.token_usage)

    def test_never_raises_on_none_path(self):
        try:
            transcript.read_last_assistant_facts(None)
        except Exception as e:
            self.fail(f"read_last_assistant_facts raised on None: {e}")

    def test_never_raises_on_missing_file(self):
        try:
            transcript.read_last_assistant_facts("/no/such/file.jsonl")
        except Exception as e:
            self.fail(f"read_last_assistant_facts raised on missing file: {e}")

    def test_never_raises_on_empty_file(self):
        path = self.tmp_path / "empty.jsonl"
        path.write_bytes(b"")
        try:
            transcript.read_last_assistant_facts(str(path))
        except Exception as e:
            self.fail(f"read_last_assistant_facts raised on empty file: {e}")


# ---------------------------------------------------------------------------
# T4.6  artifact rendering (pure function tests)
# ---------------------------------------------------------------------------

class TestArtifactRendering(unittest.TestCase):
    """render_input and render_output return markdown text; write_artifact writes
    it via atomic replace. None of the render functions perform any I/O."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _ctx(self, agent_type="TestWriter", **extra):
        return _make_start_ctx(str(self.tmp_path), agent_id="aid-art",
                               agent_type=agent_type, **extra)

    def test_render_input_returns_a_string(self):
        result = artifacts.render_input(self._ctx(), "TestWriter#28", "prompt text")
        self.assertIsInstance(result, str)

    def test_render_input_heading_contains_agent_instance_id(self):
        result = artifacts.render_input(self._ctx(), "TestWriter#28", "prompt text")
        self.assertIn("TestWriter#28", result)

    def test_render_input_contains_prompt_content_verbatim(self):
        prompt = "Execute the following task: do something very specific."
        result = artifacts.render_input(self._ctx(), "TestWriter#28", prompt)
        self.assertIn(prompt, result)

    def test_render_input_contains_harness_value(self):
        result = artifacts.render_input(self._ctx(), "TestWriter#28", "p")
        self.assertIn("claude-code", result)

    def test_render_input_contains_session_id_when_resolvable(self):
        result = artifacts.render_input(self._ctx(), "TestWriter#28", "p")
        self.assertIn(_SESSION_ID, result)

    def test_render_input_contains_run_id_when_resolvable(self):
        result = artifacts.render_input(self._ctx(), "TestWriter#28", "p")
        self.assertIn(_RUN_ID, result)

    def test_render_input_omits_rows_with_unresolvable_values(self):
        """Rows with absent/None values are omitted, not rendered as empty cells."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": _SESSION_ID,
            "agent_id": "aid-art2",
            # No agent_type
        }
        workspace_root = self.tmp_path
        paths = core.build_paths(workspace_root)
        ctx = core.HookContext(payload, workspace_root, paths, _TS)
        ctx.run_id = _RUN_ID
        result = artifacts.render_input(ctx, "TestWriter#29", "p")
        # The row should be entirely absent, not present with empty value
        self.assertNotIn("| Agent Type |  |", result)

    def test_render_input_performs_no_io(self):
        """Calling render_input must not create any files or directories."""
        before = set(self.tmp_path.rglob("*"))
        artifacts.render_input(self._ctx(), "TestWriter#28", "prompt")
        after = set(self.tmp_path.rglob("*"))
        self.assertEqual(before, after)

    def test_render_output_returns_a_string(self):
        result = artifacts.render_output(
            self._ctx(), "TestWriter#28", "response text", "SUCCESS", _MOCK_FACTS
        )
        self.assertIsInstance(result, str)

    def test_render_output_heading_contains_agent_instance_id(self):
        result = artifacts.render_output(
            self._ctx(), "TestWriter#28", "response", "SUCCESS", _MOCK_FACTS
        )
        self.assertIn("TestWriter#28", result)

    def test_render_output_contains_response_content_verbatim(self):
        response = "Final answer with all the details needed."
        result = artifacts.render_output(
            self._ctx(), "TestWriter#28", response, "SUCCESS", _MOCK_FACTS
        )
        self.assertIn(response, result)

    def test_render_output_contains_status_code_when_present(self):
        result = artifacts.render_output(
            self._ctx(), "TestWriter#28", "response", "BLOCKED", _MOCK_FACTS
        )
        self.assertIn("BLOCKED", result)

    def test_render_output_omits_status_code_row_when_none(self):
        result = artifacts.render_output(
            self._ctx(), "TestWriter#28", "response", None, _MOCK_EMPTY_FACTS
        )
        self.assertNotIn("Status Code", result)

    def test_render_output_performs_no_io(self):
        before = set(self.tmp_path.rglob("*"))
        artifacts.render_output(
            self._ctx(), "TestWriter#28", "resp", "SUCCESS", _MOCK_FACTS
        )
        after = set(self.tmp_path.rglob("*"))
        self.assertEqual(before, after)

    def test_write_artifact_creates_file_with_given_text(self):
        path = self.tmp_path / "sub" / "01_input.md"
        text = "# Invocation Input: TestWriter#1\n\nContent here."
        result = artifacts.write_artifact(path, text)
        self.assertTrue(result)
        self.assertTrue(path.exists())
        self.assertEqual(text, path.read_text(encoding="utf-8"))

    def test_write_artifact_creates_parent_directories(self):
        path = self.tmp_path / "deep" / "nested" / "01_input.md"
        artifacts.write_artifact(path, "content")
        self.assertTrue(path.exists())

    def test_write_artifact_returns_true_on_success(self):
        path = self.tmp_path / "01_input.md"
        result = artifacts.write_artifact(path, "text")
        self.assertTrue(result)

    def test_write_artifact_never_raises(self):
        # A file placed at the would-be parent blocks directory creation.
        blocker = self.tmp_path / "blocker"
        blocker.write_bytes(b"")
        try:
            artifacts.write_artifact(blocker / "01_input.md", "text")
        except Exception as e:
            self.fail(f"write_artifact raised: {e}")



# ---------------------------------------------------------------------------
# T4.6  per-invocation artifact files (handler integration)
# ---------------------------------------------------------------------------

class TestPerInvocationArtifactFiles(unittest.TestCase):
    """handle_subagent_start and handle_subagent_stop must produce the full
    artifact set in the invocation directory: 01_input.md, 02_output.md,
    03_events.jsonl (already covered above), 04_session.raw + 04_session.meta.json.
    SubagentStart also refreshes 00_orchestrator_session.raw."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)
        self.transcript_path = _write_transcript_file(str(self.tmp_path), [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 100},
            }},
        ])

    def tearDown(self):
        self.tmp.cleanup()

    def _run_start(self, agent_id, agent_instance_id, **kwargs):
        _register_mapping(str(self.tmp_path), _RUN_ID, agent_id, agent_instance_id)
        ctx = _make_start_ctx(
            str(self.tmp_path),
            agent_id=agent_id,
            transcript_path=self.transcript_path,
            **kwargs,
        )
        invocation.handle_subagent_start(ctx)
        return ctx

    def _run_stop(self, agent_id, agent_instance_id, agent_transcript_path=None, **kwargs):
        _register_mapping(str(self.tmp_path), _RUN_ID, agent_id, agent_instance_id)
        ctx = _make_stop_ctx(
            str(self.tmp_path),
            agent_id=agent_id,
            agent_transcript_path=agent_transcript_path or self.transcript_path,
            transcript_path=self.transcript_path,
            **kwargs,
        )
        invocation.handle_subagent_stop(ctx)
        return ctx

    def test_subagent_start_writes_01_input_md(self):
        self._run_start("aid-a1", "ArtAgent#1", agent_type="ArtAgent",
                        agent_prompt='{"agent_instance_id": "ArtAgent#1"}')
        path = self.paths.invocation_input(_RUN_ID, "ArtAgent#1")
        self.assertTrue(path.exists(), "01_input.md must be written by SubagentStart")

    def test_01_input_md_contains_agent_instance_id(self):
        self._run_start("aid-a2", "ArtAgent#2", agent_type="ArtAgent",
                        agent_prompt='{"agent_instance_id": "ArtAgent#2"} The task.')
        path = self.paths.invocation_input(_RUN_ID, "ArtAgent#2")
        content = path.read_text(encoding="utf-8")
        self.assertIn("ArtAgent#2", content)

    def test_01_input_md_contains_raw_prompt(self):
        prompt = '{"agent_instance_id": "ArtAgent#3"} Full raw prompt content here.'
        self._run_start("aid-a3", "ArtAgent#3", agent_type="ArtAgent", agent_prompt=prompt)
        path = self.paths.invocation_input(_RUN_ID, "ArtAgent#3")
        content = path.read_text(encoding="utf-8")
        self.assertIn(prompt, content)

    def test_subagent_stop_writes_02_output_md(self):
        self._run_stop("aid-b1", "ArtAgent#4",
                       last_assistant_message='{"status_code": "SUCCESS"}')
        path = self.paths.invocation_output(_RUN_ID, "ArtAgent#4")
        self.assertTrue(path.exists(), "02_output.md must be written by SubagentStop")

    def test_02_output_md_contains_agent_instance_id(self):
        self._run_stop("aid-b2", "ArtAgent#5",
                       last_assistant_message='{"status_code": "SUCCESS"}')
        path = self.paths.invocation_output(_RUN_ID, "ArtAgent#5")
        content = path.read_text(encoding="utf-8")
        self.assertIn("ArtAgent#5", content)

    def test_02_output_md_contains_raw_response(self):
        response = '{"status_code": "SUCCESS", "status_message": "Work is complete."}'
        self._run_stop("aid-b3", "ArtAgent#6", last_assistant_message=response)
        path = self.paths.invocation_output(_RUN_ID, "ArtAgent#6")
        content = path.read_text(encoding="utf-8")
        self.assertIn(response, content)

    def test_subagent_stop_exports_agent_transcript_to_raw_file(self):
        """04_session.raw must be a byte-identical copy of agent_transcript_path."""
        agent_transcript = _write_transcript_file(str(self.tmp_path / "agent"), [
            {"type": "assistant", "message": {"model": "m", "usage": {"input_tokens": 5}}}
        ])
        self._run_stop("aid-c1", "ArtAgent#7",
                       agent_transcript_path=agent_transcript,
                       last_assistant_message='{"status_code": "SUCCESS"}')
        raw_path = self.paths.invocation_raw(_RUN_ID, "ArtAgent#7")
        self.assertTrue(raw_path.exists(), "04_session.raw must be written by SubagentStop")
        original_bytes = pathlib.Path(agent_transcript).read_bytes()
        self.assertEqual(original_bytes, raw_path.read_bytes(),
                         "04_session.raw must be byte-identical to the source transcript")

    def test_subagent_stop_writes_raw_export_sidecar(self):
        """04_session.meta.json must be written beside 04_session.raw."""
        agent_transcript = _write_transcript_file(str(self.tmp_path / "agent2"), [
            {"type": "assistant", "message": {"model": "m", "usage": {"input_tokens": 5}}}
        ])
        self._run_stop("aid-c2", "ArtAgent#8",
                       agent_transcript_path=agent_transcript,
                       last_assistant_message='{"status_code": "SUCCESS"}')
        raw_path = self.paths.invocation_raw(_RUN_ID, "ArtAgent#8")
        sidecar_path = raw_path.with_suffix(".meta.json")
        self.assertTrue(sidecar_path.exists(), "04_session.meta.json must be written")

    def test_subagent_start_refreshes_orchestrator_session_raw(self):
        """SubagentStart must refresh 00_orchestrator_session.raw from transcript_path."""
        self._run_start("aid-d1", "ArtAgent#9", agent_type="ArtAgent",
                        agent_prompt='{"agent_instance_id": "ArtAgent#9"}')
        orc_raw = self.paths.orchestrator_raw(_RUN_ID)
        self.assertTrue(orc_raw.exists(),
                        "SubagentStart must refresh 00_orchestrator_session.raw")

    def test_subagent_stop_refreshes_orchestrator_session_raw(self):
        """SubagentStop must refresh 00_orchestrator_session.raw from transcript_path."""
        agent_transcript = _write_transcript_file(str(self.tmp_path / "agent3"), [
            {"type": "assistant", "message": {"model": "m", "usage": {"input_tokens": 5}}}
        ])
        self._run_stop("aid-d2", "ArtAgent#10",
                       agent_transcript_path=agent_transcript,
                       last_assistant_message='{"status_code": "SUCCESS"}')
        orc_raw = self.paths.orchestrator_raw(_RUN_ID)
        self.assertTrue(orc_raw.exists(),
                        "SubagentStop must refresh 00_orchestrator_session.raw")

    def test_orchestrator_refresh_content_matches_source_transcript(self):
        """The refreshed 00_orchestrator_session.raw is byte-identical to transcript_path."""
        self._run_start("aid-d3", "ArtAgent#11", agent_type="ArtAgent",
                        agent_prompt='{"agent_instance_id": "ArtAgent#11"}')
        orc_raw = self.paths.orchestrator_raw(_RUN_ID)
        expected_bytes = pathlib.Path(self.transcript_path).read_bytes()
        self.assertEqual(expected_bytes, orc_raw.read_bytes(),
                         "00_orchestrator_session.raw must be byte-identical to transcript_path")

    def test_no_raw_export_written_when_agent_transcript_absent(self):
        """04_session.raw is not written when agent_transcript_path is absent."""
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-e1", "ArtAgent#12")
        ctx = _make_stop_ctx(
            str(self.tmp_path),
            agent_id="aid-e1",
            transcript_path=self.transcript_path,
            last_assistant_message='{"status_code": "SUCCESS"}',
            # No agent_transcript_path
        )
        invocation.handle_subagent_stop(ctx)
        raw_path = self.paths.invocation_raw(_RUN_ID, "ArtAgent#12")
        self.assertFalse(raw_path.exists(),
                         "04_session.raw must not be written when source is absent")


# ---------------------------------------------------------------------------
# T4.7  agent_id mapping round trip and folder-name sanitization
# ---------------------------------------------------------------------------

class TestAgentIdMappingRoundTrip(unittest.TestCase):
    """put_agent_mapping persists a mapping; get_agent_mapping retrieves it;
    resolve_invocation_id returns the stored ID or the deterministic fallback.
    Instance IDs with reserved characters are sanitized in folder names but
    kept verbatim in event JSON."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_put_and_get_round_trip_returns_agent_instance_id(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "agent-x1", "TestWriter#5", "TestWriter"
        )
        mapping = runstate.get_agent_mapping(self.paths, _RUN_ID, "agent-x1")
        self.assertIsNotNone(mapping)
        self.assertEqual("TestWriter#5", mapping["agent_instance_id"])

    def test_put_and_get_round_trip_stores_agent_type(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "agent-x2", "TestWriter#6", "TestWriter"
        )
        mapping = runstate.get_agent_mapping(self.paths, _RUN_ID, "agent-x2")
        self.assertEqual("TestWriter", mapping.get("agent_type"))

    def test_put_and_get_round_trip_stores_agent_id(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "agent-x3", "TestWriter#7", "TestWriter"
        )
        mapping = runstate.get_agent_mapping(self.paths, _RUN_ID, "agent-x3")
        self.assertEqual("agent-x3", mapping.get("agent_id"))

    def test_get_returns_none_when_mapping_absent(self):
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, "nonexistent-agent-id")
        self.assertIsNone(result)

    def test_resolve_returns_stored_instance_id(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "agent-y1", "Researcher#3", "Researcher"
        )
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "agent-y1")
        self.assertEqual("Researcher#3", result)

    def test_resolve_returns_deterministic_fallback_when_mapping_absent(self):
        """unmapped_{agent_id} is returned when no mapping file exists."""
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "unmapped-agent-z9")
        self.assertEqual("unmapped_unmapped-agent-z9", result)

    def test_fallback_never_returns_none(self):
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "any-missing-agent")
        self.assertIsNotNone(result)

    def test_fallback_never_returns_empty_string(self):
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "any-missing-agent2")
        self.assertTrue(result, "resolve_invocation_id must never return an empty string")

    def test_separate_agent_ids_get_separate_mapping_files(self):
        """Each agent_id has its own file; parallel SubagentStart processes never contend."""
        runstate.put_agent_mapping(self.paths, _RUN_ID, "agent-z1", "AgentA#1", "AgentA")
        runstate.put_agent_mapping(self.paths, _RUN_ID, "agent-z2", "AgentB#1", "AgentB")
        map_dir = self.paths.agent_map_dir(_RUN_ID)
        files = list(map_dir.glob("*.json"))
        self.assertEqual(2, len(files),
                         "Each agent_id must get its own separate mapping file")

    def test_instance_id_with_reserved_char_uses_sanitized_folder_name(self):
        """Reserved chars in agent_instance_id are replaced in the folder name."""
        # Directly test LogPaths sanitization: a colon must be replaced with underscore
        iid_with_colon = "Agent:Special#1"
        invocation_dir = self.paths.invocation_dir(_RUN_ID, iid_with_colon)
        self.assertNotIn(":", str(invocation_dir.name),
                         "Folder name must not contain the reserved colon character")
        self.assertIn("_", invocation_dir.name)

    def test_event_json_contains_unsanitized_instance_id(self):
        """The agent_instance_id in the event JSON is the original, not sanitized."""
        prompt = '{"agent_instance_id": "TestWriter#28"}'
        ctx = _make_start_ctx(
            str(self.tmp_path),
            agent_id="agent-ev1",
            agent_type="TestWriter",
            agent_prompt=prompt,
        )
        invocation.handle_subagent_start(ctx)
        event_file = self.paths.invocation_events(_RUN_ID, "TestWriter#28")
        self.assertTrue(event_file.exists())
        event = _read_jsonl(event_file)[0]
        self.assertEqual("TestWriter#28", event["agent_instance_id"])

    def test_put_agent_mapping_returns_true_on_success(self):
        result = runstate.put_agent_mapping(
            self.paths, _RUN_ID, "agent-ret1", "SomeAgent#1", "SomeAgent"
        )
        self.assertTrue(result)

    def test_put_agent_mapping_never_raises(self):
        try:
            runstate.put_agent_mapping(
                self.paths, _RUN_ID, "agent-nr1", "SomeAgent#1", None
            )
        except Exception as e:
            self.fail(f"put_agent_mapping raised: {e}")

    def test_get_agent_mapping_never_raises_on_missing_run(self):
        try:
            runstate.get_agent_mapping(self.paths, "nonexistent-run", "agent-x")
        except Exception as e:
            self.fail(f"get_agent_mapping raised: {e}")

    def test_resolve_invocation_id_never_raises(self):
        try:
            runstate.resolve_invocation_id(self.paths, "nonexistent-run", "agent-x")
        except Exception as e:
            self.fail(f"resolve_invocation_id raised: {e}")


# ---------------------------------------------------------------------------
# T4.8  no invocation event written to the orchestrator event stream
# ---------------------------------------------------------------------------

class TestNoInvocationEventToOrchestratorStream(unittest.TestCase):
    """invocation_start, invocation_end, and the boundary turns must ONLY appear
    in the invocation's own 03_events.jsonl, never in 00_orchestrator_events.jsonl."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _orc_events(self):
        path = self.paths.orchestrator_events(_RUN_ID)
        if not path.exists():
            return []
        return _read_jsonl(path)

    def test_subagent_start_does_not_create_orchestrator_events_file(self):
        prompt = '{"agent_instance_id": "SomeAgent#1"}'
        ctx = _make_start_ctx(
            str(self.tmp_path),
            agent_id="aid-orc1", agent_type="SomeAgent", agent_prompt=prompt,
        )
        invocation.handle_subagent_start(ctx)
        self.assertFalse(
            self.paths.orchestrator_events(_RUN_ID).exists(),
            "SubagentStart must not create 00_orchestrator_events.jsonl",
        )

    def test_subagent_stop_does_not_create_orchestrator_events_file(self):
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-orc2", "SomeAgent#2")
        ctx = _make_stop_ctx(
            str(self.tmp_path),
            agent_id="aid-orc2", last_assistant_message="Response.",
        )
        invocation.handle_subagent_stop(ctx)
        self.assertFalse(
            self.paths.orchestrator_events(_RUN_ID).exists(),
            "SubagentStop must not create 00_orchestrator_events.jsonl",
        )

    def test_invocation_start_not_in_orchestrator_events(self):
        """invocation_start must never appear in the orchestrator event stream."""
        prompt = '{"agent_instance_id": "SomeAgent#3"}'
        ctx = _make_start_ctx(
            str(self.tmp_path),
            agent_id="aid-orc3", agent_type="SomeAgent", agent_prompt=prompt,
        )
        invocation.handle_subagent_start(ctx)
        event_names = [e["event"] for e in self._orc_events()]
        self.assertNotIn("invocation_start", event_names)

    def test_invocation_end_not_in_orchestrator_events(self):
        """invocation_end must never appear in the orchestrator event stream."""
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-orc4", "SomeAgent#4")
        ctx = _make_stop_ctx(
            str(self.tmp_path),
            agent_id="aid-orc4", last_assistant_message="Done.",
        )
        invocation.handle_subagent_stop(ctx)
        event_names = [e["event"] for e in self._orc_events()]
        self.assertNotIn("invocation_end", event_names)

    def test_invocation_boundary_turns_not_in_orchestrator_events(self):
        """Neither the initial nor the final invocation turn is in the orchestrator stream."""
        prompt = '{"agent_instance_id": "SomeAgent#5"}'
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-orc5", "SomeAgent#5")

        start_ctx = _make_start_ctx(
            str(self.tmp_path),
            agent_id="aid-orc5", agent_type="SomeAgent", agent_prompt=prompt,
        )
        invocation.handle_subagent_start(start_ctx)

        stop_ctx = _make_stop_ctx(
            str(self.tmp_path),
            agent_id="aid-orc5", last_assistant_message="Final response.",
        )
        invocation.handle_subagent_stop(stop_ctx)

        orc_event_names = [e["event"] for e in self._orc_events()]
        self.assertNotIn("turn", orc_event_names,
                         "Invocation boundary turns must not appear in orchestrator stream")

    def test_invocation_events_are_in_invocation_stream_not_orchestrator(self):
        """Full round-trip: both start and stop events appear in the invocation stream."""
        prompt = '{"agent_instance_id": "SomeAgent#6"}'
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-orc6", "SomeAgent#6")

        start_ctx = _make_start_ctx(
            str(self.tmp_path),
            agent_id="aid-orc6", agent_type="SomeAgent", agent_prompt=prompt,
        )
        invocation.handle_subagent_start(start_ctx)

        stop_ctx = _make_stop_ctx(
            str(self.tmp_path),
            agent_id="aid-orc6", last_assistant_message='{"status_code": "SUCCESS"}',
        )
        invocation.handle_subagent_stop(stop_ctx)

        inv_events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SomeAgent#6"))
        inv_event_names = [e["event"] for e in inv_events]
        self.assertIn("invocation_start", inv_event_names)
        self.assertIn("invocation_end", inv_event_names)
        # Orchestrator stream must remain empty (or non-existent)
        self.assertEqual([], self._orc_events())

    def test_handlers_registered_for_both_subagent_events(self):
        """HANDLERS dict must include both SubagentStart and SubagentStop."""
        self.assertIn("SubagentStart", invocation.HANDLERS)
        self.assertIn("SubagentStop", invocation.HANDLERS)


# ---------------------------------------------------------------------------
# Helpers for prompt-threading tests
# ---------------------------------------------------------------------------

def _iso_ms_now() -> str:
    now = datetime.datetime.now(datetime.timezone.utc)
    ms = now.microsecond // 1000
    return now.strftime("%Y-%m-%dT%H:%M:%S.") + f"{ms:03d}Z"


def _write_dispatch_entry_for_prompt_tests(
    paths, session_id: str,
    agent_instance_id: str, prompt=None, extracted_run_id=None
) -> None:
    """Write a raw pending-dispatch JSONL entry directly into the queue file.

    This bypasses put_pending_dispatch so the prompt-threading tests can inject
    a dispatch entry containing a 'prompt' field without depending on the
    put_pending_dispatch API accepting a prompt keyword argument (I1.2).

    When prompt is given it is included in the entry. When prompt is None the
    'prompt' key is omitted, simulating an old-format pre-fix entry.
    """
    entry_path = paths.pending_dispatch_entry(session_id)
    entry_path.parent.mkdir(parents=True, exist_ok=True)
    entry: dict = {
        "agent_instance_id": agent_instance_id,
        "run_id": extracted_run_id,
        "created_at": _iso_ms_now(),
    }
    if prompt is not None:
        entry["prompt"] = prompt
    with open(entry_path, "w", encoding="utf-8") as fh:
        fh.write(json.dumps(entry) + "\n")


# ---------------------------------------------------------------------------
# Prompt present in pending dispatch — expected to fail until fix is applied
# ---------------------------------------------------------------------------

_PROMPT_THREADING_SESSION = "test-session-prompt-thread"
_PROMPT_THREADING_AGENT_ID = "harness-prompt-thread-001"
_PROMPT_THREADING_INSTANCE_ID = "test-writer-tdd#6"
_PROMPT_THREADING_TEXT = (
    '{"agent_instance_id": "test-writer-tdd#6", '
    '"run_id": "20260731T141500Z-e5a2", '
    '"task_description": "Write tests for the hook logging fix."}'
)


class TestSubagentStartPromptFromDispatch(unittest.TestCase):
    """When the pending dispatch carries a prompt, handle_subagent_start must:
      (a) include the prompt text in the invocation_start event's 'prompt' field;
      (b) emit a turn event with role='user' and the prompt as content;
      (c) pass the prompt to render_input so 01_input.md contains '## Prompt'.

    All three positive tests will FAIL until both fixes are in place:
      1. pop_pending_dispatch returns 'prompt' from the stored entry.
      2. handle_subagent_start reads the prompt from dispatch.get('prompt')
         instead of ctx.field('agent_prompt').
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _setup_dispatch(self):
        """Write a dispatch entry carrying the test prompt into the unknown-run bucket."""
        _write_dispatch_entry_for_prompt_tests(
            self.paths, _PROMPT_THREADING_SESSION,
            _PROMPT_THREADING_INSTANCE_ID, prompt=_PROMPT_THREADING_TEXT,
        )

    def _start_ctx(self, agent_id=_PROMPT_THREADING_AGENT_ID):
        """Build a SubagentStart context with run_id=None (routes to unknown-run bucket)."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": _PROMPT_THREADING_SESSION,
            "agent_id": agent_id,
        }
        workspace_root = pathlib.Path(self.tmp.name)
        ctx = core.HookContext(payload, workspace_root, self.paths, _TS)
        ctx.run_id = None
        return ctx

    def _invocation_events_from_run(self, run_id="unknown-run"):
        run_root = self.paths.run_root(run_id)
        if not run_root.exists():
            return []
        for inv_dir in run_root.iterdir():
            if inv_dir.is_dir() and not inv_dir.name.startswith("."):
                events_file = inv_dir / "03_events.jsonl"
                if events_file.exists():
                    return _read_jsonl(events_file)
        return []

    def _input_md_text(self, run_id="unknown-run"):
        run_root = self.paths.run_root(run_id)
        if not run_root.exists():
            return ""
        for inv_dir in run_root.iterdir():
            if inv_dir.is_dir() and not inv_dir.name.startswith("."):
                input_md = inv_dir / "01_input.md"
                if input_md.exists():
                    return input_md.read_text("utf-8")
        return ""

    def test_invocation_start_includes_prompt_from_pending_dispatch(self):
        """When the pending dispatch carries a prompt, invocation_start must include
        the prompt text in its 'prompt' field.

        Currently FAILS: pop_pending_dispatch does not return 'prompt', and
        handle_subagent_start reads agent_prompt from the payload (absent here)
        rather than from the dispatch.
        """
        self._setup_dispatch()
        invocation.handle_subagent_start(self._start_ctx())
        events = self._invocation_events_from_run()
        inv_start = next(
            (e for e in events if e.get("event") == "invocation_start"), None
        )
        self.assertIsNotNone(inv_start, "invocation_start event must be emitted")
        self.assertIn(
            "prompt", inv_start,
            "invocation_start must include 'prompt' when the dispatch carries one",
        )
        self.assertEqual(
            _PROMPT_THREADING_TEXT, inv_start["prompt"],
            "prompt in invocation_start must match the dispatch's prompt text exactly",
        )

    def test_user_turn_emitted_with_dispatch_prompt_as_content(self):
        """A turn event with role='user' must be emitted carrying the dispatch prompt
        as content when the pending dispatch includes a prompt.

        Currently FAILS: agent_prompt is absent from the payload, so the current
        implementation emits no user turn.
        """
        self._setup_dispatch()
        invocation.handle_subagent_start(self._start_ctx())
        events = self._invocation_events_from_run()
        user_turns = [
            e for e in events
            if e.get("event") == "turn" and e.get("role") == "user"
        ]
        self.assertEqual(
            1, len(user_turns),
            "Exactly one user turn must be emitted when the dispatch carries a prompt",
        )
        self.assertEqual(
            _PROMPT_THREADING_TEXT, user_turns[0].get("content"),
            "User turn content must equal the dispatch's prompt text",
        )

    def test_input_artifact_has_prompt_section_when_dispatch_carries_prompt(self):
        """01_input.md must contain a '## Prompt' section followed by the prompt text
        when the pending dispatch includes a prompt.

        Currently FAILS: render_input is called with agent_prompt=None (absent from
        the payload), so no '## Prompt' section is rendered.
        """
        self._setup_dispatch()
        invocation.handle_subagent_start(self._start_ctx())
        text = self._input_md_text()
        self.assertIn(
            "## Prompt", text,
            "01_input.md must contain a '## Prompt' section when dispatch has a prompt",
        )
        self.assertIn(
            _PROMPT_THREADING_TEXT, text,
            "01_input.md must contain the full prompt text from the dispatch",
        )

    def test_never_raises_when_dispatch_carries_prompt(self):
        """handle_subagent_start must never raise when the dispatch carries a prompt."""
        self._setup_dispatch()
        ctx = self._start_ctx()
        try:
            invocation.handle_subagent_start(ctx)
        except Exception as exc:
            self.fail(
                f"handle_subagent_start raised when dispatch carries prompt: {exc}"
            )


# ---------------------------------------------------------------------------
# Degrade-never-fabricate: prompt absent → no prompt field, no user turn
# ---------------------------------------------------------------------------

class TestSubagentStartPromptDegradeNeverFabricate(unittest.TestCase):
    """When no pending dispatch exists, or the dispatch has no 'prompt' field,
    the degrade-never-fabricate convention must be preserved:
      - 'prompt' is absent from invocation_start (not null, not empty string).
      - No user turn event is emitted.
      - 01_input.md has no '## Prompt' section.

    These tests serve as a regression guard: the prompt-threading fix must not
    accidentally introduce prompt fabrication when no prompt is available.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    _DEGRADE_SESSION = "test-session-degrade-inv"

    def _start_ctx(self, agent_id, session_id=None):
        session_id = session_id or self._DEGRADE_SESSION
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": agent_id,
        }
        workspace_root = pathlib.Path(self.tmp.name)
        ctx = core.HookContext(payload, workspace_root, self.paths, _TS)
        ctx.run_id = None
        return ctx

    def _invocation_events(self, run_id="unknown-run"):
        run_root = self.paths.run_root(run_id)
        if not run_root.exists():
            return []
        for inv_dir in run_root.iterdir():
            if inv_dir.is_dir() and not inv_dir.name.startswith("."):
                events_file = inv_dir / "03_events.jsonl"
                if events_file.exists():
                    return _read_jsonl(events_file)
        return []

    def _input_md_text(self, run_id="unknown-run"):
        run_root = self.paths.run_root(run_id)
        if not run_root.exists():
            return ""
        for inv_dir in run_root.iterdir():
            if inv_dir.is_dir() and not inv_dir.name.startswith("."):
                input_md = inv_dir / "01_input.md"
                if input_md.exists():
                    return input_md.read_text("utf-8")
        return ""

    # --- No pending dispatch at all ---

    def test_prompt_absent_from_invocation_start_when_no_pending_dispatch(self):
        """When no pending dispatch exists, 'prompt' must be absent from
        invocation_start (not null, not any fabricated value)."""
        ctx = self._start_ctx("harness-dg-001")
        invocation.handle_subagent_start(ctx)
        events = self._invocation_events()
        inv_start = next(
            (e for e in events if e.get("event") == "invocation_start"), None
        )
        self.assertIsNotNone(inv_start, "invocation_start must be emitted")
        self.assertNotIn(
            "prompt", inv_start,
            "prompt must be absent when no pending dispatch exists",
        )

    def test_no_user_turn_emitted_when_no_pending_dispatch(self):
        """When no pending dispatch exists, no user turn must be emitted."""
        ctx = self._start_ctx("harness-dg-002")
        invocation.handle_subagent_start(ctx)
        events = self._invocation_events()
        user_turns = [
            e for e in events
            if e.get("event") == "turn" and e.get("role") == "user"
        ]
        self.assertEqual(
            0, len(user_turns),
            "No user turn must be emitted when no pending dispatch exists",
        )

    def test_no_prompt_section_in_input_artifact_when_no_pending_dispatch(self):
        """01_input.md must not contain '## Prompt' when no dispatch exists."""
        ctx = self._start_ctx("harness-dg-003")
        invocation.handle_subagent_start(ctx)
        text = self._input_md_text()
        self.assertNotIn(
            "## Prompt", text,
            "01_input.md must not have '## Prompt' when no pending dispatch exists",
        )

    # --- Dispatch exists but has no prompt field (backward-compatible old format) ---

    def test_prompt_absent_from_invocation_start_when_dispatch_has_no_prompt_field(self):
        """When the dispatch entry has no 'prompt' field (old-format entry), 'prompt'
        must be absent from invocation_start (backward-compatible degrade path)."""
        _write_dispatch_entry_for_prompt_tests(
            self.paths, self._DEGRADE_SESSION,
            "unknown-agent",
            # prompt omitted -> old-format entry
        )
        ctx = self._start_ctx("harness-dg-004", session_id=self._DEGRADE_SESSION)
        invocation.handle_subagent_start(ctx)
        events = self._invocation_events()
        inv_start = next(
            (e for e in events if e.get("event") == "invocation_start"), None
        )
        self.assertIsNotNone(inv_start)
        self.assertNotIn(
            "prompt", inv_start,
            "prompt must be absent when the dispatch entry has no 'prompt' field",
        )

    def test_no_user_turn_when_dispatch_has_no_prompt_field(self):
        """No user turn must be emitted when the dispatch entry has no 'prompt' field."""
        _write_dispatch_entry_for_prompt_tests(
            self.paths, self._DEGRADE_SESSION,
            "unknown-agent",
        )
        ctx = self._start_ctx("harness-dg-005", session_id=self._DEGRADE_SESSION)
        invocation.handle_subagent_start(ctx)
        events = self._invocation_events()
        user_turns = [
            e for e in events
            if e.get("event") == "turn" and e.get("role") == "user"
        ]
        self.assertEqual(
            0, len(user_turns),
            "No user turn must be emitted when dispatch has no 'prompt' field",
        )

    def test_no_prompt_section_in_input_artifact_when_dispatch_has_no_prompt_field(self):
        """01_input.md must not have '## Prompt' when the dispatch entry has no prompt."""
        _write_dispatch_entry_for_prompt_tests(
            self.paths, self._DEGRADE_SESSION,
            "unknown-agent",
        )
        ctx = self._start_ctx("harness-dg-006", session_id=self._DEGRADE_SESSION)
        invocation.handle_subagent_start(ctx)
        text = self._input_md_text()
        self.assertNotIn(
            "## Prompt", text,
            "01_input.md must not have '## Prompt' when dispatch has no 'prompt' field",
        )


# ---------------------------------------------------------------------------
# T3.2  resolve_invocation — mapped flag and agent-map miss diagnostic
# ---------------------------------------------------------------------------

class TestResolveInvocationMappedFlag(unittest.TestCase):
    """runstate.resolve_invocation(paths, run_id, agent_id) returns
    (agent_instance_id, mapped): mapped is True when an agent-map entry
    supplied the identity, False when the deterministic 'unmapped_<agent_id>'
    fallback was used. A miss emits an 'agent-map: miss' diagnostic naming
    run_id and agent_id."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug

    def test_mapped_true_when_mapping_exists(self):
        runstate.put_agent_mapping(self.paths, _RUN_ID, "aid-map-1", "Reviewer#9", "Reviewer")
        identity, mapped = runstate.resolve_invocation(self.paths, _RUN_ID, "aid-map-1")
        self.assertEqual("Reviewer#9", identity)
        self.assertTrue(mapped)

    def test_mapped_false_when_mapping_absent(self):
        identity, mapped = runstate.resolve_invocation(self.paths, _RUN_ID, "aid-map-missing-1")
        self.assertEqual("unmapped_aid-map-missing-1", identity)
        self.assertFalse(mapped)

    def test_resolve_invocation_id_wraps_first_tuple_element(self):
        """resolve_invocation_id keeps returning just the identity string."""
        runstate.put_agent_mapping(self.paths, _RUN_ID, "aid-map-2", "Reviewer#10", "Reviewer")
        identity, _mapped = runstate.resolve_invocation(self.paths, _RUN_ID, "aid-map-2")
        self.assertEqual(identity, runstate.resolve_invocation_id(self.paths, _RUN_ID, "aid-map-2"))

    def test_never_returns_none_identity_on_miss(self):
        identity, _mapped = runstate.resolve_invocation(self.paths, _RUN_ID, "aid-map-missing-2")
        self.assertIsNotNone(identity)

    def test_never_misattributes_to_orchestrator(self):
        """A miss must never resolve to an identity indistinguishable from the
        orchestrator (no agent_id at all)."""
        identity, _mapped = runstate.resolve_invocation(self.paths, _RUN_ID, "aid-map-missing-3")
        self.assertNotEqual("", identity)
        self.assertTrue(identity.startswith("unmapped_"))

    def test_agent_map_miss_emits_diagnostic_with_run_id_and_agent_id(self):
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        runstate.resolve_invocation(self.paths, _RUN_ID, "aid-diag-miss-1")
        self.assertTrue(debug_file.exists(), "Diagnostic file must be written on a miss")
        text = debug_file.read_text(encoding="utf-8")
        self.assertIn("agent-map: miss", text)
        self.assertIn(_RUN_ID, text)
        self.assertIn("aid-diag-miss-1", text)

    def test_agent_map_hit_emits_no_miss_diagnostic(self):
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        runstate.put_agent_mapping(self.paths, _RUN_ID, "aid-diag-hit-1", "Reviewer#11", "Reviewer")
        runstate.resolve_invocation(self.paths, _RUN_ID, "aid-diag-hit-1")
        if debug_file.exists():
            text = debug_file.read_text(encoding="utf-8")
            self.assertNotIn("agent-map: miss", text)

    def test_resolve_invocation_never_raises(self):
        try:
            runstate.resolve_invocation(self.paths, "nonexistent-run", "aid-never-raise")
        except Exception as exc:
            self.fail(f"resolve_invocation raised: {exc}")


# ---------------------------------------------------------------------------
# T3.3  classify_unmapped_stop — genuine vs spurious pure classification
# ---------------------------------------------------------------------------

class TestClassifyUnmappedStop(unittest.TestCase):
    """classify_unmapped_stop(ctx) returns 'spurious' only when the firing
    carries neither an agent_transcript_path nor a last_assistant_message.
    Every other case is 'genuine'. The distinction is biased toward
    recording: ambiguity resolves to 'genuine'."""

    def _ctx(self, tmp_path, **kwargs):
        return _make_stop_ctx(tmp_path, agent_id="aid-classify", **kwargs)

    def test_spurious_when_neither_field_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            ctx = self._ctx(tmp)
            self.assertEqual("spurious", invocation.classify_unmapped_stop(ctx))

    def test_genuine_when_transcript_path_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            ctx = self._ctx(tmp, agent_transcript_path="/some/transcript.jsonl")
            self.assertEqual("genuine", invocation.classify_unmapped_stop(ctx))

    def test_genuine_when_last_assistant_message_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            ctx = self._ctx(tmp, last_assistant_message="Some final response.")
            self.assertEqual("genuine", invocation.classify_unmapped_stop(ctx))

    def test_genuine_when_both_fields_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            ctx = self._ctx(
                tmp,
                agent_transcript_path="/some/transcript.jsonl",
                last_assistant_message="Some final response.",
            )
            self.assertEqual("genuine", invocation.classify_unmapped_stop(ctx))

    def test_empty_string_fields_are_treated_as_absent_and_classify_spurious(self):
        """HookContext normalizes empty strings to None, so an empty-string
        transcript path or message must classify the same as absent."""
        with tempfile.TemporaryDirectory() as tmp:
            ctx = self._ctx(tmp, agent_transcript_path="", last_assistant_message="")
            self.assertEqual("spurious", invocation.classify_unmapped_stop(ctx))

    def test_whitespace_only_last_assistant_message_classifies_genuine(self):
        """A whitespace-only last_assistant_message is a present (non-empty)
        string, not an absent field -- classification must be biased toward
        recording, so this classifies 'genuine' rather than 'spurious'."""
        with tempfile.TemporaryDirectory() as tmp:
            ctx = self._ctx(tmp, last_assistant_message="   ")
            self.assertEqual("genuine", invocation.classify_unmapped_stop(ctx))

    def test_never_raises(self):
        with tempfile.TemporaryDirectory() as tmp:
            ctx = self._ctx(tmp)
            try:
                invocation.classify_unmapped_stop(ctx)
            except Exception as exc:
                self.fail(f"classify_unmapped_stop raised: {exc}")


# ---------------------------------------------------------------------------
# T3.3 / T3.4  handle_subagent_stop — genuine-unmapped quarantine outcome
# ---------------------------------------------------------------------------

class TestSubagentStopQuarantineGenuine(unittest.TestCase):
    """An unmapped SubagentStop classified 'genuine' must still produce its
    invocation_end event, its 02_output.md artifact, and its transcript
    export -- at the quarantine destination, with attribution='quarantined'
    on the event and a diagnostic emitted."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug

    def _run_stop(self, agent_id, **kwargs):
        ctx = _make_stop_ctx(str(self.tmp_path), agent_id=agent_id, **kwargs)
        invocation.handle_subagent_stop(ctx)
        return ctx

    def test_quarantine_events_file_receives_invocation_end(self):
        self._run_stop("aid-quarantine-1", last_assistant_message="Genuine but unmapped.")
        events = _read_jsonl(self.paths.quarantine_events(_RUN_ID, "aid-quarantine-1"))
        self.assertIn("invocation_end", [e["event"] for e in events])

    def test_quarantine_invocation_end_has_attribution_quarantined(self):
        self._run_stop("aid-quarantine-2", last_assistant_message="Genuine but unmapped.")
        events = _read_jsonl(self.paths.quarantine_events(_RUN_ID, "aid-quarantine-2"))
        event = next(e for e in events if e["event"] == "invocation_end")
        self.assertEqual("quarantined", event.get("attribution"))

    def test_quarantine_final_turn_is_written(self):
        self._run_stop("aid-quarantine-3", last_assistant_message="Final quarantined response.")
        events = _read_jsonl(self.paths.quarantine_events(_RUN_ID, "aid-quarantine-3"))
        turns = [e for e in events if e["event"] == "turn" and e.get("role") == "assistant"]
        self.assertEqual(1, len(turns))

    def test_quarantine_output_artifact_is_written(self):
        self._run_stop("aid-quarantine-4", last_assistant_message="Output goes to quarantine.")
        output_path = self.paths.quarantine_output(_RUN_ID, "aid-quarantine-4")
        self.assertTrue(output_path.exists(), "02_output.md must be written to the quarantine location")

    def test_quarantine_transcript_export_is_attempted(self):
        """04_session.raw at the quarantine destination must be a
        byte-identical copy of agent_transcript_path, mirroring the
        mapped-path export contract."""
        transcript_path = _write_transcript_file(self.tmp_path, [
            {"type": "assistant", "message": {"model": "claude-opus-4-5",
                                              "usage": {"input_tokens": 5, "output_tokens": 3}}}
        ])
        self._run_stop(
            "aid-quarantine-5",
            agent_transcript_path=transcript_path,
            last_assistant_message="Response with transcript.",
        )
        raw_path = self.paths.quarantine_raw(_RUN_ID, "aid-quarantine-5")
        self.assertTrue(raw_path.exists(),
                        "Quarantine transcript export must be written to quarantine_raw")
        original_bytes = pathlib.Path(transcript_path).read_bytes()
        self.assertEqual(original_bytes, raw_path.read_bytes(),
                         "Quarantine transcript export must be byte-identical to the source transcript")

    def test_no_unmapped_prefixed_directory_is_created(self):
        """AC3.4 boundary: the genuine-unmapped case must not fall back to the
        legacy unmapped_* directory; it must land in quarantine instead."""
        self._run_stop("aid-quarantine-6", last_assistant_message="No legacy unmapped dir.")
        run_root = self.paths.run_root(_RUN_ID)
        if run_root.exists():
            legacy_dirs = [
                d for d in run_root.iterdir()
                if d.is_dir() and d.name.startswith("unmapped_")
            ]
            self.assertEqual(
                0, len(legacy_dirs),
                "Genuine-unmapped stop must not create a legacy unmapped_* directory",
            )

    def test_quarantine_directory_is_dot_prefixed(self):
        """D5: the quarantine destination lives under a dot-prefixed directory
        inside the run folder."""
        self._run_stop("aid-quarantine-7", last_assistant_message="Dot-prefixed check.")
        quarantine_path = self.paths.quarantine_events(_RUN_ID, "aid-quarantine-7")
        run_root = self.paths.run_root(_RUN_ID)
        relative = quarantine_path.relative_to(run_root)
        self.assertTrue(
            relative.parts[0].startswith("."),
            "Quarantine destination must be dot-prefixed so the analyzer's scanner skips it",
        )

    def test_emits_quarantine_diagnostic(self):
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        self._run_stop("aid-quarantine-8", last_assistant_message="Diagnostic check.")
        self.assertTrue(debug_file.exists())
        text = debug_file.read_text(encoding="utf-8")
        self.assertIn("subagent-stop: quarantined", text)
        self.assertIn("aid-quarantine-8", text,
                      "Diagnostic must name the agent_id so a warm-up miss can be told apart from a genuine failure")
        self.assertIn(_RUN_ID, text)

    def test_never_raises(self):
        try:
            self._run_stop("aid-quarantine-9", last_assistant_message="No crash.")
        except Exception as exc:
            self.fail(f"handle_subagent_stop raised on genuine-unmapped input: {exc}")


# ---------------------------------------------------------------------------
# T3.3 / T3.4  handle_subagent_stop — spurious narration discard outcome
# ---------------------------------------------------------------------------

class TestSubagentStopSpuriousDiscard(unittest.TestCase):
    """An unmapped SubagentStop classified 'spurious' (no agent_transcript_path
    and no last_assistant_message) is discarded: no event, no artifact, no
    quarantine directory, no legacy unmapped_* directory -- but a diagnostic
    is still emitted."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug

    def _run_stop(self, agent_id, **kwargs):
        ctx = _make_stop_ctx(str(self.tmp_path), agent_id=agent_id, **kwargs)
        invocation.handle_subagent_stop(ctx)
        return ctx

    def test_no_quarantine_directory_is_created(self):
        self._run_stop("aid-spurious-1")
        self.assertFalse(
            self.paths.quarantine_events(_RUN_ID, "aid-spurious-1").exists(),
            "A spurious narration firing must not create a quarantine entry",
        )

    def test_no_legacy_unmapped_directory_is_created(self):
        self._run_stop("aid-spurious-2")
        run_root = self.paths.run_root(_RUN_ID)
        if run_root.exists():
            legacy_dirs = [
                d for d in run_root.iterdir()
                if d.is_dir() and d.name.startswith("unmapped_")
            ]
            self.assertEqual(0, len(legacy_dirs))

    def test_no_output_artifact_is_written(self):
        self._run_stop("aid-spurious-3")
        self.assertFalse(
            self.paths.quarantine_output(_RUN_ID, "aid-spurious-3").exists(),
        )

    def test_emits_discard_diagnostic(self):
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        self._run_stop("aid-spurious-4")
        self.assertTrue(debug_file.exists())
        text = debug_file.read_text(encoding="utf-8")
        self.assertIn("subagent-stop: discarded", text)
        self.assertIn("aid-spurious-4", text,
                      "Diagnostic must name the agent_id so a warm-up miss can be told apart from a genuine failure")
        self.assertIn(_RUN_ID, text)

    def test_never_raises(self):
        try:
            self._run_stop("aid-spurious-5")
        except Exception as exc:
            self.fail(f"handle_subagent_stop raised on spurious input: {exc}")


# ---------------------------------------------------------------------------
# T3.5  Regression: the mapped path is unaffected by the quarantine change
# ---------------------------------------------------------------------------

class TestSubagentStopRegressionMappedPathUnaffected(unittest.TestCase):
    """A SubagentStop whose agent_id resolves via a real agent-map entry must
    behave exactly as it did before this stage: normal invocation directory,
    no attribution field, and no quarantine/discard diagnostic."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug

    def test_mapped_stop_writes_invocation_end_without_attribution_field(self):
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-regress-1", "Reviewer#20", "Reviewer")
        ctx = _make_stop_ctx(str(self.tmp_path), agent_id="aid-regress-1",
                             last_assistant_message="Mapped and normal.")
        invocation.handle_subagent_stop(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "Reviewer#20"))
        event = next(e for e in events if e["event"] == "invocation_end")
        self.assertNotIn("attribution", event,
                         "The mapped path must never carry the quarantine attribution field")

    def test_mapped_stop_does_not_create_quarantine_directory(self):
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-regress-2", "Reviewer#21", "Reviewer")
        ctx = _make_stop_ctx(str(self.tmp_path), agent_id="aid-regress-2",
                             last_assistant_message="Mapped and normal.")
        invocation.handle_subagent_stop(ctx)
        self.assertFalse(self.paths.quarantine_events(_RUN_ID, "Reviewer#21").exists())

    def test_mapped_stop_emits_no_quarantine_or_discard_diagnostic(self):
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-regress-3", "Reviewer#22", "Reviewer")
        ctx = _make_stop_ctx(str(self.tmp_path), agent_id="aid-regress-3",
                             last_assistant_message="Mapped and normal.")
        invocation.handle_subagent_stop(ctx)
        if debug_file.exists():
            text = debug_file.read_text(encoding="utf-8")
            self.assertNotIn("subagent-stop: quarantined", text)
            self.assertNotIn("subagent-stop: discarded", text)

    def test_mapped_stop_still_writes_output_artifact_at_ordinary_location(self):
        _register_mapping(str(self.tmp_path), _RUN_ID, "aid-regress-4", "Reviewer#23", "Reviewer")
        ctx = _make_stop_ctx(str(self.tmp_path), agent_id="aid-regress-4",
                             last_assistant_message="Mapped and normal.")
        invocation.handle_subagent_stop(ctx)
        self.assertTrue(self.paths.invocation_output(_RUN_ID, "Reviewer#23").exists())


# ---------------------------------------------------------------------------
# Subagent-stop capture path: regression guards for the tool-firing fix
# ---------------------------------------------------------------------------

class TestSubagentStopCapturePathUnaffected(unittest.TestCase):
    """The subagent-stop path is the sole authoritative source of a subagent's
    own usage records. Its behavior must remain unchanged regardless of any
    change to how tool-path emission is handled.

    The key invariant: handle_subagent_stop reads ctx.field("agent_transcript_path")
    — the subagent's own file — not ctx.transcript_path (the orchestrator's
    transcript). This is what makes stop-path attribution correct and distinct
    from the orchestrator stream."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _setup_mapping(self, agent_id, agent_instance_id, agent_type=None):
        _register_mapping(self.tmp.name, _RUN_ID, agent_id, agent_instance_id, agent_type)

    def _run_stop(self, agent_id, **kwargs):
        ctx = _make_stop_ctx(self.tmp.name, agent_id=agent_id, **kwargs)
        invocation.handle_subagent_stop(ctx)
        return ctx

    def test_subagent_usage_reaches_invocation_stream_from_own_transcript(self):
        """A subagent's usage records must land on its invocation stream, read
        from agent_transcript_path — the subagent's own file, not ctx.transcript_path."""
        own_transcript = _write_transcript_file(
            str(self.tmp_path / "own"),
            [{"type": "assistant", "message": {
                "id": "msg_own", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 15},
            }}],
        )
        self._setup_mapping("aid-stop-u1", "StopAgent#1", "StopAgent")
        self._run_stop("aid-stop-u1", agent_type="StopAgent",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       agent_transcript_path=own_transcript)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "StopAgent#1"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events),
                         "Subagent's own usage records must reach the invocation stream at stop")
        self.assertEqual("msg_own", usage_events[0].get("record_id"))

    def test_orchestrator_transcript_records_reach_orchestrator_stream_at_stop(self):
        """At SubagentStop the orchestrator transcript (ctx.transcript_path) must still
        produce usage_records on the orchestrator stream, separately from the subagent's."""
        orch_transcript = _write_transcript_file(
            str(self.tmp_path / "orch"),
            [{"type": "assistant", "message": {
                "id": "msg_orch", "model": "claude-opus-4-5",
                "usage": {"output_tokens": 25},
            }}],
        )
        self._setup_mapping("aid-stop-u2", "StopAgent#2", "StopAgent")
        self._run_stop("aid-stop-u2", agent_type="StopAgent",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       transcript_path=orch_transcript)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertTrue(orc_path.exists(),
                        "Orchestrator-stream usage_record must be emitted at SubagentStop")
        events = _read_jsonl(orc_path)
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))
        self.assertEqual("msg_orch", usage_events[0].get("record_id"))

    def test_subagent_own_records_not_drawn_from_orchestrator_transcript(self):
        """Records from ctx.transcript_path (orchestrator) must NOT appear on the
        invocation stream. Only agent_transcript_path records reach the subagent's stream."""
        own_transcript = _write_transcript_file(
            str(self.tmp_path / "own2"),
            [{"type": "assistant", "message": {
                "id": "msg_subagent_only", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 20},
            }}],
        )
        orch_transcript = _write_transcript_file(
            str(self.tmp_path / "orch2"),
            [{"type": "assistant", "message": {
                "id": "msg_orchestrator_only", "model": "claude-opus-4-5",
                "usage": {"output_tokens": 30},
            }}],
        )
        self._setup_mapping("aid-stop-u3", "StopAgent#3", "StopAgent")
        self._run_stop("aid-stop-u3", agent_type="StopAgent",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       agent_transcript_path=own_transcript,
                       transcript_path=orch_transcript)
        inv_events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "StopAgent#3"))
        inv_record_ids = {e["record_id"] for e in inv_events if e["event"] == "usage_record"}
        self.assertIn("msg_subagent_only", inv_record_ids,
                      "Subagent's own record must reach the invocation stream")
        self.assertNotIn("msg_orchestrator_only", inv_record_ids,
                         "Orchestrator-transcript record must not reach the invocation stream")

    def test_quarantine_stop_usage_records_still_reach_quarantine_stream(self):
        """An unmapped SubagentStop (quarantine path) still delivers the subagent's
        usage records to the quarantine event stream, not to any ordinary invocation dir."""
        quarantine_transcript = _write_transcript_file(
            str(self.tmp_path / "quarantine"),
            [{"type": "assistant", "message": {
                "id": "msg_quarantine", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 5},
            }}],
        )
        # No mapping registered — triggers the quarantine path
        self._run_stop("aid-stop-q1",
                       last_assistant_message="Genuine but unmapped.",
                       agent_transcript_path=quarantine_transcript)
        events = _read_jsonl(self.paths.quarantine_events(_RUN_ID, "aid-stop-q1"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events),
                         "Quarantine-path usage records must still reach the quarantine stream")

    def test_stop_handler_degrades_gracefully_on_missing_agent_transcript(self):
        """When agent_transcript_path is absent or unreadable, no usage_record is written
        to the invocation stream and the handler completes without raising."""
        self._setup_mapping("aid-stop-u4", "StopAgent#4", "StopAgent")
        try:
            self._run_stop("aid-stop-u4", agent_type="StopAgent",
                           last_assistant_message='{"status_code": "SUCCESS"}',
                           agent_transcript_path="/nonexistent/transcript.jsonl")
        except Exception as exc:
            self.fail(f"handle_subagent_stop raised on missing agent transcript: {exc}")
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "StopAgent#4"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "No usage_record must be written when agent_transcript_path is unreadable")

    def test_stop_handler_invocation_end_still_written_alongside_usage_records(self):
        """invocation_end must appear on the invocation stream regardless of whether
        agent_transcript_path is provided. Usage emission is independent of event emission."""
        own_transcript = _write_transcript_file(
            str(self.tmp_path / "own3"),
            [{"type": "assistant", "message": {
                "id": "msg_check", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 8},
            }}],
        )
        self._setup_mapping("aid-stop-u5", "StopAgent#5", "StopAgent")
        self._run_stop("aid-stop-u5", agent_type="StopAgent",
                       last_assistant_message='{"status_code": "SUCCESS"}',
                       agent_transcript_path=own_transcript)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "StopAgent#5"))
        event_types = [e["event"] for e in events]
        self.assertIn("invocation_end", event_types,
                      "invocation_end must be written alongside usage records at stop")
        self.assertIn("usage_record", event_types,
                      "usage_record must be written from agent_transcript_path at stop")


if __name__ == "__main__":
    unittest.main()
