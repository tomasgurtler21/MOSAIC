"""Tests for transcript path scoping in the degradation bucket (claude-code adapter).

The orchestrator transcript inside the unknown-run/ bucket must be scoped by
harness and session so two unrelated runs or harnesses cannot overwrite the
same file. Real run folders keep their existing fixed names exactly as before.

Covers:
    - transcript_scope_segment: the shared naming helper (module-level function)
    - LogPaths.orchestrator_raw: scoped inside the bucket, unchanged outside it
    - Sidecar derivation via with_suffix stays correct when dots are stripped
    - Two sessions in the bucket produce two distinct, non-overwriting files
    - Unsafe session identifiers are sanitized into safe single path components
    - Events file name, bucket folder name, and bucket nesting depth are unchanged
"""

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core

BUCKET = "unknown-run"
REAL_RUN_ID = "20260101T170000Z-ab12"


class TestHarnessConstant(unittest.TestCase):
    """The HARNESS constant must identify this adapter for scope construction."""

    def test_harness_constant_is_claude_code(self):
        self.assertEqual("claude-code", core.HARNESS)


class TestTranscriptScopeSegment(unittest.TestCase):
    """transcript_scope_segment builds the scope component shared across all adapters.

    The segment is sanitize(harness)__sanitize(session_id) with dots additionally
    replaced by underscores so sidecar derivation via with_suffix() remains correct.
    """

    def test_returns_non_empty_string(self):
        result = core.transcript_scope_segment("claude-code", "sess-abc-123")
        self.assertIsInstance(result, str)
        self.assertTrue(result)

    def test_separates_harness_and_session_with_double_underscore(self):
        result = core.transcript_scope_segment("claude-code", "sess-abc-123")
        self.assertEqual("claude-code__sess-abc-123", result)

    def test_replaces_dots_in_session_id_with_underscores(self):
        result = core.transcript_scope_segment("claude-code", "sess.with.dots")
        self.assertEqual("claude-code__sess_with_dots", result)

    def test_replaces_dots_in_harness_name_with_underscores(self):
        result = core.transcript_scope_segment("my.harness", "sess-1")
        self.assertNotIn(".", result)

    def test_scope_segment_contains_no_dots(self):
        result = core.transcript_scope_segment("claude-code", "a.b.c.d.e")
        self.assertNotIn(".", result)

    def test_does_not_contain_path_separator_from_session_id(self):
        result = core.transcript_scope_segment("claude-code", "sess/sub/path")
        self.assertNotIn("/", result)
        self.assertNotIn("\\", result)

    def test_sanitizes_reserved_chars_in_session_id(self):
        result = core.transcript_scope_segment("claude-code", 'sess<with>bad:chars"here')
        for ch in '<>:"/\\|?*':
            self.assertNotIn(ch, result,
                             f"Reserved char {ch!r} must not appear in scope segment")

    def test_sanitizes_reserved_chars_in_harness_name(self):
        result = core.transcript_scope_segment("har<ness>", "sess-1")
        for ch in '<>:"/\\|?*':
            self.assertNotIn(ch, result,
                             f"Reserved char {ch!r} must not appear in scope segment")

    def test_is_deterministic_for_identical_inputs(self):
        a = core.transcript_scope_segment("claude-code", "sess-abc-123")
        b = core.transcript_scope_segment("claude-code", "sess-abc-123")
        self.assertEqual(a, b)

    def test_produces_different_output_for_different_session_ids(self):
        a = core.transcript_scope_segment("claude-code", "sess-A")
        b = core.transcript_scope_segment("claude-code", "sess-B")
        self.assertNotEqual(a, b)

    def test_produces_different_output_for_different_harness_names(self):
        a = core.transcript_scope_segment("claude-code", "sess-1")
        b = core.transcript_scope_segment("opencode", "sess-1")
        self.assertNotEqual(a, b)

    def test_produces_canonical_form_matching_all_adapters_contract(self):
        """All four adapters use the same contract; the canonical form is explicit here."""
        result = core.transcript_scope_segment("claude-code", "sess-abc-123")
        self.assertEqual("claude-code__sess-abc-123", result)


class TestOrchestratorRawRealRunFolder(unittest.TestCase):
    """orchestrator_raw in a real run folder returns the unchanged fixed filename."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_fixed_filename_in_real_run_folder(self):
        result = self.paths.orchestrator_raw(REAL_RUN_ID)
        self.assertEqual("00_orchestrator_session.raw", result.name)

    def test_session_id_ignored_in_real_run_folder(self):
        without_session = self.paths.orchestrator_raw(REAL_RUN_ID)
        with_session = self.paths.orchestrator_raw(REAL_RUN_ID, "sess-abc-123")
        self.assertEqual(without_session, with_session)

    def test_path_is_inside_the_real_run_folder(self):
        result = self.paths.orchestrator_raw(REAL_RUN_ID)
        self.assertTrue(str(result).startswith(str(self.paths.run_root(REAL_RUN_ID))))


class TestOrchestratorRawBucketScoping(unittest.TestCase):
    """orchestrator_raw inside the bucket uses session-scoped filenames."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_scoped_name_includes_harness_and_session(self):
        result = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        self.assertIn(core.HARNESS, result.name)
        self.assertIn("sess-abc-123", result.name)

    def test_scoped_name_exact_form(self):
        result = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        expected_name = f"00_orchestrator_session__{core.HARNESS}__sess-abc-123.raw"
        self.assertEqual(expected_name, result.name)

    def test_scoped_path_has_raw_suffix(self):
        result = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        self.assertEqual(".raw", result.suffix)

    def test_scoped_path_is_inside_the_bucket_folder(self):
        result = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        self.assertTrue(str(result).startswith(str(self.paths.run_root(BUCKET))))

    def test_scoped_path_is_a_direct_child_of_the_bucket_folder(self):
        result = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        bucket_root = self.paths.run_root(BUCKET)
        self.assertEqual(bucket_root, result.parent)

    def test_replaces_dots_in_session_id_in_scope(self):
        result = self.paths.orchestrator_raw(BUCKET, "sess.with.dots")
        expected_name = f"00_orchestrator_session__{core.HARNESS}__sess_with_dots.raw"
        self.assertEqual(expected_name, result.name)

    def test_sanitizes_path_separator_in_session_id(self):
        result = self.paths.orchestrator_raw(BUCKET, "sess/sub/path")
        self.assertNotIn("/", result.name)

    def test_sanitizes_reserved_chars_in_session_id(self):
        result = self.paths.orchestrator_raw(BUCKET, 'sess<with>bad:chars"here')
        for ch in '<>:"/\\|?*':
            self.assertNotIn(ch, result.name,
                             f"Reserved char {ch!r} must not appear in raw path name")

    def test_falls_back_to_unscoped_name_when_no_session_id(self):
        result = self.paths.orchestrator_raw(BUCKET)
        self.assertEqual("00_orchestrator_session.raw", result.name)

    def test_falls_back_to_unscoped_name_when_session_id_is_none(self):
        result = self.paths.orchestrator_raw(BUCKET, None)
        self.assertEqual("00_orchestrator_session.raw", result.name)

    def test_falls_back_to_unscoped_name_when_session_id_is_empty_string(self):
        result = self.paths.orchestrator_raw(BUCKET, "")
        self.assertEqual("00_orchestrator_session.raw", result.name)


class TestSidecarDerivationCorrectness(unittest.TestCase):
    """The sidecar is derived from the raw path via with_suffix('').with_suffix('.meta.json').

    Dots in the scope segment must be replaced so this derivation does not truncate
    the stem at the wrong position, keeping the sidecar adjacent to its transcript.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_sidecar_derivation_gives_correct_name_for_scoped_transcript(self):
        raw = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        sidecar = raw.with_suffix("").with_suffix(".meta.json")
        expected = f"00_orchestrator_session__{core.HARNESS}__sess-abc-123.meta.json"
        self.assertEqual(expected, sidecar.name)

    def test_sidecar_derivation_is_adjacent_to_transcript(self):
        raw = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        sidecar = raw.with_suffix("").with_suffix(".meta.json")
        self.assertEqual(raw.parent, sidecar.parent)

    def test_sidecar_derivation_correct_when_session_id_contains_dots(self):
        """Dots in session id are replaced so the derivation sees no misleading dot.

        Without dot-stripping, with_suffix('') on a name containing internal dots
        would remove only the part after the last dot, leaving a broken stem.
        """
        raw = self.paths.orchestrator_raw(BUCKET, "sess.with.dots")
        sidecar = raw.with_suffix("").with_suffix(".meta.json")
        expected = f"00_orchestrator_session__{core.HARNESS}__sess_with_dots.meta.json"
        self.assertEqual(expected, sidecar.name)

    def test_sidecar_and_transcript_stems_match(self):
        raw = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        sidecar = raw.with_suffix("").with_suffix(".meta.json")
        # stem of the raw file (without .raw) must equal stem of the sidecar (without .meta.json)
        raw_stem = raw.name.removesuffix(".raw")
        sidecar_stem = sidecar.name.removesuffix(".meta.json")
        self.assertEqual(raw_stem, sidecar_stem)

    def test_sidecar_derivation_on_unscoped_fallback(self):
        raw = self.paths.orchestrator_raw(BUCKET)
        sidecar = raw.with_suffix("").with_suffix(".meta.json")
        self.assertEqual("00_orchestrator_session.meta.json", sidecar.name)


class TestTwoSessionsProduceDistinctFiles(unittest.TestCase):
    """Two sessions in the bucket must produce two distinct, non-overwriting files."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_two_session_ids_produce_different_raw_paths(self):
        pA = self.paths.orchestrator_raw(BUCKET, "sess-A")
        pB = self.paths.orchestrator_raw(BUCKET, "sess-B")
        self.assertNotEqual(pA, pB)

    def test_two_session_ids_produce_different_sidecar_paths(self):
        rawA = self.paths.orchestrator_raw(BUCKET, "sess-A")
        rawB = self.paths.orchestrator_raw(BUCKET, "sess-B")
        sidecarA = rawA.with_suffix("").with_suffix(".meta.json")
        sidecarB = rawB.with_suffix("").with_suffix(".meta.json")
        self.assertNotEqual(sidecarA, sidecarB)

    def test_session_a_raw_path_does_not_equal_session_b_sidecar_path(self):
        rawA = self.paths.orchestrator_raw(BUCKET, "sess-A")
        rawB = self.paths.orchestrator_raw(BUCKET, "sess-B")
        sidecarB = rawB.with_suffix("").with_suffix(".meta.json")
        self.assertNotEqual(rawA, sidecarB)

    def test_two_harnesses_for_same_session_produce_different_scope_segments(self):
        segA = core.transcript_scope_segment("claude-code", "shared-session")
        segB = core.transcript_scope_segment("opencode", "shared-session")
        self.assertNotEqual(segA, segB)


class TestBucketInvariantsUnchanged(unittest.TestCase):
    """Events file name, bucket folder name, and nesting depth must not change."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_events_file_name_inside_bucket_is_unchanged(self):
        events = self.paths.orchestrator_events(BUCKET)
        self.assertEqual("00_orchestrator_events.jsonl", events.name)

    def test_bucket_folder_name_is_unknown_run(self):
        bucket_root = self.paths.run_root(BUCKET)
        self.assertEqual("unknown-run", bucket_root.name)

    def test_bucket_folder_is_direct_child_of_logs_root(self):
        bucket_root = self.paths.run_root(BUCKET)
        self.assertEqual(self.paths.root, bucket_root.parent)

    def test_real_run_folder_is_at_same_depth_as_bucket(self):
        bucket_root = self.paths.run_root(BUCKET)
        real_root = self.paths.run_root(REAL_RUN_ID)
        self.assertEqual(bucket_root.parent, real_root.parent)

    def test_transcript_is_direct_child_of_bucket_no_extra_nesting(self):
        raw = self.paths.orchestrator_raw(BUCKET, "sess-abc-123")
        bucket_root = self.paths.run_root(BUCKET)
        self.assertEqual(bucket_root, raw.parent)

    def test_events_file_is_direct_child_of_bucket(self):
        events = self.paths.orchestrator_events(BUCKET)
        bucket_root = self.paths.run_root(BUCKET)
        self.assertEqual(bucket_root, events.parent)


class TestHandlerCallSiteScoping(unittest.TestCase):
    """Integration: the transcript-writing call site in handle_stop wires session_id through.

    These tests drive handle_stop (a real lifecycle handler) with run_id unset
    so effective_run_id returns 'unknown-run', and verify that two different
    session IDs produce two distinct scoped files on disk — confirming the
    wiring fix rather than LogPaths in isolation.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.workspace = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.workspace)
        # A minimal source transcript that export_transcript can actually read.
        self.transcript = self.workspace / "transcript.jsonl"
        self.transcript.write_text('{"type":"assistant"}\n', encoding="utf-8")

    def tearDown(self):
        self.tmp.cleanup()

    def _make_ctx(self, session_id: str):
        """Minimal HookContext for handle_stop: no run_id so bucket is used."""
        import mosaic_logger_handlers_session  # noqa: F401 — imported for side effects
        payload = {
            "hook_event_name": "Stop",
            "session_id": session_id,
            "transcript_path": str(self.transcript),
            "last_assistant_message": "Done.",
        }
        return core.HookContext(payload, self.workspace, self.paths,
                                core.current_timestamp())

    def test_handle_stop_writes_scoped_transcript_to_bucket(self):
        """handle_stop with unknown-run bucket routes to a session-scoped path on disk."""
        import mosaic_logger_handlers_session as session_handlers
        ctx = self._make_ctx("sess-integration-A")
        session_handlers.handle_stop(ctx)
        expected = self.paths.orchestrator_raw("unknown-run", "sess-integration-A")
        self.assertTrue(expected.exists(),
                        f"Expected scoped transcript at {expected}")

    def test_two_sessions_in_bucket_produce_distinct_files(self):
        """Two handle_stop calls with different session IDs write to two different paths."""
        import mosaic_logger_handlers_session as session_handlers
        ctxA = self._make_ctx("sess-bucket-A")
        ctxB = self._make_ctx("sess-bucket-B")
        session_handlers.handle_stop(ctxA)
        session_handlers.handle_stop(ctxB)
        pathA = self.paths.orchestrator_raw("unknown-run", "sess-bucket-A")
        pathB = self.paths.orchestrator_raw("unknown-run", "sess-bucket-B")
        self.assertTrue(pathA.exists(), f"Expected scoped transcript at {pathA}")
        self.assertTrue(pathB.exists(), f"Expected scoped transcript at {pathB}")
        self.assertNotEqual(pathA, pathB)

    def test_session_b_write_does_not_overwrite_session_a_file(self):
        """Session B writing to the bucket does not erase session A's transcript."""
        import mosaic_logger_handlers_session as session_handlers
        ctxA = self._make_ctx("sess-nooverwrite-A")
        ctxB = self._make_ctx("sess-nooverwrite-B")
        session_handlers.handle_stop(ctxA)
        # Write different content for session B by replacing the source
        (self.workspace / "transcript_b.jsonl").write_text(
            '{"type":"user"}\n', encoding="utf-8"
        )
        ctxB.transcript_path = str(self.workspace / "transcript_b.jsonl")  # type: ignore[attr-defined]
        ctxB.payload["transcript_path"] = ctxB.transcript_path
        session_handlers.handle_stop(ctxB)
        pathA = self.paths.orchestrator_raw("unknown-run", "sess-nooverwrite-A")
        # Session A's file must still exist and contain the original content.
        self.assertTrue(pathA.exists())
        self.assertIn(b'"assistant"', pathA.read_bytes())


if __name__ == "__main__":
    unittest.main()
