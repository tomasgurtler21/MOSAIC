"""Format validator for MOSAIC agent instruction .md files.

Validates that boundary tags are correctly formed, matched, ordered, nested,
and that all instructional content falls within a boundary. Also validates
that no unexpected YAML frontmatter keys are present.

CLI usage:
    python Tools/boundary_validator.py <path> [--batch]

Exit codes:
    0 -- all files pass validation
    1 -- one or more files have validation errors
"""
from __future__ import annotations

import dataclasses
import pathlib
import re

from boundary_constants import (
    CANONICAL_DEPLOYED,
    CANONICAL_ORDER,
    CANONICAL_SECTIONS,
    DEPLOYED_PARENT_MAP,
    EXPECTED_MARKER,
    INJECTION_PARENT_MAP,
    KNOWN_FRONTMATTER_KEYS,
    TAG_PATTERN,
    BoundaryKind,
)

# Regex to extract a top-level YAML key from a frontmatter line.
# Matches lines like: "key: value" or "key:" at the start of a line
# (no leading whitespace, key is a valid YAML bare key).
_YAML_KEY_PATTERN: re.Pattern[str] = re.compile(r"^([A-Za-z_][A-Za-z0-9_]*)\s*:")


@dataclasses.dataclass
class ValidationError:
    """Represents a single validation finding found in an agent instruction file.

    The __str__ method produces the CLI output format:
        <filepath>:<line>: <error-code> <message>

    severity is "error" for findings that fail validation, or "advice" for
    informational findings that are reported but never fatal. The CLI exits 1
    only when at least one finding has severity "error".
    """
    file_path: pathlib.Path
    line_number: int
    error_code: str       # One of "E000" through "E011"
    message: str
    severity: str = "error"  # "error" or "advice"; defaults to "error"

    def __str__(self) -> str:
        """Format as '<filepath>:<line>: <error-code> <message>'.

        file_path is rendered as-is (preserving whatever was passed in --
        absolute or relative). line_number is a plain integer (no zero-padding).
        No trailing whitespace.
        """
        return f"{self.file_path}:{self.line_number}: {self.error_code} {self.message}"


def _parse_frontmatter(
    lines: list[str],
    file_path: pathlib.Path,
) -> tuple[int | None, list[ValidationError]]:
    """Parse YAML frontmatter lines and validate keys.

    Args:
        lines: All lines of the file (from splitlines()).
        file_path: Path to the file (for error reporting).

    Returns:
        A tuple of (end_idx, errors) where:
        - end_idx: 0-based index of the closing '---' line, or None if malformed.
        - errors: E000 if frontmatter is malformed, E009 for unexpected keys.
          If E000 is returned, further validation should be skipped.
    """
    # File must start with '---'
    if not lines or lines[0].strip() != "---":
        return None, [ValidationError(
            file_path=file_path,
            line_number=1,
            error_code="E000",
            message="Malformed YAML frontmatter: expected '---' on line 1",
        )]

    # Find the closing '---'
    for i in range(1, len(lines)):
        if lines[i].strip() == "---":
            # Found closing separator at index i. Extract and validate keys.
            errors: list[ValidationError] = []
            for j in range(1, i):
                key_match = _YAML_KEY_PATTERN.match(lines[j])
                if key_match:
                    key = key_match.group(1)
                    if key not in KNOWN_FRONTMATTER_KEYS:
                        errors.append(ValidationError(
                            file_path=file_path,
                            line_number=j + 1,  # 1-based
                            error_code="E009",
                            message=f"Unexpected frontmatter key: {key!r}",
                        ))
            return i, errors

    # No closing '---' found — malformed frontmatter
    return None, [ValidationError(
        file_path=file_path,
        line_number=len(lines),
        error_code="E000",
        message="Malformed YAML frontmatter: missing closing '---'",
    )]


def validate_file(file_path: pathlib.Path) -> list[ValidationError]:
    """Validate the boundary structure of a single MOSAIC agent instruction file.

    Checks that boundary tags are correctly formed, matched, ordered, nested,
    and that all instructional content falls within a boundary. Also validates
    that no unexpected YAML frontmatter keys are present.

    Args:
        file_path: Path to the .md file to validate.

    Returns:
        A list of ValidationError instances. Empty list means the file is valid.

    Validation rules applied:
    1. Every [[SECTION:Name]] has a matching [[/SECTION:Name]] (E001/E002).
    2. Every [[INJECTION:Name]] has a matching [[/INJECTION:Name]] (E001/E002).
    3. Every [[DEPLOYED:Name]] has a matching [[/DEPLOYED:Name]] (E001/E002).
    4. Open/close tag names match within each pair (E003).
    5. Only canonical boundary names are used under the right marker (E004/E010/E011).
    6. All instructional content after YAML frontmatter falls within some
       boundary (E005). Blank lines and --- separators between sections
       are exempt.
    7. No duplicate boundary names within a single file (E006).
    8. Sections and top-level deployed names appear in canonical order (E007).
    9. Injection boundaries are nested inside their correct parent section (E008).
    10. No unexpected YAML frontmatter keys (E009).
    11. A canonical name declared under the wrong marker (E010).
    12. An unknown [[DEPLOYED:]] name (E011).

    Edge cases:
    - If YAML frontmatter is malformed (missing closing '---' or
      unparseable YAML), returns a single ValidationError with
      error_code 'E000' and no further rules are checked.
    - Lines resembling but not exactly matching TAG_PATTERN (e.g.,
      '[[SECTION: Identity]]' with a space after the colon) are
      treated as ordinary content and may trigger E005 if they fall
      outside a boundary.
    """
    errors: list[ValidationError] = []

    try:
        content = file_path.read_text(encoding="utf-8")
    except OSError as exc:
        return [ValidationError(
            file_path=file_path,
            line_number=0,
            error_code="E000",
            message=str(exc),
        )]

    lines = content.splitlines()

    # --- Parse and validate YAML frontmatter ---
    fm_end_idx, fm_errors = _parse_frontmatter(lines, file_path)
    if any(e.error_code == "E000" for e in fm_errors):
        # Malformed frontmatter — return only E000, skip further validation
        return fm_errors
    errors.extend(fm_errors)

    # --- Validate boundary structure ---
    # Stacks hold (name, line_number) for currently open boundaries.
    section_stack: list[tuple[str, int]] = []
    injection_stack: list[tuple[str, int]] = []
    deployed_stack: list[tuple[str, int]] = []
    # All boundary names seen so far (for E006 duplicate detection).
    seen_names: set[str] = set()
    # Index into CANONICAL_ORDER of the last canonical slot seen (for E007).
    # CANONICAL_ORDER covers both section opens and top-level deployed opens.
    last_order_idx: int = -1
    # Track fenced code blocks so tags inside them are not validated.
    in_fenced_block: bool = False

    for i, line in enumerate(lines):
        # Skip frontmatter lines (indices 0 through fm_end_idx, inclusive).
        if fm_end_idx is not None and i <= fm_end_idx:
            continue

        line_num = i + 1
        stripped = line.strip()

        # Track fenced code blocks; boundary tags inside them are not checked.
        if stripped.startswith("```"):
            in_fenced_block = not in_fenced_block

        if in_fenced_block:
            continue

        m = TAG_PATTERN.match(stripped)

        if m:
            # This line is a boundary tag.
            is_close = bool(m.group("close"))
            kind_str = m.group("kind")
            name = m.group("name")
            kind = BoundaryKind(kind_str)

            # Check whether the name is canonical for its marker kind (E004/E010/E011).
            expected = EXPECTED_MARKER.get(name)
            if expected is not None and expected != kind:
                # Canonical name declared under the wrong marker.
                errors.append(ValidationError(
                    file_path=file_path,
                    line_number=line_num,
                    error_code="E010",
                    message=(
                        f"Name {name!r} must be declared with "
                        f"[[{expected.value}:]] but found [[{kind_str}:]]"
                    ),
                ))
                # Count as non-canonical for further checks below, but still
                # track stacks so mismatched-name and unclosed errors are accurate.
                is_canonical = False
            elif kind == BoundaryKind.SECTION:
                is_canonical = name in CANONICAL_SECTIONS
                if not is_canonical:
                    errors.append(ValidationError(
                        file_path=file_path,
                        line_number=line_num,
                        error_code="E004",
                        message=f"Non-canonical SECTION name: {name!r}",
                    ))
            elif kind == BoundaryKind.INJECTION:
                # Injection names are open: any name is valid and is preserved.
                is_canonical = True
            else:  # DEPLOYED
                is_canonical = name in CANONICAL_DEPLOYED
                if not is_canonical:
                    # Unknown DEPLOYED name — no generator exists for it.
                    errors.append(ValidationError(
                        file_path=file_path,
                        line_number=line_num,
                        error_code="E011",
                        message=f"Unrecognised tool-managed boundary name: {name!r}",
                    ))

            if not is_close:
                # --- Opening tag ---
                # Check for duplicate boundary name (E006).
                if name in seen_names:
                    errors.append(ValidationError(
                        file_path=file_path,
                        line_number=line_num,
                        error_code="E006",
                        message=f"Duplicate boundary name: {name!r}",
                    ))
                else:
                    seen_names.add(name)

                if kind == BoundaryKind.SECTION:
                    # Check canonical document order (E007) — only for canonical names.
                    if is_canonical:
                        order_idx = CANONICAL_ORDER.index(name)
                        if order_idx <= last_order_idx:
                            errors.append(ValidationError(
                                file_path=file_path,
                                line_number=line_num,
                                error_code="E007",
                                message=(
                                    f"Section {name!r} appears out of canonical order "
                                    f"(expected after index {last_order_idx} "
                                    f"in canonical sequence)"
                                ),
                            ))
                        last_order_idx = max(last_order_idx, order_idx)

                    section_stack.append((name, line_num))

                elif kind == BoundaryKind.INJECTION:
                    # Check that the injection is inside its required parent section (E008).
                    # INJECTION_PARENT_MAP sentinel values:
                    #   None (absent key): no parent requirement — no check performed
                    #   ""   (present, empty): must be at body top level (not inside any section)
                    #   str  (non-empty): must be inside that named section
                    # Injection names are open (is_canonical is always True for INJECTION), so
                    # the parent-map lookup runs unconditionally for any name present in the map.
                    required_parent = INJECTION_PARENT_MAP.get(name)
                    current_section = section_stack[-1][0] if section_stack else None
                    if required_parent is None:
                        pass  # no parent requirement
                    elif required_parent == "":
                        # Must be at body top level — not nested inside any section.
                        if current_section is not None:
                            errors.append(ValidationError(
                                file_path=file_path,
                                line_number=line_num,
                                error_code="E008",
                                message=(
                                    f"Injection {name!r} must be at body top level "
                                    f"but is inside section {current_section!r}"
                                ),
                                severity="advice",
                            ))
                    elif current_section != required_parent:
                        errors.append(ValidationError(
                            file_path=file_path,
                            line_number=line_num,
                            error_code="E008",
                            message=(
                                f"Injection {name!r} must be inside "
                                f"section {required_parent!r}, "
                                f"but is inside {current_section!r}"
                            ),
                            severity="advice",
                        ))

                    injection_stack.append((name, line_num))

                else:  # DEPLOYED
                    if is_canonical:
                        required_parent = DEPLOYED_PARENT_MAP.get(name)
                        current_section = section_stack[-1][0] if section_stack else None

                        if required_parent is None:
                            # Must be at body top level (not inside any section).
                            if current_section is not None:
                                errors.append(ValidationError(
                                    file_path=file_path,
                                    line_number=line_num,
                                    error_code="E008",
                                    message=(
                                        f"Deployed region {name!r} must be at body top level "
                                        f"but is inside section {current_section!r}"
                                    ),
                                ))
                            else:
                                # Top-level deployed: participates in canonical order check.
                                if name in CANONICAL_ORDER:
                                    order_idx = CANONICAL_ORDER.index(name)
                                    if order_idx <= last_order_idx:
                                        errors.append(ValidationError(
                                            file_path=file_path,
                                            line_number=line_num,
                                            error_code="E007",
                                            message=(
                                                f"Deployed region {name!r} appears out of "
                                                f"canonical order (expected after index "
                                                f"{last_order_idx} in canonical sequence)"
                                            ),
                                        ))
                                    last_order_idx = max(last_order_idx, order_idx)
                        else:
                            # Must be inside the required parent section.
                            if current_section != required_parent:
                                errors.append(ValidationError(
                                    file_path=file_path,
                                    line_number=line_num,
                                    error_code="E008",
                                    message=(
                                        f"Deployed region {name!r} must be inside "
                                        f"section {required_parent!r}, "
                                        f"but is inside {current_section!r}"
                                    ),
                                ))

                    deployed_stack.append((name, line_num))

            else:
                # --- Closing tag ---
                if kind == BoundaryKind.SECTION:
                    if not section_stack:
                        errors.append(ValidationError(
                            file_path=file_path,
                            line_number=line_num,
                            error_code="E002",
                            message=f"Unmatched closing tag [[/SECTION:{name}]] (no open tag)",
                        ))
                    else:
                        top_name, _top_line = section_stack[-1]
                        if top_name != name:
                            errors.append(ValidationError(
                                file_path=file_path,
                                line_number=line_num,
                                error_code="E003",
                                message=(
                                    f"Mismatched open/close names: "
                                    f"opened {top_name!r}, closed {name!r}"
                                ),
                            ))
                        section_stack.pop()

                elif kind == BoundaryKind.INJECTION:
                    if not injection_stack:
                        errors.append(ValidationError(
                            file_path=file_path,
                            line_number=line_num,
                            error_code="E002",
                            message=f"Unmatched closing tag [[/INJECTION:{name}]] (no open tag)",
                        ))
                    else:
                        top_name, _top_line = injection_stack[-1]
                        if top_name != name:
                            errors.append(ValidationError(
                                file_path=file_path,
                                line_number=line_num,
                                error_code="E003",
                                message=(
                                    f"Mismatched open/close names: "
                                    f"opened {top_name!r}, closed {name!r}"
                                ),
                            ))
                        injection_stack.pop()

                else:  # DEPLOYED
                    if not deployed_stack:
                        errors.append(ValidationError(
                            file_path=file_path,
                            line_number=line_num,
                            error_code="E002",
                            message=f"Unmatched closing tag [[/DEPLOYED:{name}]] (no open tag)",
                        ))
                    else:
                        top_name, _top_line = deployed_stack[-1]
                        if top_name != name:
                            errors.append(ValidationError(
                                file_path=file_path,
                                line_number=line_num,
                                error_code="E003",
                                message=(
                                    f"Mismatched open/close names: "
                                    f"opened {top_name!r}, closed {name!r}"
                                ),
                            ))
                        deployed_stack.pop()

        else:
            # Not a boundary tag — check for content outside boundaries (E005).
            # Blank lines and '---' section separators are exempt.
            if stripped and stripped != "---":
                if not section_stack and not injection_stack and not deployed_stack:
                    errors.append(ValidationError(
                        file_path=file_path,
                        line_number=line_num,
                        error_code="E005",
                        message="Content outside all boundaries",
                    ))

    # Any remaining open tags are unmatched (E001).
    for name, open_line_num in section_stack:
        errors.append(ValidationError(
            file_path=file_path,
            line_number=open_line_num,
            error_code="E001",
            message=f"Unmatched open tag [[SECTION:{name}]] (no close tag found)",
        ))
    for name, open_line_num in injection_stack:
        errors.append(ValidationError(
            file_path=file_path,
            line_number=open_line_num,
            error_code="E001",
            message=f"Unmatched open tag [[INJECTION:{name}]] (no close tag found)",
        ))
    for name, open_line_num in deployed_stack:
        errors.append(ValidationError(
            file_path=file_path,
            line_number=open_line_num,
            error_code="E001",
            message=f"Unmatched open tag [[DEPLOYED:{name}]] (no close tag found)",
        ))

    return errors


def validate_batch(directory: pathlib.Path) -> dict[pathlib.Path, list[ValidationError]]:
    """Recursively validate all .md files under a directory.

    Args:
        directory: Root directory to search for .md files.

    Returns:
        A dict mapping each file path to its list of ValidationError instances.
        Files with no errors are omitted from the dict. If a file cannot
        be read due to an OS error (permission denied, deleted mid-run,
        etc.), it is included in the result dict with a single
        ValidationError whose error_code is 'E000', line_number is 0,
        and message contains the exception description.
    """
    results: dict[pathlib.Path, list[ValidationError]] = {}

    for md_file in sorted(directory.rglob("*.md")):
        try:
            errors = validate_file(md_file)
        except OSError as exc:
            errors = [ValidationError(
                file_path=md_file,
                line_number=0,
                error_code="E000",
                message=str(exc),
            )]

        if errors:
            results[md_file] = errors

    return results


if __name__ == "__main__":
    import argparse
    import sys

    parser = argparse.ArgumentParser(
        description="Validate MOSAIC agent instruction .md files for correct boundary structure."
    )
    parser.add_argument(
        "path",
        type=pathlib.Path,
        help="Path to a .md file, or a directory when --batch is used.",
    )
    parser.add_argument(
        "--batch",
        action="store_true",
        help="Recursively validate all .md files under path.",
    )

    args = parser.parse_args()

    if args.batch:
        results = validate_batch(args.path)
        has_error = False
        for _file_path, file_errors in sorted(results.items()):
            for error in file_errors:
                print(str(error))
                if error.severity == "error":
                    has_error = True
        sys.exit(1 if has_error else 0)
    else:
        errors = validate_file(args.path)
        for error in errors:
            print(str(error))
        has_error = any(e.severity == "error" for e in errors)
        sys.exit(1 if has_error else 0)
