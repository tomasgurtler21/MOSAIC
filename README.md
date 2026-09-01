# MOSAIC

Multi-agent Orchestration System for AI Coding — a platform-agnostic framework for orchestrating specialized AI agents through structured workflows.

## Repository Structure

```
MOSAIC/
├── docs/                             # User-facing guides for the deployed system
│   ├── AgentCustomizationGuide.md
│   ├── DeploymentGuide.md
│   ├── OrchestrationGuide.md
│   └── RunnerGuide.md
│
├── Catalog/                         # Deployable content — agents, workflows, skills, hooks
│   ├── Orchestrator/                # The primary orchestration agent (exactly one)
│   │   └── orchestrator.md
│   ├── Subagents/                   # Specialized agents dispatched by the orchestrator
│   │   ├── Audit/
│   │   ├── Creation/
│   │   ├── Execution/
│   │   ├── Infrastructure/
│   │   ├── Interface/
│   │   ├── Planning/
│   │   ├── Research/
│   │   └── Validation/
│   ├── UtilityAgents/               # MOSAIC system-maintenance agents (not dispatched in runs)
│   ├── StandaloneAgents/            # User-authored agents outside the MOSAIC orchestration system
│   ├── Workflows/                   # Workflow definitions consumed by the orchestrator
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
├── Development/                     # Internal design work (not part of the deployed system)
│   ├── Designs/                     # Specifications and architecture decisions
│   ├── Research/                    # Background research and theory
│   └── Analysis/                    # Design decision analysis
```

## Key Concepts

**Orchestrator** — the central agent that reads workflow definitions and dispatches subagents in sequence. There is exactly one. It contains zero workflow-specific logic — workflows are pure configuration.

**Subagents** — specialized agents (research, planning, code generation, review, etc.) that receive tasks from the orchestrator and return structured results. They never communicate with each other directly — all coordination flows through the orchestrator (hub-and-spoke).

**Utility Agents** — agents that help maintain the MOSAIC system itself (creating new subagents, authoring workflows, managing transformations). They are not dispatched during orchestration runs.

**Standalone Agents** — user-authored agents that are not part of the MOSAIC orchestration system. They live in the catalog so they can be deployed through `mosaic-deploy` like any other agent, but they are never dispatched by the orchestrator.

**Workflows** — declarative tables that define which subagents run in what order, with what routing logic. Injected into the orchestrator's prompt at deploy time.

**Communication Protocol** — all orchestrator-subagent communication uses structured JSON messages with standardized status codes. Shared state is maintained through an orchestration artifact (blackboard pattern) that persists across context windows.

**Skills** — reusable knowledge modules (e.g. TDD methodology, file reading patterns) that get injected into agent prompts at deployment time.

**Hooks** — platform-specific code bundles (e.g. logging) that run alongside agents in supported environments.

**Harness Injections** — platform-specific prompt fragments that adapt generic agents to a particular AI coding tool's capabilities and syntax.
