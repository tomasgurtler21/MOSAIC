"""Shared constants and helpers for MOSAIC boundary tools.

Both boundary_transformer.py and boundary_validator.py import from this module.
It defines the canonical boundary vocabulary, tag syntax, and parent mappings.
"""
from __future__ import annotations

import re
from enum import Enum

from document_kind import DocumentKind


class BoundaryKind(Enum):
    SECTION = "SECTION"
    INJECTION = "INJECTION"
    DEPLOYED = "DEPLOYED"
    CUSTOM = "CUSTOM"


# Six canonical section names in document order.
# CommunicationProtocol is NOT a member — it is a tool-managed boundary name
# declared with [[DEPLOYED:]] and occupies position 2 in CANONICAL_ORDER.
CANONICAL_SECTIONS: tuple[str, ...] = (
    "Identity",
    "Capabilities",
    "Constraints",
    "ErrorHandling",
    "OutputFormat",
    "ExecutionPhilosophy",
)

# Seven canonical document slots in required order. The entry at index 1 is
# "CommunicationProtocol", satisfied by a top-level [[DEPLOYED:CommunicationProtocol]]
# boundary; every other entry is a section name. ArtifactProvenance is removed.
# This is the list the document-order check walks.
CANONICAL_ORDER: tuple[str, ...] = (
    "Identity",
    "CommunicationProtocol",
    "Capabilities",
    "Constraints",
    "ErrorHandling",
    "OutputFormat",
    "ExecutionPhilosophy",
)

# Nine tool-managed boundary names, a closed set.
# These must be declared with [[DEPLOYED:]] in any document that uses them.
# ArtifactProvenance is removed; AuthorityHierarchy, ClosingProcedure,
# ProtocolConstraints, ErrorHandlingCommon, and ExecutionPhilosophyCommon are added.
# LanguagePatterns moves to INJECTION_PARENT_MAP; CustomConstraints is deleted
# outright (it is never tool-managed, only an advisory old-marker name).
CANONICAL_DEPLOYED: tuple[str, ...] = (
    "CommunicationProtocol",
    "AuthorityHierarchy",
    "ClosingProcedure",
    "AvailableWorkflows",
    "InfrastructureAgents",
    "ProtocolConstraints",
    "HarnessConstraints",
    "ErrorHandlingCommon",
    "ExecutionPhilosophyCommon",
)

# Advisory injection name -> usual parent section.
# Injection names are open: an unlisted name is valid and preserved like any other.
# This table is consulted for advisory reporting only, never enforcement.
# A value of None means the injection usually appears at body top level (not inside
# any section). An absent key means no usual parent is recorded.
# ArtifactProvenanceExtension is removed; ProtocolExtension and LanguagePatterns
# are added.
INJECTION_PARENT_MAP: dict[str, str | None] = {
    "ProtocolExtension": None,      # top level
    "IdentityExtension": "Identity",
    "CodebaseContext": "Capabilities",
    "OutputArtifactTemplate": "Capabilities",
    "SeverityThresholds": "Capabilities",
    "SeverityDefinitions": "Capabilities",
    "LanguagePatterns": "Capabilities",
    "ErrorHandlingExtension": "ErrorHandling",
    "ContextLimits": "ExecutionPhilosophy",
}

# Tool-managed boundary name -> required parent section.
# A value of None means the boundary must appear at body top level.
# Nine entries mirroring the Go DeployedParent map.
DEPLOYED_PARENT_MAP: dict[str, str | None] = {
    "CommunicationProtocol": None,      # top level — must not be nested in any section
    "AuthorityHierarchy": "Identity",
    "ClosingProcedure": "Identity",
    "AvailableWorkflows": "Identity",
    "InfrastructureAgents": "Identity",
    "ProtocolConstraints": "Constraints",
    "HarnessConstraints": "Constraints",
    "ErrorHandlingCommon": "ErrorHandling",
    "ExecutionPhilosophyCommon": "ExecutionPhilosophy",
}

# Canonical name -> the marker kind it must be declared with.
# Rebuilt from the deployed names only: injection names are open and have no allowlist.
EXPECTED_MARKER: dict[str, BoundaryKind] = {
    name: BoundaryKind.DEPLOYED for name in CANONICAL_DEPLOYED
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
    # Source-declared role field: "subagent" or "orchestrator". Declared by every
    # migrated agent source file. Added here before the migration batches run so the
    # validator does not reject migrated files with E009.
    "role",
    # Tool-written stamp: the bundle version deployed into every subagent file.
    # This key is never authored in source; it is stamped by the deployment tool.
    # Registered here so the validator accepts deployed trees without E009.
    "bundle_version",
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
    # Bundle document keys (DeployedSections.md and any other type: bundle file).
    # These four keys are also the members of BUNDLE_FRONTMATTER_KEYS, included
    # here so KNOWN_FRONTMATTER_KEYS remains the union of all per-kind key sets.
    "type",
    "author",
    "status",
    "blocks",
})

# Section name -> Markdown heading that introduces that section.
# CommunicationProtocol is not present: its heading arrives inside the deployed
# region content rather than being authored in the source file.
SECTION_HEADING_MAP: dict[str, str] = {
    "Identity": "# ",           # H1 -- matches any "# ... Agent" line
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

# Name admits compound forms `Prefix:id` and hyphenated segments, matching the
# Go parser in Tools/Common/docformat/boundary.go. Each segment must start with
# a letter; digits and hyphens are allowed after the first character of a segment.
# A trailing colon or a segment starting with a digit or hyphen does not match.
TAG_PATTERN: re.Pattern[str] = re.compile(
    r"^\[\[(?P<close>/?)(?P<kind>SECTION|INJECTION|DEPLOYED|CUSTOM):"
    r"(?P<name>[A-Za-z][A-Za-z0-9-]*(?::[A-Za-z][A-Za-z0-9-]*)*)\]\]$"
)


def tag_base_name(name: str) -> str:
    """Return the portion of a tag name before the first colon.

    'AuthorityHierarchy:Subagent' -> 'AuthorityHierarchy'
    'Identity'                    -> 'Identity'

    Used where a compound name should be treated as an instance of its base
    name (the idempotency guard). NOT used by the validator's canonical-name
    check, which is deliberately strict for agent documents.
    """
    colon_pos = name.find(":")
    if colon_pos == -1:
        return name
    return name[:colon_pos]


def tag_qualifier(name: str) -> str | None:
    """Return the portion after the first colon, or None when the name is simple.

    'AuthorityHierarchy:Subagent' -> 'Subagent'
    'Identity'                    -> None
    """
    colon_pos = name.find(":")
    if colon_pos == -1:
        return None
    return name[colon_pos + 1:]


# Keys that only a bundle document carries; no agent file uses these.
BUNDLE_FRONTMATTER_KEYS: frozenset[str] = frozenset({
    "type", "author", "status", "blocks",
})

# ---------------------------------------------------------------------------
# Stage 6: per-kind frontmatter key sets
#
# These three constants are stubs; their values are populated by Stage 6's
# implementation (I6.5). Tests asserting on their contents are in TDD RED
# phase and will fail until I6.5 is complete.
#
# Contract:
#   KNOWN_FRONTMATTER_KEYS == set().union(*FRONTMATTER_KEYS_BY_KIND.values())
# ---------------------------------------------------------------------------

# Keys that only a harness-path file may carry.  On the generic transform
# path, these are stripped from the output.  On the harness path, they are
# preserved verbatim.
HARNESS_ONLY_FRONTMATTER_KEYS: frozenset[str] = frozenset({
    "mode", "permission", "model", "transform_version",
})

# Keys legal on any agent file (generic or harness) regardless of path.
# Equivalent to KNOWN_FRONTMATTER_KEYS minus HARNESS_ONLY_FRONTMATTER_KEYS
# minus BUNDLE_FRONTMATTER_KEYS.
AGENT_COMMON_FRONTMATTER_KEYS: frozenset[str] = (
    KNOWN_FRONTMATTER_KEYS - HARNESS_ONLY_FRONTMATTER_KEYS - BUNDLE_FRONTMATTER_KEYS
)

# Per-kind allowlist consulted by the validator.
# Contract: KNOWN_FRONTMATTER_KEYS == set().union(*FRONTMATTER_KEYS_BY_KIND.values())
FRONTMATTER_KEYS_BY_KIND: dict[DocumentKind, frozenset[str]] = {
    DocumentKind.AGENT_GENERIC: AGENT_COMMON_FRONTMATTER_KEYS,
    DocumentKind.AGENT_HARNESS: AGENT_COMMON_FRONTMATTER_KEYS | HARNESS_ONLY_FRONTMATTER_KEYS,
    DocumentKind.BUNDLE: BUNDLE_FRONTMATTER_KEYS | frozenset({
        "id", "name", "description", "bundle_version",
    }),
}


def open_tag(kind: BoundaryKind, name: str) -> str:
    """Return e.g. '[[SECTION:Identity]]', '[[INJECTION:ContextLimits]]', or '[[DEPLOYED:LanguagePatterns]]'."""
    return f"[[{kind.value}:{name}]]"


def close_tag(kind: BoundaryKind, name: str) -> str:
    """Return e.g. '[[/SECTION:Identity]]', '[[/INJECTION:ContextLimits]]', or '[[/DEPLOYED:LanguagePatterns]]'."""
    return f"[[/{kind.value}:{name}]]"
