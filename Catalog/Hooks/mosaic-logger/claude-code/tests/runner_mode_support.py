"""Shared fixtures for the Runner-mode adapter tests.

Provides an isolated workspace plus environment control (MOSAIC_ROLE,
MOSAIC_RUN_ID, MOSAIC_AGENT_INSTANCE_ID), a payload dispatcher, and readers
for the resulting OrchestrationLogs tree.  Every environment variable touched
is restored on exit so tests never leak state into each other.
"""

import contextlib
import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core

RUN_ID = "20261003T185044Z-07e9"
OTHER_RUN_ID = "20261001T101010Z-aaaa"
SESSION_ID = "runner-session-001"
INSTANCE_ID = "Research#3"

_MANAGED_ENV = ("MOSAIC_ROLE", "MOSAIC_RUN_ID", "MOSAIC_AGENT_INSTANCE_ID",
                "CLAUDE_PROJECT_DIR")

# Event types a native Claude Code invocation folder contains.
NATIVE_INVOCATION_EVENT_TYPES = frozenset({
    "invocation_start", "invocation_end", "turn",
    "tool_call_start", "tool_call_end", "usage_record",
})


@contextlib.contextmanager
def runner_env(workspace, role=None, run_id=None, instance_id=None):
    """Set the Runner environment for the duration of the block.

    A None argument means the variable is absent (not empty).
    """
    saved = {k: os.environ.get(k) for k in _MANAGED_ENV}
    for key in _MANAGED_ENV:
        os.environ.pop(key, None)
    os.environ["CLAUDE_PROJECT_DIR"] = str(workspace)
    if role is not None:
        os.environ["MOSAIC_ROLE"] = role
    if run_id is not None:
        os.environ["MOSAIC_RUN_ID"] = run_id
    if instance_id is not None:
        os.environ["MOSAIC_AGENT_INSTANCE_ID"] = instance_id
    try:
        yield
    finally:
        for key, value in saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value


def fire(workspace, event, **fields):
    """Dispatch one hook firing (default session id unless overridden)."""
    payload = {"hook_event_name": event,
               "session_id": fields.pop("session_id", SESSION_ID),
               "cwd": str(workspace)}
    payload.update(fields)
    mosaic_logger.dispatch(json.dumps(payload))


def read_jsonl(path):
    path = pathlib.Path(path)
    if not path.exists():
        return []
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines()
            if ln.strip()]


def write_transcript(directory, records, name="transcript.jsonl"):
    path = pathlib.Path(directory) / name
    path.write_text("\n".join(json.dumps(r) for r in records) + "\n", "utf-8")
    return str(path)


def assistant_record(text, model="claude-opus-4-5", input_tokens=120,
                     output_tokens=40, message_id="msg_1"):
    return {"type": "assistant", "message": {
        "id": message_id, "model": model, "role": "assistant",
        "content": [{"type": "text", "text": text}],
        "usage": {"input_tokens": input_tokens,
                  "output_tokens": output_tokens}}}


def user_record(text):
    return {"type": "user", "message": {
        "role": "user", "content": [{"type": "text", "text": text}]}}


class WorkspaceTestCase(unittest.TestCase):
    """Temp workspace with convenience accessors onto the log tree."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.workspace = pathlib.Path(self._tmp.name)
        self.paths = core.build_paths(self.workspace)

    def orchestrator_events(self, run_id=RUN_ID):
        return read_jsonl(self.paths.orchestrator_events(run_id))

    def invocation_events(self, instance_id=INSTANCE_ID, run_id=RUN_ID):
        return read_jsonl(self.paths.invocation_events(run_id, instance_id))

    def run_folders(self):
        root = self.paths.root
        if not root.exists():
            return []
        return sorted(p.name for p in root.iterdir()
                      if p.is_dir() and not p.name.startswith("."))
