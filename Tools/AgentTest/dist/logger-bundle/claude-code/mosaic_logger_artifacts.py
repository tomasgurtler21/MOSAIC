"""mosaic_logger_artifacts.py — Human-browsable markdown artifact rendering.

Owns: rendering 01_input.md and 02_output.md for each subagent invocation.
Returns markdown text and performs no I/O; callers write the result via core.

Implementation: see ContractsDesign.md section "mosaic_logger_artifacts".
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
    """Render the 01_input.md body: metadata table followed by raw prompt content.

    Returns markdown text. Performs no I/O.
    Rows with unresolvable (None) values are omitted from the table entirely.
    """
    lines = []
    lines.append(f"# Invocation Input: {agent_instance_id}\n\n")

    # Metadata table header
    lines.append("| Field | Value |\n")
    lines.append("|---|---|\n")

    # Rows — omit when value is None
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
    """Render the 02_output.md body: metadata table followed by raw response content.

    Returns markdown text. Performs no I/O.
    Rows with unresolvable (None) values are omitted from the table entirely.
    `facts` is a TurnFacts-compatible object with .model and .token_usage attributes.
    """
    lines = []
    lines.append(f"# Invocation Output: {agent_instance_id}\n\n")

    # Metadata table header
    lines.append("| Field | Value |\n")
    lines.append("|---|---|\n")

    # Rows — omit when value is None
    lines.append(_row("Agent Instance ID", agent_instance_id))
    lines.append(_row("Agent Type", ctx.agent_type))
    lines.append(_row("Session ID", ctx.session_id))
    lines.append(_row("Run ID", ctx.run_id))
    lines.append(_row("Harness", core.HARNESS))
    lines.append(_row("Captured At", ctx.timestamp))
    lines.append(_row("Status Code", status_code))

    model = getattr(facts, "model", None)
    lines.append(_row("Model", model))

    # Token usage rows — each independently optional; omitted when value absent
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
    """Write a markdown artifact via core.atomic_replace_text.

    Creates parent directories as needed. Returns True on success. Never raises.
    """
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        return core.atomic_replace_text(path, text)
    except Exception:
        return False
