"""Tests for markdown artifact rendering in mosaic_logger_artifacts
(vscode-ghcp variant).

Key differences from claude-code and ghcp-cli variants:
- HookContext.__init__ takes (payload, workspace_root, paths, timestamp);
  event comes from payload["hook_event_name"].
- Payload fields are snake_case (session_id, agent_type, etc.).
- HARNESS constant is 'vscode-ghcp'.

Covers:
  render_input: heading (contains agent_instance_id), metadata table rows
    (Agent Instance ID, Agent Type, Session ID, Run ID, Harness, Captured At),
    prompt section, None-row omission, no I/O.
  render_output: heading, metadata table with Status Code and facts fields
    (Model, token counts), response section, None-value omission, no I/O.
    facts is duck-typed (accepts any object with .model and .token_usage).
  write_artifact: file creation via core.atomic_replace_text, parent dir
    creation, content fidelity, overwrite, returns True, never raises.
"""

import os
import pathlib
import sys
import tempfile
import types
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_artifacts as artifacts


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

_RUN_ID = "20260101T000000Z-ab12"
_INSTANCE_ID = "Research#3"
_TS = "2026-01-01T00:00:00.000Z"


def _make_ctx(tmp_path: pathlib.Path,
              session_id: str = "sess-art-001",
              agent_type: str = "worker",
              run_id: str = _RUN_ID) -> core.HookContext:
    payload = {
        "hook_event_name": "SubagentStart",
        "session_id": session_id,
        "agent_type": agent_type,
    }
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(payload, tmp_path, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _empty_facts():
    return types.SimpleNamespace(model=None, token_usage=None)


def _facts_with_data():
    return types.SimpleNamespace(
        model="gpt-4o",
        token_usage={"input_tokens": 100, "output_tokens": 50},
    )


# ---------------------------------------------------------------------------
# render_input
# ---------------------------------------------------------------------------

class TestRenderInput(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_a_string(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "Do some research.")
        self.assertIsInstance(result, str)

    def test_heading_contains_agent_instance_id(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt text")
        self.assertIn(_INSTANCE_ID, result)

    def test_heading_starts_with_hash(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt text")
        self.assertTrue(result.startswith("#"))

    def test_contains_harness_vscode_ghcp(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt text")
        self.assertIn("vscode-ghcp", result)

    def test_harness_row_is_not_claude_code(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt text")
        self.assertNotIn("claude-code", result)

    def test_contains_session_id(self):
        ctx = _make_ctx(self.tmp_path, session_id="my-session-123")
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt text")
        self.assertIn("my-session-123", result)

    def test_contains_agent_type(self):
        ctx = _make_ctx(self.tmp_path, agent_type="researcher")
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt text")
        self.assertIn("researcher", result)

    def test_prompt_section_included_when_prompt_present(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "Do the task.")
        self.assertIn("Do the task.", result)

    def test_no_prompt_section_when_prompt_is_none(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, None)
        self.assertNotIn("## Prompt", result)

    def test_run_id_row_included_when_run_id_set(self):
        ctx = _make_ctx(self.tmp_path, run_id=_RUN_ID)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt")
        self.assertIn(_RUN_ID, result)

    def test_run_id_row_excluded_when_run_id_is_none(self):
        ctx = _make_ctx(self.tmp_path, run_id=None)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt")
        self.assertNotIn("| Run ID |", result)

    def test_timestamp_included(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt")
        self.assertIn(_TS, result)

    def test_performs_no_io(self):
        """render_input performs no I/O; must not create any file."""
        ctx = _make_ctx(self.tmp_path)
        files_before = list(self.tmp_path.rglob("*"))
        artifacts.render_input(ctx, _INSTANCE_ID, "prompt")
        files_after = list(self.tmp_path.rglob("*"))
        self.assertEqual(len(files_before), len(files_after))


# ---------------------------------------------------------------------------
# render_output
# ---------------------------------------------------------------------------

class TestRenderOutput(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_a_string(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS",
                                         _empty_facts())
        self.assertIsInstance(result, str)

    def test_heading_contains_agent_instance_id(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS",
                                         _empty_facts())
        self.assertIn(_INSTANCE_ID, result)

    def test_status_code_row_included_when_present(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS",
                                         _empty_facts())
        self.assertIn("SUCCESS", result)

    def test_status_code_row_excluded_when_none(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", None,
                                         _empty_facts())
        self.assertNotIn("| Status Code |", result)

    def test_response_section_included_when_present(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Agent response here.",
                                         "SUCCESS", _empty_facts())
        self.assertIn("Agent response here.", result)

    def test_response_section_excluded_when_none(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, None, "SUCCESS",
                                         _empty_facts())
        self.assertNotIn("## Response", result)

    def test_model_row_included_when_facts_have_model(self):
        ctx = _make_ctx(self.tmp_path)
        facts = _facts_with_data()
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", facts)
        self.assertIn("gpt-4o", result)

    def test_model_row_excluded_when_facts_model_is_none(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS",
                                         _empty_facts())
        self.assertNotIn("| Model |", result)

    def test_token_count_rows_included_when_usage_present(self):
        ctx = _make_ctx(self.tmp_path)
        facts = _facts_with_data()
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", facts)
        self.assertIn("100", result)
        self.assertIn("50", result)

    def test_harness_vscode_ghcp_included(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS",
                                         _empty_facts())
        self.assertIn("vscode-ghcp", result)

    def test_session_id_included(self):
        ctx = _make_ctx(self.tmp_path, session_id="output-sess-456")
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS",
                                         _empty_facts())
        self.assertIn("output-sess-456", result)

    def test_performs_no_io(self):
        """render_output performs no I/O."""
        ctx = _make_ctx(self.tmp_path)
        files_before = list(self.tmp_path.rglob("*"))
        artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", _empty_facts())
        files_after = list(self.tmp_path.rglob("*"))
        self.assertEqual(len(files_before), len(files_after))

    def test_facts_is_duck_typed(self):
        """render_output accepts any object with .model and .token_usage (no import needed)."""
        ctx = _make_ctx(self.tmp_path)
        custom_facts = types.SimpleNamespace(
            model="custom-model",
            token_usage={"input_tokens": 50, "output_tokens": 25},
        )
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS",
                                         custom_facts)
        self.assertIn("custom-model", result)


# ---------------------------------------------------------------------------
# write_artifact
# ---------------------------------------------------------------------------

class TestWriteArtifact(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_creates_file_at_path(self):
        path = self.tmp_path / "invocation" / "01_input.md"
        artifacts.write_artifact(path, "# Content\n")
        self.assertTrue(path.exists())

    def test_file_content_matches_text(self):
        path = self.tmp_path / "01_input.md"
        text = "# Invocation Input: TestAgent#1\n\n| Field | Value |\n"
        artifacts.write_artifact(path, text)
        self.assertEqual(text, path.read_text("utf-8"))

    def test_creates_parent_directories(self):
        deep_path = self.tmp_path / "a" / "b" / "c" / "01_input.md"
        artifacts.write_artifact(deep_path, "content")
        self.assertTrue(deep_path.exists())

    def test_returns_true_on_success(self):
        path = self.tmp_path / "01_input.md"
        result = artifacts.write_artifact(path, "some content")
        self.assertTrue(result)

    def test_overwrites_existing_file(self):
        path = self.tmp_path / "01_input.md"
        path.write_text("old content", encoding="utf-8")
        artifacts.write_artifact(path, "new content")
        self.assertEqual("new content", path.read_text("utf-8"))

    def test_never_raises(self):
        path = self.tmp_path / "output.md"
        try:
            artifacts.write_artifact(path, "text")
        except Exception as exc:
            self.fail(f"write_artifact raised: {exc}")

    def test_empty_text_creates_empty_file(self):
        path = self.tmp_path / "empty.md"
        artifacts.write_artifact(path, "")
        self.assertTrue(path.exists())


if __name__ == "__main__":
    unittest.main()
