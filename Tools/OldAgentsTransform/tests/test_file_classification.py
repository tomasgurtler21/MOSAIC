"""Tests for file_classification module (Stage 5 — T5.1).

Tests cover:
- FileClass enum with four members: SUBAGENT, ORCHESTRATOR, UTILITY, NON_AGENT
- UTILITY_AGENT_FILENAMES and NON_AGENT_FILENAMES allowlist contents
- classify_file() returning UTILITY for every utility-agent filename in the allowlist
- classify_file() returning NON_AGENT for every non-agent filename in the allowlist
- classify_file() case-insensitivity (uppercase, mixed-case filenames)
- classify_file() handling the '.agent.md' double extension
- classify_file() handling names that contain spaces
- Negative cases: ordinary agent files return SUBAGENT
- Near-miss names are not misclassified as UTILITY or NON_AGENT
- Orchestrator filenames continue to return ORCHESTRATOR
- is_skippable() returns True only for UTILITY and NON_AGENT
- skip_message() returns a reason-bearing message for skippable classes
- skip_message() raises ValueError for non-skippable classes

All tests are in TDD RED phase: they will fail until file_classification.py is created
with FileClass, UTILITY_AGENT_FILENAMES, NON_AGENT_FILENAMES, classify_file(),
is_skippable() and skip_message().
"""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

import file_classification as fc  # noqa: E402


# ---------------------------------------------------------------------------
# FileClass enum
# ---------------------------------------------------------------------------

class TestFileClassEnum:
    """FileClass must be an enum with exactly the four expected members."""

    def test_file_class_has_subagent(self):
        """FileClass.SUBAGENT must exist."""
        assert hasattr(fc.FileClass, "SUBAGENT"), (
            "FileClass must have a SUBAGENT member"
        )

    def test_file_class_has_orchestrator(self):
        """FileClass.ORCHESTRATOR must exist."""
        assert hasattr(fc.FileClass, "ORCHESTRATOR"), (
            "FileClass must have an ORCHESTRATOR member"
        )

    def test_file_class_has_utility(self):
        """FileClass.UTILITY must exist."""
        assert hasattr(fc.FileClass, "UTILITY"), (
            "FileClass must have a UTILITY member"
        )

    def test_file_class_has_non_agent(self):
        """FileClass.NON_AGENT must exist."""
        assert hasattr(fc.FileClass, "NON_AGENT"), (
            "FileClass must have a NON_AGENT member"
        )

    def test_subagent_value(self):
        """FileClass.SUBAGENT.value must be 'subagent'."""
        assert fc.FileClass.SUBAGENT.value == "subagent"

    def test_orchestrator_value(self):
        """FileClass.ORCHESTRATOR.value must be 'orchestrator'."""
        assert fc.FileClass.ORCHESTRATOR.value == "orchestrator"

    def test_utility_value(self):
        """FileClass.UTILITY.value must be 'utility'."""
        assert fc.FileClass.UTILITY.value == "utility"

    def test_non_agent_value(self):
        """FileClass.NON_AGENT.value must be 'non_agent'."""
        assert fc.FileClass.NON_AGENT.value == "non_agent"


# ---------------------------------------------------------------------------
# Allowlist constants
# ---------------------------------------------------------------------------

class TestAllowlistConstants:
    """UTILITY_AGENT_FILENAMES and NON_AGENT_FILENAMES must contain the required names."""

    def test_utility_agent_filenames_exists(self):
        """UTILITY_AGENT_FILENAMES must be defined in file_classification."""
        assert hasattr(fc, "UTILITY_AGENT_FILENAMES"), (
            "file_classification must define UTILITY_AGENT_FILENAMES"
        )

    def test_non_agent_filenames_exists(self):
        """NON_AGENT_FILENAMES must be defined in file_classification."""
        assert hasattr(fc, "NON_AGENT_FILENAMES"), (
            "file_classification must define NON_AGENT_FILENAMES"
        )

    def test_utility_contains_transformation(self):
        """UTILITY_AGENT_FILENAMES must include 'transformation'."""
        assert "transformation" in fc.UTILITY_AGENT_FILENAMES

    def test_utility_contains_anthropic_agent_creator(self):
        """UTILITY_AGENT_FILENAMES must include 'anthropic-agent-creator'."""
        assert "anthropic-agent-creator" in fc.UTILITY_AGENT_FILENAMES

    def test_utility_contains_anthropic_subagent_creator(self):
        """UTILITY_AGENT_FILENAMES must include 'anthropic-subagent-creator'."""
        assert "anthropic-subagent-creator" in fc.UTILITY_AGENT_FILENAMES

    def test_utility_contains_workflow_creator(self):
        """UTILITY_AGENT_FILENAMES must include 'workflow-creator'."""
        assert "workflow-creator" in fc.UTILITY_AGENT_FILENAMES

    def test_utility_contains_orchestration_architect(self):
        """UTILITY_AGENT_FILENAMES must include 'orchestration-architect'."""
        assert "orchestration-architect" in fc.UTILITY_AGENT_FILENAMES

    def test_utility_contains_system_prompt_capturer(self):
        """UTILITY_AGENT_FILENAMES must include 'system-prompt-capturer'."""
        assert "system-prompt-capturer" in fc.UTILITY_AGENT_FILENAMES

    def test_non_agent_contains_scl_developer_chatmode(self):
        """NON_AGENT_FILENAMES must include 'scl developer.chatmode'."""
        assert "scl developer.chatmode" in fc.NON_AGENT_FILENAMES

    def test_non_agent_contains_cheap_orchestrator(self):
        """NON_AGENT_FILENAMES must include 'cheap orchestrator'."""
        assert "cheap orchestrator" in fc.NON_AGENT_FILENAMES

    def test_utility_filenames_are_lowercase(self):
        """Every entry in UTILITY_AGENT_FILENAMES must be lower-cased."""
        for name in fc.UTILITY_AGENT_FILENAMES:
            assert name == name.lower(), (
                f"UTILITY_AGENT_FILENAMES entry {name!r} must be lower-cased"
            )

    def test_non_agent_filenames_are_lowercase(self):
        """Every entry in NON_AGENT_FILENAMES must be lower-cased."""
        for name in fc.NON_AGENT_FILENAMES:
            assert name == name.lower(), (
                f"NON_AGENT_FILENAMES entry {name!r} must be lower-cased"
            )


# ---------------------------------------------------------------------------
# T5.1 — classify_file: utility agent allowlist
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("filename", [
    "transformation.md",
    "anthropic-agent-creator.md",
    "anthropic-subagent-creator.md",
    "workflow-creator.md",
    "orchestration-architect.md",
    "system-prompt-capturer.md",
])
class TestClassifyFileUtilityAgents:
    """classify_file must return UTILITY for every utility agent filename with .md extension."""

    def test_classify_returns_utility(self, filename):
        """classify_file on a utility agent filename must return FileClass.UTILITY."""
        path = pathlib.Path("/some/harness/Agents") / filename
        result = fc.classify_file(path)
        assert result is fc.FileClass.UTILITY, (
            f"Expected UTILITY for {filename!r}; got {result!r}"
        )


@pytest.mark.parametrize("filename", [
    "transformation.agent.md",
    "anthropic-agent-creator.agent.md",
    "anthropic-subagent-creator.agent.md",
    "workflow-creator.agent.md",
    "orchestration-architect.agent.md",
    "system-prompt-capturer.agent.md",
])
class TestClassifyFileUtilityAgentsDoubleExtension:
    """classify_file must return UTILITY for utility agent filenames with .agent.md extension."""

    def test_classify_returns_utility_with_agent_md(self, filename):
        """classify_file must strip '.agent.md' before matching; utility agents must return UTILITY."""
        path = pathlib.Path("/some/harness/Agents") / filename
        result = fc.classify_file(path)
        assert result is fc.FileClass.UTILITY, (
            f"Expected UTILITY for {filename!r} (.agent.md extension); got {result!r}"
        )


# ---------------------------------------------------------------------------
# T5.1 — classify_file: non-agent allowlist
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("filename", [
    "SCL developer.chatmode.md",
    "Cheap orchestrator.md",
])
class TestClassifyFileNonAgentFiles:
    """classify_file must return NON_AGENT for files in the non-agent allowlist."""

    def test_classify_returns_non_agent(self, filename):
        """classify_file on a non-agent filename must return FileClass.NON_AGENT."""
        path = pathlib.Path("/some/harness/Agents") / filename
        result = fc.classify_file(path)
        assert result is fc.FileClass.NON_AGENT, (
            f"Expected NON_AGENT for {filename!r}; got {result!r}"
        )


class TestClassifyFileNonAgentWithAgentMdExtension:
    """classify_file must strip '.agent.md' before matching non-agent names."""

    def test_scl_developer_chatmode_agent_md(self):
        """'SCL developer.chatmode.agent.md' must be classified as NON_AGENT."""
        path = pathlib.Path("/some/dir") / "SCL developer.chatmode.agent.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.NON_AGENT, (
            f"Expected NON_AGENT for 'SCL developer.chatmode.agent.md'; got {result!r}"
        )


# ---------------------------------------------------------------------------
# T5.1 — classify_file: case-insensitivity
# ---------------------------------------------------------------------------

class TestClassifyFileCaseInsensitivity:
    """classify_file must match filenames case-insensitively."""

    def test_transformation_uppercase(self):
        """'TRANSFORMATION.MD' must be classified as UTILITY."""
        path = pathlib.Path("/dir") / "TRANSFORMATION.MD"
        result = fc.classify_file(path)
        assert result is fc.FileClass.UTILITY, (
            f"Expected UTILITY for 'TRANSFORMATION.MD'; got {result!r}"
        )

    def test_transformation_mixed_case(self):
        """'Transformation.md' must be classified as UTILITY."""
        path = pathlib.Path("/dir") / "Transformation.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.UTILITY, (
            f"Expected UTILITY for 'Transformation.md'; got {result!r}"
        )

    def test_workflow_creator_uppercase(self):
        """'WORKFLOW-CREATOR.MD' must be classified as UTILITY."""
        path = pathlib.Path("/dir") / "WORKFLOW-CREATOR.MD"
        result = fc.classify_file(path)
        assert result is fc.FileClass.UTILITY, (
            f"Expected UTILITY for 'WORKFLOW-CREATOR.MD'; got {result!r}"
        )

    def test_anthropic_agent_creator_mixed_case(self):
        """'Anthropic-Agent-Creator.md' must be classified as UTILITY."""
        path = pathlib.Path("/dir") / "Anthropic-Agent-Creator.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.UTILITY, (
            f"Expected UTILITY for 'Anthropic-Agent-Creator.md'; got {result!r}"
        )

    def test_scl_developer_all_upper(self):
        """'SCL DEVELOPER.CHATMODE.MD' must be classified as NON_AGENT."""
        path = pathlib.Path("/dir") / "SCL DEVELOPER.CHATMODE.MD"
        result = fc.classify_file(path)
        assert result is fc.FileClass.NON_AGENT, (
            f"Expected NON_AGENT for 'SCL DEVELOPER.CHATMODE.MD'; got {result!r}"
        )

    def test_cheap_orchestrator_uppercase(self):
        """'CHEAP ORCHESTRATOR.MD' must be classified as NON_AGENT."""
        path = pathlib.Path("/dir") / "CHEAP ORCHESTRATOR.MD"
        result = fc.classify_file(path)
        assert result is fc.FileClass.NON_AGENT, (
            f"Expected NON_AGENT for 'CHEAP ORCHESTRATOR.MD'; got {result!r}"
        )


# ---------------------------------------------------------------------------
# T5.1 — classify_file: names with spaces
# ---------------------------------------------------------------------------

class TestClassifyFileNamesWithSpaces:
    """classify_file must correctly match names that contain spaces."""

    def test_scl_developer_chatmode_with_spaces(self):
        """'SCL developer.chatmode.md' (spaces in filename) must return NON_AGENT."""
        path = pathlib.Path("/dir") / "SCL developer.chatmode.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.NON_AGENT

    def test_cheap_orchestrator_with_space(self):
        """'Cheap orchestrator.md' (space in filename) must return NON_AGENT."""
        path = pathlib.Path("/dir") / "Cheap orchestrator.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.NON_AGENT

    def test_cheap_orchestrator_in_nested_path(self):
        """A non-agent filename in a deeply nested path is still classified by base name only."""
        path = pathlib.Path("/some/deep/nested/path") / "Cheap orchestrator.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.NON_AGENT, (
            "Classification must depend only on the base name, not the directory path"
        )


# ---------------------------------------------------------------------------
# T5.1 — classify_file: orchestrator stays ORCHESTRATOR
# ---------------------------------------------------------------------------

class TestClassifyFileOrchestratorUnchanged:
    """Orchestrator files must continue to classify as ORCHESTRATOR, not SUBAGENT or UTILITY."""

    def test_orchestrator_md_is_orchestrator(self):
        """'orchestrator.md' must still return ORCHESTRATOR."""
        path = pathlib.Path("/dir") / "orchestrator.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.ORCHESTRATOR

    def test_orchestrator_agent_md_is_orchestrator(self):
        """'orchestrator.agent.md' must still return ORCHESTRATOR."""
        path = pathlib.Path("/dir") / "orchestrator.agent.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.ORCHESTRATOR

    def test_orchestrator_uppercase_is_orchestrator(self):
        """'ORCHESTRATOR.MD' must return ORCHESTRATOR (case-insensitive)."""
        path = pathlib.Path("/dir") / "ORCHESTRATOR.MD"
        result = fc.classify_file(path)
        assert result is fc.FileClass.ORCHESTRATOR


# ---------------------------------------------------------------------------
# T5.1 — classify_file: ordinary agent files return SUBAGENT
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("filename", [
    "implementation-tdd.md",
    "test-writer-tdd.md",
    "code-reviewer.md",
    "research-agent.md",
    "planner-tdd-soft.md",
    "implementation-tdd.agent.md",
])
class TestClassifyFileOrdinaryAgentsReturnSubagent:
    """Ordinary agent files (not in any skip allowlist) must return SUBAGENT."""

    def test_classify_returns_subagent(self, filename):
        """An ordinary agent filename must return FileClass.SUBAGENT."""
        path = pathlib.Path("/dir") / filename
        result = fc.classify_file(path)
        assert result is fc.FileClass.SUBAGENT, (
            f"Expected SUBAGENT for {filename!r}; got {result!r}"
        )


# ---------------------------------------------------------------------------
# T5.1 — classify_file: near-miss names are not misclassified
# ---------------------------------------------------------------------------

class TestClassifyFileNearMissNames:
    """Names that resemble utility/non-agent names but differ must return SUBAGENT."""

    def test_my_transformation_is_subagent(self):
        """'my-transformation.md' must not match the 'transformation' utility entry."""
        path = pathlib.Path("/dir") / "my-transformation.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.SUBAGENT, (
            "'my-transformation.md' must not be classified as a utility agent "
            "(allowlist match must be exact, not substring)"
        )

    def test_transformation_v2_is_subagent(self):
        """'transformation-v2.md' must not match the 'transformation' utility entry."""
        path = pathlib.Path("/dir") / "transformation-v2.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.SUBAGENT, (
            "'transformation-v2.md' must not be classified as a utility agent"
        )

    def test_workflow_creator_extended_is_subagent(self):
        """'workflow-creator-extended.md' must not match 'workflow-creator'."""
        path = pathlib.Path("/dir") / "workflow-creator-extended.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.SUBAGENT, (
            "'workflow-creator-extended.md' must not be classified as a utility agent"
        )

    def test_cheap_orchestrator_hyphenated_is_subagent(self):
        """'cheap-orchestrator.md' (hyphenated) must not match 'cheap orchestrator' (space)."""
        path = pathlib.Path("/dir") / "cheap-orchestrator.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.SUBAGENT, (
            "'cheap-orchestrator.md' must not be classified as NON_AGENT; "
            "the non-agent entry uses a space, not a hyphen"
        )

    def test_orchestration_architect_prefix_is_subagent(self):
        """'my-orchestration-architect.md' must not match 'orchestration-architect'."""
        path = pathlib.Path("/dir") / "my-orchestration-architect.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.SUBAGENT, (
            "'my-orchestration-architect.md' must not be classified as a utility agent"
        )

    def test_system_prompt_capturer_suffix_is_subagent(self):
        """'system-prompt-capturer-v2.md' must not match 'system-prompt-capturer'."""
        path = pathlib.Path("/dir") / "system-prompt-capturer-v2.md"
        result = fc.classify_file(path)
        assert result is fc.FileClass.SUBAGENT, (
            "'system-prompt-capturer-v2.md' must not be classified as a utility agent"
        )

    def test_classification_depends_only_on_base_name_not_path(self):
        """Classification must depend only on the file's base name, not its directory path."""
        # A real subagent file placed in a directory with a utility name must still be SUBAGENT
        path = pathlib.Path("/path/to/transformation/real-subagent.md")
        result = fc.classify_file(path)
        assert result is fc.FileClass.SUBAGENT, (
            "Classification must use path.name (base name), not path components"
        )


# ---------------------------------------------------------------------------
# T5.1 — is_skippable
# ---------------------------------------------------------------------------

class TestIsSkippable:
    """is_skippable must return True for UTILITY and NON_AGENT, False for others."""

    def test_utility_is_skippable(self):
        """is_skippable(FileClass.UTILITY) must return True."""
        assert fc.is_skippable(fc.FileClass.UTILITY) is True

    def test_non_agent_is_skippable(self):
        """is_skippable(FileClass.NON_AGENT) must return True."""
        assert fc.is_skippable(fc.FileClass.NON_AGENT) is True

    def test_subagent_is_not_skippable(self):
        """is_skippable(FileClass.SUBAGENT) must return False."""
        assert fc.is_skippable(fc.FileClass.SUBAGENT) is False

    def test_orchestrator_is_not_skippable(self):
        """is_skippable(FileClass.ORCHESTRATOR) must return False."""
        assert fc.is_skippable(fc.FileClass.ORCHESTRATOR) is False


# ---------------------------------------------------------------------------
# T5.1 — skip_message
# ---------------------------------------------------------------------------

class TestSkipMessageUtility:
    """skip_message must return a reason-bearing message for UTILITY files."""

    def test_skip_message_for_utility_contains_path(self):
        """The utility skip message must contain the file path string."""
        path = pathlib.Path("/dir") / "transformation.md"
        msg = fc.skip_message(path, fc.FileClass.UTILITY)
        assert str(path) in msg, (
            f"skip_message for UTILITY must contain the file path; got {msg!r}"
        )

    def test_skip_message_for_utility_contains_utility_reason(self):
        """The utility skip message must mention 'utility agent'."""
        path = pathlib.Path("/dir") / "transformation.md"
        msg = fc.skip_message(path, fc.FileClass.UTILITY)
        assert "utility agent" in msg.lower(), (
            f"skip_message for UTILITY must mention 'utility agent'; got {msg!r}"
        )

    def test_skip_message_for_utility_matches_constant(self):
        """The utility skip message must match the SKIP_UTILITY_AGENT template."""
        path = pathlib.Path("/dir") / "transformation.md"
        msg = fc.skip_message(path, fc.FileClass.UTILITY)
        expected = fc.SKIP_UTILITY_AGENT.format(path=path)
        assert msg == expected, (
            f"skip_message for UTILITY must match SKIP_UTILITY_AGENT template; "
            f"expected {expected!r}, got {msg!r}"
        )


class TestSkipMessageNonAgent:
    """skip_message must return a reason-bearing message for NON_AGENT files."""

    def test_skip_message_for_non_agent_contains_path(self):
        """The non-agent skip message must contain the file path string."""
        path = pathlib.Path("/dir") / "Cheap orchestrator.md"
        msg = fc.skip_message(path, fc.FileClass.NON_AGENT)
        assert str(path) in msg, (
            f"skip_message for NON_AGENT must contain the file path; got {msg!r}"
        )

    def test_skip_message_for_non_agent_contains_reason(self):
        """The non-agent skip message must mention 'MOSAIC agent file' or 'not a MOSAIC agent'."""
        path = pathlib.Path("/dir") / "Cheap orchestrator.md"
        msg = fc.skip_message(path, fc.FileClass.NON_AGENT)
        assert "mosaic agent" in msg.lower(), (
            f"skip_message for NON_AGENT must mention 'MOSAIC agent'; got {msg!r}"
        )

    def test_skip_message_for_non_agent_matches_constant(self):
        """The non-agent skip message must match the SKIP_NON_AGENT template."""
        path = pathlib.Path("/dir") / "Cheap orchestrator.md"
        msg = fc.skip_message(path, fc.FileClass.NON_AGENT)
        expected = fc.SKIP_NON_AGENT.format(path=path)
        assert msg == expected, (
            f"skip_message for NON_AGENT must match SKIP_NON_AGENT template; "
            f"expected {expected!r}, got {msg!r}"
        )


class TestSkipMessageRaisesForNonSkippable:
    """skip_message must raise ValueError when called on a non-skippable FileClass."""

    def test_skip_message_raises_for_subagent(self):
        """skip_message must raise ValueError when file_class is SUBAGENT."""
        path = pathlib.Path("/dir") / "real-agent.md"
        with pytest.raises(ValueError):
            fc.skip_message(path, fc.FileClass.SUBAGENT)

    def test_skip_message_raises_for_orchestrator(self):
        """skip_message must raise ValueError when file_class is ORCHESTRATOR."""
        path = pathlib.Path("/dir") / "orchestrator.md"
        with pytest.raises(ValueError):
            fc.skip_message(path, fc.FileClass.ORCHESTRATOR)


# ---------------------------------------------------------------------------
# T5.1 — Skip message constants are defined
# ---------------------------------------------------------------------------

class TestSkipMessageConstants:
    """SKIP_UTILITY_AGENT and SKIP_NON_AGENT constants must be defined."""

    def test_skip_utility_agent_constant_exists(self):
        """SKIP_UTILITY_AGENT must be defined in file_classification."""
        assert hasattr(fc, "SKIP_UTILITY_AGENT"), (
            "file_classification must define SKIP_UTILITY_AGENT"
        )

    def test_skip_non_agent_constant_exists(self):
        """SKIP_NON_AGENT must be defined in file_classification."""
        assert hasattr(fc, "SKIP_NON_AGENT"), (
            "file_classification must define SKIP_NON_AGENT"
        )

    def test_skip_utility_agent_contains_path_placeholder(self):
        """SKIP_UTILITY_AGENT must contain a '{path}' format placeholder."""
        assert "{path}" in fc.SKIP_UTILITY_AGENT, (
            f"SKIP_UTILITY_AGENT must contain '{{path}}' placeholder; got {fc.SKIP_UTILITY_AGENT!r}"
        )

    def test_skip_non_agent_contains_path_placeholder(self):
        """SKIP_NON_AGENT must contain a '{path}' format placeholder."""
        assert "{path}" in fc.SKIP_NON_AGENT, (
            f"SKIP_NON_AGENT must contain '{{path}}' placeholder; got {fc.SKIP_NON_AGENT!r}"
        )
