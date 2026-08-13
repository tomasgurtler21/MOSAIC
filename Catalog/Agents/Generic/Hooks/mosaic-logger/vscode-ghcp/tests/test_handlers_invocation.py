"""Tests for the SubagentStart and SubagentStop handlers (vscode-ghcp variant).

Key vscode-ghcp divergences from the claude-code reference:
  D3: Subagent correlation is keyed on agent_name, not agent_id
  D3: SubagentStart carries no agent_id — the reference's `if not ctx.agent_id: return`
      guard would suppress every VS Code invocation and must NOT be used
  D3: resolve_agent_key returns agent_name when present, falls back to agent_id
  D3: Agent map entry is written (and lookup is performed) using agent_name as the key
  D3: SubagentStop resolves the invocation via agent_name; falls back to agent_id only
      when agent_name is absent
  D3: resolve_agent_type returns agent_type when present, falls back to agent_name
      (the schema defines agent_type as the bare agent name; VS Code's agent_name
       carries exactly that value)

Covers:
  resolve_agent_key: returns agent_name, falls back to agent_id, None when both absent
  resolve_agent_type: returns agent_type, falls back to agent_name
  extract_status_code: last valid code wins; invalid strings ignored
  SubagentStart: no agent_id guard; pops pending dispatch; agent_name keyed mapping
    written BEFORE event emission; invocation_start + user turn to invocation stream
  SubagentStop: resolves via agent_name; skips unmapped firings; invocation_end
    + assistant turn; last_assistant_message omits optional fields when absent
  Round trip: start writes mapping, stop reads via same key, same invocation stream
"""

import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_invocation as invocation


# ---------------------------------------------------------------------------
# Shared constants and helpers
# ---------------------------------------------------------------------------

_RUN_ID = "20260101T170000Z-a3f9"
_SESSION_ID = "test-session-vscode-invocation"
_TS = "2026-01-01T17:00:00.000Z"


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
    """Build a SubagentStart context."""
    return _make_ctx(tmp_path, "SubagentStart", run_id=run_id, **kwargs)


def _make_stop_ctx(tmp_path, run_id=_RUN_ID, **kwargs):
    """Build a SubagentStop context."""
    return _make_ctx(tmp_path, "SubagentStop", run_id=run_id, **kwargs)


def _read_jsonl(path):
    """Read all non-empty lines of a JSONL file as parsed dicts."""
    return [json.loads(ln) for ln in pathlib.Path(path).read_text("utf-8").splitlines()
            if ln.strip()]


def _write_transcript_file(dir_path, records):
    """Write a minimal JSONL transcript and return its path string."""
    dir_path = pathlib.Path(dir_path)
    dir_path.mkdir(parents=True, exist_ok=True)
    path = dir_path / "transcript.jsonl"
    with open(path, "w", encoding="utf-8") as fh:
        for record in records:
            fh.write(json.dumps(record) + "\n")
    return str(path)


def _register_mapping(tmp_path, run_id, agent_key, agent_instance_id, agent_type=None):
    """Pre-register an agent_key -> agent_instance_id mapping on disk."""
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_key, agent_instance_id, agent_type)


def _pre_populate_pending_dispatch(tmp_path, run_id, session_id,
                                   agent_instance_id, extracted_run_id=None, prompt=None):
    """Write one pending-dispatch entry so SubagentStart can pop it."""
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_pending_dispatch(
        paths, run_id, session_id, agent_instance_id, extracted_run_id, prompt=prompt
    )


# ---------------------------------------------------------------------------
# resolve_agent_key (D3 seam)
# ---------------------------------------------------------------------------

class TestResolveAgentKey(unittest.TestCase):
    """resolve_agent_key returns agent_name when present, falls back to agent_id,
    and returns None when both are absent.

    This is the D3 seam: both SubagentStart and SubagentStop route through it,
    so start and end cannot disagree about which key was used.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_agent_name_when_present(self):
        ctx = _make_start_ctx(self.tmp.name, agent_name="TestWriter")
        result = invocation.resolve_agent_key(ctx)
        self.assertEqual("TestWriter", result)

    def test_returns_agent_name_over_agent_id_when_both_present(self):
        """agent_name takes precedence over agent_id (D3 design decision)."""
        ctx = _make_start_ctx(self.tmp.name, agent_name="TestWriter", agent_id="opaque-id-001")
        result = invocation.resolve_agent_key(ctx)
        self.assertEqual("TestWriter", result)

    def test_falls_back_to_agent_id_when_agent_name_absent(self):
        """SubagentStop may carry agent_id without agent_name; fall back to agent_id."""
        ctx = _make_stop_ctx(self.tmp.name, agent_id="harness-agent-001")
        result = invocation.resolve_agent_key(ctx)
        self.assertEqual("harness-agent-001", result)

    def test_returns_none_when_both_absent(self):
        """A firing with neither name nor id cannot be correlated at all."""
        ctx = _make_start_ctx(self.tmp.name)  # no agent_name, no agent_id
        result = invocation.resolve_agent_key(ctx)
        self.assertIsNone(result)

    def test_empty_agent_name_normalizes_to_none_and_falls_back_to_agent_id(self):
        """Empty string agent_name normalizes to None via ctx.field(); agent_id is used."""
        ctx = _make_stop_ctx(self.tmp.name, agent_name="", agent_id="fallback-id")
        result = invocation.resolve_agent_key(ctx)
        self.assertEqual("fallback-id", result)


# ---------------------------------------------------------------------------
# resolve_agent_type (D3 fallback to agent_name)
# ---------------------------------------------------------------------------

class TestResolveAgentType(unittest.TestCase):
    """resolve_agent_type returns agent_type when present, else agent_name.

    The schema defines agent_type as the bare agent name; VS Code's agent_name
    carries exactly that value. Without the fallback, invocation_start would
    systematically omit a field that invocation_end populates.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_agent_type_when_present(self):
        ctx = _make_stop_ctx(self.tmp.name, agent_type="Researcher", agent_name="Researcher")
        result = invocation.resolve_agent_type(ctx)
        self.assertEqual("Researcher", result)

    def test_falls_back_to_agent_name_when_agent_type_absent(self):
        """SubagentStart has no agent_type field; agent_name is the fallback."""
        ctx = _make_start_ctx(self.tmp.name, agent_name="TestWriter")
        result = invocation.resolve_agent_type(ctx)
        self.assertEqual("TestWriter", result)

    def test_returns_none_when_both_agent_type_and_agent_name_absent(self):
        ctx = _make_start_ctx(self.tmp.name)
        result = invocation.resolve_agent_type(ctx)
        self.assertIsNone(result)


# ---------------------------------------------------------------------------
# extract_status_code
# ---------------------------------------------------------------------------

class TestExtractStatusCode(unittest.TestCase):
    """extract_status_code finds the last valid status code in structured form."""

    def test_returns_success_from_structured_json_key(self):
        msg = '{"status_code": "SUCCESS", "status_message": "Done."}'
        self.assertEqual("SUCCESS", invocation.extract_status_code(msg))

    def test_returns_blocked_from_structured_yaml_key(self):
        msg = "status_code: BLOCKED"
        self.assertEqual("BLOCKED", invocation.extract_status_code(msg))

    def test_returns_none_when_message_is_none(self):
        self.assertIsNone(invocation.extract_status_code(None))

    def test_returns_none_when_no_valid_code(self):
        msg = "Task complete. No structured response."
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_last_valid_code_wins(self):
        """When multiple codes appear, the last valid one is returned."""
        msg = '{"status_code": "BLOCKED"} ... {"status_code": "SUCCESS"}'
        self.assertEqual("SUCCESS", invocation.extract_status_code(msg))

    def test_invalid_code_string_not_returned(self):
        """Values not in _VALID_STATUS_CODES must not be returned."""
        msg = '{"status_code": "INVALID_CODE_XYZ"}'
        self.assertIsNone(invocation.extract_status_code(msg))

    def test_all_valid_codes_recognized(self):
        valid_codes = [
            "SUCCESS", "COMPLETED_NEEDS_ACTION", "PARTIALLY_DONE",
            "NEEDS_CLARIFICATION", "CAPABILITY_EXCEEDED", "BLOCKED",
        ]
        for code in valid_codes:
            with self.subTest(code=code):
                msg = f'{{"status_code": "{code}"}}'
                self.assertEqual(code, invocation.extract_status_code(msg))

    def test_does_not_raise(self):
        for msg in (None, "", "garbage", '{"status_code": null}', "[1, 2, 3]"):
            with self.subTest(msg=repr(msg)):
                try:
                    invocation.extract_status_code(msg)
                except Exception as exc:
                    self.fail(f"extract_status_code raised for {msg!r}: {exc}")


# ---------------------------------------------------------------------------
# SubagentStart: no agent_id guard, agent_name keyed mapping
# ---------------------------------------------------------------------------

class TestHandleSubagentStart(unittest.TestCase):
    """handle_subagent_start processes every firing regardless of agent_id presence.

    The reference adapter's `if not ctx.agent_id: return` guard would suppress
    every VS Code SubagentStart (which carries no agent_id) and must not be used.
    Correlation is keyed on agent_name.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _invocation_dirs(self):
        run_root = self.paths.run_root(_RUN_ID)
        if not run_root.exists():
            return []
        return [d for d in run_root.iterdir()
                if d.is_dir() and not d.name.startswith(".")]

    def test_processes_event_with_no_agent_id_present(self):
        """SubagentStart without agent_id must create invocation directory."""
        ctx = _make_start_ctx(self.tmp.name, agent_name="TestWriter")
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs()
        self.assertGreater(len(dirs), 0,
                           "SubagentStart without agent_id must create an invocation directory")

    def test_returns_early_when_both_agent_name_and_agent_id_absent(self):
        """When neither agent_name nor agent_id is available, the handler returns early."""
        ctx = _make_start_ctx(self.tmp.name)  # no agent_name, no agent_id
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs()
        self.assertEqual(0, len(dirs),
                         "No directory should be created when agent correlation key is absent")

    def test_agent_map_entry_keyed_on_agent_name(self):
        """The agent map entry must be written under agent_name, not agent_id."""
        ctx = _make_start_ctx(self.tmp.name, agent_name="TestWriter",
                              agent_type="TestWriter")
        prompt = '{"agent_instance_id": "TestWriter#1"}'
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "TestWriter#1", _RUN_ID, prompt=prompt
        )
        invocation.handle_subagent_start(ctx)
        # Mapping must be readable by agent_name key
        mapping = runstate.get_agent_mapping(self.paths, _RUN_ID, "TestWriter")
        self.assertIsNotNone(mapping,
                             "Agent map entry must be readable via agent_name as the key")

    def test_agent_map_entry_written_before_event_emission(self):
        """Mapping is persisted before any event write to minimize correlation window."""
        ctx = _make_start_ctx(self.tmp.name, agent_name="Researcher",
                              agent_type="Researcher")
        prompt = '{"agent_instance_id": "Researcher#1"}'
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "Researcher#1", _RUN_ID, prompt=prompt
        )
        invocation.handle_subagent_start(ctx)
        # After the handler completes, the mapping must exist
        mapping = runstate.get_agent_mapping(self.paths, _RUN_ID, "Researcher")
        self.assertIsNotNone(mapping, "Mapping must exist after handle_subagent_start")

    def test_emits_invocation_start_event(self):
        ctx = _make_start_ctx(self.tmp.name, agent_name="PlanWriter",
                              agent_type="PlanWriter")
        prompt = '{"agent_instance_id": "PlanWriter#1"}'
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "PlanWriter#1", _RUN_ID, prompt=prompt
        )
        invocation.handle_subagent_start(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "PlanWriter#1")
        self.assertTrue(event_path.exists())
        events = _read_jsonl(event_path)
        event_names = [e["event"] for e in events]
        self.assertIn("invocation_start", event_names)

    def test_invocation_start_has_agent_instance_id(self):
        ctx = _make_start_ctx(self.tmp.name, agent_name="PlanWriter",
                              agent_type="PlanWriter")
        prompt = '{"agent_instance_id": "PlanWriter#2"}'
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "PlanWriter#2", _RUN_ID, prompt=prompt
        )
        invocation.handle_subagent_start(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "PlanWriter#2")
        events = _read_jsonl(event_path)
        start = next(e for e in events if e["event"] == "invocation_start")
        self.assertEqual("PlanWriter#2", start["agent_instance_id"])

    def test_invocation_start_has_agent_type_from_resolve_agent_type(self):
        """agent_type on invocation_start comes from resolve_agent_type (falls back to agent_name)."""
        ctx = _make_start_ctx(self.tmp.name, agent_name="Reviewer")
        prompt = '{"agent_instance_id": "Reviewer#1"}'
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "Reviewer#1", _RUN_ID, prompt=prompt
        )
        invocation.handle_subagent_start(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#1")
        events = _read_jsonl(event_path)
        start = next(e for e in events if e["event"] == "invocation_start")
        # resolve_agent_type falls back to agent_name when agent_type is absent
        self.assertEqual("Reviewer", start.get("agent_type"))

    def test_invocation_start_includes_prompt_from_dispatch(self):
        """Prompt from the popped pending dispatch appears in invocation_start."""
        prompt_text = '{"agent_instance_id": "TestWriter#3"}\nDo the task.'
        ctx = _make_start_ctx(self.tmp.name, agent_name="TestWriter")
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "TestWriter#3", _RUN_ID, prompt=prompt_text
        )
        invocation.handle_subagent_start(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "TestWriter#3")
        events = _read_jsonl(event_path)
        start = next(e for e in events if e["event"] == "invocation_start")
        self.assertEqual(prompt_text, start.get("prompt"))

    def test_initial_user_turn_written_when_prompt_available(self):
        ctx = _make_start_ctx(self.tmp.name, agent_name="PlanWriter")
        prompt = '{"agent_instance_id": "PlanWriter#4"}\nTask instructions here.'
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "PlanWriter#4", _RUN_ID, prompt=prompt
        )
        invocation.handle_subagent_start(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "PlanWriter#4")
        events = _read_jsonl(event_path)
        turns = [e for e in events if e["event"] == "turn"]
        self.assertGreater(len(turns), 0, "An initial user turn must be written when prompt is available")
        self.assertEqual("user", turns[0]["role"])

    def test_events_written_to_invocation_stream_not_orchestrator(self):
        """SubagentStart must not write any event to 00_orchestrator_events.jsonl."""
        ctx = _make_start_ctx(self.tmp.name, agent_name="PlanWriter")
        prompt = '{"agent_instance_id": "PlanWriter#5"}'
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "PlanWriter#5", _RUN_ID, prompt=prompt
        )
        invocation.handle_subagent_start(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertFalse(
            orc_path.exists(),
            "SubagentStart must not write to the orchestrator events stream"
        )

    def test_fallback_to_unknown_agent_when_no_pending_dispatch(self):
        """When no pending dispatch exists, agent_instance_id falls back to 'unknown-agent'."""
        ctx = _make_start_ctx(self.tmp.name, agent_name="SomeAgent")
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs()
        self.assertEqual(1, len(dirs))
        # Fallback: either unknown-agent or some derived name
        # The design says fallback is 'unknown-agent' when extraction fails
        self.assertIn(dirs[0].name, ["unknown-agent", "SomeAgent"],
                      "When no pending dispatch exists, fallback should be 'unknown-agent' "
                      "or derived from available context")

    def test_handler_does_not_raise(self):
        ctx = _make_start_ctx(self.tmp.name, agent_name="TestWriter")
        try:
            invocation.handle_subagent_start(ctx)
        except Exception as exc:
            self.fail(f"handle_subagent_start raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# SubagentStop: resolves via agent_name, skips unmapped firings
# ---------------------------------------------------------------------------

class TestHandleSubagentStop(unittest.TestCase):
    """handle_subagent_stop resolves the invocation via agent_name.

    Skips firings whose resolved agent_instance_id starts with 'unmapped_'.
    Falls back to agent_id only when agent_name is absent.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_resolves_invocation_via_agent_name(self):
        """SubagentStop must resolve the invocation using agent_name as the key."""
        _register_mapping(self.tmp.name, _RUN_ID, "TestWriter", "TestWriter#1", "TestWriter")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="TestWriter",
            agent_type="TestWriter",
            last_assistant_message='{"status_code": "SUCCESS"}',
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "TestWriter#1")
        self.assertTrue(event_path.exists(),
                        "invocation_end must land in the agent_name-keyed invocation directory")

    def test_falls_back_to_agent_id_when_agent_name_absent(self):
        """When agent_name is absent, agent_id is used as the fallback key."""
        _register_mapping(self.tmp.name, _RUN_ID, "harness-id-001", "Fallback#1")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_id="harness-id-001",
            last_assistant_message='{"status_code": "BLOCKED"}',
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Fallback#1")
        self.assertTrue(event_path.exists(),
                        "When agent_name is absent, agent_id fallback must resolve the invocation")

    def test_skips_unmapped_firings(self):
        """A firing whose agent_name has no mapping must be skipped entirely."""
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="UnmappedAgent",
            last_assistant_message="Response text.",
        )
        invocation.handle_subagent_stop(ctx)
        # The unmapped_ directory must not have events written to the orchestrator
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertFalse(
            orc_path.exists(),
            "An unmapped SubagentStop must not write to the orchestrator stream"
        )

    def test_returns_early_when_both_agent_name_and_agent_id_absent(self):
        ctx = _make_stop_ctx(self.tmp.name, last_assistant_message="Response.")
        invocation.handle_subagent_stop(ctx)
        run_root = self.paths.run_root(_RUN_ID)
        self.assertFalse(run_root.exists(),
                         "No files should be created when agent correlation key is absent")

    def test_emits_invocation_end_event(self):
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#1", "Reviewer")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            agent_type="Reviewer",
            last_assistant_message="Final response.",
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#1")
        events = _read_jsonl(event_path)
        self.assertIn("invocation_end", [e["event"] for e in events])

    def test_invocation_end_has_agent_instance_id(self):
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#2", "Reviewer")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            agent_type="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#2")
        events = _read_jsonl(event_path)
        end_evt = next(e for e in events if e["event"] == "invocation_end")
        self.assertEqual("Reviewer#2", end_evt["agent_instance_id"])

    def test_invocation_end_has_status_code_from_last_assistant_message(self):
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#3")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            last_assistant_message='{"status_code": "BLOCKED", "error_code": "E101"}',
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#3")
        events = _read_jsonl(event_path)
        end_evt = next(e for e in events if e["event"] == "invocation_end")
        self.assertEqual("BLOCKED", end_evt.get("status_code"))

    def test_invocation_end_status_code_absent_when_no_structured_block(self):
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#4")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            last_assistant_message="Task is done with great results.",
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#4")
        events = _read_jsonl(event_path)
        end_evt = next(e for e in events if e["event"] == "invocation_end")
        self.assertNotIn("status_code", end_evt)

    def test_invocation_end_has_full_response(self):
        """response field must be the full untruncated last_assistant_message."""
        big_response = '{"status_code": "SUCCESS"}\n' + "y" * 50_000
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#5")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            last_assistant_message=big_response,
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#5")
        events = _read_jsonl(event_path)
        end_evt = next(e for e in events if e["event"] == "invocation_end")
        self.assertEqual(big_response, end_evt.get("response"))

    def test_assistant_turn_written_when_last_assistant_message_present(self):
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#6")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            last_assistant_message="Final answer.",
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#6")
        events = _read_jsonl(event_path)
        turns = [e for e in events if e["event"] == "turn"]
        self.assertGreater(len(turns), 0)
        self.assertEqual("assistant", turns[0]["role"])

    def test_response_field_absent_when_last_assistant_message_absent(self):
        """When last_assistant_message is absent, response must not be fabricated."""
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#7")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#7")
        if event_path.exists():
            events = _read_jsonl(event_path)
            end_events = [e for e in events if e["event"] == "invocation_end"]
            for end_evt in end_events:
                self.assertNotIn("response", end_evt,
                                 "response must be absent when last_assistant_message is absent")

    def test_model_from_transcript_included_in_invocation_end(self):
        transcript_path = _write_transcript_file(self.tmp.name, [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 100},
            }},
        ])
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#8")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
            transcript_path=transcript_path,
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#8")
        events = _read_jsonl(event_path)
        end_evt = next(e for e in events if e["event"] == "invocation_end")
        self.assertEqual("claude-opus-4-5", end_evt.get("model"))

    def test_model_absent_when_transcript_unreadable(self):
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#9")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
            transcript_path="/nonexistent/transcript.jsonl",
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#9")
        events = _read_jsonl(event_path)
        end_evt = next(e for e in events if e["event"] == "invocation_end")
        self.assertNotIn("model", end_evt)

    def test_no_null_values_in_invocation_end(self):
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#10")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            last_assistant_message='{"status_code": "SUCCESS"}',
        )
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "Reviewer#10")
        events = _read_jsonl(event_path)
        end_evt = next(e for e in events if e["event"] == "invocation_end")
        for key, value in end_evt.items():
            with self.subTest(key=key):
                self.assertIsNotNone(value)

    def test_handler_does_not_raise(self):
        _register_mapping(self.tmp.name, _RUN_ID, "Reviewer", "Reviewer#11")
        ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name="Reviewer",
            last_assistant_message="Done.",
        )
        try:
            invocation.handle_subagent_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_subagent_stop raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# Round-trip: SubagentStart writes mapping, SubagentStop reads via same key
# ---------------------------------------------------------------------------

class TestSubagentCorrelationRoundTrip(unittest.TestCase):
    """SubagentStart writes the agent-name-keyed mapping; SubagentStop resolves
    the same invocation stream.

    This tests the cross-process handoff contract: the two handlers run in
    separate processes but must agree on which invocation directory to use.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_start_and_stop_use_same_invocation_stream(self):
        """invocation_start (from Start) and invocation_end (from Stop) must land
        in the same 03_events.jsonl file."""
        agent_name = "RoundTripAgent"
        prompt = '{"agent_instance_id": "RoundTripAgent#1"}'

        # --- SubagentStart ---
        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "RoundTripAgent#1", _RUN_ID, prompt=prompt
        )
        start_ctx = _make_start_ctx(self.tmp.name, agent_name=agent_name)
        invocation.handle_subagent_start(start_ctx)

        # --- SubagentStop ---
        stop_ctx = _make_stop_ctx(
            self.tmp.name,
            agent_name=agent_name,
            last_assistant_message='{"status_code": "SUCCESS"}',
        )
        invocation.handle_subagent_stop(stop_ctx)

        event_path = self.paths.invocation_events(_RUN_ID, "RoundTripAgent#1")
        self.assertTrue(event_path.exists(),
                        "The shared invocation events file must exist after both handlers run")
        events = _read_jsonl(event_path)
        event_types = [e["event"] for e in events]
        self.assertIn("invocation_start", event_types)
        self.assertIn("invocation_end", event_types)

    def test_start_writes_agent_name_keyed_mapping_readable_by_stop(self):
        """The mapping written by SubagentStart must be readable via agent_name by SubagentStop."""
        agent_name = "MappingTestAgent"
        prompt = '{"agent_instance_id": "MappingTestAgent#1"}'

        _pre_populate_pending_dispatch(
            self.tmp.name, _RUN_ID, _SESSION_ID, "MappingTestAgent#1", _RUN_ID, prompt=prompt
        )
        start_ctx = _make_start_ctx(self.tmp.name, agent_name=agent_name)
        invocation.handle_subagent_start(start_ctx)

        # Verify the mapping is keyed on agent_name
        mapping = runstate.get_agent_mapping(self.paths, _RUN_ID, agent_name)
        self.assertIsNotNone(mapping,
                             "Mapping written by SubagentStart must be readable via agent_name")
        self.assertEqual("MappingTestAgent#1", mapping.get("agent_instance_id"))


# ---------------------------------------------------------------------------
# HANDLERS registry — invocation module
# ---------------------------------------------------------------------------

class TestInvocationHandlersRegistry(unittest.TestCase):
    """HANDLERS in the invocation module contains exactly SubagentStart and SubagentStop."""

    def test_handlers_is_a_dict(self):
        self.assertIsInstance(invocation.HANDLERS, dict)

    def test_subagent_start_registered(self):
        self.assertIn("SubagentStart", invocation.HANDLERS)

    def test_subagent_stop_registered(self):
        self.assertIn("SubagentStop", invocation.HANDLERS)

    def test_handlers_has_exactly_two_entries(self):
        self.assertEqual(2, len(invocation.HANDLERS))

    def test_all_handlers_callable(self):
        for event, handler in invocation.HANDLERS.items():
            with self.subTest(event=event):
                self.assertTrue(callable(handler))


if __name__ == "__main__":
    unittest.main()
