"""Shared constants and helpers for MOSAIC boundary tools.

Both boundary_transformer.py and boundary_validator.py import from this module.
It defines the canonical boundary vocabulary, tag syntax, and parent mappings.
"""
from __future__ import annotations

import re
from enum import Enum


class BoundaryKind(Enum):
    SECTION = "SECTION"
    INJECTION = "INJECTION"


CANONICAL_SECTIONS: tuple[str, ...] = (
    "Identity",
    "CommunicationProtocol",
    "Capabilities",
    "Constraints",
    "ErrorHandling",
    "OutputFormat",
    "ExecutionPhilosophy",
)

CANONICAL_INJECTIONS: tuple[str, ...] = (
    "IdentityExtension",
    "ProtocolExtension",
    "LanguagePatterns",
    "CodebaseContext",
    "OutputArtifactTemplate",
    "HarnessConstraints",
    "CustomConstraints",
    "ErrorHandlingExtension",
    "ContextLimits",
    "SeverityThresholds",
    "SeverityDefinitions",
    "AvailableWorkflows",
)

INJECTION_PARENT_MAP: dict[str, str] = {
    "IdentityExtension": "Identity",
    "ProtocolExtension": "CommunicationProtocol",
    "LanguagePatterns": "Capabilities",
    "CodebaseContext": "Capabilities",
    "OutputArtifactTemplate": "Capabilities",
    "SeverityThresholds": "Capabilities",
    "SeverityDefinitions": "Capabilities",
    "HarnessConstraints": "Constraints",
    "CustomConstraints": "Constraints",
    "ErrorHandlingExtension": "ErrorHandling",
    "ContextLimits": "ExecutionPhilosophy",
    "AvailableWorkflows": "Identity",
}

KNOWN_FRONTMATTER_KEYS: frozenset[str] = frozenset({
    "id",
    "version",
    "name",
    "description",
    "model",
    "tools",
    "transform_version",
    "injections_version",
    # Harness-specific frontmatter fields carried by some concrete agent files.
    # These are preserved verbatim by the transformer and must be accepted by
    # the validator: OpenCode agents use `mode` and `permission`; agents that
    # rely on MCP tooling declare `mcpServers`.
    "mode",
    "permission",
    "mcpServers",
})

SECTION_HEADING_MAP: dict[str, str] = {
    "Identity": "# ",           # H1 -- matches any "# ... Agent" line
    "CommunicationProtocol": "## Communication Protocol",
    "Capabilities": "## Capabilities",
    "Constraints": "## Constraints",
    "ErrorHandling": "## Error Handling",
    "OutputFormat": "## Output Format",
    "ExecutionPhilosophy": "## Execution Philosophy",
}

INJECTION_OLD_MARKER_MAP: dict[str, str] = {
    "IdentityExtension": "[INJECTION: identity_extension]",
    "ProtocolExtension": "[INJECTION: protocol_extension]",
    "LanguagePatterns": "[INJECTION: language_patterns]",
    "CodebaseContext": "[INJECTION: codebase_context]",
    "OutputArtifactTemplate": "[INJECTION: output_artifact_template]",
    "HarnessConstraints": "[INJECTION: harness_constraints]",
    "CustomConstraints": "[INJECTION: custom_constraints]",
    "ErrorHandlingExtension": "[INJECTION: error_handling_extension]",
    "ContextLimits": "[INJECTION: context_limits]",
    "SeverityThresholds": "[INJECTION: severity_thresholds]",
    "SeverityDefinitions": "[INJECTION: severity_definitions]",
    "AvailableWorkflows": "[INJECTION: available_workflows]",
}

# Reverse map: old marker string -> canonical injection name
MARKER_TO_INJECTION_NAME: dict[str, str] = {
    v: k for k, v in INJECTION_OLD_MARKER_MAP.items()
}

TAG_PATTERN: re.Pattern[str] = re.compile(
    r"^\[\[(?P<close>/?)(?P<kind>SECTION|INJECTION):(?P<name>[A-Za-z]+)\]\]$"
)


def open_tag(kind: BoundaryKind, name: str) -> str:
    """Return e.g. '[[SECTION:Identity]]' or '[[INJECTION:ContextLimits]]'."""
    return f"[[{kind.value}:{name}]]"


def close_tag(kind: BoundaryKind, name: str) -> str:
    """Return e.g. '[[/SECTION:Identity]]' or '[[/INJECTION:ContextLimits]]'."""
    return f"[[/{kind.value}:{name}]]"
