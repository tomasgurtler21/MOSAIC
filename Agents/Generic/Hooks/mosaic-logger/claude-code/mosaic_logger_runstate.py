"""mosaic_logger_runstate.py — Run identity extraction and agent mapping store.

Owns: run_id and agent_instance_id extraction from dispatch content,
agent_id -> agent_instance_id mapping files.
"""

import datetime
import json
import random
import re

import mosaic_logger_core as core


# ---------------------------------------------------------------------------
# Identifier generation helpers
# ---------------------------------------------------------------------------

def _random_suffix() -> str:
    return "".join(random.choices("0123456789abcdef", k=4))


def _compact_ts(dt: datetime.datetime) -> str:
    """Compact UTC timestamp: YYYYMMDDTHHMMSSz."""
    return dt.strftime("%Y%m%dT%H%M%SZ")


def _iso_ms(dt: datetime.datetime) -> str:
    """ISO 8601 UTC with ms precision and Z suffix."""
    ms = dt.microsecond // 1000
    return dt.strftime("%Y-%m-%dT%H:%M:%S.") + f"{ms:03d}Z"


def _utcnow(now: "datetime.datetime | None") -> datetime.datetime:
    if now is None:
        return datetime.datetime.now(datetime.timezone.utc)
    return now


# ---------------------------------------------------------------------------
# Identifier minting
# ---------------------------------------------------------------------------

def new_run_id(now: "datetime.datetime | None" = None) -> str:
    """Return {YYYYMMDD}T{HHMMSS}Z-{4 lowercase hex chars}."""
    return f"{_compact_ts(_utcnow(now))}-{_random_suffix()}"


def fallback_instance_id(prefix: "str | None",
                         now: "datetime.datetime | None" = None) -> str:
    """Return {prefix}_{YYYYMMDD}T{HHMMSS}Z-{4 hex chars}.
    Uses literal 'agent' when prefix is None."""
    p = prefix if prefix else "agent"
    return f"{p}_{_compact_ts(_utcnow(now))}-{_random_suffix()}"


def fallback_call_id(tool_name: "str | None",
                     now: "datetime.datetime | None" = None) -> str:
    """Return {tool_name or 'tool'}_{YYYYMMDD}T{HHMMSS}Z-{4 hex chars}."""
    p = tool_name if tool_name else "tool"
    return f"{p}_{_compact_ts(_utcnow(now))}-{_random_suffix()}"


# ---------------------------------------------------------------------------
# agent_instance_id extraction
# ---------------------------------------------------------------------------

# Structured match: agent_instance_id key (with optional quotes/colon) followed
# by a quoted or bare {Name}#{Number} value.
_STRUCTURED_RE = re.compile(
    r'(?<![A-Za-z0-9_])agent_instance_id\s*["\']?\s*[:=]\s*["\']?\s*'
    r'([A-Za-z][A-Za-z0-9_.-]*#[0-9]+)'
)

# Bare match: {Name}#{Number} not immediately preceded by an alnum/.-_ character.
_BARE_RE = re.compile(r'(?<![A-Za-z0-9_.-])([A-Za-z][A-Za-z0-9_.-]*#[0-9]+)')


def extract_instance_id(prompt: "str | None") -> "str | None":
    """Extract a MOSAIC {AgentName}#{Number} instance ID from dispatched prompt text.

    Matching order (stops at first hit):
      1. Structured: agent_instance_id key followed by the value (normal dispatch case).
      2. Bare: earliest {Name}#{Number} occurrence in the text.
    Returns None when no confident match exists. Never raises.
    """
    if not prompt:
        return None
    try:
        m = _STRUCTURED_RE.search(prompt)
        if m:
            return m.group(1)
        m = _BARE_RE.search(prompt)
        if m:
            return m.group(1)
        return None
    except Exception:
        return None


# ---------------------------------------------------------------------------
# run_id extraction
# ---------------------------------------------------------------------------

# Structured match: run_id key (with optional quotes/colon/equals) followed
# by a value matching the {YYYYMMDD}T{HHMMSS}Z-{4hex} format.
_RUN_ID_STRUCTURED_RE = re.compile(
    r'(?<![A-Za-z0-9_])run_id\s*["\']?\s*[:=]\s*["\']?\s*'
    r'([0-9]{8}T[0-9]{6}Z-[0-9a-f]{4})'
)

# Bare match: {YYYYMMDD}T{HHMMSS}Z-{4hex} not immediately preceded by alnum/.-_
_RUN_ID_BARE_RE = re.compile(
    r'(?<![A-Za-z0-9_.-])([0-9]{8}T[0-9]{6}Z-[0-9a-f]{4})'
)


def extract_run_id(prompt: "str | None") -> "str | None":
    """Extract a MOSAIC run_id from dispatched prompt text.

    Matching order (stops at first hit):
      1. Structured: run_id key followed by a value matching the format
         {YYYYMMDD}T{HHMMSS}Z-{4 lowercase hex chars}.
      2. Bare: earliest occurrence of the format pattern in the text.
    Returns None when no confident match exists. Never raises.
    """
    if not prompt:
        return None
    try:
        m = _RUN_ID_STRUCTURED_RE.search(prompt)
        if m:
            return m.group(1)
        m = _RUN_ID_BARE_RE.search(prompt)
        if m:
            return m.group(1)
        return None
    except Exception:
        return None


# ---------------------------------------------------------------------------
# agent_id -> agent_instance_id mapping
# ---------------------------------------------------------------------------

def put_agent_mapping(paths: "core.LogPaths",
                      run_id: str,
                      agent_id: str,
                      agent_instance_id: str,
                      agent_type: "str | None") -> bool:
    """Persist the agent_id -> agent_instance_id mapping via atomic replace.

    One file per agent_id; parallel SubagentStart processes never contend.
    Returns True on success. Never raises.
    """
    try:
        now = datetime.datetime.now(datetime.timezone.utc)
        data = {
            "agent_id": agent_id,
            "agent_instance_id": agent_instance_id,
            "agent_type": agent_type,
            "created_at": _iso_ms(now),
        }
        path = paths.agent_map_entry(run_id, agent_id)
        path.parent.mkdir(parents=True, exist_ok=True)
        return core.atomic_replace(
            path, json.dumps(data, ensure_ascii=False).encode("utf-8")
        )
    except Exception:
        return False


def get_agent_mapping(paths: "core.LogPaths",
                      run_id: str,
                      agent_id: str) -> "dict | None":
    """Read one mapping. Returns None when absent, unreadable, or malformed.
    Never raises."""
    try:
        path = paths.agent_map_entry(run_id, agent_id)
        text = path.read_text(encoding="utf-8")
        data = json.loads(text)
        if isinstance(data, dict):
            return data
        return None
    except Exception:
        return None


def resolve_invocation_id(paths: "core.LogPaths",
                          run_id: str,
                          agent_id: str) -> str:
    """Return the agent_instance_id for routing a subagent-scoped event.

    Falls back to the deterministic 'unmapped_{agent_id}' string when no
    mapping exists. Never returns None. Never misattributes to the orchestrator.
    """
    mapping = get_agent_mapping(paths, run_id, agent_id)
    if mapping is not None:
        iid = mapping.get("agent_instance_id")
        if iid:
            return iid
    return f"unmapped_{agent_id}"
