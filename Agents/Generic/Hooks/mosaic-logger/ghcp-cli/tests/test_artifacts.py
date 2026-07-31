"""Tests for markdown artifact rendering in mosaic_logger_artifacts (ghcp-cli variant).

Key differences from the claude-code variant:
- HookContext.__init__ takes event name as first argument (from sys.argv[1])
- Payload fields are camelCase; ctx.agent_type comes from 'agentType'
- HARNESS constant is 'ghcp-cli'

Covers:
  render_input: heading, metadata table rows, prompt section, None-row omission
  render_output: heading, metadata table with status_code and facts fields
  write_artifact: file creation via core.atomic_replace_text, parent dir creation
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
              event: str = "subagentStart",
              session_id: str = "sess-art-001",
              agent_type: str = "worker",
              run_id: str = _RUN_ID) -> core.HookContext:
    payload = {
        "sessionId": session_id,
        "agentType": agent_type,
    }
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(event, payload, tmp_path, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _empty_facts():
    return types.SimpleNamespace(model=None, token_usage=None)


def _facts_with_data():
    return types.SimpleNamespace(
        model="claude-opus-4-5",
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

    def test_contains_harness_ghcp_cli(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_input(ctx, _INSTANCE_ID, "prompt text")
        self.assertIn("ghcp-cli", result)

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
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", _empty_facts())
        self.assertIsInstance(result, str)

    def test_heading_contains_agent_instance_id(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", _empty_facts())
        self.assertIn(_INSTANCE_ID, result)

    def test_status_code_row_included_when_present(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", _empty_facts())
        self.assertIn("SUCCESS", result)

    def test_status_code_row_excluded_when_none(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", None, _empty_facts())
        self.assertNotIn("| Status Code |", result)

    def test_response_section_included_when_present(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Agent response here.",
                                         "SUCCESS", _empty_facts())
        self.assertIn("Agent response here.", result)

    def test_response_section_excluded_when_none(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, None, "SUCCESS", _empty_facts())
        self.assertNotIn("## Response", result)

    def test_model_row_included_when_facts_have_model(self):
        ctx = _make_ctx(self.tmp_path)
        facts = _facts_with_data()
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", facts)
        self.assertIn("claude-opus-4-5", result)

    def test_token_count_rows_included_when_usage_present(self):
        ctx = _make_ctx(self.tmp_path)
        facts = _facts_with_data()
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", facts)
        self.assertIn("100", result)
        self.assertIn("50", result)

    def test_harness_ghcp_cli_included(self):
        ctx = _make_ctx(self.tmp_path)
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", _empty_facts())
        self.assertIn("ghcp-cli", result)

    def test_performs_no_io(self):
        ctx = _make_ctx(self.tmp_path)
        files_before = list(self.tmp_path.rglob("*"))
        artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", _empty_facts())
        files_after = list(self.tmp_path.rglob("*"))
        self.assertEqual(len(files_before), len(files_after))

    def test_session_id_included(self):
        ctx = _make_ctx(self.tmp_path, session_id="output-sess-456")
        result = artifacts.render_output(ctx, _INSTANCE_ID, "Done.", "SUCCESS", _empty_facts())
        self.assertIn("output-sess-456", result)


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
