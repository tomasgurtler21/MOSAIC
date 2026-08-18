"""Document classification for MOSAIC boundary tools.

Classifies a document from its frontmatter as AGENT_GENERIC, AGENT_HARNESS, or BUNDLE.
This classification drives the per-kind frontmatter key allowlist in the validator and
the harness-only key stripping in the transformer.

New in Stage 6. Both boundary_transformer and boundary_validator import this module.
"""
from __future__ import annotations

from collections.abc import Collection, Mapping
from enum import Enum


class DocumentKind(Enum):
    """The kind of document, derived from frontmatter alone.

    Members:
        AGENT_GENERIC  — agent source file, harness-agnostic (no transform_version).
        AGENT_HARNESS  — agent source file carrying transform_version (harness-specific).
        BUNDLE         — DeployedSections.md or any other file with type: bundle.
    """
    AGENT_GENERIC = "agent_generic"
    AGENT_HARNESS = "agent_harness"
    BUNDLE        = "bundle"


def classify_document(
    frontmatter_keys: Collection[str],
    frontmatter_values: Mapping[str, str],
) -> DocumentKind:
    """Classify a document from its frontmatter alone.

    Precedence, first match wins:
      1. frontmatter_values.get("type") == "bundle"   -> BUNDLE
      2. "transform_version" in frontmatter_keys      -> AGENT_HARNESS
      3. otherwise                                    -> AGENT_GENERIC

    Deliberately mirrors the transformer's own path selection so the validator
    and the transformer never disagree about which kind of file they are looking
    at. Takes parsed frontmatter rather than a path: classification must not
    depend on file location.

    Args:
        frontmatter_keys:   The set of keys present in the document's frontmatter.
        frontmatter_values: A mapping of key -> value for the frontmatter.

    Returns:
        The DocumentKind for this document.
    """
    if frontmatter_values.get("type") == "bundle":
        return DocumentKind.BUNDLE
    if "transform_version" in frontmatter_keys:
        return DocumentKind.AGENT_HARNESS
    return DocumentKind.AGENT_GENERIC
