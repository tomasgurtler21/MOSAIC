# MOSAIC

Multi-agent Orchestration System for AI Collaboration — a platform-agnostic framework for orchestrating specialized AI agents through structured workflows.

MOSAIC is primarily a set of **design principles and tooling** that enable you to build your own multi-agent orchestration system — the communication protocol, the three-layer agent composition model, the workflow-as-configuration pattern, and deployment tooling that makes it all work across AI platforms. The repository also ships reference subagents and workflows that demonstrate these principles in practice and give you a working starting point, but the framework is the product — the catalog is how you learn to use it.

The current harnesses are AI coding tools, but the orchestration model is not limited to coding; workflows can coordinate any work that AI agents can perform.

---

## Quick Start

> **Prerequisite:** You need a working AI coding tool — Claude Code, OpenCode, VS Code GitHub Copilot, or GitHub Copilot CLI — already installed and configured with model access.

1. **Clone & get tools** — `git clone https://github.com/tomasgurtler21/MOSAIC.git` + download the [latest release](https://github.com/tomasgurtler21/MOSAIC/releases) for your platform (Linux x64, Windows x64) and unpack into the repo root
2. **Deploy** — run `mosaic-deploy` from the MOSAIC repo root — pick your harness, select `kb-generation` workflow, assign models, and point it at your project workspace
3. **Run** — open the orchestrator agent in your AI tool within the project workspace and tell it: `Use kb-generation workflow. Task: Generate knowledge base for this codebase. Checkpoints disabled.`

Full walkthrough: **[Getting Started Guide](docs/GettingStarted.md)**

---

## What Makes MOSAIC Different

### Three-Layer Agent Composition

A single deployed MOSAIC agent is assembled from three independent sources, each owned by a different role:

| Layer | Owner | What They Contribute | Examples |
|-------|-------|---------------------|----------|
| **System** | AI/orchestration experts | Communication protocol, error handling, status codes, authority hierarchy — everything that makes multi-agent coordination reliable | Protocol envelope, status code mapping, structured output format |
| **Agent** | Agent design specialists | The agent's core identity, scope, process, constraints — what it does and how it thinks | A planner that produces phased implementation plans; a reviewer that validates contracts |
| **Project** | End users | Project-specific context that makes generic agents truly specialized — codebase conventions, cross-tool dependencies, quality bar, domain vocabulary | "Tests must not depend on files outside their own tool tree"; "Tools/AgentTest drives Tools/Deployment as a subprocess" |

Each layer is authored independently. The project owner never needs to understand what the AI experts put in the system layer. The agent designer never needs to know the user's codebase. At deployment, `mosaic-deploy` composes all three layers into one coherent agent file through injection points — placeholder regions in the agent template that get filled with content from the appropriate owner.

This is architecturally unusual. A single agent's instructions are sensitive to phrasing, emphasis, and internal consistency — and here three different authors contribute to one prompt, each unaware of the others' specific content. It works because the boundaries are structural (injection points with defined semantics), not ad-hoc.

The payoff is deep specialization. Each agent gets exactly the right context and nothing else — which directly improves performance and enables running narrowly-scoped agents on smaller, cheaper models without losing quality.

### Human-Readable Workflow Tables

Workflows are plain markdown tables, not code, not YAML pipelines, not visual node editors. A workflow looks like this:

| Phase | Subagent | On Success | On Findings | HITL | Artifacts In | Artifacts Out |
|-------|----------|------------|-------------|------|-------------|---------------|
| RESEARCH | codebase-research | requirements-refinement | — | Autonomous | — | Research.md |
| PLANNING | planner-tdd-soft | plan-review | — | Approve | Research.md, Requirements.md | Plan.md, PlanDetails.md |
| PLANNING | plan-review | contracts-designer | planner-tdd-soft | Autonomous | Plan.md, PlanDetails.md | plan-review.md |

Full transparency — any human can read the routing, understand what happens when, and modify it. Creating a new workflow or adjusting an existing one means editing a markdown table, not writing code or learning a DSL. This trades some machine-friendliness for complete human accessibility, and MOSAIC compensates with validation tooling and a workflow-creator agent that helps with the design.

---

## Harness-Agnostic Design

MOSAIC is not tied to any single AI agent platform. It targets four **harnesses** today — Claude Code, OpenCode, VS Code GitHub Copilot, and GitHub Copilot CLI — and treats them as interchangeable backends. The same agent definitions and workflows deploy to all of them through a transformation layer that adapts generic source files to each harness's syntax and conventions.

---

## Orchestration Model

**Hub-and-spoke.** One orchestrator agent coordinates specialized subagents. No direct agent-to-agent communication — all routing flows through the orchestrator. The orchestrator reads a workflow table (a compact markdown state machine) and dispatches subagents in the order and conditions it specifies. It contains zero workflow-specific logic — workflows are pure configuration.

**Blackboard state.** Each orchestration run gets an `Orchestration-{run_id}/` folder. Inside, `Orchestration.md` tracks every dispatch, every status response, and every artifact produced. This file is the shared state — agents read it to understand what has happened so far and write their outputs as artifacts in the same folder.

**Structured communication.** Orchestrator-subagent messages use a JSON protocol with standardized status codes (SUCCESS, COMPLETED_NEEDS_ACTION, PARTIALLY_DONE, NEEDS_CLARIFICATION, CAPABILITY_EXCEEDED, BLOCKED). The orchestrator interprets these to decide routing — retry, escalate, continue, or halt.

### Two Execution Modes

| Mode | Mechanism | When It Happens |
|------|-----------|-----------------|
| **Interactive / harness-native** | The harness (e.g., Claude Code) runs the orchestrator agent, which dispatches subagents using the harness's native agent/task tool. Everything happens within one harness session. | User starts a conversation with the orchestrator agent in their AI agent harness |
| **Runner pipeline** | `Tools/Runner` (`mosaic-run`) drives headless CLI invocations per workflow stage, treating each harness as a subprocess backend. The runner manages the session lifecycle externally. | Automated or semi-automated runs via the CLI |

---

## Key Concepts

| Term | Meaning |
|------|---------|
| **Harness** | An AI agent platform that MOSAIC deploys to and runs on (Claude Code, OpenCode, VS Code GHCP, GHCP CLI) |
| **Orchestrator** | The single primary agent that reads a workflow and dispatches subagents. Contains zero workflow-specific logic |
| **Subagent** | A specialized agent dispatched by the orchestrator for a single task within a workflow (research, planning, implementation, review, etc.). Never communicates with other subagents directly — all coordination flows through the orchestrator |
| **Utility agent** | An agent that maintains MOSAIC itself — creating subagents, authoring workflows, hunting harness issues. Not dispatched during orchestration runs |
| **Standalone agent** | A user-authored agent outside the orchestration system, deployed via MOSAIC tooling but never orchestrator-dispatched |
| **Workflow** | A declarative table defining which subagents run in what order, with routing conditions. Pure configuration, not code |
| **Skill** | A reusable knowledge module (e.g., TDD methodology) injected into agent prompts at deployment |
| **Hook** | Platform-specific code bundle (e.g., logging) that runs alongside agents in supported environments |
| **Injection point** | A placeholder region in a source agent file that gets filled with layer-specific content at deploy time — the mechanism that makes three-layer composition work |
| **Deployment** | The process where `Tools/Deployment` (`mosaic-deploy`) assembles source agents + harness injections + skills into harness-ready agent files in a target project |
| **Run** | A single execution of a workflow — gets its own folder, state file, and artifact set |
| **Blackboard / Orchestration.md** | The shared state artifact for a run — full audit trail of dispatches, responses, and artifacts |
| **Harness injection** | Platform-specific prompt fragments that adapt generic agents to a specific harness's capabilities |

---

## Repository Structure

```
MOSAIC/
├── docs/                             # User-facing guides for the deployed system
│   ├── GettingStarted.md
│   ├── AgentCustomizationGuide.md
│   ├── DeploymentGuide.md
│   ├── OrchestrationGuide.md
│   └── RunnerGuide.md
│
├── Catalog/                         # Deployable content — framework core + reference examples
│   ├── Orchestrator/                # The primary orchestration agent (exactly one)
│   │   └── orchestrator.md
│   ├── Subagents/                   # Reference subagents — working examples to use or build on
│   │   ├── Audit/
│   │   ├── Creation/
│   │   ├── Execution/
│   │   ├── Infrastructure/
│   │   ├── Interface/
│   │   ├── Planning/
│   │   ├── Research/
│   │   └── Validation/
│   ├── UtilityAgents/               # Framework agents — help you create subagents, workflows, and diagnose issues
│   ├── StandaloneAgents/            # User-authored agents outside the MOSAIC orchestration system
│   ├── Workflows/                   # Reference workflows — examples of the workflow-as-config pattern
│   │   ├── Audit/
│   │   ├── Build/
│   │   ├── DataPreprocessing/
│   │   ├── Design/
│   │   ├── Research/
│   │   └── Verification/
│   ├── Skills/                      # Shared knowledge modules injected into agents at deploy
│   │   ├── efficient-file-reading/
│   │   ├── git-read-commands/
│   │   ├── lean-tdd/
│   │   └── pr-scope-filtering/
│   ├── Hooks/                       # Platform hook bundles (e.g. logging)
│   │   └── mosaic-logger/
│   ├── HarnessInjections/           # Platform-specific deployment config
│   │   ├── Claude Code/
│   │   ├── GHCP CLI/
│   │   ├── OpenCode/
│   │   └── VS Code GHCP/
│   ├── SourceFilesFormat.md         # Source file format specification (frontmatter, boundary tags)
│   └── DeployedSections.md          # Canonical deployed section blocks for agent deployment
│
├── Tools/                           # Go-based CLI tooling
│   ├── Deployment/                  # mosaic-deploy — assembles and deploys agents to target projects
│   │   └── docs/                    # CLI reference, config, descriptor schema, external protocol, harness contributor guide
│   ├── Runner/                      # mosaic-run — starts orchestration runs
│   │   └── docs/                    # Design, running tests, script/orchestrator contract, test catalog design
│   ├── AgentTest/                   # mosaic-agent-test — test harness for agents
│   │   └── docs/                    # Design, launch guide, test authoring guide, test results design
│   ├── LogAnalyzer/                 # Post-run log analysis
│   └── Common/                      # Shared Go packages
│
├── HarnessKnowledge/                # Documentation about harness behaviors
│   ├── KnownIssues/                 # Tracked harness bugs, limitations, and quirks
│   └── SystemPromptCapture/         # Captured harness system prompts and tool definitions
│
├── Development/                     # Internal design work (not part of the deployed system)
│   ├── Designs/                     # Specifications and architecture decisions
│   ├── Research/                    # Background research and theory
│   └── Analysis/                    # Design decision analysis
```
