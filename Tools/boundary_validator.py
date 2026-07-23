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
    CANONICAL_INJECTIONS,
    CANONICAL_SECTIONS,
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
    """Represents a single validation error found in an agent instruction file.

    The __str__ method produces the CLI output format:
        <filepath>:<line>: <error-code> <message>
    """
    file_path: pathlib.Path
    line_number: int
    error_code: str       # One of "E000" through "E009"
    message: str

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
    3. Open/close tag names match within each pair (E003).
    4. Only the 19 canonical boundary names are used (E004).
    5. All instructional content after YAML frontmatter falls within some
       boundary (E005). Blank lines and --- separators between sections
       are exempt.
    6. No duplicate boundary names within a single file (E006).
    7. Sections appear in canonical order (E007).
    8. Injection boundaries are nested inside their correct parent section (E008).
    9. No unexpected YAML frontmatter keys (E009).

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
    # All boundary names seen so far (for E006 duplicate detection).
    seen_names: set[str] = set()
    # Index into CANONICAL_SECTIONS of the last section tag seen (for E007).
    last_section_order_idx: int = -1

    for i, line in enumerate(lines):
        # Skip frontmatter lines (indices 0 through fm_end_idx, inclusive).
        if fm_end_idx is not None and i <= fm_end_idx:
            continue

        line_num = i + 1
        stripped = line.strip()
        m = TAG_PATTERN.match(stripped)

        if m:
            # This line is a boundary tag.
            is_close = bool(m.group("close"))
            kind_str = m.group("kind")
            name = m.group("name")
            kind = BoundaryKind(kind_str)

            # Check whether the name is canonical (E004).
            is_canonical = (
                (kind == BoundaryKind.SECTION and name in CANONICAL_SECTIONS)
                or (kind == BoundaryKind.INJECTION and name in CANONICAL_INJECTIONS)
            )
            if not is_canonical:
                errors.append(ValidationError(
                    file_path=file_path,
                    line_number=line_num,
                    error_code="E004",
                    message=f"Non-canonical {kind_str} name: {name!r}",
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
                    # Check canonical section order (E007) — only for canonical names.
                    if is_canonical:
                        section_idx = CANONICAL_SECTIONS.index(name)
                        if section_idx <= last_section_order_idx:
                            errors.append(ValidationError(
                                file_path=file_path,
                                line_number=line_num,
                                error_code="E007",
                                message=(
                                    f"Section {name!r} appears out of canonical order "
                                    f"(expected after index {last_section_order_idx} "
                                    f"in canonical sequence)"
                                ),
                            ))
                        # Update order tracker even for out-of-order sections so
                        # subsequent checks measure relative to the latest seen.
                        last_section_order_idx = max(last_section_order_idx, section_idx)

                    section_stack.append((name, line_num))

                elif kind == BoundaryKind.INJECTION:
                    # Check that the injection is inside its required parent section (E008).
                    if is_canonical:
                        required_parent = INJECTION_PARENT_MAP.get(name)
                        current_section = section_stack[-1][0] if section_stack else None
                        if required_parent is not None and current_section != required_parent:
                            errors.append(ValidationError(
                                file_path=file_path,
                                line_number=line_num,
                                error_code="E008",
                                message=(
                                    f"Injection {name!r} must be inside "
                                    f"section {required_parent!r}, "
                                    f"but is inside {current_section!r}"
                                ),
                            ))

                    injection_stack.append((name, line_num))

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

        else:
            # Not a boundary tag — check for content outside boundaries (E005).
            # Blank lines and '---' section separators are exempt.
            if stripped and stripped != "---":
                if not section_stack and not injection_stack:
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
        for _file_path, file_errors in sorted(results.items()):
            for error in file_errors:
                print(str(error))
        sys.exit(1 if results else 0)
    else:
        errors = validate_file(args.path)
        for error in errors:
            print(str(error))
        sys.exit(1 if errors else 0)
