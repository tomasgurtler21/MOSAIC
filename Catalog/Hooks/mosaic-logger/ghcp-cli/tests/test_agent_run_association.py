"""Tests for the workspace-level agent-to-run association store (ghcp-cli variant).

The agent-to-run association allows tool-event handlers to recover the run_id
for a subagent without knowing it in advance, breaking the circular dependency
of needing the run_id to locate the record that would tell you the run_id.

Covers:
- AGENT_RUN_MAX_AGE_SECONDS constant (must be 86400 / 24 hours)
- put_agent_run: writes an AgentRunRecord for agent_id via atomic replace,
  returns True on success, returns False on falsy inputs or I/O failure,
  latest-wins (overwrites prior record), creates parent directory, never raises
- get_agent_run: returns the parsed record dict on success, returns None
  for absent/empty/malformed/non-dict/stale files and for missing/empty run_id,
  max_age_seconds injectable, never raises
- resolve_run_for_agent: convenience accessor returning record["run_id"] when
  a fresh record with non-empty run_id exists, else None (never a fabricated
  fallback — that is the caller's responsibility), never raises
- Latest-wins overwrite semantics (most recent subagentStart wins per agent_id)
- Cross-agent isolation: different agent_ids cache independently
"""

import datetime
import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


_RUN_ID = "20260804T150537Z-796a"
_RUN_ID_ALT = "20260804T160000Z-aabb"
_AGENT_ID = "harness-agent-assoc-001"
_AGENT_ID_ALT = "harness-agent-assoc-002"


def _old_timestamp():
    """Return an ISO 8601 UTC timestamp that is definitely stale (year 2020)."""
    return "2020-01-01T00:00:00.000Z"


def _fresh_timestamp():
    now = datetime.datetime.now(datetime.timezone.utc)
    ms = now.microsecond // 1000
    return now.strftime("%Y-%m-%dT%H:%M:%S.") + f"{ms:03d}Z"


# ---------------------------------------------------------------------------
# Module constant
# ---------------------------------------------------------------------------

class TestAgentRunMaxAgeConstant(unittest.TestCase):
    """AGENT_RUN_MAX_AGE_SECONDS must be defined and set to 86400 (24 hours)."""

    def test_max_age_constant_exists(self):
        self.assertTrue(hasattr(runstate, "AGENT_RUN_MAX_AGE_SECONDS"))

    def test_max_age_constant_is_86400(self):
        self.assertEqual(86400, runstate.AGENT_RUN_MAX_AGE_SECONDS)


# ---------------------------------------------------------------------------
# put_agent_run
# ---------------------------------------------------------------------------

class TestPutAgentRun(unittest.TestCase):
    """put_agent_run persists an agent_id -> run_id association via atomic replace."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _put(self, agent_id=_AGENT_ID, run_id=_RUN_ID):
        return runstate.put_agent_run(self.paths, agent_id, run_id)

    def test_returns_true_on_success(self):
        self.assertTrue(self._put())

    def test_creates_association_file(self):
        self._put()
        path = self.paths.agent_run_entry(_AGENT_ID)
        self.assertTrue(path.exists())

    def test_file_contains_agent_id_field(self):
        self._put(agent_id=_AGENT_ID)
        path = self.paths.agent_run_entry(_AGENT_ID)
        data = json.loads(path.read_text("utf-8"))
        self.assertEqual(_AGENT_ID, data["agent_id"])

    def test_file_contains_run_id_field(self):
        self._put(run_id=_RUN_ID)
        path = self.paths.agent_run_entry(_AGENT_ID)
        data = json.loads(path.read_text("utf-8"))
        self.assertEqual(_RUN_ID, data["run_id"])

    def test_file_contains_created_at_field(self):
        self._put()
        path = self.paths.agent_run_entry(_AGENT_ID)
        data = json.loads(path.read_text("utf-8"))
        self.assertIn("created_at", data)

    def test_created_at_is_iso8601_utc_ms_format(self):
        import re
        self._put()
        path = self.paths.agent_run_entry(_AGENT_ID)
        data = json.loads(path.read_text("utf-8"))
        pattern = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")
        self.assertRegex(data["created_at"], pattern)

    def test_creates_parent_directory_when_missing(self):
        agent_run_dir = self.paths.agent_run_dir()
        self.assertFalse(agent_run_dir.exists())
        self._put()
        self.assertTrue(agent_run_dir.exists())

    def test_returns_false_for_none_agent_id(self):
        result = runstate.put_agent_run(self.paths, None, _RUN_ID)
        self.assertFalse(result)

    def test_returns_false_for_empty_string_agent_id(self):
        result = runstate.put_agent_run(self.paths, "", _RUN_ID)
        self.assertFalse(result)

    def test_returns_false_for_none_run_id(self):
        result = runstate.put_agent_run(self.paths, _AGENT_ID, None)
        self.assertFalse(result)

    def test_returns_false_for_empty_string_run_id(self):
        result = runstate.put_agent_run(self.paths, _AGENT_ID, "")
        self.assertFalse(result)

    def test_never_raises_for_none_inputs(self):
        try:
            runstate.put_agent_run(self.paths, None, None)
        except Exception as exc:
            self.fail(f"put_agent_run raised unexpectedly: {exc}")

    def test_latest_write_overwrites_previous_record(self):
        """Latest-wins: a second put for the same agent_id replaces the first."""
        self._put(run_id=_RUN_ID)
        runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID_ALT)
        path = self.paths.agent_run_entry(_AGENT_ID)
        data = json.loads(path.read_text("utf-8"))
        self.assertEqual(_RUN_ID_ALT, data["run_id"])

    def test_second_put_same_agent_still_returns_true(self):
        self._put(run_id=_RUN_ID)
        result = runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID_ALT)
        self.assertTrue(result)

    def test_different_agent_ids_write_to_separate_files(self):
        self._put(agent_id=_AGENT_ID, run_id=_RUN_ID)
        runstate.put_agent_run(self.paths, _AGENT_ID_ALT, _RUN_ID_ALT)
        entry_main = self.paths.agent_run_entry(_AGENT_ID)
        entry_alt = self.paths.agent_run_entry(_AGENT_ID_ALT)
        self.assertNotEqual(entry_main, entry_alt)
        self.assertTrue(entry_main.exists())
        self.assertTrue(entry_alt.exists())

    def test_returns_false_on_io_failure(self):
        """Returns False when the association file cannot be written."""
        agent_run_dir = self.paths.agent_run_dir()
        agent_run_dir.parent.mkdir(parents=True, exist_ok=True)
        agent_run_dir.write_bytes(b"blocker")  # place a file where dir would be
        result = runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID)
        self.assertFalse(result)

    def test_never_raises_on_io_failure(self):
        """put_agent_run must never raise, even on I/O failure."""
        agent_run_dir = self.paths.agent_run_dir()
        agent_run_dir.parent.mkdir(parents=True, exist_ok=True)
        agent_run_dir.write_bytes(b"blocker")
        try:
            runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID)
        except Exception as exc:
            self.fail(f"put_agent_run raised unexpectedly on I/O failure: {exc}")


# ---------------------------------------------------------------------------
# get_agent_run
# ---------------------------------------------------------------------------

class TestGetAgentRun(unittest.TestCase):
    """get_agent_run reads the association dict; returns None on any miss or staleness."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _put(self, agent_id=_AGENT_ID, run_id=_RUN_ID):
        runstate.put_agent_run(self.paths, agent_id, run_id)

    def _get(self, agent_id=_AGENT_ID, max_age=86400):
        return runstate.get_agent_run(
            self.paths, agent_id, max_age_seconds=max_age
        )

    def test_returns_dict_after_successful_put(self):
        self._put()
        result = self._get()
        self.assertIsNotNone(result)
        self.assertIsInstance(result, dict)

    def test_returned_dict_has_correct_run_id(self):
        self._put(run_id=_RUN_ID)
        result = self._get()
        self.assertEqual(_RUN_ID, result["run_id"])

    def test_returned_dict_has_correct_agent_id(self):
        self._put(agent_id=_AGENT_ID)
        result = self._get()
        self.assertEqual(_AGENT_ID, result["agent_id"])

    def test_returns_none_for_unknown_agent_id(self):
        result = self._get(agent_id="nonexistent-agent-xyz")
        self.assertIsNone(result)

    def test_returns_none_when_no_associations_exist(self):
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_for_none_agent_id(self):
        result = runstate.get_agent_run(self.paths, None)
        self.assertIsNone(result)

    def test_returns_none_for_empty_string_agent_id(self):
        result = runstate.get_agent_run(self.paths, "")
        self.assertIsNone(result)

    def test_returns_none_when_file_absent(self):
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_for_empty_file(self):
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"")
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_for_malformed_json(self):
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("this is not json", encoding="utf-8")
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_when_json_is_not_a_dict(self):
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('["agent_id", "harness-agent-001"]', encoding="utf-8")
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_for_stale_entry(self):
        """An entry with a far-past created_at is not returned."""
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({
            "agent_id": _AGENT_ID,
            "run_id": _RUN_ID,
            "created_at": _old_timestamp(),
        }), encoding="utf-8")
        result = self._get(max_age=120)  # year-2020 entry is stale
        self.assertIsNone(result)

    def test_fresh_entry_returned_within_max_age(self):
        self._put()
        result = self._get(max_age=86400)
        self.assertIsNotNone(result)

    def test_max_age_zero_evicts_all_entries(self):
        """max_age_seconds=0 means every entry is immediately stale."""
        self._put()
        result = self._get(max_age=0)
        self.assertIsNone(result)

    def test_stale_read_does_not_delete_or_truncate_file(self):
        """Staleness is not an error; the file is left intact after a stale read."""
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({
            "agent_id": _AGENT_ID,
            "run_id": _RUN_ID,
            "created_at": _old_timestamp(),
        }), encoding="utf-8")
        self._get(max_age=120)
        self.assertTrue(path.exists())
        self.assertGreater(path.stat().st_size, 0)

    def test_never_raises_on_absent_file(self):
        try:
            runstate.get_agent_run(self.paths, _AGENT_ID)
        except Exception as exc:
            self.fail(f"get_agent_run raised on absent file: {exc}")

    def test_never_raises_on_malformed_file(self):
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("}{totally broken{{", encoding="utf-8")
        try:
            runstate.get_agent_run(self.paths, _AGENT_ID)
        except Exception as exc:
            self.fail(f"get_agent_run raised on malformed file: {exc}")

    def test_never_raises_on_none_agent_id(self):
        try:
            runstate.get_agent_run(self.paths, None)
        except Exception as exc:
            self.fail(f"get_agent_run raised for None agent_id: {exc}")

    def test_different_agents_return_their_respective_records(self):
        runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID)
        runstate.put_agent_run(self.paths, _AGENT_ID_ALT, _RUN_ID_ALT)
        result_main = runstate.get_agent_run(self.paths, _AGENT_ID)
        result_alt = runstate.get_agent_run(self.paths, _AGENT_ID_ALT)
        self.assertEqual(_RUN_ID, result_main["run_id"])
        self.assertEqual(_RUN_ID_ALT, result_alt["run_id"])

    def test_max_age_seconds_parameter_is_injectable(self):
        """The max_age_seconds parameter must be injectable for test control."""
        import inspect
        sig = inspect.signature(runstate.get_agent_run)
        self.assertIn("max_age_seconds", sig.parameters)


# ---------------------------------------------------------------------------
# resolve_run_for_agent
# ---------------------------------------------------------------------------

class TestResolveRunForAgent(unittest.TestCase):
    """resolve_run_for_agent returns run_id from a fresh record, else None.

    Unlike resolve_invocation_id, it returns None on a miss rather than a
    fabricated fallback — the caller decides the fallback because the distinction
    between 'no association' and 'associated to run X' requires different routing.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_run_id_when_fresh_association_exists(self):
        runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID)
        result = runstate.resolve_run_for_agent(self.paths, _AGENT_ID)
        self.assertEqual(_RUN_ID, result)

    def test_returns_none_for_unknown_agent(self):
        result = runstate.resolve_run_for_agent(self.paths, "nonexistent-agent")
        self.assertIsNone(result)

    def test_returns_none_for_none_agent_id(self):
        result = runstate.resolve_run_for_agent(self.paths, None)
        self.assertIsNone(result)

    def test_returns_none_for_empty_string_agent_id(self):
        result = runstate.resolve_run_for_agent(self.paths, "")
        self.assertIsNone(result)

    def test_returns_none_for_stale_association(self):
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({
            "agent_id": _AGENT_ID,
            "run_id": _RUN_ID,
            "created_at": _old_timestamp(),
        }), encoding="utf-8")
        result = runstate.resolve_run_for_agent(
            self.paths, _AGENT_ID, max_age_seconds=120
        )
        self.assertIsNone(result)

    def test_returns_none_for_malformed_file(self):
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("{bad json{{", encoding="utf-8")
        result = runstate.resolve_run_for_agent(self.paths, _AGENT_ID)
        self.assertIsNone(result)

    def test_result_is_a_string_not_a_dict(self):
        """resolve_run_for_agent returns a plain run_id string, not the whole record."""
        runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID)
        result = runstate.resolve_run_for_agent(self.paths, _AGENT_ID)
        self.assertIsInstance(result, str)

    def test_does_not_fabricate_fallback_on_miss(self):
        """On a miss, returns None (not a string like 'unmapped_...')."""
        result = runstate.resolve_run_for_agent(self.paths, "no-such-agent")
        self.assertIsNone(result)

    def test_latest_wins_most_recent_run_id_returned(self):
        """After two puts, the second run_id is returned."""
        runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID)
        runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID_ALT)
        result = runstate.resolve_run_for_agent(self.paths, _AGENT_ID)
        self.assertEqual(_RUN_ID_ALT, result)

    def test_never_raises_on_absent_file(self):
        try:
            runstate.resolve_run_for_agent(self.paths, _AGENT_ID)
        except Exception as exc:
            self.fail(f"resolve_run_for_agent raised on absent file: {exc}")

    def test_never_raises_on_none_agent_id(self):
        try:
            runstate.resolve_run_for_agent(self.paths, None)
        except Exception as exc:
            self.fail(f"resolve_run_for_agent raised for None agent_id: {exc}")

    def test_never_raises_on_corrupt_file(self):
        path = self.paths.agent_run_entry(_AGENT_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("}{totally broken{{", encoding="utf-8")
        try:
            runstate.resolve_run_for_agent(self.paths, _AGENT_ID)
        except Exception as exc:
            self.fail(f"resolve_run_for_agent raised on corrupt file: {exc}")

    def test_max_age_seconds_parameter_is_injectable(self):
        """The max_age_seconds parameter must be injectable for test control."""
        import inspect
        sig = inspect.signature(runstate.resolve_run_for_agent)
        self.assertIn("max_age_seconds", sig.parameters)

    def test_different_agents_resolve_independently(self):
        runstate.put_agent_run(self.paths, _AGENT_ID, _RUN_ID)
        runstate.put_agent_run(self.paths, _AGENT_ID_ALT, _RUN_ID_ALT)
        result_main = runstate.resolve_run_for_agent(self.paths, _AGENT_ID)
        result_alt = runstate.resolve_run_for_agent(self.paths, _AGENT_ID_ALT)
        self.assertEqual(_RUN_ID, result_main)
        self.assertEqual(_RUN_ID_ALT, result_alt)
        self.assertNotEqual(result_main, result_alt)


if __name__ == "__main__":
    unittest.main()
