"""Python-side parity tests for the four-marker vocabulary.

Verifies that the Python implementation (boundary_constants and boundary_validator)
satisfies the same cross-implementation parity table as the Go implementation.
Neither implementation's coverage stands in for the other's; both are verified
independently.

Parity table properties verified here (Python side):
  - All four BoundaryKind members exist and are distinct.
  - TAG_PATTERN recognises CUSTOM open/close tags with any valid name.
  - The validator's CUSTOM arm: any name is accepted (open name set, no E004).
  - The validator does not apply document-order checks (E007) to CUSTOM.
  - The validator does not apply required-parent checks (E008) to CUSTOM.
  - Open/close balance for CUSTOM is tracked (E001 for missing close, E002 for orphan close).
  - Duplicate CUSTOM names are rejected (E006).
  - INJECTION and DEPLOYED name-set rules are unchanged after CUSTOM is added.
"""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_constants import BoundaryKind, TAG_PATTERN, open_tag, close_tag  # noqa: E402
from boundary_validator import ValidationError, validate_file  # noqa: E402


# ---------------------------------------------------------------------------
# Shared helpers: write inline fixtures to tmp_path and validate them
# ---------------------------------------------------------------------------

def _write_and_validate(tmp_path: pathlib.Path, filename: str, content: str) -> list[ValidationError]:
    """Write content to a temp file and return the validation result."""
    fpath = tmp_path / filename
    fpath.write_text(content, encoding="utf-8")
    return validate_file(fpath)


def _error_codes(errors: list[ValidationError]) -> list[str]:
    """Return the error codes from a validation result as a sorted list."""
    return sorted(e.error_code for e in errors)


def _has_error_code(errors: list[ValidationError], code: str) -> bool:
    """Return True if any error in the result has the given error code."""
    return any(e.error_code == code for e in errors)


# ---------------------------------------------------------------------------
# Inline fixture templates — minimal well-formed deployed-style documents with
# boundary-tagged content. The validator operates on arbitrary boundary-tagged
# files; it does not require specific section headings.
# ---------------------------------------------------------------------------

_FM = """\
---
id: 1
version: 1.0.0
name: parity-test
description: Parity test file
model: claude/sonnet
tools: []
transform_version: 3.0.0
injections_version: 1.2.0
mode: subagent
---
"""

# A valid deployed-style file with a single [[CUSTOM:]] region.
_CUSTOM_SINGLE = _FM + """\
[[SECTION:Identity]]
# Parity Agent

Some text.

[[CUSTOM:ProjectNotes]]
User-owned notes.
[[/CUSTOM:ProjectNotes]]
[[/SECTION:Identity]]
"""

# A valid deployed-style file with a [[CUSTOM:]] region using an arbitrary unknown name.
_CUSTOM_UNKNOWN_NAME = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[CUSTOM:SomeCompletelyArbitraryUnregisteredName]]
No registry, open name set — this must be valid.
[[/CUSTOM:SomeCompletelyArbitraryUnregisteredName]]
[[/SECTION:Identity]]
"""

# A deployed-style file with a [[CUSTOM:]] region using a compound name.
_CUSTOM_COMPOUND_NAME = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[CUSTOM:Project:my-feature]]
Compound custom name — valid per name syntax.
[[/CUSTOM:Project:my-feature]]
[[/SECTION:Identity]]
"""

# A deployed-style file with multiple [[CUSTOM:]] regions at different nesting levels.
_CUSTOM_MULTIPLE = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[CUSTOM:TopLevelNote]]
Top-level custom region.
[[/CUSTOM:TopLevelNote]]

[[CUSTOM:AnotherNote]]
Second custom region.
[[/CUSTOM:AnotherNote]]
[[/SECTION:Identity]]
"""

# A file with a [[CUSTOM:]] region missing its close tag — must trigger E001.
_CUSTOM_MISSING_CLOSE = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[CUSTOM:ProjectNotes]]
No closing tag follows.
[[/SECTION:Identity]]
"""

# A file with an orphan [[/CUSTOM:]] close tag — must trigger E002.
_CUSTOM_ORPHAN_CLOSE = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[/CUSTOM:ProjectNotes]]
[[/SECTION:Identity]]
"""

# A file with a duplicate [[CUSTOM:]] name — must trigger E006.
_CUSTOM_DUPLICATE = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[CUSTOM:ProjectNotes]]
First occurrence.
[[/CUSTOM:ProjectNotes]]

[[CUSTOM:ProjectNotes]]
Second occurrence — duplicate name must be rejected.
[[/CUSTOM:ProjectNotes]]
[[/SECTION:Identity]]
"""

# A file with a [[CUSTOM:]] region nested inside a [[DEPLOYED:]] region.
# Must be accepted: custom regions may nest inside DEPLOYED.
_CUSTOM_IN_DEPLOYED = _FM + """\
[[SECTION:Constraints]]
## Constraints

[[DEPLOYED:HarnessConstraints]]
Canonical harness constraints content.
[[CUSTOM:TeamNote]]
A custom note inside the deployed region — legal per the parity table.
[[/CUSTOM:TeamNote]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
"""


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: BoundaryKind
# ---------------------------------------------------------------------------

class TestFourMarkerParityBoundaryKind:
    """BoundaryKind must have exactly four members: SECTION, INJECTION, DEPLOYED, CUSTOM.

    This mirrors the Go test that verifies all four NodeKind values are distinct.
    Neither implementation's coverage substitutes for the other's.
    """

    def test_four_members_total(self) -> None:
        assert len(BoundaryKind) == 4, (
            f"BoundaryKind must have exactly four members (SECTION, INJECTION, DEPLOYED, CUSTOM), "
            f"got {len(BoundaryKind)}: {list(BoundaryKind)}"
        )

    def test_all_four_members_distinct(self) -> None:
        kinds = list(BoundaryKind)
        for i in range(len(kinds)):
            for j in range(i + 1, len(kinds)):
                assert kinds[i] != kinds[j], (
                    f"BoundaryKind members are not distinct: {kinds[i]!r} == {kinds[j]!r}"
                )

    def test_section_member_value(self) -> None:
        assert BoundaryKind.SECTION.value == "SECTION"

    def test_injection_member_value(self) -> None:
        assert BoundaryKind.INJECTION.value == "INJECTION"

    def test_deployed_member_value(self) -> None:
        assert BoundaryKind.DEPLOYED.value == "DEPLOYED"

    def test_custom_member_value(self) -> None:
        assert BoundaryKind.CUSTOM.value == "CUSTOM"

    def test_custom_is_distinct_from_injection(self) -> None:
        assert BoundaryKind.CUSTOM != BoundaryKind.INJECTION, (
            "BoundaryKind.CUSTOM and BoundaryKind.INJECTION must be distinct — "
            "parity table: content owner = project for both, but they are separate marker kinds"
        )


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: TAG_PATTERN accepts CUSTOM
# ---------------------------------------------------------------------------

class TestFourMarkerParityTagPattern:
    """TAG_PATTERN must recognise CUSTOM tags with the same name syntax rules as the
    other three kinds. This mirrors the Go test that parseBoundaryTag handles NodeCustom."""

    def test_custom_open_tag_matches(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:CustomConstraints]]") is not None, (
            "TAG_PATTERN must match a CUSTOM open tag — parity table: CUSTOM recognised"
        )

    def test_custom_close_tag_matches(self) -> None:
        assert TAG_PATTERN.match("[[/CUSTOM:CustomConstraints]]") is not None, (
            "TAG_PATTERN must match a CUSTOM close tag"
        )

    def test_custom_empty_name_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:]]") is None, (
            "TAG_PATTERN must not match [[CUSTOM:]] with an empty name — "
            "parity table: empty name matches = no for all four kinds"
        )

    def test_custom_name_starting_with_digit_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:1invalid]]") is None

    def test_compound_custom_name_matches(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:Project:my-feature]]") is not None

    def test_all_four_kinds_accepted_by_tag_pattern(self) -> None:
        """All four kinds must be in the TAG_PATTERN alternation."""
        examples = {
            "SECTION":   "[[SECTION:Identity]]",
            "INJECTION": "[[INJECTION:IdentityExtension]]",
            "DEPLOYED":  "[[DEPLOYED:CommunicationProtocol]]",
            "CUSTOM":    "[[CUSTOM:ProjectNotes]]",
        }
        for kind, tag in examples.items():
            m = TAG_PATTERN.match(tag)
            assert m is not None, (
                f"TAG_PATTERN must match a {kind} open tag, but did not match {tag!r}"
            )
            assert m.group("kind") == kind, (
                f"TAG_PATTERN 'kind' group must be {kind!r} for {tag!r}, got {m.group('kind')!r}"
            )


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: open_tag / close_tag helpers render CUSTOM correctly
# ---------------------------------------------------------------------------

class TestFourMarkerParityTagHelpers:
    """open_tag and close_tag must produce correct CUSTOM boundary strings, matching
    the Go parseBoundaryTag inverse."""

    def test_open_tag_custom(self) -> None:
        assert open_tag(BoundaryKind.CUSTOM, "CustomConstraints") == "[[CUSTOM:CustomConstraints]]"

    def test_close_tag_custom(self) -> None:
        assert close_tag(BoundaryKind.CUSTOM, "CustomConstraints") == "[[/CUSTOM:CustomConstraints]]"

    def test_open_tag_custom_round_trips_through_tag_pattern(self) -> None:
        result = open_tag(BoundaryKind.CUSTOM, "ProjectNotes")
        m = TAG_PATTERN.match(result)
        assert m is not None, f"open_tag(CUSTOM, 'ProjectNotes') = {result!r} must match TAG_PATTERN"
        assert m.group("kind") == "CUSTOM"
        assert m.group("name") == "ProjectNotes"
        assert m.group("close") == ""


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: open name-set rule (validator does not apply E004 to CUSTOM)
# ---------------------------------------------------------------------------

class TestCustomNameSetIsOpen:
    """The validator must accept any [[CUSTOM:]] name without an E004-class error.

    Parity table: CUSTOM name set = open; unknown name is NOT an error.
    This mirrors the Go assertion that ClassifyRegion(NodeCustom, anyName) never errors.
    """

    def test_known_custom_constraints_name_accepted(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_known.md", _CUSTOM_SINGLE)
        non_e004 = [e for e in errors if e.error_code != "E004"]
        # There must be no E004 for a [[CUSTOM:]] region regardless of name.
        e004_errors = [e for e in errors if e.error_code == "E004"]
        assert len(e004_errors) == 0, (
            f"Validator must not report E004 for [[CUSTOM:ProjectNotes]] — "
            f"CUSTOM name set is open. E004 errors: {e004_errors}"
        )

    def test_arbitrary_unknown_name_accepted_without_e004(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_unknown.md", _CUSTOM_UNKNOWN_NAME)
        e004_errors = [e for e in errors if e.error_code == "E004"]
        assert len(e004_errors) == 0, (
            f"Validator must not report E004 for an unknown [[CUSTOM:]] name — "
            f"CUSTOM name set is open (same rule as INJECTION). Got: {e004_errors}"
        )

    def test_compound_custom_name_accepted_without_e004(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_compound.md", _CUSTOM_COMPOUND_NAME)
        e004_errors = [e for e in errors if e.error_code == "E004"]
        assert len(e004_errors) == 0, (
            f"Validator must not report E004 for a compound [[CUSTOM:]] name. Got: {e004_errors}"
        )

    def test_multiple_different_custom_names_accepted(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_multiple.md", _CUSTOM_MULTIPLE)
        e004_errors = [e for e in errors if e.error_code == "E004"]
        assert len(e004_errors) == 0, (
            f"Validator must accept multiple [[CUSTOM:]] regions with arbitrary names — "
            f"open name set means no E004 for any of them. Got: {e004_errors}"
        )


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: no document-order check (E007) for CUSTOM
# ---------------------------------------------------------------------------

class TestCustomNoDocumentOrderCheck:
    """The validator must not apply E007 (document-order) to [[CUSTOM:]] regions.

    Parity table: document-order check = not applied for CUSTOM.
    Custom regions are not canonical slots; they have no required position.
    """

    def test_custom_region_out_of_canonical_order_no_e007(self, tmp_path: pathlib.Path) -> None:
        # A [[CUSTOM:]] region appearing before the canonical sections should not
        # trigger E007. E007 only applies to SECTION and DEPLOYED canonical slots.
        content = _FM + """\
[[CUSTOM:EarlyNote]]
A custom region before any canonical section — must not trigger E007.
[[/CUSTOM:EarlyNote]]

[[SECTION:Identity]]
# Parity Agent
[[/SECTION:Identity]]
"""
        errors = _write_and_validate(tmp_path, "custom_early.md", content)
        e007_errors = [e for e in errors if e.error_code == "E007"]
        assert len(e007_errors) == 0, (
            f"Validator must not report E007 for a [[CUSTOM:]] region — "
            f"parity table: document-order check not applied to CUSTOM. Got: {e007_errors}"
        )


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: no required-parent check (E008) for CUSTOM
# ---------------------------------------------------------------------------

class TestCustomNoRequiredParentCheck:
    """The validator must not apply E008 (required-parent) to [[CUSTOM:]] regions.

    Parity table: required-parent check = not applied for CUSTOM.
    There is no CUSTOM_PARENT_MAP and none should be added.
    """

    def test_top_level_custom_region_no_e008(self, tmp_path: pathlib.Path) -> None:
        content = _FM + """\
[[CUSTOM:TopLevelNote]]
A custom region at body top level — no parent required, must not trigger E008.
[[/CUSTOM:TopLevelNote]]

[[SECTION:Identity]]
# Parity Agent
[[/SECTION:Identity]]
"""
        errors = _write_and_validate(tmp_path, "custom_toplevel.md", content)
        e008_errors = [e for e in errors if e.error_code == "E008"]
        assert len(e008_errors) == 0, (
            f"Validator must not report E008 for a top-level [[CUSTOM:]] region — "
            f"parity table: required-parent check not applied to CUSTOM. Got: {e008_errors}"
        )

    def test_custom_inside_non_standard_section_no_e008(self, tmp_path: pathlib.Path) -> None:
        content = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[CUSTOM:LocalNote]]
Custom region in a non-standard section — valid, no parent restriction.
[[/CUSTOM:LocalNote]]
[[/SECTION:Identity]]
"""
        errors = _write_and_validate(tmp_path, "custom_nonsection.md", content)
        e008_errors = [e for e in errors if e.error_code == "E008"]
        assert len(e008_errors) == 0, (
            f"Validator must not report E008 for a [[CUSTOM:]] region in any parent. Got: {e008_errors}"
        )


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: open/close balance tracked for CUSTOM
# ---------------------------------------------------------------------------

class TestCustomOpenCloseBalance:
    """The validator must track open/close balance for [[CUSTOM:]] regions.

    Parity table: open/close balance = applied for CUSTOM (same as other kinds).
    An unclosed [[CUSTOM:]] tag must produce E001; an orphan [[/CUSTOM:]] must produce E002.
    """

    def test_missing_custom_close_tag_reports_e001(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_no_close.md", _CUSTOM_MISSING_CLOSE)
        assert _has_error_code(errors, "E001"), (
            f"Validator must report E001 for a [[CUSTOM:]] region missing its close tag — "
            f"open/close balance applies to CUSTOM. Got error codes: {_error_codes(errors)}"
        )

    def test_orphan_custom_close_tag_reports_e002(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_orphan.md", _CUSTOM_ORPHAN_CLOSE)
        assert _has_error_code(errors, "E002"), (
            f"Validator must report E002 for an orphan [[/CUSTOM:]] close tag — "
            f"open/close balance applies to CUSTOM. Got error codes: {_error_codes(errors)}"
        )

    def test_well_paired_custom_region_no_e001_e002(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_paired.md", _CUSTOM_SINGLE)
        balance_errors = [e for e in errors if e.error_code in ("E001", "E002")]
        assert len(balance_errors) == 0, (
            f"A well-paired [[CUSTOM:]] region must not trigger E001 or E002. "
            f"Got: {balance_errors}"
        )


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: duplicate CUSTOM names are rejected (E006)
# ---------------------------------------------------------------------------

class TestCustomDuplicateNameRejected:
    """A duplicate [[CUSTOM:]] name must produce E006.

    Parity table: duplicate-name check = applied for CUSTOM (names must be unique per file).
    """

    def test_duplicate_custom_name_reports_e006(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_duplicate.md", _CUSTOM_DUPLICATE)
        assert _has_error_code(errors, "E006"), (
            f"Validator must report E006 for a duplicate [[CUSTOM:]] name — "
            f"names are unique per file across all marker kinds. Got: {_error_codes(errors)}"
        )


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: CUSTOM may nest inside SECTION and DEPLOYED
# ---------------------------------------------------------------------------

class TestCustomNestingRules:
    """CUSTOM may nest inside SECTION regions and inside DEPLOYED regions.

    Parity table:
      - May nest inside SECTION: yes (for CUSTOM)
      - May nest inside DEPLOYED: yes (for CUSTOM)
    """

    def test_custom_nested_inside_section_accepted(self, tmp_path: pathlib.Path) -> None:
        errors = _write_and_validate(tmp_path, "custom_in_section.md", _CUSTOM_SINGLE)
        # _CUSTOM_SINGLE has [[CUSTOM:ProjectNotes]] inside [[SECTION:Identity]].
        # The only errors should not be about the nesting itself.
        nesting_errors = [e for e in errors if e.error_code in ("E001", "E002", "E003")]
        assert len(nesting_errors) == 0, (
            f"[[CUSTOM:]] nested inside [[SECTION:]] must not produce nesting errors. "
            f"Got: {nesting_errors}"
        )

    def test_custom_nested_inside_deployed_accepted(self, tmp_path: pathlib.Path) -> None:
        # _CUSTOM_IN_DEPLOYED has [[CUSTOM:TeamNote]] inside [[DEPLOYED:HarnessConstraints]].
        # This nesting is legal per the parity table.
        errors = _write_and_validate(tmp_path, "custom_in_deployed.md", _CUSTOM_IN_DEPLOYED)
        e001_e002 = [e for e in errors if e.error_code in ("E001", "E002")]
        assert len(e001_e002) == 0, (
            f"[[CUSTOM:]] nested inside [[DEPLOYED:]] must not produce balance errors — "
            f"parity table: CUSTOM may nest inside DEPLOYED. Got: {e001_e002}"
        )


# ---------------------------------------------------------------------------
# T9.3 — Four-marker parity: INJECTION and DEPLOYED name-set rules unchanged
# ---------------------------------------------------------------------------

class TestExistingNameSetRulesUnchanged:
    """Adding CUSTOM to the vocabulary must not alter the name-set rules for INJECTION
    and DEPLOYED. INJECTION names remain open; DEPLOYED names remain closed (E004).
    """

    def test_injection_open_name_still_accepted(self, tmp_path: pathlib.Path) -> None:
        content = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[INJECTION:SomeCompletelyUnknownInjectionName]]
Open injection name — must be accepted after CUSTOM is added to vocabulary.
[[/INJECTION:SomeCompletelyUnknownInjectionName]]
[[/SECTION:Identity]]
"""
        errors = _write_and_validate(tmp_path, "inj_open.md", content)
        e004_errors = [e for e in errors if e.error_code == "E004"]
        assert len(e004_errors) == 0, (
            f"Unknown [[INJECTION:]] name must not trigger E004 — injection names remain open. "
            f"Got: {e004_errors}"
        )

    def test_deployed_unknown_name_still_rejected_e011(self, tmp_path: pathlib.Path) -> None:
        content = _FM + """\
[[SECTION:Identity]]
# Parity Agent

[[DEPLOYED:TotallyUnknownDeployedName]]
Unknown deployed name — must still be rejected with E011 after CUSTOM is added.
[[/DEPLOYED:TotallyUnknownDeployedName]]
[[/SECTION:Identity]]
"""
        errors = _write_and_validate(tmp_path, "dep_unknown.md", content)
        assert _has_error_code(errors, "E011"), (
            f"Unknown [[DEPLOYED:]] name must still trigger E011 ('Unrecognised tool-managed "
            f"boundary name') — deployed names are closed. "
            f"Got error codes: {_error_codes(errors)}"
        )
