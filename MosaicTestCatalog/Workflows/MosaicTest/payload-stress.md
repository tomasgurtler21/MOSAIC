---
version: "1.0"
name: "MosaicTest Payload Stress Workflow"
description: "Harness conformance fixture — three awkward status_message payloads (unicode, fenced backticks, JSON-in-JSON) returned through one run, to find where a harness mangles encoding or escaping."
hint: "Harness test — unicode, backtick and JSON-in-JSON payload fidelity"
author: MOSAIC
id: payload-stress
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/payload-unicode.md
  - MosaicTestScript/payload-fences.md
  - MosaicTestScript/payload-json.md
---

[[SECTION:Workflow:payload-stress]]
<!-- workflow-version: 1.0 -->
## MosaicTest Payload Stress Workflow

**Use when:** Checking whether a harness carries awkward `status_message` content through intact. This is where harnesses genuinely differ, and a failure here is usually an escaping or encoding bug rather than a routing one.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | mosaictest-scripted | ❌ | - | - | MosaicTestScript/payload-unicode.md | - |
| EXECUTION.[StageNumber] | mosaictest-scripted | ❌ | - | - | MosaicTestScript/payload-fences.md | - |
| EXECUTION.[StageNumber] | mosaictest-scripted | ❌ | - | - | MosaicTestScript/payload-json.md | - |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). The fixture plan declares a single stage, so each row runs exactly once.

**Notes:**
- **`Plan.md` is a pre-placed fixture.** It and the scripts must be seeded into the run folder at run creation. Seed `Fixtures/payload-stress` — the whole directory, not anything inside it — as the single seed path. See `Fixtures/README.md`.
- One payload class per row, so a failure names the class that broke rather than "the payload broke."
- Nothing is written. The payload under test travels only in `status_message`, which is where a harness's own JSON envelope has to carry it.

[[/SECTION:Workflow:payload-stress]]

---

## Design Rationale

The three rows are staged rather than sequential non-staged rows for a structural reason, not a semantic one: outside EXECUTION, the runner resolves `On Success` to the **first** row matching the named agent, so a workflow cannot use the same agent in two non-staged rows without routing looping back to the first. Inside EXECUTION, `On Success` is ignored and routing is positional, so one agent may occupy any number of rows. A single-stage plan therefore gives three sequential invocations of one stub — the shape this fixture needs — at the cost of a pre-placed plan.

The payload classes are separated because they fail for different reasons and are fixed in different places. Unicode failures are encoding — code page, byte-order, or astral-plane handling. Fenced backticks fail when a harness or a wrapper re-parses the response as markdown and the payload terminates a block early. JSON-in-JSON fails on escaping, and is the one most likely to corrupt the envelope itself rather than just the message. Combining them into one payload would leave the diagnosis to guesswork.

Row order runs cheapest-to-diagnose first. If the unicode row already mangles its output there is little point reading the JSON row's result closely.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-03 | MOSAIC | Initial version |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A fourth row carrying several kilobytes of text, to find truncation limits. Left out of v1 because the failure mode is a threshold rather than a yes/no, so it needs a different readout than "watch the TUI."
- Payloads containing a literal `[checkpoint:...]` marker, to check that marker extraction does not misfire on agent-supplied text.

**Dead ends (tried and rejected):**
- Three non-staged rows. Not authorable: `On Success` resolves by agent name to the first matching non-staged row, so rows 2 and 3 would be unreachable and row 1 would loop.
