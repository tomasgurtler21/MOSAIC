"""Shared constants and helpers for MOSAIC boundary tools.

Both boundary_transformer.py and boundary_validator.py import from this module.
It defines the canonical boundary vocabulary, tag syntax, and parent mappings.

Tag syntax (current):
  Open  : <Name type="core|managed|project|custom"> [name="qualifier"] [version="..."]
  Close : </Name>

Type attribute values map to boundary kinds:
  core    -> SECTION   (canonical sections)
  managed -> DEPLOYED  (tool-managed regions)
  project -> INJECTION (project-provided fill)
  custom  -> CUSTOM    (project-invented regions)
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


# Five canonical section names in document order.
# CommunicationProtocol is NOT a member — it is a tool-managed boundary name
# declared with [[DEPLOYED:]] and occupies position 2 in CANONICAL_ORDER.
# OutputFormat is NOT a member — it was a legacy section whose content is deleted
# outright during transform by delete_legacy_sections.
CANONICAL_SECTIONS: tuple[str, ...] = (
    "Identity",
    "Capabilities",
    "Constraints",
    "ErrorHandling",
    "ExecutionPhilosophy",
)

# Six canonical document slots in required order. The entry at index 1 is
# "CommunicationProtocol", satisfied by a top-level [[DEPLOYED:CommunicationProtocol]]
# boundary; every other entry is a section name. ArtifactProvenance and OutputFormat
# are removed.
# This is the list the document-order check walks.
CANONICAL_ORDER: tuple[str, ...] = (
    "Identity",
    "CommunicationProtocol",
    "Capabilities",
    "Constraints",
    "ErrorHandling",
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

# ---------------------------------------------------------------------------
# Tag grammar (new XML-style syntax)
#
# Open tag:  <TagName type="core|managed|project|custom" [name="qualifier"] [version="..."]>
# Close tag: </TagName>
#
# A line is a MOSAIC boundary only when it is a well-formed tag on its own line
# whose type attribute is one of the four recognised values.  Everything else is
# treated as literal text (inert).  Lenient parsing: attribute order is free,
# whitespace around attributes is unrestricted, single or double quotes accepted.
# ---------------------------------------------------------------------------

# Mapping between type attribute values and the BoundaryKind string values.
_TYPE_TO_KIND: dict[str, str] = {
    "core":    "SECTION",
    "managed": "DEPLOYED",
    "project": "INJECTION",
    "custom":  "CUSTOM",
}

# Reverse map — used by open_tag() and close_tag() to produce type attribute values.
_KIND_TO_TYPE: dict[str, str] = {v: k for k, v in _TYPE_TO_KIND.items()}

# Internal regexes used by _TagPatternMatcher.
_OPEN_TAG_RE: re.Pattern[str] = re.compile(
    r"^<(?P<tagname>[A-Za-z][A-Za-z0-9-]*)(?P<attrs>\s[^>]*)?>$"
)
_CLOSE_TAG_RE: re.Pattern[str] = re.compile(
    r"^</(?P<tagname>[A-Za-z][A-Za-z0-9-]*)\s*>$"
)
# Lenient attribute extractors — value may be single- or double-quoted.
_TYPE_ATTR_RE: re.Pattern[str] = re.compile(
    r"""(?:^|\s)type=["'](?P<type>core|managed|project|custom)["']"""
)
# The name attribute value may contain colons (e.g. name="id:sub" for deeper compound names).
_NAME_ATTR_RE: re.Pattern[str] = re.compile(
    r"""(?:^|\s)name=["'](?P<name>[A-Za-z][A-Za-z0-9:-]*)["']"""
)


class _TagMatchResult:
    """Lightweight match-result object returned by _TagPatternMatcher.match().

    Exposes .group(key) for the three logical fields so callers need not change
    from the old re.Match usage.

    close  : "/"  for a close tag, "" for an open tag
    kind   : "SECTION" / "DEPLOYED" / "INJECTION" / "CUSTOM" for open tags;
             "" for close tags (kind is not encoded in the new close-tag syntax)
    name   : compound name "Prefix:qualifier" for open tags;
             bare prefix "Prefix" for close tags
    """
    __slots__ = ("_close", "_kind", "_name")

    def __init__(self, close: str, kind: str, name: str) -> None:
        self._close = close
        self._kind = kind
        self._name = name

    def group(self, key: str) -> str:
        if key == "close":
            return self._close
        if key == "kind":
            return self._kind
        if key == "name":
            return self._name
        raise KeyError(f"Unknown group: {key!r}")


class _TagPatternMatcher:
    """Replacement for re.Pattern: recognises new-syntax MOSAIC boundary tags.

    Accepts open tags of the form:
        <Name type="core|managed|project|custom" [name="qualifier"] [version="..."]>
    and close tags of the form:
        </Name>

    Returns None for lines that are not MOSAIC boundary tags (inert text):
      - no type attribute
      - unrecognised type value
      - trailing content after the closing >
      - self-closing tag (ends with /)
    """

    def match(self, line: str) -> "_TagMatchResult | None":  # noqa: ANN201
        """Return a _TagMatchResult or None, mirroring re.Pattern.match semantics."""
        stripped = line.strip()

        # --- Close tag: </TagName> ---
        cm = _CLOSE_TAG_RE.match(stripped)
        if cm:
            tagname = cm.group("tagname")
            return _TagMatchResult(close="/", kind="", name=tagname)

        # --- Open tag: <TagName attrs...> ---
        om = _OPEN_TAG_RE.match(stripped)
        if om:
            tagname = om.group("tagname")
            attrs: str = om.group("attrs") or ""

            # Reject self-closing tags (<Foo type="core"/>).
            if attrs.rstrip().endswith("/"):
                return None

            # Require a recognised type attribute value.
            type_m = _TYPE_ATTR_RE.search(attrs)
            if not type_m:
                return None  # no type attribute → inert text
            type_val = type_m.group("type")
            kind = _TYPE_TO_KIND.get(type_val)
            if not kind:
                return None  # unrecognised type value → inert text

            # Optional name attribute (compound names).
            name_m = _NAME_ATTR_RE.search(attrs)
            qualifier = name_m.group("name") if name_m else None
            name = tagname + (":" + qualifier if qualifier else "")
            return _TagMatchResult(close="", kind=kind, name=name)

        return None


# Public tag-pattern object.  Callers use TAG_PATTERN.match(line) exactly as
# before, receiving a _TagMatchResult (or None) with .group("close"),
# .group("kind"), and .group("name").
TAG_PATTERN: _TagPatternMatcher = _TagPatternMatcher()


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

# Keys that only a generic source file may carry. Consumed by the deployment
# pipeline and stripped from deployed output, per AgentTemplateArchitecture.md
# §3.1 "Source only". These keys are never written to harness-path output.
GENERIC_ONLY_FRONTMATTER_KEYS: frozenset[str] = frozenset({
    "required_skills", "recommended_tier", "tier_rationale",
})

# Keys legal on any agent file (generic or harness) regardless of path.
# Equivalent to KNOWN_FRONTMATTER_KEYS minus HARNESS_ONLY_FRONTMATTER_KEYS
# minus GENERIC_ONLY_FRONTMATTER_KEYS minus BUNDLE_FRONTMATTER_KEYS.
AGENT_COMMON_FRONTMATTER_KEYS: frozenset[str] = (
    KNOWN_FRONTMATTER_KEYS
    - HARNESS_ONLY_FRONTMATTER_KEYS
    - GENERIC_ONLY_FRONTMATTER_KEYS
    - BUNDLE_FRONTMATTER_KEYS
)

# Per-kind allowlist consulted by the validator.
# Contract: KNOWN_FRONTMATTER_KEYS == set().union(*FRONTMATTER_KEYS_BY_KIND.values())
# The generic entry re-adds the generic-only keys; the harness entry does not.
FRONTMATTER_KEYS_BY_KIND: dict[DocumentKind, frozenset[str]] = {
    DocumentKind.AGENT_GENERIC: AGENT_COMMON_FRONTMATTER_KEYS | GENERIC_ONLY_FRONTMATTER_KEYS,
    DocumentKind.AGENT_HARNESS: AGENT_COMMON_FRONTMATTER_KEYS | HARNESS_ONLY_FRONTMATTER_KEYS,
    DocumentKind.BUNDLE: BUNDLE_FRONTMATTER_KEYS | frozenset({
        "id", "name", "description", "bundle_version",
    }),
}


def open_tag(kind: BoundaryKind, name: str) -> str:
    """Return the canonical open tag for a region.

    Simple name:   open_tag(SECTION, "Identity")             -> '<Identity type="core">'
    Compound name: open_tag(SECTION, "Workflow:quick-fix")   -> '<Workflow type="core" name="quick-fix">'

    A compound name is split at the first colon: the prefix becomes the tag name
    and the remainder becomes the 'name' attribute.
    """
    type_val = _KIND_TO_TYPE[kind.value]
    colon_pos = name.find(":")
    if colon_pos == -1:
        return f'<{name} type="{type_val}">'
    tag_name = name[:colon_pos]
    qualifier = name[colon_pos + 1:]
    return f'<{tag_name} type="{type_val}" name="{qualifier}">'


def close_tag(kind: BoundaryKind, name: str) -> str:
    """Return the canonical close tag for a region.

    Simple name:   close_tag(SECTION, "Identity")           -> '</Identity>'
    Compound name: close_tag(SECTION, "Workflow:quick-fix") -> '</Workflow>'

    Only the prefix of a compound name appears in the close tag.
    The 'kind' parameter is accepted for API compatibility but is not used —
    close tags carry only the tag name, not the type attribute.
    """
    colon_pos = name.find(":")
    if colon_pos == -1:
        return f"</{name}>"
    return f"</{name[:colon_pos]}>"
