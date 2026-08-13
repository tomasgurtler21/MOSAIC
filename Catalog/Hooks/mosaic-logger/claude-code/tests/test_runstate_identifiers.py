"""Tests for identifier generation and extraction in mosaic_logger_runstate.

Covers: new_run_id format, fallback_instance_id format and prefix handling,
extract_instance_id structured-match priority over bare-match, near-miss and
no-match cases, and never-raises contract. Also covers fallback_call_id format.
"""

import datetime
import os
import re
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_runstate as runstate


# Injectable timestamp for deterministic format assertions.
BASE_TIME = datetime.datetime(2026, 1, 1, 17, 0, 0, tzinfo=datetime.timezone.utc)

RUN_ID_PATTERN = re.compile(r"^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")
FALLBACK_PATTERN = re.compile(r"^[A-Za-z][A-Za-z0-9_.-]*_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")


class TestNewRunId(unittest.TestCase):
    """new_run_id returns {YYYYMMDD}T{HHMMSS}Z-{4 lowercase hex} with injectable now."""

    def test_format_matches_documented_pattern(self):
        run_id = runstate.new_run_id(now=BASE_TIME)
        self.assertRegex(run_id, RUN_ID_PATTERN)

    def test_timestamp_component_reflects_injected_now(self):
        run_id = runstate.new_run_id(now=BASE_TIME)
        self.assertTrue(run_id.startswith("20260101T170000Z-"))

    def test_suffix_is_exactly_four_lowercase_hex_chars(self):
        run_id = runstate.new_run_id(now=BASE_TIME)
        suffix = run_id.split("-")[-1]
        self.assertEqual(4, len(suffix))
        self.assertRegex(suffix, r"^[0-9a-f]{4}$")

    def test_suffix_is_not_uppercase(self):
        for _ in range(10):
            run_id = runstate.new_run_id(now=BASE_TIME)
            suffix = run_id.split("-")[-1]
            self.assertEqual(suffix, suffix.lower())

    def test_consecutive_calls_produce_different_suffixes(self):
        # Random suffix: 16^4 = 65536 possibilities; collision in 20 draws is negligible
        ids = {runstate.new_run_id(now=BASE_TIME) for _ in range(20)}
        self.assertGreater(len(ids), 1)

    def test_without_injected_now_still_returns_valid_format(self):
        run_id = runstate.new_run_id()
        self.assertRegex(run_id, RUN_ID_PATTERN)

    def test_different_timestamps_produce_different_prefixes(self):
        t1 = datetime.datetime(2026, 1, 1, 12, 0, 0, tzinfo=datetime.timezone.utc)
        t2 = datetime.datetime(2026, 6, 15, 9, 30, 0, tzinfo=datetime.timezone.utc)
        id1 = runstate.new_run_id(now=t1)
        id2 = runstate.new_run_id(now=t2)
        self.assertNotEqual(id1.split("-")[0], id2.split("-")[0])


class TestFallbackInstanceId(unittest.TestCase):
    """fallback_instance_id returns {prefix}_{YYYYMMDD}T{HHMMSS}Z-{4hex};
    uses "agent" when prefix is None."""

    def test_with_prefix_returns_prefix_underscore_timestamp_suffix(self):
        result = runstate.fallback_instance_id("RequirementsRefinement", now=BASE_TIME)
        self.assertRegex(
            result,
            r"^RequirementsRefinement_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$",
        )

    def test_with_none_prefix_uses_agent_literal(self):
        result = runstate.fallback_instance_id(None, now=BASE_TIME)
        self.assertRegex(result, r"^agent_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")

    def test_timestamp_component_reflects_injected_now(self):
        result = runstate.fallback_instance_id("prefix", now=BASE_TIME)
        self.assertIn("20260101T170000Z", result)

    def test_suffix_is_four_lowercase_hex_chars(self):
        result = runstate.fallback_instance_id("prefix", now=BASE_TIME)
        suffix = result.split("-")[-1]
        self.assertEqual(4, len(suffix))
        self.assertRegex(suffix, r"^[0-9a-f]{4}$")

    def test_consecutive_calls_may_produce_different_suffixes(self):
        ids = {runstate.fallback_instance_id("prefix", now=BASE_TIME) for _ in range(20)}
        self.assertGreater(len(ids), 1)

    def test_different_prefixes_produce_different_results(self):
        a = runstate.fallback_instance_id("AgentA", now=BASE_TIME)
        b = runstate.fallback_instance_id("AgentB", now=BASE_TIME)
        self.assertFalse(a.startswith("AgentB"))
        self.assertFalse(b.startswith("AgentA"))


class TestFallbackCallId(unittest.TestCase):
    """fallback_call_id returns {tool_name or "tool"}_{YYYYMMDD}T{HHMMSS}Z-{4hex}."""

    def test_with_tool_name_uses_tool_name_as_prefix(self):
        result = runstate.fallback_call_id("Bash", now=BASE_TIME)
        self.assertRegex(result, r"^Bash_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")

    def test_with_none_tool_name_uses_tool_literal(self):
        result = runstate.fallback_call_id(None, now=BASE_TIME)
        self.assertRegex(result, r"^tool_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")

    def test_suffix_is_four_lowercase_hex_chars(self):
        result = runstate.fallback_call_id("Read", now=BASE_TIME)
        suffix = result.split("-")[-1]
        self.assertEqual(4, len(suffix))
        self.assertRegex(suffix, r"^[0-9a-f]{4}$")

    def test_consecutive_calls_may_differ(self):
        ids = {runstate.fallback_call_id("Bash", now=BASE_TIME) for _ in range(20)}
        self.assertGreater(len(ids), 1)


class TestExtractInstanceId(unittest.TestCase):
    """extract_instance_id matches {AgentName}#{Number} preferring structured
    key-value occurrences; falls back to the earliest bare occurrence; returns
    None when neither form exists."""

    # --- Structured (key-value) match ---

    def test_extracts_from_json_key_with_quoted_value(self):
        prompt = '{"agent_instance_id": "RequirementsRefinement#2", "task": "something"}'
        result = runstate.extract_instance_id(prompt)
        self.assertEqual("RequirementsRefinement#2", result)

    def test_extracts_from_json_key_with_bare_value(self):
        prompt = "agent_instance_id: RequirementsRefinement#2"
        result = runstate.extract_instance_id(prompt)
        self.assertEqual("RequirementsRefinement#2", result)

    def test_extracts_two_digit_number(self):
        prompt = '{"agent_instance_id": "TestAgent#10"}'
        result = runstate.extract_instance_id(prompt)
        self.assertEqual("TestAgent#10", result)

    def test_structured_match_preferred_over_earlier_bare_match(self):
        # A bare occurrence appears before the structured key/value in the text.
        prompt = 'BareAgent#5 is mentioned first. {"agent_instance_id": "StructuredAgent#2"}'
        result = runstate.extract_instance_id(prompt)
        self.assertEqual("StructuredAgent#2", result)

    # --- Bare match (fallback when no structured occurrence) ---

    def test_extracts_earliest_bare_occurrence_when_no_structured_match(self):
        prompt = "Hello, this task is for TestAgent#3. Also see OtherAgent#7."
        result = runstate.extract_instance_id(prompt)
        self.assertEqual("TestAgent#3", result)

    def test_bare_match_requires_letter_start(self):
        # Name must match [A-Za-z][A-Za-z0-9_.-]* so leading digit is not a match
        result = runstate.extract_instance_id("2Agent#1 mentioned here")
        self.assertIsNone(result)

    # --- No-match cases ---

    def test_returns_none_when_no_match_in_plain_text(self):
        result = runstate.extract_instance_id("plain text with no agent id anywhere")
        self.assertIsNone(result)

    def test_returns_none_for_none_input(self):
        result = runstate.extract_instance_id(None)
        self.assertIsNone(result)

    def test_returns_none_for_empty_string(self):
        result = runstate.extract_instance_id("")
        self.assertIsNone(result)

    def test_does_not_match_name_without_hash_number(self):
        result = runstate.extract_instance_id("SomeAgent is mentioned but no hash-number")
        self.assertIsNone(result)

    def test_does_not_match_hash_without_agent_name(self):
        result = runstate.extract_instance_id("#2 is just a number")
        self.assertIsNone(result)

    def test_does_not_match_number_without_hash(self):
        result = runstate.extract_instance_id("SomeAgent2 has no hash separator")
        self.assertIsNone(result)

    def test_free_prose_mention_of_status_name_is_not_a_match(self):
        # Prose mentioning a status keyword must not be mistaken for an agent id
        result = runstate.extract_instance_id("The status is SUCCESS")
        self.assertIsNone(result)

    # --- Never-raises contract ---

    def test_never_raises_on_control_characters(self):
        try:
            runstate.extract_instance_id("\x00\x01\x02\x03")
        except Exception as e:
            self.fail(f"extract_instance_id raised on control chars: {e}")

    def test_never_raises_on_very_long_input(self):
        try:
            runstate.extract_instance_id("a" * 100_000)
        except Exception as e:
            self.fail(f"extract_instance_id raised on long input: {e}")

    def test_never_raises_on_unusual_punctuation(self):
        try:
            runstate.extract_instance_id("!@#$%^&*()_+{}|:<>?")
        except Exception as e:
            self.fail(f"extract_instance_id raised on punctuation: {e}")
