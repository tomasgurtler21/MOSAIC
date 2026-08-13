"""Tests for the pending-dispatch correlation mechanism in mosaic_logger_runstate
(vscode-ghcp variant).

In vscode-ghcp, the pending-dispatch queue bridges the gap between PreToolUse
(where the Agent/Task/runSubagent tool input contains the agent_instance_id
in tool_input.prompt) and SubagentStart (where agent_name is first known).
The queue mechanism is identical in behavior to the claude-code and ghcp-cli
variants.

Covers:
- put_pending_dispatch: persistence, JSONL append (FIFO), parent-dir creation,
  success/failure return, never-raises, prompt field round-trip
- pop_pending_dispatch: FIFO ordering, stale-entry eviction, empty-queue
  resilience, corrupt-file resilience, absent-file resilience,
  concurrent session isolation, max_age_seconds parameter
- Concurrent write safety
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


_RUN_ID = "20260101T120000Z-d7e3"
_SESSION_ID = "vscode-sess-pd-main"
_SESSION_ID_ALT = "vscode-sess-pd-alt"
_AGENT_INSTANCE_ID = "planner-tdd-soft#12"
_EXTRACTED_RUN_ID = "20260101T110000Z-a1b2"

_ADAPTER_ROOT = pathlib.Path(__file__).parent.parent

_DISPATCH_PROMPT = (
    '{"agent_instance_id": "planner-tdd-soft#12", '
    '"run_id": "20260101T110000Z-a1b2", '
    '"task_description": "Write the plan."}'
)


def _old_timestamp():
    """Return an ISO 8601 UTC timestamp that is definitely stale (year 2020)."""
    return "2020-01-01T00:00:00.000Z"


def _fresh_timestamp():
    """Return the current UTC time as ISO 8601 ms timestamp."""
    now = datetime.datetime.now(datetime.timezone.utc)
    ms = now.microsecond // 1000
    return now.strftime("%Y-%m-%dT%H:%M:%S.") + f"{ms:03d}Z"


def _read_jsonl(path):
    """Read all non-empty lines of a JSONL file as parsed dicts."""
    return [json.loads(ln) for ln in pathlib.Path(path).read_text("utf-8").splitlines()
            if ln.strip()]


def _write_pending_dispatch_file(path, entries):
    """Write a JSONL pending-dispatch file directly (for test setup)."""
    path = pathlib.Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        for entry in entries:
            fh.write(json.dumps(entry) + "\n")


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
        """When extracted_run_id is None it must be stored as null (not omitted).
        This is adapter-internal state, not an emitted event; the omit-null rule
        does not apply here."""
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
        pending_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        self.assertFalse(pending_dir.exists())
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
        self._put(session_id="session-x", agent_instance_id="agent-x#1")
        self._put(session_id="session-y", agent_instance_id="agent-y#2")
        entries_x = self._read_queue(session_id="session-x")
        entries_y = self._read_queue(session_id="session-y")
        self.assertEqual(1, len(entries_x))
        self.assertEqual(1, len(entries_y))
        self.assertEqual("agent-x#1", entries_x[0]["agent_instance_id"])
        self.assertEqual("agent-y#2", entries_y[0]["agent_instance_id"])

    def test_returns_false_on_io_failure(self):
        pending_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        pending_dir.parent.mkdir(parents=True, exist_ok=True)
        pending_dir.write_bytes(b"blocker")
        result = self._put()
        self.assertFalse(result)

    def test_never_raises(self):
        pending_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        pending_dir.parent.mkdir(parents=True, exist_ok=True)
        pending_dir.write_bytes(b"blocker")
        try:
            runstate.put_pending_dispatch(
                self.paths, _RUN_ID, _SESSION_ID, _AGENT_INSTANCE_ID, None
            )
        except Exception as exc:
            self.fail(f"put_pending_dispatch raised unexpectedly: {exc}")


class TestPutPendingDispatchPromptField(unittest.TestCase):
    """put_pending_dispatch accepts and persists a 'prompt' keyword argument.
    pop_pending_dispatch returns the stored prompt value.

    In vscode-ghcp, the prompt is the text from tool_input.prompt on a
    PreToolUse event with tool_name in SUBAGENT_DISPATCH_TOOL_NAMES. The full
    text is stored so that SubagentStart can recover agent_instance_id and
    run_id from it.
    """

    _SAMPLE_PROMPT = (
        '{"agent_instance_id": "test-writer-tdd#18", '
        '"run_id": "20260802T181610Z-e66f", '
        '"task_description": "Write failing tests for Stage 1."}'
    )

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_put_accepts_prompt_keyword_argument(self):
        try:
            result = runstate.put_pending_dispatch(
                self.paths, _RUN_ID, _SESSION_ID,
                _AGENT_INSTANCE_ID, _EXTRACTED_RUN_ID,
                prompt=self._SAMPLE_PROMPT,
            )
        except TypeError as exc:
            self.fail(
                "put_pending_dispatch does not accept 'prompt' keyword argument: "
                f"{exc}"
            )
        self.assertTrue(result)

    def test_put_persists_prompt_text_in_queue_entry(self):
        runstate.put_pending_dispatch(
            self.paths, _RUN_ID, _SESSION_ID,
            _AGENT_INSTANCE_ID, _EXTRACTED_RUN_ID,
            prompt=self._SAMPLE_PROMPT,
        )
        entry_path = self.paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        self.assertTrue(entry_path.exists())
        line = entry_path.read_text("utf-8").strip().splitlines()[0]
        entry = json.loads(line)
        self.assertIn("prompt", entry)
        self.assertEqual(self._SAMPLE_PROMPT, entry["prompt"])

    def test_prompt_key_absent_from_entry_when_not_given(self):
        """When no prompt is given, the queue entry must not contain a 'prompt' key."""
        runstate.put_pending_dispatch(
            self.paths, _RUN_ID, _SESSION_ID, _AGENT_INSTANCE_ID, _EXTRACTED_RUN_ID
        )
        entry_path = self.paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        line = entry_path.read_text("utf-8").strip().splitlines()[0]
        entry = json.loads(line)
        self.assertNotIn("prompt", entry)

    def test_pop_includes_prompt_key_in_returned_dict(self):
        runstate.put_pending_dispatch(
            self.paths, _RUN_ID, _SESSION_ID,
            _AGENT_INSTANCE_ID, _EXTRACTED_RUN_ID,
            prompt=self._SAMPLE_PROMPT,
        )
        result = runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNotNone(result)
        self.assertIn("prompt", result)

    def test_pop_returns_correct_prompt_value(self):
        runstate.put_pending_dispatch(
            self.paths, _RUN_ID, _SESSION_ID,
            _AGENT_INSTANCE_ID, _EXTRACTED_RUN_ID,
            prompt=self._SAMPLE_PROMPT,
        )
        result = runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNotNone(result)
        self.assertEqual(self._SAMPLE_PROMPT, result["prompt"])

    def test_pop_returns_none_for_prompt_when_entry_lacks_prompt_field(self):
        """Backward-compatible: entry without 'prompt' yields None from pop."""
        entry_path = self.paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        entry_path.parent.mkdir(parents=True, exist_ok=True)
        entry_path.write_text(
            json.dumps({
                "agent_instance_id": _AGENT_INSTANCE_ID,
                "run_id": _EXTRACTED_RUN_ID,
                "created_at": _fresh_timestamp(),
            }) + "\n",
            encoding="utf-8",
        )
        result = runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNotNone(result)
        self.assertIn("prompt", result)
        self.assertIsNone(result["prompt"])


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
        self._put(agent_instance_id="first-agent#1")
        self._put(agent_instance_id="second-agent#2")
        result = self._pop()
        self.assertIsNotNone(result)
        self.assertEqual("first-agent#1", result["agent_instance_id"])

    def test_removes_popped_entry_from_queue(self):
        self._put(agent_instance_id="first-agent#1")
        self._put(agent_instance_id="second-agent#2")
        self._pop()
        result2 = self._pop()
        self.assertIsNotNone(result2)
        self.assertEqual("second-agent#2", result2["agent_instance_id"])

    def test_queue_empty_after_all_entries_popped(self):
        self._put(agent_instance_id="only-agent#1")
        self._pop()
        result = self._pop()
        self.assertIsNone(result)

    def test_preserves_remaining_entries_in_order(self):
        self._put(agent_instance_id="first#1")
        self._put(agent_instance_id="second#2")
        self._put(agent_instance_id="third#3")
        self._pop()
        result2 = self._pop()
        self.assertEqual("second#2", result2["agent_instance_id"])
        result3 = self._pop()
        self.assertEqual("third#3", result3["agent_instance_id"])

    def test_stale_entries_are_evicted_during_pop(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        _write_pending_dispatch_file(path, [
            {"agent_instance_id": "stale-agent#1", "run_id": None,
             "created_at": _old_timestamp()},
        ])
        result = self._pop(max_age_seconds=120)
        self.assertIsNone(result, "Stale entry must be evicted, not returned")

    def test_fresh_entry_returned_after_stale_entries_evicted(self):
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
        self._put(agent_instance_id="any-agent#1")
        result = self._pop(max_age_seconds=0)
        self.assertIsNone(result)

    def test_returns_none_on_corrupt_file(self):
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
        runstate.put_pending_dispatch(
            self.paths, _RUN_ID, "session-alpha", "alpha-agent#1", None
        )
        runstate.put_pending_dispatch(
            self.paths, _RUN_ID, "session-beta", "beta-agent#2", None
        )
        runstate.pop_pending_dispatch(self.paths, _RUN_ID, "session-alpha")
        result = runstate.pop_pending_dispatch(self.paths, _RUN_ID, "session-beta")
        self.assertIsNotNone(result)
        self.assertEqual("beta-agent#2", result["agent_instance_id"])

    def test_path_run_id_mismatch_returns_none(self):
        """put and pop targeting different path_run_ids land in different buckets."""
        runstate.put_pending_dispatch(
            self.paths, "run-bucket-A", _SESSION_ID, _AGENT_INSTANCE_ID, None
        )
        result = runstate.pop_pending_dispatch(
            self.paths, "run-bucket-B", _SESSION_ID
        )
        self.assertIsNone(result)


class TestConcurrentPutPendingDispatch(unittest.TestCase):
    """Multiple concurrent put_pending_dispatch calls for the same session must not
    lose entries. Uses real subprocesses to exercise OS-level concurrent writes."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.tmp.cleanup()

    def test_concurrent_writes_produce_no_torn_lines(self):
        """All concurrent put_pending_dispatch subprocesses must produce intact
        JSONL lines (no torn writes)."""
        n_concurrent = 6
        workspace = self.tmp.name

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

        for i, r in enumerate(results):
            if isinstance(r, Exception):
                self.fail(f"Subprocess {i} raised: {r}")
            self.assertEqual(0, r.returncode,
                             f"Subprocess {i} failed: {r.stderr.decode()[:300]}")

        paths = core.build_paths(pathlib.Path(workspace))
        entry_path = paths.pending_dispatch_entry(_RUN_ID, _SESSION_ID)
        self.assertTrue(entry_path.exists())

        lines = entry_path.read_text("utf-8").splitlines()
        non_empty = [ln for ln in lines if ln.strip()]
        self.assertEqual(n_concurrent, len(non_empty))

        for i, ln in enumerate(non_empty):
            try:
                obj = json.loads(ln)
            except json.JSONDecodeError as exc:
                self.fail(f"Line {i+1} is not valid JSON (torn write?): {exc!r}")
            self.assertIn("agent_instance_id", obj)


if __name__ == "__main__":
    unittest.main()
