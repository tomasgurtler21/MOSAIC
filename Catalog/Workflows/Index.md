# Workflow Index

Workflow definitions organized by category. This file is a lookup table pointing at the 14 workflow files in this catalog — it is not the source of truth for any workflow's metadata. Version, description, and hint always come from the workflow file's own frontmatter; open the file for that.

## How to Use Workflows

### Authoring Guides

- **[Execution Groups](ExecutionGroups.md)** — How to write a grouped workflow: Phase-column notation, the `**Execution Groups:**` table, the activation rule, contiguity constraint, refusal reference, and the `current_state.phase` convention.

### Finding a Workflow

Workflows are organized into seven categories reflecting their primary purpose:

- **Build** — Feature development and implementation (new projects, existing codebases, fixes)
- **Audit** — Quality assessment of existing code (PR reviews, system-level audits)
- **Research** — Codebase exploration and feasibility analysis without implementation
- **Design** — Architecture review and design proposals without implementation
- **Verification** — Deriving verification artifacts (test scenarios, test cases) from specifications; no implementation
- **DataPreprocessing** — Knowledge base generation, verification, and correction

Browse the [Workflow Summary](#workflow-summary) table to find a workflow by name and category, then open the file listed in the **File** column — that file's frontmatter and Design Rationale carry the actual description, hint, and version.

### Injecting a Workflow into an Orchestrator

Each workflow file contains a phase table that your orchestrator reads to know which agents to dispatch, in what order, and with what routing logic. To use a workflow:

1. Identify the workflow ID from this index.
2. Open the corresponding file under `Workflows/<Category>/<id>.md`.
3. Copy or reference the phase table into your orchestrator configuration.
4. Provide the required input artifacts listed in the workflow's frontmatter `artifacts` field.

Orchestrators that support auto-discovery can parse this index programmatically: the **ID** and **File** columns are the stable keys. For version, description, and hint, read the workflow file's own frontmatter — this index does not carry those fields.

### Category Taxonomy

| Category | Subfolder | Purpose |
|----------|-----------|---------|
| Build | `Workflows/Build/` | End-to-end feature and project development |
| Audit | `Workflows/Audit/` | Evidence-based quality analysis of existing code |
| Research | `Workflows/Research/` | Exploration and feasibility — no implementation |
| Design | `Workflows/Design/` | Architecture and design — no implementation |
| Verification | `Workflows/Verification/` | Deriving verification artifacts from specifications — no implementation |
| DataPreprocessing | `Workflows/DataPreprocessing/` | Knowledge base lifecycle (generate, verify, correct) |

Harness conformance fixtures for `mosaic-run` live under `Tools/Runner/TestCatalog/Workflows/MosaicTest/` — they are test fixtures for the Runner tool, not part of this catalog.

---

## Workflow Summary

### Build

| ID | Category | Name | Author | File |
|----|----------|------|--------|------|
| greenfield-tdd | Build | Greenfield TDD Workflow | MOSAIC | `Build/greenfield-tdd.md` |
| brownfield-tdd | Build | Brownfield TDD Workflow | MOSAIC | `Build/brownfield-tdd.md` |
| quick-fix | Build | Quick Fix Workflow | MOSAIC | `Build/quick-fix.md` |
| implementation-only | Build | Implementation Only Workflow | MOSAIC | `Build/implementation-only.md` |
| brownfield-tdd-build-verified | Build | Brownfield TDD Build-Verified Workflow | MOSAIC | `Build/brownfield-tdd-build-verified.md` |

### Audit

| ID | Category | Name | Author | File |
|----|----------|------|--------|------|
| brownfield-pr-audit | Audit | Brownfield PR Audit Workflow | MOSAIC | `Audit/brownfield-pr-audit.md` |
| brownfield-system-audit | Audit | Brownfield System Audit Workflow | MOSAIC | `Audit/brownfield-system-audit.md` |

### Research

| ID | Category | Name | Author | File |
|----|----------|------|--------|------|
| brownfield-research-only | Research | Brownfield Research Only Workflow | MOSAIC | `Research/brownfield-research-only.md` |

### Design

| ID | Category | Name | Author | File |
|----|----------|------|--------|------|
| brownfield-design | Design | Brownfield Design Workflow | MOSAIC | `Design/brownfield-design.md` |

### Verification

| ID | Category | Name | Author | File |
|----|----------|------|--------|------|
| requirements-to-test-cases | Verification | Requirements to Test Cases Workflow | MOSAIC | `Verification/requirements-to-test-cases.md` |

### DataPreprocessing

| ID | Category | Name | Author | File |
|----|----------|------|--------|------|
| kb-generation | DataPreprocessing | Knowledge Base Generation Workflow | MOSAIC | `DataPreprocessing/kb-generation.md` |
| kb-verification-human | DataPreprocessing | Knowledge Verification (Human) Workflow | MOSAIC | `DataPreprocessing/kb-verification-human.md` |
| kb-correction | DataPreprocessing | Knowledge Base Correction Workflow | MOSAIC | `DataPreprocessing/kb-correction.md` |
| hw-schema-kb-generation | DataPreprocessing | HW Schema Knowledge Base Generation Workflow | MOSAIC | `DataPreprocessing/hw-schema-kb-generation.md` |
