# Workflow Index

Workflow definitions organized by category. This file is the canonical registry — all 19 workflows are listed here with their metadata aligned to the frontmatter of each individual workflow file.

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
- **MosaicTest** — Harness conformance fixtures for `mosaic-run`; not for productive work

Browse the [Workflow Summary](#workflow-summary) table to find the right workflow by scanning the **Hint** column — each hint is a one-line signal for when to use that workflow. For fuller context, open the individual workflow file listed in the **File** column.

### Injecting a Workflow into an Orchestrator

Each workflow file contains a phase table that your orchestrator reads to know which agents to dispatch, in what order, and with what routing logic. To use a workflow:

1. Identify the workflow ID from this index.
2. Open the corresponding file under `Workflows/<Category>/<id>.md`.
3. Copy or reference the phase table into your orchestrator configuration.
4. Provide the required input artifacts listed in the workflow's frontmatter `artifacts` field.

Orchestrators that support auto-discovery can parse this index programmatically: the **ID** and **File** columns are the stable keys. The **Version** column lets you detect when a workflow has changed.

### Category Taxonomy

| Category | Subfolder | Purpose |
|----------|-----------|---------|
| Build | `Workflows/Build/` | End-to-end feature and project development |
| Audit | `Workflows/Audit/` | Evidence-based quality analysis of existing code |
| Research | `Workflows/Research/` | Exploration and feasibility — no implementation |
| Design | `Workflows/Design/` | Architecture and design — no implementation |
| Verification | `Workflows/Verification/` | Deriving verification artifacts from specifications — no implementation |
| DataPreprocessing | `Workflows/DataPreprocessing/` | Knowledge base lifecycle (generate, verify, correct) |

---

## Workflow Summary

### Build

| ID | Category | Version | Name | Description | Hint | Author | File |
|----|----------|---------|------|-------------|------|--------|------|
| greenfield-tdd | Build | 3.3 | Greenfield TDD Workflow | Building a new project from scratch requiring system architecture, test-first development, and full design. | Full greenfield with architecture, TDD, and design phases | MOSAIC | `Build/greenfield-tdd.md` |
| brownfield-tdd | Build | 3.4 | Brownfield TDD Workflow | New features or significant changes to an existing codebase requiring test-first development with full research and design. | Brownfield with research, TDD, and design phases | MOSAIC | `Build/brownfield-tdd.md` |
| quick-fix | Build | 3.0 | Quick Fix Workflow | Small changes, bug fixes, or well-understood modifications. Skips research and design. | Small fixes and bug fixes without research or design | MOSAIC | `Build/quick-fix.md` |
| implementation-only | Build | 3.1 | Implementation Only Workflow | Research, planning, and design already complete. Direct implementation from existing artifacts. | Direct implementation from existing plan and contracts | MOSAIC | `Build/implementation-only.md` |
| brownfield-tdd-build-verified | Build | 2.0 | Brownfield TDD Build-Verified Workflow | New features or significant changes to an existing codebase requiring test-first development where compilation/build cannot be verified via standard terminal tools (e.g., PLC/SCL with proprietary toolchains, embedded systems, cross-compilation environments). | Brownfield TDD with build verification for proprietary toolchains | MOSAIC | `Build/brownfield-tdd-build-verified.md` |

### Audit

| ID | Category | Version | Name | Description | Hint | Author | File |
|----|----------|---------|------|-------------|------|--------|------|
| brownfield-pr-audit | Audit | 3.1 | Brownfield PR Audit Workflow | Audit quality of existing code for PR review — multi-pass research, plan-driven staged audits with 4 types, per-audit PR comment transform with cross-audit deduplication, and post. | PR audit with multi-pass research, parallel audit tracks, and PR comment integration | MOSAIC | `Audit/brownfield-pr-audit.md` |
| brownfield-system-audit | Audit | 1.0 | Brownfield System Audit Workflow | High-level quality assessment of an existing codebase or major subsystem — architecture and contracts audit without file-level analysis. | High-level system audit — architecture and contracts only, no file-level analysis | MOSAIC | `Audit/brownfield-system-audit.md` |

### Research

| ID | Category | Version | Name | Description | Hint | Author | File |
|----|----------|---------|------|-------------|------|--------|------|
| brownfield-research-only | Research | 2.1 | Brownfield Research Only Workflow | Exploration, feasibility studies, or codebase analysis for an existing codebase without implementation. | Research-only for existing codebase — no planning, design, or implementation | MOSAIC | `Research/brownfield-research-only.md` |

### Design

| ID | Category | Version | Name | Description | Hint | Author | File |
|----|----------|---------|------|-------------|------|--------|------|
| brownfield-design | Design | 3.2 | Brownfield Design Workflow | Architecture review, design proposals, or planning large features for an existing codebase without implementation. | Full design workflow for existing codebase — research, requirements, planning, and design | MOSAIC | `Design/brownfield-design.md` |

### Verification

| ID | Category | Version | Name | Description | Hint | Author | File |
|----|----------|---------|------|-------------|------|--------|------|
| requirements-to-test-cases | Verification | 1.1 | Requirements to Test Cases Workflow | Derive abstract test cases from a requirement held in large external specification documents. Resolves the requirement to closure via runtime retrieval, models the test scenario space, authors test cases in a project-defined format, and exports them to the target system. | Generate abstract test cases from specification documents via runtime retrieval — no implementation | MOSAIC | `Verification/requirements-to-test-cases.md` |

### DataPreprocessing

| ID | Category | Version | Name | Description | Hint | Author | File |
|----|----------|---------|------|-------------|------|--------|------|
| kb-generation | DataPreprocessing | 0.5 | Knowledge Base Generation Workflow | Generate N-tier knowledge base documentation for a codebase, producing hierarchical documentation optimized for AI agent navigation — tiered from project overview down to complex subsystem specs. | Generate tiered KB documentation for a codebase with flag-based correction loop | MOSAIC | `DataPreprocessing/kb-generation.md` |
| kb-verification-human | DataPreprocessing | 0.4 | Knowledge Verification (Human) Workflow | Verify knowledge quality using architect-provided challenge questions. Tests whether an agent can answer expert questions using available knowledge sources + codebase. Produces a diagnostic report — remediation is a separate concern. | Verify KB quality using human-provided challenge questions | MOSAIC | `DataPreprocessing/kb-verification-human.md` |
| kb-verification-sampler | DataPreprocessing | 0.4 | Knowledge Verification (Sampler) Workflow | Verify knowledge quality using automated random sampling. A sampler agent explores the codebase, generates challenge questions about non-obvious details, then tests whether available knowledge sources guide an agent to the correct answers. Produces a diagnostic report — remediation is a separate concern. | Verify KB quality using automated random question sampling | MOSAIC | `DataPreprocessing/kb-verification-sampler.md` |
| kb-correction | DataPreprocessing | 0.1 | Knowledge Base Correction Workflow | Apply known corrections to an existing knowledge base. Input is user-provided correction instructions in Requirements.md — could be pasted verification findings, direct feedback, or change descriptions. | Apply targeted corrections to an existing knowledge base | MOSAIC | `DataPreprocessing/kb-correction.md` |
| kb-verification-sampler-human | DataPreprocessing | 1.0 | Knowledge Verification (Sampler + Human) Workflow | Verify knowledge quality using both architect-provided challenge questions and automated random sampling. Gathers questions from both sources in parallel, tests whether an agent can answer them using available knowledge sources + codebase, and produces a unified diagnostic report. Remediation is a separate concern. | Verify KB quality using both human questions and automated sampling in parallel | MOSAIC | `DataPreprocessing/kb-verification-sampler-human.md` |
| hw-schema-kb-generation | DataPreprocessing | 0.5 | HW Schema Knowledge Base Generation Workflow | Generate knowledge base documentation for a hardware schematic design. Researches each sheet individually, then synthesizes domain-oriented KB documentation with tiered abstraction — from project overview down to complex circuit subsystems. | Generate tiered KB documentation from hardware schematic sheets | MOSAIC | `DataPreprocessing/hw-schema-kb-generation.md` |