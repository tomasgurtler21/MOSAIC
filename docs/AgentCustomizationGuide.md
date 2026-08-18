# Agent Customization Guide

How to customize MOSAIC agents after deployment, and what happens when MOSAIC updates arrive.

This guide is for **project authors** — people who have received a MOSAIC deployment and want to tailor agents to their codebase, their conventions, and their workflows. You do not need to understand how MOSAIC is built; you need to understand what you can change, what you should not, and why.

---

## At a Glance

Every region in an agent file carries a `type` attribute on its opening tag. That attribute tells you everything you need to know:

| Type | Who owns it | Can you edit it? | Survives update? | On schema reorder |
|------|------------|-----------------|-----------------|-------------------|
| `core` | Catalog author | No — replaced on every update | No | N/A |
| `managed` | MOSAIC (canonical sources) | No — regenerated on every update | No (but nested project/custom regions inside it are preserved) | N/A |
| `project` | You (slot declared by catalog) | **Yes** — fill these, they improve agent performance | **Yes** — byte-for-byte | Moves automatically to the correct position |
| `custom` | You (slot you invented) | **Yes** — your content entirely | **Yes** — byte-for-byte | Parked at end of file with a TODO to reposition |

**The one rule:** Check the `type` before editing. If it says `core` or `managed`, your changes will be lost on the next update. Put your content in `project` or `custom` regions instead.

**Project vs. custom in one sentence:** If the catalog already declares a slot for what you need, use it (`project`) — it tracks structural changes automatically. If you're inventing something new, create your own (`custom`).

---

## How an Agent File Is Organized

A deployed agent file is a markdown document with two parts:

1. **Frontmatter** — a YAML block at the top (`---` delimiters) containing metadata: the agent's name, model, version, and deployment tracking fields.
2. **Body** — the agent's instructions, organized into named **regions** bounded by XML-like tags.

The body is where customization happens. Every region's opening tag declares who owns it:

```
<Identity type="core">
...
</Identity>
```

The `type` attribute — `core`, `managed`, `project`, or `custom` — tells you whether you can edit the content inside. That's the single thing to look at. If you're unsure whether something is yours to change, check the tag.

---

## The Four Region Types

### Core Regions — Catalog-authored instructions (`type="core"`)

```
<Identity type="core">
...
</Identity>
```

Core regions contain the agent's identity, capabilities, constraints, error handling, and working philosophy — written by the catalog author. They define what the agent is and how it behaves.

**Can you edit these?** No. On update, core content is replaced with whatever the new catalog version says. Any changes you make will be lost silently.

**What they look like in your file:** Filled with content. The six top-level sections of an agent (`Identity`, `CommunicationProtocol`, `Capabilities`, `Constraints`, `ErrorHandling`, `ExecutionPhilosophy`) are mostly core regions.

### Managed Regions — Tool-generated content (`type="managed"`)

```
<AuthorityHierarchy type="managed">
...
</AuthorityHierarchy>
```

Managed regions are filled from MOSAIC's canonical sources — the communication protocol, authority hierarchy, closing procedure, and other standardized text shared across all agents. The deployment tool writes the content, but the content itself is authored and maintained by MOSAIC's design layer. They appear nested inside core regions.

**Can you edit these?** No. The content is regenerated from scratch on every deploy or update. Anything you write directly inside a managed region will be overwritten.

**However:** You *can* nest project or custom regions inside a managed region (see below). The tool preserves those nested regions when regenerating the managed parent.

**Common managed regions you'll see:**

| Region | What it contains |
|--------|-----------------|
| `AuthorityHierarchy` | How the agent ranks conflicting instructions |
| `ClosingProcedure` | HITL review gate and response procedure |
| `CommunicationProtocol` | The orchestration contract — message format, status codes |
| `ProtocolConstraints` | Artifact access and status code discipline |
| `HarnessConstraints` | Platform-specific rules for your AI coding tool |
| `ErrorHandlingCommon` | Shared error handling (retry rules) |
| `ExecutionPhilosophyCommon` | Shared working posture (context, memory, quality) |

### Project Regions — Your slots, declared by the catalog (`type="project"`)

```
<CodebaseContext type="project">
</CodebaseContext>
```

Project regions are **slots declared in the catalog's source file for you to fill**. They appear in the source agent file (usually empty), and they're yours to fill with content specific to your project. On update, your content is preserved byte-for-byte — the tool never touches what's inside.

**Can you edit these?** Yes — that's what they're for. Fill them, change them, clear them. They're yours.

**Should you fill them?** Yes. If a project region exists on an agent, there is a good reason — the agent performs measurably better when that region carries project-specific content. An agent with empty project regions still *works*, but it works with less context than it was designed to use. Filling them is not mandatory, but leaving them empty degrades the agent's output quality. Treat them as strongly recommended, not optional.

**Project regions you'll encounter:**

| Region | Where it appears | What to put in it |
|--------|-----------------|-------------------|
| `CodebaseContext` | Capabilities | Knowledge about your codebase — repo structure, tech stack, naming conventions, important paths |
| `OutputArtifactTemplate` | Capabilities | Your preferred structure for this agent's output artifact |
| `SeverityThresholds` | Capabilities | Which issue severities require rework (validation agents only) |
| `SeverityDefinitions` | Capabilities | What each severity level means in your project (validation agents only) |
| `ContextLimits` | ExecutionPhilosophy | Context window thresholds and guidance |

Some project regions ship with **default content** — a starting point the catalog provides. After the first deploy, the content is yours. Even if the catalog updates the default in a future version, your copy is never overwritten.

### Custom Regions — Your slots, invented by you (`type="custom"`)

```
<LanguagePatterns type="custom">
### TypeScript Conventions
- Use `interface` over `type` for object shapes
- Prefer named exports
- ...
</LanguagePatterns>
```

Custom regions are regions **you create yourself** in your deployed files. The catalog source knows nothing about them — they don't exist in the source. You choose the name, you choose where they go, you write the content.

**Can you edit these?** Yes — you created them.

**On update:** Your content is preserved byte-for-byte, same as project regions.

---

## Project vs. Custom Regions — The Key Difference

Both project and custom regions hold your content, and both are preserved on update. The difference matters when MOSAIC reorganizes its agent structure (a **schema reorder**):

| | Project region (`type="project"`) | Custom region (`type="custom"`) |
|---|---|---|
| **Who created the slot** | Catalog author — declared in the source file | You — exists only in your deployed file |
| **On normal update** | Preserved in place | Preserved in place |
| **On schema reorder** | Moves automatically to the new correct position (because the source file defines where it belongs) | **Parked at the end of the file** with a TODO telling you to reposition it (because the source has no idea where you intended it) |

**In practice:** Schema reorders are rare. When they happen, your custom regions aren't lost — they're collected at the bottom of the file, and the tool tells you to move them. But if you want a region that tracks structural changes automatically, check whether the catalog already declares a project region for your use case before inventing a custom one.

---

## Where to Place Custom Regions

You have two choices for where a custom region lives: as a **sibling** (top-level or alongside other regions in a section) or **nested** inside another region. Both are valid; the choice depends on what the content is for.

### Sibling Placement

A sibling custom region sits at the same level as the core sections or alongside other regions within a core section:

```
<Capabilities type="core">
### Core Capabilities
- ...

<CodebaseContext type="project">
Your codebase knowledge here
</CodebaseContext>

<LanguagePatterns type="custom">
Your language conventions here
</LanguagePatterns>
</Capabilities>
```

**Use sibling placement when:** Your content is an independent block of guidance — language conventions, project-specific capabilities, domain knowledge. It stands on its own and doesn't modify any existing catalog content.

### Nested Placement (Inside a Managed Region)

A custom region can be nested inside a managed region:

```
<ErrorHandlingCommon type="managed">
- **Retry a transient error once** before escalating
  ...

<CustomRecovery type="custom">
- If a database connection fails, check the `.env.local` file first
- Network timeouts in CI are usually transient — always retry
</CustomRecovery>
</ErrorHandlingCommon>
```

**Use nested placement when:** Your content directly extends or annotates the managed content around it. The example above adds project-specific recovery guidance right next to the general retry rule.

**How it works on update:** The tool regenerates the managed parent's canonical text and preserves your nested region within it. Your content stays exactly where you put it.

### Nested vs. Sibling — Decision Guide

| Question | If yes → |
|----------|----------|
| Does your content extend a specific managed region's guidance? | Nest it inside that managed region |
| Is your content a standalone block of knowledge or rules? | Place it as a sibling in the appropriate core section |
| Are you unsure? | Start with sibling — it's simpler and you can always move it later |

---

## What Happens on Update

When a new version of MOSAIC is deployed over your existing agents, the tool does the following:

### What Gets Replaced (You Lose Your Changes)

| Content | What happens |
|---------|-------------|
| **Core regions** | Replaced with new catalog source content |
| **Managed regions** | Regenerated from canonical sources |
| **Frontmatter** | Catalog-controlled fields are updated; deployment tracking fields are rewritten |

**Do not edit core or managed region content directly.** If you need to customize behavior in an area covered by these regions, use a project or custom region instead.

### What Gets Preserved (Your Changes Survive)

| Content | What happens |
|---------|-------------|
| **Project regions** (`type="project"`) | Preserved byte-for-byte in their current position |
| **Custom regions** (`type="custom"`) | Preserved byte-for-byte in their current position |
| **Custom regions nested in managed regions** | Preserved — the managed parent regenerates around them |

### What Happens on Schema Reorder

A schema reorder is when MOSAIC changes the structural order of sections (rare). When this happens:

1. **Project regions** follow the source file's new position automatically — no action needed.
2. **Custom regions with a surviving parent** move with their parent — no action needed.
3. **Custom regions whose parent context no longer exists** are collected at the end of the file, and a TODO entry is emitted telling you to reposition them.

Your content is never deleted by a schema reorder. In the worst case, it's moved to the end of the file and you're told about it.

---

## Practical Examples

### Adding Codebase Knowledge

Fill the `CodebaseContext` project region that the catalog already declares:

```
<CodebaseContext type="project">
### Repository Structure
- `src/` — application source code (TypeScript)
- `tests/` — test files mirror `src/` structure
- `scripts/` — build and deployment scripts

### Tech Stack
- Runtime: Node.js 20
- Framework: Express 4
- Database: PostgreSQL 15 via Prisma ORM
- Testing: Vitest

### Conventions
- Feature branches off `main`, squash merge
- All public functions documented with JSDoc
</CodebaseContext>
```

### Adding Language-Specific Patterns

Create a custom region (no catalog declares this one — it's only meaningful once you have a language):

```
<LanguagePatterns type="custom">
### TypeScript Patterns
- Use `interface` for object shapes, `type` for unions and intersections
- Prefer `unknown` over `any`; narrow with type guards
- Use barrel exports (`index.ts`) only at package boundaries
- Errors are always `Error` subclasses, never thrown strings
</LanguagePatterns>
```

Place it inside the `Capabilities` core region, near `CodebaseContext`.

### Extending the Communication Protocol

If your deployment has a transport-layer need (e.g. subagents running behind network endpoints), use a custom `ProtocolExtension` as a **top-level sibling** of the managed protocol region — never nested inside it:

```
<CommunicationProtocol type="managed">
...
</CommunicationProtocol>

<ProtocolExtension type="custom">
### Transport
Messages are delivered via HTTP POST to the agent's endpoint.
Timeout: 120 seconds. Retry once on 5xx.
</ProtocolExtension>
```

**Important:** Extend mechanics (transport, delivery, environment handling). Do not restate or contradict what the protocol already defines (message shape, status codes, HITL gate). An extension that contradicts the protocol leaves the agent with two conflicting answers, and it will follow whichever instruction is closer in the prompt.

### Extending Error Handling

Nest a custom region inside the managed `ErrorHandlingCommon` to add project-specific recovery guidance:

```
<ErrorHandlingCommon type="managed">
- **Retry a transient error once** before escalating
...

<ProjectRecovery type="custom">
- If `prisma generate` fails, check that the database is running
- API rate limit errors (429) — wait 60 seconds and retry
</ProjectRecovery>
</ErrorHandlingCommon>
```

---

## The Deployment TODO File

After deployment or update, the tool generates a timestamped TODO file (e.g. `MOSAIC-DEPLOYMENT-TODO-<timestamp>.md`) listing items that need your attention:

- **Unfilled project regions** — slots you should fill to get the best performance from your agents
- **Project regions with default content** — regions pre-filled with a starting point; review and adjust for your project
- **Parked custom regions** — custom regions displaced by a schema reorder; move them to their correct section
- **Review notices** — other items the tool wants you to be aware of

The TODO file is a checklist, not an error report — but do not ignore project region entries. Each one represents context the agent was designed to use. Filling them is the single highest-impact thing you can do after deployment.

> **If you're using the MOSAIC catalog:** It ships an **Injections Helper** utility agent (`Catalog/UtilityAgents/injections-helper.md`) that can walk you through filling project and custom regions interactively. It knows the region rules, understands what each slot is for, and will insist on real project context before writing anything. Deploy it and point it at your workspace — it's the fastest way to get high-quality injection content into your agents.

---

## Rules of Thumb

1. **Check the `type` attribute** before editing anything. If it's `core` or `managed`, don't edit the content directly.

2. **Use project regions first.** Before inventing a custom region, check if the catalog already declares a project region for your purpose. Project regions track schema changes automatically; custom regions don't.

3. **Custom regions extend, they don't contradict.** A custom region that redefines something a managed region already states leaves the agent with two conflicting instructions. The agent tends to follow whichever instruction is closer in the prompt, which may not be yours.

4. **Fill project regions.** They exist because the agent benefits from the context. An agent with all project regions empty still runs, but it runs with less context than it was designed for. Filling them is the highest-leverage customization you can do.

5. **Name custom regions clearly.** Use PascalCase names that describe the content: `LanguagePatterns`, `ProjectRecovery`, `DeploymentContext`. The name appears in the tag and should be self-explanatory to anyone reading the file.

6. **One name per file.** Each region name can appear at most once in an agent file. Don't create two regions with the same name.

7. **Don't worry about updates.** Your project and custom content survives every update. The only scenario that requires action from you is a schema reorder displacing custom regions, and the tool tells you when that happens.

---

## Quick Reference

| I want to... | Do this |
|--------------|---------|
| Add codebase knowledge | Fill the `<CodebaseContext type="project">` region |
| Add language conventions | Create `<LanguagePatterns type="custom">` in the Capabilities section |
| Customize severity thresholds | Fill the `<SeverityThresholds type="project">` region (validation agents) |
| Extend error handling | Nest `<YourName type="custom">` inside `<ErrorHandlingCommon type="managed">` |
| Extend the protocol | Add `<ProtocolExtension type="custom">` as a sibling after `<CommunicationProtocol>` |
| Set context window limits | Fill the `<ContextLimits type="project">` region |
| Add any project-specific guidance | Create a `<YourName type="custom">` region in the appropriate section |
