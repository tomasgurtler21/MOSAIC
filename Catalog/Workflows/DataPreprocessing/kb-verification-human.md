---
version: 0.4
name: Knowledge Verification (Human) Workflow
description: Verify knowledge quality using architect-provided challenge questions. Tests whether an agent can answer expert questions using available knowledge sources + codebase. Produces a diagnostic report — remediation is a separate concern.
hint: "Theoretical — never used in practice. Beyond its stated diagnostic purpose, this is also a natural harness for an A/B comparison: run the same question set with and without an existing KB in scope to measure the actual delta in speed, cost, and answer precision the KB provides, rather than assuming it."
author: MOSAIC
id: kb-verification-human
referenced_agents:
  - verification-questions-preparer
  - codebase-research
  - verification-answer-validator
artifacts:
  - VerificationQuestions.md
  - VerificationAnswers.md
  - VerificationAttemptedAnswers.md
  - VerificationReport.md
---

<Workflow type="core" name="kb-verification-human" version="0.4">
## Knowledge Verification (Human) Workflow

**Use when:** Verify knowledge quality using **architect-provided challenge questions**. Tests whether an agent can answer expert questions using available knowledge sources + codebase. Produces a diagnostic report — remediation is a separate concern.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | verification-questions-preparer | ✅ | codebase-research* | - | - | - | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md |
| EXECUTION.[StageNumber] | codebase-research | ❌ | verification-answer-validator | - | - | VerificationQuestions.md, VerificationAttemptedAnswers.md | VerificationAttemptedAnswers.md |
| REVIEW | verification-answer-validator | ✅ | COMPLETE | - | codebase-research* | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md | VerificationReport.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**EXECUTION Stages:** One stage per question batch, all dispatched in parallel via `codebase-research*`. Stages tracked in VerificationAttemptedAnswers.md.

**Notes:**
- **Diagnostic only** — workflow ends at VerificationReport.md. To act on findings, run a separate remediation workflow (e.g., Knowledge Base Correction)
- **Knowledge-source agnostic** — verifies whether questions can be answered from whatever knowledge sources the codebase-research agent has access to (KB, docs, code comments, etc.)
</Workflow>

---

## Design Rationale

Theoretical at this point — designed but never actually run. Its stated purpose is diagnostic: does the available knowledge (KB, docs, code comments, whatever the research agent can reach) let an agent correctly answer expert-authored challenge questions.

There's a second use this workflow's structure naturally supports and hasn't been exploited yet: because it's knowledge-source agnostic by design, the same question set can be run twice — once with an existing KB in scope, once without — to directly measure what the KB actually buys in speed, cost, and answer precision, rather than assuming `kb-generation` is worth its cost on faith.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 0.4 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- **With/without-KB comparison run.** Run the same VerificationQuestions.md twice — once against a codebase with an existing KB, once with the KB excluded from scope — and diff the resulting VerificationReport.md for speed, cost, and precision. Would turn this from a pass/fail diagnostic into actual evidence for whether kb-generation is worth running on a given codebase.

**Dead ends (tried and rejected):**
- (none yet)
