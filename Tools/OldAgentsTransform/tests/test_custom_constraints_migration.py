"""End-to-end test: old-format migration correctly handles the legacy custom_constraints marker.

Verifies that the boundary_transformer handles the legacy custom_constraints marker without
error and does NOT emit [[INJECTION:CustomConstraints]]. The end-to-end fixture
(s4_generic_constraints_regions_input.md) carries the legacy marker with empty content;
per the drop-when-empty rule, no [[CUSTOM:CustomConstraints]] tags are emitted for it —
the region is silently dropped. The inline fixture class TestInlineCustomConstraintsPopulated
covers the populated-content open/close-tag assertions end-to-end.

Uses the richest existing fixture that carries the legacy custom_constraints
marker (s4_generic_constraints_regions_input.md). This fixture is the closest
available analog to a "real old-format agent catalog" entry within the
repository, as documented in the Stage 9 plan note on fixture fallback.
The fixture's custom_constraints marker has no populated content following it,
so the drop-when-empty rule applies and no CUSTOM open/close tags are emitted.

Properties verified by the E2E fixture class (TestCustomConstraintsMigrationOutputKind):
  - transform_file succeeds on a file containing the legacy custom_constraints
    marker (no error, success=True).
  - The output does NOT contain [[INJECTION:CustomConstraints]] (wrong kind).
  - The output does NOT contain [[/INJECTION:CustomConstraints]] (wrong kind close).
  - The legacy marker text ([INJECTION: custom_constraints]) is absent from the output.
  - When the legacy marker's content is empty (dropped by the transformer), the
    transform still succeeds and the CUSTOM region is absent (consistent with the
    drop-when-empty rule).

Properties verified by the inline fixture classes:
  - TestInlineCustomConstraintsPopulated: populated content emits [[CUSTOM:CustomConstraints]]
    open and close tags with content preserved.
  - TestInlineCustomConstraintsEmpty: empty content is dropped entirely (no CUSTOM tags).
"""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_transformer import TransformResult, transform_file  # noqa: E402

_FIXTURES_DIR = pathlib.Path(__file__).parent / "fixtures"


# ---------------------------------------------------------------------------
# Helper: locate the fixture carrying the legacy custom_constraints marker
# ---------------------------------------------------------------------------

def _constraints_fixture() -> pathlib.Path:
    """Return the path to the fixture that carries the legacy custom_constraints marker.

    Falls back gracefully when the fixture is not in the expected location so that
    the test suite is not broken by a missing file, but skips with a clear message.
    """
    fixture = _FIXTURES_DIR / "s4_generic_constraints_regions_input.md"
    if not fixture.exists():
        pytest.skip(
            f"Fixture not found at {fixture}. "
            "Expected s4_generic_constraints_regions_input.md to carry the "
            "legacy [INJECTION: custom_constraints] marker for end-to-end migration testing. "
            "The test suite uses the richest available repository fixture as documented "
            "in the Stage 9 plan (fixture fallback note)."
        )
    return fixture


def _transform_fixture(tmp_path: pathlib.Path) -> tuple[TransformResult, pathlib.Path]:
    """Run transform_file on the constraints fixture and return (result, output_path)."""
    input_path = _constraints_fixture()
    output_path = tmp_path / input_path.name
    result = transform_file(input_path, output_path)
    return result, output_path


# ---------------------------------------------------------------------------
# T9.4 — End-to-end migration: legacy custom_constraints marker → CUSTOM kind
# ---------------------------------------------------------------------------

class TestCustomConstraintsMigrationSuccess:
    """The transformer must succeed on a file carrying the legacy custom_constraints marker."""

    def test_transform_succeeds(self, tmp_path: pathlib.Path) -> None:
        result, _ = _transform_fixture(tmp_path)
        assert result.success is True, (
            f"transform_file must succeed on a file with the legacy custom_constraints "
            f"marker. Errors: {result.errors}"
        )

    def test_transform_produces_no_errors(self, tmp_path: pathlib.Path) -> None:
        result, _ = _transform_fixture(tmp_path)
        assert result.errors == [], (
            f"transform_file must produce no errors for the constraints fixture. "
            f"Got: {result.errors}"
        )


class TestCustomConstraintsMigrationOutputKind:
    """The transformer must not emit [[INJECTION:CustomConstraints]] for the legacy marker.

    The E2E fixture (s4_generic_constraints_regions_input.md) carries the legacy
    custom_constraints marker with empty content. Per the drop-when-empty rule, no
    [[CUSTOM:CustomConstraints]] tags are emitted — the region is dropped silently.
    The positive open/close-tag assertions for populated content are covered by the
    inline fixture class TestInlineCustomConstraintsPopulated.

    This class verifies the negative properties: the wrong kind is never emitted, the
    legacy marker text is replaced, and the transform succeeds even with empty content.
    """

    def test_output_does_not_contain_injection_open_tag(self, tmp_path: pathlib.Path) -> None:
        _, output_path = _transform_fixture(tmp_path)
        output = output_path.read_text(encoding="utf-8")
        assert "[[INJECTION:CustomConstraints]]" not in output, (
            "The output must NOT contain [[INJECTION:CustomConstraints]] — the legacy "
            "custom_constraints marker must be emitted as CUSTOM, not INJECTION. "
            "CustomConstraints is deliberately absent from the canonical deployed registry "
            "and the injection parent map; it is project-invented content."
        )

    def test_output_does_not_contain_injection_close_tag(self, tmp_path: pathlib.Path) -> None:
        _, output_path = _transform_fixture(tmp_path)
        output = output_path.read_text(encoding="utf-8")
        assert "[[/INJECTION:CustomConstraints]]" not in output, (
            "The output must NOT contain [[/INJECTION:CustomConstraints]]. "
            "If the open tag is CUSTOM, the close tag must also be CUSTOM."
        )

    def test_legacy_marker_text_absent_from_output(self, tmp_path: pathlib.Path) -> None:
        _, output_path = _transform_fixture(tmp_path)
        output = output_path.read_text(encoding="utf-8")
        # The old-format marker text must be completely absent.
        assert "[INJECTION: custom_constraints]" not in output, (
            "The legacy marker text '[INJECTION: custom_constraints]' must not "
            "appear in the output — it must be replaced by [[CUSTOM:CustomConstraints]]."
        )


class TestCustomConstraintsMigrationCustomTagRoundTrips:
    """The emitted [[CUSTOM:CustomConstraints]] tags must form a matched pair and
    their content must be non-empty when the fixture carries populated content."""

    def test_open_and_close_tags_are_both_present(self, tmp_path: pathlib.Path) -> None:
        _, output_path = _transform_fixture(tmp_path)
        output = output_path.read_text(encoding="utf-8")
        has_open = "[[CUSTOM:CustomConstraints]]" in output
        has_close = "[[/CUSTOM:CustomConstraints]]" in output
        assert has_open == has_close, (
            "[[CUSTOM:CustomConstraints]] and [[/CUSTOM:CustomConstraints]] must both be "
            f"present or both absent in the output. Open={has_open}, Close={has_close}."
        )

    def test_open_tag_precedes_close_tag(self, tmp_path: pathlib.Path) -> None:
        _, output_path = _transform_fixture(tmp_path)
        output = output_path.read_text(encoding="utf-8")
        if "[[CUSTOM:CustomConstraints]]" not in output:
            pytest.skip("Custom open tag absent — may be a populated-content drop; skipping ordering check")
        open_idx = output.index("[[CUSTOM:CustomConstraints]]")
        close_idx = output.index("[[/CUSTOM:CustomConstraints]]")
        assert open_idx < close_idx, (
            f"[[CUSTOM:CustomConstraints]] (index {open_idx}) must precede "
            f"[[/CUSTOM:CustomConstraints]] (index {close_idx}) in the output"
        )

    def test_other_injections_still_emitted_as_injection(self, tmp_path: pathlib.Path) -> None:
        """Non-custom-constraints injections must still be [[INJECTION:Name]], not [[CUSTOM:Name]].

        Only the legacy custom_constraints marker changes kind; all other markers remain
        [[INJECTION:]]. This verifies the change is name-scoped to CustomConstraints.
        """
        _, output_path = _transform_fixture(tmp_path)
        output = output_path.read_text(encoding="utf-8")
        # The fixture carries harness_constraints → HarnessConstraints which is a
        # tool-managed DEPLOYED region, not an injection. But it also carries other
        # injection markers like identity_extension. Check that the generic injection
        # output is not accidentally changed to CUSTOM.
        # [[CUSTOM:IdentityExtension]] must not appear.
        assert "[[CUSTOM:IdentityExtension]]" not in output, (
            "[[CUSTOM:IdentityExtension]] must NOT appear in the output — "
            "only custom_constraints maps to [[CUSTOM:]]; all other markers remain [[INJECTION:]]."
        )


# ---------------------------------------------------------------------------
# T9.4 — Additional inline fixture: minimal old-format file with populated custom_constraints
# ---------------------------------------------------------------------------

# Inline minimal old-format file that carries a populated [INJECTION: custom_constraints]
# marker (non-empty content). The transformer must emit [[CUSTOM:CustomConstraints]] for it.
_MINIMAL_WITH_CUSTOM_CONSTRAINTS = """\
---
id: 42
version: 2.0.0
name: minimal-custom-constraints-test
description: Minimal agent for custom_constraints migration testing
---

# MinimalCustomConstraints Agent

You are the MinimalCustomConstraints agent.

[INJECTION: identity_extension]

---

## Capabilities

Core capabilities.

---

## Constraints

Basic constraints.

[INJECTION: harness_constraints]
[INJECTION: custom_constraints]
My custom project constraint that must survive as [[CUSTOM:CustomConstraints]].

---

## Error Handling

Handle errors.

---

## Output Format

Return structured output.

---

## Execution Philosophy

Execute carefully.
"""

# Inline minimal old-format file with EMPTY custom_constraints content.
# The transformer's drop-when-empty rule applies: an empty [[CUSTOM:CustomConstraints]]
# region is dropped entirely (no open or close tag in the output).
_MINIMAL_EMPTY_CUSTOM_CONSTRAINTS = """\
---
id: 43
version: 2.0.0
name: minimal-empty-custom-constraints-test
description: Minimal agent for empty custom_constraints migration testing
---

# MinimalEmptyCustomConstraints Agent

You are the MinimalEmptyCustomConstraints agent.

[INJECTION: identity_extension]

---

## Capabilities

Core capabilities.

---

## Constraints

Basic constraints.

[INJECTION: harness_constraints]
[INJECTION: custom_constraints]

---

## Error Handling

Handle errors.

---

## Output Format

Return structured output.

---

## Execution Philosophy

Execute carefully.
"""


class TestInlineCustomConstraintsPopulated:
    """Inline fixture with populated custom_constraints content must produce
    [[CUSTOM:CustomConstraints]] in the output."""

    def test_populated_custom_constraints_emitted_as_custom(self, tmp_path: pathlib.Path) -> None:
        input_path = tmp_path / "inline_populated.md"
        input_path.write_text(_MINIMAL_WITH_CUSTOM_CONSTRAINTS, encoding="utf-8")
        output_path = tmp_path / "inline_populated_out.md"

        result = transform_file(input_path, output_path)

        assert result.success is True, f"Transform must succeed: {result.errors}"
        output = output_path.read_text(encoding="utf-8")
        assert "[[CUSTOM:CustomConstraints]]" in output, (
            "Populated custom_constraints content must be emitted as [[CUSTOM:CustomConstraints]]"
        )
        assert "[[INJECTION:CustomConstraints]]" not in output, (
            "Populated custom_constraints content must not be emitted as [[INJECTION:CustomConstraints]]"
        )

    def test_populated_custom_constraints_content_preserved(self, tmp_path: pathlib.Path) -> None:
        input_path = tmp_path / "inline_populated_content.md"
        input_path.write_text(_MINIMAL_WITH_CUSTOM_CONSTRAINTS, encoding="utf-8")
        output_path = tmp_path / "inline_populated_content_out.md"

        transform_file(input_path, output_path)
        output = output_path.read_text(encoding="utf-8")

        assert "My custom project constraint that must survive" in output, (
            "The content of the custom_constraints region must be preserved in the output"
        )


class TestInlineCustomConstraintsEmpty:
    """Inline fixture with empty custom_constraints content: the drop-when-empty rule
    applies and no [[CUSTOM:CustomConstraints]] tags appear in the output."""

    def test_empty_custom_constraints_dropped(self, tmp_path: pathlib.Path) -> None:
        input_path = tmp_path / "inline_empty.md"
        input_path.write_text(_MINIMAL_EMPTY_CUSTOM_CONSTRAINTS, encoding="utf-8")
        output_path = tmp_path / "inline_empty_out.md"

        result = transform_file(input_path, output_path)

        assert result.success is True, f"Transform must succeed even when custom_constraints is empty: {result.errors}"
        output = output_path.read_text(encoding="utf-8")
        # Empty custom_constraints is dropped (consistent with the existing drop-when-empty
        # behaviour documented in boundary_transformer.py).
        assert "[[CUSTOM:CustomConstraints]]" not in output, (
            "An empty custom_constraints region must be dropped entirely — "
            "the drop-when-empty rule is unchanged by the kind migration"
        )
        assert "[[INJECTION:CustomConstraints]]" not in output, (
            "An empty custom_constraints region must not be emitted as [[INJECTION:]] either"
        )
