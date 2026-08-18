"""Tests for deployed_blocks module: DEFAULT_BUNDLE_PATH Catalog/ migration.

Covers:
- DEFAULT_BUNDLE_PATH must include 'Catalog' as a path segment
- DEFAULT_BUNDLE_PATH must equal the expected Catalog/-prefixed absolute path
- DEFAULT_BUNDLE_PATH must not contain 'Catalog' more than once (double-prefix guard)
- The default bundle loads successfully from the Catalog/-prefixed path
- The loaded bundle's path field confirms the Catalog path was used

All tests in TestDefaultBundlePathCatalogPrefix are in TDD RED phase: they assert
the Catalog/-prefixed path is set and will fail until deployed_blocks.py is updated
with the Catalog segment in DEFAULT_BUNDLE_PATH.

Tests in TestDefaultBundleLoadsFromCatalog that assert on bundle.path contents are
also RED until the implementation is in place. The load-succeeds tests themselves
may pass in RED because the pre-migration path also exists on disk; they serve as
non-regression anchors that survive both the RED and GREEN phases.
"""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

import deployed_blocks  # noqa: E402

# Derive repo root the same way deployed_blocks.py does:
# module lives at <repo>/Tools/OldAgentsTransform/deployed_blocks.py
# two parent hops: OldAgentsTransform/ → Tools/ → <repo>
_REPO_ROOT = pathlib.Path(deployed_blocks.__file__).parent.parent.parent


# ---------------------------------------------------------------------------
# DEFAULT_BUNDLE_PATH value assertions
# ---------------------------------------------------------------------------

class TestDefaultBundlePathCatalogPrefix:
    """DEFAULT_BUNDLE_PATH must point at the Catalog/-prefixed DeployedSections.md.

    All tests in this class fail in RED: the current DEFAULT_BUNDLE_PATH is
    _REPO_ROOT / 'Agents' / 'Generic' / 'DeployedSections.md', which omits the
    'Catalog' segment. GREEN after the constant is corrected.
    """

    def test_default_bundle_path_has_catalog_segment(self):
        """DEFAULT_BUNDLE_PATH must include 'Catalog' as a path segment.

        Fails in RED: the current constant resolves to Agents/Generic/DeployedSections.md
        with no 'Catalog' segment.
        """
        assert "Catalog" in deployed_blocks.DEFAULT_BUNDLE_PATH.parts, (
            f"DEFAULT_BUNDLE_PATH ({deployed_blocks.DEFAULT_BUNDLE_PATH!r}) does not "
            "contain 'Catalog' as a path segment — the constant has not been migrated to "
            "the Catalog/ layout"
        )

    def test_default_bundle_path_value_is_catalog_prefixed(self):
        """DEFAULT_BUNDLE_PATH must equal <repo-root>/Catalog/DeployedSections.md.

        Fails in RED: the current value omits the 'Catalog' segment.
        """
        expected = _REPO_ROOT / "Catalog" / "DeployedSections.md"
        assert deployed_blocks.DEFAULT_BUNDLE_PATH == expected, (
            f"DEFAULT_BUNDLE_PATH must be {expected!r}; "
            f"got {deployed_blocks.DEFAULT_BUNDLE_PATH!r}"
        )

    def test_default_bundle_path_does_not_contain_catalog_twice(self):
        """DEFAULT_BUNDLE_PATH must contain 'Catalog' exactly once — double-prefix guard.

        Fails in RED: the current path has zero 'Catalog' occurrences, not one.
        GREEN after the fix, and guards against accidentally introducing
        'Catalog/Catalog/...' in the path.
        """
        parts = deployed_blocks.DEFAULT_BUNDLE_PATH.parts
        catalog_count = sum(1 for p in parts if p == "Catalog")
        assert catalog_count == 1, (
            f"DEFAULT_BUNDLE_PATH must contain 'Catalog' exactly once; "
            f"found {catalog_count} occurrence(s) in {deployed_blocks.DEFAULT_BUNDLE_PATH!r}. "
            "A count of 0 means the Catalog segment is missing; "
            "a count > 1 means a double-prefix was accidentally introduced."
        )


# ---------------------------------------------------------------------------
# Non-regression anchors for DEFAULT_BUNDLE_PATH (pass in both RED and GREEN)
# ---------------------------------------------------------------------------

class TestDefaultBundlePathBasicProperties:
    """Basic properties of DEFAULT_BUNDLE_PATH that are true regardless of migration state."""

    def test_default_bundle_path_is_absolute(self):
        """DEFAULT_BUNDLE_PATH must be an absolute path."""
        assert deployed_blocks.DEFAULT_BUNDLE_PATH.is_absolute(), (
            f"DEFAULT_BUNDLE_PATH must be an absolute path; "
            f"got {deployed_blocks.DEFAULT_BUNDLE_PATH!r}"
        )

    def test_default_bundle_path_name_is_deployed_sections(self):
        """DEFAULT_BUNDLE_PATH must name 'DeployedSections.md'."""
        assert deployed_blocks.DEFAULT_BUNDLE_PATH.name == "DeployedSections.md", (
            f"DEFAULT_BUNDLE_PATH must name 'DeployedSections.md'; "
            f"got {deployed_blocks.DEFAULT_BUNDLE_PATH.name!r}"
        )

    def test_default_bundle_path_exists(self):
        """DEFAULT_BUNDLE_PATH must point to an existing file."""
        assert deployed_blocks.DEFAULT_BUNDLE_PATH.exists(), (
            f"DEFAULT_BUNDLE_PATH ({deployed_blocks.DEFAULT_BUNDLE_PATH!r}) does not exist"
        )

    def test_default_bundle_path_is_file(self):
        """DEFAULT_BUNDLE_PATH must point to a regular file, not a directory."""
        assert deployed_blocks.DEFAULT_BUNDLE_PATH.is_file(), (
            f"DEFAULT_BUNDLE_PATH ({deployed_blocks.DEFAULT_BUNDLE_PATH!r}) is not a regular file"
        )


# ---------------------------------------------------------------------------
# Bundle load from Catalog path
# ---------------------------------------------------------------------------

class TestDefaultBundleLoadsFromCatalog:
    """The default bundle must load successfully from the Catalog/-prefixed path,
    and the loaded bundle's metadata must confirm the Catalog path was used."""

    def test_load_bundle_from_default_path_succeeds(self):
        """load_bundle(DEFAULT_BUNDLE_PATH) must not raise BundleError."""
        bundle = deployed_blocks.load_bundle(deployed_blocks.DEFAULT_BUNDLE_PATH)
        assert bundle is not None, (
            "load_bundle(DEFAULT_BUNDLE_PATH) must return a Bundle; got None"
        )

    def test_load_bundle_from_default_path_returns_bundle_type(self):
        """load_bundle(DEFAULT_BUNDLE_PATH) must return a Bundle instance."""
        bundle = deployed_blocks.load_bundle(deployed_blocks.DEFAULT_BUNDLE_PATH)
        assert isinstance(bundle, deployed_blocks.Bundle), (
            f"load_bundle must return a Bundle; got {type(bundle).__name__!r}"
        )

    def test_bundle_from_default_path_has_blocks(self):
        """The bundle loaded from DEFAULT_BUNDLE_PATH must contain at least one block."""
        bundle = deployed_blocks.load_bundle(deployed_blocks.DEFAULT_BUNDLE_PATH)
        assert len(bundle.blocks) > 0, (
            "Bundle loaded from DEFAULT_BUNDLE_PATH must contain at least one block"
        )

    def test_bundle_path_field_matches_default_bundle_path(self):
        """The loaded bundle's path field must equal DEFAULT_BUNDLE_PATH."""
        bundle = deployed_blocks.load_bundle(deployed_blocks.DEFAULT_BUNDLE_PATH)
        assert bundle.path == deployed_blocks.DEFAULT_BUNDLE_PATH, (
            f"bundle.path must equal DEFAULT_BUNDLE_PATH; "
            f"got {bundle.path!r}, expected {deployed_blocks.DEFAULT_BUNDLE_PATH!r}"
        )

    def test_bundle_path_field_has_catalog_segment(self):
        """The loaded bundle's path field must contain 'Catalog' — confirms the Catalog path was used.

        Fails in RED: DEFAULT_BUNDLE_PATH currently lacks 'Catalog', so bundle.path
        also lacks it. GREEN after the fix propagates through load.
        """
        bundle = deployed_blocks.load_bundle(deployed_blocks.DEFAULT_BUNDLE_PATH)
        assert "Catalog" in bundle.path.parts, (
            f"bundle.path {bundle.path!r} must contain 'Catalog' as a path segment — "
            "load_bundle used the pre-migration path instead of the Catalog/-prefixed one"
        )

    def test_load_bundle_with_catalog_path_directly(self):
        """load_bundle called with the Catalog path directly must succeed and return a valid Bundle.

        This test does NOT depend on DEFAULT_BUNDLE_PATH's value: it constructs the
        Catalog path explicitly from the repo root and verifies the bundle can be loaded
        from it. Serves as an existence check that the Catalog tree has a valid bundle file.
        """
        catalog_bundle_path = _REPO_ROOT / "Catalog" / "DeployedSections.md"
        assert catalog_bundle_path.exists(), (
            f"Catalog bundle file {catalog_bundle_path!r} must exist for this test to run"
        )
        bundle = deployed_blocks.load_bundle(catalog_bundle_path)
        assert isinstance(bundle, deployed_blocks.Bundle), (
            f"load_bundle with explicit Catalog path must return a Bundle; "
            f"got {type(bundle).__name__!r}"
        )
        assert len(bundle.blocks) > 0, (
            "Bundle loaded from the Catalog path must contain at least one block"
        )
