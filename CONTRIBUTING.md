# Contributing to MOSAIC

Thanks for your interest in MOSAIC. This guide explains how you can help, what we accept, and what we don't.

## Philosophy

MOSAIC teaches you to fish. It provides the framework — the agent composition model, the communication protocol, the deployment tooling, the workflow engine — and ships reference examples that show how it all works together. The framework is the product. The examples are how you learn to use it.

At its core, MOSAIC is just markdown files. Agents, workflows, artifacts — all plain text. The tools exist to manage those files, not to be the system. This simplicity is deliberate: MOSAIC only depends on capabilities every harness already has (read files, write files, dispatch agents), so it deploys anywhere with minimal friction. Advanced features like hooks are optional layers on top — logging hooks exist today, and hooks could do much more in the future — but the core system runs without them.

This means contributions that strengthen the framework, its tooling, and its knowledge base are where help matters most. The reference catalog (agents, workflows, skills) is intentionally "good enough" — meant to get people started and inspire them to build better. We don't polish the examples to perfection; we make the framework powerful enough that you can.

## How to Contribute

**Every contribution starts with an issue.** Before writing any code or submitting a PR, open a GitHub issue describing what you want to do and why. This lets us align on approach before you invest time. Even for small fixes — a quick issue takes a minute and saves potential rework.

Contributions are listed below in rough priority order — what helps the project most, first.

### 1. Use It and Report What Breaks

This is the single most valuable thing you can do. MOSAIC supports multiple harnesses, multiple execution modes, and a growing set of workflows. Every real-world run on a harness/model/workflow combination we haven't tested is a discovery opportunity.

**What to report:**
- Orchestrator routing errors — wrong subagent dispatched, incorrect status interpretation, workflow stuck
- Harness-specific failures — things that work on one harness but break on another
- Deployment issues — `mosaic-deploy` producing incorrect output for your harness or project structure
- Runner pipeline problems — `mosaic-run` session failures, incorrect stage transitions
- Unclear documentation — if you got stuck, others will too

**How to report:** Open a GitHub issue with steps to reproduce. Include the harness, model, and workflow you were using. Attach the `Orchestration.md` artifact and any runner logs if available — these give us the full picture.

### 2. Harness Knowledge

MOSAIC's effectiveness depends on understanding how each harness actually behaves — not how it's documented, but what it does in practice. This knowledge lives in `HarnessKnowledge/` and has two parts:

**Known Issues** (`HarnessKnowledge/KnownIssues/`): Bugs, limitations, and quirks in harness platforms that affect MOSAIC orchestration. If you discover a harness behavior that causes unexpected results, document it. See the [Capture Guide](HarnessKnowledge/KnownIssues/CaptureGuide.md) for the format.

**System Prompt Captures** (`HarnessKnowledge/SystemPromptCapture/`): Harnesses inject system prompts and tool definitions that affect agent behavior. Captures of these across harness versions help us understand and adapt. See the [SystemPromptCapture README](HarnessKnowledge/SystemPromptCapture/README.md) for details.

Both areas have established file structures, naming conventions, and capture guides — follow the existing patterns. The harness-issue-hunter utility agent can help with the format if you have MOSAIC deployed.

### 3. Tooling

MOSAIC has four Go CLI tools. All of them welcome improvements:

| Tool | Binary | What It Does |
|------|--------|-------------|
| **Deployment** | `mosaic-deploy` | Assembles and deploys agents to target projects |
| **Runner** | `mosaic-run` | Drives headless orchestration runs |
| **AgentTest** | `mosaic-agent-test` | Test harness for orchestrator routing accuracy |
| **LogAnalyzer** | `mosaic-log-analyzer` | Post-run log analysis |

Particularly welcome: bug fixes, performance improvements, better error messages, and UX improvements.

**Adding new harness support** is one of the highest-impact tooling contributions. Simple harnesses that only need tool mapping and static frontmatter can use a descriptor-only approach (a single YAML file, no code). Harnesses that need richer logic — dynamic tool expansion, conditional configuration, complex path generation — require a full built-in module compiled into the MOSAIC binary. See the [Harness Contributor Guide](Tools/Deployment/docs/harness-contributor-guide.md) for background on both approaches.

### 4. Harness Injections

The platform-specific prompt fragments in `Catalog/HarnessInjections/` adapt generic agents to each harness. If you have deep experience with a specific harness, improvements to its injection content directly improve every deployed agent on that platform.

### 5. Orchestration Test Results

We need test coverage across harness/model combinations that no single person can run. If you run `mosaic-agent-test` suites, we'd like to include your results in the project's test result archive.

**Requirements for submitted test results:**
- Full AgentTest JSON report files (the `report-*.json` output), not summaries
- Full run logs from the mosaic-logger hook for each test run
- Results must not be hand-edited — submit them as the tools produced them

Community-submitted results will be labeled as **community-reported** and stored separately from maintainer-verified results. This isn't a trust judgment — it's a provenance signal. Different environments produce different results, and readers should know the source. Results are archived under `OrchestrationTestResults/` following the existing directory and naming conventions.

> **Note:** The report JSON schema doesn't currently carry a provenance field. This is a known tooling gap — community vs. maintainer labeling is handled through directory separation until the schema is extended.

## What We Don't Accept

### Catalog Content (Agents, Workflows, Skills)

We do not accept PRs that add or modify content in:
- `Catalog/Subagents/`
- `Catalog/Workflows/`
- `Catalog/Skills/`
- `Catalog/Orchestrator/`
- `Catalog/UtilityAgents/`

These are reference examples, not a community library. They exist to demonstrate patterns and give you a working starting point. They could be better — that's the point. The framework gives you everything you need to create agents and workflows that are perfectly tuned to your needs. We'd rather you build great ones for your projects than polish the reference set.

**Exception:** If you find a genuine bug in a reference agent or workflow (broken protocol compliance, incorrect status codes, malformed structure), report it as an issue. We'll fix it.

### Standalone Agents

`Catalog/StandaloneAgents/` contains agents authored for specific purposes outside the orchestration system. We don't accept additions here — build your own standalone agents in your projects.

## Contribution Guidelines

### Code Contributions

**Language:** Tools are written in Go. Follow the existing code style and package structure.

**Tests:** Tool changes should include tests. Run the existing test suite before submitting — `Tools/run-all-tests.ps1` or run tests per-tool with `go test ./...` from the tool directory.

**MOSAIC-assisted development encouraged:** If you use MOSAIC itself to develop your contribution (deploy it to your workspace, run a workflow), we appreciate seeing the orchestration logs alongside your PR. This isn't required, but it does two things we value — it tests the system in a real scenario, and it demonstrates working familiarity with the project. Include the full `Orchestration-{run_id}/` folder from your run if you have one — not just `Orchestration.md`, but the complete artifact set.

### Documentation Contributions

Clear documentation matters. If you found something confusing, a PR that improves it is welcome. Documentation lives in:
- `docs/` — user-facing guides for the deployed system
- `Tools/*/docs/` — tool-specific documentation
- `HarnessKnowledge/` — harness behavior documentation

## Development Setup

1. **Go toolchain** — Install [Go](https://go.dev/) (check `go.work` for the minimum version)
2. **Clone the repo** — `git clone https://github.com/tomasgurtler21/MOSAIC.git`
3. **Build tools** — From the repo root:
   ```
   cd Tools/Deployment/cmd/mosaic-deploy && go build
   cd Tools/Runner/cmd/mosaic-run && go build
   cd Tools/AgentTest/cmd/mosaic-agent-test && go build
   cd Tools/LogAnalyzer/cmd/mosaic-log-analyzer && go build
   ```
4. **Run tests** — `Tools/run-all-tests.ps1` (PowerShell) or `go test ./...` from each tool directory

## PR Process

1. **Open an issue first** describing what you want to do
2. **Fork the repo** and create a branch from `main`
3. **Make your changes** — keep PRs focused on a single concern
4. **Run tests** — make sure existing tests pass
5. **Submit the PR** referencing the issue number
6. **Respond to review** — we may ask questions or request changes

## Questions?

Open a GitHub issue with the `question` label. We're happy to help you figure out where your contribution fits.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
