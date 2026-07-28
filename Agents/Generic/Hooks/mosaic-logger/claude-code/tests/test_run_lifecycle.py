"""Tests for run lifecycle synthesis in the orchestrator dispatcher.

Covers T3.2: run_start emitted when a run_id is minted (before the firing's
handler events), run_end emitted when the run marker is closed (after the same
firing's session_end), the resume-into-a-new-run case, run_end outcome derivation
from SessionEnd reason, and ordering invariants.

Tests drive through mosaic_logger.dispatch() so the prelude/postlude split is
exercised end-to-end.
"""

import datetime
import json
import os
import pathlib
import sys
import tempfile
import unittest
import unittest.mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

BASE_TIME = datetime.datetime(2026, 1, 1, 17, 0, 0, tzinfo=datetime.timezone.utc)


def _dispatch_with_env(payload: dict, tmp_path: pathlib.Path) -> None:
    """Dispatch a hook payload with CLAUDE_PROJECT_DIR pointing at tmp_path."""
    orig = os.environ.get("CLAUDE_PROJECT_DIR")
    os.environ["CLAUDE_PROJECT_DIR"] = str(tmp_path)
    try:
        mosaic_logger.dispatch(json.dumps(payload))
    finally:
        if orig is None:
            os.environ.pop("CLAUDE_PROJECT_DIR", None)
        else:
            os.environ["CLAUDE_PROJECT_DIR"] = orig


def _read_orchestrator_events(tmp_path: pathlib.Path, run_id: str) -> list:
    """Return all parsed JSONL lines from the orchestrator events file."""
    events_path = (
        tmp_path / "OrchestrationLogs" / run_id / "00_orchestrator_events.jsonl"
    )
    if not events_path.exists():
        return []
    lines = events_path.read_text(encoding="utf-8").splitlines()
    return [json.loads(line) for line in lines if line.strip()]


def _find_run_id(tmp_path: pathlib.Path, session_id: str) -> "str | None":
    """Read the run_id from the run-marker file for session_id, or None if absent."""
    paths = core.build_paths(tmp_path)
    marker_path = paths.marker(session_id)
    if not marker_path.exists():
        return None
    try:
        data = json.loads(marker_path.read_text(encoding="utf-8"))
        return data.get("run_id")
    except Exception:
        return None


def _dispatch_session_start(tmp_path: pathlib.Path,
                             session_id: str,
                             source: str = "startup",
                             model: str | None = None) -> "str | None":
    """Dispatch a SessionStart and return the minted run_id."""
    payload: dict = {
        "hook_event_name": "SessionStart",
        "session_id": session_id,
        "source": source,
        "cwd": str(tmp_path),
    }
    if model:
        payload["model"] = model
    _dispatch_with_env(payload, tmp_path)
    return _find_run_id(tmp_path, session_id)


def _dispatch_session_end(tmp_path: pathlib.Path,
                           session_id: str,
                           reason: "str | None" = "clear") -> None:
    """Dispatch a SessionEnd."""
    payload: dict = {
        "hook_event_name": "SessionEnd",
        "session_id": session_id,
        "cwd": str(tmp_path),
    }
    if reason is not None:
        payload["reason"] = reason
    _dispatch_with_env(payload, tmp_path)


# ---------------------------------------------------------------------------
# T3.2 – run_start emitted on mint
# ---------------------------------------------------------------------------

class TestRunStart(unittest.TestCase):
    """run_start is emitted exactly when a new run_id is minted."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.session_id = "run-lifecycle-session-001"

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_start_causes_run_start_in_event_stream(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        self.assertIsNotNone(run_id, "No run_id minted after SessionStart")
        events = _read_orchestrator_events(self.tmp_path, run_id)
        event_types = [e["event"] for e in events]
        self.assertIn("run_start", event_types)

    def test_run_start_precedes_session_start_in_stream(self):
        """run_start is emitted in the prelude, before handle_session_start runs."""
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        self.assertIsNotNone(run_id)
        events = _read_orchestrator_events(self.tmp_path, run_id)
        types = [e["event"] for e in events]
        self.assertIn("run_start", types)
        self.assertIn("session_start", types)
        self.assertLess(types.index("run_start"), types.index("session_start"))

    def test_run_start_carries_run_id_field(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_start = next(e for e in events if e["event"] == "run_start")
        self.assertIn("run_id", run_start)
        self.assertEqual(run_id, run_start["run_id"])

    def test_run_start_carries_cwd_when_present(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_start = next(e for e in events if e["event"] == "run_start")
        self.assertIn("cwd", run_start)

    def test_run_start_carries_model_when_present(self):
        run_id = _dispatch_session_start(
            self.tmp_path, self.session_id, model="claude-opus-4-5"
        )
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_start = next(e for e in events if e["event"] == "run_start")
        self.assertIn("model", run_start)
        self.assertEqual("claude-opus-4-5", run_start["model"])

    def test_run_start_model_absent_when_not_in_session_start_payload(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_start = next(e for e in events if e["event"] == "run_start")
        self.assertNotIn("model", run_start)

    def test_run_start_not_emitted_on_second_event_for_same_session(self):
        """REUSED outcome: no new run_start on subsequent events within same run."""
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        self.assertIsNotNone(run_id)
        # Dispatch a non-minting event (notification) — must not mint again
        payload = {
            "hook_event_name": "Notification",
            "session_id": self.session_id,
            "notification_type": "info",
            "message": "Second event",
        }
        _dispatch_with_env(payload, self.tmp_path)
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_start_count = sum(1 for e in events if e["event"] == "run_start")
        self.assertEqual(1, run_start_count)


# ---------------------------------------------------------------------------
# T3.2 – run_end emitted on close, after session_end
# ---------------------------------------------------------------------------

class TestRunEnd(unittest.TestCase):
    """run_end is emitted exactly when the run marker is closed, after session_end."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.session_id = "run-lifecycle-session-002"

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_end_causes_run_end_in_event_stream(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="clear")
        events = _read_orchestrator_events(self.tmp_path, run_id)
        event_types = [e["event"] for e in events]
        self.assertIn("run_end", event_types)

    def test_run_end_follows_session_end_in_stream(self):
        """run_end is the outer scope; it must be the last event in the stream."""
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="clear")
        events = _read_orchestrator_events(self.tmp_path, run_id)
        types = [e["event"] for e in events]
        self.assertIn("session_end", types)
        self.assertIn("run_end", types)
        self.assertLess(types.index("session_end"), types.index("run_end"))

    def test_run_end_carries_run_id_field(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="clear")
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_end = next(e for e in events if e["event"] == "run_end")
        self.assertIn("run_id", run_end)
        self.assertEqual(run_id, run_end["run_id"])

    def test_run_end_outcome_derived_from_session_end_reason(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="clear")
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_end = next(e for e in events if e["event"] == "run_end")
        self.assertIn("outcome", run_end)
        self.assertEqual("clear", run_end["outcome"])

    def test_run_end_outcome_from_resume_reason(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="resume")
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_end = next(e for e in events if e["event"] == "run_end")
        self.assertEqual("resume", run_end["outcome"])

    def test_run_end_outcome_absent_when_session_end_has_no_reason(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason=None)
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_end = next(e for e in events if e["event"] == "run_end")
        self.assertNotIn("outcome", run_end)

    def test_run_end_not_emitted_before_session_end_fires(self):
        """run_end only fires on close; intermediate events must not trigger it."""
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        # An intermediate event (user prompt) should not cause run_end
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": self.session_id,
            "user_prompt": "Hello!",
        }
        _dispatch_with_env(payload, self.tmp_path)
        events = _read_orchestrator_events(self.tmp_path, run_id)
        event_types = [e["event"] for e in events]
        self.assertNotIn("run_end", event_types)


# ---------------------------------------------------------------------------
# T3.2 – Run start ordering: run_start before session_start in same firing
# ---------------------------------------------------------------------------

class TestRunStartSessionStartOrdering(unittest.TestCase):
    """The prelude/postlude split guarantees run_start is first, run_end is last."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.session_id = "ordering-session-003"

    def tearDown(self):
        self.tmp.cleanup()

    def test_full_lifecycle_ordering_is_run_start_session_start_session_end_run_end(self):
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="clear")
        events = _read_orchestrator_events(self.tmp_path, run_id)
        types = [e["event"] for e in events]
        # All four must be present
        for required in ("run_start", "session_start", "session_end", "run_end"):
            self.assertIn(required, types)
        # Ordering constraints
        self.assertLess(types.index("run_start"), types.index("session_start"))
        self.assertLess(types.index("session_end"), types.index("run_end"))


# ---------------------------------------------------------------------------
# T3.2 – Resume-into-a-new-run case
# ---------------------------------------------------------------------------

class TestResumeIntoNewRun(unittest.TestCase):
    """After a run is closed, a new SessionStart mints a distinct new run_id."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.session_id = "resume-session-004"

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_end_then_session_start_mints_new_run_id(self):
        run_id_1 = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="resume")
        run_id_2 = _dispatch_session_start(
            self.tmp_path, self.session_id, source="resume"
        )
        self.assertIsNotNone(run_id_1)
        self.assertIsNotNone(run_id_2)
        self.assertNotEqual(run_id_1, run_id_2)

    def test_new_run_has_its_own_event_stream(self):
        run_id_1 = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="resume")
        run_id_2 = _dispatch_session_start(
            self.tmp_path, self.session_id, source="resume"
        )
        events_1 = _read_orchestrator_events(self.tmp_path, run_id_1)
        events_2 = _read_orchestrator_events(self.tmp_path, run_id_2)
        # Each run should have its own events directory
        self.assertGreater(len(events_1), 0)
        self.assertGreater(len(events_2), 0)

    def test_run_end_in_first_run_only(self):
        run_id_1 = _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="resume")
        # First run should have run_end
        events_1 = _read_orchestrator_events(self.tmp_path, run_id_1)
        types_1 = [e["event"] for e in events_1]
        self.assertIn("run_end", types_1)

    def test_second_run_has_run_start_but_not_run_end_yet(self):
        _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="resume")
        run_id_2 = _dispatch_session_start(
            self.tmp_path, self.session_id, source="resume"
        )
        events_2 = _read_orchestrator_events(self.tmp_path, run_id_2)
        types_2 = [e["event"] for e in events_2]
        self.assertIn("run_start", types_2)
        self.assertNotIn("run_end", types_2)

    def test_resumed_true_on_session_start_after_close(self):
        _dispatch_session_start(self.tmp_path, self.session_id)
        _dispatch_session_end(self.tmp_path, self.session_id, reason="resume")
        run_id_2 = _dispatch_session_start(
            self.tmp_path, self.session_id, source="resume"
        )
        events_2 = _read_orchestrator_events(self.tmp_path, run_id_2)
        session_start_2 = next(
            (e for e in events_2 if e["event"] == "session_start"), None
        )
        self.assertIsNotNone(session_start_2)
        self.assertIs(True, session_start_2.get("resumed"))


# ---------------------------------------------------------------------------
# T3.2 – run_start adapter_version field
# ---------------------------------------------------------------------------

class TestRunStartAdapterVersion(unittest.TestCase):
    """run_start carries adapter_version when hook.yaml is present and readable."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.session_id = "adapter-version-session-005"

    def tearDown(self):
        self.tmp.cleanup()

    def test_run_start_adapter_version_is_string_or_absent(self):
        """adapter_version must be a string when present; never null or 0."""
        run_id = _dispatch_session_start(self.tmp_path, self.session_id)
        events = _read_orchestrator_events(self.tmp_path, run_id)
        run_start = next(e for e in events if e["event"] == "run_start")
        if "adapter_version" in run_start:
            self.assertIsInstance(run_start["adapter_version"], str)
            self.assertGreater(len(run_start["adapter_version"]), 0)
        # Absence is also valid (hook.yaml may not be deployed in test env)


if __name__ == "__main__":
    unittest.main()
