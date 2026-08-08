"""Stage 10 -- End-to-End Acceptance Verification.

Corpus-level acceptance tests that verify the run's overall success conditions
across a multi-file transform: properties no single earlier stage's per-file
unit tests can establish on their own.

The corpus is assembled entirely from repository fixtures under
tests/fixtures/ (see the `transformed_corpus` fixture below) -- never from any
path outside the repository. It covers both transform paths (generic: no
`transform_version` in source frontmatter; harness: has `transform_version`).

Properties asserted, one test class per property (see Stage-10/Plan.md):
  - Uniformity of every emitted deployed region across the whole corpus.
  - Isolation: content outside the touched sections is byte-identical.
  - Deletion completeness: no superseded fragment survives anywhere in the
    output tree.
  - Ordering constraints that the validator does not itself enforce.
  - Batch-level behaviour: skipping, the version fallback, validator-clean
    output (including the canonical bundle).
  - Report coverage: each detectable non-conformance class present in the
    corpus is named in the rendered report.

All tests are read-only with respect to `Tools/OldAgentsTransform/*.py`: this
module writes tests only. A finding here about an earlier stage's
implementation is reported, not fixed in this module.
"""
from __future__ import annotations

import dataclasses
import pathlib
import re
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_constants import BoundaryKind  # noqa: E402
from boundary_transformer import TransformResult, transform_file  # noqa: E402
from boundary_validator import validate_file  # noqa: E402
from deployed_blocks import DEFAULT_BUNDLE_PATH, load_bundle  # noqa: E402
from region_insertion import CONDUCT_REGIONS, find_section_spans  # noqa: E402
from non_conformance import (  # noqa: E402
    NC_HARNESS_PROSE,
    NC_JSON_ENVELOPE,
    NC_NO_INJECTIONS,
    render_report,
)
import file_classification as fc  # noqa: E402

from acceptance_helpers import (  # noqa: E402
    contains_fragment,
    extract_region,
    strip_tag_lines,
)

_FIXTURES_DIR = pathlib.Path(__file__).parent / "fixtures"

# Boundary tag line regex, used only to identify the legacy (single-bracket)
# marker syntax that predates this tool's tag vocabulary -- stripped alongside
# the new tag syntax so pre/post-transform prose can be compared directly.
_LEGACY_MARKER_RE = re.compile(r"^\[INJECTION: [\w-]+\]$")


# ---------------------------------------------------------------------------
# Corpus assembly (T10.1)
# ---------------------------------------------------------------------------

@dataclasses.dataclass
class CorpusEntry:
    label: str
    input_path: pathlib.Path
    output_path: pathlib.Path
    generic_ref_path: pathlib.Path | None
    result: TransformResult
    input_text: str
    output_text: str


# (label, input filename, generic-ref filename or None). No transform_version
# in the input's frontmatter -> generic path.
_GENERIC_CORPUS_SPECS: list[tuple[str, str, str | None]] = [
    ("generic_identity_regions", "generic_identity_regions_input.md", None),
    ("s3_generic_eh_ep", "s3_generic_eh_ep_input.md", None),
    ("s4_generic_constraints_regions", "s4_generic_constraints_regions_input.md", None),
    ("s6_generic_missing_derived_keys", "s6_generic_missing_derived_keys_input.md", None),
    ("generic_no_version_no_transform", "generic_no_version_no_transform_input.md", None),
    ("s8_generic_drift_probe_only_bullet", "s8_generic_drift_probe_only_bullet_input.md", None),
    ("stage10_json_envelope", "stage10_json_envelope_input.md", None),
    ("stage10_no_injections", "stage10_no_injections_input.md", None),
    ("stage10_harness_prose", "stage10_harness_prose_input.md", None),
]

# transform_version present -> harness path. Where a generic reference is
# needed it is named; "harness_only_no_version_degraded" deliberately omits
# one to exercise the automatic degraded-path fallback (AD present since an
# earlier stage; not an orchestrator file, so no hard failure).
_HARNESS_CORPUS_SPECS: list[tuple[str, str, str | None]] = [
    ("s3_harness_eh_ep", "s3_harness_eh_ep_input.md", "s3_harness_eh_ep_generic_ref.md"),
    ("s4_harness_constraints_regions", "s4_harness_constraints_regions_input.md",
     "s4_harness_constraints_regions_generic_ref.md"),
    ("harness_identity_regions", "harness_identity_regions_input.md",
     "harness_identity_regions_generic_ref.md"),
    ("s6_harness_id10", "s6_harness_id10_input.md", "s6_generic_ref_id51.md"),
    ("harness_only_no_version_degraded", "harness_only_no_version_input.md", None),
]

_ALL_CORPUS_SPECS = _GENERIC_CORPUS_SPECS + _HARNESS_CORPUS_SPECS

# Corpus members deliberately excluded from the validator-clean sweep: both
# are minimal, single-purpose fixtures authored to isolate the version
# fallback (one has a source section ordered ExecutionPhilosophy-before-
# OutputFormat; the other omits ErrorHandling/OutputFormat/ExecutionPhilosophy
# entirely). Neither property is something this run's fixes were meant to
# correct -- the transformer tags what it finds, it does not reorder or
# complete a file's sections -- so holding them to the full-schema validator
# would be testing the fixture's authored shape, not this run's behaviour.
_VALIDATOR_CLEAN_EXCLUDED_LABELS = {
    "generic_no_version_no_transform",
    "harness_only_no_version_degraded",
}

# Corpus members whose Identity section carries a real numbered Process list
# followed by an Authority Hierarchy block -- the subset against which the
# "nothing between the process list and ClosingProcedure" ordering constraint
# is meaningful. Other corpus members either lack a Process list (ClosingProcedure
# falls back to section-content-end) or lack Identity-region content entirely.
_HAS_PROCESS_LIST_LABELS = {
    "generic_identity_regions",
    "s3_generic_eh_ep",
    "s3_harness_eh_ep",
    "harness_identity_regions",
}


@pytest.fixture(scope="module")
def transformed_corpus(tmp_path_factory) -> list[CorpusEntry]:
    """Transform the whole acceptance corpus once per test module run.

    Every entry is read from Tools/OldAgentsTransform/tests/fixtures/ only.
    """
    out_dir = tmp_path_factory.mktemp("stage10_corpus")
    entries: list[CorpusEntry] = []
    for label, input_name, ref_name in _ALL_CORPUS_SPECS:
        input_path = _FIXTURES_DIR / input_name
        ref_path = (_FIXTURES_DIR / ref_name) if ref_name else None
        output_path = out_dir / f"{label}.md"
        result = transform_file(input_path, output_path, ref_path)
        input_text = input_path.read_text(encoding="utf-8")
        output_text = output_path.read_text(encoding="utf-8") if output_path.exists() else ""
        entries.append(CorpusEntry(
            label=label,
            input_path=input_path,
            output_path=output_path,
            generic_ref_path=ref_path,
            result=result,
            input_text=input_text,
            output_text=output_text,
        ))
    return entries


@pytest.fixture(scope="module")
def successful_corpus(transformed_corpus: list[CorpusEntry]) -> list[CorpusEntry]:
    """The subset of the corpus that transformed successfully and wrote output."""
    return [e for e in transformed_corpus if e.result.success and not e.result.skipped]


def _entry(corpus: list[CorpusEntry], label: str) -> CorpusEntry:
    for e in corpus:
        if e.label == label:
            return e
    raise AssertionError(f"No corpus entry labelled {label!r}")


# ---------------------------------------------------------------------------
# Corpus sanity (T10.1 / AC10.7 -- both transform paths are covered)
# ---------------------------------------------------------------------------

class TestCorpusAssembly:
    """The corpus covers both transform paths and every member transforms."""

    def test_every_corpus_member_transforms_successfully(
        self, transformed_corpus: list[CorpusEntry]
    ):
        failures = [
            (e.label, [err.message for err in e.result.errors])
            for e in transformed_corpus
            if not e.result.success
        ]
        assert failures == [], f"Corpus members failed to transform: {failures!r}"

    def test_corpus_covers_the_generic_path(self, successful_corpus: list[CorpusEntry]):
        generic_labels = {label for label, _, _ in _GENERIC_CORPUS_SPECS}
        present = {e.label for e in successful_corpus} & generic_labels
        assert present, "Corpus must include at least one generic-path member"

    def test_corpus_covers_the_harness_path(self, successful_corpus: list[CorpusEntry]):
        harness_labels = {label for label, _, _ in _HARNESS_CORPUS_SPECS}
        present = {e.label for e in successful_corpus} & harness_labels
        assert present, "Corpus must include at least one harness-path member"


# ---------------------------------------------------------------------------
# Uniformity (T10.2 / AC10.2)
# ---------------------------------------------------------------------------

class TestUniformity:
    """Every emitted deployed region is byte-identical across every file."""

    def test_every_emitted_conduct_region_is_empty(self, successful_corpus: list[CorpusEntry]):
        """AD-8: emitted deployed regions carry no body -- open tag, close tag, nothing between.

        Checked for every conduct-region name that actually appears in each
        corpus file's output, not merely for one file.
        """
        conduct_names = [spec.name for spec in CONDUCT_REGIONS]
        checked = 0
        for entry in successful_corpus:
            for name in conduct_names:
                body = extract_region(entry.output_text, BoundaryKind.DEPLOYED, name)
                if body is None:
                    continue
                checked += 1
                assert body == "", (
                    f"{entry.label}: [[DEPLOYED:{name}]] must be empty; got {body!r}"
                )
        assert checked > 0, "No conduct region was found in any corpus output; corpus is too thin"

    def test_conduct_region_tag_pairs_are_byte_identical_across_files(
        self, successful_corpus: list[CorpusEntry]
    ):
        """The raw two-line tag pair for a given region name must never vary between files."""
        conduct_names = [spec.name for spec in CONDUCT_REGIONS]
        raw_by_name: dict[str, set[str]] = {name: set() for name in conduct_names}

        for entry in successful_corpus:
            lines = entry.output_text.splitlines(keepends=False)
            for i, line in enumerate(lines):
                for name in conduct_names:
                    if line == f"[[DEPLOYED:{name}]]" and i + 1 < len(lines):
                        raw_by_name[name].add(f"{line}\n{lines[i + 1]}")

        variant_names = {
            name: variants for name, variants in raw_by_name.items() if len(variants) > 1
        }
        assert variant_names == {}, (
            f"Region emission is not byte-identical across the corpus: {variant_names!r}"
        )
        assert any(raw_by_name.values()), "No conduct region tag pair was found in the corpus"

    def test_every_emitted_conduct_region_has_a_canonical_bundle_block(
        self, successful_corpus: list[CorpusEntry]
    ):
        """Every harness-agnostic conduct-region name this run emits has a canonical
        block behind it in the generic bundle.

        Uses deployed_blocks.load_bundle().targets(), the prerequisite this design
        introduces for a future byte-identity rule against the bundle (Open Question 1).

        HarnessConstraints is deliberately excluded: per the Deletion Rule Catalogue
        it "supersedes no prose" and, unlike the other five conduct regions, its body
        is per-harness content sourced outside the single generic bundle -- it is not
        expected to have a DeployedSections.md block, and its absence from
        Bundle.targets() is not a defect.
        """
        bundle = load_bundle(DEFAULT_BUNDLE_PATH)
        targets = bundle.targets()

        emitted: set[str] = set()
        for entry in successful_corpus:
            emitted.update(entry.result.deployed_added)

        conduct_names = {spec.name for spec in CONDUCT_REGIONS} - {"HarnessConstraints"}
        touched = emitted & conduct_names
        assert touched, "Corpus produced no conduct-region emissions to check against the bundle"
        missing = {name for name in touched if name not in targets}
        assert missing == set(), (
            f"Conduct regions emitted with no canonical bundle block: {missing!r}"
        )


# ---------------------------------------------------------------------------
# Isolation (T10.2 / AC10.2)
# ---------------------------------------------------------------------------

def _strip_legacy_and_tag_lines(lines: list[str]) -> list[str]:
    kept: list[str] = []
    for line in strip_tag_lines(lines):
        if _LEGACY_MARKER_RE.match(line.rstrip("\r\n").strip()):
            continue
        kept.append(line)
    return kept


def _body_lines(text: str) -> list[str]:
    """Return the body lines of a source or output file, frontmatter excluded."""
    lines = text.splitlines(keepends=True)
    if not lines or lines[0].rstrip("\r\n") != "---":
        return lines
    for i in range(1, len(lines)):
        if lines[i].rstrip("\r\n") == "---":
            return lines[i + 1:]
    return lines


class TestIsolation:
    """Content outside the regions a transform was meant to touch is unchanged.

    Capabilities and OutputFormat carry no rows in CONDUCT_REGIONS (see the
    Region Placement Contract) -- they are the sections this run's conduct-
    region insertions and deletions never touch. Their prose, with boundary
    tag lines (new syntax) and legacy marker lines (pre-existing syntax) both
    stripped from each side, must be identical before and after transform.
    """

    @pytest.mark.parametrize("section_name", ["Capabilities", "OutputFormat"])
    def test_untouched_section_prose_is_unchanged(
        self, successful_corpus: list[CorpusEntry], section_name: str
    ):
        checked = 0
        for entry in successful_corpus:
            before_lines = _body_lines(entry.input_text)
            after_lines = _body_lines(entry.output_text)

            before_spans = find_section_spans(before_lines)
            after_spans = find_section_spans(after_lines)

            before_span = before_spans.get(section_name)
            after_span = after_spans.get(section_name)
            if before_span is None or after_span is None:
                continue
            checked += 1

            before_prose = _strip_legacy_and_tag_lines(
                before_lines[before_span.heading_line:before_span.content_end]
            )
            after_prose = _strip_legacy_and_tag_lines(
                after_lines[after_span.heading_line:after_span.content_end]
            )
            assert after_prose == before_prose, (
                f"{entry.label}: {section_name} prose changed outside of tag wrapping.\n"
                f"before={before_prose!r}\nafter={after_prose!r}"
            )
        assert checked > 0, f"No corpus file carried a {section_name} section to check"


# ---------------------------------------------------------------------------
# Deletion completeness (T10.3 / AC10.2)
# ---------------------------------------------------------------------------

# Fragments this run's deletion rules are meant to remove, drawn verbatim from
# the corpus fixtures that carry them (see the Deletion Rule Catalogue in
# ContractsDesign.md), and confirmed by direct inspection of transform_file's
# current output to be the wordings that actually match the STRICT pattern
# (not merely the permissive drift_probe) for their rule.
#
# Several corpus fixtures carry other, non-canonical wordings of the same
# EH-retry / EP-context / EP-quality bullets (e.g. "Retry transient errors
# once before escalating", "Dedicate your full context window to this
# task.", "Stop at a good stopping point."). Those trip only the permissive
# drift_probe, not the strict pattern, and are LEFT IN PLACE per the outcome
# contract -- deliberately excluded here, alongside the third-wording
# PC-bullet-5 variant covered by s8_generic_drift_probe_only_bullet_input.md.
# Asserting their absence would assert the wrong contract.
_SUPERSEDED_FRAGMENTS: tuple[tuple[str, str], ...] = (
    ("AH-block", "### Authority Hierarchy"),
    ("CP-hitl-step", "present all output artifacts to the user for review"),
    ("CP-json-step", "Return ONLY output json defined by communication protocol"),
    ("PC-bullet-1", "NEVER access an orchestration artifact that is not named in your"),
    ("PC-bullet-2", "You MAY read, modify, or create any project file"),
    ("PC-bullet-3", "NEVER skip the JSON response block"),
    ("PC-bullet-4", "NEVER invent status codes"),
    ("PC-bullet-5-canonical", "Note work that belongs to another agent; do not do it yourself"),
    ("EH-retry", "Retry a transient error once"),
    ("EH-errcodes", "E101: input not found, E401: dependency missing"),
    ("EP-context", "You can dedicate your full context window to this task. Follow-up work"),
    ("EP-memory", "Input and output artifacts are the persistent memory between invocations."),
    ("EP-quality", "Finishing part of the task well beats finishing all of it badly"),
)


class TestDeletionCompleteness:
    """No superseded fragment survives anywhere in the output tree.

    Swept across the whole transformed corpus, not merely the single file that
    originally carried each fragment -- the tree-wide check the Plan calls for.
    """

    def test_fragment_present_pre_transform_somewhere_in_corpus(self):
        """Sanity guard: every fragment in the catalogue actually appears in some
        corpus input, otherwise its absence downstream would be a vacuous pass."""
        combined_input = "\n".join(
            (_FIXTURES_DIR / name).read_text(encoding="utf-8")
            for _, name, _ in _ALL_CORPUS_SPECS
        )
        never_present = [
            (rule_id, fragment)
            for rule_id, fragment in _SUPERSEDED_FRAGMENTS
            if not contains_fragment(combined_input, fragment)
        ]
        assert never_present == [], (
            f"Fragments not present in any corpus input (dead assertions "
            f"downstream): {never_present!r}"
        )

    def test_no_superseded_fragment_survives_anywhere_in_the_output_tree(
        self, successful_corpus: list[CorpusEntry]
    ):
        combined_output = "\n".join(entry.output_text for entry in successful_corpus)
        survivors = [
            (rule_id, fragment)
            for rule_id, fragment in _SUPERSEDED_FRAGMENTS
            if contains_fragment(combined_output, fragment)
        ]
        assert survivors == [], (
            f"Superseded fragments survive somewhere in the transformed corpus: {survivors!r}"
        )


# ---------------------------------------------------------------------------
# Ordering constraints that pass the validator when wrong (T10.4 / AC10.4)
# ---------------------------------------------------------------------------

class TestOrderingConstraints:
    """Placement constraints the validator's own rules do not enforce.

    The validator checks canonical *section* order and tag pairing; none of
    its rules would catch AuthorityHierarchy landing before ClosingProcedure,
    ExecutionPhilosophyCommon landing after ContextLimits, or the Constraints
    conduct regions landing out of order. These are asserted explicitly here.
    """

    def test_closing_procedure_immediately_precedes_authority_hierarchy(
        self, successful_corpus: list[CorpusEntry]
    ):
        checked = 0
        for entry in successful_corpus:
            lines = entry.output_text.splitlines(keepends=False)
            if "[[DEPLOYED:ClosingProcedure]]" not in lines:
                continue
            if "[[DEPLOYED:AuthorityHierarchy]]" not in lines:
                continue
            checked += 1
            cp_close_idx = lines.index("[[/DEPLOYED:ClosingProcedure]]")
            ah_open_idx = lines.index("[[DEPLOYED:AuthorityHierarchy]]")
            assert ah_open_idx == cp_close_idx + 1, (
                f"{entry.label}: AuthorityHierarchy must open on the line immediately "
                f"after ClosingProcedure closes; found {ah_open_idx - cp_close_idx - 1} "
                f"line(s) between them"
            )
        assert checked > 0, "No corpus file carried both ClosingProcedure and AuthorityHierarchy"

    def test_closing_procedure_has_nothing_between_it_and_the_process_list(
        self, successful_corpus: list[CorpusEntry]
    ):
        checked = 0
        for entry in successful_corpus:
            if entry.label not in _HAS_PROCESS_LIST_LABELS:
                continue
            lines = entry.output_text.splitlines(keepends=False)
            if "[[DEPLOYED:ClosingProcedure]]" not in lines:
                continue
            checked += 1
            cp_open_idx = lines.index("[[DEPLOYED:ClosingProcedure]]")
            preceding = lines[cp_open_idx - 1].strip()
            assert re.match(r"^\d+\.", preceding), (
                f"{entry.label}: expected a numbered Process step immediately before "
                f"[[DEPLOYED:ClosingProcedure]]; found {preceding!r}"
            )
        assert checked > 0, "No corpus file with a real Process list was checked"

    def test_execution_philosophy_common_precedes_context_limits(
        self, successful_corpus: list[CorpusEntry]
    ):
        checked = 0
        for entry in successful_corpus:
            lines = entry.output_text.splitlines(keepends=False)
            if "[[DEPLOYED:ExecutionPhilosophyCommon]]" not in lines:
                continue
            if "[[INJECTION:ContextLimits]]" not in lines:
                continue
            checked += 1
            ep_idx = lines.index("[[DEPLOYED:ExecutionPhilosophyCommon]]")
            cl_idx = lines.index("[[INJECTION:ContextLimits]]")
            assert ep_idx < cl_idx, (
                f"{entry.label}: ExecutionPhilosophyCommon (line {ep_idx}) must precede "
                f"ContextLimits (line {cl_idx})"
            )
        assert checked > 0, (
            "No corpus file carried both ExecutionPhilosophyCommon and ContextLimits"
        )

    def test_constraints_regions_are_ordered_pc_then_hc_then_cc(
        self, successful_corpus: list[CorpusEntry]
    ):
        checked_any = False
        for entry in successful_corpus:
            lines = entry.output_text.splitlines(keepends=False)
            pc_idx = lines.index("[[DEPLOYED:ProtocolConstraints]]") \
                if "[[DEPLOYED:ProtocolConstraints]]" in lines else None
            hc_idx = lines.index("[[DEPLOYED:HarnessConstraints]]") \
                if "[[DEPLOYED:HarnessConstraints]]" in lines else None
            cc_idx = lines.index("[[INJECTION:CustomConstraints]]") \
                if "[[INJECTION:CustomConstraints]]" in lines else None

            if pc_idx is not None and hc_idx is not None:
                checked_any = True
                assert pc_idx < hc_idx, (
                    f"{entry.label}: ProtocolConstraints must precede HarnessConstraints"
                )
            if hc_idx is not None and cc_idx is not None:
                checked_any = True
                assert hc_idx < cc_idx, (
                    f"{entry.label}: HarnessConstraints must precede CustomConstraints"
                )
        assert checked_any, "No corpus file exercised the Constraints region ordering"


# ---------------------------------------------------------------------------
# Batch-level: skipping, version fallback, validator-clean (T10.5 / AC10.3, AC10.5)
# ---------------------------------------------------------------------------

class TestSkipping:
    """Utility and non-agent files produce no output and a visible skip message."""

    @pytest.mark.parametrize("base_name", sorted(fc.UTILITY_AGENT_FILENAMES))
    def test_utility_agent_filenames_are_skipped(self, tmp_path, base_name: str):
        input_path = tmp_path / f"{base_name}.md"
        input_path.write_text("---\nversion: 1.0.0\n---\n\nIrrelevant content.\n", encoding="utf-8")
        output_path = tmp_path / f"{base_name}.out.md"

        result = transform_file(input_path, output_path)

        assert result.skipped is True
        assert result.success is True
        assert not output_path.exists(), "A skipped file must produce no output"
        assert any("skipped" in w.lower() for w in result.warnings), (
            "A skip must leave a visible message in warnings"
        )

    @pytest.mark.parametrize("base_name", sorted(fc.NON_AGENT_FILENAMES))
    def test_non_agent_filenames_are_skipped(self, tmp_path, base_name: str):
        input_path = tmp_path / f"{base_name}.md"
        input_path.write_text("---\nversion: 1.0.0\n---\n\nIrrelevant content.\n", encoding="utf-8")
        output_path = tmp_path / f"{base_name}.out.md"

        result = transform_file(input_path, output_path)

        assert result.skipped is True
        assert result.success is True
        assert not output_path.exists(), "A skipped file must produce no output"
        assert any("skipped" in w.lower() for w in result.warnings), (
            "A skip must leave a visible message in warnings"
        )


class TestVersionFallback:
    """A file with neither version nor transform_version transforms successfully."""

    def test_generic_path_with_no_version_fields_succeeds(
        self, transformed_corpus: list[CorpusEntry]
    ):
        entry = _entry(transformed_corpus, "generic_no_version_no_transform")
        assert entry.result.success is True
        assert entry.result.errors == []
        assert re.match(r"^\d+\.\d+\.\d+$", entry.result.version_after), (
            f"version_after must be a valid semantic version; got {entry.result.version_after!r}"
        )

    def test_harness_degraded_path_with_no_version_field_succeeds(
        self, transformed_corpus: list[CorpusEntry]
    ):
        entry = _entry(transformed_corpus, "harness_only_no_version_degraded")
        assert entry.result.success is True
        assert entry.result.errors == []
        assert re.match(r"^\d+\.\d+\.\d+$", entry.result.version_after), (
            f"version_after must be a valid semantic version; got {entry.result.version_after!r}"
        )


class TestValidatorCleanAcrossTheTree:
    """Every transformed corpus file, and the canonical bundle, validate clean."""

    def test_every_transformed_corpus_file_validates_clean(
        self, successful_corpus: list[CorpusEntry]
    ):
        checked = 0
        failing: dict[str, list[str]] = {}
        for entry in successful_corpus:
            if entry.label in _VALIDATOR_CLEAN_EXCLUDED_LABELS:
                continue
            checked += 1
            errors = [
                str(err) for err in validate_file(entry.output_path) if err.severity == "error"
            ]
            if errors:
                failing[entry.label] = errors
        assert checked > 0, "No corpus file was eligible for the validator-clean sweep"
        assert failing == {}, f"Validator errors in transformed corpus output: {failing!r}"

    def test_canonical_bundle_validates_clean(self):
        errors = [
            str(err) for err in validate_file(DEFAULT_BUNDLE_PATH) if err.severity == "error"
        ]
        assert errors == [], f"Canonical bundle DeployedSections.md has validator errors: {errors!r}"


# ---------------------------------------------------------------------------
# Report coverage (T10.6 / AC10.6)
# ---------------------------------------------------------------------------

class TestReportCoverage:
    """The non-conformance report names each detectable class present in the corpus."""

    def test_report_names_json_envelope_zero_injections_and_harness_prose(
        self, successful_corpus: list[CorpusEntry]
    ):
        all_findings = [
            nc for entry in successful_corpus for nc in entry.result.non_conformances
        ]
        codes_present = {nc.code for nc in all_findings}

        expected_codes = {NC_JSON_ENVELOPE, NC_NO_INJECTIONS, NC_HARNESS_PROSE}
        missing_from_corpus = expected_codes - codes_present
        assert missing_from_corpus == set(), (
            f"Corpus did not actually produce these detectable classes; the "
            f"report-coverage assertion below would be vacuous for them: "
            f"{missing_from_corpus!r}"
        )

        report = render_report(all_findings)
        assert f"[{NC_JSON_ENVELOPE}]" in report, "Report must name a retained JSON response envelope"
        assert f"[{NC_NO_INJECTIONS}]" in report, "Report must name a zero-injection file"
        assert f"[{NC_HARNESS_PROSE}]" in report, (
            "Report must name harness-specific prose in an agent-authored section"
        )
