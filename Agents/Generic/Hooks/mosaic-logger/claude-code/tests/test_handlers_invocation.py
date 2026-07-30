"""Tests for the SubagentStart and SubagentStop handlers.

Covers agent_instance_id resolution from prompt text, fallback when prompt is
absent or unparseable, invocation_start and invocation_end events, boundary
turns, status_code extraction, token usage and model from subagent transcript,
per-invocation artifact set, agent_id mapping round trip, folder-name
sanitization, and the guarantee that no invocation event reaches the
orchestrator event stream.
"""

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


if __name__ == "__main__":
    unittest.main()
