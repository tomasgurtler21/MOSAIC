"""Tests for run_id extraction in mosaic_logger_runstate.

Covers: extract_run_id structured-match from JSON key-value, plain key-value (colon
and equals forms), bare-match fallback, structured-match priority over bare match,
near-miss cases (wrong format, uppercase hex, partial pattern), no-match cases
(None, empty string, plain text), and the never-raises contract.

These tests are written against mosaic_logger_runstate.extract_run_id, which does
not yet exist. They fail in TDD RED phase and become GREEN once the function is
implemented.
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_runstate as runstate


# Canonical run_id values in the established format: {YYYYMMDD}T{HHMMSS}Z-{4hex}
VALID_RUN_ID = "20260727T170000Z-a3f9"
ANOTHER_RUN_ID = "20260101T120000Z-bcde"


class TestExtractRunIdStructuredMatch(unittest.TestCase):
    """extract_run_id extracts run_id from structured key-value occurrences.

    Structured match: run_id key (with optional quotes/colon/equals) followed
    by a value matching the {YYYYMMDD}T{HHMMSS}Z-{4hex} format. This is the
    primary match path, matching the JSON dispatch format used by the runner.
    """

    def test_extracts_from_json_with_quoted_value(self):
        prompt = (
            '{"agent_instance_id": "TestAgent#1", '
            f'"run_id": "{VALID_RUN_ID}", '
            '"task_description": "do something"}'
        )
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_extracts_from_json_key_colon_space_value(self):
        prompt = f'"run_id": "{VALID_RUN_ID}"'
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_extracts_from_plain_key_colon_bare_value(self):
        prompt = f"run_id: {VALID_RUN_ID}"
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_extracts_from_plain_key_equals_bare_value(self):
        prompt = f"run_id = {VALID_RUN_ID}"
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_extracts_from_key_with_quoted_value_and_surrounding_whitespace(self):
        prompt = f'run_id : "{VALID_RUN_ID}"'
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_structured_match_preferred_over_earlier_bare_match(self):
        # A bare occurrence of a different run_id appears before the structured key-value.
        prompt = (
            f"The previous run was {ANOTHER_RUN_ID}. "
            f'"run_id": "{VALID_RUN_ID}"'
        )
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_extracts_from_multiline_json_dispatch(self):
        prompt = (
            "{\n"
            '  "agent_instance_id": "TestWriter#5",\n'
            f'  "run_id": "{VALID_RUN_ID}",\n'
            '  "task_description": "Write tests"\n'
            "}"
        )
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_extracts_from_structured_key_value_format_text(self):
        prompt = (
            "agent_instance_id: TestWriter#5\n"
            f"run_id: {VALID_RUN_ID}\n"
            "task_description: Write tests"
        )
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)


class TestExtractRunIdBareMatch(unittest.TestCase):
    """extract_run_id falls back to the earliest bare format occurrence when
    no structured key-value match is found."""

    def test_extracts_earliest_bare_occurrence_when_no_structured_match(self):
        prompt = f"Processing run {VALID_RUN_ID} for agent TestWriter#1."
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_bare_match_returns_first_occurrence_when_multiple_present(self):
        # First valid pattern should win when no structured key exists
        prompt = f"First: {VALID_RUN_ID}. Second: {ANOTHER_RUN_ID}."
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)

    def test_bare_match_requires_no_alnum_immediately_before(self):
        # The lookbehind prevents matching when a run_id format value is
        # immediately preceded by an alphanumeric character.
        prompt = f"prefix{VALID_RUN_ID}"
        result = runstate.extract_run_id(prompt)
        self.assertIsNone(result)

    def test_bare_match_at_start_of_string(self):
        prompt = f"{VALID_RUN_ID} is the run identifier"
        result = runstate.extract_run_id(prompt)
        self.assertEqual(VALID_RUN_ID, result)


class TestExtractRunIdNoMatch(unittest.TestCase):
    """extract_run_id returns None when no valid run_id is present."""

    def test_returns_none_for_none_input(self):
        result = runstate.extract_run_id(None)
        self.assertIsNone(result)

    def test_returns_none_for_empty_string(self):
        result = runstate.extract_run_id("")
        self.assertIsNone(result)

    def test_returns_none_for_plain_text_without_run_id(self):
        result = runstate.extract_run_id("plain text without any run id pattern")
        self.assertIsNone(result)

    def test_returns_none_when_hex_suffix_is_too_short(self):
        # Only 3 hex chars — does not match (format requires exactly 4)
        result = runstate.extract_run_id("run_id: 20260727T170000Z-a3f")
        self.assertIsNone(result)

    def test_returns_none_when_hex_suffix_is_uppercase(self):
        # Uppercase hex is not matched (format requires lowercase only)
        result = runstate.extract_run_id("run_id: 20260727T170000Z-A3F9")
        self.assertIsNone(result)

    def test_returns_none_when_key_present_but_value_has_wrong_format(self):
        result = runstate.extract_run_id('run_id: "not-a-valid-run-id"')
        self.assertIsNone(result)

    def test_returns_none_when_only_date_part_present_without_hex_suffix(self):
        result = runstate.extract_run_id("20260727T170000Z")
        self.assertIsNone(result)

    def test_returns_none_when_only_hex_suffix_present(self):
        result = runstate.extract_run_id("a3f9")
        self.assertIsNone(result)

    def test_returns_none_when_only_agent_instance_id_key_present(self):
        result = runstate.extract_run_id('{"agent_instance_id": "TestAgent#1"}')
        self.assertIsNone(result)

    def test_returns_none_when_separator_between_parts_is_wrong(self):
        # Dash replaced with underscore — not a valid format
        result = runstate.extract_run_id("20260727T170000Z_a3f9")
        self.assertIsNone(result)

    def test_returns_none_when_timestamp_has_no_t_separator(self):
        # Missing 'T' between date and time parts
        result = runstate.extract_run_id("20260727170000Z-a3f9")
        self.assertIsNone(result)

    def test_returns_none_when_timestamp_has_no_z_suffix(self):
        # Missing trailing 'Z' on the timestamp
        result = runstate.extract_run_id("20260727T170000-a3f9")
        self.assertIsNone(result)


class TestExtractRunIdNeverRaises(unittest.TestCase):
    """extract_run_id never raises any exception, regardless of input content."""

    def test_never_raises_on_none(self):
        try:
            runstate.extract_run_id(None)
        except Exception as e:
            self.fail(f"extract_run_id raised on None: {e}")

    def test_never_raises_on_empty_string(self):
        try:
            runstate.extract_run_id("")
        except Exception as e:
            self.fail(f"extract_run_id raised on empty string: {e}")

    def test_never_raises_on_control_characters(self):
        try:
            runstate.extract_run_id("\x00\x01\x02\x03")
        except Exception as e:
            self.fail(f"extract_run_id raised on control chars: {e}")

    def test_never_raises_on_very_long_input(self):
        try:
            runstate.extract_run_id("a" * 100_000)
        except Exception as e:
            self.fail(f"extract_run_id raised on 100k-char input: {e}")

    def test_never_raises_on_unusual_punctuation(self):
        try:
            runstate.extract_run_id("!@#$%^&*()_+{}|:<>?")
        except Exception as e:
            self.fail(f"extract_run_id raised on punctuation: {e}")

    def test_never_raises_on_json_like_malformed_content(self):
        try:
            runstate.extract_run_id('{"run_id": null, "other": [1, 2, 3}')
        except Exception as e:
            self.fail(f"extract_run_id raised on malformed JSON-like content: {e}")

    def test_never_raises_on_whitespace_only_string(self):
        try:
            runstate.extract_run_id("   \t\n   ")
        except Exception as e:
            self.fail(f"extract_run_id raised on whitespace-only input: {e}")


if __name__ == "__main__":
    unittest.main()
