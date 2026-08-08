"""File classification for the boundary transformer tool.

Classifies files by base name to determine whether they should be skipped
(utility agents and non-MOSAIC-agent files) before any transform work begins.

Detection is by filename allowlist, matching the convention already used to
special-case orchestrator files in boundary_transformer.is_orchestrator_file().
Base names are lower-cased and matched exactly (no substring matching).
"""
from __future__ import annotations

import enum
import pathlib


class FileClass(enum.Enum):
    """Classification of a file by its role in the MOSAIC corpus."""
    SUBAGENT     = "subagent"
    ORCHESTRATOR = "orchestrator"
    UTILITY      = "utility"
    NON_AGENT    = "non_agent"


# Lower-cased base names (stem, extension removed) of utility agents.
# These files are outside the agent schema and must not be transformed.
UTILITY_AGENT_FILENAMES: frozenset[str] = frozenset({
    "transformation",
    "anthropic-agent-creator",
    "anthropic-subagent-creator",
    "workflow-creator",
    "orchestration-architect",
    "system-prompt-capturer",
})

# Lower-cased base names of files that are not MOSAIC agents at all.
NON_AGENT_FILENAMES: frozenset[str] = frozenset({
    "scl developer.chatmode",
    "cheap orchestrator",
})

# Skip message templates.  {path} is replaced with the file path at call time.
SKIP_UTILITY_AGENT: str = "{path}: skipped — utility agent, outside the agent schema"
SKIP_NON_AGENT: str     = "{path}: skipped — not a MOSAIC agent file"


def _file_base(path: pathlib.Path) -> str:
    """Return the lower-cased base name with trailing '.agent.md' or '.md' removed.

    Matches the convention in boundary_transformer.is_orchestrator_file() and
    batch_transform.file_base(), but applied case-insensitively.
    """
    name = path.name.lower()
    if name.endswith(".agent.md"):
        return name[: -len(".agent.md")]
    if name.endswith(".md"):
        return name[: -len(".md")]
    return name


def classify_file(path: pathlib.Path) -> FileClass:
    """Classify a file by base name only.

    The base name is ``path.name`` with a trailing '.agent.md' or '.md'
    removed, lower-cased.  Frontmatter and directory location are never
    consulted.

    Classification order (first match wins):
      1. ORCHESTRATOR — base name is 'orchestrator'
      2. NON_AGENT    — base name is in NON_AGENT_FILENAMES
      3. UTILITY      — base name is in UTILITY_AGENT_FILENAMES
      4. SUBAGENT     — everything else
    """
    base = _file_base(path)
    if base == "orchestrator":
        return FileClass.ORCHESTRATOR
    if base in NON_AGENT_FILENAMES:
        return FileClass.NON_AGENT
    if base in UTILITY_AGENT_FILENAMES:
        return FileClass.UTILITY
    return FileClass.SUBAGENT


def is_skippable(file_class: FileClass) -> bool:
    """Return True when the file class should be skipped without transformation.

    True for UTILITY and NON_AGENT; False for SUBAGENT and ORCHESTRATOR.
    """
    return file_class in (FileClass.UTILITY, FileClass.NON_AGENT)


def skip_message(path: pathlib.Path, file_class: FileClass) -> str:
    """Return the formatted skip message for a skippable file class.

    Raises ValueError when ``file_class`` is not skippable (SUBAGENT or
    ORCHESTRATOR).
    """
    if file_class is FileClass.UTILITY:
        return SKIP_UTILITY_AGENT.format(path=path)
    if file_class is FileClass.NON_AGENT:
        return SKIP_NON_AGENT.format(path=path)
    raise ValueError(
        f"skip_message called on non-skippable FileClass {file_class!r}; "
        "only UTILITY and NON_AGENT are skippable"
    )
