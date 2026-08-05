"""Tests for dispatcher run-identity resolution including session cache fallback
and prompt-extraction write-back in mosaic_logger (ghcp-cli variant).

Design: resolve_run_identity sets ctx.run_id via the following precedence:
  1. Prompt-field extraction (unchanged, highest priority)
  2. Session cache (fallback when step 1 yields nothing)
  3. None -> core.effective_run_id yields "unknown-run"
  Write-back: when step 1 produces a run_id and ctx.session_id is present,
  the value is persisted to the session cache. A value from step 2 is not
  written back (it is already cached).

Covers:
- Prompt extraction still wins when it yields a run_id (existing behaviour)
- Prompt result supersedes a different cached value for the same session
- Session cache supplies run_id for events with no prompt-bearing field
- Cache miss leaves ctx.run_id as None (core.effective_run_id -> "unknown-run")
- No session_id means no cache lookup and no write-back; never raises
- Write-back: prompt-extracted run_id is persisted to the session cache
- Write-back is skipped when session_id is absent
- Never raises under any input shape
"""

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger


_RUN_ID = "20260804T150537Z-796a"
_RUN_ID_CACHED = "20260101T000000Z-dead"
_SESSION_ID = "ghcp-sess-dispatcher-test"
_TS = "2026-08-04T15:05:37.000Z"


def _make_ctx(event, tmp_path, session_id=_SESSION_ID, **payload_extra):
    """Build a HookContext (run_id unset, as the dispatcher builds it)."""
    payload = {"sessionId": session_id, **payload_extra}
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(event, payload, tmp_path, paths, _TS)
    return ctx


class TestPromptExtractionStillWins(unittest.TestCase):
    """Prompt-field extraction behaviour is unchanged (existing tests remain green)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_subagent_start_extracts_run_id_from_agent_prompt(self):
        prompt = f'{{"agent_instance_id": "planner#1", "run_id": "{_RUN_ID}"}}'
        ctx = _make_ctx("subagentStart", self.tmp_path, agentPrompt=prompt)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_user_prompt_submitted_extracts_run_id_from_prompt_field(self):
        prompt = f"Starting orchestration run {_RUN_ID}"
        ctx = _make_ctx("userPromptSubmitted", self.tmp_path, prompt=prompt)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_notification_extracts_run_id_from_message_field(self):
        message = f"run_id: {_RUN_ID}"
        ctx = _make_ctx("notification", self.tmp_path, message=message)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_prompt_result_supersedes_cached_different_run_id(self):
        """When cache holds a different run_id, prompt extraction wins."""
        paths = core.build_paths(self.tmp_path)
        runstate.put_session_run_id(paths, _SESSION_ID, _RUN_ID_CACHED)
        prompt = f"run_id: {_RUN_ID}"
        ctx = _make_ctx("userPromptSubmitted", self.tmp_path, prompt=prompt)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_never_raises_on_malformed_prompt(self):
        ctx = _make_ctx("subagentStart", self.tmp_path, agentPrompt="}{broken json}")
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised: {exc}")

    def test_no_prompt_field_leaves_run_id_none_without_cache(self):
        """An event whose field has no run_id yields None when cache is also empty."""
        ctx = _make_ctx("subagentStart", self.tmp_path, agentPrompt="hello world")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)


class TestSessionCacheFallback(unittest.TestCase):
    """Session cache supplies run_id when prompt extraction yields nothing."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _populate_cache(self, run_id=_RUN_ID, session_id=_SESSION_ID):
        paths = core.build_paths(self.tmp_path)
        runstate.put_session_run_id(paths, session_id, run_id)

    def test_pre_tool_use_uses_cached_run_id(self):
        """preToolUse has no prompt-bearing field; cache hit supplies run_id."""
        self._populate_cache()
        ctx = _make_ctx("preToolUse", self.tmp_path, toolArgs={"command": "ls"})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_session_start_uses_cached_run_id(self):
        """sessionStart has no prompt-bearing field; cache hit supplies run_id."""
        self._populate_cache()
        ctx = _make_ctx("sessionStart", self.tmp_path)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_session_end_uses_cached_run_id(self):
        self._populate_cache()
        ctx = _make_ctx("sessionEnd", self.tmp_path)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_agent_stop_uses_cached_run_id(self):
        self._populate_cache()
        ctx = _make_ctx("agentStop", self.tmp_path)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_post_tool_use_uses_cached_run_id(self):
        self._populate_cache()
        ctx = _make_ctx("postToolUse", self.tmp_path)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_error_occurred_uses_cached_run_id(self):
        self._populate_cache()
        ctx = _make_ctx("errorOccurred", self.tmp_path)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_cache_miss_leaves_run_id_as_none(self):
        """When no cache entry exists, ctx.run_id stays None."""
        ctx = _make_ctx("preToolUse", self.tmp_path)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_cache_miss_effective_run_id_is_unknown_run(self):
        ctx = _make_ctx("preToolUse", self.tmp_path)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual("unknown-run", core.effective_run_id(ctx))

    def test_none_session_id_no_cache_lookup_no_raise(self):
        """Without a session_id there is nothing to look up; must not raise."""
        payload = {}
        paths = core.build_paths(self.tmp_path)
        ctx = core.HookContext("preToolUse", payload, self.tmp_path, paths, _TS)
        self.assertIsNone(ctx.session_id)
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised without session_id: {exc}")
        self.assertIsNone(ctx.run_id)


class TestRunIdentityWriteBack(unittest.TestCase):
    """Prompt-extracted run_id is written back to the session cache."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_prompt_extracted_run_id_is_persisted_to_cache(self):
        prompt = f"run_id: {_RUN_ID}"
        ctx = _make_ctx("userPromptSubmitted", self.tmp_path, prompt=prompt)
        mosaic_logger.resolve_run_identity(ctx)
        cached = runstate.get_session_run_id(ctx.paths, _SESSION_ID)
        self.assertEqual(_RUN_ID, cached)

    def test_subagent_start_prompt_run_id_is_cached(self):
        prompt = f'{{"agent_instance_id": "planner#1", "run_id": "{_RUN_ID}"}}'
        ctx = _make_ctx("subagentStart", self.tmp_path, agentPrompt=prompt)
        mosaic_logger.resolve_run_identity(ctx)
        cached = runstate.get_session_run_id(ctx.paths, _SESSION_ID)
        self.assertEqual(_RUN_ID, cached)

    def test_no_write_back_when_session_id_is_none(self):
        """Without session_id, no cache write is attempted and nothing is created."""
        prompt = f"run_id: {_RUN_ID}"
        payload = {"prompt": prompt}  # no sessionId
        paths = core.build_paths(self.tmp_path)
        ctx = core.HookContext("userPromptSubmitted", payload, self.tmp_path, paths, _TS)
        self.assertIsNone(ctx.session_id)
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised: {exc}")
        # No session dir should have been created
        session_dir = paths.session_run_id_dir()
        self.assertFalse(session_dir.exists())

    def test_cache_supplied_value_is_not_redundantly_written_back(self):
        """A run_id sourced from the cache must not be re-written (it is already there)."""
        # Populate cache with run_id, then resolve via cache for a non-prompt event.
        # This test verifies no errors occur; the implementation is free to skip the
        # redundant write as a detail.
        paths = core.build_paths(self.tmp_path)
        runstate.put_session_run_id(paths, _SESSION_ID, _RUN_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path)
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised: {exc}")
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_never_raises_on_write_back_failure(self):
        """Even if the cache write fails, the resolved run_id must still be set."""
        prompt = f"run_id: {_RUN_ID}"
        ctx = _make_ctx("userPromptSubmitted", self.tmp_path, prompt=prompt)
        # Sabotage the session run-id dir by placing a file where the dir should be
        session_dir = ctx.paths.session_run_id_dir()
        session_dir.parent.mkdir(parents=True, exist_ok=True)
        session_dir.write_bytes(b"blocker")  # prevents mkdir inside put_session_run_id
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised on write-back failure: {exc}")
        # The run_id resolved from the prompt must still be set
        self.assertEqual(_RUN_ID, ctx.run_id)


if __name__ == "__main__":
    unittest.main()
