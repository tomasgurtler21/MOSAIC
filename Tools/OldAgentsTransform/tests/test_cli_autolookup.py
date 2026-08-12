"""Tests for Stage 2: CLI auto-lookup in boundary_transformer.

Covers:
- resolve_cli_generic_ref() helper: precedence rules, harness-only applicability,
  fallback to None, and graceful error handling
- CLI behavior: auto-lookup on harness file with matching stem, explicit
  --generic-ref precedence, degraded fallback when no match, orchestrator
  hard-fail, CWD independence, generic file unaffected, idempotency skip
  unchanged when auto-lookup resolves a match
- transform_file() direct call: frozen contract — no auto-lookup inside
  transform_file(), still takes the degraded path when called with no
  generic_ref_path, pinning the behavior batch_transform.py depends on

Tests in TestResolveCliGenericRef and TestCLIAutoLookup are in TDD RED phase:
they compile but fail at runtime because resolve_cli_generic_ref() does not
exist yet and _main() does not invoke it.

Tests in TestOrchestratorNoMatchHardFail, TestTransformFileFrozenContract, and
TestIdempotencySkipWithAutoLookup verify pre-existing behavior and pass in TDD
RED — they pin the contracts that Stage 2 must not accidentally break.
"""
from __future__ import annotations

import pathlib
import subprocess
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_transformer import (  # noqa: E402
    WARN_NO_GENERIC_REF,
    WARN_ALREADY_TRANSFORMED,
    ERR_HARNESS_NO_GENERIC_REF,
    transform_file,
)

# resolve_cli_generic_ref is a Stage 2 addition. Import defensively so the
# file compiles and individual tests can be collected in TDD RED phase even
# before the implementation is written.
try:
    from boundary_transformer import resolve_cli_generic_ref as _resolve_cli_generic_ref
    _HAS_RESOLVE = True
except (ImportError, AttributeError):
    _resolve_cli_generic_ref = None  # type: ignore[assignment]
    _HAS_RESOLVE = False


_TRANSFORMER_CLI = _TOOLS_DIR / "boundary_transformer.py"

# ---------------------------------------------------------------------------
# Fixture content helpers
# ---------------------------------------------------------------------------

# Minimal valid harness frontmatter. transform_version marks it as AGENT_HARNESS
# so the degraded / auto-lookup path is taken by both transform_file() and the CLI.
_HARNESS_CONTENT = """\
---
id: 99
version: 1.0.0
transform_version: 1.0.0
name: test-harness
description: Minimal harness for Stage 2 auto-lookup tests
role: subagent
---

# TestHarness Agent

You are the **TestHarness** agent in a multi-agent orchestration system.

**Goal:** Serve as a fixture for auto-lookup tests.

**Scope:**
- You DO: Provide fixture data
- You DO NOT: Run in production

---

## Capabilities

### Core Capabilities
- Provide fixture data for tests

---

## Constraints

- Stay within test scope

---

## Error Handling

- Handle errors gracefully

---

## Output Format

Return a JSON object.

---

## Execution Philosophy

Be minimal and deterministic.
"""

# Minimal generic (non-harness) content. No transform_version means it is
# classified as AGENT_GENERIC — auto-lookup must not apply.
_GENERIC_CONTENT = """\
---
id: 99
version: 1.0.0
name: test-generic
description: Minimal generic agent for Stage 2 auto-lookup tests
role: subagent
---

# TestGeneric Agent

You are the **TestGeneric** agent in a multi-agent orchestration system.

**Goal:** Serve as a fixture for auto-lookup tests.

**Scope:**
- You DO: Provide fixture data
- You DO NOT: Run in production

---

## Capabilities

### Core Capabilities
- Provide fixture data for tests

---

## Constraints

- Stay within test scope

---

## Error Handling

- Handle errors gracefully

---

## Output Format

Return a JSON object.

---

## Execution Philosophy

Be minimal and deterministic.
"""


# Harness content with canonical boundary tags already present. Carries
# transform_version (so it is classified AGENT_HARNESS), but the body already
# has paired [[SECTION:...]] tags — the idempotency-skip condition. Used to
# verify that the ALREADY_TRANSFORMED skip path fires even when auto-lookup
# resolves a real generic ref (the Stage 2 regression risk from the Plan).
_ALREADY_TRANSFORMED_HARNESS_CONTENT = """\
---
id: 99
version: 1.0.0
transform_version: 1.0.0
name: test-harness
description: Already-transformed harness for idempotency-skip tests
role: subagent
---

[[SECTION:Identity]]
# TestHarness Agent

You are the **TestHarness** agent in a multi-agent orchestration system.

**Goal:** Serve as a fixture for idempotency-skip tests.

**Scope:**
- You DO: Provide fixture data
- You DO NOT: Run in production
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

### Core Capabilities
- Provide fixture data for tests
[[/SECTION:Capabilities]]

[[SECTION:Constraints]]
## Constraints

- Stay within test scope
[[/SECTION:Constraints]]

[[SECTION:ErrorHandling]]
## Error Handling

- Handle errors gracefully
[[/SECTION:ErrorHandling]]

[[SECTION:OutputFormat]]
## Output Format

Return a JSON object.
[[/SECTION:OutputFormat]]

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy

Be minimal and deterministic.
[[/SECTION:ExecutionPhilosophy]]
"""


# ---------------------------------------------------------------------------
# resolve_cli_generic_ref() — unit tests
# ---------------------------------------------------------------------------

class TestResolveCliGenericRef:
    """Unit tests for the resolve_cli_generic_ref() helper.

    All tests in this class fail in TDD RED because the function does not
    exist yet in boundary_transformer.py.
    """

    @pytest.fixture(autouse=True)
    def _require_function(self):
        """Fail immediately and clearly when the function is not yet implemented."""
        if not _HAS_RESOLVE:
            pytest.fail(
                "resolve_cli_generic_ref is not yet present in boundary_transformer.py "
                "— expected TDD RED failure. Implement Stage 2 to make this pass."
            )

    def test_explicit_ref_returned_unchanged_for_harness_file(self, tmp_path):
        """When explicit_ref is not None it is returned as-is; no lookup is performed.

        Precedence rule 1: explicit --generic-ref always wins over auto-lookup.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")
        explicit_ref = tmp_path / "some-other.md"
        explicit_ref.write_text("# other", encoding="utf-8")

        result = _resolve_cli_generic_ref(harness_file, explicit_ref)

        assert result == explicit_ref, (
            "resolve_cli_generic_ref must return explicit_ref unchanged when it is "
            f"provided, regardless of whether auto-lookup would find a match; "
            f"got {result!r}"
        )

    def test_explicit_ref_returned_unchanged_for_generic_file(self, tmp_path):
        """explicit_ref wins even for a non-harness file — rule 1 is unconditional."""
        generic_file = tmp_path / "generic-agent.md"
        generic_file.write_text(_GENERIC_CONTENT, encoding="utf-8")
        explicit_ref = tmp_path / "ref.md"
        explicit_ref.write_text("# ref", encoding="utf-8")

        result = _resolve_cli_generic_ref(generic_file, explicit_ref)

        assert result == explicit_ref, (
            "resolve_cli_generic_ref must return explicit_ref for any input type "
            f"when explicit_ref is provided; got {result!r}"
        )

    def test_harness_with_matching_stem_returns_generic_path(self, tmp_path):
        """For a harness file whose stem is in the real generic map, return the mapped path.

        Uses 'contracts-designer' as a stable anchor that is known to exist in
        the real repo tree (Agents/Generic/Agents/Planning/contracts-designer.md).
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = _resolve_cli_generic_ref(harness_file, explicit_ref=None)

        assert result is not None, (
            "resolve_cli_generic_ref must return a Path for a harness file whose stem "
            "('contracts-designer') is present in the real generic map; got None. "
            "Auto-lookup must apply to harness files with a matching stem."
        )
        assert isinstance(result, pathlib.Path), (
            f"resolve_cli_generic_ref must return a pathlib.Path; got {type(result)!r}"
        )
        assert result.exists(), (
            f"The returned path must exist on disk: {result!r}"
        )
        assert result.name == "contracts-designer.md", (
            f"resolve_cli_generic_ref must return the contracts-designer.md file; "
            f"got {result.name!r}"
        )

    def test_harness_with_no_match_returns_none(self, tmp_path):
        """For a harness file whose stem is not in the map, return None.

        None signals the caller to fall through to the degraded path — the behavior
        is unchanged from before auto-lookup, just reached via a different code path.
        """
        harness_file = tmp_path / "__no_stem_match_xyzzy__.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = _resolve_cli_generic_ref(harness_file, explicit_ref=None)

        assert result is None, (
            "resolve_cli_generic_ref must return None when the harness file's stem is "
            f"absent from the generic map (degraded-path fallback); got {result!r}"
        )

    def test_generic_file_input_returns_none(self, tmp_path):
        """For a generic (non-harness) file, return None regardless of stem.

        Auto-lookup applies only to harness files (those whose frontmatter carries
        transform_version). Generic files ignore generic_ref_path entirely.
        """
        # Use the 'contracts-designer' stem so we know it IS in the map — the test
        # isolates the harness-only applicability rule, not a missing-stem case.
        generic_file = tmp_path / "contracts-designer.md"
        generic_file.write_text(_GENERIC_CONTENT, encoding="utf-8")

        result = _resolve_cli_generic_ref(generic_file, explicit_ref=None)

        assert result is None, (
            "resolve_cli_generic_ref must return None for a generic (non-harness) file "
            f"even when its stem matches the map; got {result!r}. "
            "Auto-lookup must only apply when the file carries transform_version."
        )

    def test_unreadable_input_returns_none_without_raising(self, tmp_path):
        """When the input file does not exist, return None without raising.

        The real diagnosis (file not found, bad frontmatter, etc.) belongs to
        transform_file(), which already reports it through TransformResult.errors.
        """
        nonexistent = tmp_path / "no-such-file.md"

        result = _resolve_cli_generic_ref(nonexistent, explicit_ref=None)

        assert result is None, (
            "resolve_cli_generic_ref must return None (not raise) when the input "
            f"file cannot be read; got {result!r}"
        )

    def test_agent_md_double_extension_resolves_via_file_base(self, tmp_path):
        """A harness with .agent.md extension resolves via file_base() semantics.

        'contracts-designer.agent.md' must look up 'contracts-designer' in the
        map, not 'contracts-designer.agent'. This re-uses the Stage 1 file_base
        helper that batch_transform.py also uses.
        """
        harness_file = tmp_path / "contracts-designer.agent.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = _resolve_cli_generic_ref(harness_file, explicit_ref=None)

        assert result is not None, (
            "resolve_cli_generic_ref must find 'contracts-designer' from the "
            "'contracts-designer.agent.md' filename; got None. "
            "The double-extension stem must be handled via file_base()."
        )
        assert result.name == "contracts-designer.md", (
            f"resolve_cli_generic_ref must return contracts-designer.md; "
            f"got {result.name!r}"
        )

    def test_empty_map_returns_none_without_raising(self, tmp_path, monkeypatch):
        """When the generic tree is missing (map is empty), return None without raising.

        Error-handling contract from the design: missing GENERIC_AGENTS_ROOT →
        build_generic_map() returns {} → resolve_cli_generic_ref returns None.
        """
        import generic_lookup

        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        monkeypatch.setattr(generic_lookup, "GENERIC_AGENTS_ROOT", tmp_path / "nonexistent")
        monkeypatch.setattr(
            generic_lookup, "GENERIC_ORCHESTRATOR_PATH", tmp_path / "nonexistent_orch.md"
        )

        result = _resolve_cli_generic_ref(harness_file, explicit_ref=None)

        assert result is None, (
            "resolve_cli_generic_ref must return None (not raise) when the generic "
            f"map is empty because the tree is absent; got {result!r}"
        )

    def test_returns_none_not_raises_for_malformed_frontmatter(self, tmp_path):
        """An input file with invalid YAML frontmatter must produce None, not an exception."""
        bad_frontmatter = "---\nkey: [unclosed\n---\n# body\n"
        bad_file = tmp_path / "bad-frontmatter.md"
        bad_file.write_text(bad_frontmatter, encoding="utf-8")

        try:
            result = _resolve_cli_generic_ref(bad_file, explicit_ref=None)
        except Exception as exc:  # noqa: BLE001
            pytest.fail(
                "resolve_cli_generic_ref must return None rather than raising for "
                f"malformed frontmatter; got {type(exc).__name__}: {exc}"
            )
        else:
            assert result is None, (
                "resolve_cli_generic_ref must return None for malformed frontmatter; "
                f"got {result!r}"
            )


# ---------------------------------------------------------------------------
# CLI auto-lookup — subprocess tests
# ---------------------------------------------------------------------------

class TestCLIAutoLookup:
    """CLI auto-lookup behavior verified end-to-end via subprocess.

    All tests in this class fail in TDD RED because _main() does not yet
    call resolve_cli_generic_ref(); harness files with a matching stem still
    take the degraded path and emit WARN_NO_GENERIC_REF.
    """

    def test_harness_with_matching_stem_no_degraded_warning(self, tmp_path):
        """CLI must not emit WARN_NO_GENERIC_REF when the stem matches a real generic file.

        'contracts-designer.md' is a stable anchor present in the real generic map.
        With auto-lookup wired in _main(), the harness file gets a full-quality transform
        and the degraded-path warning must not appear on stderr.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
        )

        assert "No generic reference found for" not in proc.stderr, (
            "CLI must not emit the WARN_NO_GENERIC_REF degraded-path warning when "
            "auto-lookup finds a matching generic file for 'contracts-designer.md'. "
            f"stderr: {proc.stderr!r}"
        )

    def test_harness_with_matching_stem_exits_zero(self, tmp_path):
        """CLI exits 0 for a harness file whose stem matches a real generic file.

        No --generic-ref flag is passed. The auto-lookup must supply the reference
        so the transform takes the non-degraded path and succeeds.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
        )

        assert proc.returncode == 0, (
            f"CLI must exit 0 when a harness file's stem matches a generic file "
            f"and no --generic-ref is given; got exit {proc.returncode}. "
            f"stderr: {proc.stderr!r}"
        )

    def test_explicit_generic_ref_wins_over_autolookup(self, tmp_path):
        """--generic-ref takes precedence over auto-lookup when both are applicable.

        The harness file stem ('contracts-designer') would auto-resolve to the real
        generic file. Providing an explicit --generic-ref (the same real path) must
        still work — verifying the explicit-wins precedence without content mismatch.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        import generic_lookup
        real_map = generic_lookup.build_generic_map()
        real_ref_path = real_map.get("contracts-designer")
        assert real_ref_path is not None, (
            "contracts-designer must be in the generic map for this test to be valid; "
            "check that the repo tree is intact"
        )

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file),
             "--output", str(output_path),
             "--generic-ref", str(real_ref_path)],
            capture_output=True, text=True,
        )

        assert "No generic reference found for" not in proc.stderr, (
            "CLI must not emit the degraded-path warning when --generic-ref is given "
            f"explicitly. stderr: {proc.stderr!r}"
        )

    def test_harness_with_no_match_takes_degraded_path(self, tmp_path):
        """Harness file with no matching stem still takes the degraded path.

        The degraded behavior — exit 0 with WARN_NO_GENERIC_REF on stderr — must be
        byte-identical to today's behavior. Auto-lookup must not change it.
        """
        harness_file = tmp_path / "__no_stem_match_xyzzy_stage2__.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
        )

        assert proc.returncode == 0, (
            f"CLI must exit 0 for a degraded-path harness transform (non-orchestrator); "
            f"exit {proc.returncode}. stderr: {proc.stderr!r}"
        )
        assert "No generic reference found for" in proc.stderr, (
            "CLI must emit the WARN_NO_GENERIC_REF warning when no stem match is found. "
            f"The warning text must be unchanged. stderr: {proc.stderr!r}"
        )

    def test_degraded_warning_text_is_unchanged(self, tmp_path):
        """The degraded-path warning text must remain byte-identical to WARN_NO_GENERIC_REF.

        The WARN_NO_GENERIC_REF constant value must not be altered by Stage 2.
        """
        harness_file = tmp_path / "__no_stem_match_xyzzy_stage2__.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
        )

        # Check the constant's literal prefix so any rewording is caught.
        assert "No generic reference found for" in proc.stderr, (
            f"Degraded-path warning must contain the WARN_NO_GENERIC_REF prefix text; "
            f"stderr: {proc.stderr!r}"
        )
        assert "running degraded-quality transform" in proc.stderr, (
            f"Degraded-path warning must contain the WARN_NO_GENERIC_REF suffix text; "
            f"stderr: {proc.stderr!r}"
        )

    def test_generic_file_input_unaffected(self, tmp_path):
        """A generic (non-harness) file transforms successfully without --generic-ref.

        Auto-lookup must not interfere with generic-file transforms. The behavior
        must be unchanged from before Stage 2.
        """
        generic_file = tmp_path / "generic-agent.md"
        generic_file.write_text(_GENERIC_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(generic_file), "--output", str(output_path)],
            capture_output=True, text=True,
        )

        assert proc.returncode == 0, (
            f"CLI must exit 0 for a generic file input when no --generic-ref is given; "
            f"auto-lookup must not interfere. exit {proc.returncode}. "
            f"stderr: {proc.stderr!r}"
        )

    def test_autolookup_succeeds_regardless_of_cwd(self, tmp_path):
        """CLI auto-lookup works correctly regardless of the process's working directory.

        REPO_ROOT is anchored to __file__ in generic_lookup.py, not to cwd. Running
        the CLI from an unrelated directory (tmp_path) must produce the same result
        as running from the repo root.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
            cwd=str(tmp_path),  # unrelated working directory
        )

        assert "No generic reference found for" not in proc.stderr, (
            "CLI must not emit the degraded-path warning for 'contracts-designer.md' "
            "even when the process cwd is an unrelated directory. "
            f"REPO_ROOT must be __file__-anchored, not cwd-anchored. "
            f"stderr: {proc.stderr!r}"
        )

    def test_autolookup_cwd_changed_exits_zero(self, tmp_path):
        """CLI exits 0 for a matching harness file when cwd is an unrelated directory."""
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
            cwd=str(tmp_path),
        )

        assert proc.returncode == 0, (
            f"CLI must exit 0 for a matching harness file even when cwd is changed; "
            f"exit {proc.returncode}. stderr: {proc.stderr!r}"
        )


# ---------------------------------------------------------------------------
# Orchestrator hard-fail preserved when no stem match
# ---------------------------------------------------------------------------

class TestOrchestratorNoMatchHardFail:
    """An orchestrator harness file with no map match must still hard-fail.

    When resolve_cli_generic_ref returns None for an orchestrator file (because
    the stem is absent from the generic map), the existing transform_file()
    hard-fail must fire. These tests verify the behavior at the transform_file()
    level using a synthetic scenario — they pass in TDD RED because they test
    existing behavior, not the Stage 2 addition.

    Note: in practice, 'orchestrator' IS in the real generic map (build_generic_map
    adds it explicitly). These tests simulate the "no match" case that the design
    requires to continue hard-failing.
    """

    def test_transform_file_orchestrator_no_ref_returns_failure(self, tmp_path):
        """transform_file() on orchestrator.md with no generic_ref_path returns success=False.

        This is the code path reached when resolve_cli_generic_ref() returns None for
        an orchestrator file — the existing hard-fail must remain intact.
        """
        orch_file = tmp_path / "orchestrator.md"
        orch_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = transform_file(orch_file, None, None)

        assert result.success is False, (
            "transform_file() on an orchestrator harness file with generic_ref_path=None "
            f"must return success=False (hard-fail); got success={result.success!r}"
        )

    def test_transform_file_orchestrator_no_ref_emits_hard_error(self, tmp_path):
        """transform_file() on orchestrator.md with no generic_ref emits ERR_HARNESS_NO_GENERIC_REF."""
        orch_file = tmp_path / "orchestrator.md"
        orch_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = transform_file(orch_file, None, None)

        error_messages = [err.message for err in result.errors]
        assert any(ERR_HARNESS_NO_GENERIC_REF in msg for msg in error_messages), (
            f"transform_file() on an orchestrator harness file with no generic ref must "
            f"report ERR_HARNESS_NO_GENERIC_REF in result.errors; "
            f"got errors: {error_messages!r}"
        )

    def test_transform_file_orchestrator_agent_md_no_ref_returns_failure(self, tmp_path):
        """orchestrator.agent.md with no generic_ref_path also hard-fails."""
        orch_file = tmp_path / "orchestrator.agent.md"
        orch_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = transform_file(orch_file, None, None)

        assert result.success is False, (
            "transform_file() on 'orchestrator.agent.md' with generic_ref_path=None "
            f"must return success=False; got success={result.success!r}"
        )


# ---------------------------------------------------------------------------
# T2.2 — Direct transform_file() call: frozen contract
# ---------------------------------------------------------------------------

class TestTransformFileFrozenContract:
    """Direct calls to transform_file() with no generic_ref_path keep today's degraded behavior.

    This pins the contract that batch_transform.py depends on: when called with
    generic_ref_path=None on a non-orchestrator harness file, transform_file() must
    take the degraded path and emit WARN_NO_GENERIC_REF. No auto-lookup may be
    added inside this function — auto-lookup lives exclusively in _main().

    All tests in this class pass in TDD RED because they verify pre-existing behavior.
    They must continue to pass after Stage 2 is implemented.
    """

    def test_direct_call_no_ref_returns_degraded_true(self, tmp_path):
        """transform_file() with generic_ref_path=None on a harness file returns degraded=True.

        Even when the stem would match via auto-lookup ('contracts-designer'), calling
        transform_file() directly must still produce degraded=True — auto-lookup is the
        CLI layer's responsibility, not the transform function's.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = transform_file(harness_file, None, None)

        assert result.degraded is True, (
            "transform_file() with generic_ref_path=None must return degraded=True on a "
            "harness file even when the stem ('contracts-designer') exists in the real "
            "generic map. Auto-lookup must not be inside transform_file(). "
            f"Got degraded={result.degraded!r}."
        )

    def test_direct_call_no_ref_emits_warn_no_generic_ref(self, tmp_path):
        """transform_file() with generic_ref_path=None emits WARN_NO_GENERIC_REF in warnings.

        batch_transform.py uses the degraded flag and this warning for its summary
        counts and per-file reporting. Both must remain in result.warnings.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = transform_file(harness_file, None, None)

        assert any(
            "No generic reference found for" in w for w in result.warnings
        ), (
            "transform_file() with generic_ref_path=None must include the "
            "WARN_NO_GENERIC_REF text in result.warnings; "
            f"got warnings: {result.warnings!r}"
        )

    def test_direct_call_no_ref_returns_success_true(self, tmp_path):
        """transform_file() on the degraded path returns success=True.

        A degraded transform is a complete (if lower-quality) transform, not a
        failure. batch_transform.py classifies it as degraded, not as error.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = transform_file(harness_file, None, None)

        assert result.success is True, (
            "transform_file() on the degraded path must return success=True; "
            f"got success={result.success!r}"
        )

    def test_direct_call_no_autolookup_even_for_known_stem(self, tmp_path):
        """transform_file() never performs auto-lookup, even for a stem present in the real map.

        This is the load-bearing architectural constraint: nothing reachable from
        transform_file() may call resolve_cli_generic_ref or build_generic_map.
        If auto-lookup were added inside transform_file(), batch_transform.py's
        deliberate degraded routing for unmatched non-orchestrator harness files
        would silently disappear and batch summary counts would be wrong.
        """
        # 'contracts-designer' IS in the real generic map — if auto-lookup were
        # added inside transform_file(), this call would NOT be degraded.
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = transform_file(harness_file, None, None)

        assert result.degraded is True, (
            "transform_file() must take the degraded path even when the file's stem "
            "('contracts-designer') is present in the real generic map. "
            "Auto-lookup belongs in _main() only, not in transform_file(). "
            f"Got degraded={result.degraded!r}. "
            "If this fails, auto-lookup was incorrectly added inside transform_file()."
        )

    def test_direct_call_stem_mismatch_also_degraded(self, tmp_path):
        """transform_file() takes the degraded path even when the stem has no map match."""
        harness_file = tmp_path / "__unknown_stem_xyzzy__.md"
        harness_file.write_text(_HARNESS_CONTENT, encoding="utf-8")

        result = transform_file(harness_file, None, None)

        assert result.degraded is True, (
            "transform_file() must return degraded=True for any harness file called "
            "with generic_ref_path=None, including stems not in the map. "
            f"Got degraded={result.degraded!r}."
        )


# ---------------------------------------------------------------------------
# Idempotency skip preserved after auto-lookup is wired in
# ---------------------------------------------------------------------------

class TestIdempotencySkipWithAutoLookup:
    """Idempotency skip fires unchanged when auto-lookup is wired into _main().

    Risk from the Plan's Risks table: 'The idempotency skip for already-tagged
    harness files starts behaving differently now that a generic ref is usually
    found — cover the already-transformed input case in T2.1 and keep its
    outcome unchanged.'

    Before Stage 2: _main() calls transform_file(input, output, None). The
    transform_file() sees canonical boundary tags first → idempotency skip fires
    → WARN_ALREADY_TRANSFORMED on stderr, exit 0. The skip is never reached via
    a non-None generic_ref_path on this code path.

    After Stage 2: _main() calls resolve_cli_generic_ref() → gets a real generic
    path for 'contracts-designer' (it IS in the map) → calls transform_file(input,
    output, generic_ref). The transform_file() MUST still check the canonical
    boundary tags and fire the idempotency skip even when generic_ref_path is not
    None. The outcome (WARN_ALREADY_TRANSFORMED, exit 0) must be unchanged.

    These tests pass in TDD RED because they pin pre-existing transform_file()
    behavior (the idempotency skip predates Stage 2). They must also pass after
    Stage 2 is implemented. A post-implementation failure means the skip check
    was accidentally placed after the generic-ref dispatch, which is the exact
    regression the Plan flagged.
    """

    def test_already_transformed_harness_with_matching_stem_takes_skip_path(
        self, tmp_path
    ):
        """Already-transformed harness still takes the skip path even when auto-lookup finds a match.

        'contracts-designer' is in the real generic map, so after Stage 2 is wired,
        resolve_cli_generic_ref() would return a real path and pass it to
        transform_file(). The idempotency skip must fire regardless, because the
        file's body already carries canonical boundary tags. The
        WARN_ALREADY_TRANSFORMED message must appear on stderr, unchanged from
        the pre-Stage-2 behavior.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_ALREADY_TRANSFORMED_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
        )

        # WARN_ALREADY_TRANSFORMED = "{path} already carries canonical boundary tags; skipping"
        assert "already carries canonical boundary tags" in proc.stderr, (
            "CLI must emit the WARN_ALREADY_TRANSFORMED skip message when the harness "
            "file's body already carries canonical boundary tags, even when auto-lookup "
            "would find a matching generic file ('contracts-designer'). "
            "The idempotency skip must take precedence over any resolved generic ref. "
            f"stderr: {proc.stderr!r}"
        )

    def test_already_transformed_harness_with_matching_stem_exits_zero(
        self, tmp_path
    ):
        """CLI exits 0 for an already-transformed harness file (skip is a successful outcome).

        The skip path is not a failure. The exit code must be 0 regardless of
        whether auto-lookup finds a generic ref or not.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_ALREADY_TRANSFORMED_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
        )

        assert proc.returncode == 0, (
            "CLI must exit 0 for an already-transformed harness file even when "
            "auto-lookup would resolve a matching generic file ('contracts-designer'). "
            "The idempotency skip path must not change the exit code. "
            f"exit {proc.returncode}. stderr: {proc.stderr!r}"
        )

    def test_already_transformed_harness_does_not_emit_no_generic_ref_warning(
        self, tmp_path
    ):
        """Idempotency skip must not emit the degraded-path warning.

        The skip path fires before the degraded-path branch. The
        WARN_NO_GENERIC_REF warning ('No generic reference found for ...') must
        not appear in stderr — the file is skipped, not degraded-transformed.
        """
        harness_file = tmp_path / "contracts-designer.md"
        harness_file.write_text(_ALREADY_TRANSFORMED_HARNESS_CONTENT, encoding="utf-8")
        output_path = tmp_path / "out.md"

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_file), "--output", str(output_path)],
            capture_output=True, text=True,
        )

        assert "No generic reference found for" not in proc.stderr, (
            "CLI must not emit the WARN_NO_GENERIC_REF degraded-path warning for an "
            "already-transformed harness file — it must take the skip path, not the "
            "degraded-transform path. "
            f"stderr: {proc.stderr!r}"
        )
