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
    DEPLOYED = "DEPLOYED"


# Seven canonical section names in document order.
# CommunicationProtocol is NOT a member — it is a tool-managed boundary name
# declared with [[DEPLOYED:]] and occupies position 2 in CANONICAL_ORDER.
CANONICAL_SECTIONS: tuple[str, ...] = (
    "Identity",
    "ArtifactProvenance",
    "Capabilities",
    "Constraints",
    "ErrorHandling",
    "OutputFormat",
    "ExecutionPhilosophy",
)

# Eight canonical document slots in required order.  Entry at index 1 is
# "CommunicationProtocol", satisfied by a top-level [[DEPLOYED:CommunicationProtocol]]
# boundary; every other entry is a section name.  This is the list the
# document-order check walks.
CANONICAL_ORDER: tuple[str, ...] = (
    "Identity",
    "CommunicationProtocol",
    "ArtifactProvenance",
    "Capabilities",
    "Constraints",
    "ErrorHandling",
    "OutputFormat",
    "ExecutionPhilosophy",
)

# Eight user-owned injection names.  Tool-managed names and ProtocolExtension
# are NOT members.
CANONICAL_INJECTIONS: tuple[str, ...] = (
    "IdentityExtension",
    "ArtifactProvenanceExtension",
    "CodebaseContext",
    "OutputArtifactTemplate",
    "ErrorHandlingExtension",
    "ContextLimits",
    "SeverityThresholds",
    "SeverityDefinitions",
)

# Six tool-managed boundary names.  These must be declared with [[DEPLOYED:]]
# in any document that uses them.
CANONICAL_DEPLOYED: tuple[str, ...] = (
    "CommunicationProtocol",
    "AvailableWorkflows",
    "InfrastructureAgents",
    "LanguagePatterns",
    "HarnessConstraints",
    "CustomConstraints",
)

# User-owned injection name -> required parent section.
# Tool-managed names and ProtocolExtension are NOT members.
INJECTION_PARENT_MAP: dict[str, str] = {
    "IdentityExtension": "Identity",
    "ArtifactProvenanceExtension": "ArtifactProvenance",
    "CodebaseContext": "Capabilities",
    "OutputArtifactTemplate": "Capabilities",
    "SeverityThresholds": "Capabilities",
    "SeverityDefinitions": "Capabilities",
    "ErrorHandlingExtension": "ErrorHandling",
    "ContextLimits": "ExecutionPhilosophy",
}

# Tool-managed boundary name -> required parent section.
# A value of None means the boundary must appear at body top level.
DEPLOYED_PARENT_MAP: dict[str, str | None] = {
    "CommunicationProtocol": None,   # top level — must not be nested in any section
    "AvailableWorkflows": "Identity",
    "InfrastructureAgents": "Identity",
    "LanguagePatterns": "Capabilities",
    "HarnessConstraints": "Constraints",
    "CustomConstraints": "Constraints",
}

# Canonical name -> the marker kind it must be declared with.
EXPECTED_MARKER: dict[str, BoundaryKind] = {
    **{name: BoundaryKind.INJECTION for name in CANONICAL_INJECTIONS},
    **{name: BoundaryKind.DEPLOYED for name in CANONICAL_DEPLOYED},
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
    # Deployment metadata added to generic source files.
    "recommended_tier",
    "tier_rationale",
    "required_skills",
    # Utility-agent-specific fields (e.g. base-version tracks the template version
    # the utility agent was built from).
    "base-version",
    # Harness-specific frontmatter fields carried by some concrete agent files.
    # These are preserved verbatim by the transformer and must be accepted by
    # the validator: OpenCode agents use `mode` and `permission`; agents that
    # rely on MCP tooling declare `mcpServers`.
    "mode",
    "permission",
    "mcpServers",
    # Infrastructure-agent-specific fields: class, trigger schedule, failure policy.
    "infrastructure",
    "triggers",
    "on_failure",
})

# Section name -> Markdown heading that introduces that section.
# CommunicationProtocol is not present: its heading arrives inside the deployed
# region content rather than being authored in the source file.
SECTION_HEADING_MAP: dict[str, str] = {
    "Identity": "# ",           # H1 -- matches any "# ... Agent" line
    "ArtifactProvenance": "## Artifact Provenance",
    "Capabilities": "## Capabilities",
    "Constraints": "## Constraints",
    "ErrorHandling": "## Error Handling",
    "OutputFormat": "## Output Format",
    "ExecutionPhilosophy": "## Execution Philosophy",
}

# Legacy pre-boundary-tag migration aids.  Unrelated to the vocabulary split
# and left unchanged.
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

# Name restriction is [A-Za-z]+ (pre-existing; compound names with colons or
# hyphens do not match and that is unchanged by this update).
TAG_PATTERN: re.Pattern[str] = re.compile(
    r"^\[\[(?P<close>/?)(?P<kind>SECTION|INJECTION|DEPLOYED):(?P<name>[A-Za-z]+)\]\]$"
)


def open_tag(kind: BoundaryKind, name: str) -> str:
    """Return e.g. '[[SECTION:Identity]]', '[[INJECTION:ContextLimits]]', or '[[DEPLOYED:LanguagePatterns]]'."""
    return f"[[{kind.value}:{name}]]"


def close_tag(kind: BoundaryKind, name: str) -> str:
    """Return e.g. '[[/SECTION:Identity]]', '[[/INJECTION:ContextLimits]]', or '[[/DEPLOYED:LanguagePatterns]]'."""
    return f"[[/{kind.value}:{name}]]"
