"""mosaic_logger_runstate.py — Run marker state machine and agent mapping store.

Owns: per-session run marker (open/closed), run_id and instance_id minting,
agent_id -> agent_instance_id mapping files.
"""

import datetime
import json
import random
import re

import mosaic_logger_core as core


# ---------------------------------------------------------------------------
# RunResolution
# ---------------------------------------------------------------------------

class RunResolution:
    """Outcome of a resolve_run or close_run call.

    outcome: "minted" | "reused" | "closed" | "unresolved"
    run_id:  the resolved identifier, or None when outcome is "unresolved"
    opened_at: marker opened_at string, or None
    """

    def __init__(self, run_id: "str | None", outcome: str,
                 opened_at: "str | None" = None):
        self.run_id = run_id
        self.outcome = outcome
        self.opened_at = opened_at


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
# Run-marker state machine
# ---------------------------------------------------------------------------

def _parse_opened_at(s: "str | None") -> "datetime.datetime | None":
    """Parse an ISO 8601 Z-suffix string to a timezone-aware datetime."""
    if not s:
        return None
    try:
        return datetime.datetime.fromisoformat(s.replace("Z", "+00:00"))
    except Exception:
        return None


def _read_marker(paths: "core.LogPaths",
                 session_id: str) -> "dict | None":
    """Read and parse the marker file. Returns None if absent, unreadable, or malformed."""
    try:
        path = paths.marker(session_id)
        text = path.read_text(encoding="utf-8")
        data = json.loads(text)
        if not isinstance(data, dict):
            return None
        return data
    except Exception:
        return None


def _write_marker(paths: "core.LogPaths", session_id: str, marker: dict) -> None:
    """Write the marker via atomic replace, creating the markers directory if needed."""
    path = paths.marker(session_id)
    path.parent.mkdir(parents=True, exist_ok=True)
    core.atomic_replace(path, json.dumps(marker, ensure_ascii=False).encode("utf-8"))


def resolve_run(paths: "core.LogPaths",
                session_id: "str | None",
                allow_mint: bool,
                now: "datetime.datetime | None" = None) -> RunResolution:
    """Read-check-write the per-session run marker and return the resolved identity.

    Branches (evaluated in order):
      absent/unreadable/malformed + allow_mint  -> MINTED
      status "open" and opened_at within 24h    -> REUSED (any allow_mint)
      status "closed" + allow_mint              -> MINTED (resume)
      opened_at older than 24h + allow_mint     -> MINTED (stale)
      any non-reusable state + !allow_mint      -> UNRESOLVED
      missing/empty session_id                  -> UNRESOLVED
    Never raises.
    """
    try:
        if not session_id:
            return RunResolution(run_id=None, outcome="unresolved")

        now_dt = _utcnow(now)
        marker = _read_marker(paths, session_id)

        if marker is None:
            # Absent or unreadable
            if allow_mint:
                return _mint(paths, session_id, now_dt)
            return RunResolution(run_id=None, outcome="unresolved")

        run_id = marker.get("run_id")
        status = marker.get("status")
        opened_at_str = marker.get("opened_at")
        opened_at_dt = _parse_opened_at(opened_at_str)

        # Check freshness first — stale trumps both open and closed
        if opened_at_dt is not None:
            age_seconds = (now_dt - opened_at_dt).total_seconds()
            is_stale = age_seconds > core.STALE_AFTER_SECONDS
        else:
            # Unparseable opened_at: treat as stale
            is_stale = True

        # REUSED: open and fresh
        if status == "open" and not is_stale:
            return RunResolution(run_id=run_id, outcome="reused",
                                 opened_at=opened_at_str)

        # MINTED: closed and not stale (resume-for-a-new-run)
        if status == "closed" and not is_stale:
            if allow_mint:
                return _mint(paths, session_id, now_dt)
            return RunResolution(run_id=None, outcome="unresolved")

        # MINTED: stale (any status)
        if is_stale:
            if allow_mint:
                return _mint(paths, session_id, now_dt)
            return RunResolution(run_id=None, outcome="unresolved")

        # Fallback — any other non-reusable state
        if allow_mint:
            return _mint(paths, session_id, now_dt)
        return RunResolution(run_id=None, outcome="unresolved")

    except Exception:
        return RunResolution(run_id=None, outcome="unresolved")


def _mint(paths: "core.LogPaths", session_id: str,
          now_dt: datetime.datetime) -> RunResolution:
    """Mint a new run_id and write the marker with status 'open'."""
    run_id = new_run_id(now=now_dt)
    opened_at = _iso_ms(now_dt)
    marker = {"run_id": run_id, "opened_at": opened_at, "status": "open"}
    _write_marker(paths, session_id, marker)
    return RunResolution(run_id=run_id, outcome="minted", opened_at=opened_at)


def close_run(paths: "core.LogPaths",
              session_id: "str | None",
              now: "datetime.datetime | None" = None) -> RunResolution:
    """Set the existing marker's status to 'closed', preserving run_id and opened_at.

    Returns CLOSED with the run_id, or UNRESOLVED when no marker exists.
    Never raises.
    """
    try:
        if not session_id:
            return RunResolution(run_id=None, outcome="unresolved")

        marker = _read_marker(paths, session_id)
        if marker is None:
            return RunResolution(run_id=None, outcome="unresolved")

        run_id = marker.get("run_id")
        opened_at = marker.get("opened_at")

        updated = {"run_id": run_id, "opened_at": opened_at, "status": "closed"}
        _write_marker(paths, session_id, updated)

        return RunResolution(run_id=run_id, outcome="closed",
                             opened_at=opened_at)
    except Exception:
        return RunResolution(run_id=None, outcome="unresolved")


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
