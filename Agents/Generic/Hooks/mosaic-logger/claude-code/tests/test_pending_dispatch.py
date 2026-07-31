"""Tests for the pending-dispatch correlation mechanism (Stage 1: Agent-ID Attribution).

Covers:
  - LogPaths extensions for pending-dispatch directory/entry paths
  - put_pending_dispatch: persistence, JSONL append (FIFO), parent-dir creation,
    success/failure return, never-raises contract, None run_id serialization
  - pop_pending_dispatch: FIFO ordering, stale-entry eviction, empty-queue
    resilience, corrupt-file resilience, absent-file resilience, concurrent
    session isolation, max_age_seconds parameter
  - Concurrent write safety for put_pending_dispatch (real subprocesses)
  - handle_pre_tool_use Agent branch: pending dispatch created when
    tool_name == "Agent", correct extraction, existing tool_call_start event
    still emitted, non-Agent calls unaffected
  - handle_subagent_start consuming pending dispatch: uses dispatch's
    agent_instance_id for mapping, consumes the entry, falls back to
    "unknown-agent" when no dispatch exists, optionally updates ctx.run_id,
    guard-then-pop ordering (no agent_id guard triggers early return before pop),
    path_run_id bucket-mismatch fallback
  - End-to-end correlation: PreToolUse Agent -> SubagentStart produces correct
    agent_instance_id in the mapping store
  - Unmapped SubagentStop filtering: no files/events written when
    resolve_invocation_id returns "unmapped_*"
  - handle_stop guard removal: Stop with agent_id present still emits turn event

RED-phase note: All tests in this file will FAIL until the Stage 1 implementation
is complete. Functions put_pending_dispatch and pop_pending_dispatch do not exist
yet in mosaic_logger_runstate. The LogPaths pending_dispatch_dir/pending_dispatch_entry
methods do not exist yet. The Agent-branch in handle_pre_tool_use, the
pending-dispatch pop in handle_subagent_start, the unmapped guard in
handle_subagent_stop, and the guard removal in handle_stop are all unimplemented.
"""

import datetime
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import textwrap
import threading
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_tools as tools
import mosaic_logger_handlers_invocation as invocation
import mosaic_logger_handlers_session as session_handlers


# ---------------------------------------------------------------------------
# Shared constants
# ---------------------------------------------------------------------------

_RUN_ID = "20260101T120000Z-d7e3"
_SESSION_ID = "test-session-pd-main"
_SESSION_ID_ALT = "test-session-pd-alt"
_AGENT_INSTANCE_ID = "planner-tdd-soft#9"
_EXTRACTED_RUN_ID = "20260101T110000Z-a1b2"
_TS = "2026-01-01T12:00:00.000Z"

# A realistic dispatch prompt that contains both agent_instance_id and run_id.
_DISPATCH_PROMPT = (
    '{"agent_instance_id": "planner-tdd-soft#9", '
    '"run_id": "20260101T110000Z-a1b2", '
    '"task_description": "Write the plan."}'
)

# A dispatch prompt with agent_instance_id but no run_id.
_DISPATCH_PROMPT_NO_RUN_ID = (
    '{"agent_instance_id": "test-writer-tdd#3", '
    '"task_description": "Write the tests."}'
)

# A prompt that contains no extractable agent_instance_id.
_PLAIN_PROMPT = "Here are the instructions for the task. Please proceed."

_ISO8601_Z_RE = __import__("re").compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$"
)

# Path to the adapter root (parent of this test file's directory).
_ADAPTER_ROOT = pathlib.Path(__file__).parent.parent


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


def _make_pre_tool_ctx(tmp_path, **kwargs):
    return _make_ctx(tmp_path, "PreToolUse", **kwargs)


def _make_start_ctx(tmp_path, **kwargs):
    return _make_ctx(tmp_path, "SubagentStart", **kwargs)


def _make_stop_ctx(tmp_path, **kwargs):
    return _make_ctx(tmp_path, "SubagentStop", **kwargs)


def _make_session_stop_ctx(tmp_path, **kwargs):
    return _make_ctx(tmp_path, "Stop", **kwargs)


def _read_jsonl(path):
    """Read all non-empty lines of a JSONL file as parsed dicts."""
    return [json.loads(ln) for ln in pathlib.Path(path).read_text("utf-8").splitlines()
            if ln.strip()]


def _register_mapping(tmp_path, run_id, agent_id, agent_instance_id, agent_type=None):
    """Pre-register an agent_id -> agent_instance_id mapping on disk."""
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_id, agent_instance_id, agent_type)


def _old_timestamp():
    """Return an ISO 8601 UTC timestamp that is definitely stale (year 2020)."""
    return "2020-01-01T00:00:00.000Z"


def _fresh_timestamp():
    """Return the current UTC time as ISO 8601 ms timestamp."""
    now = datetime.datetime.now(datetime.timezone.utc)
    ms = now.microsecond // 1000
    return now.strftime("%Y-%m-%dT%H:%M:%S.") + f"{ms:03d}Z"


def _write_pending_dispatch_file(path, entries):
    """Write a JSONL pending-dispatch file directly (for test setup).

    Each entry in `entries` is a dict with at minimum 'agent_instance_id',
    'run_id' (may be None), and 'created_at'.
    """
    path = pathlib.Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        for entry in entries:
            fh.write(json.dumps(entry) + "\n")


# ---------------------------------------------------------------------------
# LogPaths extension tests
# ---------------------------------------------------------------------------

class TestLogPathsPendingDispatch(unittest.TestCase):
    """LogPaths must expose pending_dispatch_dir and pending_dispatch_entry methods."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_pending_dispatch_dir_returns_path_under_run_root(self):
        """pending_dispatch_dir must return a path inside the run root."""
        d = self.paths.pending_dispatch_dir(_RUN_ID)
        self.assertTrue(str(d).startswith(str(self.paths.run_root(_RUN_ID))))

    def test_pending_dispatch_dir_uses_dot_prefix_dirname(self):
        """The pending-dispatch directory must use the documented dirname."""
        d = self.paths.pending_dispatch_dir(_RUN_ID)
        self.assertEqual(".pending-dispatch", d.name)

    def test_pending_dispatch_entry_is_inside_pending_dispatch_dir(self):
        """pending_dispatch_entry must be inside pending_dispatch_dir."""
        entry = self.paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        expected_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        self.assertEqual(expected_dir, entry.parent)

    def test_pending_dispatch_entry_has_jsonl_suffix(self):
        """The pending-dispatch queue file must have a .jsonl extension."""
        entry = self.paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        self.assertEqual(".jsonl", entry.suffix)

    def test_different_sessions_produce_different_entry_paths(self):
        """Two different session_ids must map to different queue files."""
        e1 = self.paths.pending_dispatch_entry(_RUN_ID, "session-a")
        e2 = self.paths.pending_dispatch_entry(_RUN_ID, "session-b")
        self.assertNotEqual(e1, e2)

    def test_same_session_different_runs_produce_different_paths(self):
        """Same session_id under different run_ids must be in different directories."""
        e1 = self.paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        e2 = self.paths.pending_dispatch_entry("other-run-id", _SESSION_ID)
        self.assertNotEqual(e1, e2)


# ---------------------------------------------------------------------------
# put_pending_dispatch tests
# ---------------------------------------------------------------------------

class TestPutPendingDispatch(unittest.TestCase):
    """put_pending_dispatch persists a pending-dispatch entry for the given session."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _put(self, session_id=_SESSION_ID, agent_instance_id=_AGENT_INSTANCE_ID,
             extracted_run_id=_EXTRACTED_RUN_ID, path_run_id=_RUN_ID):
        return runstate.put_pending_dispatch(
            self.paths, path_run_id, session_id, agent_instance_id, extracted_run_id
        )

    def _read_queue(self, session_id=_SESSION_ID, path_run_id=_RUN_ID):
        entry_path = self.paths.pending_dispatch_entry(path_run_id, session_id)
        return _read_jsonl(entry_path)

    def test_returns_true_on_success(self):
        result = self._put()
        self.assertTrue(result)

    def test_creates_entry_file(self):
        self._put()
        entry_path = self.paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        self.assertTrue(entry_path.exists())

    def test_entry_has_agent_instance_id(self):
        self._put(agent_instance_id="contracts-designer#4")
        entries = self._read_queue()
        self.assertEqual(1, len(entries))
        self.assertEqual("contracts-designer#4", entries[0]["agent_instance_id"])

    def test_entry_has_run_id(self):
        self._put(extracted_run_id="20260101T090000Z-cc11")
        entries = self._read_queue()
        self.assertEqual("20260101T090000Z-cc11", entries[0]["run_id"])

    def test_entry_has_null_run_id_when_none(self):
        """When extracted_run_id is None it must be stored as null (not omitted)."""
        self._put(extracted_run_id=None)
        entries = self._read_queue()
        self.assertIn("run_id", entries[0])
        self.assertIsNone(entries[0]["run_id"])

    def test_entry_has_created_at_field(self):
        self._put()
        entries = self._read_queue()
        self.assertIn("created_at", entries[0])

    def test_created_at_is_iso8601_utc_ms(self):
        self._put()
        entries = self._read_queue()
        ts = entries[0]["created_at"]
        self.assertRegex(ts, r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")

    def test_creates_parent_directory(self):
        """put_pending_dispatch must create the .pending-dispatch directory if absent."""
        pending_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        self.assertFalse(pending_dir.exists(), "Pre-condition: dir must not exist yet")
        self._put()
        self.assertTrue(pending_dir.exists())

    def test_multiple_calls_append_in_order(self):
        """Multiple calls for the same session must append, preserving insertion order."""
        self._put(agent_instance_id="agent-alpha#1")
        self._put(agent_instance_id="agent-beta#2")
        self._put(agent_instance_id="agent-gamma#3")
        entries = self._read_queue()
        self.assertEqual(3, len(entries))
        self.assertEqual("agent-alpha#1", entries[0]["agent_instance_id"])
        self.assertEqual("agent-beta#2", entries[1]["agent_instance_id"])
        self.assertEqual("agent-gamma#3", entries[2]["agent_instance_id"])

    def test_different_sessions_write_to_separate_files(self):
        """Entries for different session_ids must land in separate files."""
        self._put(session_id="session-x", agent_instance_id="agent-x#1")
        self._put(session_id="session-y", agent_instance_id="agent-y#2")
        entries_x = self._read_queue(session_id="session-x")
        entries_y = self._read_queue(session_id="session-y")
        self.assertEqual(1, len(entries_x))
        self.assertEqual(1, len(entries_y))
        self.assertEqual("agent-x#1", entries_x[0]["agent_instance_id"])
        self.assertEqual("agent-y#2", entries_y[0]["agent_instance_id"])

    def test_returns_false_on_io_failure(self):
        """Returns False when the file cannot be written (parent is a file, not dir)."""
        # Block directory creation by placing a file where the dir would be.
        pending_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        pending_dir.parent.mkdir(parents=True, exist_ok=True)
        # Write a regular file where the directory should be.
        pending_dir.write_bytes(b"blocker")
        result = self._put()
        self.assertFalse(result)

    def test_never_raises(self):
        """put_pending_dispatch must never raise, even on total I/O failure."""
        pending_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        pending_dir.parent.mkdir(parents=True, exist_ok=True)
        pending_dir.write_bytes(b"blocker")
        try:
            runstate.put_pending_dispatch(
                self.paths, _RUN_ID, _SESSION_ID, _AGENT_INSTANCE_ID, None
            )
        except Exception as exc:
            self.fail(f"put_pending_dispatch raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# pop_pending_dispatch tests
# ---------------------------------------------------------------------------

class TestPopPendingDispatch(unittest.TestCase):
    """pop_pending_dispatch: FIFO, stale eviction, resilience."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _entry_path(self, session_id=_SESSION_ID):
        return self.paths.pending_dispatch_entry(_RUN_ID, session_id)

    def _pop(self, session_id=_SESSION_ID, max_age_seconds=120):
        return runstate.pop_pending_dispatch(
            self.paths, _RUN_ID, session_id, max_age_seconds=max_age_seconds
        )

    def _put(self, session_id=_SESSION_ID, agent_instance_id=_AGENT_INSTANCE_ID,
             extracted_run_id=_EXTRACTED_RUN_ID):
        runstate.put_pending_dispatch(
            self.paths, _RUN_ID, session_id, agent_instance_id, extracted_run_id
        )

    def test_returns_none_when_file_absent(self):
        result = self._pop()
        self.assertIsNone(result)

    def test_returns_none_for_empty_file(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"")
        result = self._pop()
        self.assertIsNone(result)

    def test_returns_dict_with_agent_instance_id(self):
        self._put(agent_instance_id="reviewer-tdd#7")
        result = self._pop()
        self.assertIsNotNone(result)
        self.assertEqual("reviewer-tdd#7", result["agent_instance_id"])

    def test_returns_dict_with_run_id(self):
        self._put(extracted_run_id="20260201T080000Z-ff99")
        result = self._pop()
        self.assertIsNotNone(result)
        self.assertEqual("20260201T080000Z-ff99", result["run_id"])

    def test_returns_dict_with_none_run_id_when_stored_as_null(self):
        self._put(extracted_run_id=None)
        result = self._pop()
        self.assertIsNotNone(result)
        self.assertIn("run_id", result)
        self.assertIsNone(result["run_id"])

    def test_pops_oldest_entry_first_fifo(self):
        """The oldest (first-appended) entry must be returned first."""
        self._put(agent_instance_id="first-agent#1")
        self._put(agent_instance_id="second-agent#2")
        result = self._pop()
        self.assertIsNotNone(result)
        self.assertEqual("first-agent#1", result["agent_instance_id"])

    def test_removes_popped_entry_from_queue(self):
        """After a pop, the consumed entry must no longer be in the queue."""
        self._put(agent_instance_id="first-agent#1")
        self._put(agent_instance_id="second-agent#2")
        self._pop()
        # A second pop must return the second entry, not the first.
        result2 = self._pop()
        self.assertIsNotNone(result2)
        self.assertEqual("second-agent#2", result2["agent_instance_id"])

    def test_queue_empty_after_all_entries_popped(self):
        """After popping all entries, further pops return None."""
        self._put(agent_instance_id="only-agent#1")
        self._pop()
        result = self._pop()
        self.assertIsNone(result)

    def test_preserves_remaining_entries_in_order(self):
        """After popping the first entry, remaining entries stay in FIFO order."""
        self._put(agent_instance_id="first#1")
        self._put(agent_instance_id="second#2")
        self._put(agent_instance_id="third#3")
        self._pop()  # pops first#1
        result2 = self._pop()
        self.assertEqual("second#2", result2["agent_instance_id"])
        result3 = self._pop()
        self.assertEqual("third#3", result3["agent_instance_id"])

    def test_stale_entries_are_evicted_during_pop(self):
        """Entries older than max_age_seconds are discarded without being returned."""
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        _write_pending_dispatch_file(path, [
            {"agent_instance_id": "stale-agent#1", "run_id": None,
             "created_at": _old_timestamp()},
        ])
        result = self._pop(max_age_seconds=120)
        self.assertIsNone(result, "Stale entry must be evicted, not returned")

    def test_fresh_entry_returned_after_stale_entries_evicted(self):
        """After evicting stale leading entries, the first fresh entry is returned."""
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        _write_pending_dispatch_file(path, [
            {"agent_instance_id": "stale-agent#1", "run_id": None,
             "created_at": _old_timestamp()},
            {"agent_instance_id": "fresh-agent#2", "run_id": "20260101T110000Z-a1b2",
             "created_at": _fresh_timestamp()},
        ])
        result = self._pop(max_age_seconds=120)
        self.assertIsNotNone(result)
        self.assertEqual("fresh-agent#2", result["agent_instance_id"])

    def test_all_stale_entries_returns_none(self):
        """When all entries are stale, pop returns None."""
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        _write_pending_dispatch_file(path, [
            {"agent_instance_id": "stale#1", "run_id": None,
             "created_at": _old_timestamp()},
            {"agent_instance_id": "stale#2", "run_id": None,
             "created_at": _old_timestamp()},
        ])
        result = self._pop(max_age_seconds=120)
        self.assertIsNone(result)

    def test_max_age_seconds_zero_evicts_all_entries(self):
        """max_age_seconds=0 makes all entries immediately stale."""
        self._put(agent_instance_id="any-agent#1")
        result = self._pop(max_age_seconds=0)
        self.assertIsNone(result)

    def test_returns_none_on_corrupt_file(self):
        """A file with invalid JSON lines returns None without raising."""
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("this is not json at all\n{broken\n", encoding="utf-8")
        result = self._pop()
        self.assertIsNone(result)

    def test_never_raises_on_absent_file(self):
        try:
            runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        except Exception as exc:
            self.fail(f"pop_pending_dispatch raised on absent file: {exc}")

    def test_never_raises_on_corrupt_file(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("}{totally broken{{", encoding="utf-8")
        try:
            runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        except Exception as exc:
            self.fail(f"pop_pending_dispatch raised on corrupt file: {exc}")

    def test_concurrent_session_isolation(self):
        """pop for one session_id must not affect the queue of another session_id."""
        self._put(session_id="session-alpha", agent_instance_id="alpha-agent#1")
        self._put(session_id="session-beta", agent_instance_id="beta-agent#2")
        # Pop only from session-alpha
        runstate.pop_pending_dispatch(self.paths, _RUN_ID, "session-alpha")
        # session-beta queue must be unaffected
        result = runstate.pop_pending_dispatch(self.paths, _RUN_ID, "session-beta")
        self.assertIsNotNone(result)
        self.assertEqual("beta-agent#2", result["agent_instance_id"])

    def test_path_run_id_mismatch_returns_none(self):
        """put and pop targeting different path_run_ids land in different buckets.

        When a dispatch is written under one run_id bucket and popped under a
        different run_id bucket, pop returns None (the entry was not found).
        This guards against the path_run_id consistency invariant regression
        documented in the design: if put and pop diverge, correlation silently
        fails and the fallback to 'unknown-agent' is the correct behavior.
        """
        runstate.put_pending_dispatch(
            self.paths, "run-bucket-A", _SESSION_ID, _AGENT_INSTANCE_ID, None
        )
        result = runstate.pop_pending_dispatch(
            self.paths, "run-bucket-B", _SESSION_ID
        )
        self.assertIsNone(result,
                           "pop from a different run_id bucket must return None")


# ---------------------------------------------------------------------------
# Concurrent write safety
# ---------------------------------------------------------------------------

class TestConcurrentPutPendingDispatch(unittest.TestCase):
    """Multiple concurrent put_pending_dispatch calls for the same session must not
    lose entries. Uses real subprocesses to exercise OS-level concurrent writes."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.tmp.cleanup()

    def test_concurrent_writes_produce_no_torn_lines(self):
        """All concurrent put_pending_dispatch subprocesses must produce intact
        JSONL lines (no torn writes, no interleaved partial lines).

        Asserts a structural invariant (every line is valid JSON) rather than
        a specific ordering, consistent with test_e2e_concurrency.py conventions.
        """
        n_concurrent = 6
        workspace = self.tmp.name

        # Helper script: each subprocess calls put_pending_dispatch once.
        helper_script = textwrap.dedent(f"""\
            import sys, os, pathlib
            sys.path.insert(0, {str(_ADAPTER_ROOT)!r})
            import mosaic_logger_core as core
            import mosaic_logger_runstate as runstate
            idx = sys.argv[1]
            workspace_root = pathlib.Path({workspace!r})
            paths = core.build_paths(workspace_root)
            runstate.put_pending_dispatch(
                paths, {_RUN_ID!r}, {_SESSION_ID!r},
                f"concurrent-agent#{{idx}}", None
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

        # Verify all subprocesses succeeded.
        for i, r in enumerate(results):
            if isinstance(r, Exception):
                self.fail(f"Subprocess {i} raised: {r}")
            self.assertEqual(0, r.returncode,
                             f"Subprocess {i} failed: {r.stderr.decode()[:300]}")

        # Verify structural integrity of the resulting JSONL file.
        paths = core.build_paths(pathlib.Path(workspace))
        entry_path = paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        self.assertTrue(entry_path.exists(), "Queue file must exist after concurrent puts")

        lines = entry_path.read_text("utf-8").splitlines()
        non_empty = [ln for ln in lines if ln.strip()]
        self.assertEqual(n_concurrent, len(non_empty),
                         f"Expected {n_concurrent} entries, got {len(non_empty)}")

        for i, ln in enumerate(non_empty):
            try:
                obj = json.loads(ln)
            except json.JSONDecodeError as exc:
                self.fail(f"Line {i+1} is not valid JSON (torn write?): {exc!r} | raw: {ln[:80]!r}")
            self.assertIn("agent_instance_id", obj,
                          f"Line {i+1} missing agent_instance_id")


# ---------------------------------------------------------------------------
# handle_pre_tool_use Agent branch
# ---------------------------------------------------------------------------

class TestPreToolUseAgentCapture(unittest.TestCase):
    """handle_pre_tool_use must capture pending dispatch when tool_name is 'Agent'."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _agent_ctx(self, prompt=_DISPATCH_PROMPT, **kwargs):
        return _make_pre_tool_ctx(
            self.tmp.name,
            run_id=None,  # PreToolUse has no run_id set (unknown-run bucket)
            tool_name="Agent",
            tool_input={"prompt": prompt},
            **kwargs,
        )

    def _read_queue(self, session_id=_SESSION_ID):
        # Under PreToolUse, effective_run_id returns "unknown-run".
        entry_path = self.paths.pending_dispatch_entry("unknown-run", session_id)
        if not entry_path.exists():
            return []
        return _read_jsonl(entry_path)

    def test_creates_pending_dispatch_for_agent_tool(self):
        """Calling handle_pre_tool_use with tool_name='Agent' creates a queue entry."""
        ctx = self._agent_ctx()
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(1, len(entries))

    def test_extracts_agent_instance_id_from_prompt(self):
        """The pending dispatch entry must contain the extracted agent_instance_id."""
        ctx = self._agent_ctx(prompt=_DISPATCH_PROMPT)
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(1, len(entries))
        self.assertEqual("planner-tdd-soft#9", entries[0]["agent_instance_id"])

    def test_extracts_run_id_from_prompt(self):
        """The pending dispatch entry must contain the extracted run_id."""
        ctx = self._agent_ctx(prompt=_DISPATCH_PROMPT)
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(1, len(entries))
        self.assertEqual("20260101T110000Z-a1b2", entries[0]["run_id"])

    def test_run_id_is_null_when_not_in_prompt(self):
        """When the prompt contains no run_id, the entry's run_id must be null."""
        ctx = self._agent_ctx(prompt=_DISPATCH_PROMPT_NO_RUN_ID)
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(1, len(entries))
        self.assertIsNone(entries[0]["run_id"])

    def test_tool_call_start_event_still_emitted_for_agent_tool(self):
        """The pending-dispatch write must not prevent the tool_call_start event."""
        ctx = self._agent_ctx(tool_use_id="tu-agent-001")
        tools.handle_pre_tool_use(ctx)
        # Agent tool with no agent_id -> routes to orchestrator stream.
        orc_events_path = self.paths.orchestrator_events("unknown-run")
        self.assertTrue(orc_events_path.exists(),
                        "tool_call_start event must still be written for Agent tool")
        events = _read_jsonl(orc_events_path)
        event_names = [e["event"] for e in events]
        self.assertIn("tool_call_start", event_names)

    def test_no_pending_dispatch_for_non_agent_tool(self):
        """A non-Agent tool (e.g., 'Read') must not create a pending dispatch."""
        ctx = _make_pre_tool_ctx(
            self.tmp.name,
            run_id=None,
            tool_name="Read",
            tool_input={"file_path": "/some/file.py"},
        )
        tools.handle_pre_tool_use(ctx)
        # The .pending-dispatch directory must not be created at all.
        pending_dir = self.paths.pending_dispatch_dir("unknown-run")
        if pending_dir.exists():
            files = list(pending_dir.iterdir())
            self.assertEqual(0, len(files),
                             "Non-Agent tool must not write any pending dispatch")

    def test_no_pending_dispatch_when_prompt_has_no_extractable_id(self):
        """When no agent_instance_id can be extracted from the prompt, no entry is written."""
        ctx = self._agent_ctx(prompt=_PLAIN_PROMPT)
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(0, len(entries),
                         "No pending dispatch when extraction yields no agent_instance_id")

    def test_no_pending_dispatch_when_tool_input_has_no_prompt(self):
        """When tool_input lacks a 'prompt' key, no pending dispatch is written."""
        ctx = _make_pre_tool_ctx(
            self.tmp.name,
            run_id=None,
            tool_name="Agent",
            tool_input={"other_key": "value"},
        )
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(0, len(entries))

    def test_tool_call_start_still_emitted_even_when_extraction_fails(self):
        """Pending-dispatch extraction failure must not suppress tool_call_start."""
        ctx = _make_pre_tool_ctx(
            self.tmp.name,
            run_id=None,
            tool_name="Agent",
            tool_use_id="tu-noextract-001",
            tool_input={"prompt": _PLAIN_PROMPT},
        )
        tools.handle_pre_tool_use(ctx)
        orc_events_path = self.paths.orchestrator_events("unknown-run")
        self.assertTrue(orc_events_path.exists())
        events = _read_jsonl(orc_events_path)
        self.assertIn("tool_call_start", [e["event"] for e in events])

    def test_never_raises_on_agent_tool(self):
        ctx = self._agent_ctx()
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised: {exc}")

    def test_never_raises_on_non_agent_tool(self):
        ctx = _make_pre_tool_ctx(
            self.tmp.name,
            run_id=None,
            tool_name="Write",
            tool_input={"file_path": "/f", "content": "x"},
        )
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised: {exc}")


# ---------------------------------------------------------------------------
# handle_subagent_start consuming pending dispatch
# ---------------------------------------------------------------------------

class TestSubagentStartConsumingPendingDispatch(unittest.TestCase):
    """handle_subagent_start must source agent_instance_id from the pending dispatch."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _pre_populate(self, session_id=_SESSION_ID,
                      agent_instance_id=_AGENT_INSTANCE_ID,
                      extracted_run_id=None,
                      path_run_id="unknown-run"):
        """Write a pending dispatch as if handle_pre_tool_use already ran."""
        runstate.put_pending_dispatch(
            self.paths, path_run_id, session_id,
            agent_instance_id, extracted_run_id
        )

    def _start_ctx(self, agent_id="harness-001", run_id=None, **kwargs):
        """Build a SubagentStart context with run_id=None by default (unknown-run bucket)."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": _SESSION_ID,
            "agent_id": agent_id,
            **kwargs,
        }
        workspace_root = pathlib.Path(self.tmp.name)
        ctx = core.HookContext(payload, workspace_root, self.paths, _TS)
        ctx.run_id = run_id  # None -> unknown-run bucket, matching put's path
        return ctx

    def _invocation_dirs(self, run_id="unknown-run"):
        run_root = self.paths.run_root(run_id)
        if not run_root.exists():
            return []
        return [d for d in run_root.iterdir()
                if d.is_dir() and not d.name.startswith(".")]

    def test_uses_pending_dispatch_agent_instance_id_for_mapping(self):
        """handle_subagent_start must use the pending dispatch's agent_instance_id."""
        self._pre_populate(agent_instance_id="planner-tdd-soft#9")
        ctx = self._start_ctx(agent_id="harness-abc")
        invocation.handle_subagent_start(ctx)
        mapping = runstate.get_agent_mapping(self.paths, "unknown-run", "harness-abc")
        self.assertIsNotNone(mapping)
        self.assertEqual("planner-tdd-soft#9", mapping["agent_instance_id"])

    def test_uses_pending_dispatch_not_agent_prompt(self):
        """The pending dispatch agent_instance_id must override any value in agent_prompt.

        After the fix, agent_prompt is no longer the source of identity; the
        pending dispatch is. Even when agent_prompt contains a different
        agent_instance_id, the pending dispatch's value must be used.
        """
        self._pre_populate(agent_instance_id="correct-agent#1")
        # agent_prompt contains a different (wrong) agent_instance_id.
        ctx = self._start_ctx(
            agent_id="harness-xyz",
            agent_prompt='{"agent_instance_id": "wrong-agent#99"}'
        )
        invocation.handle_subagent_start(ctx)
        mapping = runstate.get_agent_mapping(self.paths, "unknown-run", "harness-xyz")
        self.assertIsNotNone(mapping)
        self.assertEqual("correct-agent#1", mapping["agent_instance_id"],
                         "pending dispatch must be used, not agent_prompt")

    def test_pending_dispatch_consumed_after_start(self):
        """After handle_subagent_start pops the dispatch, the queue must be empty."""
        self._pre_populate(agent_instance_id="planner-tdd-soft#9")
        ctx = self._start_ctx(agent_id="harness-aaa")
        invocation.handle_subagent_start(ctx)
        # A second pop must return None (entry was consumed).
        result = runstate.pop_pending_dispatch(self.paths, "unknown-run", _SESSION_ID)
        self.assertIsNone(result,
                           "Pending dispatch must be consumed by handle_subagent_start")

    def test_falls_back_to_unknown_agent_when_no_pending_dispatch(self):
        """When no pending dispatch exists for the session, fallback is 'unknown-agent'."""
        ctx = self._start_ctx(agent_id="harness-bbb")
        invocation.handle_subagent_start(ctx)
        mapping = runstate.get_agent_mapping(self.paths, "unknown-run", "harness-bbb")
        self.assertIsNotNone(mapping)
        self.assertEqual("unknown-agent", mapping["agent_instance_id"])

    def test_fallback_does_not_raise(self):
        """Fallback when no pending dispatch exists must never raise."""
        ctx = self._start_ctx(agent_id="harness-ccc")
        try:
            invocation.handle_subagent_start(ctx)
        except Exception as exc:
            self.fail(f"handle_subagent_start raised when no pending dispatch: {exc}")

    def test_sets_ctx_run_id_from_pending_dispatch_when_unset(self):
        """When ctx.run_id is None and dispatch carries a run_id, ctx.run_id must be set."""
        self._pre_populate(
            agent_instance_id="planner-tdd-soft#9",
            extracted_run_id=_EXTRACTED_RUN_ID,
        )
        ctx = self._start_ctx(agent_id="harness-ddd")
        # ctx.run_id starts as None.
        self.assertIsNone(ctx.run_id)
        invocation.handle_subagent_start(ctx)
        self.assertEqual(_EXTRACTED_RUN_ID, ctx.run_id,
                         "ctx.run_id must be set from pending dispatch's run_id")

    def test_does_not_override_existing_ctx_run_id(self):
        """When ctx.run_id is already set, the pending dispatch's run_id must not override it."""
        self._pre_populate(
            agent_instance_id="planner-tdd-soft#9",
            extracted_run_id=_EXTRACTED_RUN_ID,
            path_run_id=_RUN_ID,
        )
        ctx = self._start_ctx(agent_id="harness-eee", run_id=_RUN_ID)
        # ctx.run_id is pre-set to _RUN_ID.
        invocation.handle_subagent_start(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id,
                         "ctx.run_id must not be overridden when already set")

    def test_events_still_emitted_when_falling_back(self):
        """Even on fallback to 'unknown-agent', invocation_start must be emitted."""
        ctx = self._start_ctx(agent_id="harness-fff")
        invocation.handle_subagent_start(ctx)
        dirs = self._invocation_dirs("unknown-run")
        self.assertEqual(1, len(dirs))
        event_file = dirs[0] / "03_events.jsonl"
        self.assertTrue(event_file.exists())
        events = _read_jsonl(event_file)
        event_names = [e["event"] for e in events]
        self.assertIn("invocation_start", event_names)


# ---------------------------------------------------------------------------
# Guard-then-pop ordering
# ---------------------------------------------------------------------------

class TestSubagentStartGuardThenPop(unittest.TestCase):
    """pop_pending_dispatch must be placed AFTER the `if not ctx.agent_id` guard.

    A SubagentStart firing with no ctx.agent_id must not consume the pending
    dispatch. A subsequent genuine SubagentStart must still pop it correctly.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _pre_populate(self, agent_instance_id=_AGENT_INSTANCE_ID):
        runstate.put_pending_dispatch(
            self.paths, "unknown-run", _SESSION_ID, agent_instance_id, None
        )

    def test_guard_triggered_call_does_not_consume_pending_dispatch(self):
        """SubagentStart with no agent_id must not pop the pending dispatch."""
        self._pre_populate(agent_instance_id="planner-tdd-soft#9")
        # Fire handle_subagent_start with no agent_id -> triggers the early-return guard.
        guard_payload = {
            "hook_event_name": "SubagentStart",
            "session_id": _SESSION_ID,
            # agent_id intentionally absent
        }
        workspace_root = pathlib.Path(self.tmp.name)
        guard_ctx = core.HookContext(guard_payload, workspace_root, self.paths, _TS)
        guard_ctx.run_id = None
        invocation.handle_subagent_start(guard_ctx)

        # The pending dispatch must still be available for the genuine call.
        genuine_payload = {
            "hook_event_name": "SubagentStart",
            "session_id": _SESSION_ID,
            "agent_id": "harness-genuine-001",
        }
        genuine_ctx = core.HookContext(genuine_payload, workspace_root, self.paths, _TS)
        genuine_ctx.run_id = None
        invocation.handle_subagent_start(genuine_ctx)

        # The genuine call must have used the pending dispatch's agent_instance_id.
        mapping = runstate.get_agent_mapping(
            self.paths, "unknown-run", "harness-genuine-001"
        )
        self.assertIsNotNone(mapping)
        self.assertEqual("planner-tdd-soft#9", mapping["agent_instance_id"],
                         "Genuine SubagentStart must pop the pending dispatch even after "
                         "a no-agent_id firing came first")


# ---------------------------------------------------------------------------
# End-to-end correlation
# ---------------------------------------------------------------------------

class TestEndToEndPreToolUseToSubagentStart(unittest.TestCase):
    """PreToolUse Agent firing followed by SubagentStart with the same session_id
    must produce the correct agent_instance_id in the mapping store."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _pre_tool_ctx(self, prompt, session_id=_SESSION_ID):
        return _make_pre_tool_ctx(
            self.tmp.name,
            run_id=None,
            session_id=session_id,
            tool_name="Agent",
            tool_input={"prompt": prompt},
            tool_use_id="tu-e2e-001",
        )

    def _start_ctx(self, agent_id, session_id=_SESSION_ID):
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": agent_id,
        }
        workspace_root = pathlib.Path(self.tmp.name)
        ctx = core.HookContext(payload, workspace_root, self.paths, _TS)
        ctx.run_id = None
        return ctx

    def test_correct_agent_instance_id_in_mapping_after_correlation(self):
        """PreToolUse Agent + SubagentStart with same session_id -> correct mapping.

        The dispatch prompt carries run_id "20260101T090000Z-bb33". handle_subagent_start
        sets ctx.run_id from the pending dispatch before computing its path bucket, so
        the agent mapping lands under "20260101T090000Z-bb33", not "unknown-run".
        """
        prompt = '{"agent_instance_id": "contracts-designer#17", "run_id": "20260101T090000Z-bb33"}'
        pre_ctx = self._pre_tool_ctx(prompt)
        tools.handle_pre_tool_use(pre_ctx)

        start_ctx = self._start_ctx("harness-e2e-001")
        invocation.handle_subagent_start(start_ctx)

        mapping = runstate.get_agent_mapping(
            self.paths, "20260101T090000Z-bb33", "harness-e2e-001"
        )
        self.assertIsNotNone(mapping)
        self.assertEqual("contracts-designer#17", mapping["agent_instance_id"])

    def test_fifo_ordering_for_multiple_dispatches(self):
        """Two PreToolUse firings followed by two SubagentStart firings: FIFO ordering."""
        prompt_a = '{"agent_instance_id": "agent-first#1"}'
        prompt_b = '{"agent_instance_id": "agent-second#2"}'

        # Fire two pre-tool-use events for the same session (parallel dispatches).
        pre_ctx_a = self._pre_tool_ctx(prompt_a)
        pre_ctx_b = self._pre_tool_ctx(prompt_b)
        tools.handle_pre_tool_use(pre_ctx_a)
        tools.handle_pre_tool_use(pre_ctx_b)

        # First SubagentStart pops first dispatch.
        start_ctx_a = self._start_ctx("harness-first-001")
        invocation.handle_subagent_start(start_ctx_a)

        # Second SubagentStart pops second dispatch.
        start_ctx_b = self._start_ctx("harness-second-002")
        invocation.handle_subagent_start(start_ctx_b)

        mapping_a = runstate.get_agent_mapping(
            self.paths, "unknown-run", "harness-first-001"
        )
        mapping_b = runstate.get_agent_mapping(
            self.paths, "unknown-run", "harness-second-002"
        )

        self.assertIsNotNone(mapping_a)
        self.assertEqual("agent-first#1", mapping_a["agent_instance_id"],
                         "First SubagentStart must receive first dispatch (FIFO)")
        self.assertIsNotNone(mapping_b)
        self.assertEqual("agent-second#2", mapping_b["agent_instance_id"],
                         "Second SubagentStart must receive second dispatch (FIFO)")

    def test_subagent_start_without_prior_pre_tool_use_falls_back(self):
        """SubagentStart with no prior PreToolUse for that session falls back to 'unknown-agent'."""
        start_ctx = self._start_ctx("harness-nopretool-001", session_id="no-prior-session")
        invocation.handle_subagent_start(start_ctx)

        mapping = runstate.get_agent_mapping(
            self.paths, "unknown-run", "harness-nopretool-001"
        )
        self.assertIsNotNone(mapping)
        self.assertEqual("unknown-agent", mapping["agent_instance_id"])

    def test_ctx_run_id_updated_from_dispatch_enabling_correct_path_routing(self):
        """When the pending dispatch carries a run_id and ctx.run_id is None,
        the resulting mapping must land under the dispatch's run_id bucket
        (not 'unknown-run'), because ctx.run_id is set before path resolution."""
        prompt = (
            f'{{"agent_instance_id": "planner-tdd-soft#9", '
            f'"run_id": "{_EXTRACTED_RUN_ID}"}}'
        )
        pre_ctx = self._pre_tool_ctx(prompt)
        tools.handle_pre_tool_use(pre_ctx)

        start_ctx = self._start_ctx("harness-runid-001")
        invocation.handle_subagent_start(start_ctx)

        # After the handler runs, ctx.run_id should be set to the extracted run_id.
        self.assertEqual(_EXTRACTED_RUN_ID, start_ctx.run_id,
                         "ctx.run_id must be set from the pending dispatch's run_id")

        # The mapping must land under _EXTRACTED_RUN_ID, not "unknown-run".
        mapping = runstate.get_agent_mapping(
            self.paths, _EXTRACTED_RUN_ID, "harness-runid-001"
        )
        self.assertIsNotNone(mapping,
                             "Agent mapping must be written under the dispatch's run_id bucket")
        self.assertEqual("planner-tdd-soft#9", mapping["agent_instance_id"])


# ---------------------------------------------------------------------------
# Unmapped SubagentStop filtering
# ---------------------------------------------------------------------------

class TestUnmappedSubagentStopFiltering(unittest.TestCase):
    """handle_subagent_stop must return early (no files, no events) when
    resolve_invocation_id returns an 'unmapped_*' value."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _stop_ctx(self, agent_id, run_id=_RUN_ID, **kwargs):
        return _make_stop_ctx(
            self.tmp.name, run_id=run_id, agent_id=agent_id,
            last_assistant_message='{"status_code": "SUCCESS"}',
            **kwargs,
        )

    def test_no_events_written_for_unmapped_agent_id(self):
        """When agent_id has no mapping, resolve_invocation_id returns 'unmapped_*'
        and handle_subagent_stop must write no event files."""
        # agent_id has no prior put_agent_mapping call -> unmapped.
        ctx = self._stop_ctx(agent_id="unmapped-harness-xyz")
        invocation.handle_subagent_stop(ctx)
        # No invocation directory must be created under the run root.
        run_root = self.paths.run_root(_RUN_ID)
        if run_root.exists():
            invocation_dirs = [
                d for d in run_root.iterdir()
                if d.is_dir() and not d.name.startswith(".")
            ]
            self.assertEqual(0, len(invocation_dirs),
                             "No invocation directory must be created for unmapped SubagentStop")
            # No artifact or transcript export files must exist (explicit AC2.1 coverage).
            md_files = list(run_root.rglob("*.md"))
            raw_files = list(run_root.rglob("*.raw"))
            self.assertEqual(0, len(md_files),
                             f"No .md artifact (02_output.md) must be written for unmapped "
                             f"SubagentStop; found: {md_files}")
            self.assertEqual(0, len(raw_files),
                             f"No .raw transcript export (04_session.raw) must be written for "
                             f"unmapped SubagentStop; found: {raw_files}")

    def test_no_invocation_end_event_for_unmapped_agent_id(self):
        """No invocation_end event must be written when the agent_id is unmapped."""
        ctx = self._stop_ctx(agent_id="unmapped-harness-abc")
        invocation.handle_subagent_stop(ctx)
        run_root = self.paths.run_root(_RUN_ID)
        # Traverse all jsonl files under the run root and check no invocation_end.
        if run_root.exists():
            for jsonl_file in run_root.rglob("*.jsonl"):
                events = _read_jsonl(jsonl_file)
                for e in events:
                    self.assertNotEqual(
                        "invocation_end", e.get("event"),
                        f"invocation_end must not be written for unmapped SubagentStop "
                        f"(found in {jsonl_file})"
                    )
            # No artifact or transcript export files must exist (explicit AC2.1 coverage).
            md_files = list(run_root.rglob("*.md"))
            raw_files = list(run_root.rglob("*.raw"))
            self.assertEqual(0, len(md_files),
                             f"No .md artifact (02_output.md) must be written for unmapped "
                             f"SubagentStop; found: {md_files}")
            self.assertEqual(0, len(raw_files),
                             f"No .raw transcript export (04_session.raw / 00_orchestrator_session.raw) "
                             f"must be written for unmapped SubagentStop; found: {raw_files}")

    def test_genuine_mapped_stop_still_processed(self):
        """A SubagentStop with a genuine mapping must still be processed normally."""
        _register_mapping(self.tmp.name, _RUN_ID, "harness-mapped-001",
                          "genuine-agent#1", "GenuineAgent")
        ctx = self._stop_ctx(agent_id="harness-mapped-001")
        invocation.handle_subagent_stop(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "genuine-agent#1")
        self.assertTrue(event_path.exists(),
                        "Genuine SubagentStop must write to the invocation event stream")
        events = _read_jsonl(event_path)
        self.assertIn("invocation_end", [e["event"] for e in events])

    def test_never_raises_for_unmapped_stop(self):
        """handle_subagent_stop must never raise when the agent_id is unmapped."""
        ctx = self._stop_ctx(agent_id="unmapped-harness-neverraise")
        try:
            invocation.handle_subagent_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_subagent_stop raised for unmapped agent_id: {exc}")


# ---------------------------------------------------------------------------
# handle_stop guard removal
# ---------------------------------------------------------------------------

class TestHandleStopGuardRemoval(unittest.TestCase):
    """handle_stop must process Stop events even when ctx.agent_id is present.

    The current code has `if ctx.agent_id: return` which silently skips
    orchestrator Stop events when the orchestrator is launched with --agent.
    After the fix, event-name-based dispatch (Stop vs SubagentStop) already
    distinguishes orchestrator from subagent; the agent_id guard is redundant
    and must be removed.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _stop_ctx(self, agent_id=None, **kwargs):
        payload = {
            "hook_event_name": "Stop",
            "session_id": _SESSION_ID,
            # last_assistant_message must be present for handle_stop to emit a turn;
            # without it, handle_stop returns early regardless of agent_id.
            "last_assistant_message": '{"status_code": "SUCCESS", "status_message": "Done."}',
        }
        if agent_id is not None:
            payload["agent_id"] = agent_id
        payload.update(kwargs)
        workspace_root = pathlib.Path(self.tmp.name)
        ctx = core.HookContext(payload, workspace_root, self.paths, _TS)
        ctx.run_id = _RUN_ID
        return ctx

    def test_stop_with_agent_id_emits_turn_event(self):
        """handle_stop with agent_id set must still emit a turn event.

        Currently FAILS because the guard `if ctx.agent_id: return` is in place.
        After the fix (guard removed), this test must pass.
        """
        ctx = self._stop_ctx(
            agent_id="orchestrator-agent-id-xyz",
            stop_reason="end_turn",
        )
        session_handlers.handle_stop(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertTrue(orc_path.exists(),
                        "handle_stop must write to orchestrator events even when agent_id is set")
        events = _read_jsonl(orc_path)
        event_names = [e["event"] for e in events]
        self.assertIn("turn", event_names,
                      "handle_stop must emit a turn event regardless of agent_id")

    def test_stop_without_agent_id_still_emits_turn_event(self):
        """Baseline: handle_stop without agent_id must still emit a turn event."""
        ctx = self._stop_ctx(stop_reason="end_turn")
        session_handlers.handle_stop(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertTrue(orc_path.exists())
        events = _read_jsonl(orc_path)
        self.assertIn("turn", [e["event"] for e in events])

    def test_emitted_turn_has_role_assistant(self):
        """The turn event emitted by handle_stop must have role='assistant'."""
        ctx = self._stop_ctx(agent_id="orchestrator-agent-id-abc")
        session_handlers.handle_stop(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        events = _read_jsonl(orc_path)
        turn_events = [e for e in events if e.get("event") == "turn"]
        self.assertTrue(len(turn_events) > 0, "At least one turn event must be emitted")
        self.assertEqual("assistant", turn_events[-1].get("role"))

    def test_never_raises_when_agent_id_present(self):
        """handle_stop must never raise when agent_id is present."""
        ctx = self._stop_ctx(agent_id="orchestrator-agent-id-safe")
        try:
            session_handlers.handle_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_stop raised when agent_id was set: {exc}")


if __name__ == "__main__":
    unittest.main()
