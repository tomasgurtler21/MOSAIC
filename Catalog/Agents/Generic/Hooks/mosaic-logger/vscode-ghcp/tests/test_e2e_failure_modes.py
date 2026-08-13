"""End-to-end failure-mode and transcript-degradation tests for the vscode-ghcp adapter.

Verifies the process-level safety contract and graceful degradation behavior:

Process-level safety (T4.2):
  For each of the 8 supported events plus an unknown event name, all of the
  following input types must produce exit code 0 and empty stdout:
  - Malformed JSON (not parseable)
  - Non-object JSON (array, string, number, null)
  - Empty stdin
  - Minimal well-formed payload (only hook_event_name, missing all optional fields)

Transcript degradation (T4.3):
  - An unreadable or absent transcript_path leaves model and token_usage omitted
    from emitted events (not null — absent entirely)
  - A malformed/unparseable transcript has the same omission behavior
  - No emitted event line contains a null value under any degradation path

vscode-ghcp-specific notes:
  - The adapter is invoked as 'py mosaic_logger.py' — the working Python launcher
    on this machine (not 'python' or 'python3')
  - Workspace root comes from payload['cwd'], not from CLAUDE_PROJECT_DIR
  - SubagentStart uses agent_name (not agent_id) as the correlation key (D3)
  - Every PostToolUse records status='success' (no failure event for VS Code)
"""

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core


_ADAPTER_PATH = str(
    pathlib.Path(__file__).parent.parent / "mosaic_logger.py"
)

_FIXTURE_DIR = pathlib.Path(__file__).parent / "fixtures"
_SAMPLE_TRANSCRIPT = _FIXTURE_DIR / "sample_transcript.jsonl"


# ---------------------------------------------------------------------------
# Subprocess helper
# ---------------------------------------------------------------------------

def _run_adapter(
    stdin_bytes: bytes,
    timeout: int = 15,
) -> subprocess.CompletedProcess:
    """Run mosaic_logger.py as a subprocess with the given stdin bytes.

    Uses 'py' as the Python launcher, matching the working alias on this machine
    and matching the configuration in the adapter's hook.yaml.
    No extra environment variables are set; workspace comes from payload['cwd'].
    """
    return subprocess.run(
        ["py", _ADAPTER_PATH],
        input=stdin_bytes,
        capture_output=True,
        timeout=timeout,
    )


def _payload(event: str, tmp_path: "str | None" = None, **extra) -> bytes:
    """Build a minimal JSON payload for the given event name."""
    p: dict = {"hook_event_name": event}
    if tmp_path is not None:
        p["cwd"] = tmp_path
    p.update(extra)
    return json.dumps(p).encode()


# ---------------------------------------------------------------------------
# TestExitCodeForAllInputTypes — subprocess, exit 0 invariant
# ---------------------------------------------------------------------------

class TestExitCodeForAllInputTypes(unittest.TestCase):
    """Every documented input type — malformed, non-object, empty, and minimal —
    must exit 0 for each of the 8 registered events and for an unknown event name.

    The adapter must never exit non-zero regardless of input quality (AC4.5).
    """

    # The 8 registered event names.
    _REGISTERED_EVENTS = [
        "SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
        "PreCompact", "SubagentStart", "SubagentStop", "Stop",
    ]
    # One unknown event name (not in the registry).
    _UNKNOWN_EVENT = "NonExistentEventXYZ"

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = self.tmp.name

    def tearDown(self):
        self.tmp.cleanup()

    def _assert_exit_0(self, stdin_bytes: bytes, label: str) -> None:
        result = _run_adapter(stdin_bytes)
        self.assertEqual(
            0, result.returncode,
            f"Expected exit 0 for {label!r}, got {result.returncode}"
        )

    # --- Malformed JSON (one check covers all events — the adapter fails before
    #     dispatch, so event name is irrelevant) ---

    def test_malformed_json_exits_0(self):
        self._assert_exit_0(b"not json at all {{{", "malformed JSON")

    def test_truncated_json_exits_0(self):
        self._assert_exit_0(b'{"hook_event_name": "Session', "truncated JSON")

    # --- Empty stdin ---

    def test_empty_stdin_exits_0(self):
        self._assert_exit_0(b"", "empty stdin")

    # --- Non-object JSON root types ---

    def test_json_array_exits_0(self):
        self._assert_exit_0(b"[1, 2, 3]", "JSON array")

    def test_json_string_exits_0(self):
        self._assert_exit_0(b'"just a string"', "JSON string")

    def test_json_number_exits_0(self):
        self._assert_exit_0(b"42", "JSON number")

    def test_json_null_exits_0(self):
        self._assert_exit_0(b"null", "JSON null")

    def test_json_boolean_exits_0(self):
        self._assert_exit_0(b"true", "JSON boolean")

    # --- Unknown event name ---

    def test_unknown_event_name_exits_0(self):
        self._assert_exit_0(
            _payload(self._UNKNOWN_EVENT, self.tmp_path),
            f"unknown event {self._UNKNOWN_EVENT!r}"
        )

    # --- Minimal payload (hook_event_name only, all other fields absent) for each event ---

    def test_session_start_minimal_payload_exits_0(self):
        self._assert_exit_0(
            _payload("SessionStart", self.tmp_path),
            "SessionStart minimal payload"
        )

    def test_user_prompt_submit_minimal_payload_exits_0(self):
        self._assert_exit_0(
            _payload("UserPromptSubmit", self.tmp_path),
            "UserPromptSubmit minimal payload"
        )

    def test_pre_tool_use_minimal_payload_exits_0(self):
        self._assert_exit_0(
            _payload("PreToolUse", self.tmp_path),
            "PreToolUse minimal payload"
        )

    def test_post_tool_use_minimal_payload_exits_0(self):
        self._assert_exit_0(
            _payload("PostToolUse", self.tmp_path),
            "PostToolUse minimal payload"
        )

    def test_pre_compact_minimal_payload_exits_0(self):
        self._assert_exit_0(
            _payload("PreCompact", self.tmp_path),
            "PreCompact minimal payload"
        )

    def test_subagent_start_minimal_payload_exits_0(self):
        """SubagentStart with no agent_name and no agent_id: resolve_agent_key returns
        None, so the handler returns immediately — a safe graceful no-op."""
        self._assert_exit_0(
            _payload("SubagentStart", self.tmp_path),
            "SubagentStart minimal payload (no agent_name, no agent_id)"
        )

    def test_subagent_stop_minimal_payload_exits_0(self):
        self._assert_exit_0(
            _payload("SubagentStop", self.tmp_path),
            "SubagentStop minimal payload"
        )

    def test_stop_minimal_payload_exits_0(self):
        self._assert_exit_0(
            _payload("Stop", self.tmp_path),
            "Stop minimal payload"
        )

    # --- Malformed payload for each event (using subTest for compactness) ---

    def test_malformed_json_exits_0_for_all_events(self):
        """Malformed JSON must exit 0 regardless of which event would have been fired."""
        for event in self._REGISTERED_EVENTS + [self._UNKNOWN_EVENT]:
            with self.subTest(event=event):
                # Payload that starts valid then breaks mid-parse.
                bad = f'{{"hook_event_name": "{event}", "junk": '.encode()
                self._assert_exit_0(bad, f"malformed JSON for {event!r}")

    def test_non_object_json_exits_0_for_each_type(self):
        """Non-object JSON root types all exit 0."""
        for bad_input, label in [
            (b"[1,2]", "array"),
            (b'"string"', "string"),
            (b"99", "number"),
            (b"null", "null"),
            (b"false", "boolean"),
        ]:
            with self.subTest(type=label):
                self._assert_exit_0(bad_input, label)


# ---------------------------------------------------------------------------
# TestEmptyStdoutForAllInputTypes — subprocess, no stdout invariant
# ---------------------------------------------------------------------------

class TestEmptyStdoutForAllInputTypes(unittest.TestCase):
    """Every input type must produce empty stdout — no JSON, no decision output,
    no diagnostic text.

    VS Code parses stdout as a hook-output control object on exit 0, so any stray
    output could alter the user's session (AC4.5).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = self.tmp.name

    def tearDown(self):
        self.tmp.cleanup()

    def _assert_empty_stdout(self, stdin_bytes: bytes, label: str) -> None:
        result = _run_adapter(stdin_bytes)
        self.assertEqual(
            b"", result.stdout,
            f"Expected empty stdout for {label!r}, got {result.stdout!r}"
        )

    def test_malformed_json_produces_no_stdout(self):
        self._assert_empty_stdout(b"not json {{{", "malformed JSON")

    def test_empty_stdin_produces_no_stdout(self):
        self._assert_empty_stdout(b"", "empty stdin")

    def test_json_array_produces_no_stdout(self):
        self._assert_empty_stdout(b"[1, 2]", "JSON array")

    def test_json_null_produces_no_stdout(self):
        self._assert_empty_stdout(b"null", "JSON null")

    def test_unknown_event_produces_no_stdout(self):
        self._assert_empty_stdout(
            _payload("NoSuchEventXYZ", self.tmp_path),
            "unknown event"
        )

    def test_session_start_produces_no_stdout(self):
        self._assert_empty_stdout(
            _payload("SessionStart", self.tmp_path, session_id="s1", source="startup"),
            "SessionStart"
        )

    def test_user_prompt_submit_produces_no_stdout(self):
        self._assert_empty_stdout(
            _payload("UserPromptSubmit", self.tmp_path, session_id="s1",
                     prompt="hello world"),
            "UserPromptSubmit"
        )

    def test_pre_tool_use_produces_no_stdout(self):
        self._assert_empty_stdout(
            _payload("PreToolUse", self.tmp_path, session_id="s1",
                     tool_name="Bash", tool_input={"command": "ls"}),
            "PreToolUse"
        )

    def test_post_tool_use_produces_no_stdout(self):
        self._assert_empty_stdout(
            _payload("PostToolUse", self.tmp_path, session_id="s1",
                     tool_name="Bash",
                     tool_result={"text_result_for_llm": "output text"}),
            "PostToolUse"
        )

    def test_stop_produces_no_stdout(self):
        self._assert_empty_stdout(
            _payload("Stop", self.tmp_path, session_id="s1",
                     stop_reason="end_turn", stop_hook_active=False),
            "Stop"
        )

    def test_all_eight_registered_events_produce_no_stdout(self):
        """Every one of the 8 registered events must produce empty stdout."""
        events_with_minimal_extras = {
            "SessionStart": {},
            "UserPromptSubmit": {},
            "PreToolUse": {},
            "PostToolUse": {},
            "PreCompact": {},
            "SubagentStart": {},
            "SubagentStop": {},
            "Stop": {},
        }
        for event, extra in events_with_minimal_extras.items():
            with self.subTest(event=event):
                stdin_bytes = _payload(event, self.tmp_path, session_id="s1", **extra)
                self._assert_empty_stdout(stdin_bytes, event)


# ---------------------------------------------------------------------------
# TestTranscriptDegradation — T4.3, AC4.6
# ---------------------------------------------------------------------------

class TestTranscriptDegradation(unittest.TestCase):
    """An unreadable or absent transcript_path leaves model and token_usage omitted
    from emitted events — not null values, not empty strings: absent entirely.

    Verifies AC4.6 (missing/unparseable transcript → model and token_usage omitted)
    and the AC4.4 no-null requirement under degradation conditions.

    Tests are in-process (via dispatch()) for precise event inspection, with a
    subprocess guard confirming exit 0 on the same degraded paths.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _dispatch(self, payload: dict) -> None:
        """Dispatch a payload, asserting dispatch() does not raise."""
        try:
            mosaic_logger.dispatch(json.dumps(payload))
        except Exception as exc:
            self.fail(f"dispatch() raised unexpectedly: {exc!r}")

    def _read_events(self, path: pathlib.Path) -> list:
        if not path.exists():
            return []
        return [
            json.loads(ln)
            for ln in path.read_text("utf-8").splitlines()
            if ln.strip()
        ]

    def _find_run_id(self) -> "str | None":
        import re
        _RUN_ID_RE = re.compile(r"^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")
        log_root = core.build_paths(self.tmp_path).root
        if not log_root.exists():
            return None
        for child in log_root.iterdir():
            if child.is_dir() and _RUN_ID_RE.match(child.name):
                return child.name
        unknown_run = log_root / "unknown-run"
        if unknown_run.exists():
            return "unknown-run"
        return None

    # ------------------------------------------------------------------
    # Stop event with unreadable transcript
    # ------------------------------------------------------------------

    def test_stop_with_absent_transcript_omits_model_and_token_usage(self):
        """Stop with transcript_path pointing to a nonexistent file produces a
        session_end event (and an assistant turn if last_assistant_message was
        present) with neither model nor token_usage — never null for those fields."""
        session_id = "degrad-stop-absent-001"
        cwd = str(self.tmp_path)
        self._dispatch({
            "hook_event_name": "SessionStart",
            "session_id": session_id,
            "source": "startup",
            "cwd": cwd,
        })
        self._dispatch({
            "hook_event_name": "Stop",
            "session_id": session_id,
            "cwd": cwd,
            "transcript_path": "/nonexistent/path/transcript.jsonl",
            "last_assistant_message": "Orchestration complete.",
            "stop_reason": "end_turn",
        })

        run_id = self._find_run_id()
        self.assertIsNotNone(run_id)
        paths = core.build_paths(self.tmp_path)
        events = self._read_events(paths.orchestrator_events(run_id))

        # The assistant turn emitted by handle_stop must not carry model or token_usage.
        assistant_turns = [
            e for e in events
            if e.get("event") == "turn" and e.get("role") == "assistant"
        ]
        self.assertGreater(
            len(assistant_turns), 0,
            "An assistant turn must be emitted when last_assistant_message is present"
        )
        for turn in assistant_turns:
            self.assertNotIn(
                "model", turn,
                "model must be absent from turn when transcript is unreadable"
            )
            self.assertNotIn(
                "token_usage", turn,
                "token_usage must be absent from turn when transcript is unreadable"
            )

    def test_stop_with_absent_transcript_still_emits_session_end(self):
        """session_end must be emitted unconditionally on Stop, even when the
        transcript is unreadable (AC4.5, D5)."""
        session_id = "degrad-stop-session-end-001"
        cwd = str(self.tmp_path)
        for p in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup", "cwd": cwd},
            {"hook_event_name": "Stop", "session_id": session_id, "cwd": cwd,
             "transcript_path": "/no/such/file.jsonl",
             "stop_reason": "end_turn"},
        ]:
            self._dispatch(p)

        run_id = self._find_run_id()
        self.assertIsNotNone(run_id)
        paths = core.build_paths(self.tmp_path)
        events = self._read_events(paths.orchestrator_events(run_id))
        session_end_events = [e for e in events if e.get("event") == "session_end"]
        self.assertEqual(
            1, len(session_end_events),
            "Exactly one session_end must be emitted even when transcript is unreadable"
        )

    def test_stop_with_malformed_transcript_omits_model_and_token_usage(self):
        """Stop with transcript_path pointing to a file containing invalid JSON
        produces events with model and token_usage absent."""
        session_id = "degrad-stop-malformed-001"
        cwd = str(self.tmp_path)

        # Write a file that parse_transcript_records cannot parse.
        bad_transcript = self.tmp_path / "bad_transcript.txt"
        bad_transcript.write_text("this is not json\nnot json either\n", "utf-8")

        self._dispatch({
            "hook_event_name": "SessionStart",
            "session_id": session_id,
            "source": "startup",
            "cwd": cwd,
        })
        self._dispatch({
            "hook_event_name": "Stop",
            "session_id": session_id,
            "cwd": cwd,
            "transcript_path": str(bad_transcript),
            "last_assistant_message": "Orchestration complete.",
            "stop_reason": "end_turn",
        })

        run_id = self._find_run_id()
        self.assertIsNotNone(run_id)
        paths = core.build_paths(self.tmp_path)
        events = self._read_events(paths.orchestrator_events(run_id))

        assistant_turns = [
            e for e in events
            if e.get("event") == "turn" and e.get("role") == "assistant"
        ]
        for turn in assistant_turns:
            self.assertNotIn("model", turn,
                             "model must be absent when transcript is malformed")
            self.assertNotIn("token_usage", turn,
                             "token_usage must be absent when transcript is malformed")

    # ------------------------------------------------------------------
    # SubagentStop with unreadable transcript
    # ------------------------------------------------------------------

    def test_subagent_stop_with_absent_transcript_omits_model_and_token_usage(self):
        """SubagentStop with transcript_path pointing to a nonexistent file produces
        invocation_end with model and token_usage absent (AC4.6)."""
        session_id = "degrad-inv-absent-001"
        run_id_str = "20260101T130000Z-ef01"
        agent_name = "Research"
        instance_id = "Research#2"
        cwd = str(self.tmp_path)

        # Full correlation chain: SessionStart → UserPromptSubmit (embeds run_id)
        # → PreToolUse (pending dispatch) → SubagentStart → SubagentStop.
        for p in [
            {
                "hook_event_name": "SessionStart",
                "session_id": session_id,
                "source": "startup",
                "cwd": cwd,
            },
            {
                "hook_event_name": "UserPromptSubmit",
                "session_id": session_id,
                "cwd": cwd,
                "prompt": f"run_id: {run_id_str} task: test",
            },
            {
                "hook_event_name": "PreToolUse",
                "session_id": session_id,
                "cwd": cwd,
                "tool_name": "Agent",
                "tool_use_id": "call-degrad-001",
                "tool_input": {
                    "prompt": (
                        f"agent_instance_id: {instance_id}\n"
                        f"run_id: {run_id_str}\n"
                        "task: conduct analysis"
                    )
                },
            },
            {
                "hook_event_name": "SubagentStart",
                "session_id": session_id,
                "cwd": cwd,
                "agent_name": agent_name,
            },
        ]:
            self._dispatch(p)

        # SubagentStop with an unreadable transcript path.
        self._dispatch({
            "hook_event_name": "SubagentStop",
            "session_id": session_id,
            "cwd": cwd,
            "agent_id": "agt-degrad-001",
            "agent_name": agent_name,
            "transcript_path": "/nonexistent/path/transcript.jsonl",
            "last_assistant_message": (
                f'{{"status_code": "SUCCESS", "run_id": "{run_id_str}"}}'
            ),
        })

        found_run_id = self._find_run_id()
        self.assertIsNotNone(found_run_id)
        paths = core.build_paths(self.tmp_path)
        inv_events_path = paths.invocation_events(found_run_id, instance_id)
        inv_events = self._read_events(inv_events_path)

        inv_end = next((e for e in inv_events if e.get("event") == "invocation_end"), None)
        self.assertIsNotNone(inv_end, "invocation_end must be emitted even with missing transcript")
        self.assertNotIn(
            "model", inv_end,
            "model must be absent from invocation_end when transcript is unreadable"
        )
        self.assertNotIn(
            "token_usage", inv_end,
            "token_usage must be absent from invocation_end when transcript is unreadable"
        )

    def test_subagent_stop_with_absent_transcript_still_emits_invocation_end(self):
        """invocation_end must be emitted even when the transcript path is absent."""
        session_id = "degrad-inv-emitted-001"
        run_id_str = "20260101T140000Z-ff02"
        agent_name = "TestWriter"
        instance_id = "TestWriter#3"
        cwd = str(self.tmp_path)

        for p in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup", "cwd": cwd},
            {"hook_event_name": "UserPromptSubmit", "session_id": session_id,
             "cwd": cwd, "prompt": f"run_id: {run_id_str} write tests"},
            {"hook_event_name": "PreToolUse", "session_id": session_id,
             "cwd": cwd, "tool_name": "Agent", "tool_use_id": "call-emitted-001",
             "tool_input": {
                 "prompt": f"agent_instance_id: {instance_id}\nrun_id: {run_id_str}"
             }},
            {"hook_event_name": "SubagentStart", "session_id": session_id,
             "cwd": cwd, "agent_name": agent_name},
            {"hook_event_name": "SubagentStop", "session_id": session_id,
             "cwd": cwd, "agent_id": "agt-emitted-001", "agent_name": agent_name,
             "transcript_path": "/no/such/file.jsonl",
             "last_assistant_message": (
                 f'{{"status_code": "SUCCESS", "run_id": "{run_id_str}"}}'
             )},
        ]:
            self._dispatch(p)

        found_run_id = self._find_run_id()
        self.assertIsNotNone(found_run_id)
        paths = core.build_paths(self.tmp_path)
        inv_events = self._read_events(paths.invocation_events(found_run_id, instance_id))
        inv_ends = [e for e in inv_events if e.get("event") == "invocation_end"]
        self.assertEqual(
            1, len(inv_ends),
            "Exactly one invocation_end must be emitted even with unreadable transcript"
        )

    # ------------------------------------------------------------------
    # No-null invariant under all degradation paths
    # ------------------------------------------------------------------

    def test_no_null_values_when_transcript_absent_for_stop(self):
        """No emitted event must carry a null value even when degradation occurs."""
        session_id = "nonull-stop-001"
        cwd = str(self.tmp_path)
        for p in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup", "cwd": cwd},
            {"hook_event_name": "Stop", "session_id": session_id, "cwd": cwd,
             "transcript_path": "/no/such/transcript.jsonl",
             "last_assistant_message": "Done.", "stop_reason": "end_turn"},
        ]:
            self._dispatch(p)

        run_id = self._find_run_id()
        self.assertIsNotNone(run_id)
        paths = core.build_paths(self.tmp_path)
        events_path = paths.orchestrator_events(run_id)
        events = self._read_events(events_path)
        for ev in events:
            self.assertFalse(
                _contains_null(ev),
                f"Null value found in event {ev.get('event')!r} under degradation: {ev}"
            )

    def test_no_null_values_when_transcript_absent_for_subagent_stop(self):
        """No invocation event carries a null value even when transcript is absent."""
        session_id = "nonull-inv-001"
        run_id_str = "20260101T150000Z-aa03"
        agent_name = "Reviewer"
        instance_id = "Reviewer#4"
        cwd = str(self.tmp_path)

        for p in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup", "cwd": cwd},
            {"hook_event_name": "UserPromptSubmit", "session_id": session_id,
             "cwd": cwd, "prompt": f"run_id: {run_id_str} review"},
            {"hook_event_name": "PreToolUse", "session_id": session_id,
             "cwd": cwd, "tool_name": "Agent", "tool_use_id": "call-nonull-001",
             "tool_input": {
                 "prompt": f"agent_instance_id: {instance_id}\nrun_id: {run_id_str}"
             }},
            {"hook_event_name": "SubagentStart", "session_id": session_id,
             "cwd": cwd, "agent_name": agent_name},
            {"hook_event_name": "SubagentStop", "session_id": session_id,
             "cwd": cwd, "agent_id": "agt-nonull-001", "agent_name": agent_name,
             "transcript_path": "/no/such/transcript.jsonl",
             "last_assistant_message": (
                 f'{{"status_code": "SUCCESS", "run_id": "{run_id_str}"}}'
             )},
        ]:
            self._dispatch(p)

        found_run_id = self._find_run_id()
        self.assertIsNotNone(found_run_id)
        paths = core.build_paths(self.tmp_path)
        inv_events = self._read_events(paths.invocation_events(found_run_id, instance_id))
        for ev in inv_events:
            self.assertFalse(
                _contains_null(ev),
                f"Null value found in invocation event {ev.get('event')!r}: {ev}"
            )

    def test_no_null_values_for_session_with_no_optional_fields(self):
        """A minimal SessionStart→Stop sequence produces no null values even when
        all optional fields (model, transcript_path, session_id, etc.) are absent."""
        cwd = str(self.tmp_path)
        mosaic_logger.dispatch(json.dumps({
            "hook_event_name": "SessionStart",
            "cwd": cwd,
        }))
        mosaic_logger.dispatch(json.dumps({
            "hook_event_name": "Stop",
            "cwd": cwd,
        }))

        run_id = self._find_run_id()
        if run_id is None:
            return  # Nothing was written — no events to check
        paths = core.build_paths(self.tmp_path)
        events_path = paths.orchestrator_events(run_id)
        if not events_path.exists():
            return
        events = self._read_events(events_path)
        for ev in events:
            self.assertFalse(
                _contains_null(ev),
                f"Null value found in event {ev.get('event')!r}: {ev}"
            )

    # ------------------------------------------------------------------
    # Subprocess guard — degradation paths also exit 0
    # ------------------------------------------------------------------

    def test_subagent_stop_with_missing_transcript_exits_0_subprocess(self):
        """Subprocess-level confirmation: exit 0 when transcript is unreadable."""
        with tempfile.TemporaryDirectory() as tmp:
            # Establish session and agent mapping first (each in its own subprocess call).
            session_id = "degrad-proc-001"
            run_id_str = "20260101T160000Z-bb04"
            agent_name = "Orchestrator"
            instance_id = "Orchestrator#5"
            cwd = tmp

            setup_payloads = [
                {"hook_event_name": "SessionStart", "session_id": session_id,
                 "source": "startup", "cwd": cwd},
                {"hook_event_name": "UserPromptSubmit", "session_id": session_id,
                 "cwd": cwd, "prompt": f"run_id: {run_id_str}"},
                {"hook_event_name": "PreToolUse", "session_id": session_id,
                 "cwd": cwd, "tool_name": "Agent", "tool_use_id": "call-proc-001",
                 "tool_input": {
                     "prompt": f"agent_instance_id: {instance_id}\nrun_id: {run_id_str}"
                 }},
                {"hook_event_name": "SubagentStart", "session_id": session_id,
                 "cwd": cwd, "agent_name": agent_name},
            ]
            for p in setup_payloads:
                r = _run_adapter(json.dumps(p).encode())
                self.assertEqual(0, r.returncode,
                                 f"Setup step {p['hook_event_name']!r} must exit 0")

            # SubagentStop with unreadable transcript.
            stop_p = {
                "hook_event_name": "SubagentStop",
                "session_id": session_id,
                "cwd": cwd,
                "agent_id": "agt-proc-001",
                "agent_name": agent_name,
                "transcript_path": "/nonexistent/transcript.jsonl",
                "last_assistant_message": (
                    f'{{"status_code": "SUCCESS", "run_id": "{run_id_str}"}}'
                ),
            }
            result = _run_adapter(json.dumps(stop_p).encode())
            self.assertEqual(0, result.returncode,
                             "SubagentStop with missing transcript must exit 0")
            self.assertEqual(b"", result.stdout,
                             "SubagentStop with missing transcript must produce no stdout")

    def test_stop_with_missing_transcript_exits_0_subprocess(self):
        """Subprocess-level: Stop with absent transcript_path exits 0."""
        with tempfile.TemporaryDirectory() as tmp:
            cwd = tmp
            session_id = "degrad-stop-proc-001"
            # SessionStart first.
            r = _run_adapter(_payload(
                "SessionStart", cwd, session_id=session_id, source="startup"
            ))
            self.assertEqual(0, r.returncode)

            # Stop with nonexistent transcript.
            r = _run_adapter(json.dumps({
                "hook_event_name": "Stop",
                "session_id": session_id,
                "cwd": cwd,
                "transcript_path": "/no/such/path.jsonl",
                "stop_reason": "end_turn",
            }).encode())
            self.assertEqual(0, r.returncode)
            self.assertEqual(b"", r.stdout)


# ---------------------------------------------------------------------------
# Shared helper (used in degradation tests)
# ---------------------------------------------------------------------------

def _contains_null(obj: object) -> bool:
    """Return True if obj or any value nested inside it is None (JSON null)."""
    if obj is None:
        return True
    if isinstance(obj, dict):
        return any(_contains_null(v) for v in obj.values())
    if isinstance(obj, list):
        return any(_contains_null(v) for v in obj)
    return False


if __name__ == "__main__":
    unittest.main()
