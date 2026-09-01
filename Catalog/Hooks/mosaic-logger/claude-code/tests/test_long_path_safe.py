"""Tests for _long_path_safe helper and preserved error-handling contracts.

Covers:
  T4.1 - Helper function behaviour: no-op on non-Windows, prefix on Windows
          (sys.platform mocked), no double-prefix, relative paths resolved.
  T4.2 - read_last_assistant_facts and read_assistant_records still degrade
          gracefully (never raise) when the transcript path is unreadable,
          confirming error-handling contracts are preserved after the fix.
"""

import os
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_transcript as transcript
from mosaic_logger_transcript import _long_path_safe, TurnFacts


# ---------------------------------------------------------------------------
# T4.1 — _long_path_safe helper
# ---------------------------------------------------------------------------

class TestLongPathSafeNonWindows(unittest.TestCase):
    """On non-Windows platforms the helper must be a no-op."""

    def _run_as_non_windows(self, path):
        with mock.patch.object(sys, "platform", "linux"):
            return _long_path_safe(path)

    def test_short_path_returned_unchanged(self):
        result = self._run_as_non_windows("/tmp/some/path.jsonl")
        self.assertEqual(result, "/tmp/some/path.jsonl")

    def test_long_path_returned_unchanged(self):
        long_path = "/tmp/" + "a" * 300 + ".jsonl"
        result = self._run_as_non_windows(long_path)
        self.assertEqual(result, long_path)

    def test_already_prefixed_path_returned_unchanged(self):
        # Prefix is meaningless on non-Windows but the function must not strip it
        prefixed = "\\\\?\\C:\\some\\path.jsonl"
        result = self._run_as_non_windows(prefixed)
        self.assertEqual(result, prefixed)

    def test_relative_path_returned_unchanged(self):
        result = self._run_as_non_windows("relative/path.jsonl")
        self.assertEqual(result, "relative/path.jsonl")


class TestLongPathSafeWindows(unittest.TestCase):
    """On Windows the helper must apply the \\?\\ prefix."""

    def _run_as_windows(self, path):
        with mock.patch.object(sys, "platform", "win32"):
            return _long_path_safe(path)

    def test_absolute_path_gets_prefix(self):
        path = "C:\\Users\\test\\transcript.jsonl"
        result = self._run_as_windows(path)
        self.assertTrue(result.startswith("\\\\?\\"),
                        f"Expected \\\\?\\ prefix, got: {result!r}")

    def test_prefix_contains_original_path(self):
        path = "C:\\Users\\test\\transcript.jsonl"
        result = self._run_as_windows(path)
        # After normalisation via abspath the path should still be present
        self.assertIn("transcript.jsonl", result)

    def test_no_double_prefix(self):
        already_prefixed = "\\\\?\\C:\\Users\\test\\transcript.jsonl"
        result = self._run_as_windows(already_prefixed)
        # Must not gain a second prefix
        self.assertEqual(result, already_prefixed)
        self.assertFalse(result.startswith("\\\\?\\\\\\?\\"),
                         "Double-prefix detected")

    def test_relative_path_resolved_before_prefix(self):
        # abspath turns relative into absolute; the prefix must follow that
        relative = "some\\relative\\path.jsonl"
        with mock.patch.object(sys, "platform", "win32"), \
             mock.patch("os.path.abspath", return_value="C:\\abs\\path.jsonl"):
            result = _long_path_safe(relative)
        self.assertEqual(result, "\\\\?\\C:\\abs\\path.jsonl")

    def test_forward_slash_path_normalised(self):
        # os.path.abspath on Windows converts forward slashes
        with mock.patch.object(sys, "platform", "win32"), \
             mock.patch("os.path.abspath", return_value="C:\\Users\\test\\file.jsonl"):
            result = _long_path_safe("C:/Users/test/file.jsonl")
        self.assertTrue(result.startswith("\\\\?\\"))


# ---------------------------------------------------------------------------
# T4.2 — Error-handling contracts preserved after the fix
# ---------------------------------------------------------------------------

class TestReadLastAssistantFactsContract(unittest.TestCase):
    """read_last_assistant_facts must never raise and must degrade to TurnFacts()."""

    def test_none_path_returns_empty_facts(self):
        result = transcript.read_last_assistant_facts(None)
        self.assertIsInstance(result, TurnFacts)
        self.assertIsNone(result.model)
        self.assertIsNone(result.token_usage)

    def test_empty_string_path_returns_empty_facts(self):
        result = transcript.read_last_assistant_facts("")
        self.assertIsInstance(result, TurnFacts)
        self.assertIsNone(result.model)
        self.assertIsNone(result.token_usage)

    def test_nonexistent_path_returns_empty_facts(self):
        result = transcript.read_last_assistant_facts(
            "/nonexistent/path/transcript.jsonl"
        )
        self.assertIsInstance(result, TurnFacts)
        self.assertIsNone(result.model)
        self.assertIsNone(result.token_usage)

    def test_open_raises_oserror_returns_empty_facts(self):
        with mock.patch("builtins.open", side_effect=OSError("permission denied")):
            result = transcript.read_last_assistant_facts("/some/path.jsonl")
        self.assertIsInstance(result, TurnFacts)
        self.assertIsNone(result.model)
        self.assertIsNone(result.token_usage)

    def test_open_raises_unexpected_error_returns_empty_facts(self):
        with mock.patch("builtins.open", side_effect=RuntimeError("unexpected")):
            result = transcript.read_last_assistant_facts("/some/path.jsonl")
        self.assertIsInstance(result, TurnFacts)
        self.assertIsNone(result.model)
        self.assertIsNone(result.token_usage)


class TestReadAssistantRecordsContract(unittest.TestCase):
    """read_assistant_records must never raise and must degrade to []."""

    def test_none_path_returns_empty_list(self):
        result = transcript.read_assistant_records(None)
        self.assertEqual(result, [])

    def test_empty_string_path_returns_empty_list(self):
        result = transcript.read_assistant_records("")
        self.assertEqual(result, [])

    def test_nonexistent_path_returns_empty_list(self):
        result = transcript.read_assistant_records(
            "/nonexistent/path/transcript.jsonl"
        )
        self.assertEqual(result, [])

    def test_open_raises_oserror_returns_empty_list(self):
        with mock.patch("builtins.open", side_effect=OSError("permission denied")):
            result = transcript.read_assistant_records("/some/path.jsonl")
        self.assertEqual(result, [])

    def test_open_raises_unexpected_error_returns_empty_list(self):
        with mock.patch("builtins.open", side_effect=RuntimeError("unexpected")):
            result = transcript.read_assistant_records("/some/path.jsonl")
        self.assertEqual(result, [])


if __name__ == "__main__":
    unittest.main()
