"""End-to-end synthetic session simulation tests for the vscode-ghcp mosaic-logger adapter.

Drives the full dispatcher pipeline by feeding a realistic hook payload sequence through
mosaic_logger.dispatch() against a temporary workspace and asserting the resulting on-disk
log tree, event streams, schema conformance, and invocation pairing.

Key vscode-ghcp behaviors exercised here:
- Workspace root resolved from payload['cwd'], not from any environment variable (D2)
- SubagentStart carries agent_name but no agent_id; correlation key is agent_name (D3)
- Matched invocation_start/invocation_end pair produced despite SubagentStart having no
  agent_id — the correlation flows through: PreToolUse (pending dispatch) →
  SubagentStart (pops dispatch, writes agent-map keyed on agent_name) →
  SubagentStop (resolves agent-map by agent_name, emits invocation_end)
- Ordinary tool output is nested under tool_result.text_result_for_llm (D4)
- Session lifecycle anchors on SessionStart/Stop; no SessionEnd event (D5)
- run_end and session_end are both emitted on Stop, not on a separate SessionEnd (D5)
- Every emitted line stamps harness as 'vscode-ghcp' (D1)
- No null value anywhere in any emitted event

Event-routing note:
  Different events carry different run_id context, so events are distributed across
  multiple run-bucket directories within OrchestrationLogs/:
  - 'unknown-run': events fired without a extractable run_id context
    (SessionStart → run_start, session_start; PreToolUse/PostToolUse → tool events;
    Stop without run_id in last_assistant_message → session_end, run_end)
  - '{extracted-run-id}': events fired when a run_id was resolved
    (UserPromptSubmit with run_id in prompt → user turn;
    SubagentStart after popping dispatch → invocation events in sub-dir)
  E2E tests aggregate across all run buckets for presence assertions,
  while invocation-correlation tests use the specific invocation bucket.
"""

import json
import os
import pathlib
import re
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core


# ---------------------------------------------------------------------------
# Constants shared across all test classes in this module
# ---------------------------------------------------------------------------

_SESSION_ID = "e2e-vscode-session-001"

# Canonical run_id in MOSAIC format: {YYYYMMDD}T{HHMMSS}Z-{4 lowercase hex}.
# Embedded in UserPromptSubmit's prompt and in PreToolUse's tool_input.prompt
# so that SubagentStart can recover it from the pending dispatch queue,
# placing the invocation directory under this run_id bucket.
_RUN_ID = "20260101T120000Z-abcd"

# The agent_name used in SubagentStart/SubagentStop — the correlation key for
# this harness (D3). SubagentStart carries ONLY this name, no agent_id.
_AGENT_NAME = "Research"

# The agent_instance_id extracted from PreToolUse's tool_input.prompt.
# The invocation directory is named by sanitize_component of this value.
# '#' is not in RESERVED_FILENAME_CHARS so the name is unchanged.
_INSTANCE_ID = "Research#1"

# Canonical event catalog from schema §3.1 — the complete closed set.
_CATALOG = frozenset({
    "run_start", "run_end", "session_start", "session_end",
    "invocation_start", "invocation_end", "turn",
    "tool_call_start", "tool_call_end", "notification", "compaction",
})

# The required envelope fields on every emitted event.
_REQUIRED_ENVELOPE = ("schema_version", "event", "timestamp", "harness")

# Tool call id for the ordinary (non-dispatch) tool call.
_TOOL_CALL_ID = "call-orch-read-001"


# ---------------------------------------------------------------------------
# Replay harness
# ---------------------------------------------------------------------------

class _ReplayHarness:
    """Drives mosaic_logger.dispatch() against an isolated temporary workspace.

    For vscode-ghcp, workspace root is resolved from the 'cwd' field in each
    payload — no environment variable is set or mutated (D2 divergence). The
    harness simply dispatches each payload in order through mosaic_logger.dispatch().

    Events may be distributed across multiple run-bucket directories because
    different events carry different run_id context. Use all_jsonl_files() and
    all_orchestrator_events() to aggregate across all buckets for presence
    assertions; use invocation_events(run_id, instance_id) for correlation tests.
    """

    def __init__(self, workspace: pathlib.Path):
        self.workspace = workspace

    def __enter__(self) -> "_ReplayHarness":
        return self

    def __exit__(self, *_) -> None:
        pass

    def replay(self, payloads: list) -> None:
        """Dispatch each payload dict in order, serializing each to JSON first."""
        for payload in payloads:
            mosaic_logger.dispatch(json.dumps(payload))

    # ------------------------------------------------------------------
    # Inspection helpers
    # ------------------------------------------------------------------

    def find_run_id(self) -> "str | None":
        """Return the effective run_id for the invocation bucket.

        Scans OrchestrationLogs/ for a timestamp-format run_id folder first —
        this is where SubagentStart places the invocation directory after recovering
        the run_id from the pending dispatch. Falls back to 'unknown-run' if no
        timestamp-format folder exists. Returns None if the log root does not exist.
        """
        _RUN_ID_RE = re.compile(r"^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")
        log_root = core.build_paths(self.workspace).root
        if not log_root.exists():
            return None
        for child in log_root.iterdir():
            if child.is_dir() and _RUN_ID_RE.match(child.name):
                return child.name
        unknown_run = log_root / "unknown-run"
        if unknown_run.exists():
            return "unknown-run"
        return None

    def all_run_dirs(self) -> "list[pathlib.Path]":
        """Return every run directory (including unknown-run) under OrchestrationLogs/."""
        log_root = core.build_paths(self.workspace).root
        if not log_root.exists():
            return []
        return [d for d in log_root.iterdir() if d.is_dir()]

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
        """Read orchestrator events from a specific run_id bucket."""
        return self.read_jsonl(
            core.build_paths(self.workspace).orchestrator_events(run_id)
        )

    def all_orchestrator_events(self) -> list:
        """Aggregate orchestrator events from every run-bucket directory.

        Events are distributed across multiple run-bucket directories because
        different events carry different run_id context. This method returns the
        union of all orchestrator event streams for presence assertions that do not
        depend on which specific bucket an event landed in.
        """
        paths = core.build_paths(self.workspace)
        result = []
        for run_dir in self.all_run_dirs():
            events_path = run_dir / "00_orchestrator_events.jsonl"
            result.extend(self.read_jsonl(events_path))
        return result

    def invocation_events(self, run_id: str, agent_instance_id: str) -> list:
        return self.read_jsonl(
            core.build_paths(self.workspace).invocation_events(run_id, agent_instance_id)
        )

    def all_jsonl_files(self) -> "list[pathlib.Path]":
        """Return every .jsonl path under OrchestrationLogs/, across ALL run buckets."""
        log_root = core.build_paths(self.workspace).root
        if not log_root.exists():
            return []
        return list(log_root.rglob("*.jsonl"))

    def run_root(self, run_id: str) -> pathlib.Path:
        return core.build_paths(self.workspace).run_root(run_id)

    def log_root(self) -> pathlib.Path:
        return core.build_paths(self.workspace).root


# ---------------------------------------------------------------------------
# Full-session payload builder
# ---------------------------------------------------------------------------

def _full_session_payloads(
    tmp_path: pathlib.Path,
    transcript_path: "pathlib.Path | None" = None,
) -> list:
    """Return the vscode-ghcp full-session payload sequence.

    Covers the sequence specified in Stage 4:
      1. SessionStart — source: startup
      2. UserPromptSubmit — prompt embedding run_id (routes user turn to run_id bucket)
      3. PreToolUse (Agent dispatch) — tool_input.prompt carrying agent_instance_id
         and run_id; this populates the pending-dispatch queue under 'unknown-run'
         (no run_id context at PreToolUse time)
      4. SubagentStart — agent_name only, no agent_id (D3, key test of AC4.3)
         Pops the pending dispatch, recovers run_id, places invocation under run_id bucket
      5. PreToolUse (ordinary) — no agent_id, routes to orchestrator stream (unknown-run)
      6. PostToolUse (ordinary) — tool_result.text_result_for_llm present (D4)
      7. SubagentStop — agent_id + agent_name + last_assistant_message with status code
         and run_id; triggers invocation_end in the run_id bucket
      8. Stop — stop_reason end_turn, stop_hook_active false (D5)
         session_end and run_end go to unknown-run (no run_id in last_assistant_message)
    """
    tp = str(transcript_path) if transcript_path else None

    def _base(event: str, **extra) -> dict:
        p: dict = {
            "hook_event_name": event,
            "session_id": _SESSION_ID,
            "cwd": str(tmp_path),
        }
        if tp:
            p["transcript_path"] = tp
        p.update(extra)
        return p

    return [
        # 1 — SessionStart: emits run_start then session_start (to unknown-run).
        _base("SessionStart", source="startup"),

        # 2 — UserPromptSubmit: embeds run_id so resolve_run_identity extracts it.
        #     The user turn lands in the run_id bucket.
        _base(
            "UserPromptSubmit",
            prompt=(
                f"Running orchestration workflow run_id: {_RUN_ID}. "
                "Please start the research task."
            ),
        ),

        # 3 — PreToolUse with Agent dispatch tool: tool_input.prompt embeds
        #     agent_instance_id and run_id so the pending dispatch is populated.
        #     Context at this point: no run_id (PreToolUse not in RUN_ID_PROMPT_FIELDS),
        #     so pending dispatch is queued under the 'unknown-run' bucket.
        _base(
            "PreToolUse",
            tool_name="Agent",
            tool_use_id="call-dispatch-001",
            tool_input={
                "prompt": (
                    f"agent_instance_id: {_INSTANCE_ID}\n"
                    f"run_id: {_RUN_ID}\n"
                    "task: conduct research on the topic"
                )
            },
        ),

        # 4 — SubagentStart: carries agent_name ONLY — no agent_id (D3).
        #     Pops pending dispatch from 'unknown-run' queue, recovers run_id
        #     and agent_instance_id; places invocation directory under run_id bucket.
        _base(
            "SubagentStart",
            agent_name=_AGENT_NAME,
            # agent_id deliberately absent — this is the D3 divergence
        ),

        # 5 — PreToolUse (ordinary, no agent_id): routes to orchestrator stream.
        #     No run_id at this point → lands in unknown-run orchestrator stream.
        _base(
            "PreToolUse",
            tool_name="Read",
            tool_use_id=_TOOL_CALL_ID,
            tool_input={"file_path": "/workspace/data.txt"},
        ),

        # 6 — PostToolUse (ordinary): output nested under tool_result.text_result_for_llm (D4).
        _base(
            "PostToolUse",
            tool_name="Read",
            tool_use_id=_TOOL_CALL_ID,
            tool_result={"text_result_for_llm": "Research data loaded successfully."},
        ),

        # 7 — SubagentStop: carries agent_id + agent_name (resolve_agent_key prefers
        #     agent_name so the correlation key is still "Research") and
        #     last_assistant_message containing run_id for stop-side bucket lookup.
        _base(
            "SubagentStop",
            agent_id="agt-vscode-001",
            agent_name=_AGENT_NAME,
            last_assistant_message=(
                f'{{"status_code": "SUCCESS", '
                f'"run_id": "{_RUN_ID}", '
                '"result": "Research complete."}}'
            ),
        ),

        # 8 — Stop: emits session_end + run_end (D5). No run_id in last_assistant_message
        #     here, so session_end/run_end land in the unknown-run orchestrator stream.
        _base(
            "Stop",
            stop_reason="end_turn",
            stop_hook_active=False,
            last_assistant_message="Orchestration workflow complete.",
        ),
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


# ---------------------------------------------------------------------------
# TestReplayHarness — self-check
# ---------------------------------------------------------------------------

class TestReplayHarness(unittest.TestCase):
    """The vscode-ghcp replay harness correctly drives dispatch() and makes the
    tree readable without relying on any environment variable."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_replay_creates_log_directory_after_session_start(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "harness-check-001",
            "source": "startup",
            "cwd": str(self.tmp_path),
        }
        with _ReplayHarness(self.tmp_path) as h:
            h.replay([payload])
            run_id = h.find_run_id()
        self.assertIsNotNone(run_id,
                             "SessionStart must create a log directory (run_id or unknown-run)")

    def test_replay_single_payload_produces_orchestrator_events(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "harness-check-002",
            "source": "startup",
            "cwd": str(self.tmp_path),
        }
        with _ReplayHarness(self.tmp_path) as h:
            h.replay([payload])
            events = h.all_orchestrator_events()
        self.assertGreater(len(events), 0,
                           "At least run_start and session_start expected")

    def test_replay_full_session_produces_multiple_events(self):
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path))
            events = h.all_orchestrator_events()
        self.assertGreater(len(events), 4,
                           "Full session must produce several orchestrator events across all buckets")

    def test_harness_find_run_id_returns_none_when_no_log_root(self):
        with _ReplayHarness(self.tmp_path) as h:
            result = h.find_run_id()
        self.assertIsNone(result,
                          "find_run_id must return None when no events have been dispatched")

    def test_harness_read_jsonl_returns_empty_list_for_missing_file(self):
        with _ReplayHarness(self.tmp_path) as h:
            result = h.read_jsonl(self.tmp_path / "does_not_exist.jsonl")
        self.assertEqual([], result)

    def test_workspace_isolation_two_harnesses_write_separate_trees(self):
        """Two back-to-back harness uses write to separate file trees."""
        with tempfile.TemporaryDirectory() as tmp2:
            tmp2_path = pathlib.Path(tmp2)

            with _ReplayHarness(self.tmp_path) as h1:
                h1.replay([{
                    "hook_event_name": "SessionStart",
                    "session_id": "isolation-a",
                    "source": "startup",
                    "cwd": str(self.tmp_path),
                }])
                run_a = h1.find_run_id()

            with _ReplayHarness(tmp2_path) as h2:
                h2.replay([{
                    "hook_event_name": "SessionStart",
                    "session_id": "isolation-b",
                    "source": "startup",
                    "cwd": str(tmp2_path),
                }])
                run_b = h2.find_run_id()

        self.assertIsNotNone(run_a, "First workspace must have a run_id")
        self.assertIsNotNone(run_b, "Second workspace must have a run_id")
        log_root_1 = core.build_paths(self.tmp_path).root
        log_root_2 = core.build_paths(tmp2_path).root
        self.assertNotEqual(log_root_1, log_root_2,
                            "Two harness instances must use different log roots")

    def test_harness_does_not_require_environment_variable(self):
        """vscode-ghcp resolves workspace from cwd in the payload, not from an env var.

        Confirm that replaying works even when CLAUDE_PROJECT_DIR is absent,
        because this adapter must not depend on that env var (D2).
        """
        original = os.environ.pop("CLAUDE_PROJECT_DIR", None)
        try:
            payload = {
                "hook_event_name": "SessionStart",
                "session_id": "harness-noenv-001",
                "source": "startup",
                "cwd": str(self.tmp_path),
            }
            with _ReplayHarness(self.tmp_path) as h:
                h.replay([payload])
                run_id = h.find_run_id()
            self.assertIsNotNone(run_id,
                                 "Replay must succeed using cwd even without CLAUDE_PROJECT_DIR")
        finally:
            if original is not None:
                os.environ["CLAUDE_PROJECT_DIR"] = original


# ---------------------------------------------------------------------------
# TestFullSessionLayout — directory tree assertions
# ---------------------------------------------------------------------------

class TestFullSessionLayout(unittest.TestCase):
    """A replayed vscode-ghcp full session produces the expected on-disk tree.

    Invocation events (SubagentStart/SubagentStop) are placed under the run_id
    bucket recovered from the pending dispatch queue (the timestamp-format dir).
    The per-invocation directory is named by the sanitized agent_instance_id.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path))
            self.run_id = h.find_run_id()
            self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_run_id_is_not_none(self):
        self.assertIsNotNone(self.run_id,
                             "A run directory must be created after the full session")

    def test_run_id_is_timestamp_format(self):
        """The run_id recovered from the pending dispatch must be in MOSAIC format."""
        _RUN_ID_RE = re.compile(r"^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")
        self.assertRegex(self.run_id, _RUN_ID_RE,
                         f"run_id {self.run_id!r} is not in MOSAIC timestamp format")

    def test_run_root_exists(self):
        self.assertTrue(self.paths.run_root(self.run_id).exists())

    def test_orchestrator_events_file_exists(self):
        """At least one orchestrator events file must be written across any run bucket."""
        log_root = self.paths.root
        self.assertTrue(log_root.exists())
        any_orch_file = any(
            (run_dir / "00_orchestrator_events.jsonl").exists()
            for run_dir in log_root.iterdir()
            if run_dir.is_dir()
        )
        self.assertTrue(any_orch_file,
                        "At least one 00_orchestrator_events.jsonl must exist")

    def test_orchestrator_events_file_is_non_empty(self):
        log_root = self.paths.root
        for run_dir in log_root.iterdir():
            if not run_dir.is_dir():
                continue
            orch_file = run_dir / "00_orchestrator_events.jsonl"
            if orch_file.exists():
                self.assertGreater(orch_file.stat().st_size, 0,
                                   f"{orch_file} is empty")
                return  # Found and verified at least one non-empty file
        self.fail("No non-empty orchestrator events file found")

    def test_invocation_directory_exists_named_by_agent_instance_id(self):
        """The per-invocation directory must be named by the extracted agent_instance_id.

        For this session, agent_instance_id = 'Research#1'. Since '#' is not in
        RESERVED_FILENAME_CHARS, sanitize_component leaves the name unchanged.
        The invocation is placed under the run_id bucket (recovered from pending dispatch).
        """
        inv_dir = self.paths.invocation_dir(self.run_id, _INSTANCE_ID)
        self.assertTrue(
            inv_dir.exists(),
            f"Invocation directory {inv_dir} must exist for agent_instance_id {_INSTANCE_ID!r}"
        )

    def test_invocation_events_file_exists(self):
        self.assertTrue(
            self.paths.invocation_events(self.run_id, _INSTANCE_ID).exists()
        )

    def test_invocation_events_file_is_non_empty(self):
        events_file = self.paths.invocation_events(self.run_id, _INSTANCE_ID)
        self.assertGreater(events_file.stat().st_size, 0)

    def test_invocation_input_artifact_exists(self):
        self.assertTrue(
            self.paths.invocation_input(self.run_id, _INSTANCE_ID).exists(),
            "01_input.md must be written by handle_subagent_start"
        )

    def test_invocation_output_artifact_exists(self):
        self.assertTrue(
            self.paths.invocation_output(self.run_id, _INSTANCE_ID).exists(),
            "02_output.md must be written by handle_subagent_stop"
        )

    def test_agent_map_entry_exists_for_agent_name(self):
        """The agent-map entry must exist keyed by agent_name ('Research'), not agent_id."""
        agent_map_entry = self.paths.agent_map_entry(self.run_id, _AGENT_NAME)
        self.assertTrue(
            agent_map_entry.exists(),
            f"Agent-map entry keyed on agent_name {_AGENT_NAME!r} must exist"
        )

    def test_agent_map_entry_is_valid_json(self):
        entry_path = self.paths.agent_map_entry(self.run_id, _AGENT_NAME)
        data = json.loads(entry_path.read_text("utf-8"))
        self.assertIsInstance(data, dict)
        self.assertIn("agent_instance_id", data)
        self.assertEqual(_INSTANCE_ID, data["agent_instance_id"])


# ---------------------------------------------------------------------------
# TestEventStreamScoping — events in correct streams
# ---------------------------------------------------------------------------

class TestEventStreamScoping(unittest.TestCase):
    """Events appear in exactly the correct stream; no cross-stream leakage.

    Lifecycle events (run_start, session_start, session_end, run_end) and tool
    events (tool_call_start, tool_call_end for calls with no agent_id) appear in
    orchestrator streams. The exact run bucket depends on run_id extraction context;
    tests use all_orchestrator_events() for presence checks to cover both buckets.

    Invocation events (invocation_start, invocation_end) appear only in the
    invocation stream for Research#1 under the extracted run_id bucket.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path))
            self.run_id = h.find_run_id()
            self.paths = core.build_paths(self.tmp_path)
            # Aggregated across all run buckets (unknown-run + timestamp-format dirs).
            self.all_orch_events = h.all_orchestrator_events()
            self.inv_events = h.invocation_events(self.run_id, _INSTANCE_ID)

    def tearDown(self):
        self.tmp.cleanup()

    # --- Orchestrator stream (aggregated across all run buckets) ---

    def test_orchestrator_stream_contains_run_start(self):
        """run_start lands in the unknown-run orchestrator stream (SessionStart fires
        before any run_id is known). Present when aggregating across all buckets."""
        types = [e["event"] for e in self.all_orch_events]
        self.assertIn("run_start", types)

    def test_orchestrator_stream_contains_session_start(self):
        types = [e["event"] for e in self.all_orch_events]
        self.assertIn("session_start", types)

    def test_orchestrator_stream_contains_user_turn(self):
        """UserPromptSubmit extracts run_id from the prompt; user turn lands in run_id bucket."""
        user_turns = [
            e for e in self.all_orch_events
            if e["event"] == "turn" and e.get("role") == "user"
        ]
        self.assertGreater(len(user_turns), 0,
                           "Orchestrator user turn must be emitted from UserPromptSubmit")

    def test_orchestrator_stream_contains_tool_call_start(self):
        """Ordinary PreToolUse (no agent_id) routes to the orchestrator stream."""
        types = [e["event"] for e in self.all_orch_events]
        self.assertIn("tool_call_start", types)

    def test_orchestrator_stream_contains_tool_call_end(self):
        """Ordinary PostToolUse (no agent_id) routes to the orchestrator stream."""
        types = [e["event"] for e in self.all_orch_events]
        self.assertIn("tool_call_end", types)

    def test_orchestrator_stream_contains_session_end(self):
        """session_end is emitted on Stop (D5 — no SessionEnd event for VS Code)."""
        types = [e["event"] for e in self.all_orch_events]
        self.assertIn("session_end", types,
                      "session_end must appear after Stop fires (D5)")

    def test_orchestrator_stream_contains_run_end(self):
        """run_end is emitted on Stop (D5)."""
        types = [e["event"] for e in self.all_orch_events]
        self.assertIn("run_end", types,
                      "run_end must appear after Stop fires (D5)")

    # --- Invocation stream ---

    def test_invocation_stream_contains_invocation_start(self):
        types = [e["event"] for e in self.inv_events]
        self.assertIn("invocation_start", types)

    def test_invocation_stream_contains_invocation_end(self):
        types = [e["event"] for e in self.inv_events]
        self.assertIn("invocation_end", types)

    # --- No cross-stream leakage ---

    def test_no_invocation_event_in_any_orchestrator_stream(self):
        """invocation_start and invocation_end must never appear in any orch stream."""
        invocation_types = {"invocation_start", "invocation_end"}
        orch_types = {e["event"] for e in self.all_orch_events}
        leaked = orch_types & invocation_types
        self.assertEqual(set(), leaked,
                         f"Invocation events leaked into orchestrator stream: {leaked}")

    def test_no_lifecycle_event_in_invocation_stream(self):
        """Lifecycle events belong only in orchestrator streams, not invocation streams."""
        lifecycle = {"run_start", "run_end", "session_start", "session_end"}
        inv_types = {e["event"] for e in self.inv_events}
        leaked = inv_types & lifecycle
        self.assertEqual(set(), leaked,
                         f"Lifecycle events leaked into invocation stream: {leaked}")


# ---------------------------------------------------------------------------
# TestSchemaConformance — every emitted line
# ---------------------------------------------------------------------------

class TestSchemaConformance(unittest.TestCase):
    """Every emitted JSONL line across all run buckets carries a complete envelope,
    a catalog event type, harness stamp 'vscode-ghcp', no null values, and valid
    ISO 8601 UTC timestamps."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path))
            # Collect all JSONL files across all run buckets.
            self.all_files = h.all_jsonl_files()
            self.all_events: list = []
            for f in self.all_files:
                self.all_events.extend(h.read_jsonl(f))

    def tearDown(self):
        self.tmp.cleanup()

    def test_at_least_one_jsonl_file_exists(self):
        self.assertGreater(len(self.all_files), 0, "No JSONL files produced")

    def test_at_least_one_event_emitted(self):
        self.assertGreater(len(self.all_events), 0, "No events emitted")

    def test_every_line_is_a_json_object(self):
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
                          f"Missing schema_version in event: {ev.get('event')!r}")

    def test_schema_version_is_1_0_0(self):
        for ev in self.all_events:
            self.assertEqual(
                "1.0.0", ev.get("schema_version"),
                f"Unexpected schema_version in event {ev.get('event')!r}"
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
                          f"Missing timestamp in event: {ev.get('event')!r}")

    def test_every_event_timestamp_is_iso8601_utc_ms(self):
        _TS_PATTERN = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")
        for ev in self.all_events:
            ts = ev.get("timestamp", "")
            self.assertRegex(
                ts, _TS_PATTERN,
                f"Timestamp {ts!r} is not ISO 8601 UTC ms-precision on event {ev.get('event')!r}"
            )

    def test_every_event_has_harness_field(self):
        for ev in self.all_events:
            self.assertIn("harness", ev,
                          f"Missing harness in event: {ev.get('event')!r}")

    def test_every_event_harness_is_vscode_ghcp(self):
        """Every emitted line must stamp harness as 'vscode-ghcp' (D1)."""
        for ev in self.all_events:
            self.assertEqual(
                "vscode-ghcp", ev.get("harness"),
                f"Unexpected harness value {ev.get('harness')!r} in event {ev.get('event')!r}"
            )

    def test_no_null_value_anywhere_in_any_event(self):
        """No emitted field may carry a null value — degradation means omission (AC4.4)."""
        for ev in self.all_events:
            self.assertFalse(
                _contains_null(ev),
                f"Null value found in event {ev.get('event')!r}: {ev}"
            )

    def test_no_empty_string_for_required_envelope_fields(self):
        for ev in self.all_events:
            for field in _REQUIRED_ENVELOPE:
                if field in ev:
                    self.assertNotEqual(
                        "", ev[field],
                        f"Empty string for required field {field!r} in event {ev.get('event')!r}"
                    )


# ---------------------------------------------------------------------------
# TestInvocationPairing — matched invocation_start/invocation_end (AC4.3)
# ---------------------------------------------------------------------------

class TestInvocationPairing(unittest.TestCase):
    """The invocation stream contains a matched invocation_start/invocation_end pair
    for the same agent_instance_id, produced despite SubagentStart carrying no agent_id.

    This directly verifies AC4.3: correlation flows through the pending-dispatch
    queue (keyed on session_id+run_id) and the agent-map (keyed on agent_name).
    The run_id is recovered from the pending dispatch entry written by PreToolUse,
    not from any field at SubagentStart itself.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path))
            self.run_id = h.find_run_id()
            self.inv_events = h.invocation_events(self.run_id, _INSTANCE_ID)

    def tearDown(self):
        self.tmp.cleanup()

    def test_invocation_start_is_present(self):
        types = [e["event"] for e in self.inv_events]
        self.assertIn("invocation_start", types,
                      "invocation_start must be emitted by handle_subagent_start")

    def test_invocation_end_is_present(self):
        types = [e["event"] for e in self.inv_events]
        self.assertIn("invocation_end", types,
                      "invocation_end must be emitted by handle_subagent_stop")

    def test_invocation_start_and_end_share_agent_instance_id(self):
        """Both events must carry the same agent_instance_id extracted from PreToolUse."""
        start_ev = next(
            (e for e in self.inv_events if e["event"] == "invocation_start"), None
        )
        end_ev = next(
            (e for e in self.inv_events if e["event"] == "invocation_end"), None
        )
        self.assertIsNotNone(start_ev, "invocation_start not found")
        self.assertIsNotNone(end_ev, "invocation_end not found")
        start_id = start_ev.get("agent_instance_id")
        end_id = end_ev.get("agent_instance_id")
        self.assertIsNotNone(start_id, "invocation_start missing agent_instance_id")
        self.assertIsNotNone(end_id, "invocation_end missing agent_instance_id")
        self.assertEqual(
            start_id, end_id,
            f"agent_instance_id mismatch: invocation_start={start_id!r}, "
            f"invocation_end={end_id!r}"
        )

    def test_agent_instance_id_matches_extracted_value(self):
        """agent_instance_id in invocation events must match 'Research#1', extracted
        from PreToolUse's tool_input.prompt."""
        start_ev = next(
            (e for e in self.inv_events if e["event"] == "invocation_start"), None
        )
        self.assertIsNotNone(start_ev)
        self.assertEqual(
            _INSTANCE_ID, start_ev.get("agent_instance_id"),
            f"Expected agent_instance_id {_INSTANCE_ID!r}"
        )

    def test_invocation_start_precedes_invocation_end(self):
        """invocation_start must appear before invocation_end in the event stream."""
        types = [e["event"] for e in self.inv_events]
        start_idx = types.index("invocation_start")
        end_idx = types.index("invocation_end")
        self.assertLess(start_idx, end_idx,
                        "invocation_end must not precede invocation_start")

    def test_invocation_end_carries_status_code_success(self):
        """invocation_end must carry the status_code extracted from last_assistant_message."""
        end_ev = next(
            (e for e in self.inv_events if e["event"] == "invocation_end"), None
        )
        self.assertIsNotNone(end_ev)
        self.assertEqual(
            "SUCCESS", end_ev.get("status_code"),
            "status_code must be extracted from SubagentStop's last_assistant_message"
        )

    def test_subagent_start_had_no_agent_id_in_payload(self):
        """Confirm the test exercises D3: the SubagentStart payload has no agent_id.

        The invocation must be correlated purely through agent_name via the
        pending dispatch and agent-map mechanisms.
        """
        subagent_start_payloads = [
            p for p in _full_session_payloads(self.tmp_path)
            if p.get("hook_event_name") == "SubagentStart"
        ]
        self.assertEqual(1, len(subagent_start_payloads),
                         "Expected exactly one SubagentStart in the session")
        self.assertNotIn(
            "agent_id", subagent_start_payloads[0],
            "SubagentStart payload must not carry agent_id (D3 divergence)"
        )
        self.assertIn(
            "agent_name", subagent_start_payloads[0],
            "SubagentStart payload must carry agent_name as the correlation key"
        )


# ---------------------------------------------------------------------------
# TestVsCodeDivergences — vscode-ghcp-specific behavior assertions
# ---------------------------------------------------------------------------

class TestVsCodeDivergences(unittest.TestCase):
    """Assertions for vscode-ghcp-specific behaviors that differ from claude-code."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path))
            self.run_id = h.find_run_id()
            self.paths = core.build_paths(self.tmp_path)
            self.all_orch_events = h.all_orchestrator_events()
            self.inv_events = h.invocation_events(self.run_id, _INSTANCE_ID)

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_end_emitted_on_stop_not_session_end_event(self):
        """D5: VS Code has no SessionEnd event; session_end must appear after Stop fires."""
        orch_types = [e["event"] for e in self.all_orch_events]
        self.assertIn("session_end", orch_types,
                      "session_end must be emitted when Stop fires (D5)")

    def test_run_end_emitted_on_stop(self):
        """D5: run_end is emitted on Stop."""
        orch_types = [e["event"] for e in self.all_orch_events]
        self.assertIn("run_end", orch_types)

    def test_session_end_has_no_reason_field(self):
        """D5: stop_reason='end_turn' is not a schema-valid reason enum value;
        session_end.reason must be absent."""
        session_end = next(
            (e for e in self.all_orch_events if e["event"] == "session_end"), None
        )
        self.assertIsNotNone(session_end, "session_end event not found in any orch stream")
        self.assertNotIn(
            "reason", session_end,
            "session_end.reason must be absent; 'end_turn' is not a valid enum value (D5)"
        )

    def test_run_end_has_no_outcome_field(self):
        """D5: run_end.outcome is always omitted for vscode-ghcp."""
        run_end = next(
            (e for e in self.all_orch_events if e["event"] == "run_end"), None
        )
        self.assertIsNotNone(run_end, "run_end event not found in any orch stream")
        self.assertNotIn(
            "outcome", run_end,
            "run_end.outcome must always be absent for vscode-ghcp (D5)"
        )

    def test_tool_call_end_output_from_nested_tool_result_field(self):
        """D4: tool_call_end.tool_output must be read from tool_result.text_result_for_llm."""
        tool_end = next(
            (e for e in self.all_orch_events
             if e["event"] == "tool_call_end" and e.get("call_id") == _TOOL_CALL_ID),
            None,
        )
        self.assertIsNotNone(
            tool_end,
            f"tool_call_end for call_id {_TOOL_CALL_ID!r} not found in any orch stream"
        )
        self.assertEqual(
            "Research data loaded successfully.", tool_end.get("tool_output"),
            "tool_output must be populated from tool_result.text_result_for_llm (D4)"
        )

    def test_user_prompt_field_is_prompt_not_user_prompt(self):
        """D5: VS Code uses 'prompt' for UserPromptSubmit, not 'user_prompt'.

        A user turn must be emitted, confirming 'prompt' was read correctly.
        """
        user_turns = [
            e for e in self.all_orch_events
            if e["event"] == "turn" and e.get("role") == "user"
        ]
        self.assertGreater(
            len(user_turns), 0,
            "No user turn emitted — 'prompt' field may not have been read correctly"
        )
        content = user_turns[0].get("content", "")
        self.assertIn(
            _RUN_ID, content,
            "User turn content must include the run_id from the 'prompt' payload field"
        )

    def test_ordinary_tool_calls_route_to_orchestrator_stream(self):
        """When a tool event carries no agent_id, it lands in an orchestrator stream.

        VS Code's documented tool payloads do not carry agent_id, so all currently
        observed tool calls land in the orchestrator stream.
        """
        tool_starts = [
            e for e in self.all_orch_events
            if e["event"] == "tool_call_start" and e.get("call_id") == _TOOL_CALL_ID
        ]
        self.assertGreater(
            len(tool_starts), 0,
            "Ordinary tool call must land in an orchestrator stream (no agent_id)"
        )

    def test_cwd_based_workspace_root_used(self):
        """D2: workspace root must be resolved from payload['cwd'], not from any env var."""
        expected_log_root = self.tmp_path / core.LOGS_DIRNAME
        self.assertTrue(
            expected_log_root.exists(),
            f"Expected log root at {expected_log_root} — workspace must come from 'cwd'"
        )

    def test_every_emitted_line_stamps_harness_vscode_ghcp(self):
        """AC4.4: harness field must be 'vscode-ghcp' on every line in every file."""
        with _ReplayHarness(self.tmp_path) as h:
            h.replay(_full_session_payloads(self.tmp_path))
            all_files = h.all_jsonl_files()
        for f in all_files:
            for ln in f.read_text("utf-8").splitlines():
                if not ln.strip():
                    continue
                obj = json.loads(ln)
                self.assertEqual(
                    "vscode-ghcp", obj.get("harness"),
                    f"Harness must be 'vscode-ghcp' in {f.name}: {ln[:80]}"
                )


if __name__ == "__main__":
    unittest.main()
