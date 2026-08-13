"""Tests for the generic_lookup module.

Covers: repo-root resolution (including cwd independence), the expected values of
GENERIC_AGENTS_ROOT and GENERIC_ORCHESTRATOR_PATH against the restructured Catalog/
layout, build_generic_map() contents and error handling, file_base() double-extension
semantics, and find_generic_ref() lookup and fallback behaviour.

Tests in TestGenericAgentsRootValue and TestGenericOrchestratorPathValue are in TDD
RED phase: they assert the final Catalog/-prefixed constant values and will fail until
generic_lookup.py is updated to use the new paths.
"""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

import generic_lookup  # noqa: E402


# ---------------------------------------------------------------------------
# Repo-root resolution
# ---------------------------------------------------------------------------

class TestRepoRootResolution:
    """REPO_ROOT must point to the actual mosaic repo root, resolved from __file__."""

    def test_repo_root_catalog_subagents_tree_exists(self):
        """REPO_ROOT / 'Catalog' / 'Subagents' must be an existing directory."""
        assert (generic_lookup.REPO_ROOT / "Catalog" / "Subagents").is_dir(), (
            f"REPO_ROOT ({generic_lookup.REPO_ROOT!r}) does not contain "
            "'Catalog/Subagents' — repo-root resolution is wrong"
        )

    def test_repo_root_is_cwd_independent(self, tmp_path, monkeypatch):
        """REPO_ROOT must resolve to the real repo root regardless of process cwd.

        REPO_ROOT is anchored to __file__ and evaluated at module import time, so
        its value is frozen. This test verifies that frozen value is correct even
        when the process cwd is an unrelated directory — the direct test of the
        FR-5b requirement.
        """
        expected = generic_lookup.REPO_ROOT
        monkeypatch.chdir(tmp_path)
        assert (expected / "Catalog" / "Subagents").is_dir(), (
            "REPO_ROOT must resolve correctly even when cwd is changed to an unrelated directory"
        )

    def test_generic_agents_root_constant_is_directory(self):
        """GENERIC_AGENTS_ROOT must point to an existing directory."""
        assert generic_lookup.GENERIC_AGENTS_ROOT.is_dir(), (
            f"GENERIC_AGENTS_ROOT ({generic_lookup.GENERIC_AGENTS_ROOT!r}) is not a directory"
        )

    def test_generic_orchestrator_path_exists(self):
        """GENERIC_ORCHESTRATOR_PATH must point to an existing file."""
        assert generic_lookup.GENERIC_ORCHESTRATOR_PATH.exists(), (
            f"GENERIC_ORCHESTRATOR_PATH ({generic_lookup.GENERIC_ORCHESTRATOR_PATH!r}) "
            "does not exist"
        )

    def test_repo_root_is_not_tools_dir(self):
        """REPO_ROOT must not resolve to the Tools/ directory — that is the FR-5a bug."""
        assert generic_lookup.REPO_ROOT.name != "Tools", (
            f"REPO_ROOT resolved to {generic_lookup.REPO_ROOT!r}, which ends at 'Tools/'. "
            "Correct resolution is two parent hops above the module's own directory."
        )


# ---------------------------------------------------------------------------
# GENERIC_AGENTS_ROOT value assertions (TDD RED — fails until I5.2)
# ---------------------------------------------------------------------------

class TestGenericAgentsRootValue:
    """GENERIC_AGENTS_ROOT must equal REPO_ROOT / 'Catalog' / 'Subagents'.

    All tests in this class fail in RED: the current constant is
    REPO_ROOT / 'Agents' / 'Generic' / 'Agents', which uses the old path shape.
    GREEN after the constant is corrected to the restructured Catalog/ layout.
    """

    def test_generic_agents_root_value_equals_catalog_subagents(self):
        """GENERIC_AGENTS_ROOT must equal REPO_ROOT / 'Catalog' / 'Subagents'.

        Fails in RED: the current value uses the old 'Agents/Generic/Agents' shape.
        """
        expected = generic_lookup.REPO_ROOT / "Catalog" / "Subagents"
        assert generic_lookup.GENERIC_AGENTS_ROOT == expected, (
            f"GENERIC_AGENTS_ROOT must be {expected!r}; "
            f"got {generic_lookup.GENERIC_AGENTS_ROOT!r}"
        )

    def test_generic_agents_root_has_catalog_segment(self):
        """GENERIC_AGENTS_ROOT must include 'Catalog' as a path segment.

        Fails in RED: the current constant resolves to Agents/Generic/Agents
        with no 'Catalog' segment.
        """
        assert "Catalog" in generic_lookup.GENERIC_AGENTS_ROOT.parts, (
            f"GENERIC_AGENTS_ROOT ({generic_lookup.GENERIC_AGENTS_ROOT!r}) does not "
            "contain 'Catalog' as a path segment — the constant has not been migrated "
            "to the Catalog/ layout"
        )

    def test_generic_agents_root_catalog_segment_appears_exactly_once(self):
        """GENERIC_AGENTS_ROOT must contain 'Catalog' exactly once — double-prefix guard.

        Fails in RED: the current path has zero 'Catalog' occurrences.
        GREEN after the fix, and guards against accidentally introducing
        'Catalog/Catalog/...' in the path.
        """
        parts = generic_lookup.GENERIC_AGENTS_ROOT.parts
        catalog_count = sum(1 for p in parts if p == "Catalog")
        assert catalog_count == 1, (
            f"GENERIC_AGENTS_ROOT must contain 'Catalog' exactly once; "
            f"found {catalog_count} occurrence(s) in {generic_lookup.GENERIC_AGENTS_ROOT!r}. "
            "A count of 0 means the Catalog segment is missing; "
            "a count > 1 means a double-prefix was accidentally introduced."
        )


# ---------------------------------------------------------------------------
# GENERIC_ORCHESTRATOR_PATH value assertions (TDD RED — fails until I5.2)
# ---------------------------------------------------------------------------

class TestGenericOrchestratorPathValue:
    """GENERIC_ORCHESTRATOR_PATH must equal REPO_ROOT / 'Catalog' / 'Orchestrator' / 'orchestrator.md'.

    All tests in this class fail in RED: the current constant is
    REPO_ROOT / 'Agents' / 'Generic' / 'Orchestrator' / 'orchestrator.md',
    which uses the old path shape.
    GREEN after the constant is corrected to the restructured Catalog/ layout.
    """

    def test_generic_orchestrator_path_value_equals_catalog_orchestrator(self):
        """GENERIC_ORCHESTRATOR_PATH must equal REPO_ROOT / 'Catalog' / 'Orchestrator' / 'orchestrator.md'.

        Fails in RED: the current value uses the old 'Agents/Generic/Orchestrator' shape.
        """
        expected = generic_lookup.REPO_ROOT / "Catalog" / "Orchestrator" / "orchestrator.md"
        assert generic_lookup.GENERIC_ORCHESTRATOR_PATH == expected, (
            f"GENERIC_ORCHESTRATOR_PATH must be {expected!r}; "
            f"got {generic_lookup.GENERIC_ORCHESTRATOR_PATH!r}"
        )

    def test_generic_orchestrator_path_has_catalog_segment(self):
        """GENERIC_ORCHESTRATOR_PATH must include 'Catalog' as a path segment.

        Fails in RED: the current constant uses the old 'Agents/Generic/Orchestrator'
        shape with no 'Catalog' segment.
        """
        assert "Catalog" in generic_lookup.GENERIC_ORCHESTRATOR_PATH.parts, (
            f"GENERIC_ORCHESTRATOR_PATH ({generic_lookup.GENERIC_ORCHESTRATOR_PATH!r}) "
            "does not contain 'Catalog' as a path segment — the constant has not been "
            "migrated to the Catalog/ layout"
        )

    def test_generic_orchestrator_path_catalog_segment_appears_exactly_once(self):
        """GENERIC_ORCHESTRATOR_PATH must contain 'Catalog' exactly once — double-prefix guard.

        Fails in RED: the current path has zero 'Catalog' occurrences.
        GREEN after the fix, and guards against accidentally introducing
        'Catalog/Catalog/...' in the path.
        """
        parts = generic_lookup.GENERIC_ORCHESTRATOR_PATH.parts
        catalog_count = sum(1 for p in parts if p == "Catalog")
        assert catalog_count == 1, (
            f"GENERIC_ORCHESTRATOR_PATH must contain 'Catalog' exactly once; "
            f"found {catalog_count} occurrence(s) in {generic_lookup.GENERIC_ORCHESTRATOR_PATH!r}. "
            "A count of 0 means the Catalog segment is missing; "
            "a count > 1 means a double-prefix was accidentally introduced."
        )


# ---------------------------------------------------------------------------
# build_generic_map()
# ---------------------------------------------------------------------------

class TestBuildGenericMap:
    """build_generic_map() must return a non-empty, correctly indexed mapping."""

    def test_build_generic_map_returns_dict(self):
        """build_generic_map() must return a dict."""
        result = generic_lookup.build_generic_map()
        assert isinstance(result, dict), (
            f"build_generic_map() must return a dict; got {type(result).__name__!r}"
        )

    def test_build_generic_map_is_nonempty(self):
        """build_generic_map() must return a non-empty mapping for the real repo tree."""
        result = generic_lookup.build_generic_map()
        assert len(result) > 0, (
            "build_generic_map() must return a non-empty mapping; "
            "check that REPO_ROOT is correct and the Catalog/Subagents tree exists"
        )

    def test_build_generic_map_contains_contracts_designer(self):
        """build_generic_map() must include a 'contracts-designer' key."""
        result = generic_lookup.build_generic_map()
        assert "contracts-designer" in result, (
            "'contracts-designer' must be present in the generic map; "
            f"map is empty or missing this key. REPO_ROOT: {generic_lookup.REPO_ROOT!r}"
        )

    def test_contracts_designer_maps_to_correct_path(self):
        """The 'contracts-designer' entry must point to the real Planning agent file."""
        result = generic_lookup.build_generic_map()
        path = result.get("contracts-designer")
        assert path is not None, "'contracts-designer' key must be present in the map"
        assert path.exists(), f"contracts-designer path {path!r} does not exist"
        assert path.name == "contracts-designer.md", (
            f"Expected filename 'contracts-designer.md'; got {path.name!r}"
        )
        # The path must live under the Catalog/Subagents tree, not under Tools/
        assert "Subagents" in path.parts, (
            f"contracts-designer path {path!r} is not under the Catalog/Subagents tree"
        )

    def test_build_generic_map_excludes_readme_keys(self):
        """build_generic_map() must not yield any key derived from a README.md file."""
        result = generic_lookup.build_generic_map()
        readme_keys = [k for k in result if "readme" in k.lower()]
        assert not readme_keys, (
            f"build_generic_map() must skip README.md files; "
            f"found unexpected keys: {readme_keys!r}"
        )

    def test_build_generic_map_excludes_readme_values(self):
        """build_generic_map() must not include paths that point to README.md files."""
        result = generic_lookup.build_generic_map()
        readme_values = [str(v) for v in result.values() if v.name == "README.md"]
        assert not readme_values, (
            f"build_generic_map() must not include README.md paths; "
            f"found: {readme_values!r}"
        )

    def test_build_generic_map_has_orchestrator_entry(self):
        """build_generic_map() must include an 'orchestrator' key (special-cased entry)."""
        result = generic_lookup.build_generic_map()
        assert "orchestrator" in result, (
            "'orchestrator' key must be present (added from GENERIC_ORCHESTRATOR_PATH "
            "when that file exists)"
        )

    def test_orchestrator_entry_points_to_existing_file(self):
        """The 'orchestrator' entry must point to an existing orchestrator.md."""
        result = generic_lookup.build_generic_map()
        orch_path = result.get("orchestrator")
        assert orch_path is not None, "'orchestrator' key must be present in the map"
        assert orch_path.exists(), f"Orchestrator path {orch_path!r} does not exist"
        assert orch_path.name == "orchestrator.md", (
            f"Orchestrator entry must point to 'orchestrator.md'; got {orch_path.name!r}"
        )

    def test_build_generic_map_values_are_paths(self):
        """All values in the generic map must be pathlib.Path instances."""
        result = generic_lookup.build_generic_map()
        non_paths = [(k, type(v).__name__) for k, v in result.items()
                     if not isinstance(v, pathlib.Path)]
        assert not non_paths, (
            f"All map values must be pathlib.Path objects; "
            f"non-Path entries found: {non_paths!r}"
        )

    def test_build_generic_map_returns_empty_for_missing_tree(self, tmp_path, monkeypatch):
        """build_generic_map() must return {} rather than raising when the tree is absent.

        The error-handling contract: missing GENERIC_AGENTS_ROOT → empty dict, never an exception.
        Verified by patching GENERIC_AGENTS_ROOT to a non-existent path.
        """
        monkeypatch.setattr(generic_lookup, "GENERIC_AGENTS_ROOT", tmp_path / "nonexistent")
        monkeypatch.setattr(
            generic_lookup, "GENERIC_ORCHESTRATOR_PATH", tmp_path / "nonexistent_orch.md"
        )
        result = generic_lookup.build_generic_map()
        assert isinstance(result, dict), (
            "build_generic_map() must return a dict even when the tree is missing"
        )
        assert result == {}, (
            f"build_generic_map() must return an empty dict when GENERIC_AGENTS_ROOT "
            f"does not exist; got: {result!r}"
        )

    def test_build_generic_map_is_cwd_independent(self, tmp_path, monkeypatch):
        """build_generic_map() must yield the same content regardless of process cwd.

        A caller may invoke this function from any working directory; the result must
        be identical because the glob is rooted at GENERIC_AGENTS_ROOT, not at cwd.
        """
        result_default = generic_lookup.build_generic_map()
        monkeypatch.chdir(tmp_path)
        result_different_cwd = generic_lookup.build_generic_map()
        assert "contracts-designer" in result_different_cwd, (
            "build_generic_map() must find 'contracts-designer' even when cwd is changed"
        )
        assert len(result_different_cwd) == len(result_default), (
            "build_generic_map() must return the same size map regardless of cwd; "
            f"default cwd size={len(result_default)}, "
            f"changed cwd size={len(result_different_cwd)}"
        )

    def test_build_generic_map_stem_collision_last_writer_wins(self, tmp_path, monkeypatch):
        """build_generic_map() yields exactly one entry when two files share the same stem.

        The build_generic_map() docstring states: "last writer wins on a stem collision;
        no disambiguation is performed." This test verifies that contract:
        - No exception is raised when a stem appears in more than one subdirectory.
        - The map contains exactly one entry for the colliding stem.
        - The winning path is one of the two collision files (not an error sentinel).

        Uses a monkeypatched GENERIC_AGENTS_ROOT so no real repo files are mutated.
        """
        subdir_a = tmp_path / "SubdirA"
        subdir_b = tmp_path / "SubdirB"
        subdir_a.mkdir()
        subdir_b.mkdir()
        (subdir_a / "collision.md").write_text("# collision version A", encoding="utf-8")
        (subdir_b / "collision.md").write_text("# collision version B", encoding="utf-8")

        monkeypatch.setattr(generic_lookup, "GENERIC_AGENTS_ROOT", tmp_path)
        monkeypatch.setattr(
            generic_lookup, "GENERIC_ORCHESTRATOR_PATH", tmp_path / "nonexistent_orch.md"
        )

        result = generic_lookup.build_generic_map()

        assert "collision" in result, (
            "build_generic_map() must contain a 'collision' key when files with "
            "that stem exist in the tree"
        )
        collision_count = sum(1 for k in result if k == "collision")
        assert collision_count == 1, (
            f"build_generic_map() must contain exactly one entry for a colliding stem "
            f"(last writer wins, no duplication); found {collision_count} entries"
        )
        winning_path = result["collision"]
        expected_paths = {subdir_a / "collision.md", subdir_b / "collision.md"}
        assert winning_path in expected_paths, (
            f"The winning path {winning_path!r} must be one of the two collision files; "
            f"expected one of {expected_paths!r}"
        )


# ---------------------------------------------------------------------------
# file_base()
# ---------------------------------------------------------------------------

class TestFileBase:
    """file_base() must extract the logical stem, handling the .agent.md double extension."""

    def test_file_base_agent_md_double_extension(self):
        """file_base strips both .agent.md to yield the stem before the first dot."""
        result = generic_lookup.file_base(pathlib.Path("contracts-designer.agent.md"))
        assert result == "contracts-designer", (
            f"file_base('contracts-designer.agent.md') must return 'contracts-designer'; "
            f"got {result!r}"
        )

    def test_file_base_plain_md_extension(self):
        """file_base strips .md from a plain filename."""
        result = generic_lookup.file_base(pathlib.Path("contracts-designer.md"))
        assert result == "contracts-designer", (
            f"file_base('contracts-designer.md') must return 'contracts-designer'; "
            f"got {result!r}"
        )

    def test_file_base_hyphenated_name_agent_md(self):
        """file_base preserves hyphens and strips .agent.md."""
        result = generic_lookup.file_base(pathlib.Path("planner-tdd-soft.agent.md"))
        assert result == "planner-tdd-soft", (
            f"file_base('planner-tdd-soft.agent.md') must return 'planner-tdd-soft'; "
            f"got {result!r}"
        )

    def test_file_base_with_nested_path_md(self):
        """file_base extracts the stem from a path with multiple directory components."""
        result = generic_lookup.file_base(
            pathlib.Path("/repo/Agents/Generic/Agents/Planning/contracts-designer.md")
        )
        assert result == "contracts-designer", (
            f"file_base must work with a nested path; got {result!r}"
        )

    def test_file_base_with_nested_path_agent_md(self):
        """file_base strips .agent.md even when the path has directory components."""
        result = generic_lookup.file_base(
            pathlib.Path("/repo/Agents/Claude Code/CodebaseAgnostic/Agents/foo.agent.md")
        )
        assert result == "foo", (
            f"file_base must strip .agent.md from a nested path; got {result!r}"
        )

    def test_file_base_short_name_md(self):
        """file_base works for a short filename with .md."""
        result = generic_lookup.file_base(pathlib.Path("orchestrator.md"))
        assert result == "orchestrator", (
            f"file_base('orchestrator.md') must return 'orchestrator'; got {result!r}"
        )

    def test_file_base_agent_md_not_stem_only(self):
        """file_base must strip the full .agent.md suffix, not rely on pathlib.stem.

        pathlib.Path('foo.agent.md').stem returns 'foo.agent' (wrong).
        file_base must return 'foo' (correct).
        """
        result = generic_lookup.file_base(pathlib.Path("foo.agent.md"))
        assert result == "foo", (
            f"file_base('foo.agent.md') must return 'foo', not 'foo.agent'; got {result!r}"
        )

    def test_file_base_single_char_stem(self):
        """file_base works for a one-character stem."""
        result = generic_lookup.file_base(pathlib.Path("x.md"))
        assert result == "x", (
            f"file_base('x.md') must return 'x'; got {result!r}"
        )


# ---------------------------------------------------------------------------
# find_generic_ref()
# ---------------------------------------------------------------------------

class TestFindGenericRef:
    """find_generic_ref() must return the matching Path or None, using file_base semantics."""

    def test_returns_path_when_stem_matches(self, tmp_path):
        """find_generic_ref returns the mapped Path when the stem is present in the map."""
        generic_file = tmp_path / "foo.md"
        generic_file.write_text("# foo", encoding="utf-8")
        result = generic_lookup.find_generic_ref(
            pathlib.Path("foo.md"),
            generic_map={"foo": generic_file},
        )
        assert result == generic_file, (
            f"find_generic_ref must return the mapped path; got {result!r}"
        )

    def test_returns_none_when_stem_absent(self, tmp_path):
        """find_generic_ref returns None when the stem is not in the map."""
        generic_file = tmp_path / "other.md"
        generic_file.write_text("# other", encoding="utf-8")
        result = generic_lookup.find_generic_ref(
            pathlib.Path("not-in-map.md"),
            generic_map={"other": generic_file},
        )
        assert result is None, (
            f"find_generic_ref must return None when the stem is absent from the map; "
            f"got {result!r}"
        )

    def test_returns_none_for_empty_map(self):
        """find_generic_ref returns None when the map is empty."""
        result = generic_lookup.find_generic_ref(
            pathlib.Path("anything.md"),
            generic_map={},
        )
        assert result is None, (
            f"find_generic_ref must return None for an empty map; got {result!r}"
        )

    def test_uses_file_base_for_agent_md(self, tmp_path):
        """find_generic_ref applies file_base semantics: strips .agent.md to find the key."""
        generic_file = tmp_path / "foo.md"
        generic_file.write_text("# foo", encoding="utf-8")
        result = generic_lookup.find_generic_ref(
            pathlib.Path("foo.agent.md"),
            generic_map={"foo": generic_file},
        )
        assert result == generic_file, (
            "find_generic_ref must strip .agent.md via file_base to match the map key; "
            f"got {result!r}"
        )

    def test_uses_file_base_for_nested_path(self, tmp_path):
        """find_generic_ref extracts the stem from a full harness path."""
        generic_file = tmp_path / "contracts-designer.md"
        generic_file.write_text("# contracts-designer", encoding="utf-8")
        full_path = pathlib.Path(
            "/repo/Agents/Claude Code/CodebaseAgnostic/Agents/contracts-designer.md"
        )
        result = generic_lookup.find_generic_ref(
            full_path,
            generic_map={"contracts-designer": generic_file},
        )
        assert result == generic_file, (
            f"find_generic_ref must extract stem from a full path; got {result!r}"
        )

    def test_without_map_builds_from_real_tree(self):
        """find_generic_ref with generic_map=None builds a map from the real repo tree.

        Uses 'contracts-designer' as a stable anchor: it must be present in the real tree.
        """
        result = generic_lookup.find_generic_ref(
            pathlib.Path("contracts-designer.md"),
            generic_map=None,
        )
        assert result is not None, (
            "find_generic_ref(contracts-designer.md, generic_map=None) must return a Path; "
            "got None — build_generic_map() is not finding the real repo tree"
        )
        assert result.name == "contracts-designer.md", (
            f"find_generic_ref must return the contracts-designer.md file; got {result!r}"
        )

    def test_without_map_returns_none_for_unknown_stem(self):
        """find_generic_ref with generic_map=None returns None for a stem not in the real tree."""
        result = generic_lookup.find_generic_ref(
            pathlib.Path("__nonexistent_stem_xyzzy__.md"),
            generic_map=None,
        )
        assert result is None, (
            "find_generic_ref must return None for a stem absent from the real tree; "
            f"got {result!r}"
        )


# ---------------------------------------------------------------------------
# AC1.5 — Import cleanliness (no circular imports, importable from both entry points)
# ---------------------------------------------------------------------------

class TestImportCleanliness:
    """generic_lookup must not create import cycles when loaded from either entry point.

    AC1.5 contract: boundary_transformer.py must not import batch_transform.py.
    generic_lookup must be a leaf module (standard-library only), importable safely
    from both batch_transform and boundary_transformer without creating a cycle.

    Most tests in this class are non-regression anchors: they pass in RED because the
    architectural constraint exists today (the current boundary_transformer does not
    import batch_transform) and must continue to hold after Stage 1's refactor.
    """

    def test_generic_lookup_has_no_package_local_imports(self):
        """generic_lookup.py must import only standard-library modules — no package-local imports.

        Verified via AST parsing of the source file so the check is independent of test
        ordering and module caching. generic_lookup is designed as a leaf with zero
        intra-package dependencies, which is what makes it safely importable from both
        batch_transform and boundary_transformer without creating a cycle.
        """
        import ast

        generic_lookup_path = _TOOLS_DIR / "generic_lookup.py"
        assert generic_lookup_path.exists(), (
            f"generic_lookup.py not found at {generic_lookup_path!r}"
        )

        source = generic_lookup_path.read_text(encoding="utf-8")
        tree = ast.parse(source, filename=str(generic_lookup_path))

        # Package-local modules that generic_lookup must NOT import
        package_local_modules = {
            "batch_transform",
            "boundary_transformer",
            "boundary_constants",
            "boundary_validator",
            "frontmatter_build",
            "non_conformance",
        }

        imported_modules: set[str] = set()
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    imported_modules.add(alias.name.split(".")[0])
            elif isinstance(node, ast.ImportFrom):
                if node.module:
                    imported_modules.add(node.module.split(".")[0])

        forbidden_found = package_local_modules & imported_modules
        assert not forbidden_found, (
            f"generic_lookup.py must not import any package-local module; "
            f"found forbidden imports: {sorted(forbidden_found)!r}. "
            "generic_lookup must remain a standard-library-only leaf module."
        )

    def test_boundary_transformer_does_not_statically_import_batch_transform(self):
        """boundary_transformer.py must not contain a static import of batch_transform.

        Verified via AST parsing of the source file so that no import side-effects
        occur during the check. The circular import constraint: batch_transform imports
        from boundary_transformer (SkipReason, is_orchestrator_file, transform_file),
        so boundary_transformer must never import batch_transform in return.
        """
        import ast

        boundary_transformer_path = _TOOLS_DIR / "boundary_transformer.py"
        assert boundary_transformer_path.exists(), (
            f"boundary_transformer.py not found at {boundary_transformer_path!r}; "
            "cannot verify the no-circular-import constraint for AC1.5"
        )

        source = boundary_transformer_path.read_text(encoding="utf-8")
        tree = ast.parse(source, filename=str(boundary_transformer_path))

        imported_modules: set[str] = set()
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    imported_modules.add(alias.name.split(".")[0])
            elif isinstance(node, ast.ImportFrom):
                if node.module:
                    imported_modules.add(node.module.split(".")[0])

        assert "batch_transform" not in imported_modules, (
            "boundary_transformer.py must not import batch_transform — "
            "batch_transform already imports from boundary_transformer, so the reverse "
            "direction would create a circular import. "
            f"All imports found in boundary_transformer.py: {sorted(imported_modules)!r}"
        )

    def test_generic_lookup_and_boundary_transformer_importable_together(self):
        """Importing generic_lookup and boundary_transformer in the same process must not raise.

        Runtime check for AC1.5: both modules can coexist in sys.modules without
        triggering an ImportError or any other import-time error. generic_lookup is
        already active (imported at module level above); this test loads
        boundary_transformer alongside it.
        """
        try:
            import boundary_transformer  # noqa: F401
        except ImportError as exc:
            pytest.fail(
                "Importing boundary_transformer alongside generic_lookup raised "
                f"ImportError: {exc}. "
                "Check that generic_lookup introduces no import that creates a cycle."
            )
        except Exception as exc:  # noqa: BLE001
            pytest.fail(
                "Importing boundary_transformer alongside generic_lookup raised an "
                f"unexpected error — {type(exc).__name__}: {exc}"
            )
