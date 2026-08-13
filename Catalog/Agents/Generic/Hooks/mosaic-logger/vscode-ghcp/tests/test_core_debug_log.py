"""Tests for debug_log() in mosaic_logger_core (vscode-ghcp variant).

The project's error-handling contract mandates zero bytes to stdout on every
code path and treats this as non-negotiable.  debug_log() is the only sanctioned
diagnostic channel; it must:
  - write only to the file named by the MOSAIC_LOGGER_DEBUG environment variable,
  - write nothing to stdout or stderr under any circumstance,
  - never raise, regardless of whether the env var is set or the target is writable,
  - incorporate an optional exception argument into the logged text when provided.
"""

import contextlib
import io
import os
import pathlib
import sys
import tempfile
import unittest
import unittest.mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core


class TestDebugLogFileWrite(unittest.TestCase):
    """debug_log writes to the file named by MOSAIC_LOGGER_DEBUG."""

    def test_writes_message_to_file_named_by_env_var(self):
        with tempfile.TemporaryDirectory() as tmp:
            log_path = str(pathlib.Path(tmp) / "debug.log")
            with unittest.mock.patch.dict("os.environ",
                                          {core.DEBUG_ENV_VAR: log_path}):
                core.debug_log("hello debug")
            content = pathlib.Path(log_path).read_text(encoding="utf-8")
            self.assertIn("hello debug", content)

    def test_message_with_exception_arg_incorporates_exception_text(self):
        with tempfile.TemporaryDirectory() as tmp:
            log_path = str(pathlib.Path(tmp) / "debug.log")
            exc = ValueError("something went wrong")
            with unittest.mock.patch.dict("os.environ",
                                          {core.DEBUG_ENV_VAR: log_path}):
                core.debug_log("error occurred", exc=exc)
            content = pathlib.Path(log_path).read_text(encoding="utf-8")
            self.assertIn("something went wrong", content)

    def test_multiple_calls_append_rather_than_overwrite(self):
        with tempfile.TemporaryDirectory() as tmp:
            log_path = str(pathlib.Path(tmp) / "debug.log")
            with unittest.mock.patch.dict("os.environ",
                                          {core.DEBUG_ENV_VAR: log_path}):
                core.debug_log("first message")
                core.debug_log("second message")
            content = pathlib.Path(log_path).read_text(encoding="utf-8")
            self.assertIn("first message", content)
            self.assertIn("second message", content)

    def test_does_nothing_when_env_var_absent(self):
        """No file must be created when MOSAIC_LOGGER_DEBUG is not set."""
        cleaned_env = {k: v for k, v in os.environ.items()
                       if k != core.DEBUG_ENV_VAR}
        with tempfile.TemporaryDirectory() as tmp:
            with unittest.mock.patch.dict("os.environ", cleaned_env, clear=True):
                core.debug_log("should go nowhere")
            # Confirm no unexpected file was written anywhere in tmp
            entries = list(pathlib.Path(tmp).iterdir())
            self.assertEqual([], entries)


class TestDebugLogStdoutStderrSilence(unittest.TestCase):
    """debug_log writes nothing to stdout or stderr under any circumstance.

    The zero-stdout contract is non-negotiable: VS Code interprets any byte on
    stdout as a hook-output control object and would alter the user session.
    """

    def test_stdout_is_empty_when_env_var_absent(self):
        cleaned_env = {k: v for k, v in os.environ.items()
                       if k != core.DEBUG_ENV_VAR}
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            with unittest.mock.patch.dict("os.environ", cleaned_env, clear=True):
                core.debug_log("silent message")
        self.assertEqual("", buf.getvalue())

    def test_stderr_is_empty_when_env_var_absent(self):
        cleaned_env = {k: v for k, v in os.environ.items()
                       if k != core.DEBUG_ENV_VAR}
        buf = io.StringIO()
        with contextlib.redirect_stderr(buf):
            with unittest.mock.patch.dict("os.environ", cleaned_env, clear=True):
                core.debug_log("silent message")
        self.assertEqual("", buf.getvalue())

    def test_stdout_is_empty_when_env_var_set(self):
        """Nothing must reach stdout even when the env var is set and a file is written."""
        with tempfile.TemporaryDirectory() as tmp:
            log_path = str(pathlib.Path(tmp) / "debug.log")
            buf = io.StringIO()
            with contextlib.redirect_stdout(buf):
                with unittest.mock.patch.dict("os.environ",
                                              {core.DEBUG_ENV_VAR: log_path}):
                    core.debug_log("logged to file only")
            self.assertEqual("", buf.getvalue())

    def test_stderr_is_empty_when_env_var_set(self):
        with tempfile.TemporaryDirectory() as tmp:
            log_path = str(pathlib.Path(tmp) / "debug.log")
            buf = io.StringIO()
            with contextlib.redirect_stderr(buf):
                with unittest.mock.patch.dict("os.environ",
                                              {core.DEBUG_ENV_VAR: log_path}):
                    core.debug_log("logged to file only")
            self.assertEqual("", buf.getvalue())

    def test_stdout_is_empty_when_exception_arg_provided(self):
        with tempfile.TemporaryDirectory() as tmp:
            log_path = str(pathlib.Path(tmp) / "debug.log")
            buf = io.StringIO()
            with contextlib.redirect_stdout(buf):
                with unittest.mock.patch.dict("os.environ",
                                              {core.DEBUG_ENV_VAR: log_path}):
                    core.debug_log("error info", exc=RuntimeError("boom"))
            self.assertEqual("", buf.getvalue())


class TestDebugLogNeverRaises(unittest.TestCase):
    """debug_log must never propagate an exception, matching the total-functions contract."""

    def test_never_raises_when_env_var_absent(self):
        cleaned_env = {k: v for k, v in os.environ.items()
                       if k != core.DEBUG_ENV_VAR}
        try:
            with unittest.mock.patch.dict("os.environ", cleaned_env, clear=True):
                core.debug_log("no destination configured")
        except Exception as exc:
            self.fail(f"debug_log raised when env var absent: {exc}")

    def test_never_raises_when_target_path_is_unwritable(self):
        """A path that cannot be written to (file-as-parent-dir) must degrade silently."""
        with tempfile.TemporaryDirectory() as tmp:
            blocker = pathlib.Path(tmp) / "not_a_dir"
            blocker.write_bytes(b"")
            bad_path = str(blocker / "debug.log")
            try:
                with unittest.mock.patch.dict("os.environ",
                                              {core.DEBUG_ENV_VAR: bad_path}):
                    core.debug_log("should not raise")
            except Exception as exc:
                self.fail(f"debug_log raised on unwritable target: {exc}")

    def test_never_raises_with_exception_arg_and_env_var_absent(self):
        cleaned_env = {k: v for k, v in os.environ.items()
                       if k != core.DEBUG_ENV_VAR}
        try:
            with unittest.mock.patch.dict("os.environ", cleaned_env, clear=True):
                core.debug_log("context", exc=ValueError("exc arg provided"))
        except Exception as exc:
            self.fail(f"debug_log raised with exc arg when env var absent: {exc}")

    def test_never_raises_with_exception_arg_and_unwritable_target(self):
        with tempfile.TemporaryDirectory() as tmp:
            blocker = pathlib.Path(tmp) / "not_a_dir"
            blocker.write_bytes(b"")
            bad_path = str(blocker / "debug.log")
            try:
                with unittest.mock.patch.dict("os.environ",
                                              {core.DEBUG_ENV_VAR: bad_path}):
                    core.debug_log("context", exc=OSError("write failed"))
            except Exception as exc:
                self.fail(f"debug_log raised with exc arg on unwritable target: {exc}")


if __name__ == "__main__":
    unittest.main()
