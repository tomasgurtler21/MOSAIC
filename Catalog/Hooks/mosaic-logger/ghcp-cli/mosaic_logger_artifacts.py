"""mosaic_logger_artifacts.py -- Human-browsable markdown artifact rendering.

Returns markdown text and performs no I/O; callers write the result via core.
"""

import pathlib

import mosaic_logger_core as core


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _row(label: str, value: "str | None") -> str:
    """Return a markdown table row, or empty string when value is absent."""
    if value is None:
        return ""
    return f"| {label} | {value} |\n"


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

def render_input(ctx: "core.HookContext",
                 agent_instance_id: str,
                 prompt: "str | None") -> str:
    """Render 01_input.md: metadata table + raw prompt content.
    Rows with None values omitted. Performs no I/O."""
    lines = []
    lines.append(f"# Invocation Input: {agent_instance_id}\n\n")

    lines.append("| Field | Value |\n")
    lines.append("|---|---|\n")

    lines.append(_row("Agent Instance ID", agent_instance_id))
    lines.append(_row("Agent Type", ctx.agent_type))
    lines.append(_row("Session ID", ctx.session_id))
    lines.append(_row("Run ID", ctx.run_id))
    lines.append(_row("Harness", core.HARNESS))
    lines.append(_row("Captured At", ctx.timestamp))

    if prompt is not None:
        lines.append("\n## Prompt\n\n")
        lines.append(prompt)
        lines.append("\n")

    return "".join(lines)


def render_output(ctx: "core.HookContext",
                  agent_instance_id: str,
                  response: "str | None",
                  status_code: "str | None",
                  facts) -> str:
    """Render 02_output.md: metadata table + raw response content.
    facts is a TurnFacts-compatible object. Performs no I/O."""
    lines = []
    lines.append(f"# Invocation Output: {agent_instance_id}\n\n")

    lines.append("| Field | Value |\n")
    lines.append("|---|---|\n")

    lines.append(_row("Agent Instance ID", agent_instance_id))
    lines.append(_row("Agent Type", ctx.agent_type))
    lines.append(_row("Session ID", ctx.session_id))
    lines.append(_row("Run ID", ctx.run_id))
    lines.append(_row("Harness", core.HARNESS))
    lines.append(_row("Captured At", ctx.timestamp))
    lines.append(_row("Status Code", status_code))

    model = getattr(facts, "model", None)
    lines.append(_row("Model", model))

    token_usage = getattr(facts, "token_usage", None)
    tu = token_usage or {}
    lines.append(_row("Input Tokens", str(tu["input_tokens"]) if "input_tokens" in tu else None))
    lines.append(_row("Output Tokens", str(tu["output_tokens"]) if "output_tokens" in tu else None))
    lines.append(_row("Cache Read Tokens", str(tu["cache_read_tokens"]) if "cache_read_tokens" in tu else None))
    lines.append(_row("Cache Creation Tokens", str(tu["cache_creation_tokens"]) if "cache_creation_tokens" in tu else None))

    if response is not None:
        lines.append("\n## Response\n\n")
        lines.append(response)
        lines.append("\n")

    return "".join(lines)


def write_artifact(path: pathlib.Path, text: str) -> bool:
    """Write markdown artifact via core.atomic_replace_text.
    Creates parent dirs. Returns True on success. Never raises."""
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        return core.atomic_replace_text(path, text)
    except Exception:
        return False
