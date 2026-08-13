"""Tests for resolve_destination with the agent-to-run association (ghcp-cli variant).

Verifies the extended resolve_destination contract from Stage 2:

1. If ctx.agent_id is falsy → orchestrator stream (unchanged).
2. Otherwise, consult resolve_run_for_agent(ctx.paths, ctx.agent_id).
   - When it returns a run_id, that run_id is authoritative for the destination
     and overrides core.effective_run_id(ctx).
   - When it returns None, fall back to core.effective_run_id(ctx).
3. Resolve agent_instance_id = resolve_invocation_id(ctx.paths, run_id, ctx.agent_id)
   using the run_id chosen in step 2.
4. Return ctx.paths.invocation_events(run_id, agent_instance_id).

The association run_id is authoritative for agent-scoped events because it was
written synchronously by the very subagentStart that created the agent, whereas
ctx.run_id reflects the envelope (which may be stale or unknown-run).

resolve_destination must not mutate ctx; the run_id override is local to the
returned path so the event envelope's own run_id field keeps its existing
provenance.
"""

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_tools as tools


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

_RUN_ID = "20260804T150537Z-796a"
_RUN_ID_ALT = "20260804T160000Z-ccdd"
_SESSION_ID = "test-agent-run-dest-session"
_AGENT_ID = "harness-agent-dest-001"
_INSTANCE_ID = "ResearchAgent#3"
_TS = "2026-08-04T15:05:37.000Z"


def _make_ctx(event: str, tmp_path: pathlib.Path,
              run_id: "str | None" = _RUN_ID,
              session_id: str = _SESSION_ID,
              **payload_extra) -> core.HookContext:
    payload = {"sessionId": session_id, **payload_extra}
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(event, payload, tmp_path, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _register_mapping(tmp_path, run_id, agent_id, agent_instance_id):
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_id, agent_instance_id, None)


def _register_association(tmp_path, agent_id, run_id):
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_run(paths, agent_id, run_id)


# ---------------------------------------------------------------------------
# Orchestrator routing (no agentId) — unchanged
# ---------------------------------------------------------------------------

class TestResolveDestinationOrchestratorRouting(unittest.TestCase):
    """Events with no agentId still route to the orchestrator stream."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_routes_to_orchestrator_when_no_agent_id(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash")
        dest = tools.resolve_destination(ctx)
        expected = ctx.paths.orchestrator_events(_RUN_ID)
        self.assertEqual(expected, dest)

    def test_routes_to_orchestrator_when_agent_id_is_empty_string(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId="", toolName="bash")
        dest = tools.resolve_destination(ctx)
        expected = ctx.paths.orchestrator_events(_RUN_ID)
        self.assertEqual(expected, dest)

    def test_orchestrator_routing_uses_ctx_run_id(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash", run_id=_RUN_ID)
        dest = tools.resolve_destination(ctx)
        # Path must include the run_id component
        self.assertIn(_RUN_ID, str(dest))


# ---------------------------------------------------------------------------
# Association-based routing (agentId present, association recorded)
# ---------------------------------------------------------------------------

class TestResolveDestinationWithAssociation(unittest.TestCase):
    """When an agent-to-run association exists, it is authoritative for the destination."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_uses_association_run_id_when_association_exists(self):
        """The destination path must be rooted in the association's run_id."""
        _register_association(self.tmp_path, _AGENT_ID, _RUN_ID)
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash")
        dest = tools.resolve_destination(ctx)
        self.assertIn(_RUN_ID, str(dest))

    def test_routes_to_correct_invocation_events_file(self):
        """Destination must be the 03_events.jsonl inside the agent instance folder."""
        _register_association(self.tmp_path, _AGENT_ID, _RUN_ID)
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash")
        dest = tools.resolve_destination(ctx)
        expected = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertEqual(expected, dest)

    def test_association_run_id_overrides_ctx_run_id(self):
        """When ctx.run_id differs from the association, the association wins."""
        # ctx.run_id is "unknown-run" (simulating a cold-cache tool event),
        # but the association correctly maps the agent to _RUN_ID.
        _register_association(self.tmp_path, _AGENT_ID, _RUN_ID)
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        # Build ctx with run_id unset (will resolve to "unknown-run")
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash",
                        run_id=None)
        dest = tools.resolve_destination(ctx)
        expected = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertEqual(
            expected, dest,
            f"Association run_id {_RUN_ID!r} must override unknown-run; got {dest}",
        )

    def test_ctx_run_id_is_not_mutated_by_resolve_destination(self):
        """resolve_destination must not change ctx.run_id; the override is path-local."""
        _register_association(self.tmp_path, _AGENT_ID, _RUN_ID)
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash",
                        run_id=None)
        original_run_id = ctx.run_id
        tools.resolve_destination(ctx)
        self.assertEqual(
            original_run_id, ctx.run_id,
            "resolve_destination must not mutate ctx.run_id",
        )

    def test_correct_agent_instance_id_used_from_run_scoped_mapping(self):
        """Within the chosen run, agent_instance_id comes from the run-scoped map."""
        _register_association(self.tmp_path, _AGENT_ID, _RUN_ID)
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash")
        dest = tools.resolve_destination(ctx)
        self.assertIn(_INSTANCE_ID, str(dest))


# ---------------------------------------------------------------------------
# Fallback routing (agentId present, no association)
# ---------------------------------------------------------------------------

class TestResolveDestinationWithoutAssociation(unittest.TestCase):
    """When no association exists, resolve_destination falls back to ctx effective run_id."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_uses_ctx_run_id_when_no_association(self):
        """Without an association, ctx.run_id (effective_run_id) is used."""
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        # No put_agent_run — association is absent
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash",
                        run_id=_RUN_ID)
        dest = tools.resolve_destination(ctx)
        expected = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertEqual(expected, dest)

    def test_unmapped_fallback_when_no_association_and_no_mapping(self):
        """With no association and no run-scoped mapping, falls back to unmapped_."""
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId="unregistered-agent-xyz",
                        toolName="bash")
        dest = tools.resolve_destination(ctx)
        self.assertIn("unmapped_", str(dest))

    def test_unmapped_fallback_includes_agent_id(self):
        agent_id = "solo-unregistered-agent"
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=agent_id, toolName="bash")
        dest = tools.resolve_destination(ctx)
        self.assertIn(agent_id, str(dest))

    def test_unmapped_fallback_uses_ctx_effective_run_id(self):
        """The unmapped folder is under the currently-resolved run, not a different one."""
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId="unregistered-agent-xyz",
                        toolName="bash", run_id=_RUN_ID)
        dest = tools.resolve_destination(ctx)
        self.assertIn(_RUN_ID, str(dest))


# ---------------------------------------------------------------------------
# Association disagreement case
# ---------------------------------------------------------------------------

class TestResolveDestinationAssociationPrecedence(unittest.TestCase):
    """When the association and ctx.run_id disagree, the association is authoritative."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_association_beats_ctx_run_id(self):
        """Association run_id is authoritative over ctx.run_id for agent-scoped events."""
        association_run_id = _RUN_ID
        ctx_run_id = _RUN_ID_ALT  # different run than the association
        _register_association(self.tmp_path, _AGENT_ID, association_run_id)
        _register_mapping(self.tmp_path, association_run_id, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash",
                        run_id=ctx_run_id)
        dest = tools.resolve_destination(ctx)
        # Destination must use the association's run_id, not ctx.run_id
        expected = ctx.paths.invocation_events(association_run_id, _INSTANCE_ID)
        self.assertEqual(
            expected, dest,
            f"Expected destination under association run_id {association_run_id!r}, "
            f"but got {dest}",
        )
        # Must NOT be under ctx_run_id
        wrong_dest = ctx.paths.invocation_events(ctx_run_id, _INSTANCE_ID)
        self.assertNotEqual(wrong_dest, dest)

    def test_destination_not_under_wrong_run_when_association_present(self):
        """Destination must not use ctx.run_id when the association gives a different one."""
        _register_association(self.tmp_path, _AGENT_ID, _RUN_ID)
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash",
                        run_id=_RUN_ID_ALT)
        dest = tools.resolve_destination(ctx)
        self.assertNotIn(_RUN_ID_ALT, str(dest),
                         "Destination must not include the stale ctx.run_id when "
                         "the association provides the authoritative run_id")


if __name__ == "__main__":
    unittest.main()
