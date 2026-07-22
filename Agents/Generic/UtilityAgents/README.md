# Utility Agents

This directory contains utility agents for creating and maintaining the multi-agent orchestration system:

| Agent | Purpose |
|-------|---------|
| `anthropic-agent-creator` | Creates general-purpose AI agent instructions through iterative collaboration |
| `anthropic-subagent-creator` | Creates subagent instructions compliant with the orchestration system architecture |
| `workflow-creator` | Creates and modifies orchestration workflow definitions |
| `harness-bug-hunter` | Discovers, validates, and maintains a knowledge base of harness bugs and workarounds |
| `system-prompt-capturer` | Captures and maintains harness-injected system prompts, built-in tool definitions, and tool output formats |

---

## ⚠️ Important: Follow the Agent's Guidance for Production-Quality Output

These agents are designed to produce **production-grade results** — but only when you actively engage with their process. Each agent is built around iterative collaboration: it asks targeted questions, presents drafts for your review, flags concerns, and waits for your approval before proceeding. That process is not optional ceremony — it is what makes the output reliable.

**If you skip or rush through the agent's review steps, output quality degrades significantly.**

### What happens without proper engagement

**Suboptimal quality** is the typical outcome when guidance is bypassed:
- Instructions tend to be verbose where brevity would serve better, or vague where precision is needed
- Process steps may be over-engineered or miss practical nuance
- Injection points and harness-specific adaptations require judgment that only you can provide

**Incorrect behavior** can occur in worse cases:
- Goal–instruction misalignment: the agent's instructions describe a process that doesn't reliably achieve the stated goal
- Scope creep or scope gaps: the agent takes on too much or leaves critical responsibilities undefined
- Constraint conflicts: rules that contradict each other or undermine the agent's ability to function
- Protocol drift: subagent instructions that don't correctly comply with the orchestration communication protocol, causing integration failures at runtime

### How to get production-quality output

These agents actively seek your input at each stage. Engage genuinely:

1. **Answer the elicitation questions carefully** — the agent uses your answers to align the instructions to your actual goal; vague answers produce vague output
2. **Review each draft critically** — read it as if you were the AI that will execute it; ask "would I know what to do in every situation?"
3. **Approve or reject explicitly** — the agent waits for your sign-off before finalising; use this to catch problems early
4. **Test with simple cases** before integrating into production workflows
5. **Use the revision loop** — the agents support iteration through conversation; if something feels off, say so

### What these agents do well

- Structuring agent instructions following established patterns
- Eliciting requirements through targeted questions
- Ensuring compliance with the orchestration template format
- Catching obvious logical gaps during self-review
- Producing consistent, readable output fast

The agents handle the structural and templating work; you provide the domain knowledge and final judgment. Together, that produces production-ready results.
