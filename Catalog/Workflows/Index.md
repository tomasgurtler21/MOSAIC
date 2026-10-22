# Workflow Index

Workflow definitions organized by category. This file is a lookup table pointing at the 16 workflow files in this catalog — it is not the source of truth for any workflow's metadata. The **Version** column mirrors each file's frontmatter `version` and is updated whenever the workflow's version is bumped (`WorkflowFormat.md` §11.2); where the two disagree, the file wins. Description and hint come only from the file's frontmatter.

## How to Use Workflows

### Authoring Guides

- **Workflow Format** (`Development/Designs/WorkflowFormat.md`, outside the catalog) — The authoritative file format: frontmatter, the `<Workflow>` region, routing-table columns and cell notation, blocks (nested included), parallel work (fork / join), groups, block schedules, validity rules, and the sections that follow the region. Executor-neutral: what a particular executor supports is documented with that executor.
- **[Workflow Patterns](WorkflowPatterns.md)** — Reusable workflow shapes and anti-patterns.

### Finding a Workflow

Workflows are organized into six categories reflecting their primary purpose:

- **Build** — Feature development and implementation (new projects, existing codebases, fixes)
- **Audit** — Quality assessment of existing code (PR reviews, system-level audits)
- **Research** — Codebase exploration and feasibility analysis without implementation
- **Design** — Architecture review and design proposals without implementation
- **Verification** — Deriving verification artifacts (test scenarios, test cases) from specifications; no implementation
- **DataPreprocessing** — Knowledge base generation, verification, and correction

Browse the [Workflow Summary](#workflow-summary) table to find a workflow by name and category, then open the file listed in the **File** column — that file's frontmatter, **Use when:** line and Design Rationale carry the actual description, hint, and reasoning.

### Using a Workflow

A workflow file holds a `<Workflow>` region — the routing table, plus any blocks and groups tables — which is the only part an orchestrator ever sees. To use a workflow:

1. **Choose it.** Find it in the summary table below, and open its file to check the description, hint and **Use when:** line.
2. **Deploy it.** List its ID under `workflows:` in the deployment's `selections.yaml`. mosaic-deploy copies the file's `<Workflow>` region into every orchestrator's `<AvailableWorkflows>` region and adds the `Row` column the run log refers to; nothing outside the region is deployed. Do not paste tables into an orchestrator by hand: a hand-copied table lacks the `Row` column and is not refreshed when the workflow changes.
3. **Start a run.** Name the workflow by its ID in the starting prompt and supply its seeds — the artifacts in the first row's **Input** cell, plus the schedule source of any block that row begins (`WorkflowFormat.md` §6.7). The orchestrator asks for any required seed that is missing. The frontmatter `artifacts` field lists every artifact the workflow touches, not what you must supply.

Tools can read this index programmatically: **ID** and **File** are the stable keys. For description and hint, read the workflow file's own frontmatter.

### Category Taxonomy

| Category | Subfolder | Purpose |
|----------|-----------|---------|
| Build | `Catalog/Workflows/Build/` | End-to-end feature and project development |
| Audit | `Catalog/Workflows/Audit/` | Evidence-based quality analysis of existing code |
| Research | `Catalog/Workflows/Research/` | Exploration and feasibility — no implementation |
| Design | `Catalog/Workflows/Design/` | Architecture and design — no implementation |
| Verification | `Catalog/Workflows/Verification/` | Deriving verification artifacts from specifications — no implementation |
| DataPreprocessing | `Catalog/Workflows/DataPreprocessing/` | Knowledge base lifecycle (generate, verify, correct) |

Harness conformance fixtures for `mosaic-run` live under `Tools/Runner/TestCatalog/Workflows/MosaicTest/` — they are test fixtures for the Runner tool, not part of this catalog.

---

## Workflow Summary

### Build

| ID | Category | Version | Name | Author | File |
|----|----------|---------|------|--------|------|
| greenfield-tdd | Build | 4.0 | Greenfield TDD Workflow | MOSAIC | `Build/greenfield-tdd.md` |
| brownfield-tdd | Build | 4.0 | Brownfield TDD Workflow | MOSAIC | `Build/brownfield-tdd.md` |
| quick-fix | Build | 1.0 | Quick Fix Workflow | MOSAIC | `Build/quick-fix.md` |
| implementation-only | Build | 4.0 | Implementation Only Workflow | MOSAIC | `Build/implementation-only.md` |
| brownfield-tdd-build-verified | Build | 3.0 | Brownfield TDD Build-Verified Workflow | MOSAIC | `Build/brownfield-tdd-build-verified.md` |
| brownfield-pr-fix | Build | 1.0 | Brownfield PR Fix Workflow | MOSAIC | `Build/brownfield-pr-fix.md` |

### Audit

| ID | Category | Version | Name | Author | File |
|----|----------|---------|------|--------|------|
| brownfield-pr-audit | Audit | 4.1 | Brownfield PR Audit Workflow | MOSAIC | `Audit/brownfield-pr-audit.md` |
| brownfield-system-audit | Audit | 2.0 | Brownfield System Audit Workflow | MOSAIC | `Audit/brownfield-system-audit.md` |

### Research

| ID | Category | Version | Name | Author | File |
|----|----------|---------|------|--------|------|
| brownfield-research-only | Research | 1.0 | Brownfield Research Only Workflow | MOSAIC | `Research/brownfield-research-only.md` |
| product-comparison | Research | 2.0 | Product Comparison Workflow | MOSAIC | `Research/product-comparison.md` |

### Design

| ID | Category | Version | Name | Author | File |
|----|----------|---------|------|--------|------|
| brownfield-design | Design | 4.0 | Brownfield Design Workflow | MOSAIC | `Design/brownfield-design.md` |

### Verification

| ID | Category | Version | Name | Author | File |
|----|----------|---------|------|--------|------|
| requirements-to-test-cases | Verification | 1.0 | Requirements to Test Cases Workflow | MOSAIC | `Verification/requirements-to-test-cases.md` |

### DataPreprocessing

| ID | Category | Version | Name | Author | File |
|----|----------|---------|------|--------|------|
| kb-generation | DataPreprocessing | 2.0 | Knowledge Base Generation Workflow | MOSAIC | `DataPreprocessing/kb-generation.md` |
| kb-verification-human | DataPreprocessing | 1.0 | Knowledge Verification (Human) Workflow | MOSAIC | `DataPreprocessing/kb-verification-human.md` |
| kb-correction | DataPreprocessing | 1.0 | Knowledge Base Correction Workflow | MOSAIC | `DataPreprocessing/kb-correction.md` |
| hw-schema-kb-generation | DataPreprocessing | 1.0 | HW Schema Knowledge Base Generation Workflow | MOSAIC | `DataPreprocessing/hw-schema-kb-generation.md` |
