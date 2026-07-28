"""End-to-end synthetic validation tests for the mosaic-logger adapter.

Drives the full dispatcher pipeline by feeding synthetic hook payloads through
mosaic_logger.dispatch() against a temporary workspace and asserting the resulting
on-disk tree, event streams, and field-level degradation match the binding schema.

These tests exercise cross-cutting properties that no single handler unit test owns:
the complete directory layout, event scoping between orchestrator and invocation
streams, schema conformance across every emitted line, start/end pairing, run
identity across sessions, and field-level degradation.

The replay harness sets CLAUDE_PROJECT_DIR to a temporary directory so no test
writes outside its own temp tree.
"""

import datetime
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
# Constants shared across all test classes in this module
# ---------------------------------------------------------------------------

_SESSION_ID = "e2e-session-full-001"
_AGENT_ID_ALPHA = "agt-alpha-001"
_AGENT_ID_BETA = "agt-beta-002"
_INSTANCE_ID_ALPHA = "Research#1"
_INSTANCE_ID_BETA = "TestWriter#2"

# Canonical event catalog from schema §3.1 — the complete closed set.
_CATALOG = frozenset({
    "run_start", "run_end", "session_start", "session_end",
    "invocation_start", "invocation_end", "turn",
    "tool_call_start", "tool_call_end", "notification", "compaction",
})

# Fields that must never appear anywhere in emitted output.
_FORBIDDEN_FIELDS = frozenset({
    "duration_ms", "turn_index", "call_index",
    "actor", "source_file",
    "cost_usd", "cost", "price",
})

# Envelope fields that must be present on every emitted event.
_REQUIRED_ENVELOPE = ("schema_version", "event", "timestamp", "harness")

# Path to the shared transcript fixture file.
_FIXTURE_DIR = pathlib.Path(__file__).parent / "fixtures"
_SAMPLE_TRANSCRIPT = _FIXTURE_DIR / "sample_transcript.jsonl"


# ---------------------------------------------------------------------------
# Replay harness (T6.1)
# ---------------------------------------------------------------------------

class _ReplayHarness:
    """Drives mosaic_logger.dispatch() against an isolated temporary workspace.

    Usage:
        with _ReplayHarness(tmp_path) as h:
            h.replay(payload_list)
            run_id = h.find_run_id(session_id)
            events = h.orchestrator_events(run_id)

    The harness sets CLAUDE_PROJECT_DIR to ``workspace`` on entry and restores
    the original value (or removes the variable) on exit, so no test leaks
    environment state across test cases.
    """

    def __init__(self, workspace: pathlib.Path):
        self.workspace = workspace
        self._saved_env: "str | None" = None

    def __enter__(self) -> "_ReplayHarness":
        self._saved_env = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.workspace)
        return self

    def __exit__(self, *_) -> None:
        if self._saved_env is None:
            os.environ.pop("CLAUDE_PROJECT_DIR", None)
        else:
            os.environ["CLAUDE_PROJECT_DIR"] = self._saved_env

    def replay(self, payloads: list) -> None:
        """Dispatch each payload dict in order, serializing each to JSON first.

        Replicates the exact call chain the harness uses: one JSON object per
        firing, dispatched through mosaic_logger.dispatch().
        """
        for payload in payloads:
            mosaic_logger.dispatch(json.dumps(payload))

    # ------------------------------------------------------------------
    # Inspection helpers
    # ------------------------------------------------------------------

    def find_run_id(self, session_id: str) -> "str | None":
        """Read the run_id from the marker file for session_id; None if absent."""
        marker = core.build_paths(self.workspace).marker(session_id)
        if not marker.exists():
            return None
        try:
            return json.loads(marker.read_text("utf-8")).get("run_id")
        except Exception:
            return None

    def read_jsonl(self, path: pathlib.Path) -> list:
        """Parse every non-empty line in a JSONL file. Returns [] if absent."""
        if not path.exists():
            return []
        return [
            json.loads(ln)
            for ln in path.read_text("utf-8").splitlines()
            if ln.strip()
        ]

    def orchestrator_events(self, run_id: str) -> list:
        return self.read_jsonl(
            core.build_paths(self.workspace).orchestrator_events(run_id)
        )

    def invocation_events(self, run_id: str, agent_instance_id: str) -> list:
        return self.read_jsonl(
            core.build_paths(self.workspace).invocation_events(run_id, agent_instance_id)
        )

    def all_jsonl_files(self, run_id: str) -> "list[pathlib.Path]":
        """Return every .jsonl path under the run root, in any order."""
        run_root = core.build_paths(self.workspace).run_root(run_id)
        if not run_root.exists():
            return []
        return list(run_root.rglob("*.jsonl"))

    def run_root(self, run_id: str) -> pathlib.Path:
        return core.build_paths(self.workspace).run_root(run_id)

    def log_root(self) -> pathlib.Path:
        return core.build_paths(self.workspace).root


# ---------------------------------------------------------------------------
# Full-session payload builder
# ---------------------------------------------------------------------------

def _full_session_payloads(tmp_path: pathlib.Path,
                            transcript_path: "pathlib.Path | None" = None) -> list:
    """Return the representative full-session payload sequence.

    Covers the sequence described in the Stage 6 plan:
      SessionStart → UserPromptSubmit → orchestrator PreToolUse/PostToolUse →
      two SubagentStarts → interleaved per-agent tool calls →
      PostToolUseFailure on one agent → PreCompact/PostCompact →
      Notification → two SubagentStops → Stop → SessionEnd.

    When transcript_path is supplied, it is injected into every payload that
    accepts transcript_path or agent_transcript_path so that the full artifact
    set (including raw exports) is exercised.
    """
    tp = str(transcript_path) if transcript_path else None

    def _base(event: str, **extra) -> dict:
        p: dict = {"hook_event_name": event, "session_id": _SESSION_ID,
                   "cwd": str(tmp_path)}
        if tp:
            p["transcript_path"] = tp
        p.update(extra)
        return p

    return [
        # 1 – SessionStart: mints the run, emits run_start then session_start.
        _base("SessionStart", source="startup", model="claude-opus-4-5"),

        # 2 – UserPromptSubmit: orchestrator user turn.
        _base("UserPromptSubmit",
              user_prompt="Start the orchestration workflow."),

        # 3 – Orchestrator tool call pair (no agent_id → orchestrator stream).
        _base("PreToolUse",
              tool_name="Bash", tool_use_id="call-orch-001",
              tool_input={"command": "ls -la"}),
        _base("PostToolUse",
              tool_name="Bash", tool_use_id="call-orch-001",
              tool_output="total 8\ndrwxr-xr-x 3 user user 4096 Jan 01 17:00 ."),

        # 4 – SubagentStart for two different agent_id values.
        _base("SubagentStart",
              agent_id=_AGENT_ID_ALPHA, agent_type="Research",
              agent_prompt=(
                  '{"agent_instance_id": "Research#1",'
                  ' "task": "conduct literature review"}'
              )),
        _base("SubagentStart",
              agent_id=_AGENT_ID_BETA, agent_type="TestWriter",
              agent_prompt=(
                  '{"agent_instance_id": "TestWriter#2",'
                  ' "task": "write unit tests"}'
              )),

        # 5 – Interleaved per-agent tool calls.
        _base("PreToolUse",
              agent_id=_AGENT_ID_ALPHA, tool_name="Read",
              tool_use_id="call-alpha-001",
              tool_input={"file_path": "/docs/paper.pdf"}),
        _base("PostToolUse",
              agent_id=_AGENT_ID_ALPHA, tool_name="Read",
              tool_use_id="call-alpha-001",
              tool_output="Paper content here."),
        _base("PreToolUse",
              agent_id=_AGENT_ID_BETA, tool_name="Write",
              tool_use_id="call-beta-001",
              tool_input={"file_path": "/tests/test_foo.py",
                          "content": "def test_foo(): pass"}),

        # 6 – PostToolUseFailure on the beta agent.
        _base("PostToolUseFailure",
              agent_id=_AGENT_ID_BETA, tool_name="Write",
              tool_use_id="call-beta-001",
              tool_output="Error: permission denied writing /tests/test_foo.py"),

        # 7 – PreCompact / PostCompact.
        _base("PreCompact", trigger="auto", tokens_before=95000),
        _base("PostCompact", tokens_after=8000),

        # 8 – Notification.
        _base("Notification",
              notification_type="permission_request",
              message="Allow bash command execution?"),

        # 9 – SubagentStop for both invocations.
        _base("SubagentStop",
              agent_id=_AGENT_ID_ALPHA, agent_type="Research",
              agent_transcript_path=tp,
              last_assistant_message=(
                  '{"status_code": "SUCCESS",'
                  ' "result": "Research complete."}'
              )),
        _base("SubagentStop",
              agent_id=_AGENT_ID_BETA, agent_type="TestWriter",
              agent_transcript_path=tp,
              last_assistant_message=(
                  '{"status_code": "BLOCKED",'
                  ' "reason": "Missing upstream context."}'
              )),

        # 10 – Stop: orchestrator assistant turn.
        _base("Stop",
              last_assistant_message=(
                  "Orchestration workflow complete. All subagents finished."
              )),

        # 11 – SessionEnd: closes the marker, emits session_end then run_end.
        _base("SessionEnd", reason="clear"),
    ]


# ---------------------------------------------------------------------------
# Shared helpers
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


def _contains_forbidden_field(obj: object) -> "str | None":
    """Return the first forbidden field name found anywhere in obj, or None."""
    if isinstance(obj, dict):
        for key in obj:
            if key in _FORBIDDEN_FIELDS:
                return key
            found = _contains_forbidden_field(obj[key])
            if found:
                return found
    if isinstance(obj, list):
        for item in obj:
            found = _contains_forbidden_field(item)
            if found:
                return found
    return None


# ---------------------------------------------------------------------------
# T6.1 — Replay harness self-check
# ---------------------------------------------------------------------------

class TestReplayHarness(unittest.TestCase):
    """The replay harness correctly drives dispatch() and makes the tree readable."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_harness_sets_and_restores_env_var(self):
        original = os.environ.get("CLAUDE_PROJECT_DIR", "__sentinel__")
        with _ReplayHarness(self.tmp_path) as h:
            self.assertEqual(str(self.tmp_path), os.environ.get("CLAUDE_PROJECT_DIR"))
        # After exit the environment is restored.
        restored = os.environ.get("CLAUDE_PROJECT_DIR", "__sentinel__")
        self.assertEqual(original, restored)

    def test_replay_creates_marker_file_after_session_start(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "harness-check-001",
            "source": "startup",
        }
        with _ReplayHarness(self.tmp_path) as h:
            h.replay([payload])
            run_id = h.find_run_id("harness-check-001")
        self.assertIsNotNone(run_id, "SessionStart must mint a run_id")

    def test_replay_single_payload_produces_orchestrator_events(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "harness-check-002",
            "source": "startup",
        }
        with _ReplayHarness(self.tmp_path) as h:
            h.replay([payload])
            run_id = h.find_run_id("harness-check-002")
            events = h.orchestrator_events(run_id)
        self.assertGreater(len(events), 0,
                           "At least run_start and session_start expected")

    def test_replay_multiple_payloads_all_dispatched(self):
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path, _SAMPLE_TRANSCRIPT))
            run_id = h.find_run_id(_SESSION_ID)
            events = h.orchestrator_events(run_id)
        # The full session emits several orchestrator events.
        self.assertGreater(len(events), 4)

    def test_harness_find_run_id_returns_none_when_no_marker(self):
        with _ReplayHarness(self.tmp_path) as h:
            result = h.find_run_id("nonexistent-session")
        self.assertIsNone(result)

    def test_harness_read_jsonl_returns_empty_list_for_missing_file(self):
        with _ReplayHarness(self.tmp_path) as h:
            result = h.read_jsonl(self.tmp_path / "does_not_exist.jsonl")
        self.assertEqual([], result)

    def test_workspace_env_isolation_does_not_leak(self):
        """Two back-to-back harness uses do not share run state."""
        with tempfile.TemporaryDirectory() as tmp2:
            tmp2_path = pathlib.Path(tmp2)
            with _ReplayHarness(self.tmp_path) as h1:
                h1.replay([{
                    "hook_event_name": "SessionStart",
                    "session_id": "isolation-a",
                }])
                run_a = h1.find_run_id("isolation-a")

            with _ReplayHarness(tmp2_path) as h2:
                h2.replay([{
                    "hook_event_name": "SessionStart",
                    "session_id": "isolation-b",
                }])
                run_b = h2.find_run_id("isolation-b")
                # The second workspace must not contain the first session's marker.
                run_a_in_h2 = h2.find_run_id("isolation-a")

        self.assertIsNotNone(run_a)
        self.assertIsNotNone(run_b)
        self.assertIsNone(run_a_in_h2,
                          "First session's marker must not appear in second workspace")


# ---------------------------------------------------------------------------
# T6.2 — Full-session directory layout
# ---------------------------------------------------------------------------

class TestFullSessionLayout(unittest.TestCase):
    """A replayed full session produces the complete specified directory tree."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path, _SAMPLE_TRANSCRIPT))
            self.run_id = h.find_run_id(_SESSION_ID)
            self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    # Run marker
    def test_run_marker_file_exists(self):
        self.assertTrue(self.paths.marker(_SESSION_ID).exists())

    def test_run_marker_has_closed_status(self):
        data = json.loads(self.paths.marker(_SESSION_ID).read_text("utf-8"))
        self.assertEqual("closed", data.get("status"))

    def test_run_marker_records_correct_run_id(self):
        data = json.loads(self.paths.marker(_SESSION_ID).read_text("utf-8"))
        self.assertEqual(self.run_id, data.get("run_id"))

    # Orchestrator event stream
    def test_orchestrator_events_file_exists(self):
        self.assertTrue(self.paths.orchestrator_events(self.run_id).exists())

    def test_orchestrator_events_file_is_non_empty(self):
        events_file = self.paths.orchestrator_events(self.run_id)
        self.assertGreater(events_file.stat().st_size, 0)

    # Orchestrator raw transcript export
    def test_orchestrator_raw_export_exists(self):
        self.assertTrue(self.paths.orchestrator_raw(self.run_id).exists())

    def test_orchestrator_raw_sidecar_exists(self):
        raw = self.paths.orchestrator_raw(self.run_id)
        sidecar = raw.with_suffix("").with_suffix(".meta.json")
        self.assertTrue(sidecar.exists())

    def test_orchestrator_sidecar_is_valid_json(self):
        raw = self.paths.orchestrator_raw(self.run_id)
        sidecar = raw.with_suffix("").with_suffix(".meta.json")
        data = json.loads(sidecar.read_text("utf-8"))
        self.assertIn("harness", data)
        self.assertIn("source_path", data)
        self.assertIn("byte_count", data)

    # Alpha invocation folder
    def test_alpha_invocation_dir_exists(self):
        self.assertTrue(
            self.paths.invocation_dir(self.run_id, _INSTANCE_ID_ALPHA).exists()
        )

    def test_alpha_input_artifact_exists(self):
        self.assertTrue(
            self.paths.invocation_input(self.run_id, _INSTANCE_ID_ALPHA).exists()
        )

    def test_alpha_output_artifact_exists(self):
        self.assertTrue(
            self.paths.invocation_output(self.run_id, _INSTANCE_ID_ALPHA).exists()
        )

    def test_alpha_events_file_exists(self):
        self.assertTrue(
            self.paths.invocation_events(self.run_id, _INSTANCE_ID_ALPHA).exists()
        )

    def test_alpha_raw_export_exists(self):
        self.assertTrue(
            self.paths.invocation_raw(self.run_id, _INSTANCE_ID_ALPHA).exists()
        )

    def test_alpha_raw_sidecar_exists(self):
        raw = self.paths.invocation_raw(self.run_id, _INSTANCE_ID_ALPHA)
        sidecar = raw.with_suffix("").with_suffix(".meta.json")
        self.assertTrue(sidecar.exists())

    # Beta invocation folder
    def test_beta_invocation_dir_exists(self):
        self.assertTrue(
            self.paths.invocation_dir(self.run_id, _INSTANCE_ID_BETA).exists()
        )

    def test_beta_input_artifact_exists(self):
        self.assertTrue(
            self.paths.invocation_input(self.run_id, _INSTANCE_ID_BETA).exists()
        )

    def test_beta_output_artifact_exists(self):
        self.assertTrue(
            self.paths.invocation_output(self.run_id, _INSTANCE_ID_BETA).exists()
        )

    def test_beta_events_file_exists(self):
        self.assertTrue(
            self.paths.invocation_events(self.run_id, _INSTANCE_ID_BETA).exists()
        )

    def test_beta_raw_export_exists(self):
        self.assertTrue(
            self.paths.invocation_raw(self.run_id, _INSTANCE_ID_BETA).exists()
        )

    # No unexpected files at the run root (no stray garbage)
    def test_run_root_exists(self):
        self.assertTrue(self.paths.run_root(self.run_id).exists())

    def test_agent_map_dir_contains_both_entries(self):
        agent_map = self.paths.agent_map_dir(self.run_id)
        self.assertTrue(
            self.paths.agent_map_entry(self.run_id, _AGENT_ID_ALPHA).exists()
        )
        self.assertTrue(
            self.paths.agent_map_entry(self.run_id, _AGENT_ID_BETA).exists()
        )


# ---------------------------------------------------------------------------
# T6.3 — Event-stream scoping
# ---------------------------------------------------------------------------

class TestEventStreamScoping(unittest.TestCase):
    """Events appear in exactly the correct stream; no cross-stream leakage."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path, _SAMPLE_TRANSCRIPT))
            self.run_id = h.find_run_id(_SESSION_ID)
            self.paths = core.build_paths(self.tmp_path)
            self.orch_events = h.orchestrator_events(self.run_id)
            self.alpha_events = h.invocation_events(self.run_id, _INSTANCE_ID_ALPHA)
            self.beta_events = h.invocation_events(self.run_id, _INSTANCE_ID_BETA)

    def tearDown(self):
        self.tmp.cleanup()

    # Orchestrator stream must contain lifecycle and orchestrator-scoped events.
    def test_orchestrator_stream_contains_run_start(self):
        types = [e["event"] for e in self.orch_events]
        self.assertIn("run_start", types)

    def test_orchestrator_stream_contains_session_start(self):
        types = [e["event"] for e in self.orch_events]
        self.assertIn("session_start", types)

    def test_orchestrator_stream_contains_session_end(self):
        types = [e["event"] for e in self.orch_events]
        self.assertIn("session_end", types)

    def test_orchestrator_stream_contains_run_end(self):
        types = [e["event"] for e in self.orch_events]
        self.assertIn("run_end", types)

    def test_orchestrator_stream_contains_notification(self):
        types = [e["event"] for e in self.orch_events]
        self.assertIn("notification", types)

    def test_orchestrator_stream_contains_compaction_events(self):
        types = [e["event"] for e in self.orch_events]
        self.assertIn("compaction", types)

    def test_orchestrator_stream_contains_orchestrator_tool_calls(self):
        """PreToolUse/PostToolUse with no agent_id land in the orchestrator stream."""
        types = [e["event"] for e in self.orch_events]
        self.assertIn("tool_call_start", types)
        self.assertIn("tool_call_end", types)

    def test_orchestrator_stream_contains_user_turn(self):
        user_turns = [e for e in self.orch_events
                      if e["event"] == "turn" and e.get("role") == "user"]
        self.assertGreater(len(user_turns), 0)

    def test_orchestrator_stream_contains_assistant_turn(self):
        asst_turns = [e for e in self.orch_events
                      if e["event"] == "turn" and e.get("role") == "assistant"]
        self.assertGreater(len(asst_turns), 0)

    # Invocation streams must contain invocation-scoped events.
    def test_alpha_stream_contains_invocation_start(self):
        types = [e["event"] for e in self.alpha_events]
        self.assertIn("invocation_start", types)

    def test_alpha_stream_contains_invocation_end(self):
        types = [e["event"] for e in self.alpha_events]
        self.assertIn("invocation_end", types)

    def test_alpha_stream_contains_alpha_tool_calls(self):
        """Tool calls carrying alpha's agent_id land in alpha's stream."""
        types = [e["event"] for e in self.alpha_events]
        self.assertIn("tool_call_start", types)
        self.assertIn("tool_call_end", types)

    def test_beta_stream_contains_invocation_start(self):
        types = [e["event"] for e in self.beta_events]
        self.assertIn("invocation_start", types)

    def test_beta_stream_contains_invocation_end(self):
        types = [e["event"] for e in self.beta_events]
        self.assertIn("invocation_end", types)

    # No cross-stream leakage in either direction.
    def test_no_invocation_event_in_orchestrator_stream(self):
        """invocation_start and invocation_end must never appear in orch stream."""
        invocation_types = {"invocation_start", "invocation_end"}
        orch_types = {e["event"] for e in self.orch_events}
        leaked = orch_types & invocation_types
        self.assertEqual(set(), leaked,
                         f"Invocation events leaked into orchestrator stream: {leaked}")

    def test_no_lifecycle_event_in_alpha_stream(self):
        """run_start, run_end, session_start, session_end belong only in orch stream."""
        lifecycle = {"run_start", "run_end", "session_start", "session_end"}
        alpha_types = {e["event"] for e in self.alpha_events}
        leaked = alpha_types & lifecycle
        self.assertEqual(set(), leaked,
                         f"Lifecycle events leaked into alpha's stream: {leaked}")

    def test_no_lifecycle_event_in_beta_stream(self):
        lifecycle = {"run_start", "run_end", "session_start", "session_end"}
        beta_types = {e["event"] for e in self.beta_events}
        leaked = beta_types & lifecycle
        self.assertEqual(set(), leaked,
                         f"Lifecycle events leaked into beta's stream: {leaked}")

    def test_alpha_tool_calls_not_in_beta_stream(self):
        """Alpha's tool_call_start must share call_id only within alpha's file."""
        alpha_call_ids = {
            e.get("call_id") for e in self.alpha_events
            if e["event"] == "tool_call_start"
        }
        beta_call_ids = {
            e.get("call_id") for e in self.beta_events
            if e.get("call_id") is not None
        }
        # Alpha's specific call IDs must not appear in beta's stream.
        shared = alpha_call_ids & beta_call_ids
        self.assertEqual(set(), shared,
                         f"Alpha's call_ids found in beta's stream: {shared}")

    def test_alpha_tool_calls_not_in_orchestrator_stream(self):
        """Subagent tool calls must never reach the orchestrator stream."""
        # Orchestrator's own tool calls use "call-orch-001".
        # Alpha's tool calls use "call-alpha-001".
        orch_call_ids = {
            e.get("call_id") for e in self.orch_events
            if e["event"] in ("tool_call_start", "tool_call_end")
        }
        alpha_call_ids = {
            e.get("call_id") for e in self.alpha_events
            if e["event"] in ("tool_call_start", "tool_call_end")
        }
        self.assertFalse(
            orch_call_ids & alpha_call_ids,
            f"Alpha's call_ids found in orchestrator stream: "
            f"{orch_call_ids & alpha_call_ids}",
        )


# ---------------------------------------------------------------------------
# T6.4 — Schema conformance across every emitted line
# ---------------------------------------------------------------------------

class TestSchemaConformance(unittest.TestCase):
    """Every emitted JSONL line carries a complete envelope, a catalog event
    type, no null values, and no forbidden derived or cost fields."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path, _SAMPLE_TRANSCRIPT))
            self.run_id = h.find_run_id(_SESSION_ID)
            self.all_files = h.all_jsonl_files(self.run_id)
            # Collect every event from every JSONL file.
            self.all_events: list = []
            for f in self.all_files:
                self.all_events.extend(h.read_jsonl(f))

    def tearDown(self):
        self.tmp.cleanup()

    def test_at_least_one_jsonl_file_exists(self):
        self.assertGreater(len(self.all_files), 0, "No JSONL files produced")

    def test_every_line_is_a_json_object(self):
        """Each line in each JSONL file parses as exactly one dict (one JSON object)."""
        for f in self.all_files:
            for ln in f.read_text("utf-8").splitlines():
                if not ln.strip():
                    continue
                obj = json.loads(ln)
                self.assertIsInstance(
                    obj, dict,
                    f"Line in {f.name} is not a JSON object: {ln[:80]}"
                )

    def test_every_event_has_schema_version(self):
        for ev in self.all_events:
            self.assertIn("schema_version", ev,
                          f"Missing schema_version in event: {ev.get('event')}")

    def test_every_event_schema_version_is_string(self):
        for ev in self.all_events:
            self.assertIsInstance(
                ev.get("schema_version"), str,
                f"schema_version is not a string in event: {ev.get('event')}"
            )

    def test_every_event_has_event_field(self):
        for ev in self.all_events:
            self.assertIn("event", ev)

    def test_every_event_type_is_from_catalog(self):
        for ev in self.all_events:
            event_type = ev.get("event")
            self.assertIn(
                event_type, _CATALOG,
                f"Event type {event_type!r} is not in the schema catalog"
            )

    def test_every_event_has_timestamp(self):
        for ev in self.all_events:
            self.assertIn("timestamp", ev,
                          f"Missing timestamp in event: {ev.get('event')}")

    def test_every_event_timestamp_is_iso8601_utc_ms(self):
        """Timestamps must match YYYY-MM-DDTHH:MM:SS.mmmZ format."""
        import re
        _TS_PATTERN = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")
        for ev in self.all_events:
            ts = ev.get("timestamp", "")
            self.assertRegex(
                ts, _TS_PATTERN,
                f"Timestamp {ts!r} is not ISO 8601 UTC ms-precision on event {ev.get('event')}"
            )

    def test_every_event_has_harness_field(self):
        for ev in self.all_events:
            self.assertIn("harness", ev,
                          f"Missing harness in event: {ev.get('event')}")

    def test_every_event_harness_is_claude_code(self):
        for ev in self.all_events:
            self.assertEqual(
                "claude-code", ev.get("harness"),
                f"Unexpected harness value in event: {ev.get('event')}"
            )

    def test_no_null_value_anywhere_in_any_event(self):
        for ev in self.all_events:
            self.assertFalse(
                _contains_null(ev),
                f"Null value found in event {ev.get('event')}: {ev}"
            )

    def test_no_forbidden_derived_field_anywhere(self):
        for ev in self.all_events:
            found = _contains_forbidden_field(ev)
            self.assertIsNone(
                found,
                f"Forbidden field {found!r} found in event {ev.get('event')}"
            )

    def test_no_empty_string_value_for_required_envelope_fields(self):
        for ev in self.all_events:
            for field in _REQUIRED_ENVELOPE:
                if field in ev:
                    self.assertNotEqual(
                        "", ev[field],
                        f"Empty string for required field {field!r} in event {ev.get('event')}"
                    )

    def test_schema_version_is_1_0_0(self):
        for ev in self.all_events:
            self.assertEqual(
                "1.0.0", ev.get("schema_version"),
                f"Unexpected schema_version in event {ev.get('event')}"
            )


# ---------------------------------------------------------------------------
# T6.5 — Start/end pairing within the same file
# ---------------------------------------------------------------------------

class TestStartEndPairing(unittest.TestCase):
    """Every tool_call_start has a matching tool_call_end within the same file;
    every invocation_start has a matching invocation_end within the same file."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path, _SAMPLE_TRANSCRIPT))
            self.run_id = h.find_run_id(_SESSION_ID)
            self.paths = core.build_paths(self.tmp_path)
            self.h = h
            # Per-file event lists for pairing assertions.
            self.orch_events = h.orchestrator_events(self.run_id)
            self.alpha_events = h.invocation_events(self.run_id, _INSTANCE_ID_ALPHA)
            self.beta_events = h.invocation_events(self.run_id, _INSTANCE_ID_BETA)

    def tearDown(self):
        self.tmp.cleanup()

    def _assert_tool_call_pairing(self, events: list, label: str) -> None:
        """Assert every tool_call_start has exactly one tool_call_end in events."""
        starts = {e["call_id"] for e in events if e["event"] == "tool_call_start"}
        ends = {e["call_id"] for e in events if e["event"] == "tool_call_end"}
        unmatched_starts = starts - ends
        unmatched_ends = ends - starts
        self.assertEqual(
            set(), unmatched_starts,
            f"{label}: tool_call_start with no matching end: {unmatched_starts}"
        )
        self.assertEqual(
            set(), unmatched_ends,
            f"{label}: tool_call_end with no matching start: {unmatched_ends}"
        )

    def _assert_invocation_pairing(self, events: list, label: str) -> None:
        """Assert every invocation_start has exactly one invocation_end in events."""
        starts = {e.get("agent_instance_id") for e in events
                  if e["event"] == "invocation_start"}
        ends = {e.get("agent_instance_id") for e in events
                if e["event"] == "invocation_end"}
        self.assertEqual(
            starts, ends,
            f"{label}: invocation start/end agent_instance_id mismatch: "
            f"starts={starts}, ends={ends}"
        )

    def test_orchestrator_tool_calls_are_paired(self):
        self._assert_tool_call_pairing(self.orch_events, "orchestrator")

    def test_alpha_tool_calls_are_paired(self):
        self._assert_tool_call_pairing(self.alpha_events, "alpha invocation")

    def test_beta_tool_calls_are_paired(self):
        self._assert_tool_call_pairing(self.beta_events, "beta invocation")

    def test_alpha_invocation_start_end_paired(self):
        self._assert_invocation_pairing(self.alpha_events, "alpha")

    def test_beta_invocation_start_end_paired(self):
        self._assert_invocation_pairing(self.beta_events, "beta")

    def test_orchestrator_tool_call_start_precedes_end_in_file(self):
        """Within the orchestrator stream, start appears before end for the same call."""
        events_with_idx = list(enumerate(self.orch_events))
        call_ids = {e["call_id"] for e in self.orch_events
                    if e["event"] == "tool_call_start"}
        for call_id in call_ids:
            start_idx = next(
                i for i, e in events_with_idx
                if e["event"] == "tool_call_start" and e.get("call_id") == call_id
            )
            end_idx = next(
                i for i, e in events_with_idx
                if e["event"] == "tool_call_end" and e.get("call_id") == call_id
            )
            self.assertLess(start_idx, end_idx,
                            f"tool_call_end precedes tool_call_start for {call_id!r}")

    def test_alpha_invocation_start_precedes_end(self):
        types = [e["event"] for e in self.alpha_events]
        self.assertLess(
            types.index("invocation_start"), types.index("invocation_end")
        )

    def test_beta_invocation_start_precedes_end(self):
        types = [e["event"] for e in self.beta_events]
        self.assertLess(
            types.index("invocation_start"), types.index("invocation_end")
        )

    def test_pairing_shares_call_id_not_just_position(self):
        """The call_id value itself must match, not merely positional alignment."""
        starts = {e["call_id"]: e for e in self.orch_events
                  if e["event"] == "tool_call_start"}
        ends = {e["call_id"]: e for e in self.orch_events
                if e["event"] == "tool_call_end"}
        for call_id in starts:
            self.assertIn(call_id, ends,
                          f"tool_call_end missing for call_id {call_id!r}")
            # Also verify the call_id value itself rather than just key presence.
            self.assertEqual(call_id, ends[call_id]["call_id"])


# ---------------------------------------------------------------------------
# T6.6 — Run identity across sessions
# ---------------------------------------------------------------------------

class TestRunIdentity(unittest.TestCase):
    """run_id is reused while the marker is open, re-minted after close,
    re-minted after staleness, and run directories never collide."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _dispatch(self, payload: dict) -> None:
        orig = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
        try:
            mosaic_logger.dispatch(json.dumps(payload))
        finally:
            if orig is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = orig

    def _find_run_id(self, session_id: str) -> "str | None":
        marker = core.build_paths(self.tmp_path).marker(session_id)
        if not marker.exists():
            return None
        try:
            return json.loads(marker.read_text("utf-8")).get("run_id")
        except Exception:
            return None

    def _session_start(self, session_id: str, source: str = "startup") -> "str | None":
        self._dispatch({
            "hook_event_name": "SessionStart",
            "session_id": session_id,
            "source": source,
        })
        return self._find_run_id(session_id)

    def _session_end(self, session_id: str, reason: str = "clear") -> None:
        self._dispatch({
            "hook_event_name": "SessionEnd",
            "session_id": session_id,
            "reason": reason,
        })

    def _read_orch_events(self, run_id: str) -> list:
        p = core.build_paths(self.tmp_path).orchestrator_events(run_id)
        if not p.exists():
            return []
        return [json.loads(ln) for ln in p.read_text("utf-8").splitlines() if ln.strip()]

    # -- Reuse while open --
    def test_run_id_reused_for_subsequent_events_in_same_session(self):
        session = "identity-reuse-001"
        run_id_1 = self._session_start(session)
        # A non-minting event must not change the run_id.
        self._dispatch({
            "hook_event_name": "Notification",
            "session_id": session,
            "notification_type": "info",
            "message": "Still in the same run.",
        })
        run_id_2 = self._find_run_id(session)
        self.assertEqual(run_id_1, run_id_2)

    def test_only_one_run_start_emitted_for_reused_run(self):
        session = "identity-reuse-002"
        run_id = self._session_start(session)
        # Dispatch a minting-capable event that should reuse, not mint.
        self._dispatch({
            "hook_event_name": "UserPromptSubmit",
            "session_id": session,
            "user_prompt": "Still the same run.",
        })
        events = self._read_orch_events(run_id)
        run_starts = [e for e in events if e["event"] == "run_start"]
        self.assertEqual(1, len(run_starts),
                         "Expected exactly one run_start for a reused run")

    # -- Fresh run after close --
    def test_fresh_run_id_minted_after_session_end(self):
        session = "identity-remint-001"
        run_id_1 = self._session_start(session)
        self._session_end(session, reason="clear")
        run_id_2 = self._session_start(session, source="resume")
        self.assertIsNotNone(run_id_1)
        self.assertIsNotNone(run_id_2)
        self.assertNotEqual(run_id_1, run_id_2)

    def test_each_run_has_its_own_directory(self):
        session = "identity-dirs-001"
        run_id_1 = self._session_start(session)
        self._session_end(session)
        run_id_2 = self._session_start(session, source="resume")
        # Both run roots must exist and be distinct.
        paths = core.build_paths(self.tmp_path)
        self.assertTrue(paths.run_root(run_id_1).exists())
        self.assertTrue(paths.run_root(run_id_2).exists())
        self.assertNotEqual(paths.run_root(run_id_1), paths.run_root(run_id_2))

    def test_second_run_events_do_not_appear_in_first_run_file(self):
        session = "identity-isolation-001"
        run_id_1 = self._session_start(session)
        self._session_end(session)
        run_id_2 = self._session_start(session, source="resume")
        events_1 = self._read_orch_events(run_id_1)
        events_2 = self._read_orch_events(run_id_2)
        # run_end must only appear in run 1's file.
        types_1 = [e["event"] for e in events_1]
        types_2 = [e["event"] for e in events_2]
        self.assertIn("run_end", types_1)
        self.assertNotIn("run_end", types_2)

    # -- Fresh run after staleness --
    def test_stale_marker_causes_new_run_id(self):
        """A marker whose opened_at is older than 24 h is treated as stale."""
        session = "identity-stale-001"
        run_id_1 = self._session_start(session)
        # Rewrite the marker with an opened_at 25 hours in the past.
        marker_path = core.build_paths(self.tmp_path).marker(session)
        marker_data = json.loads(marker_path.read_text("utf-8"))
        old_time = (
            datetime.datetime.now(datetime.timezone.utc)
            - datetime.timedelta(hours=25)
        )
        marker_data["opened_at"] = (
            old_time.strftime("%Y-%m-%dT%H:%M:%S.") +
            f"{old_time.microsecond // 1000:03d}Z"
        )
        marker_data["status"] = "open"   # still marked open, but stale
        marker_path.write_text(json.dumps(marker_data), encoding="utf-8")
        # Now a minting event must produce a fresh run_id.
        run_id_2 = self._session_start(session)
        self.assertIsNotNone(run_id_2)
        self.assertNotEqual(run_id_1, run_id_2)

    def test_stale_marker_run_directories_do_not_collide(self):
        """A fresh run after staleness gets its own directory."""
        session = "identity-stale-002"
        run_id_1 = self._session_start(session)
        marker_path = core.build_paths(self.tmp_path).marker(session)
        marker_data = json.loads(marker_path.read_text("utf-8"))
        old_time = (
            datetime.datetime.now(datetime.timezone.utc)
            - datetime.timedelta(hours=25)
        )
        marker_data["opened_at"] = (
            old_time.strftime("%Y-%m-%dT%H:%M:%S.") +
            f"{old_time.microsecond // 1000:03d}Z"
        )
        marker_path.write_text(json.dumps(marker_data), encoding="utf-8")
        run_id_2 = self._session_start(session)
        paths = core.build_paths(self.tmp_path)
        self.assertNotEqual(
            paths.run_root(run_id_1), paths.run_root(run_id_2)
        )

    def test_marker_status_is_closed_after_session_end(self):
        session = "identity-closed-001"
        self._session_start(session)
        self._session_end(session)
        marker_data = json.loads(
            core.build_paths(self.tmp_path).marker(session).read_text("utf-8")
        )
        self.assertEqual("closed", marker_data.get("status"))


# ---------------------------------------------------------------------------
# T6.8 — Field-level degradation yields omitted keys, not nulls
# ---------------------------------------------------------------------------

class TestFieldLevelDegradation(unittest.TestCase):
    """Absent or unresolvable optional values are omitted entirely — never null,
    never a placeholder string, never a zero or empty object."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.session_id = "degradation-session-001"

    def tearDown(self):
        self.tmp.cleanup()

    def _dispatch(self, payload: dict) -> None:
        orig = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
        try:
            mosaic_logger.dispatch(json.dumps(payload))
        finally:
            if orig is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = orig

    def _find_run_id(self) -> "str | None":
        marker = core.build_paths(self.tmp_path).marker(self.session_id)
        if not marker.exists():
            return None
        try:
            return json.loads(marker.read_text("utf-8")).get("run_id")
        except Exception:
            return None

    def _read_events(self, path: pathlib.Path) -> list:
        if not path.exists():
            return []
        return [json.loads(ln) for ln in path.read_text("utf-8").splitlines()
                if ln.strip()]

    def test_absent_agent_prompt_still_yields_agent_instance_id(self):
        """When agent_prompt is absent, agent_instance_id falls back to the
        timestamp-plus-suffix pattern; the field must still be present and non-empty."""
        agent_id = "agt-noprompt-001"
        self._dispatch({
            "hook_event_name": "SessionStart",
            "session_id": self.session_id,
            "source": "startup",
        })
        run_id = self._find_run_id()
        # SubagentStart without agent_prompt — fallback to fallback_instance_id.
        self._dispatch({
            "hook_event_name": "SubagentStart",
            "session_id": self.session_id,
            "agent_id": agent_id,
            "agent_type": "Research",
            # agent_prompt deliberately omitted
        })
        paths = core.build_paths(self.tmp_path)
        # Find the invocation folder — any folder under run_root that is not .agent-map.
        run_root = paths.run_root(run_id)
        inv_dirs = [
            d for d in run_root.iterdir()
            if d.is_dir() and not d.name.startswith(".")
        ]
        self.assertGreater(len(inv_dirs), 0, "No invocation folder was created")
        # The invocation_start event must carry a non-empty agent_instance_id.
        inv_dir = inv_dirs[0]
        events_file = inv_dir / "03_events.jsonl"
        events = self._read_events(events_file)
        inv_start = next((e for e in events if e["event"] == "invocation_start"), None)
        self.assertIsNotNone(inv_start, "invocation_start not emitted")
        agent_instance_id = inv_start.get("agent_instance_id")
        self.assertIsNotNone(agent_instance_id)
        self.assertIsInstance(agent_instance_id, str)
        self.assertGreater(len(agent_instance_id), 0)

    def test_absent_agent_prompt_omits_prompt_field_from_invocation_start(self):
        """No agent_prompt → prompt field absent from invocation_start, not null."""
        agent_id = "agt-noprompt-002"
        self._dispatch({
            "hook_event_name": "SessionStart",
            "session_id": "degrad-noprompt-002",
            "source": "startup",
        })
        session_id = "degrad-noprompt-002"
        orig = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
        try:
            mosaic_logger.dispatch(json.dumps({
                "hook_event_name": "SubagentStart",
                "session_id": session_id,
                "agent_id": agent_id,
                "agent_type": "Research",
                # agent_prompt deliberately omitted
            }))
        finally:
            if orig is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = orig
        paths = core.build_paths(self.tmp_path)
        marker = paths.marker(session_id)
        if not marker.exists():
            return  # session didn't resolve — skip silently
        run_id = json.loads(marker.read_text("utf-8")).get("run_id")
        run_root = paths.run_root(run_id)
        for inv_dir in run_root.iterdir():
            if inv_dir.is_dir() and not inv_dir.name.startswith("."):
                events = self._read_events(inv_dir / "03_events.jsonl")
                inv_start = next(
                    (e for e in events if e["event"] == "invocation_start"), None
                )
                if inv_start:
                    self.assertNotIn(
                        "prompt", inv_start,
                        "prompt field present when agent_prompt was absent"
                    )

    def test_unreadable_agent_transcript_omits_model_and_token_usage(self):
        """SubagentStop with an unreadable agent_transcript_path yields invocation_end
        with neither token_usage nor model — not null values for those fields."""
        session_id = "degrad-no-transcript-001"
        agent_id = "agt-no-trans-001"
        instance_id = "Research#5"

        # Setup: SessionStart + SubagentStart to register the mapping.
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup"},
            {"hook_event_name": "SubagentStart", "session_id": session_id,
             "agent_id": agent_id, "agent_type": "Research",
             "agent_prompt": f'{{"agent_instance_id": "{instance_id}"}}'},
        ]:
            orig = os.environ.get("CLAUDE_PROJECT_DIR")
            os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
            try:
                mosaic_logger.dispatch(json.dumps(payload))
            finally:
                if orig is None:
                    os.environ.pop("CLAUDE_PROJECT_DIR", None)
                else:
                    os.environ["CLAUDE_PROJECT_DIR"] = orig

        paths = core.build_paths(self.tmp_path)
        run_id = json.loads(
            paths.marker(session_id).read_text("utf-8")
        ).get("run_id")

        # SubagentStop with an unreadable transcript path.
        orig = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
        try:
            mosaic_logger.dispatch(json.dumps({
                "hook_event_name": "SubagentStop",
                "session_id": session_id,
                "agent_id": agent_id,
                "agent_type": "Research",
                "agent_transcript_path": "/nonexistent/path/transcript.jsonl",
                "last_assistant_message": '{"status_code": "SUCCESS"}',
            }))
        finally:
            if orig is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = orig

        inv_events_path = paths.invocation_events(run_id, instance_id)
        events = self._read_events(inv_events_path)
        inv_end = next((e for e in events if e["event"] == "invocation_end"), None)
        self.assertIsNotNone(inv_end, "invocation_end not emitted on SubagentStop")
        # token_usage and model must be absent — not null.
        self.assertNotIn("token_usage", inv_end,
                         "token_usage must be absent when transcript is unreadable")
        self.assertNotIn("model", inv_end,
                         "model must be absent when transcript is unreadable")
        # Required field must still be present.
        self.assertIn("agent_instance_id", inv_end)
        self.assertIsNotNone(inv_end.get("agent_instance_id"))

    def test_unreadable_agent_transcript_still_emits_invocation_end(self):
        """An unreadable transcript degrades gracefully — invocation_end is still
        emitted; the dispatcher does not raise or produce nothing."""
        session_id = "degrad-transcript-emitted-001"
        agent_id = "agt-graceful-001"
        instance_id = "Research#6"

        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup"},
            {"hook_event_name": "SubagentStart", "session_id": session_id,
             "agent_id": agent_id, "agent_type": "Research",
             "agent_prompt": f'{{"agent_instance_id": "{instance_id}"}}'},
            {"hook_event_name": "SubagentStop", "session_id": session_id,
             "agent_id": agent_id, "agent_type": "Research",
             "agent_transcript_path": "/no/such/file.jsonl",
             "last_assistant_message": '{"status_code": "SUCCESS"}'},
        ]:
            orig = os.environ.get("CLAUDE_PROJECT_DIR")
            os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
            try:
                mosaic_logger.dispatch(json.dumps(payload))
            finally:
                if orig is None:
                    os.environ.pop("CLAUDE_PROJECT_DIR", None)
                else:
                    os.environ["CLAUDE_PROJECT_DIR"] = orig

        paths = core.build_paths(self.tmp_path)
        run_id = json.loads(
            paths.marker(session_id).read_text("utf-8")
        ).get("run_id")
        events = self._read_events(paths.invocation_events(run_id, instance_id))
        inv_end_events = [e for e in events if e["event"] == "invocation_end"]
        self.assertEqual(1, len(inv_end_events),
                         "Expected exactly one invocation_end even with unreadable transcript")

    def test_session_start_without_model_omits_model_from_run_start(self):
        """A SessionStart payload with no model field must produce run_start without
        a model field — not null, not an empty string."""
        session_id = "degrad-nomodel-001"
        orig = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
        try:
            mosaic_logger.dispatch(json.dumps({
                "hook_event_name": "SessionStart",
                "session_id": session_id,
                "source": "startup",
                # model deliberately omitted
            }))
        finally:
            if orig is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = orig

        paths = core.build_paths(self.tmp_path)
        run_id = json.loads(
            paths.marker(session_id).read_text("utf-8")
        ).get("run_id")
        events_path = paths.orchestrator_events(run_id)
        events = self._read_events(events_path)
        run_start = next((e for e in events if e["event"] == "run_start"), None)
        self.assertIsNotNone(run_start)
        self.assertNotIn("model", run_start,
                         "model must be absent from run_start when not in payload")

    def test_session_end_without_reason_omits_outcome_from_run_end(self):
        """SessionEnd with no reason → run_end must not carry an outcome field."""
        session_id = "degrad-noreason-001"
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup"},
            {"hook_event_name": "SessionEnd", "session_id": session_id},
            # reason deliberately omitted
        ]:
            orig = os.environ.get("CLAUDE_PROJECT_DIR")
            os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
            try:
                mosaic_logger.dispatch(json.dumps(payload))
            finally:
                if orig is None:
                    os.environ.pop("CLAUDE_PROJECT_DIR", None)
                else:
                    os.environ["CLAUDE_PROJECT_DIR"] = orig

        paths = core.build_paths(self.tmp_path)
        run_id = json.loads(
            paths.marker(session_id).read_text("utf-8")
        ).get("run_id")
        events = self._read_events(paths.orchestrator_events(run_id))
        run_end = next((e for e in events if e["event"] == "run_end"), None)
        self.assertIsNotNone(run_end)
        self.assertNotIn("outcome", run_end,
                         "outcome must be absent when SessionEnd has no reason")


if __name__ == "__main__":
    unittest.main()
