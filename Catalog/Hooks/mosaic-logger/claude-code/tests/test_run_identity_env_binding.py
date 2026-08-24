"""Tests for run-identity binding via the MOSAIC_RUN_ID environment variable.

Covers the three behavioural properties added or clarified by Stage 7:

T7.1 — From the first event onward (when MOSAIC_RUN_ID is set in the
  environment), a session's events are attributed to the run's own bucket
  rather than the fallback bucket.

T7.2 — The accepted limit: events emitted without MOSAIC_RUN_ID set and
  before any protocol-compliant collaborator dispatch may remain in the
  fallback bucket (unknown-run). This raises no error and corrupts nothing.
  Once a protocol-compliant dispatch arrives, subsequent events switch to the
  correct bucket.

T7.3 — A session that never yields an extractable run id (no MOSAIC_RUN_ID,
  no protocol-compliant dispatch, no extractable run id in any payload field)
  degrades exactly as documented: events route to unknown-run, no exception is
  raised, and no identity is invented.

RED-phase note: All tests in this file will FAIL until Stage 7 is implemented.
The changes needed are:
  - runstate.is_valid_run_id does not exist yet
  - resolve_run_identity in mosaic_logger does not read MOSAIC_RUN_ID yet
"""

import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

_VALID_RUN_ID = "20260101T090000Z-a1b2"
_VALID_RUN_ID_ALT = "20260101T100000Z-c3d4"

_ENV_VAR = "MOSAIC_RUN_ID"
_PROJECT_DIR_VAR = "CLAUDE_PROJECT_DIR"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _dispatch(payload: dict, tmp_path: pathlib.Path,
              mosaic_run_id: "str | None" = None) -> None:
    """Dispatch a hook payload, optionally with MOSAIC_RUN_ID set.

    Always sets CLAUDE_PROJECT_DIR so the bundle writes into tmp_path.
    Restores all environment variables after dispatch.
    """
    saved_project = os.environ.get(_PROJECT_DIR_VAR)
    saved_run_id = os.environ.get(_ENV_VAR)

    os.environ[_PROJECT_DIR_VAR] = str(tmp_path)
    if mosaic_run_id is not None:
        os.environ[_ENV_VAR] = mosaic_run_id
    else:
        os.environ.pop(_ENV_VAR, None)

    try:
        mosaic_logger.dispatch(json.dumps(payload))
    finally:
        # Restore CLAUDE_PROJECT_DIR
        if saved_project is None:
            os.environ.pop(_PROJECT_DIR_VAR, None)
        else:
            os.environ[_PROJECT_DIR_VAR] = saved_project

        # Restore MOSAIC_RUN_ID
        if saved_run_id is None:
            os.environ.pop(_ENV_VAR, None)
        else:
            os.environ[_ENV_VAR] = saved_run_id


def _read_events(path: pathlib.Path) -> list:
    """Return parsed JSONL lines from a file, or [] if absent."""
    if not path.exists():
        return []
    lines = path.read_text(encoding="utf-8").splitlines()
    return [json.loads(line) for line in lines if line.strip()]


def _logs_root(tmp_path: pathlib.Path) -> pathlib.Path:
    return tmp_path / "OrchestrationLogs"


def _run_folder(tmp_path: pathlib.Path, run_id: str) -> pathlib.Path:
    return _logs_root(tmp_path) / run_id


def _unknown_run_folder(tmp_path: pathlib.Path) -> pathlib.Path:
    return _logs_root(tmp_path) / "unknown-run"


# ---------------------------------------------------------------------------
# T7.0 — is_valid_run_id unit tests
#
# The design references runstate.is_valid_run_id as the gating check before
# the env-var binding is adopted. This function does not exist yet.
# ---------------------------------------------------------------------------

class TestIsValidRunId(unittest.TestCase):
    """runstate.is_valid_run_id validates run ids against the canonical format.

    Fails in RED phase: the function does not exist in mosaic_logger_runstate.
    """

    def test_accepts_canonical_format(self):
        self.assertTrue(runstate.is_valid_run_id("20260101T090000Z-a1b2"))

    def test_accepts_canonical_format_with_digits_in_suffix(self):
        self.assertTrue(runstate.is_valid_run_id("20260822T112500Z-c9d2"))

    def test_rejects_empty_string(self):
        self.assertFalse(runstate.is_valid_run_id(""))

    def test_rejects_none(self):
        self.assertFalse(runstate.is_valid_run_id(None))

    def test_rejects_unknown_run_literal(self):
        self.assertFalse(runstate.is_valid_run_id("unknown-run"))

    def test_rejects_uppercase_suffix(self):
        """Suffix must be lowercase hex; uppercase must be rejected."""
        self.assertFalse(runstate.is_valid_run_id("20260101T090000Z-A1B2"))

    def test_rejects_short_suffix(self):
        self.assertFalse(runstate.is_valid_run_id("20260101T090000Z-a1b"))

    def test_rejects_long_suffix(self):
        self.assertFalse(runstate.is_valid_run_id("20260101T090000Z-a1b2c"))

    def test_rejects_missing_dash(self):
        self.assertFalse(runstate.is_valid_run_id("20260101T090000Za1b2"))

    def test_rejects_arbitrary_string(self):
        self.assertFalse(runstate.is_valid_run_id("not-a-run-id"))

    def test_rejects_partial_format(self):
        self.assertFalse(runstate.is_valid_run_id("20260101T090000Z"))

    def test_rejects_format_with_leading_text(self):
        """Leading characters must cause rejection — not a substring match."""
        self.assertFalse(runstate.is_valid_run_id("prefix-20260101T090000Z-a1b2"))

    def test_rejects_format_with_trailing_text(self):
        self.assertFalse(runstate.is_valid_run_id("20260101T090000Z-a1b2-extra"))

    def test_never_raises_for_any_input(self):
        for val in [None, "", "unknown-run", 42, [], {}, "20260101T090000Z-a1b2"]:
            try:
                runstate.is_valid_run_id(val)
            except Exception as exc:
                self.fail(f"is_valid_run_id raised for {val!r}: {exc}")


# ---------------------------------------------------------------------------
# T7.1 — Events attributed to the run's bucket when MOSAIC_RUN_ID is set
# ---------------------------------------------------------------------------

class TestEnvVarBinding(unittest.TestCase):
    """When MOSAIC_RUN_ID is set, events route to the named run's folder.

    All tests fail in RED phase: resolve_run_identity does not yet read
    MOSAIC_RUN_ID, so events land in unknown-run instead of the run folder.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_start_with_env_var_creates_run_folder_not_unknown_run(self):
        """SessionStart with MOSAIC_RUN_ID set must create the run's folder,
        not the unknown-run fallback bucket."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "env-bind-001",
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id=_VALID_RUN_ID)

        run_folder = _run_folder(self.tmp_path, _VALID_RUN_ID)
        self.assertTrue(
            run_folder.exists(),
            f"Run folder OrchestrationLogs/{_VALID_RUN_ID}/ was not created "
            "even though MOSAIC_RUN_ID was set — env var binding is not implemented"
        )

    def test_session_start_with_env_var_does_not_create_unknown_run_folder(self):
        """When MOSAIC_RUN_ID is set, unknown-run/ must not receive the event."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "env-bind-002",
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id=_VALID_RUN_ID)

        unknown_run = _unknown_run_folder(self.tmp_path)
        if unknown_run.exists():
            files = list(unknown_run.rglob("*"))
            files_only = [f for f in files if f.is_file()]
            self.assertEqual(
                0, len(files_only),
                "SessionStart with MOSAIC_RUN_ID set wrote events to unknown-run/ "
                "instead of (or in addition to) the run's own folder"
            )

    def test_session_start_env_var_writes_session_start_event_to_run_folder(self):
        """The session_start event appears in the run's orchestrator events file."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "env-bind-003",
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id=_VALID_RUN_ID)

        events_file = _run_folder(self.tmp_path, _VALID_RUN_ID) / "00_orchestrator_events.jsonl"
        events = _read_events(events_file)
        event_types = [e.get("event") for e in events]
        self.assertIn(
            "session_start", event_types,
            "session_start event not found in the run's orchestrator events file"
        )

    def test_session_start_event_carries_correct_run_id(self):
        """The session_start event record carries the run_id from MOSAIC_RUN_ID."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "env-bind-004",
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id=_VALID_RUN_ID)

        events_file = _run_folder(self.tmp_path, _VALID_RUN_ID) / "00_orchestrator_events.jsonl"
        events = _read_events(events_file)
        session_start_events = [e for e in events if e.get("event") == "session_start"]
        self.assertTrue(session_start_events, "No session_start event found")
        for ev in session_start_events:
            self.assertEqual(
                _VALID_RUN_ID, ev.get("run_id"),
                "session_start event does not carry the run_id from MOSAIC_RUN_ID"
            )

    def test_subsequent_non_dispatch_event_also_attributed_to_run_folder(self):
        """After SessionStart binds the run id via MOSAIC_RUN_ID, a subsequent
        Notification (which has no prompt-bearing field) must also route to
        the run's folder, resolving via the session timeline."""
        session_id = "env-bind-005"
        for payload, run_id in [
            ({"hook_event_name": "SessionStart", "session_id": session_id}, _VALID_RUN_ID),
            ({"hook_event_name": "Notification", "session_id": session_id,
              "notification_type": "info", "message": "plain message"}, _VALID_RUN_ID),
        ]:
            _dispatch(payload, self.tmp_path, mosaic_run_id=run_id)

        events_file = _run_folder(self.tmp_path, _VALID_RUN_ID) / "00_orchestrator_events.jsonl"
        events = _read_events(events_file)
        event_types = [e.get("event") for e in events]
        self.assertIn(
            "notification", event_types,
            "Notification following a MOSAIC_RUN_ID-bound SessionStart did not "
            "route to the run's own folder — timeline binding did not persist"
        )

    def test_env_var_binding_writes_session_timeline_entry(self):
        """When MOSAIC_RUN_ID binds the run id, the session timeline is written
        so that later firings resolve via timeline lookup rather than re-reading
        the env var."""
        session_id = "env-bind-006"
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": session_id,
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id=_VALID_RUN_ID)

        paths = core.build_paths(self.tmp_path)
        timeline = runstate.read_session_run_timeline(paths, session_id)
        self.assertTrue(
            len(timeline) >= 1,
            "No session timeline entry written after env var binding — "
            "subsequent firings cannot resolve the run id via timeline"
        )
        run_ids_in_timeline = [entry["run_id"] for entry in timeline]
        self.assertIn(
            _VALID_RUN_ID, run_ids_in_timeline,
            f"Expected run_id {_VALID_RUN_ID!r} in session timeline but found: "
            f"{run_ids_in_timeline!r}"
        )

    def test_env_var_does_not_override_run_id_already_bound_from_prompt(self):
        """MOSAIC_RUN_ID is a fallback — it must not override a run id that was
        already bound from prompt extraction on an earlier firing of the same session.

        This verifies that the resolution order respects: prompt extraction >
        session timeline > env var fallback.
        """
        session_id = "env-bind-007"

        # First firing: SubagentStart carries a different run_id in its prompt,
        # which binds via prompt extraction.
        prompt_with_run_id = json.dumps({
            "agent_instance_id": "Planner#1",
            "run_id": _VALID_RUN_ID_ALT,
            "task_description": "first dispatch",
        })
        start_payload = {
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": "agt-007",
            "agent_type": "Planner",
            "agent_prompt": prompt_with_run_id,
        }
        # Dispatch with MOSAIC_RUN_ID set to a DIFFERENT run id
        _dispatch(start_payload, self.tmp_path, mosaic_run_id=_VALID_RUN_ID)

        # The invocation folder must be under the prompt-extracted run id, not the env var's
        invocation_folder = (
            _run_folder(self.tmp_path, _VALID_RUN_ID_ALT) / "Planner#1"
        )
        self.assertTrue(
            invocation_folder.exists(),
            f"Invocation folder should be under prompt-extracted run id "
            f"{_VALID_RUN_ID_ALT!r} but was not found — env var may have overridden "
            "the prompt-extracted run id"
        )

    def test_subagent_start_with_env_var_creates_invocation_under_run_folder(self):
        """SubagentStart with MOSAIC_RUN_ID creates the invocation folder under
        the run's own folder (not under unknown-run/)."""
        session_id = "env-bind-008"
        agent_instance_id = "TestWriter#1"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": "agt-008",
            "agent_type": "TestWriter",
            # No run_id in the prompt — relies on MOSAIC_RUN_ID
            "agent_prompt": json.dumps({
                "agent_instance_id": agent_instance_id,
                "task_description": "no run_id in prompt",
            }),
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id=_VALID_RUN_ID)

        invocation_folder = _run_folder(self.tmp_path, _VALID_RUN_ID) / agent_instance_id
        self.assertTrue(
            invocation_folder.exists(),
            f"Invocation folder not found under run's own folder — "
            "SubagentStart without run_id in prompt but with MOSAIC_RUN_ID set "
            "did not bind to the run's bucket"
        )

    def test_invalid_env_var_value_is_ignored(self):
        """When MOSAIC_RUN_ID carries a value that fails is_valid_run_id, it
        must be ignored — the bundle falls through to unknown-run as normal."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "env-bind-invalid-001",
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id="not-a-valid-run-id")

        # The unknown-run bucket receives the event (prompt extraction and timeline
        # both fail; the invalid env var value is ignored)
        unknown_run = _unknown_run_folder(self.tmp_path)
        self.assertTrue(
            unknown_run.exists(),
            "Invalid MOSAIC_RUN_ID value should have been ignored and the event "
            "should have fallen through to unknown-run/"
        )
        # The invalid value must not have created a folder named after it
        invalid_folder = _logs_root(self.tmp_path) / "not-a-valid-run-id"
        self.assertFalse(
            invalid_folder.exists(),
            "Invalid MOSAIC_RUN_ID value was used as a folder name — "
            "is_valid_run_id gating not implemented"
        )

    def test_env_var_binding_never_raises(self):
        """The env var binding path must never raise regardless of env var content."""
        for value in [_VALID_RUN_ID, "invalid", "", "unknown-run", "None"]:
            with self.subTest(mosaic_run_id=value):
                payload = {
                    "hook_event_name": "SessionStart",
                    "session_id": f"env-bind-noraise-{value[:8] if value else 'empty'}",
                }
                try:
                    _dispatch(payload, self.tmp_path, mosaic_run_id=value)
                except Exception as exc:
                    self.fail(
                        f"dispatch raised with MOSAIC_RUN_ID={value!r}: {exc}"
                    )


# ---------------------------------------------------------------------------
# T7.2 — Accepted limit: pre-dispatch events may remain in the fallback bucket
# ---------------------------------------------------------------------------

class TestPreDispatchFallbackAcceptedLimit(unittest.TestCase):
    """Without MOSAIC_RUN_ID and without a protocol-compliant dispatch, events
    before the first extractable run id remain in unknown-run/.

    This is the accepted limitation from the plan: making every event
    attributable before any subject activity would require binding run identity
    before any subject fires, which is out of scope. The tests here verify that
    the fallback is clean (no errors, no corruption) and that once a run id
    IS extracted, later events switch cleanly to the correct bucket.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_pre_dispatch_session_start_without_run_id_routes_to_unknown_run(self):
        """SessionStart before any run id is known routes to unknown-run/ without error."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "pre-dispatch-001",
        }
        # No MOSAIC_RUN_ID, no run id in payload
        try:
            _dispatch(payload, self.tmp_path, mosaic_run_id=None)
        except Exception as exc:
            self.fail(f"Pre-dispatch SessionStart raised: {exc}")

        unknown_run = _unknown_run_folder(self.tmp_path)
        self.assertTrue(
            unknown_run.exists(),
            "Pre-dispatch SessionStart (no run id) should route to unknown-run/"
        )

    def test_pre_dispatch_fallback_does_not_corrupt_later_run_folder(self):
        """Events that land in unknown-run/ before the first dispatch do not
        corrupt the run folder that later events are attributed to.

        Simulates: SessionStart → unknown-run/, then SubagentStart with
        MOSAIC_RUN_ID → run folder.
        """
        session_id = "pre-dispatch-002"

        # First event: no MOSAIC_RUN_ID — goes to unknown-run/
        _dispatch(
            {"hook_event_name": "SessionStart", "session_id": session_id},
            self.tmp_path,
            mosaic_run_id=None,
        )

        # Second event: MOSAIC_RUN_ID is now set (simulates runner having set it)
        agent_instance_id = "Planner#1"
        _dispatch(
            {
                "hook_event_name": "SubagentStart",
                "session_id": session_id,
                "agent_id": "agt-pre-002",
                "agent_type": "Planner",
                "agent_prompt": json.dumps({
                    "agent_instance_id": agent_instance_id,
                    "task_description": "dispatch with run id",
                }),
            },
            self.tmp_path,
            mosaic_run_id=_VALID_RUN_ID,
        )

        # The run folder must exist and contain the invocation
        run_folder = _run_folder(self.tmp_path, _VALID_RUN_ID)
        self.assertTrue(
            run_folder.exists(),
            f"Run folder not created for SubagentStart with MOSAIC_RUN_ID — "
            "pre-dispatch unknown-run traffic may have corrupted the binding"
        )

        invocation_folder = run_folder / agent_instance_id
        self.assertTrue(
            invocation_folder.exists(),
            "Invocation folder not found under run folder after SubagentStart "
            "with MOSAIC_RUN_ID — pre-dispatch fallback may have interfered"
        )

    def test_pre_dispatch_fallback_raises_no_error_or_exception(self):
        """An entire sequence of pre-dispatch events with no run id must
        complete without raising anything."""
        session_id = "pre-dispatch-003"
        payloads = [
            {"hook_event_name": "SessionStart", "session_id": session_id},
            {"hook_event_name": "Notification", "session_id": session_id,
             "notification_type": "info", "message": "no run id here"},
            {"hook_event_name": "PreToolUse", "session_id": session_id,
             "tool_name": "Bash", "tool_input": {"command": "ls"}},
            {"hook_event_name": "PostToolUse", "session_id": session_id,
             "tool_name": "Bash", "tool_output": "file.txt"},
        ]
        for payload in payloads:
            try:
                _dispatch(payload, self.tmp_path, mosaic_run_id=None)
            except Exception as exc:
                self.fail(
                    f"Pre-dispatch {payload['hook_event_name']} raised: {exc}"
                )

    def test_pre_dispatch_events_in_unknown_run_do_not_have_fabricated_run_id(self):
        """Events routed to unknown-run/ before a run id is known must not carry
        an invented run_id field — only the actual binding source may set run_id.
        """
        session_id = "pre-dispatch-004"
        _dispatch(
            {"hook_event_name": "SessionStart", "session_id": session_id},
            self.tmp_path,
            mosaic_run_id=None,
        )

        events_file = _unknown_run_folder(self.tmp_path) / "00_orchestrator_events.jsonl"
        events = _read_events(events_file)
        for ev in events:
            run_id_in_event = ev.get("run_id")
            if run_id_in_event is not None:
                # The only acceptable run_id in the unknown-run bucket is one that
                # was genuinely extracted — not "unknown-run" as a literal
                self.assertNotEqual(
                    "unknown-run", run_id_in_event,
                    "Event in unknown-run/ carries 'unknown-run' as a run_id value — "
                    "the literal fallback bucket name must not be injected into events"
                )


# ---------------------------------------------------------------------------
# T7.3 — Non-compliant session degrades without raising or inventing identity
# ---------------------------------------------------------------------------

class TestNonCompliantSessionDegradation(unittest.TestCase):
    """A session that never yields an extractable run id (no MOSAIC_RUN_ID,
    no protocol-compliant dispatch, no run id in any prompt-bearing field)
    degrades exactly as documented:

    - All events route to unknown-run/
    - No exception is raised at any point
    - ctx.run_id is never set to an invented value

    These tests ensure the documented degradation contract is preserved after
    Stage 7's changes.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_all_events_route_to_unknown_run_when_no_run_id_extractable(self):
        """Every event type routes to unknown-run/ when no run id is extractable."""
        session_id = "non-compliant-001"
        payloads = [
            {"hook_event_name": "SessionStart", "session_id": session_id},
            {"hook_event_name": "UserPromptSubmit", "session_id": session_id,
             "user_prompt": "Hello, no run_id in this message"},
            {"hook_event_name": "Notification", "session_id": session_id,
             "notification_type": "info", "message": "plain message"},
            {"hook_event_name": "Stop", "session_id": session_id,
             "last_assistant_message": "done"},
            {"hook_event_name": "SessionEnd", "session_id": session_id,
             "reason": "normal"},
        ]
        for payload in payloads:
            _dispatch(payload, self.tmp_path, mosaic_run_id=None)

        unknown_run = _unknown_run_folder(self.tmp_path)
        self.assertTrue(
            unknown_run.exists(),
            "unknown-run/ bucket not created for a session with no extractable run id"
        )
        files = [f for f in unknown_run.rglob("*") if f.is_file()]
        self.assertGreater(
            len(files), 0,
            "No files written under unknown-run/ for a non-compliant session"
        )

    def test_non_compliant_session_never_raises(self):
        """Every hook event for a non-compliant session completes without raising."""
        session_id = "non-compliant-002"
        payloads = [
            {"hook_event_name": "SessionStart", "session_id": session_id},
            {"hook_event_name": "UserPromptSubmit", "session_id": session_id,
             "user_prompt": "no run id here"},
            {"hook_event_name": "SubagentStart", "session_id": session_id,
             "agent_id": "agt-nc-002",
             "agent_prompt": "prose dispatch without structured fields"},
            {"hook_event_name": "PreToolUse", "session_id": session_id,
             "tool_name": "Bash", "tool_input": {}},
            {"hook_event_name": "PostToolUse", "session_id": session_id,
             "tool_name": "Bash", "tool_output": "output"},
            {"hook_event_name": "Stop", "session_id": session_id,
             "last_assistant_message": "done"},
            {"hook_event_name": "SessionEnd", "session_id": session_id,
             "reason": "normal"},
        ]
        for payload in payloads:
            try:
                _dispatch(payload, self.tmp_path, mosaic_run_id=None)
            except Exception as exc:
                self.fail(
                    f"Non-compliant session raised on {payload['hook_event_name']}: {exc}"
                )

    def test_no_invented_run_id_in_events_for_non_compliant_session(self):
        """Events written by a non-compliant session must not carry a fabricated
        run_id value — only the 'unknown-run' routing label may appear, and
        only in the path, never injected into the event record as a run_id field.
        """
        session_id = "non-compliant-003"
        _dispatch(
            {"hook_event_name": "SessionStart", "session_id": session_id},
            self.tmp_path,
            mosaic_run_id=None,
        )
        _dispatch(
            {"hook_event_name": "Notification", "session_id": session_id,
             "notification_type": "info", "message": "no run id"},
            self.tmp_path,
            mosaic_run_id=None,
        )

        unknown_run = _unknown_run_folder(self.tmp_path)
        events_file = unknown_run / "00_orchestrator_events.jsonl"
        events = _read_events(events_file)

        self.assertGreater(len(events), 0, "No events written at all")
        for ev in events:
            self.assertNotIn(
                "run_id", ev,
                f"Event in unknown-run/ carries an invented run_id field: {ev!r}"
            )

    def test_non_compliant_session_does_not_create_run_id_shaped_folder(self):
        """A non-compliant session must not create a folder whose name matches the
        run id format — no invented run id must become a path component."""
        import re
        run_id_pattern = re.compile(r"^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")

        session_id = "non-compliant-004"
        _dispatch(
            {"hook_event_name": "SessionStart", "session_id": session_id},
            self.tmp_path,
            mosaic_run_id=None,
        )
        _dispatch(
            {"hook_event_name": "SubagentStart", "session_id": session_id,
             "agent_id": "agt-nc-004",
             "agent_prompt": "prose with no run id"},
            self.tmp_path,
            mosaic_run_id=None,
        )

        logs_root = _logs_root(self.tmp_path)
        if not logs_root.exists():
            return  # Nothing written, nothing to check

        for item in logs_root.iterdir():
            if item.is_dir():
                self.assertFalse(
                    run_id_pattern.match(item.name),
                    f"A folder with a run-id-shaped name was created for a "
                    f"non-compliant session: {item.name!r} — run identity was invented"
                )

    def test_prose_dispatch_degrades_gracefully(self):
        """A SubagentStart whose agent_prompt is free-form prose (no structured
        fields) degrades to unknown-run/unknown-agent/ without raising.

        This is the 'weaker subject model' case documented in the plan.
        """
        session_id = "non-compliant-005"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": "agt-nc-005",
            "agent_prompt": (
                "Please do some work. I am the orchestrator asking you to help. "
                "There is no structured JSON here and no run id token."
            ),
        }
        try:
            _dispatch(payload, self.tmp_path, mosaic_run_id=None)
        except Exception as exc:
            self.fail(f"Prose-dispatch SubagentStart raised: {exc}")

        unknown_run = _unknown_run_folder(self.tmp_path)
        self.assertTrue(
            unknown_run.exists(),
            "unknown-run/ not created for prose-dispatch SubagentStart — "
            "event may have been silently dropped"
        )

    def test_absent_mosaic_run_id_var_means_no_env_binding(self):
        """When MOSAIC_RUN_ID is absent (not set) the env-var binding path
        is not taken — behaviour is identical to the pre-Stage-7 fallback."""
        session_id = "non-compliant-006"
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": session_id,
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id=None)

        # No run-id-shaped folder should be created
        import re
        run_id_pattern = re.compile(r"^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")
        logs_root = _logs_root(self.tmp_path)
        if logs_root.exists():
            for item in logs_root.iterdir():
                if item.is_dir():
                    self.assertFalse(
                        run_id_pattern.match(item.name),
                        f"Run-id-shaped folder {item.name!r} created for session "
                        "with no MOSAIC_RUN_ID and no prompt-based extraction"
                    )


# ---------------------------------------------------------------------------
# T7.4 — Existing bundle tests still cover their stated contract
#
# This section adds explicit regression anchors for the three behaviours that
# Stage 7's changes must not disturb, pointing back to the coverage in the
# existing test suites. These tests verify the same properties from an
# orthogonal angle — they are not duplicates of the existing suite, but
# rather end-to-end sanity checks that the existing suites' contracts hold
# even with the new env-var binding path in place.
# ---------------------------------------------------------------------------

class TestExistingContractsSurviveEnvVarPath(unittest.TestCase):
    """Regression anchors: the env-var binding path must not break existing
    session-binding, unknown-run-routing, or handler behaviour.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_prompt_extraction_takes_precedence_over_env_var(self):
        """When the prompt carries a valid run id AND MOSAIC_RUN_ID is set to a
        different run id, the prompt-extracted run id wins.

        This preserves the existing 'text extraction' path as the highest-priority
        resolution step, consistent with the resolution order documented in
        resolve_run_identity.
        """
        session_id = "regression-001"
        prompt_run_id = _VALID_RUN_ID
        env_run_id = _VALID_RUN_ID_ALT  # different from what the prompt carries

        agent_instance_id = "Planner#10"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": "agt-reg-001",
            "agent_type": "Planner",
            "agent_prompt": json.dumps({
                "agent_instance_id": agent_instance_id,
                "run_id": prompt_run_id,
                "task_description": "structured dispatch",
            }),
        }
        _dispatch(payload, self.tmp_path, mosaic_run_id=env_run_id)

        # The invocation folder must be under the prompt-extracted run id
        invocation_under_prompt_run = (
            _run_folder(self.tmp_path, prompt_run_id) / agent_instance_id
        )
        self.assertTrue(
            invocation_under_prompt_run.exists(),
            f"Invocation folder not found under prompt-extracted run id "
            f"{prompt_run_id!r} — env var may have overridden prompt extraction"
        )

    def test_session_timeline_binding_takes_precedence_over_env_var(self):
        """When the session timeline already has a binding, that binding takes
        precedence over MOSAIC_RUN_ID for subsequent events.

        Resolution order: prompt extraction > session timeline > env var.
        """
        session_id = "regression-002"
        timeline_run_id = _VALID_RUN_ID
        env_run_id = _VALID_RUN_ID_ALT

        # First: bind via prompt (writes timeline for timeline_run_id)
        _dispatch(
            {
                "hook_event_name": "SubagentStart",
                "session_id": session_id,
                "agent_id": "agt-reg-002a",
                "agent_type": "Planner",
                "agent_prompt": json.dumps({
                    "agent_instance_id": "Planner#20",
                    "run_id": timeline_run_id,
                    "task_description": "bind via prompt",
                }),
            },
            self.tmp_path,
            mosaic_run_id=env_run_id,
        )

        # Second: Notification (no prompt-bearing field) — must resolve via timeline,
        # NOT via env var, because timeline_run_id was already written
        _dispatch(
            {
                "hook_event_name": "Notification",
                "session_id": session_id,
                "notification_type": "info",
                "message": "no run id in message",
            },
            self.tmp_path,
            mosaic_run_id=env_run_id,
        )

        # The notification must appear in the timeline_run_id folder, not in env_run_id
        events_file = (
            _run_folder(self.tmp_path, timeline_run_id) / "00_orchestrator_events.jsonl"
        )
        events = _read_events(events_file)
        event_types = [e.get("event") for e in events]
        self.assertIn(
            "notification", event_types,
            "Notification not found in the timeline-bound run folder — "
            "env var may have shadowed an existing timeline binding"
        )

    def test_env_var_not_set_in_real_workspace_leaves_existing_behaviour_unchanged(self):
        """When MOSAIC_RUN_ID is absent (real MOSAIC workspace), a SubagentStart
        with a valid run id in its prompt still binds correctly — the env-var
        path is not taken and existing resolution is undisturbed.
        """
        session_id = "regression-003"
        agent_instance_id = "Researcher#5"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": "agt-reg-003",
            "agent_type": "Researcher",
            "agent_prompt": json.dumps({
                "agent_instance_id": agent_instance_id,
                "run_id": _VALID_RUN_ID,
                "task_description": "normal dispatch without env var",
            }),
        }
        # MOSAIC_RUN_ID is NOT set (simulates real MOSAIC workspace)
        _dispatch(payload, self.tmp_path, mosaic_run_id=None)

        invocation_folder = _run_folder(self.tmp_path, _VALID_RUN_ID) / agent_instance_id
        self.assertTrue(
            invocation_folder.exists(),
            "Invocation folder not found after a prompt-based dispatch without "
            "MOSAIC_RUN_ID — existing prompt extraction was broken by Stage 7 changes"
        )


if __name__ == "__main__":
    unittest.main()
