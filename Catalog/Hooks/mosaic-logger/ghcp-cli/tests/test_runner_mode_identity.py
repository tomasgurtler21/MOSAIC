"""Mode and run-identity resolution for the ghcp-cli adapter Runner mode.

Covers:
  read_runner_mode: native (None) when MOSAIC_ROLE is unset or empty, role and
    instance-id handling, run-id validity, agent_type derivation, fallbacks for
    unrecognised roles and a subagent without an instance id
  Run identity: in Runner mode MOSAIC_RUN_ID beats prompt extraction, tool-arg
    capture and the workspace session cache, and nothing is written back to the
    session cache; an invalid MOSAIC_RUN_ID falls back to the native order
  Native mode keeps prompt extraction and the session cache behaviour
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core
import mosaic_logger_runstate as runstate

from tests.runner_mode_support import (
    CACHED_RUN_ID, INSTANCE_ID, OTHER_RUN_ID, RUN_ID, SESSION_ID,
    LazyModule, RunnerModeTestCase,
)

runner_mode = LazyModule("mosaic_logger_runner_mode")


class TestReadRunnerMode(unittest.TestCase):

    def test_native_when_role_absent(self):
        self.assertIsNone(runner_mode.read_runner_mode({"MOSAIC_RUN_ID": RUN_ID}))

    def test_native_when_role_empty(self):
        self.assertIsNone(runner_mode.read_runner_mode({"MOSAIC_ROLE": ""}))

    def test_subagent_role_carries_instance_id_and_run_id(self):
        mode = runner_mode.read_runner_mode({
            "MOSAIC_ROLE": "subagent", "MOSAIC_RUN_ID": RUN_ID,
            "MOSAIC_AGENT_INSTANCE_ID": INSTANCE_ID})
        self.assertEqual("subagent", mode.role)
        self.assertEqual(RUN_ID, mode.run_id)
        self.assertEqual(INSTANCE_ID, mode.agent_instance_id)

    def test_agent_type_is_part_before_last_hash(self):
        mode = runner_mode.read_runner_mode({
            "MOSAIC_ROLE": "subagent", "MOSAIC_RUN_ID": RUN_ID,
            "MOSAIC_AGENT_INSTANCE_ID": "Test#Writer#12"})
        self.assertEqual("Test#Writer", mode.agent_type)

    def test_agent_type_omitted_when_nothing_before_hash(self):
        mode = runner_mode.read_runner_mode({
            "MOSAIC_ROLE": "subagent", "MOSAIC_RUN_ID": RUN_ID,
            "MOSAIC_AGENT_INSTANCE_ID": "#3"})
        self.assertIsNone(mode.agent_type)

    def test_orchestrator_role_has_no_instance_id_or_agent_type(self):
        mode = runner_mode.read_runner_mode({
            "MOSAIC_ROLE": "orchestrator", "MOSAIC_RUN_ID": RUN_ID,
            "MOSAIC_AGENT_INSTANCE_ID": INSTANCE_ID})
        self.assertEqual("orchestrator", mode.role)
        self.assertIsNone(mode.agent_instance_id)
        self.assertIsNone(mode.agent_type)

    def test_unrecognised_role_is_treated_as_orchestrator(self):
        mode = runner_mode.read_runner_mode({"MOSAIC_ROLE": "wizard", "MOSAIC_RUN_ID": RUN_ID})
        self.assertEqual("orchestrator", mode.role)

    def test_subagent_without_instance_id_is_treated_as_orchestrator(self):
        for env in ({"MOSAIC_ROLE": "subagent", "MOSAIC_RUN_ID": RUN_ID},
                    {"MOSAIC_ROLE": "subagent", "MOSAIC_RUN_ID": RUN_ID,
                     "MOSAIC_AGENT_INSTANCE_ID": ""}):
            with self.subTest(env=env):
                mode = runner_mode.read_runner_mode(env)
                self.assertEqual("orchestrator", mode.role)
                self.assertIsNone(mode.agent_instance_id)

    def test_invalid_run_ids_resolve_to_none(self):
        invalid = ["", "unknown-run", "../x", "20261003T185044Z-07E9",
                   "20261003T185044Z-07e9/x", " 20261003T185044Z-07e9"]
        for value in invalid:
            with self.subTest(run_id=value):
                mode = runner_mode.read_runner_mode({
                    "MOSAIC_ROLE": "orchestrator", "MOSAIC_RUN_ID": value})
                self.assertIsNotNone(mode)
                self.assertIsNone(mode.run_id)

    def test_missing_run_id_resolves_to_none_but_role_is_kept(self):
        mode = runner_mode.read_runner_mode({
            "MOSAIC_ROLE": "subagent", "MOSAIC_AGENT_INSTANCE_ID": INSTANCE_ID})
        self.assertEqual("subagent", mode.role)
        self.assertIsNone(mode.run_id)

    def test_defaults_to_process_environment(self):
        old = {k: os.environ.get(k) for k in ("MOSAIC_ROLE", "MOSAIC_RUN_ID")}
        try:
            os.environ["MOSAIC_ROLE"] = "orchestrator"
            os.environ["MOSAIC_RUN_ID"] = RUN_ID
            mode = runner_mode.read_runner_mode()
            self.assertEqual(RUN_ID, mode.run_id)
        finally:
            for key, value in old.items():
                if value is None:
                    os.environ.pop(key, None)
                else:
                    os.environ[key] = value

    def test_never_raises_on_odd_mapping(self):
        try:
            runner_mode.read_runner_mode({"MOSAIC_ROLE": "subagent", "MOSAIC_RUN_ID": None})
        except Exception as exc:  # noqa: BLE001
            self.fail(f"read_runner_mode raised: {exc}")


class TestRunnerModeRunIdentity(RunnerModeTestCase):

    def _ctx(self, event, **payload):
        body = {"sessionId": SESSION_ID, **payload}
        return core.HookContext(event, body, self.workspace, self.paths,
                                core.current_timestamp())

    def _build(self, event, **payload):
        """Context as the dispatcher builds it (reads the environment)."""
        return mosaic_logger.build_context(event, {"sessionId": SESSION_ID,
                                                   "cwd": str(self.workspace), **payload})

    def test_env_run_id_beats_prompt_extraction(self):
        self.subagent_env()
        ctx = self._build("userPromptSubmitted", prompt=f"run_id: {OTHER_RUN_ID}")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(RUN_ID, ctx.run_id)

    def test_env_run_id_beats_session_cache(self):
        self.orchestrator_env()
        runstate.put_session_run_id(self.paths, SESSION_ID, CACHED_RUN_ID)
        ctx = self._build("preToolUse", toolArgs={"command": "ls"})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(RUN_ID, ctx.run_id)

    def test_prompt_run_id_is_not_written_back_to_session_cache(self):
        self.orchestrator_env()
        ctx = self._build("userPromptSubmitted", prompt=f"run_id: {OTHER_RUN_ID}")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(runstate.get_session_run_id(self.paths, SESSION_ID))

    def test_env_run_id_applies_to_every_event_kind(self):
        self.orchestrator_env()
        for event in ("sessionStart", "sessionEnd", "agentStop", "postToolUse",
                      "subagentStart", "notification"):
            with self.subTest(event=event):
                ctx = self._build(event, agentPrompt=f"run_id: {OTHER_RUN_ID}",
                                  message=f"run_id: {OTHER_RUN_ID}")
                mosaic_logger.resolve_run_identity(ctx)
                self.assertEqual(RUN_ID, ctx.run_id)

    def test_tool_arg_capture_does_not_override_env_run_id(self):
        self.orchestrator_env()
        self.dispatch("sessionStart")
        self.dispatch("preToolUse", {
            "toolName": "agent", "toolUseId": "tu-1",
            "toolArgs": {"prompt": f'agent_instance_id: "Worker#1"\nrun_id: "{OTHER_RUN_ID}"\nGo.'}})
        self.assertNotIn(OTHER_RUN_ID, self.run_dir_names())
        self.assertNotIn("unknown-run", self.run_dir_names())

    def test_prompt_event_does_not_redirect_session_to_other_run(self):
        self.orchestrator_env()
        self.dispatch("userPromptSubmitted", {"prompt": f"run_id: {OTHER_RUN_ID}"})
        self.dispatch("sessionStart")
        self.assertNotIn(OTHER_RUN_ID, self.run_dir_names())
        self.assertNotIn("unknown-run", self.run_dir_names())

    def test_invalid_env_run_id_falls_back_to_prompt_extraction_and_write_back(self):
        self.set_runner_env("orchestrator", run_id="not-a-run-id")
        ctx = self._build("userPromptSubmitted", prompt=f"run_id: {OTHER_RUN_ID}")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(OTHER_RUN_ID, ctx.run_id)
        self.assertEqual(OTHER_RUN_ID, runstate.get_session_run_id(self.paths, SESSION_ID))

    def test_invalid_env_run_id_without_other_source_yields_unknown_run(self):
        self.set_runner_env("orchestrator", run_id="not-a-run-id")
        ctx = self._build("preToolUse")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual("unknown-run", core.effective_run_id(ctx))


class TestNativeModeIdentityUnchanged(RunnerModeTestCase):

    def test_context_has_no_runner_mode_without_role(self):
        os.environ["MOSAIC_RUN_ID"] = RUN_ID
        ctx = mosaic_logger.build_context("sessionStart", {"sessionId": SESSION_ID,
                                                           "cwd": str(self.workspace)})
        self.assertIsNone(ctx.runner_mode)

    def test_prompt_extraction_wins_without_role(self):
        os.environ["MOSAIC_RUN_ID"] = CACHED_RUN_ID
        ctx = mosaic_logger.build_context("userPromptSubmitted", {
            "sessionId": SESSION_ID, "cwd": str(self.workspace),
            "prompt": f"run_id: {RUN_ID}"})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(RUN_ID, ctx.run_id)

    def test_session_cache_still_supplies_run_id_without_role(self):
        runstate.put_session_run_id(self.paths, SESSION_ID, RUN_ID)
        ctx = mosaic_logger.build_context("preToolUse", {
            "sessionId": SESSION_ID, "cwd": str(self.workspace)})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(RUN_ID, ctx.run_id)

    def test_native_session_still_emits_run_start_and_run_end(self):
        self.dispatch("sessionStart")
        self.dispatch("sessionEnd", {"reason": "clear"})
        types = [e["event"] for e in self.orchestrator_events("unknown-run")]
        self.assertIn("run_start", types)
        self.assertIn("run_end", types)


if __name__ == "__main__":
    unittest.main()
