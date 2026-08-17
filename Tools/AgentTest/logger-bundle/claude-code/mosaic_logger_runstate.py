"""mosaic_logger_runstate.py — Run identity extraction and agent mapping store.

Owns: run_id and agent_instance_id extraction from dispatch content,
agent_id -> agent_instance_id mapping files.
"""

import datetime
import json
import os
import random
import re
import sys

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
# Pending-dispatch state management
# ---------------------------------------------------------------------------

PENDING_DISPATCH_MAX_AGE_SECONDS = 120


def put_pending_dispatch(
    paths: "core.LogPaths",
    session_id: str,
    agent_instance_id: str,
    extracted_run_id: "str | None",
    prompt: "str | None" = None,
) -> bool:
    """Persist a pending dispatch entry for the given session.

    Appends a single JSON line to a per-session JSONL file keyed by
    session_id. Uses OS-appropriate locking to ensure concurrent writers
    do not overwrite each other:

    - Windows: opens the file for read+write (O_RDWR|O_CREAT), acquires
      an advisory byte-range lock on byte 0 via msvcrt.locking (LK_LOCK
      retries for up to 10 seconds), seeks to end, writes the encoded
      line, then releases the lock. Python's O_APPEND on Windows is
      implemented by the MSVCRT CRT with a non-atomic seek+write, so
      explicit locking is required.
    - POSIX: opens with O_WRONLY|O_CREAT|O_APPEND and uses a single
      os.write() call, which the kernel guarantees is atomic for writes
      under PIPE_BUF (~4 KB). JSON lines for these entries are well
      within that bound.

    Returns True on success. Never raises.
    """
    try:
        now = datetime.datetime.now(datetime.timezone.utc)
        entry = {
            "agent_instance_id": agent_instance_id,
            "run_id": extracted_run_id,
            "created_at": _iso_ms(now),
        }
        if prompt is not None:
            entry["prompt"] = prompt
        path = paths.pending_dispatch_entry(session_id)
        path.parent.mkdir(parents=True, exist_ok=True)
        data = (json.dumps(entry, ensure_ascii=False) + "\n").encode("utf-8")

        if sys.platform == "win32":
            import msvcrt
            fd = os.open(str(path), os.O_RDWR | os.O_CREAT, 0o666)
            try:
                os.lseek(fd, 0, 0)
                msvcrt.locking(fd, msvcrt.LK_LOCK, 1)
                try:
                    os.lseek(fd, 0, 2)
                    os.write(fd, data)
                finally:
                    os.lseek(fd, 0, 0)
                    msvcrt.locking(fd, msvcrt.LK_UNLCK, 1)
            finally:
                os.close(fd)
        else:
            fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o666)
            try:
                os.write(fd, data)
            finally:
                os.close(fd)

        return True
    except Exception:
        return False


def pop_pending_dispatch(
    paths: "core.LogPaths",
    session_id: str,
    max_age_seconds: int = PENDING_DISPATCH_MAX_AGE_SECONDS,
) -> "dict | None":
    """Pop and return the oldest non-stale pending dispatch for the given session.

    Reads the JSONL file, evicts leading stale entries (older than
    max_age_seconds), then pops the first remaining entry. Atomically
    replaces the file with the remaining lines. Returns None when the
    queue is empty, the file is absent, or any I/O/parse error occurs.
    Never raises.
    """
    try:
        path = paths.pending_dispatch_entry(session_id)
        if not path.exists():
            return None

        text = path.read_text(encoding="utf-8")
        raw_lines = [ln for ln in text.splitlines() if ln.strip()]

        if not raw_lines:
            return None

        now = datetime.datetime.now(datetime.timezone.utc)

        # Parse all lines -- any corrupt line causes the whole pop to fail safely.
        parsed = []
        for ln in raw_lines:
            try:
                parsed.append((ln, json.loads(ln)))
            except (json.JSONDecodeError, ValueError):
                return None

        # Evict leading stale entries.
        first_fresh = 0
        for i, (ln, entry) in enumerate(parsed):
            try:
                created_at = entry.get("created_at", "")
                ts = datetime.datetime.fromisoformat(
                    created_at.replace("Z", "+00:00")
                )
                age_seconds = (now - ts).total_seconds()
                if age_seconds > max_age_seconds:
                    first_fresh = i + 1
                    continue
            except Exception:
                # Unparseable timestamp -- treat as stale.
                first_fresh = i + 1
                continue
            break  # Found first non-stale entry.

        if first_fresh >= len(parsed):
            # All entries are stale -- write empty file.
            core.atomic_replace(path, b"")
            return None

        # Pop the first non-stale entry.
        popped_entry = parsed[first_fresh][1]
        remaining_lines = [ln for ln, _ in parsed[first_fresh + 1:]]

        remaining_text = "".join(ln + "\n" for ln in remaining_lines)
        core.atomic_replace(path, remaining_text.encode("utf-8"))

        return {
            "agent_instance_id": popped_entry.get("agent_instance_id"),
            "run_id": popped_entry.get("run_id"),
            "prompt": popped_entry.get("prompt"),
        }
    except Exception:
        return None


# ---------------------------------------------------------------------------
# Session-run binding store
# ---------------------------------------------------------------------------

def _tail_run_id_from_bytes(data: bytes) -> "str | None":
    """Return the run_id named by the last non-empty line of `data`, or None.

    Used to re-check the timeline's tail while holding the write lock, so
    the idempotence decision and the append happen atomically. A trailing
    line that fails to decode or parse is treated as unknown (returns
    None) rather than raising -- the caller then appends, which is the
    safe default. Never raises.
    """
    try:
        text = data.decode("utf-8")
    except Exception:
        return None
    lines = [ln for ln in text.splitlines() if ln.strip()]
    if not lines:
        return None
    try:
        entry = json.loads(lines[-1])
        if isinstance(entry, dict):
            return entry.get("run_id")
    except Exception:
        pass
    return None


def put_session_run_binding(
    paths: "core.LogPaths",
    session_id: "str | None",
    run_id: "str | None",
    start_time: "str | None" = None,
) -> bool:
    """Record run_id as the run active for session_id from start_time onward.

    Appends one timeline entry under the same platform-branched lock used
    for the read-check-write sequence, so the idempotence check below is
    atomic with the append: on Windows an msvcrt byte-range lock is held
    across the tail read and the seek-to-end write; on POSIX an advisory
    fcntl.flock is held across the same read-check-write sequence (a bare
    O_APPEND write is not enough here because the tail must be inspected
    under the lock, not just appended atomically).

    Idempotence is against the TAIL of the timeline, not the whole timeline:
    when the last entry already names run_id, nothing is appended and its
    recorded start_time is left unchanged. A run id that reappears after a
    different run has intervened (A, B, A) is appended again as a new entry.

    start_time defaults to now (UTC). Callers pass ctx.timestamp so the
    recorded instant is the firing's own instant, not the write's.

    Returns True when the timeline names run_id at its tail after the call,
    whether appended now or already present. Returns False on any failure.
    A falsy session_id or run_id is a no-op returning False.

    Never raises.
    """
    try:
        if not session_id or not run_id:
            return False

        now = datetime.datetime.now(datetime.timezone.utc)
        entry = {
            "run_id": run_id,
            "start_time": start_time if start_time else _iso_ms(now),
        }
        line = (json.dumps(entry, ensure_ascii=False) + "\n").encode("utf-8")
        path = paths.session_binding_entry(session_id)
        path.parent.mkdir(parents=True, exist_ok=True)

        if sys.platform == "win32":
            import msvcrt
            fd = os.open(str(path), os.O_RDWR | os.O_CREAT, 0o666)
            try:
                os.lseek(fd, 0, 0)
                msvcrt.locking(fd, msvcrt.LK_LOCK, 1)
                try:
                    os.lseek(fd, 0, 0)
                    existing = b"".join(iter(lambda: os.read(fd, 65536), b""))
                    if _tail_run_id_from_bytes(existing) == run_id:
                        return True
                    os.lseek(fd, 0, 2)
                    os.write(fd, line)
                finally:
                    os.lseek(fd, 0, 0)
                    msvcrt.locking(fd, msvcrt.LK_UNLCK, 1)
            finally:
                os.close(fd)
        else:
            import fcntl
            fd = os.open(str(path), os.O_RDWR | os.O_CREAT, 0o666)
            try:
                fcntl.flock(fd, fcntl.LOCK_EX)
                try:
                    os.lseek(fd, 0, 0)
                    existing = b"".join(iter(lambda: os.read(fd, 65536), b""))
                    if _tail_run_id_from_bytes(existing) == run_id:
                        return True
                    os.lseek(fd, 0, 2)
                    os.write(fd, line)
                finally:
                    fcntl.flock(fd, fcntl.LOCK_UN)
            finally:
                os.close(fd)

        return True
    except Exception as exc:
        core.debug_log(
            f"session-binding: write failed for session_id={session_id!r} "
            f"run_id={run_id!r}", exc,
        )
        return False


def read_session_run_timeline(
    paths: "core.LogPaths",
    session_id: "str | None",
) -> list:
    """Return the session's timeline in file order.

    Each element is a dict with keys 'run_id' (non-empty str) and
    'start_time' (non-empty str). Entries that are unparseable, non-object,
    missing either key, or carrying an unparseable timestamp are skipped
    with a diagnostic; the remaining entries are still returned. An absent,
    unreadable or empty file returns [].

    Never raises.
    """
    result = []
    try:
        if not session_id:
            return []
        path = paths.session_binding_entry(session_id)
        if not path.exists() or not path.is_file():
            return []
        text = path.read_text(encoding="utf-8")
        for line in text.splitlines():
            if not line.strip():
                continue
            try:
                entry = json.loads(line)
                if not isinstance(entry, dict):
                    core.debug_log(
                        f"session-binding: skipped corrupt entry (non-object) "
                        f"for session_id={session_id!r}"
                    )
                    continue
                run_id = entry.get("run_id")
                start_time = entry.get("start_time")
                if not run_id or not start_time:
                    core.debug_log(
                        f"session-binding: skipped corrupt entry (missing key) "
                        f"for session_id={session_id!r}"
                    )
                    continue
                # Validate the timestamp is parseable.
                datetime.datetime.fromisoformat(start_time.replace("Z", "+00:00"))
                result.append({"run_id": run_id, "start_time": start_time})
            except Exception:
                core.debug_log(
                    f"session-binding: skipped corrupt entry for "
                    f"session_id={session_id!r}"
                )
                continue
        return result
    except Exception as exc:
        core.debug_log(
            f"session-binding: read failed for session_id={session_id!r}",
            exc,
        )
        return []


def get_session_run_binding(
    paths: "core.LogPaths",
    session_id: "str | None",
    at: "str | None" = None,
) -> "str | None":
    """Resolve the run that was active for session_id at instant `at`.

    Returns the run_id of the entry with the greatest start_time that is
    <= at. Returns None when the timeline is empty, when every entry starts
    after `at` (the before-first-entry case), or on any failure. `at`
    defaults to now (UTC); callers pass ctx.timestamp.

    Emits a diagnostic on the unresolved outcome as a normal diagnostic
    path, not only on exceptions.

    Never raises.
    """
    try:
        now = datetime.datetime.now(datetime.timezone.utc)
        at_str = at if at else _iso_ms(now)
        try:
            at_dt = datetime.datetime.fromisoformat(at_str.replace("Z", "+00:00"))
        except Exception:
            core.debug_log(
                f"session-binding: unresolved (unparseable 'at') for "
                f"session_id={session_id!r} at={at_str!r}"
            )
            return None

        timeline = read_session_run_timeline(paths, session_id)
        best_run_id = None
        best_dt = None
        for entry in timeline:
            try:
                entry_dt = datetime.datetime.fromisoformat(
                    entry["start_time"].replace("Z", "+00:00")
                )
            except Exception:
                continue
            if entry_dt <= at_dt:
                if best_dt is None or entry_dt > best_dt:
                    best_dt = entry_dt
                    best_run_id = entry["run_id"]

        if best_run_id is None:
            core.debug_log(
                f"session-binding: unresolved for session_id={session_id!r} "
                f"at={at_str!r}"
            )
        return best_run_id
    except Exception as exc:
        core.debug_log(
            f"session-binding: unresolved for session_id={session_id!r}", exc
        )
        return None


def adopt_run_id(ctx: "core.HookContext", run_id: "str | None") -> bool:
    """The single reconciliation point between ctx.run_id and the binding.

    Sets ctx.run_id to run_id and records the binding for ctx.session_id at
    ctx.timestamp. Every site that newly learns a run id calls this rather
    than assigning ctx.run_id directly, so the in-context value and the
    persisted timeline can never disagree.

    A falsy run_id is a no-op returning False. Returns True when ctx.run_id
    is set to run_id, regardless of whether the persist succeeded.

    No outer try/except here: `ctx.run_id = run_id` is a plain attribute
    assignment that cannot raise, and put_session_run_binding already
    swallows its own exceptions and emits the documented
    "session-binding: write failed" diagnostic on persist failure.

    Never raises.
    """
    if not run_id:
        return False
    ctx.run_id = run_id
    put_session_run_binding(ctx.paths, ctx.session_id, run_id, ctx.timestamp)
    return True


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


def resolve_invocation(paths: "core.LogPaths",
                       run_id: str,
                       agent_id: str) -> tuple:
    """Return (agent_instance_id, mapped).

    mapped is True when an agent-map entry supplied the identity, False when
    the deterministic 'unmapped_<agent_id>' fallback was used. The False
    outcome emits an 'agent-map: miss' diagnostic naming run_id and agent_id.

    Never returns None for the identity. Never misattributes to the
    orchestrator. Never raises.
    """
    try:
        mapping = get_agent_mapping(paths, run_id, agent_id)
        if mapping is not None:
            iid = mapping.get("agent_instance_id")
            if iid:
                return iid, True
    except Exception:
        pass
    core.debug_log(
        f"agent-map: miss for run_id={run_id!r} agent_id={agent_id!r}"
    )
    return f"unmapped_{agent_id}", False


def resolve_invocation_id(paths: "core.LogPaths",
                          run_id: str,
                          agent_id: str) -> str:
    """Return the agent_instance_id for routing a subagent-scoped event.

    Unchanged signature and return value; a thin wrapper over
    resolve_invocation. Falls back to the deterministic
    'unmapped_{agent_id}' string when no mapping exists. Never returns
    None. Never misattributes to the orchestrator.
    """
    return resolve_invocation(paths, run_id, agent_id)[0]
