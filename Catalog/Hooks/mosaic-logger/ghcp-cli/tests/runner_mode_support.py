"""Shared helpers for the Runner-mode tests of the ghcp-cli adapter.

Runner mode is selected only by environment variables (MOSAIC_ROLE,
MOSAIC_RUN_ID, MOSAIC_AGENT_INSTANCE_ID). These helpers set them for the
duration of one test, drive mosaic_logger.dispatch() against a temporary
workspace, and read back the resulting log tree.
"""

import contextlib
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core

RUN_ID = "20261003T185044Z-07e9"
OTHER_RUN_ID = "20250101T000000Z-aaaa"
CACHED_RUN_ID = "20240202T020202Z-cafe"
SESSION_ID = "ghcp-runner-session-001"
INSTANCE_ID = "Research#3"
PROMPT = "Do the research task"

FIXTURES = pathlib.Path(__file__).resolve().parent / "fixtures"
SUBAGENT_TRANSCRIPT = FIXTURES / "runner_subagent_transcript.jsonl"
FINAL_RESPONSE = '{"status_code": "SUCCESS"}\nWork complete.'
FINAL_MODEL = "ghcp-model-v1"
FINAL_TOKEN_USAGE = {"input_tokens": 120, "output_tokens": 45}

class LazyModule:
    """Imports a module on first attribute access, so a module that does not
    exist yet fails the individual tests (not collection of the whole file)."""

    def __init__(self, name: str):
        self._name = name

    def __getattr__(self, attr):
        import importlib
        return getattr(importlib.import_module(self._name), attr)


_MOSAIC_ENV_KEYS = ("MOSAIC_ROLE", "MOSAIC_RUN_ID", "MOSAIC_AGENT_INSTANCE_ID")


def read_jsonl(path: pathlib.Path) -> list:
    if not path.exists():
        return []
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


def event_types(events: list) -> list:
    return [e.get("event") for e in events]


class RunnerModeTestCase(unittest.TestCase):
    """Temp workspace plus environment control. MOSAIC_* variables start unset."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.workspace = pathlib.Path(self._tmp.name)
        self.paths = core.build_paths(self.workspace)
        env_patch = mock.patch.dict(os.environ)
        env_patch.start()
        self.addCleanup(env_patch.stop)
        for key in _MOSAIC_ENV_KEYS:
            os.environ.pop(key, None)

    def set_runner_env(self, role=None, run_id=RUN_ID, instance_id=None):
        for key, value in (("MOSAIC_ROLE", role), ("MOSAIC_RUN_ID", run_id),
                           ("MOSAIC_AGENT_INSTANCE_ID", instance_id)):
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value

    def subagent_env(self, run_id=RUN_ID, instance_id=INSTANCE_ID):
        self.set_runner_env("subagent", run_id, instance_id)

    def orchestrator_env(self, run_id=RUN_ID):
        self.set_runner_env("orchestrator", run_id, None)

    def dispatch(self, event: str, payload: dict = None, session_id=SESSION_ID) -> str:
        body = {"cwd": str(self.workspace)}
        if session_id is not None:
            body["sessionId"] = session_id
        body.update(payload or {})
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            mosaic_logger.dispatch(event, json.dumps(body))
        return buf.getvalue()

    def orchestrator_events(self, run_id=RUN_ID) -> list:
        return read_jsonl(self.paths.orchestrator_events(run_id))

    def invocation_events(self, run_id=RUN_ID, instance_id=INSTANCE_ID) -> list:
        return read_jsonl(self.paths.invocation_events(run_id, instance_id))

    def run_dir_names(self) -> set:
        logs = self.workspace / "OrchestrationLogs"
        if not logs.exists():
            return set()
        return {p.name for p in logs.iterdir()}

    def copy_transcript(self, source: pathlib.Path = SUBAGENT_TRANSCRIPT,
                        name: str = "transcript.jsonl") -> pathlib.Path:
        target = self.workspace / name
        target.write_bytes(source.read_bytes())
        return target
