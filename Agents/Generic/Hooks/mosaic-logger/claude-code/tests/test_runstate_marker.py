"""Tests for the run-marker state machine in mosaic_logger_runstate.

Covers: the four resolve_run minting branches (absent, open-fresh, closed, stale),
the always-reuse branch (open-fresh regardless of allow_mint), the UNRESOLVED branch
(no marker + allow_mint=False, missing/empty session_id), malformed-marker-as-absent,
and the close_run transition (fields preserved, resumable afterwards).
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


# Injected timestamps so tests never touch the real clock.
BASE_TIME = datetime.datetime(2026, 1, 1, 17, 0, 0, tzinfo=datetime.timezone.utc)
FRESH_TIME = BASE_TIME + datetime.timedelta(hours=1)    # within 24h of BASE_TIME
STALE_TIME = BASE_TIME + datetime.timedelta(hours=25)   # beyond 24h of BASE_TIME


class TestResolvRunMintBranch(unittest.TestCase):
    """Branch: no marker (or unreadable) + allow_mint=True → MINTED."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self.session_id = "session-abc"

    def tearDown(self):
        self.tmp.cleanup()

    def test_no_marker_with_allow_mint_yields_minted_outcome(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=BASE_TIME
        )
        self.assertEqual("minted", resolution.outcome)

    def test_minted_resolution_has_non_none_run_id(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=BASE_TIME
        )
        self.assertIsNotNone(resolution.run_id)

    def test_minted_marker_file_has_status_open(self):
        runstate.resolve_run(self.paths, self.session_id, allow_mint=True, now=BASE_TIME)
        marker = json.loads(
            self.paths.marker(self.session_id).read_text(encoding="utf-8")
        )
        self.assertEqual("open", marker["status"])

    def test_minted_marker_file_contains_same_run_id_as_resolution(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=BASE_TIME
        )
        marker = json.loads(
            self.paths.marker(self.session_id).read_text(encoding="utf-8")
        )
        self.assertEqual(resolution.run_id, marker["run_id"])

    def test_minted_marker_file_contains_opened_at(self):
        runstate.resolve_run(self.paths, self.session_id, allow_mint=True, now=BASE_TIME)
        marker = json.loads(
            self.paths.marker(self.session_id).read_text(encoding="utf-8")
        )
        self.assertIn("opened_at", marker)
        self.assertIsNotNone(marker["opened_at"])

    def test_malformed_marker_file_treated_as_absent(self):
        marker_path = self.paths.marker(self.session_id)
        marker_path.parent.mkdir(parents=True, exist_ok=True)
        marker_path.write_text("not valid json {{{", encoding="utf-8")
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=BASE_TIME
        )
        self.assertEqual("minted", resolution.outcome)


class TestResolveRunReuseBranch(unittest.TestCase):
    """Branch: open marker, opened_at within 24h → REUSED (for any allow_mint value)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self.session_id = "session-reuse"
        # Pre-create an open marker by minting first
        self.first = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=BASE_TIME
        )

    def tearDown(self):
        self.tmp.cleanup()

    def test_open_fresh_marker_yields_reused_outcome(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=FRESH_TIME
        )
        self.assertEqual("reused", resolution.outcome)

    def test_reused_resolution_carries_same_run_id_as_mint(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=FRESH_TIME
        )
        self.assertEqual(self.first.run_id, resolution.run_id)

    def test_open_fresh_marker_reused_even_when_allow_mint_false(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=False, now=FRESH_TIME
        )
        self.assertEqual("reused", resolution.outcome)


class TestResolveRunStaleMarkerBranch(unittest.TestCase):
    """Branch: marker older than 24h (any status) + allow_mint=True → MINTED."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self.session_id = "session-stale"
        self.original = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=BASE_TIME
        )

    def tearDown(self):
        self.tmp.cleanup()

    def test_stale_open_marker_yields_minted_outcome(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=STALE_TIME
        )
        self.assertEqual("minted", resolution.outcome)

    def test_stale_marker_mints_a_new_run_id(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=STALE_TIME
        )
        self.assertNotEqual(self.original.run_id, resolution.run_id)

    def test_stale_closed_marker_yields_minted_outcome(self):
        """Staleness supersedes the closed branch: a marker that is both older than
        24h and status 'closed' must still produce a MINTED outcome, not UNRESOLVED.
        The precedence table places 'older than 24h (any status)' after 'status closed',
        so staleness wins and a new run_id is minted."""
        runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=STALE_TIME
        )
        self.assertEqual("minted", resolution.outcome)

    def test_stale_closed_marker_mints_new_run_id(self):
        """The run_id minted for a stale+closed marker differs from the original."""
        runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=STALE_TIME
        )
        self.assertNotEqual(self.original.run_id, resolution.run_id)


class TestResolveRunClosedMarkerBranch(unittest.TestCase):
    """Branch: closed marker + allow_mint=True → MINTED (resume-for-a-new-run)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self.session_id = "session-resume"
        self.original = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=BASE_TIME
        )
        runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)

    def tearDown(self):
        self.tmp.cleanup()

    def test_closed_marker_with_allow_mint_yields_minted_outcome(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=FRESH_TIME
        )
        self.assertEqual("minted", resolution.outcome)

    def test_closed_marker_resume_mints_a_new_run_id(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=FRESH_TIME
        )
        self.assertNotEqual(self.original.run_id, resolution.run_id)


class TestResolveRunUnresolvedBranch(unittest.TestCase):
    """Branch: non-reusable state + allow_mint=False, or missing/empty session_id → UNRESOLVED."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self.session_id = "session-unresolved"

    def tearDown(self):
        self.tmp.cleanup()

    def test_no_marker_with_allow_mint_false_yields_unresolved(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=False, now=BASE_TIME
        )
        self.assertEqual("unresolved", resolution.outcome)

    def test_unresolved_outcome_has_none_run_id(self):
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=False, now=BASE_TIME
        )
        self.assertIsNone(resolution.run_id)

    def test_none_session_id_always_yields_unresolved(self):
        resolution = runstate.resolve_run(
            self.paths, None, allow_mint=True, now=BASE_TIME
        )
        self.assertEqual("unresolved", resolution.outcome)

    def test_empty_session_id_always_yields_unresolved(self):
        resolution = runstate.resolve_run(
            self.paths, "", allow_mint=True, now=BASE_TIME
        )
        self.assertEqual("unresolved", resolution.outcome)

    def test_stale_marker_with_allow_mint_false_yields_unresolved(self):
        runstate.resolve_run(self.paths, self.session_id, allow_mint=True, now=BASE_TIME)
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=False, now=STALE_TIME
        )
        self.assertEqual("unresolved", resolution.outcome)

    def test_closed_marker_with_allow_mint_false_yields_unresolved(self):
        runstate.resolve_run(self.paths, self.session_id, allow_mint=True, now=BASE_TIME)
        runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)
        resolution = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=False, now=FRESH_TIME
        )
        self.assertEqual("unresolved", resolution.outcome)

    def test_resolve_run_never_raises(self):
        try:
            runstate.resolve_run(self.paths, self.session_id, allow_mint=True)
        except Exception as e:
            self.fail(f"resolve_run raised unexpectedly: {e}")


class TestCloseRun(unittest.TestCase):
    """close_run transitions the marker to status:closed and returns a CLOSED resolution."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self.session_id = "session-close"
        self.minted = runstate.resolve_run(
            self.paths, self.session_id, allow_mint=True, now=BASE_TIME
        )

    def tearDown(self):
        self.tmp.cleanup()

    def test_close_run_yields_closed_outcome(self):
        resolution = runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)
        self.assertEqual("closed", resolution.outcome)

    def test_close_run_carries_original_run_id(self):
        resolution = runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)
        self.assertEqual(self.minted.run_id, resolution.run_id)

    def test_closed_marker_file_has_status_closed(self):
        runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)
        marker = json.loads(
            self.paths.marker(self.session_id).read_text(encoding="utf-8")
        )
        self.assertEqual("closed", marker["status"])

    def test_closed_marker_preserves_run_id(self):
        runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)
        marker = json.loads(
            self.paths.marker(self.session_id).read_text(encoding="utf-8")
        )
        self.assertEqual(self.minted.run_id, marker["run_id"])

    def test_closed_marker_preserves_opened_at(self):
        before = json.loads(
            self.paths.marker(self.session_id).read_text(encoding="utf-8")
        )
        runstate.close_run(self.paths, self.session_id, now=FRESH_TIME)
        after = json.loads(
            self.paths.marker(self.session_id).read_text(encoding="utf-8")
        )
        self.assertEqual(before["opened_at"], after["opened_at"])

    def test_close_run_with_no_existing_marker_yields_unresolved(self):
        # No prior resolve_run call for this session
        resolution = runstate.close_run(
            self.paths, "no-marker-session", now=FRESH_TIME
        )
        self.assertEqual("unresolved", resolution.outcome)

    def test_close_run_never_raises(self):
        try:
            runstate.close_run(self.paths, self.session_id)
        except Exception as e:
            self.fail(f"close_run raised unexpectedly: {e}")

    def test_close_run_none_session_id_yields_unresolved(self):
        resolution = runstate.close_run(self.paths, None, now=FRESH_TIME)
        self.assertEqual("unresolved", resolution.outcome)
