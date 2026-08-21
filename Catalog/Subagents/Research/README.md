# Research

Information gathering and exploration agents.

## Purpose

Research agents analyze and explore to build foundational understanding for downstream agents. They investigate and document - they do not plan, design, or implement.

## Agents

| ID | Agent | Version | Description |
|----|-------|---------|-------------|
| 1 | [codebase-research](./codebase-research.md) | 2.4.0 | Analyzes codebase, explores existing patterns, and documents findings |
| 3 | [knowledge-base-generator](./knowledge-base-generator.md) | 1.3.0 | Researches codebase scope and produces N-tier knowledge base documentation |
| 2 | [library-research](./library-research.md) | 1.0.2 | Researches libraries, frameworks, and external dependencies |
| 53 | [product-research](./product-research.md) | 1.0.0 | Builds the foundational map of a single product — its core functions and where each lives in the codebase |
| 54 | [end-user-experience-research](./end-user-experience-research.md) | 1.0.0 | Single-product, verdict-free findings on end-user experience |
| 55 | [maintainability-research](./maintainability-research.md) | 1.0.0 | Single-product, verdict-free findings on maintainability |
| 56 | [extensibility-research](./extensibility-research.md) | 1.0.0 | Single-product, verdict-free findings on extensibility |
| 57 | [design-quality-research](./design-quality-research.md) | 1.0.0 | Single-product, verdict-free findings characterizing the design |
| 58 | [quality-mechanisms-research](./quality-mechanisms-research.md) | 1.0.0 | Single-product, verdict-free findings on quality-assurance mechanisms |
| 59 | [human-oversight-research](./human-oversight-research.md) | 1.0.0 | Single-product, verdict-free findings on human oversight support |
| 60 | [cost-research](./cost-research.md) | 1.0.0 | Single-product, verdict-free findings on cost drivers |
| 61 | [security-research](./security-research.md) | 1.0.0 | Single-product, verdict-free defensive characterization of security posture |
| 62 | [performance-research](./performance-research.md) | 1.0.0 | Single-product, verdict-free findings on runtime efficiency and scalability |
| 63 | [topic-analyst](./topic-analyst.md) | 1.0.0 | Generic per-dimension comparison agent — reads every product's findings for one assigned dimension and produces an evaluative cross-product comparison |
| 64 | [comparison-analyst](./comparison-analyst.md) | 1.0.0 | Synthesizes per-topic comparisons into one decision-useful overall comparison, rolling up per-dimension verdicts and surfacing emergent cross-dimension trade-offs |

## What Research Agents Do

- Analyze requirements documents, user stories, and specifications
- Explore existing codebase to understand patterns, conventions, and architecture
- Identify dependencies, risks, and technical constraints
- Synthesize findings into structured orchestration artifacts
- Flag ambiguities and open questions for clarification

## What Research Agents Do NOT Do

- Make implementation decisions
- Write code or tests
- Validate requirements completeness
- Create implementation plans or proposals
