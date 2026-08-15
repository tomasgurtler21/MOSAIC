"""Stage 10 -- End-to-End Acceptance Verification.

Corpus-level acceptance tests that verify the run's overall success conditions
across a multi-file transform: properties no single earlier stage's per-file
unit tests can establish on their own.

Also contains Stage 5 acceptance and cross-copy consistency tests:
  - All four Stage 5 behaviours asserted together on a single real legacy
    agent file (TestStage5AllFourBehaviours).
  - Python-side pin of the cross-copy vocabulary invariant that must agree
    with the Go pin in vocabulary_boundary_sync_test.go
    (TestCrossCopyVocabularyInvariant).

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

import boundary_constants as _bc  # noqa: E402  (used by TestCrossCopyVocabularyInvariant)
from boundary_constants import BoundaryKind  # noqa: E402
from boundary_transformer import TransformResult, transform_file  # noqa: E402
from boundary_validator import validate_file  # noqa: E402
from deployed_blocks import DEFAULT_BUNDLE_PATH, load_bundle  # noqa: E402
from region_insertion import CONDUCT_REGIONS, find_section_spans  # noqa: E402
from non_conformance import (  # noqa: E402
    NC_HARNESS_PROSE,
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
                    f'{entry.label}: <{name} type="managed"> region must be empty; got {body!r}'
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
                    if line == f'<{name} type="managed">' and i + 1 < len(lines):
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

    Capabilities carries no rows in CONDUCT_REGIONS (see the Region Placement
    Contract) -- it is a section this run's conduct-region insertions and
    deletions never touch. Its prose, with boundary tag lines (new syntax) and
    legacy marker lines (pre-existing syntax) both stripped from each side,
    must be identical before and after transform.

    OutputFormat is excluded from this check: it is no longer a canonical section.
    Its legacy heading and body are deleted outright by delete_legacy_sections, so
    by definition the section cannot be "unchanged" -- it is gone.
    """

    @pytest.mark.parametrize("section_name", ["Capabilities"])
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
            if '<ClosingProcedure type="managed">' not in lines:
                continue
            if '<AuthorityHierarchy type="managed">' not in lines:
                continue
            checked += 1
            cp_close_idx = lines.index("</ClosingProcedure>")
            ah_open_idx = lines.index('<AuthorityHierarchy type="managed">')
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
            if '<ClosingProcedure type="managed">' not in lines:
                continue
            checked += 1
            cp_open_idx = lines.index('<ClosingProcedure type="managed">')
            preceding = lines[cp_open_idx - 1].strip()
            assert re.match(r"^\d+\.", preceding), (
                f"{entry.label}: expected a numbered Process step immediately before "
                f'<ClosingProcedure type="managed">; found {preceding!r}'
            )
        assert checked > 0, "No corpus file with a real Process list was checked"

    def test_execution_philosophy_common_precedes_context_limits(
        self, successful_corpus: list[CorpusEntry]
    ):
        checked = 0
        for entry in successful_corpus:
            lines = entry.output_text.splitlines(keepends=False)
            if '<ExecutionPhilosophyCommon type="managed">' not in lines:
                continue
            if '<ContextLimits type="project">' not in lines:
                continue
            checked += 1
            ep_idx = lines.index('<ExecutionPhilosophyCommon type="managed">')
            cl_idx = lines.index('<ContextLimits type="project">')
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
            pc_idx = lines.index('<ProtocolConstraints type="managed">') \
                if '<ProtocolConstraints type="managed">' in lines else None
            hc_idx = lines.index('<HarnessConstraints type="managed">') \
                if '<HarnessConstraints type="managed">' in lines else None
            cc_idx = lines.index('<CustomConstraints type="custom">') \
                if '<CustomConstraints type="custom">' in lines else None

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

    def test_report_names_zero_injections_and_harness_prose(
        self, successful_corpus: list[CorpusEntry]
    ):
        all_findings = [
            nc for entry in successful_corpus for nc in entry.result.non_conformances
        ]
        codes_present = {nc.code for nc in all_findings}

        expected_codes = {NC_NO_INJECTIONS, NC_HARNESS_PROSE}
        missing_from_corpus = expected_codes - codes_present
        assert missing_from_corpus == set(), (
            f"Corpus did not actually produce these detectable classes; the "
            f"report-coverage assertion below would be vacuous for them: "
            f"{missing_from_corpus!r}"
        )

        report = render_report(all_findings)
        assert f"[{NC_NO_INJECTIONS}]" in report, "Report must name a zero-injection file"
        assert f"[{NC_HARNESS_PROSE}]" in report, (
            "Report must name harness-specific prose in an agent-authored section"
        )


# ---------------------------------------------------------------------------
# Stage 5: End-to-end acceptance — all four behaviours on a real legacy file
# (T5.1 / AC5.3, AC5.4)
# ---------------------------------------------------------------------------

_S5_INPUT_FIXTURE = "s5_harness_contracts_input.md"
_S5_GENERIC_REF_FIXTURE = "s5_harness_contracts_generic_ref.md"


@dataclasses.dataclass
class _Stage5Result:
    """Holds the transform outcome for the Stage 5 end-to-end acceptance test."""
    input_path: pathlib.Path
    output_path: pathlib.Path
    result: TransformResult
    output_text: str


@pytest.fixture(scope="module")
def stage5_acceptance(tmp_path_factory) -> _Stage5Result:
    """Transform the Stage 5 harness legacy-agent fixture once per module.

    Input: s5_harness_contracts_input.md — harness legacy agent with all four defects:
      1. '## Output Format' H2 section (retired, must be deleted).
      2. '### Design Artifact Structure' block in Capabilities outside any region
         (must move inside OutputArtifactTemplate).
      3. Orphaned legacy prose for all five conduct regions (must be deleted by
         their respective deletion rules).
      4. No existing boundary tags (raw legacy format — the transform must produce
         a structurally valid output).

    Generic reference: s5_harness_contracts_generic_ref.md — current-format ref
      with no '## Output Format' section, so the output is clean on both sides.
    """
    out_dir = tmp_path_factory.mktemp("stage5_acceptance")
    input_path = _FIXTURES_DIR / _S5_INPUT_FIXTURE
    ref_path = _FIXTURES_DIR / _S5_GENERIC_REF_FIXTURE
    output_path = out_dir / "stage5_output.md"
    result = transform_file(input_path, output_path, ref_path)
    output_text = output_path.read_text(encoding="utf-8") if output_path.exists() else ""
    return _Stage5Result(
        input_path=input_path,
        output_path=output_path,
        result=result,
        output_text=output_text,
    )


class TestStage5AllFourBehaviours:
    """End-to-end acceptance: all four defect fixes coexist on one real legacy agent.

    The four behaviours are asserted independently because structural validation
    alone does not catch them — the original defect validated cleanly while
    duplicating its own content. Both content assertions (AC5.3) and structural
    validation (AC5.4) are required.

    Fixture: s5_harness_contracts_input.md — a real-structure harness legacy agent
    modelled on firstGeneration contracts-designer.md, containing all four defects.
    """

    # ------------------------------------------------------------------
    # Precondition
    # ------------------------------------------------------------------

    def test_fixture_input_contains_all_four_defects(self) -> None:
        """Sanity guard: the fixture input contains all four defects before any transform.

        Without this guard, absence assertions in the behaviour tests below would be
        vacuous passes if the fixture were accidentally cleaned of its defects. Each
        sentinel phrase is asserted to be present in the raw fixture text so that a
        downstream absence assertion is meaningful.

        Uses the same pattern as
        TestDeletionCompleteness.test_fragment_present_pre_transform_somewhere_in_corpus.
        """
        fixture_text = (_FIXTURES_DIR / _S5_INPUT_FIXTURE).read_text(encoding="utf-8")
        sentinel_phrases: list[tuple[str, str]] = [
            ("OutputFormat-heading",       "## Output Format"),
            ("OutputFormat-body",          "Always end with a JSON status block"),
            ("CP-hitl-step",               "present all output artifacts to the user for review"),
            ("CP-json-step",               "Return ONLY output json defined by communication protocol"),
            ("AH-heading",                 "### Authority Hierarchy"),
            ("PC-bullets",                 "NEVER access orchestration artifacts not in"),
            ("EH-retry-variant",           "Retry transient errors once"),
            ("EP-context-variant",         "Follow-up tasks are handled by spawning new agent instances"),
            ("Design-Artifact-Structure",  "Design Artifact Structure"),
        ]
        missing = [
            (name, phrase)
            for name, phrase in sentinel_phrases
            if not contains_fragment(fixture_text, phrase)
        ]
        assert missing == [], (
            f"Fixture {_S5_INPUT_FIXTURE!r} is missing expected defect content — "
            f"a sanitised fixture renders downstream absence assertions vacuous: {missing!r}"
        )

    def test_transform_succeeds(self, stage5_acceptance: _Stage5Result) -> None:
        """The transform must succeed before any behaviour assertion can proceed."""
        assert stage5_acceptance.result.success is True, (
            f"Transform failed — behaviour assertions cannot proceed. "
            f"Errors: {[e.message for e in stage5_acceptance.result.errors]}"
        )
        assert stage5_acceptance.output_text, "No output was produced by the transform"
        assert set(stage5_acceptance.result.deployed_added) >= {
            "ClosingProcedure",
            "AuthorityHierarchy",
            "ProtocolConstraints",
            "ErrorHandlingCommon",
            "ExecutionPhilosophyCommon",
        }, (
            f"result.deployed_added must record all five conduct regions as emitted; "
            f"got: {set(stage5_acceptance.result.deployed_added)!r}"
        )

    # ------------------------------------------------------------------
    # Behaviour 1: No OutputFormat section anywhere in the output
    # ------------------------------------------------------------------

    def test_output_format_h2_heading_absent_from_output(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """No '## Output Format' H2 heading must appear anywhere in the output.

        The retired '## Output Format' section must be deleted from the harness
        body by delete_legacy_sections before the body reaches any merge path.
        The generic reference used here carries no '## Output Format' section
        (current-format reference), so no such heading can enter from that side
        either. The result must be clean on both counts.
        """
        lines = stage5_acceptance.output_text.splitlines()
        offending = [
            (i + 1, line)
            for i, line in enumerate(lines)
            if line.strip() == "## Output Format"
        ]
        assert offending == [], (
            f"'## Output Format' H2 heading must not appear in the output; "
            f"found at line(s): {[i for i, _ in offending]}"
        )

    def test_output_format_section_body_absent_from_output(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The body of the '## Output Format' section must be deleted with its heading.

        The fixture's OutputFormat body contains 'Always end with a JSON status block'.
        This sentinel phrase must not survive in the transformed output.
        """
        assert not contains_fragment(
            stage5_acceptance.output_text,
            "Always end with a JSON status block",
        ), (
            "OutputFormat body sentinel ('Always end with a JSON status block') must not "
            "survive — the entire '## Output Format' section is deleted by "
            "delete_legacy_sections, heading and body together"
        )

    # ------------------------------------------------------------------
    # Behaviour 2: Artifact-structure block inside OutputArtifactTemplate
    # ------------------------------------------------------------------

    def test_output_artifact_template_region_is_present(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The OutputArtifactTemplate injection region must be present in the output."""
        body = extract_region(
            stage5_acceptance.output_text,
            BoundaryKind.INJECTION,
            "OutputArtifactTemplate",
        )
        assert body is not None, (
            "OutputArtifactTemplate injection region must be present in the output; "
            "the transform must have emitted its open and close tags"
        )

    def test_artifact_structure_block_is_inside_output_artifact_template(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The 'Design Artifact Structure' block must be inside the OutputArtifactTemplate region.

        move_artifact_structure_block must have moved the block from its original position
        in the Capabilities section (outside any region) to become project-default content
        between the OutputArtifactTemplate open and close tags.
        """
        region_body = extract_region(
            stage5_acceptance.output_text,
            BoundaryKind.INJECTION,
            "OutputArtifactTemplate",
        )
        assert region_body is not None, "OutputArtifactTemplate region must be present"
        assert region_body.strip(), (
            "OutputArtifactTemplate region must not be empty — "
            "the artifact-structure block must have been moved inside it"
        )
        assert contains_fragment(region_body, "Design Artifact Structure"), (
            "The 'Design Artifact Structure' heading must appear inside the "
            "OutputArtifactTemplate region, not as stranded prose outside it"
        )

    def test_artifact_structure_heading_appears_exactly_once_in_output(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The artifact-structure heading must appear exactly once — moved, not duplicated.

        The block must be removed from its original Capabilities location and inserted
        once inside OutputArtifactTemplate. Duplicated content (the original defect
        pattern) would show two occurrences.
        """
        count = stage5_acceptance.output_text.count("Design Artifact Structure")
        assert count == 1, (
            f"'Design Artifact Structure' must appear exactly once in the output "
            f"(moved, not duplicated); found {count} occurrence(s)"
        )

    # ------------------------------------------------------------------
    # Behaviour 3: No orphaned legacy prose beside any of the five conduct regions
    # ------------------------------------------------------------------

    def test_hitl_step_prose_does_not_survive(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The HITL step prose must not survive — it is superseded by ClosingProcedure.

        The fixture's CP-hitl-step variant ('present all output artifacts to the user
        for review/approval') must be deleted by the ClosingProcedure deletion rule.
        """
        assert not contains_fragment(
            stage5_acceptance.output_text,
            "present all output artifacts to the user for review",
        ), (
            "HITL step prose must not survive in the output — it is superseded "
            "by the ClosingProcedure managed region"
        )

    def test_json_step_prose_does_not_survive(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The JSON step prose must not survive — it is superseded by ClosingProcedure.

        The fixture's CP-json-step prose ('Return ONLY output json defined by
        communication protocol') must be deleted.
        """
        assert not contains_fragment(
            stage5_acceptance.output_text,
            "Return ONLY output json defined by communication protocol",
        ), (
            "JSON step prose must not survive in the output — it is superseded "
            "by the ClosingProcedure managed region"
        )

    def test_closing_procedure_region_is_empty(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The ClosingProcedure managed region must be empty (body is managed content)."""
        body = extract_region(
            stage5_acceptance.output_text,
            BoundaryKind.DEPLOYED,
            "ClosingProcedure",
        )
        assert body is not None, "ClosingProcedure region must be present in the output"
        assert body.strip() == "", (
            "ClosingProcedure managed region must be empty — "
            "its canonical content is provided by the deployed bundle, not inlined"
        )

    def test_authority_hierarchy_heading_does_not_survive_as_orphan(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The '### Authority Hierarchy' heading must not survive as orphaned prose.

        The AH-block deletion rule (with own_region='AuthorityHierarchy') must delete
        the heading and its prose. The fix for Defect A (find_heading_block_end with
        own_region) is what enables this deletion on the harness path.
        """
        lines = stage5_acceptance.output_text.splitlines()
        ah_heading_occurrences = [
            (i + 1, line)
            for i, line in enumerate(lines)
            if line.strip() == "### Authority Hierarchy"
        ]
        assert ah_heading_occurrences == [], (
            f"'### Authority Hierarchy' heading must not survive as orphaned prose; "
            f"found at line(s): {[i for i, _ in ah_heading_occurrences]}"
        )

    def test_authority_hierarchy_region_is_empty(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The AuthorityHierarchy managed region must be empty."""
        body = extract_region(
            stage5_acceptance.output_text,
            BoundaryKind.DEPLOYED,
            "AuthorityHierarchy",
        )
        assert body is not None, "AuthorityHierarchy region must be present"
        assert body.strip() == "", (
            "AuthorityHierarchy managed region must be empty — "
            "the inline heading and prose were deleted by the AH-block rule"
        )

    def test_protocol_constraints_bullets_do_not_survive(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """Legacy ProtocolConstraints bullets must not survive — deleted by the PC rules.

        The fixture contains the PC-bullet-1 variant ('NEVER access orchestration
        artifacts not in your `input_artifacts`/`output_artifacts` lists'), the
        PC-bullet-2 variant ('You MAY access any project file'), PC-bullet-3
        ('NEVER skip the JSON response block'), and PC-bullet-4 ('NEVER invent
        status codes'). All four must be absent from the output.
        """
        output = stage5_acceptance.output_text
        assert not contains_fragment(
            output,
            "NEVER access orchestration artifacts not in your",
        ), "PC-bullet-1 variant must not survive in the output"

        assert not contains_fragment(
            output,
            "You MAY access any project file",
        ), "PC-bullet-2 variant must not survive in the output"

        assert not contains_fragment(output, "NEVER skip the JSON response block"), (
            "PC-bullet-3 must not survive in the output"
        )
        assert not contains_fragment(output, "NEVER invent status codes"), (
            "PC-bullet-4 must not survive in the output"
        )

    def test_protocol_constraints_region_is_empty(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The ProtocolConstraints managed region must be empty."""
        body = extract_region(
            stage5_acceptance.output_text,
            BoundaryKind.DEPLOYED,
            "ProtocolConstraints",
        )
        assert body is not None, "ProtocolConstraints region must be present"
        assert body.strip() == "", (
            "ProtocolConstraints managed region must be empty — "
            "the legacy PC bullets were deleted by the PC deletion rules"
        )

    def test_eh_retry_prose_variant_does_not_survive(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The 'Retry transient errors once' EH-retry variant must not survive.

        The fixture uses the non-canonical wording ('**Retry transient errors once**
        before escalating') which Stage 5's widened EH-retry pattern now matches.
        After deletion, this phrase must not appear in the output.
        """
        assert not contains_fragment(
            stage5_acceptance.output_text,
            "Retry transient errors once",
        ), (
            "EH-retry variant ('Retry transient errors once') must not survive — "
            "Stage 5's widened EH-retry pattern deletes both the canonical and this "
            "alternate wording, and the ErrorHandlingCommon region supersedes it"
        )

    def test_error_handling_common_region_is_empty(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The ErrorHandlingCommon managed region must be empty."""
        body = extract_region(
            stage5_acceptance.output_text,
            BoundaryKind.DEPLOYED,
            "ErrorHandlingCommon",
        )
        assert body is not None, "ErrorHandlingCommon region must be present"
        assert body.strip() == "", (
            "ErrorHandlingCommon managed region must be empty — "
            "the legacy EH-retry prose was deleted by the EH-retry deletion rule"
        )

    def test_ep_context_prose_variant_does_not_survive(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The EP-context 'Follow-up tasks' variant must not survive.

        The fixture uses the alternate wording ('Follow-up tasks are handled by
        spawning new agent instances' — 'tasks' instead of 'work') which Stage 5's
        widened EP-context pattern now matches. After deletion, this phrase must not
        appear in the output.
        """
        assert not contains_fragment(
            stage5_acceptance.output_text,
            "Follow-up tasks are handled by spawning new agent instances",
        ), (
            "EP-context variant ('Follow-up tasks are handled by spawning new agent "
            "instances') must not survive — Stage 5's widened EP-context pattern "
            "deletes both the canonical ('Follow-up work') and this alternate wording"
        )

    def test_execution_philosophy_common_region_is_empty(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The ExecutionPhilosophyCommon managed region must be empty."""
        body = extract_region(
            stage5_acceptance.output_text,
            BoundaryKind.DEPLOYED,
            "ExecutionPhilosophyCommon",
        )
        assert body is not None, "ExecutionPhilosophyCommon region must be present"
        assert body.strip() == "", (
            "ExecutionPhilosophyCommon managed region must be empty — "
            "the legacy EP-context prose was deleted by the EP-context deletion rule"
        )

    # ------------------------------------------------------------------
    # Behaviour 4: Structural validation
    # ------------------------------------------------------------------

    def test_output_passes_structural_validation(
        self, stage5_acceptance: _Stage5Result
    ) -> None:
        """The transformed output must pass the boundary validator with no structural errors.

        This is separate from — and in addition to — the content assertions above.
        AC5.4 requires both to pass: the original defect validated cleanly while
        duplicating its own content, so structural validation alone is insufficient
        as an acceptance gate.
        """
        errors = [
            str(err)
            for err in validate_file(stage5_acceptance.output_path)
            if err.severity == "error"
        ]
        assert errors == [], (
            f"Transformed output must validate structurally clean. "
            f"Validator errors: {errors!r}"
        )


# ---------------------------------------------------------------------------
# Stage 5: Cross-copy vocabulary invariant (T5.2 / AC5.5)
# ---------------------------------------------------------------------------


class TestCrossCopyVocabularyInvariant:
    """The Python and Go canonical-vocabulary copies must agree on section list and order.

    Both copies are pinned independently as ordered-sequence literals — the suites live
    in different languages and cannot import each other, so a one-sided change fails
    only on whichever side was not updated.

    The Python-side pin is in this class. The Go-side pin is in:
      Tools/Common/docformat/vocabulary_boundary_sync_test.go:
        TestVocabulary_CanonicalSections_ExactFiveEntrySequence
        TestVocabulary_CanonicalOrder_ExactSixSlotSequence

    Both sides state the same sequences as literals. Disagreement between the two
    test files is a drift in one of the vocabulary copies and must be resolved by
    updating boundary_constants.py or vocabulary.go to restore agreement.
    """

    # Canonical sequences as declared by this Python-side pin.
    # The Go pin (vocabulary_boundary_sync_test.go) declares the same sequences.
    _EXPECTED_CANONICAL_SECTIONS: tuple[str, ...] = (
        "Identity",
        "Capabilities",
        "Constraints",
        "ErrorHandling",
        "ExecutionPhilosophy",
    )

    _EXPECTED_CANONICAL_ORDER: tuple[str, ...] = (
        "Identity",
        "CommunicationProtocol",
        "Capabilities",
        "Constraints",
        "ErrorHandling",
        "ExecutionPhilosophy",
    )

    def test_canonical_sections_matches_expected_five_entry_sequence(self) -> None:
        """CANONICAL_SECTIONS must equal the exact 5-entry sequence pinned here and in Go.

        OutputFormat is removed. The sequence is ordered (not a set): order is part of
        the contract, and a reordering would be a drift even if the set of names agreed.
        """
        assert _bc.CANONICAL_SECTIONS == self._EXPECTED_CANONICAL_SECTIONS, (
            f"CANONICAL_SECTIONS does not match the cross-copy invariant.\n"
            f"Expected (Python pin, must match Go pin): {self._EXPECTED_CANONICAL_SECTIONS}\n"
            f"Got: {_bc.CANONICAL_SECTIONS}\n"
            "Update boundary_constants.py OR vocabulary.go to restore cross-copy agreement."
        )

    def test_canonical_order_matches_expected_six_slot_sequence(self) -> None:
        """CANONICAL_ORDER must equal the exact 6-slot sequence pinned here and in Go.

        OutputFormat is removed. Index 1 is CommunicationProtocol (a top-level managed
        boundary, not a section). Every other slot is a section name.
        """
        assert _bc.CANONICAL_ORDER == self._EXPECTED_CANONICAL_ORDER, (
            f"CANONICAL_ORDER does not match the cross-copy invariant.\n"
            f"Expected (Python pin, must match Go pin): {self._EXPECTED_CANONICAL_ORDER}\n"
            f"Got: {_bc.CANONICAL_ORDER}\n"
            "Update boundary_constants.py OR vocabulary.go to restore cross-copy agreement."
        )

    def test_canonical_sections_has_exactly_five_entries(self) -> None:
        """CANONICAL_SECTIONS must have exactly 5 entries, matching Go CanonicalSections.

        The vocabulary correction drops OutputFormat: six sections become five in both
        the Python and Go copies. A count mismatch is a cross-copy drift.
        """
        assert len(_bc.CANONICAL_SECTIONS) == 5, (
            f"CANONICAL_SECTIONS must have exactly 5 entries to match Go CanonicalSections "
            f"(OutputFormat removed); got {len(_bc.CANONICAL_SECTIONS)}: {_bc.CANONICAL_SECTIONS}"
        )

    def test_canonical_order_has_exactly_six_slots(self) -> None:
        """CANONICAL_ORDER must have exactly 6 slots, matching Go CanonicalOrder.

        The vocabulary correction drops OutputFormat: seven ordered slots become six in
        both copies. A count mismatch is a cross-copy drift.
        """
        assert len(_bc.CANONICAL_ORDER) == 6, (
            f"CANONICAL_ORDER must have exactly 6 slots to match Go CanonicalOrder "
            f"(OutputFormat removed); got {len(_bc.CANONICAL_ORDER)}: {_bc.CANONICAL_ORDER}"
        )

    def test_output_format_absent_from_canonical_sections(self) -> None:
        """OutputFormat must not be in CANONICAL_SECTIONS, mirroring Go CanonicalSections."""
        assert "OutputFormat" not in _bc.CANONICAL_SECTIONS, (
            "OutputFormat must not be in CANONICAL_SECTIONS — it is retired; "
            "this must agree with the Go CanonicalSections (which also excludes it)"
        )

    def test_output_format_absent_from_canonical_order(self) -> None:
        """OutputFormat must not be in CANONICAL_ORDER, mirroring Go CanonicalOrder."""
        assert "OutputFormat" not in _bc.CANONICAL_ORDER, (
            "OutputFormat must not be in CANONICAL_ORDER — it is retired; "
            "this must agree with the Go CanonicalOrder (which also excludes it)"
        )

    def test_non_protocol_slots_of_canonical_order_equal_canonical_sections(self) -> None:
        """Filtering CommunicationProtocol from CANONICAL_ORDER must yield CANONICAL_SECTIONS.

        This structural invariant holds in both copies: filtering the one non-section
        slot (CommunicationProtocol) from the ordered document slots must reproduce
        the ordered section list exactly. A failure here means the two tables have
        drifted from each other within the Python copy.
        """
        non_protocol = tuple(
            n for n in _bc.CANONICAL_ORDER if n != "CommunicationProtocol"
        )
        assert non_protocol == _bc.CANONICAL_SECTIONS, (
            f"CANONICAL_ORDER minus CommunicationProtocol must equal CANONICAL_SECTIONS "
            f"(structural invariant, both copies).\n"
            f"Non-protocol slots: {non_protocol}\n"
            f"CANONICAL_SECTIONS: {_bc.CANONICAL_SECTIONS}"
        )

    def test_communication_protocol_is_second_slot_of_canonical_order(self) -> None:
        """CommunicationProtocol must occupy index 1 in CANONICAL_ORDER.

        This matches the Go CanonicalOrder and the design doc: CommunicationProtocol
        is a top-level managed boundary that occupies a slot between Identity and
        Capabilities but is not itself a section name.
        """
        assert len(_bc.CANONICAL_ORDER) > 1, "CANONICAL_ORDER must have at least 2 slots"
        assert _bc.CANONICAL_ORDER[1] == "CommunicationProtocol", (
            f"CommunicationProtocol must be at index 1 in CANONICAL_ORDER, "
            f"matching the Go CanonicalOrder; got {_bc.CANONICAL_ORDER[1]!r}"
        )
