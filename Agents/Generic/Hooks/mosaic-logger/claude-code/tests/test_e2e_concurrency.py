"""Concurrency tests for the mosaic-logger adapter.

Launches multiple dispatcher processes in parallel — simulating the real harness
scenario where several subagents dispatch tool events simultaneously — and
asserts that no JSONL file contains a torn or interleaved line and no
atomically-replaced file is half-written.

Per-invocation JSONL files are each written by exactly one in-flight invocation
(by design), so the concurrency risk is concentrated in:
  - 00_orchestrator_session.raw: written via atomic replace by every SubagentStart
    and SubagentStop process; many may fire near-simultaneously.
  - Per-invocation 03_events.jsonl files: each has one writer, but several files
    may be written by concurrent processes.

The tests assert STRUCTURAL invariants only (every line is valid JSON, every
atomically-replaced file is valid JSON/bytes) rather than any specific
interleaving order, which keeps them deterministic and non-flaky under CI.
"""

import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import textwrap
import threading
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core

_ADAPTER_ROOT = pathlib.Path(__file__).parent.parent
_ADAPTER_PATH = str(_ADAPTER_ROOT / "mosaic_logger.py")

_FIXTURE_DIR = pathlib.Path(__file__).parent / "fixtures"
_SAMPLE_TRANSCRIPT = _FIXTURE_DIR / "sample_transcript.jsonl"

# Number of concurrent "invocations" to spawn in parallel tests.
_CONCURRENCY = 6


# ---------------------------------------------------------------------------
# Helper: run one adapter payload as a subprocess in a thread
# ---------------------------------------------------------------------------

def _run_payload_in_thread(payload: dict,
                            env: dict,
                            results: list,
                            idx: int) -> None:
    """Run one adapter subprocess and store its CompletedProcess in results[idx]."""
    try:
        r = subprocess.run(
            ["py", _ADAPTER_PATH],
            input=json.dumps(payload).encode(),
            capture_output=True,
            env=env,
            timeout=20,
        )
        results[idx] = r
    except Exception as exc:
        results[idx] = exc


def _run_concurrently(payloads: "list[dict]",
                      env: dict) -> "list[subprocess.CompletedProcess]":
    """Launch one subprocess per payload in parallel threads; wait for all.
    Returns a list of CompletedProcess objects in payload order."""
    n = len(payloads)
    results: list = [None] * n
    threads = [
        threading.Thread(
            target=_run_payload_in_thread,
            args=(payloads[i], env, results, i),
            daemon=True,
        )
        for i in range(n)
    ]
    for t in threads:
        t.start()
    for t in threads:
        t.join(timeout=30)
    return results


# ---------------------------------------------------------------------------
# Helper: JSONL integrity checks
# ---------------------------------------------------------------------------

def _assert_jsonl_integrity(test_case: unittest.TestCase,
                              path: pathlib.Path,
                              label: str) -> "list[dict]":
    """Assert every non-empty line in path is exactly one valid JSON object.
    Returns the parsed list so callers can make further assertions."""
    test_case.assertTrue(path.exists(), f"{label}: file does not exist: {path}")
    lines = path.read_text("utf-8").splitlines()
    events = []
    for i, ln in enumerate(lines):
        if not ln.strip():
            continue
        try:
            obj = json.loads(ln)
        except json.JSONDecodeError as exc:
            test_case.fail(
                f"{label}: line {i + 1} in {path.name} is not valid JSON: "
                f"{exc} | raw: {ln[:120]!r}"
            )
        test_case.assertIsInstance(
            obj, dict,
            f"{label}: line {i + 1} in {path.name} parsed to {type(obj).__name__}, not dict"
        )
        events.append(obj)
    return events


# ---------------------------------------------------------------------------
# T6.9 — Concurrent dispatcher processes produce no torn or interleaved files
# ---------------------------------------------------------------------------

class TestConcurrentDispatchers(unittest.TestCase):
    """Structural integrity of JSONL files under parallel dispatcher processes."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.env = {**os.environ, "CLAUDE_PROJECT_DIR": str(self.tmp_path)}
        self.session_id = "concurrency-session-001"
        # Establish the session and mint the run_id synchronously before
        # launching any concurrent processes.
        self._run_serial({
            "hook_event_name": "SessionStart",
            "session_id": self.session_id,
            "source": "startup",
            "model": "claude-opus-4-5",
        })
        self.run_id = self._find_run_id(self.session_id)

    def tearDown(self):
        self.tmp.cleanup()

    def _run_serial(self, payload: dict) -> subprocess.CompletedProcess:
        return subprocess.run(
            ["py", _ADAPTER_PATH],
            input=json.dumps(payload).encode(),
            capture_output=True,
            env=self.env,
            timeout=20,
        )

    def _find_run_id(self, session_id: str = "") -> "str | None":
        """Return the effective run_id by scanning OrchestrationLogs/.

        The session_id parameter is accepted for API compatibility but not used.
        Scans for a timestamp-format run_id folder; falls back to 'unknown-run'.
        """
        _RUN_ID_RE = re.compile(r'^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$')
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

    # --- T6.9a: concurrent SubagentStart processes (distinct agent_ids) ---

    def test_concurrent_subagent_starts_all_exit_0(self):
        """All parallel SubagentStart processes must exit 0."""
        payloads = [
            {
                "hook_event_name": "SubagentStart",
                "session_id": self.session_id,
                "agent_id": f"agt-concurrent-{i:02d}",
                "agent_type": "Research",
                "agent_prompt": (
                    f'{{"agent_instance_id": "Research#{i + 1}",'
                    f' "task": "concurrent task {i}"}}'
                ),
                "transcript_path": str(_SAMPLE_TRANSCRIPT),
            }
            for i in range(_CONCURRENCY)
        ]
        results = _run_concurrently(payloads, self.env)
        for i, r in enumerate(results):
            self.assertIsInstance(
                r, subprocess.CompletedProcess,
                f"Process {i} raised instead of completing: {r}"
            )
            self.assertEqual(0, r.returncode,
                             f"Process {i} exited {r.returncode}")

    def test_concurrent_subagent_starts_produce_no_stdout(self):
        """No parallel process may write anything to stdout."""
        payloads = [
            {
                "hook_event_name": "SubagentStart",
                "session_id": self.session_id,
                "agent_id": f"agt-nostdout-{i:02d}",
                "agent_type": "Research",
                "agent_prompt": (
                    f'{{"agent_instance_id": "Research#{i + 10}"}}'
                ),
            }
            for i in range(_CONCURRENCY)
        ]
        results = _run_concurrently(payloads, self.env)
        for i, r in enumerate(results):
            if isinstance(r, subprocess.CompletedProcess):
                self.assertEqual(b"", r.stdout,
                                 f"Process {i} wrote to stdout: {r.stdout[:80]!r}")

    def test_concurrent_subagent_starts_each_create_mapping_file(self):
        """Each SubagentStart writes its own .agent-map entry without collision."""
        agent_ids = [f"agt-mapping-{i:02d}" for i in range(_CONCURRENCY)]
        instance_ids = [f"Research#{i + 20}" for i in range(_CONCURRENCY)]
        payloads = [
            {
                "hook_event_name": "SubagentStart",
                "session_id": self.session_id,
                "agent_id": agent_ids[i],
                "agent_type": "Research",
                "agent_prompt": f'{{"agent_instance_id": "{instance_ids[i]}"}}',
            }
            for i in range(_CONCURRENCY)
        ]
        _run_concurrently(payloads, self.env)
        paths = core.build_paths(self.tmp_path)
        for agent_id in agent_ids:
            entry_path = paths.agent_map_entry(self.run_id, agent_id)
            self.assertTrue(
                entry_path.exists(),
                f"Mapping file missing for agent_id {agent_id!r}"
            )
            # Each mapping file must be valid JSON.
            try:
                data = json.loads(entry_path.read_text("utf-8"))
            except json.JSONDecodeError:
                self.fail(f"Mapping file for {agent_id!r} is not valid JSON (torn write)")
            self.assertIn("agent_instance_id", data)
            self.assertIn("agent_id", data)

    # --- T6.9b: concurrent PreToolUse processes for different invocations ---

    def _setup_invocations(self, n: int) -> "list[tuple[str,str]]":
        """Register n subagents sequentially (SubagentStart is synchronous in the
        real harness). Returns list of (agent_id, instance_id) tuples."""
        invocations = []
        for i in range(n):
            agent_id = f"agt-tool-conc-{i:02d}"
            instance_id = f"TestWriter#{i + 100}"
            self._run_serial({
                "hook_event_name": "SubagentStart",
                "session_id": self.session_id,
                "agent_id": agent_id,
                "agent_type": "TestWriter",
                "agent_prompt": f'{{"agent_instance_id": "{instance_id}"}}',
            })
            invocations.append((agent_id, instance_id))
        return invocations

    def test_concurrent_tool_events_all_exit_0(self):
        """Concurrent PreToolUse + PostToolUse pairs for different invocations all
        exit 0."""
        invocations = self._setup_invocations(_CONCURRENCY)
        payloads = []
        for i, (agent_id, _) in enumerate(invocations):
            call_id = f"call-conc-{i:03d}"
            payloads.append({
                "hook_event_name": "PreToolUse",
                "session_id": self.session_id,
                "agent_id": agent_id,
                "tool_name": "Read",
                "tool_use_id": call_id,
                "tool_input": {"file_path": f"/path/to/file_{i}.txt"},
            })
        results = _run_concurrently(payloads, self.env)
        for i, r in enumerate(results):
            self.assertIsInstance(r, subprocess.CompletedProcess,
                                  f"PreToolUse process {i} raised: {r}")
            self.assertEqual(0, r.returncode)

    def test_concurrent_tool_events_produce_valid_jsonl_per_file(self):
        """After concurrent PreToolUse dispatches, every invocation's 03_events.jsonl
        contains only valid JSON objects — no torn or partial lines."""
        invocations = self._setup_invocations(_CONCURRENCY)
        call_ids = [f"call-valid-{i:03d}" for i in range(_CONCURRENCY)]
        pre_payloads = [
            {
                "hook_event_name": "PreToolUse",
                "session_id": self.session_id,
                "agent_id": invocations[i][0],
                "tool_name": "Read",
                "tool_use_id": call_ids[i],
                "tool_input": {"file_path": f"/file_{i}.txt"},
            }
            for i in range(_CONCURRENCY)
        ]
        _run_concurrently(pre_payloads, self.env)
        paths = core.build_paths(self.tmp_path)
        for agent_id, instance_id in invocations:
            events_path = paths.invocation_events(self.run_id, instance_id)
            _assert_jsonl_integrity(
                self, events_path, f"invocation {instance_id!r} 03_events.jsonl"
            )

    def test_concurrent_tool_events_each_land_in_own_stream(self):
        """A PreToolUse event for agent A must appear only in A's 03_events.jsonl,
        not in B's or in the orchestrator stream."""
        invocations = self._setup_invocations(3)  # 3 is enough to demonstrate isolation
        call_ids = [f"call-isolate-{i:03d}" for i in range(3)]
        pre_payloads = [
            {
                "hook_event_name": "PreToolUse",
                "session_id": self.session_id,
                "agent_id": invocations[i][0],
                "tool_name": "Bash",
                "tool_use_id": call_ids[i],
                "tool_input": {"command": f"echo {i}"},
            }
            for i in range(3)
        ]
        _run_concurrently(pre_payloads, self.env)
        paths = core.build_paths(self.tmp_path)
        for i, (agent_id, instance_id) in enumerate(invocations):
            own_path = paths.invocation_events(self.run_id, instance_id)
            own_events = _assert_jsonl_integrity(
                self, own_path, f"invocation {instance_id!r}"
            )
            own_call_ids = {
                e.get("call_id") for e in own_events
                if e.get("event") == "tool_call_start"
            }
            # This invocation's call_id must appear here.
            self.assertIn(call_ids[i], own_call_ids,
                          f"call_id {call_ids[i]!r} missing from {instance_id!r}")
            # The other agents' call_ids must NOT appear here.
            for j in range(3):
                if j != i:
                    self.assertNotIn(
                        call_ids[j], own_call_ids,
                        f"Agent {invocations[j][1]!r}'s call_id leaked into "
                        f"{instance_id!r}'s stream"
                    )

    # --- T6.9c: orchestrator stream structural integrity after concurrent writes ---

    def test_orchestrator_events_jsonl_is_valid_after_concurrent_subagent_starts(self):
        """After concurrent SubagentStart processes each refresh the orchestrator
        stream indirectly (via transcript export, not by appending to the
        orchestrator events JSONL), the orchestrator JSONL must still be valid."""
        # Run concurrent SubagentStarts, each of which refreshes
        # 00_orchestrator_session.raw via atomic replace.
        payloads = [
            {
                "hook_event_name": "SubagentStart",
                "session_id": self.session_id,
                "agent_id": f"agt-orch-conc-{i:02d}",
                "agent_type": "Research",
                "agent_prompt": f'{{"agent_instance_id": "Research#{i + 50}"}}',
                "transcript_path": str(_SAMPLE_TRANSCRIPT),
            }
            for i in range(_CONCURRENCY)
        ]
        _run_concurrently(payloads, self.env)
        paths = core.build_paths(self.tmp_path)
        orch_events_path = paths.orchestrator_events(self.run_id)
        _assert_jsonl_integrity(
            self, orch_events_path, "orchestrator 00_orchestrator_events.jsonl"
        )

    def test_orchestrator_session_raw_is_valid_bytes_after_concurrent_refreshes(self):
        """00_orchestrator_session.raw is written via atomic replace, so it must
        be either wholly absent (first write not yet done) or wholly present with
        valid byte content — never a zero-length torn file."""
        payloads = [
            {
                "hook_event_name": "SubagentStart",
                "session_id": self.session_id,
                "agent_id": f"agt-raw-{i:02d}",
                "agent_type": "Research",
                "agent_prompt": f'{{"agent_instance_id": "Research#{i + 60}"}}',
                "transcript_path": str(_SAMPLE_TRANSCRIPT),
            }
            for i in range(_CONCURRENCY)
        ]
        _run_concurrently(payloads, self.env)
        paths = core.build_paths(self.tmp_path)
        raw_path = paths.orchestrator_raw(self.run_id)
        if raw_path.exists():
            raw_bytes = raw_path.read_bytes()
            # The raw file must have nonzero content (atomic replace means
            # either the full old content or the full new content).
            self.assertGreater(
                len(raw_bytes), 0,
                "00_orchestrator_session.raw is zero bytes — possible torn write"
            )

    def test_all_invocation_event_files_valid_after_concurrent_tool_pairs(self):
        """After N concurrent PreToolUse + PostToolUse pairs (each for a distinct
        invocation), every 03_events.jsonl contains only valid JSON lines with
        no interleaving from other invocations."""
        invocations = self._setup_invocations(_CONCURRENCY)
        call_ids = [f"call-pair-{i:03d}" for i in range(_CONCURRENCY)]
        # Launch PreToolUse for all concurrently.
        pre_payloads = [
            {
                "hook_event_name": "PreToolUse",
                "session_id": self.session_id,
                "agent_id": invocations[i][0],
                "tool_name": "Write",
                "tool_use_id": call_ids[i],
                "tool_input": {"file_path": f"/out/file_{i}.txt",
                               "content": "x" * 1000},
            }
            for i in range(_CONCURRENCY)
        ]
        _run_concurrently(pre_payloads, self.env)
        # Launch PostToolUse for all concurrently.
        post_payloads = [
            {
                "hook_event_name": "PostToolUse",
                "session_id": self.session_id,
                "agent_id": invocations[i][0],
                "tool_name": "Write",
                "tool_use_id": call_ids[i],
                "tool_output": f"Wrote file_{i}.txt successfully.",
            }
            for i in range(_CONCURRENCY)
        ]
        _run_concurrently(post_payloads, self.env)
        paths = core.build_paths(self.tmp_path)
        for agent_id, instance_id in invocations:
            events_path = paths.invocation_events(self.run_id, instance_id)
            events = _assert_jsonl_integrity(
                self, events_path, f"{instance_id!r} 03_events.jsonl"
            )
            # Each file should have at least a tool_call_start.
            event_types = [e.get("event") for e in events]
            self.assertIn(
                "tool_call_start", event_types,
                f"{instance_id!r}: tool_call_start missing after PreToolUse"
            )

    # --- T6.9d: concurrent SubagentStop refreshes of 00_orchestrator_session.raw ---

    def test_concurrent_subagent_stops_all_exit_0(self):
        """Concurrent SubagentStop processes for different invocations all exit 0.
        Each simultaneously attempts to refresh 00_orchestrator_session.raw via
        atomic replace, exercising the contention path."""
        invocations = self._setup_invocations(_CONCURRENCY)
        payloads = [
            {
                "hook_event_name": "SubagentStop",
                "session_id": self.session_id,
                "agent_id": invocations[i][0],
                "agent_type": "TestWriter",
                "agent_transcript_path": str(_SAMPLE_TRANSCRIPT),
                "transcript_path": str(_SAMPLE_TRANSCRIPT),
                "last_assistant_message": f'{{"status_code": "SUCCESS", "n": {i}}}',
            }
            for i in range(_CONCURRENCY)
        ]
        results = _run_concurrently(payloads, self.env)
        for i, r in enumerate(results):
            self.assertIsInstance(r, subprocess.CompletedProcess,
                                  f"SubagentStop process {i} raised: {r}")
            self.assertEqual(0, r.returncode,
                             f"SubagentStop process {i} exited {r.returncode}")

    def test_concurrent_subagent_stops_leave_valid_jsonl(self):
        """After concurrent SubagentStops, every invocation's 03_events.jsonl
        remains valid — no torn lines, no interleaved content."""
        invocations = self._setup_invocations(_CONCURRENCY)
        payloads = [
            {
                "hook_event_name": "SubagentStop",
                "session_id": self.session_id,
                "agent_id": invocations[i][0],
                "agent_type": "TestWriter",
                "agent_transcript_path": str(_SAMPLE_TRANSCRIPT),
                "transcript_path": str(_SAMPLE_TRANSCRIPT),
                "last_assistant_message": '{"status_code": "SUCCESS"}',
            }
            for i in range(_CONCURRENCY)
        ]
        _run_concurrently(payloads, self.env)
        paths = core.build_paths(self.tmp_path)
        for agent_id, instance_id in invocations:
            events_path = paths.invocation_events(self.run_id, instance_id)
            _assert_jsonl_integrity(
                self, events_path, f"{instance_id!r} 03_events.jsonl after SubagentStop"
            )


# ---------------------------------------------------------------------------
# T1.5 — Concurrent session-run binding writes from multiple processes
# ---------------------------------------------------------------------------

class TestConcurrentSessionRunBindingWrites(unittest.TestCase):
    """Multiple concurrent put_session_run_binding calls for the SAME session
    must not lose entries. Uses real subprocesses to exercise the
    platform-appropriate locking path (POSIX O_APPEND / Windows msvcrt lock),
    following the same direct-call subprocess pattern as
    TestConcurrentPutPendingDispatch in test_pending_dispatch.py -- Stage 1
    has no handler wiring yet, so the binding functions are called directly
    rather than through the full adapter dispatcher.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.tmp.cleanup()

    def test_concurrent_writes_preserve_every_appended_entry(self):
        """Every concurrently-appended run_id must be present and intact in
        the resulting timeline -- no lost writes, no torn lines."""
        n_concurrent = 6
        workspace = self.tmp.name
        session_id = "concurrency-session-binding-001"

        helper_script = textwrap.dedent(f"""\
            import sys, pathlib
            sys.path.insert(0, {str(_ADAPTER_ROOT)!r})
            import mosaic_logger_core as core
            import mosaic_logger_runstate as runstate
            idx = sys.argv[1]
            workspace_root = pathlib.Path({workspace!r})
            paths = core.build_paths(workspace_root)
            runstate.put_session_run_binding(
                paths, {session_id!r},
                f"20260101T090000Z-{{idx.zfill(4)}}",
                f"2026-01-01T09:00:0{{idx}}.000Z",
            )
        """)

        results = [None] * n_concurrent
        threads = []

        def run_subprocess(idx):
            try:
                r = subprocess.run(
                    ["py", "-c", helper_script, str(idx)],
                    capture_output=True,
                    timeout=15,
                )
                results[idx] = r
            except Exception as exc:
                results[idx] = exc

        for i in range(n_concurrent):
            t = threading.Thread(target=run_subprocess, args=(i,), daemon=True)
            threads.append(t)
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=20)

        for i, r in enumerate(results):
            if isinstance(r, Exception):
                self.fail(f"Subprocess {i} raised: {r}")
            self.assertEqual(0, r.returncode,
                             f"Subprocess {i} failed: {r.stderr.decode()[:300]}")

        paths = core.build_paths(pathlib.Path(workspace))
        entry_path = paths.session_binding_entry(session_id)
        self.assertTrue(entry_path.exists(),
                        "Binding file must exist after concurrent puts")

        lines = entry_path.read_text("utf-8").splitlines()
        non_empty = [ln for ln in lines if ln.strip()]
        self.assertEqual(n_concurrent, len(non_empty),
                         f"Expected {n_concurrent} entries, got {len(non_empty)}")

        seen_run_ids = set()
        for i, ln in enumerate(non_empty):
            try:
                obj = json.loads(ln)
            except json.JSONDecodeError as exc:
                self.fail(f"Line {i+1} is not valid JSON (torn write?): "
                         f"{exc!r} | raw: {ln[:80]!r}")
            self.assertIn("run_id", obj, f"Line {i+1} missing run_id")
            self.assertIn("start_time", obj, f"Line {i+1} missing start_time")
            seen_run_ids.add(obj["run_id"])

        self.assertEqual(n_concurrent, len(seen_run_ids),
                         "Every concurrently-written run_id must be distinct and present "
                         "-- no entry may be lost to a torn or overwritten write")


if __name__ == "__main__":
    unittest.main()
