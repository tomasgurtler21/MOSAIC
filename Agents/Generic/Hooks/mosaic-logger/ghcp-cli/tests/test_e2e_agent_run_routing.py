"""End-to-end tests for agent-to-run routing via the workspace-level association
store (ghcp-cli variant).

Covers:
- T2.5: The full sequence subagent start → subagent tool call → subagent stop
  results in all three events landing under the same run_id / agent_instance_id
  folder, routed via the workspace-level agent-to-run association.
- T2.6: Agents belonging to two different concurrent runs each resolve to their
  own run's folder, with no cross-contamination between them.

Uses the same _GhcpReplayHarness pattern as test_e2e_full_session.py to drive
mosaic_logger.dispatch() through an isolated temporary workspace.
"""

import contextlib
import io
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
# Replay harness (mirrors test_e2e_full_session.py)
# ---------------------------------------------------------------------------

class _GhcpReplayHarness:
    """Drives mosaic_logger.dispatch() against an isolated temporary workspace.

    The cwd field is injected into every payload so all writes land under
    the temp tree rather than the real filesystem.
    """

    def __init__(self, workspace: pathlib.Path):
        self.workspace = workspace

    def __enter__(self) -> "_GhcpReplayHarness":
        return self

    def __exit__(self, *_) -> None:
        pass

    def dispatch(self, event: str, payload: dict) -> str:
        payload_with_cwd = {"cwd": str(self.workspace), **payload}
        raw = json.dumps(payload_with_cwd, ensure_ascii=False)
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            mosaic_logger.dispatch(event, raw)
        return buf.getvalue()

    def invocation_events(self, run_id: str, instance_id: str) -> list:
        paths = core.build_paths(self.workspace)
        path = paths.invocation_events(run_id, instance_id)
        if not path.exists():
            return []
        return [json.loads(ln) for ln in path.read_text("utf-8").splitlines()
                if ln.strip()]

    def orchestrator_events(self, run_id: str) -> list:
        paths = core.build_paths(self.workspace)
        path = paths.orchestrator_events(run_id)
        if not path.exists():
            return []
        return [json.loads(ln) for ln in path.read_text("utf-8").splitlines()
                if ln.strip()]


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _prompt_with_ids(instance_id: str, run_id: str) -> str:
    return (
        f'agent_instance_id: "{instance_id}"\n'
        f'run_id: "{run_id}"\n'
        f'Task description here.'
    )


# ---------------------------------------------------------------------------
# T2.5 — Full lifecycle: subagent start → tool call → stop all in one folder
# ---------------------------------------------------------------------------

class TestSubagentLifecycleFullRouting(unittest.TestCase):
    """subagentStart → tool call → subagentStop all land under the same run/instance."""

    _SESSION_ID = "e2e-assoc-session-001"
    _RUN_ID = "20260804T150537Z-e2e5"
    _AGENT_ID = "ghcp-agt-e2e5-001"
    _INSTANCE_ID = "TestWriter#5"

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _run_full_lifecycle(self, h: _GhcpReplayHarness) -> None:
        """Drive the full subagent lifecycle through the replay harness."""
        prompt = _prompt_with_ids(self._INSTANCE_ID, self._RUN_ID)
        # Orchestrator session start
        h.dispatch("sessionStart", {"sessionId": self._SESSION_ID})
        # preToolUse(agent) stores the pending dispatch and captures the run_id
        h.dispatch("preToolUse", {
            "sessionId": self._SESSION_ID,
            "toolName": "agent",
            "toolArgs": {
                "prompt": prompt,
                "filePath": (
                    f"/workspace/Orchestration-{self._RUN_ID}/Stage-2/Plan.md"
                ),
            },
            "toolUseId": "tu-lifecycle-001",
        })
        # subagentStart pops dispatch and writes the workspace-level association
        h.dispatch("subagentStart", {
            "sessionId": self._SESSION_ID,
            "agentId": self._AGENT_ID,
            "agentName": "TestWriter",
        })
        # Subagent's first tool call — must route via the association
        h.dispatch("preToolUse", {
            "sessionId": self._SESSION_ID,
            "agentId": self._AGENT_ID,
            "toolName": "bash",
            "toolArgs": {"command": "ls"},
            "toolUseId": "tu-subagent-tool-001",
        })
        # Subagent's tool result
        h.dispatch("postToolUse", {
            "sessionId": self._SESSION_ID,
            "agentId": self._AGENT_ID,
            "toolName": "bash",
            "toolUseId": "tu-subagent-tool-001",
            "toolResult": {"textResultForLlm": "file list here"},
        })
        # subagentStop — must route to the same folder
        h.dispatch("subagentStop", {
            "sessionId": self._SESSION_ID,
            "agentId": self._AGENT_ID,
            "response": f'{{"status_code": "SUCCESS"}}',
        })

    def test_invocation_start_lands_in_correct_folder(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            self._run_full_lifecycle(h)
            events = h.invocation_events(self._RUN_ID, self._INSTANCE_ID)
            event_types = {e["event"] for e in events}
            self.assertIn(
                "invocation_start", event_types,
                f"invocation_start not found in {self._RUN_ID}/{self._INSTANCE_ID}",
            )

    def test_tool_call_start_lands_in_correct_folder(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            self._run_full_lifecycle(h)
            events = h.invocation_events(self._RUN_ID, self._INSTANCE_ID)
            event_types = {e["event"] for e in events}
            self.assertIn(
                "tool_call_start", event_types,
                "tool_call_start from subagent tool call must land in "
                f"{self._RUN_ID}/{self._INSTANCE_ID} via the association",
            )

    def test_tool_call_end_lands_in_correct_folder(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            self._run_full_lifecycle(h)
            events = h.invocation_events(self._RUN_ID, self._INSTANCE_ID)
            event_types = {e["event"] for e in events}
            self.assertIn(
                "tool_call_end", event_types,
                "tool_call_end from subagent tool result must land in "
                f"{self._RUN_ID}/{self._INSTANCE_ID} via the association",
            )

    def test_invocation_end_lands_in_correct_folder(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            self._run_full_lifecycle(h)
            events = h.invocation_events(self._RUN_ID, self._INSTANCE_ID)
            event_types = {e["event"] for e in events}
            self.assertIn(
                "invocation_end", event_types,
                f"invocation_end not found in {self._RUN_ID}/{self._INSTANCE_ID}",
            )

    def test_all_subagent_events_land_in_same_folder(self):
        """All four event types land in one folder, not split across unmapped or unknown-run."""
        with _GhcpReplayHarness(self.tmp_path) as h:
            self._run_full_lifecycle(h)
            events = h.invocation_events(self._RUN_ID, self._INSTANCE_ID)
            event_types = {e["event"] for e in events}
            for expected in ("invocation_start", "tool_call_start",
                             "tool_call_end", "invocation_end"):
                self.assertIn(
                    expected, event_types,
                    f"Expected {expected!r} in {self._RUN_ID}/{self._INSTANCE_ID}, "
                    f"found types: {event_types}",
                )

    def test_no_subagent_events_in_unmapped_folder(self):
        """Tool events must not appear in any unmapped_* directory."""
        with _GhcpReplayHarness(self.tmp_path) as h:
            self._run_full_lifecycle(h)
            logs_root = self.tmp_path / "OrchestrationLogs"
            if not logs_root.exists():
                return
            for run_dir in logs_root.iterdir():
                if not run_dir.is_dir():
                    continue
                for item in run_dir.iterdir():
                    if not item.is_dir():
                        continue
                    self.assertFalse(
                        item.name.startswith("unmapped_"),
                        f"Unexpected unmapped directory: {item}",
                    )

    def test_tool_events_have_correct_run_id_in_envelope(self):
        """Even though the tool event's ctx.run_id may differ, the folder is correct."""
        with _GhcpReplayHarness(self.tmp_path) as h:
            self._run_full_lifecycle(h)
            events = h.invocation_events(self._RUN_ID, self._INSTANCE_ID)
            tool_starts = [e for e in events if e.get("event") == "tool_call_start"]
            self.assertTrue(
                len(tool_starts) >= 1,
                "At least one tool_call_start must be present in the correct folder",
            )

    def test_no_stdout_produced_during_lifecycle(self):
        """The full lifecycle must produce no stdout."""
        with _GhcpReplayHarness(self.tmp_path) as h:
            prompt = _prompt_with_ids(self._INSTANCE_ID, self._RUN_ID)
            events_and_payloads = [
                ("sessionStart", {"sessionId": self._SESSION_ID}),
                ("preToolUse", {
                    "sessionId": self._SESSION_ID,
                    "toolName": "agent",
                    "toolArgs": {"prompt": prompt,
                                 "filePath": f"/workspace/Orchestration-{self._RUN_ID}/Plan.md"},
                    "toolUseId": "tu-stdout-001",
                }),
                ("subagentStart", {
                    "sessionId": self._SESSION_ID,
                    "agentId": self._AGENT_ID,
                    "agentName": "TestWriter",
                }),
                ("preToolUse", {
                    "sessionId": self._SESSION_ID,
                    "agentId": self._AGENT_ID,
                    "toolName": "bash",
                    "toolArgs": {"command": "ls"},
                    "toolUseId": "tu-stdout-002",
                }),
                ("subagentStop", {
                    "sessionId": self._SESSION_ID,
                    "agentId": self._AGENT_ID,
                    "response": "Done.",
                }),
            ]
            for event, payload in events_and_payloads:
                stdout = h.dispatch(event, payload)
                self.assertEqual(
                    "", stdout,
                    f"Event {event!r} produced unexpected stdout: {stdout!r}",
                )


# ---------------------------------------------------------------------------
# T2.6 — Concurrent runs: no cross-contamination
# ---------------------------------------------------------------------------

class TestConcurrentRunsNoContamination(unittest.TestCase):
    """Agents belonging to two different runs each resolve to their own run,
    with no cross-contamination, because associations are keyed by agent_id."""

    _SESSION_ID = "e2e-concurrent-session-001"
    _RUN_ID_A = "20260804T150537Z-aaaa"
    _RUN_ID_B = "20260804T150538Z-aabb"
    _AGENT_ID_A = "ghcp-agt-concurrent-alpha"
    _AGENT_ID_B = "ghcp-agt-concurrent-beta"
    _INSTANCE_ID_A = "ResearchAgent#1"
    _INSTANCE_ID_B = "TestWriter#2"

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _setup_two_runs(self) -> None:
        """Write associations and agent mappings for both runs directly."""
        paths = core.build_paths(self.tmp_path)
        # Run A: agent A -> run A
        runstate.put_agent_run(paths, self._AGENT_ID_A, self._RUN_ID_A)
        runstate.put_agent_mapping(
            paths, self._RUN_ID_A, self._AGENT_ID_A, self._INSTANCE_ID_A, None
        )
        # Run B: agent B -> run B
        runstate.put_agent_run(paths, self._AGENT_ID_B, self._RUN_ID_B)
        runstate.put_agent_mapping(
            paths, self._RUN_ID_B, self._AGENT_ID_B, self._INSTANCE_ID_B, None
        )

    def test_agent_a_resolves_to_run_a(self):
        self._setup_two_runs()
        paths = core.build_paths(self.tmp_path)
        result = runstate.resolve_run_for_agent(paths, self._AGENT_ID_A)
        self.assertEqual(self._RUN_ID_A, result)

    def test_agent_b_resolves_to_run_b(self):
        self._setup_two_runs()
        paths = core.build_paths(self.tmp_path)
        result = runstate.resolve_run_for_agent(paths, self._AGENT_ID_B)
        self.assertEqual(self._RUN_ID_B, result)

    def test_agent_a_does_not_resolve_to_run_b(self):
        self._setup_two_runs()
        paths = core.build_paths(self.tmp_path)
        result = runstate.resolve_run_for_agent(paths, self._AGENT_ID_A)
        self.assertNotEqual(self._RUN_ID_B, result)

    def test_agent_b_does_not_resolve_to_run_a(self):
        self._setup_two_runs()
        paths = core.build_paths(self.tmp_path)
        result = runstate.resolve_run_for_agent(paths, self._AGENT_ID_B)
        self.assertNotEqual(self._RUN_ID_A, result)

    def test_tool_events_from_agent_a_land_under_run_a(self):
        """A tool event carrying agent A's agentId must route to run A's folder."""
        self._setup_two_runs()
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("preToolUse", {
                "sessionId": self._SESSION_ID,
                "agentId": self._AGENT_ID_A,
                "toolName": "bash",
                "toolArgs": {"command": "ls"},
                "toolUseId": "tu-concurrent-a-001",
            })
            events_a = h.invocation_events(self._RUN_ID_A, self._INSTANCE_ID_A)
            event_types_a = {e["event"] for e in events_a}
            self.assertIn(
                "tool_call_start", event_types_a,
                f"tool_call_start from agent A must land under run A ({self._RUN_ID_A})",
            )

    def test_tool_events_from_agent_b_land_under_run_b(self):
        """A tool event carrying agent B's agentId must route to run B's folder."""
        self._setup_two_runs()
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("preToolUse", {
                "sessionId": self._SESSION_ID,
                "agentId": self._AGENT_ID_B,
                "toolName": "bash",
                "toolArgs": {"command": "pwd"},
                "toolUseId": "tu-concurrent-b-001",
            })
            events_b = h.invocation_events(self._RUN_ID_B, self._INSTANCE_ID_B)
            event_types_b = {e["event"] for e in events_b}
            self.assertIn(
                "tool_call_start", event_types_b,
                f"tool_call_start from agent B must land under run B ({self._RUN_ID_B})",
            )

    def test_agent_a_events_do_not_appear_in_run_b_folder(self):
        """Events from agent A must not contaminate run B's invocation folder."""
        self._setup_two_runs()
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("preToolUse", {
                "sessionId": self._SESSION_ID,
                "agentId": self._AGENT_ID_A,
                "toolName": "bash",
                "toolArgs": {"command": "ls"},
                "toolUseId": "tu-contamination-a-001",
            })
            # Run B's folder for instance B must be absent or empty
            events_b = h.invocation_events(self._RUN_ID_B, self._INSTANCE_ID_B)
            self.assertEqual(
                [], events_b,
                "Agent A's tool events must not appear in run B's invocation folder",
            )

    def test_agent_b_events_do_not_appear_in_run_a_folder(self):
        """Events from agent B must not contaminate run A's invocation folder."""
        self._setup_two_runs()
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("preToolUse", {
                "sessionId": self._SESSION_ID,
                "agentId": self._AGENT_ID_B,
                "toolName": "bash",
                "toolArgs": {"command": "pwd"},
                "toolUseId": "tu-contamination-b-001",
            })
            events_a = h.invocation_events(self._RUN_ID_A, self._INSTANCE_ID_A)
            self.assertEqual(
                [], events_a,
                "Agent B's tool events must not appear in run A's invocation folder",
            )

    def test_each_agent_association_file_is_independent(self):
        """Each agent_id occupies its own file; they cannot collide."""
        self._setup_two_runs()
        paths = core.build_paths(self.tmp_path)
        path_a = paths.agent_run_entry(self._AGENT_ID_A)
        path_b = paths.agent_run_entry(self._AGENT_ID_B)
        self.assertNotEqual(path_a, path_b)
        self.assertTrue(path_a.exists())
        self.assertTrue(path_b.exists())

    def test_overwriting_one_agent_does_not_affect_the_other(self):
        """A re-write for agent A must not change agent B's association."""
        self._setup_two_runs()
        paths = core.build_paths(self.tmp_path)
        # Overwrite agent A with a new run_id (simulating a respawn in a new run)
        new_run_id_a = "20260804T160000Z-cafe"
        runstate.put_agent_run(paths, self._AGENT_ID_A, new_run_id_a)
        # Agent B must still resolve to run B
        result_b = runstate.resolve_run_for_agent(paths, self._AGENT_ID_B)
        self.assertEqual(
            self._RUN_ID_B, result_b,
            "Overwriting agent A's association must not affect agent B",
        )

    def test_full_concurrent_dispatch_no_cross_contamination(self):
        """Two concurrent subagent lifecycles in one workspace stay independent."""
        with _GhcpReplayHarness(self.tmp_path) as h:
            prompt_a = _prompt_with_ids(self._INSTANCE_ID_A, self._RUN_ID_A)
            prompt_b = _prompt_with_ids(self._INSTANCE_ID_B, self._RUN_ID_B)

            # Both runs start their orchestrator sessions
            h.dispatch("sessionStart", {"sessionId": f"{self._SESSION_ID}-A"})
            h.dispatch("sessionStart", {"sessionId": f"{self._SESSION_ID}-B"})

            # Both orchestrators dispatch their subagents (interleaved)
            h.dispatch("preToolUse", {
                "sessionId": f"{self._SESSION_ID}-A",
                "toolName": "agent",
                "toolArgs": {
                    "prompt": prompt_a,
                    "filePath": f"/ws/Orchestration-{self._RUN_ID_A}/Plan.md",
                },
                "toolUseId": "tu-conc-a-1",
            })
            h.dispatch("preToolUse", {
                "sessionId": f"{self._SESSION_ID}-B",
                "toolName": "agent",
                "toolArgs": {
                    "prompt": prompt_b,
                    "filePath": f"/ws/Orchestration-{self._RUN_ID_B}/Plan.md",
                },
                "toolUseId": "tu-conc-b-1",
            })

            # Both subagents start (interleaved)
            h.dispatch("subagentStart", {
                "sessionId": f"{self._SESSION_ID}-A",
                "agentId": self._AGENT_ID_A,
                "agentName": "ResearchAgent",
            })
            h.dispatch("subagentStart", {
                "sessionId": f"{self._SESSION_ID}-B",
                "agentId": self._AGENT_ID_B,
                "agentName": "TestWriter",
            })

            # Both subagents issue tool calls (interleaved)
            h.dispatch("preToolUse", {
                "sessionId": f"{self._SESSION_ID}-A",
                "agentId": self._AGENT_ID_A,
                "toolName": "bash",
                "toolArgs": {"command": "ls"},
                "toolUseId": "tu-conc-a-2",
            })
            h.dispatch("preToolUse", {
                "sessionId": f"{self._SESSION_ID}-B",
                "agentId": self._AGENT_ID_B,
                "toolName": "bash",
                "toolArgs": {"command": "pwd"},
                "toolUseId": "tu-conc-b-2",
            })

            # Each should have routed to their own folder
            events_a = h.invocation_events(self._RUN_ID_A, self._INSTANCE_ID_A)
            events_b = h.invocation_events(self._RUN_ID_B, self._INSTANCE_ID_B)

            types_a = {e["event"] for e in events_a}
            types_b = {e["event"] for e in events_b}

            self.assertIn("invocation_start", types_a,
                          "Run A's invocation_start must be in run A's folder")
            self.assertIn("tool_call_start", types_a,
                          "Run A's tool_call_start must be in run A's folder")
            self.assertIn("invocation_start", types_b,
                          "Run B's invocation_start must be in run B's folder")
            self.assertIn("tool_call_start", types_b,
                          "Run B's tool_call_start must be in run B's folder")

            # Cross-contamination check: run B's folder must not contain run A's events
            events_b_wrong = h.invocation_events(self._RUN_ID_B, self._INSTANCE_ID_A)
            self.assertEqual([], events_b_wrong,
                             "Run A's instance must not appear in run B")
            events_a_wrong = h.invocation_events(self._RUN_ID_A, self._INSTANCE_ID_B)
            self.assertEqual([], events_a_wrong,
                             "Run B's instance must not appear in run A")


if __name__ == "__main__":
    unittest.main()
