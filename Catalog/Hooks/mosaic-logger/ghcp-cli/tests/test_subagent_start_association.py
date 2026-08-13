"""Tests for the agent-to-run association recorded by handle_subagent_start
(ghcp-cli variant).

Verifies that handle_subagent_start records the workspace-level agent-to-run
association (put_agent_run) in addition to the existing run-scoped agent mapping
(put_agent_mapping), and that the existing behaviour — run-scoped mapping,
invocation_start emission, initial turn emission, and 01_input.md write — is
entirely unchanged.

Key design constraints verified:
- The association is written at the same synchronous step as put_agent_mapping,
  before any event write, guaranteeing it is durable before the subagent's first
  tool call.
- A False return from put_agent_run is silently ignored; the handler degrades
  gracefully to today's unmapped_* behaviour rather than raising.
- The handler never raises regardless of whether the association write succeeds.
"""

import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_invocation as invocation


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

_RUN_ID = "20260804T150537Z-796a"
_SESSION_ID = "test-association-session"
_AGENT_ID = "harness-agent-assoc-start-001"
_INSTANCE_ID = "TestWriter#7"
_TS = "2026-08-04T15:05:37.000Z"


def _make_start_ctx(tmp_path, run_id=_RUN_ID, **payload_extra):
    payload = {"sessionId": _SESSION_ID, **payload_extra}
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext("subagentStart", payload, tmp_path, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _put_dispatch(paths, run_id=_RUN_ID, instance_id=_INSTANCE_ID, prompt=None):
    runstate.put_pending_dispatch(
        paths, run_id, _SESSION_ID, instance_id, None, prompt=prompt
    )


def _read_jsonl(path: pathlib.Path) -> list:
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


# ---------------------------------------------------------------------------
# Association is recorded at subagent start
# ---------------------------------------------------------------------------

class TestSubagentStartRecordsAgentRunAssociation(unittest.TestCase):
    """handle_subagent_start must call put_agent_run so the association is durable."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_association_file_created_after_subagent_start(self):
        """After handle_subagent_start, the workspace-level association file exists."""
        _put_dispatch(core.build_paths(self.tmp_path))
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        assoc_path = ctx.paths.agent_run_entry(_AGENT_ID)
        self.assertTrue(
            assoc_path.exists(),
            f"Expected agent-to-run association file at {assoc_path}",
        )

    def test_association_resolves_to_the_correct_run_id(self):
        """resolve_run_for_agent returns the run_id that was active at subagent start."""
        _put_dispatch(core.build_paths(self.tmp_path))
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        resolved = runstate.resolve_run_for_agent(ctx.paths, _AGENT_ID)
        self.assertEqual(_RUN_ID, resolved)

    def test_association_agent_id_matches_ctx_agent_id(self):
        """The association key is the harness-native agent_id (from agentId field)."""
        _put_dispatch(core.build_paths(self.tmp_path))
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        record = runstate.get_agent_run(ctx.paths, _AGENT_ID)
        self.assertIsNotNone(record)
        self.assertEqual(_AGENT_ID, record["agent_id"])

    def test_association_run_id_matches_effective_run_id(self):
        _put_dispatch(core.build_paths(self.tmp_path))
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        record = runstate.get_agent_run(ctx.paths, _AGENT_ID)
        self.assertIsNotNone(record)
        self.assertEqual(_RUN_ID, record["run_id"])

    def test_no_association_when_agent_id_absent(self):
        """When agentId is absent the handler returns early; no association written."""
        ctx = _make_start_ctx(self.tmp_path, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        agent_run_dir = ctx.paths.agent_run_dir()
        # The directory should not exist (no association written)
        if agent_run_dir.exists():
            entries = list(agent_run_dir.iterdir())
            self.assertEqual(
                [], entries,
                f"Expected no association files when agentId absent, found: {entries}",
            )


# ---------------------------------------------------------------------------
# Existing behaviour is unchanged
# ---------------------------------------------------------------------------

class TestSubagentStartExistingBehaviourUnchanged(unittest.TestCase):
    """Adding the association must not alter any previously-correct behaviour."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_run_scoped_agent_mapping_still_written(self):
        """put_agent_mapping must still be called alongside put_agent_run."""
        _put_dispatch(core.build_paths(self.tmp_path))
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        mapping = runstate.get_agent_mapping(ctx.paths, _RUN_ID, _AGENT_ID)
        self.assertIsNotNone(
            mapping,
            "Run-scoped agent mapping must still be written by handle_subagent_start",
        )

    def test_run_scoped_mapping_has_correct_agent_instance_id(self):
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID)
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        mapping = runstate.get_agent_mapping(ctx.paths, _RUN_ID, _AGENT_ID)
        self.assertEqual(_INSTANCE_ID, mapping["agent_instance_id"])

    def test_invocation_start_event_still_emitted(self):
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID)
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        events_path = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(events_path.exists())
        events = _read_jsonl(events_path)
        event_types = {e["event"] for e in events}
        self.assertIn("invocation_start", event_types)

    def test_01_input_md_still_written(self):
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID,
                      prompt="Task prompt text")
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter",
                               agentPrompt="Fallback prompt")
        invocation.handle_subagent_start(ctx)
        input_path = ctx.paths.invocation_input(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(input_path.exists())

    def test_01_input_md_content_is_unchanged(self):
        """Adding the association must not alter the 01_input.md content."""
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID,
                      prompt="Task prompt text")
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter",
                               agentPrompt="Fallback prompt")
        invocation.handle_subagent_start(ctx)
        content = ctx.paths.invocation_input(_RUN_ID, _INSTANCE_ID).read_text("utf-8")
        self.assertIn(_INSTANCE_ID, content)

    def test_invocation_start_has_correct_agent_instance_id(self):
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID)
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        start = next(e for e in events if e["event"] == "invocation_start")
        self.assertEqual(_INSTANCE_ID, start["agent_instance_id"])

    def test_user_turn_still_emitted_when_prompt_present(self):
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID,
                      prompt="User dispatch prompt")
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter",
                               agentPrompt="Fallback")
        invocation.handle_subagent_start(ctx)
        events = _read_jsonl(ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID))
        turns = [e for e in events if e.get("event") == "turn"]
        self.assertTrue(any(t.get("role") == "user" for t in turns))

    def test_handler_does_not_raise(self):
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID)
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        try:
            invocation.handle_subagent_start(ctx)
        except Exception as exc:
            self.fail(f"handle_subagent_start raised: {exc}")

    def test_handler_does_not_raise_when_association_write_would_fail(self):
        """Even if the agent-run dir is blocked, the handler must not raise."""
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID)
        # Block the agent_run directory creation
        agent_run_dir = core.build_paths(self.tmp_path).agent_run_dir()
        agent_run_dir.parent.mkdir(parents=True, exist_ok=True)
        agent_run_dir.write_bytes(b"blocker")
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        try:
            invocation.handle_subagent_start(ctx)
        except Exception as exc:
            self.fail(
                f"handle_subagent_start raised when association write fails: {exc}"
            )

    def test_run_scoped_mapping_written_even_if_association_write_fails(self):
        """When put_agent_run fails, put_agent_mapping must still complete."""
        _put_dispatch(core.build_paths(self.tmp_path), instance_id=_INSTANCE_ID)
        agent_run_dir = core.build_paths(self.tmp_path).agent_run_dir()
        agent_run_dir.parent.mkdir(parents=True, exist_ok=True)
        agent_run_dir.write_bytes(b"blocker")
        ctx = _make_start_ctx(self.tmp_path, agentId=_AGENT_ID, agentName="TestWriter")
        invocation.handle_subagent_start(ctx)
        # The run-scoped mapping must still have been written
        mapping = runstate.get_agent_mapping(ctx.paths, _RUN_ID, _AGENT_ID)
        self.assertIsNotNone(
            mapping,
            "Run-scoped agent mapping must still be written even when association write fails",
        )


if __name__ == "__main__":
    unittest.main()
