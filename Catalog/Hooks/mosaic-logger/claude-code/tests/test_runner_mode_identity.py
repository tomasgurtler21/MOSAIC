"""Tests for Runner mode selection and run identity resolution.

Mode is selected only by MOSAIC_ROLE in the environment.  With it unset the
adapter is in native mode and run identity resolution is unchanged (including
the lowest-precedence MOSAIC_RUN_ID fallback).  With it set, MOSAIC_RUN_ID is
the run identity and beats any run id found in prompt text.
"""

import importlib
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from runner_mode_support import (
    INSTANCE_ID, OTHER_RUN_ID, RUN_ID, WorkspaceTestCase, fire, runner_env,
)


def _read_mode(env):
    module = importlib.import_module("mosaic_logger_runner_mode")
    return module.read_runner_mode(env)


class TestReadRunnerMode(unittest.TestCase):
    """read_runner_mode maps the environment onto a RunnerMode (or None)."""

    def test_returns_none_when_role_absent(self):
        self.assertIsNone(_read_mode({"MOSAIC_RUN_ID": RUN_ID}))

    def test_returns_none_when_role_empty(self):
        self.assertIsNone(_read_mode({"MOSAIC_ROLE": ""}))

    def test_subagent_role_carries_run_and_instance_identity(self):
        mode = _read_mode({"MOSAIC_ROLE": "subagent", "MOSAIC_RUN_ID": RUN_ID,
                           "MOSAIC_AGENT_INSTANCE_ID": INSTANCE_ID})
        self.assertEqual("subagent", mode.role)
        self.assertEqual(RUN_ID, mode.run_id)
        self.assertEqual(INSTANCE_ID, mode.agent_instance_id)

    def test_agent_type_is_part_before_last_hash(self):
        mode = _read_mode({"MOSAIC_ROLE": "subagent",
                           "MOSAIC_AGENT_INSTANCE_ID": "Test#Writer#12"})
        self.assertEqual("Test#Writer", mode.agent_type)

    def test_agent_type_omitted_when_part_before_hash_is_empty(self):
        mode = _read_mode({"MOSAIC_ROLE": "subagent",
                           "MOSAIC_AGENT_INSTANCE_ID": "#4"})
        self.assertIsNone(mode.agent_type)

    def test_orchestrator_role_has_no_instance_identity(self):
        mode = _read_mode({"MOSAIC_ROLE": "orchestrator",
                           "MOSAIC_RUN_ID": RUN_ID,
                           "MOSAIC_AGENT_INSTANCE_ID": INSTANCE_ID})
        self.assertEqual("orchestrator", mode.role)
        self.assertIsNone(mode.agent_instance_id)
        self.assertIsNone(mode.agent_type)

    def test_unrecognised_role_is_treated_as_orchestrator(self):
        mode = _read_mode({"MOSAIC_ROLE": "banana", "MOSAIC_RUN_ID": RUN_ID})
        self.assertEqual("orchestrator", mode.role)

    def test_subagent_without_instance_id_is_treated_as_orchestrator(self):
        mode = _read_mode({"MOSAIC_ROLE": "subagent", "MOSAIC_RUN_ID": RUN_ID})
        self.assertEqual("orchestrator", mode.role)

    def test_invalid_run_id_yields_none_run_id_but_keeps_role(self):
        mode = _read_mode({"MOSAIC_ROLE": "orchestrator",
                           "MOSAIC_RUN_ID": "not-a-run-id"})
        self.assertEqual("orchestrator", mode.role)
        self.assertIsNone(mode.run_id)

    def test_missing_run_id_yields_none_run_id(self):
        mode = _read_mode({"MOSAIC_ROLE": "orchestrator"})
        self.assertIsNone(mode.run_id)


def _prompt_naming(run_id):
    return f'{{"agent_instance_id": "X#1", "run_id": "{run_id}"}}'


class TestRunIdPrecedence(WorkspaceTestCase):
    """MOSAIC_RUN_ID beats prompt text in Runner mode only."""

    def test_orchestrator_role_env_run_id_beats_prompt_text(self):
        with runner_env(self.workspace, role="orchestrator", run_id=RUN_ID):
            fire(self.workspace, "SessionStart", source="startup")
            fire(self.workspace, "UserPromptSubmit",
                 user_prompt=_prompt_naming(OTHER_RUN_ID))
        self.assertEqual([RUN_ID], self.run_folders())
        self.assertTrue(self.orchestrator_events(RUN_ID))

    def test_subagent_role_env_run_id_beats_prompt_text(self):
        with runner_env(self.workspace, role="subagent", run_id=RUN_ID,
                        instance_id=INSTANCE_ID):
            fire(self.workspace, "SessionStart", source="startup")
            fire(self.workspace, "UserPromptSubmit",
                 user_prompt=_prompt_naming(OTHER_RUN_ID))
        self.assertEqual([RUN_ID], self.run_folders())
        self.assertTrue(self.invocation_events())

    def test_native_mode_prompt_text_still_beats_env_run_id(self):
        with runner_env(self.workspace, run_id=RUN_ID):
            fire(self.workspace, "UserPromptSubmit",
                 user_prompt=_prompt_naming(OTHER_RUN_ID))
        self.assertIn(OTHER_RUN_ID, self.run_folders())

    def test_native_mode_env_run_id_is_fallback_when_prompt_has_none(self):
        with runner_env(self.workspace, run_id=RUN_ID):
            fire(self.workspace, "SessionStart", source="startup")
        names = [e["event"] for e in self.orchestrator_events(RUN_ID)]
        self.assertIn("run_start", names)

    def test_empty_role_keeps_native_behaviour(self):
        with runner_env(self.workspace, role="", run_id=RUN_ID):
            fire(self.workspace, "SessionStart", source="startup")
        names = [e["event"] for e in self.orchestrator_events(RUN_ID)]
        self.assertIn("run_start", names)

    def test_runner_mode_without_run_id_still_honours_role(self):
        with runner_env(self.workspace, role="orchestrator"):
            fire(self.workspace, "SessionStart", source="startup")
            fire(self.workspace, "SessionEnd", reason="other")
        folders = self.run_folders()
        self.assertTrue(folders)
        for folder in folders:
            names = [e["event"] for e in self.orchestrator_events(folder)]
            self.assertIn("session_start", names)
            self.assertNotIn("run_start", names)
            self.assertNotIn("run_end", names)


if __name__ == "__main__":
    unittest.main()
