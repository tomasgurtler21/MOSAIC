"""Tests for run_id extraction from preToolUse tool arguments (ghcp-cli variant).

The GHCP CLI orchestrator mints its run_id when it first writes to an
Orchestration-{run_id} path.  That write appears as a preToolUse payload whose
toolArgs carry the path.  These tests verify the allowlist-based extraction and
the side effects of the capture function.

Covers:
- RUN_ID_TOOL_ARG_FIELDS constant: exists, is a tuple, contains path-bearing
  field names, excludes content-bearing field names
- extract_run_id_from_tool_args: pure function over a dict; allowlist discrimination;
  non-dict input returns None; non-string field value is skipped; first matching
  field wins; returns None (not empty string) on no match; never raises
- capture_run_id_from_tool_args: reads ctx.field("toolArgs"), delegates to
  extract_run_id_from_tool_args, on a hit persists to session cache and assigns
  ctx.run_id when currently unset; on a miss performs no write and leaves
  ctx.run_id untouched; never raises
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


_RUN_ID = "20260804T150537Z-796a"
_SESSION_ID = "ghcp-sess-tool-args-test"
_TS = "2026-08-04T15:05:37.000Z"
_ORCHESTRATION_PATH = f"C:/AI/MOSAIC/MOSAIC/Orchestration-{_RUN_ID}/Stage-1/Plan.md"
_UNRELATED_PATH = "/workspace/src/main.py"


def _make_ctx(tmp_path, tool_args=None, session_id=_SESSION_ID,
              run_id_preset=None):
    """Build a preToolUse HookContext with optional tool_args and preset run_id."""
    payload = {"sessionId": session_id}
    if tool_args is not None:
        payload["toolArgs"] = tool_args
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext("preToolUse", payload, tmp_path, paths, _TS)
    ctx.run_id = run_id_preset
    return ctx


class TestRUN_ID_TOOL_ARG_FIELDS(unittest.TestCase):
    """RUN_ID_TOOL_ARG_FIELDS allowlist constant must be defined and correct."""

    def test_constant_exists(self):
        self.assertTrue(hasattr(runstate, "RUN_ID_TOOL_ARG_FIELDS"))

    def test_constant_is_a_tuple(self):
        self.assertIsInstance(runstate.RUN_ID_TOOL_ARG_FIELDS, tuple)

    def test_file_path_field_in_allowlist(self):
        self.assertIn("filePath", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_file_path_snake_case_in_allowlist(self):
        self.assertIn("file_path", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_path_field_in_allowlist(self):
        self.assertIn("path", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_target_file_field_in_allowlist(self):
        self.assertIn("targetFile", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_dir_path_field_in_allowlist(self):
        self.assertIn("dirPath", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_cwd_field_in_allowlist(self):
        self.assertIn("cwd", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_content_field_not_in_allowlist(self):
        self.assertNotIn("content", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_new_string_field_not_in_allowlist(self):
        self.assertNotIn("newString", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_old_string_field_not_in_allowlist(self):
        self.assertNotIn("oldString", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_prompt_field_not_in_allowlist(self):
        self.assertNotIn("prompt", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_text_field_not_in_allowlist(self):
        self.assertNotIn("text", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_body_field_not_in_allowlist(self):
        self.assertNotIn("body", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_instructions_field_not_in_allowlist(self):
        self.assertNotIn("instructions", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_command_field_not_in_allowlist(self):
        self.assertNotIn("command", runstate.RUN_ID_TOOL_ARG_FIELDS)

    def test_query_field_not_in_allowlist(self):
        self.assertNotIn("query", runstate.RUN_ID_TOOL_ARG_FIELDS)


class TestExtractRunIdFromToolArgs(unittest.TestCase):
    """extract_run_id_from_tool_args: pure function, allowlist-based extraction."""

    def _extract(self, tool_args):
        return runstate.extract_run_id_from_tool_args(tool_args)

    def test_returns_none_for_none(self):
        self.assertIsNone(self._extract(None))

    def test_returns_none_for_string(self):
        self.assertIsNone(self._extract("some string"))

    def test_returns_none_for_list(self):
        self.assertIsNone(
            self._extract([f"/path/Orchestration-{_RUN_ID}/file"])
        )

    def test_returns_none_for_integer(self):
        self.assertIsNone(self._extract(42))

    def test_returns_none_for_empty_dict(self):
        self.assertIsNone(self._extract({}))

    def test_extracts_run_id_from_file_path_field(self):
        result = self._extract({"filePath": _ORCHESTRATION_PATH})
        self.assertEqual(_RUN_ID, result)

    def test_extracts_run_id_from_path_field(self):
        result = self._extract({"path": f"/workspace/Orchestration-{_RUN_ID}/artifact.md"})
        self.assertEqual(_RUN_ID, result)

    def test_extracts_run_id_from_target_file_field(self):
        result = self._extract({"targetFile": f"/ws/Orchestration-{_RUN_ID}/out.md"})
        self.assertEqual(_RUN_ID, result)

    def test_extracts_run_id_from_cwd_field(self):
        result = self._extract({"cwd": f"/workspace/Orchestration-{_RUN_ID}"})
        self.assertEqual(_RUN_ID, result)

    def test_extracts_run_id_from_dir_path_field(self):
        result = self._extract({"dirPath": f"/workspace/Orchestration-{_RUN_ID}/"})
        self.assertEqual(_RUN_ID, result)

    def test_does_not_extract_from_content_field(self):
        """A run_id string in 'content' must not be captured (allowlist enforcement)."""
        result = self._extract({"content": f"Run ID was {_RUN_ID}"})
        self.assertIsNone(result)

    def test_does_not_extract_from_new_string_field(self):
        result = self._extract({"newString": f"run_id: {_RUN_ID}"})
        self.assertIsNone(result)

    def test_does_not_extract_from_old_string_field(self):
        result = self._extract({"oldString": f"run_id: {_RUN_ID}"})
        self.assertIsNone(result)

    def test_does_not_extract_from_prompt_field(self):
        result = self._extract({"prompt": f"run_id: {_RUN_ID}"})
        self.assertIsNone(result)

    def test_does_not_extract_from_command_field(self):
        result = self._extract({"command": f"cat Orchestration-{_RUN_ID}/file.md"})
        self.assertIsNone(result)

    def test_run_id_in_content_with_path_in_file_path_uses_path(self):
        """Only the allowed field is checked; content is skipped even if non-empty."""
        result = self._extract({
            "filePath": _ORCHESTRATION_PATH,
            "content": "This file body mentions some other run",
        })
        self.assertEqual(_RUN_ID, result)

    def test_returns_none_when_no_allowed_field_contains_run_id(self):
        result = self._extract({"filePath": _UNRELATED_PATH})
        self.assertIsNone(result)

    def test_returns_none_when_allowed_field_value_is_not_a_string(self):
        result = self._extract({"filePath": 42})
        self.assertIsNone(result)

    def test_returns_none_when_allowed_field_value_is_none(self):
        result = self._extract({"filePath": None})
        self.assertIsNone(result)

    def test_returns_none_not_empty_string(self):
        result = self._extract({})
        self.assertIsNot("", result)
        self.assertIsNone(result)

    def test_never_raises_for_none(self):
        try:
            runstate.extract_run_id_from_tool_args(None)
        except Exception as exc:
            self.fail(f"Raised for None: {exc}")

    def test_never_raises_for_non_dict(self):
        for bad in ["string", 42, [], (1, 2)]:
            try:
                runstate.extract_run_id_from_tool_args(bad)
            except Exception as exc:
                self.fail(f"Raised for {bad!r}: {exc}")

    def test_never_raises_for_dict_with_non_string_values(self):
        try:
            runstate.extract_run_id_from_tool_args({"filePath": object()})
        except Exception as exc:
            self.fail(f"Raised for non-string field value: {exc}")

    def test_first_allowlisted_field_wins_when_multiple_present_with_different_run_ids(self):
        """When two allowlisted fields are both present with *different* run_ids,
        the field that appears first in RUN_ID_TOOL_ARG_FIELDS wins.
        'filePath' precedes 'path' in the tuple, so filePath's run_id must win.
        """
        run_id_from_file_path = "20260101T000000Z-aaaa"
        run_id_from_path = "20260101T000000Z-bbbb"
        result = self._extract({
            "filePath": f"/workspace/Orchestration-{run_id_from_file_path}/artifact.md",
            "path": f"/workspace/Orchestration-{run_id_from_path}/other.md",
        })
        self.assertEqual(run_id_from_file_path, result)


class TestCaptureRunIdFromToolArgs(unittest.TestCase):
    """capture_run_id_from_tool_args integrates extraction, cache write, and ctx assignment."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_function_exists_and_is_callable(self):
        self.assertTrue(hasattr(tools, "capture_run_id_from_tool_args"))
        self.assertTrue(callable(tools.capture_run_id_from_tool_args))

    def test_returns_run_id_when_tool_args_contain_orchestration_path(self):
        ctx = _make_ctx(self.tmp_path,
                        tool_args={"filePath": _ORCHESTRATION_PATH})
        result = tools.capture_run_id_from_tool_args(ctx)
        self.assertEqual(_RUN_ID, result)

    def test_assigns_ctx_run_id_when_currently_unset(self):
        """When ctx.run_id is None and a run_id is found, ctx.run_id is set."""
        ctx = _make_ctx(self.tmp_path,
                        tool_args={"filePath": _ORCHESTRATION_PATH},
                        run_id_preset=None)
        tools.capture_run_id_from_tool_args(ctx)
        self.assertEqual(_RUN_ID, ctx.run_id)

    def test_does_not_overwrite_already_set_ctx_run_id(self):
        """When ctx.run_id is already set, it must not be replaced."""
        existing = "20260101T000000Z-aaaa"
        ctx = _make_ctx(self.tmp_path,
                        tool_args={"filePath": _ORCHESTRATION_PATH},
                        run_id_preset=existing)
        tools.capture_run_id_from_tool_args(ctx)
        self.assertEqual(existing, ctx.run_id)

    def test_persists_run_id_to_session_cache(self):
        """A successful capture writes the run_id to the session run-id cache."""
        ctx = _make_ctx(self.tmp_path,
                        tool_args={"filePath": _ORCHESTRATION_PATH},
                        run_id_preset=None)
        tools.capture_run_id_from_tool_args(ctx)
        cached = runstate.get_session_run_id(ctx.paths, _SESSION_ID)
        self.assertEqual(_RUN_ID, cached)

    def test_returns_none_when_no_run_id_in_tool_args(self):
        ctx = _make_ctx(self.tmp_path,
                        tool_args={"filePath": _UNRELATED_PATH})
        result = tools.capture_run_id_from_tool_args(ctx)
        self.assertIsNone(result)

    def test_does_not_write_cache_when_no_run_id_found(self):
        ctx = _make_ctx(self.tmp_path,
                        tool_args={"filePath": _UNRELATED_PATH})
        tools.capture_run_id_from_tool_args(ctx)
        cached = runstate.get_session_run_id(ctx.paths, _SESSION_ID)
        self.assertIsNone(cached)

    def test_leaves_ctx_run_id_untouched_when_no_run_id_found(self):
        ctx = _make_ctx(self.tmp_path,
                        tool_args={"filePath": _UNRELATED_PATH},
                        run_id_preset=None)
        tools.capture_run_id_from_tool_args(ctx)
        self.assertIsNone(ctx.run_id)

    def test_content_field_run_id_does_not_populate_cache(self):
        """A run_id string in a content field must not reach the cache."""
        ctx = _make_ctx(self.tmp_path,
                        tool_args={"content": f"run_id: {_RUN_ID}"})
        tools.capture_run_id_from_tool_args(ctx)
        cached = runstate.get_session_run_id(ctx.paths, _SESSION_ID)
        self.assertIsNone(cached)

    def test_never_raises_when_tool_args_absent(self):
        ctx = _make_ctx(self.tmp_path, tool_args=None)
        try:
            tools.capture_run_id_from_tool_args(ctx)
        except Exception as exc:
            self.fail(f"capture_run_id_from_tool_args raised with absent toolArgs: {exc}")

    def test_never_raises_when_tool_args_not_a_dict(self):
        ctx = _make_ctx(self.tmp_path, tool_args="not a dict")
        try:
            tools.capture_run_id_from_tool_args(ctx)
        except Exception as exc:
            self.fail(f"capture_run_id_from_tool_args raised with non-dict toolArgs: {exc}")

    def test_never_raises_when_session_id_absent(self):
        """Capture must not raise even if ctx.session_id is None."""
        payload = {"toolArgs": {"filePath": _ORCHESTRATION_PATH}}
        paths = core.build_paths(self.tmp_path)
        ctx = core.HookContext("preToolUse", payload, self.tmp_path, paths, _TS)
        ctx.run_id = None
        # session_id should be None because sessionId not in payload
        self.assertIsNone(ctx.session_id)
        try:
            tools.capture_run_id_from_tool_args(ctx)
        except Exception as exc:
            self.fail(f"capture_run_id_from_tool_args raised with None session_id: {exc}")


if __name__ == "__main__":
    unittest.main()
