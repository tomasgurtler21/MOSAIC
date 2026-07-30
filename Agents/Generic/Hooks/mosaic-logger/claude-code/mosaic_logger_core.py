"""mosaic_logger_core.py — Core utilities for the mosaic-logger Claude Code hook adapter.

Owns: omit-absent event serializer, path layout, filename sanitization,
atomic replace, JSONL append, adapter version reading, debug sink.

Has no bundle dependencies (stdlib only) and must remain importable in isolation.
"""

import datetime
import json
import os
import pathlib
import re
import sys
import tempfile

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

SCHEMA_VERSION = "1.0.0"
HARNESS = "claude-code"
LOGS_DIRNAME = "OrchestrationLogs"
AGENT_MAP_DIRNAME = ".agent-map"
RESERVED_FILENAME_CHARS = '<>:"/\\|?*'
DEBUG_ENV_VAR = "MOSAIC_LOGGER_DEBUG"
WORKSPACE_ENV_VAR = "CLAUDE_PROJECT_DIR"

# ---------------------------------------------------------------------------
# Timestamp helpers (module-internal; used by core and callers)
# ---------------------------------------------------------------------------

def current_timestamp() -> str:
    """Return the current UTC time as ISO 8601 with ms precision and Z suffix."""
    now = datetime.datetime.now(datetime.timezone.utc)
    return _iso_ms(now)


def _iso_ms(dt: datetime.datetime) -> str:
    ms = dt.microsecond // 1000
    return dt.strftime("%Y-%m-%dT%H:%M:%S.") + f"{ms:03d}Z"


# ---------------------------------------------------------------------------
# HookContext — per-firing immutable context
# ---------------------------------------------------------------------------

class HookContext:
    """Immutable per-firing context constructed by the dispatcher.

    Handlers read it; they never mutate it, with the single exception of
    run_id, which the run-identity prelude sets before any handler runs.
    """

    def __init__(self, payload: dict, workspace_root: pathlib.Path,
                 paths: "LogPaths", timestamp: str):
        self.payload = payload
        self.event: str = payload.get("hook_event_name") or ""
        self.session_id: "str | None" = self._normalize(payload.get("session_id"))
        self.cwd: "str | None" = self._normalize(payload.get("cwd"))
        self.transcript_path: "str | None" = self._normalize(payload.get("transcript_path"))
        self.agent_id: "str | None" = self._normalize(payload.get("agent_id"))
        self.agent_type: "str | None" = self._normalize(payload.get("agent_type"))
        self.workspace_root: pathlib.Path = workspace_root
        self.paths: "LogPaths" = paths
        self.run_id: "str | None" = None
        self.timestamp: str = timestamp

    @staticmethod
    def _normalize(value):
        """Normalize absent/null/empty-string to None."""
        if value is None or value == "":
            return None
        return value

    def field(self, name: str):
        """Read a payload field, normalizing absent/null/empty-string to None."""
        return self._normalize(self.payload.get(name))


# ---------------------------------------------------------------------------
# Serialization — degrade-never-fabricate rule
# ---------------------------------------------------------------------------

def _prune(value):
    """Return (pruned_value, keep). Drops None, empty string, empty dict, empty list.
    Keeps False, 0, and any non-empty value. Recurses into dicts."""
    if value is None:
        return None, False
    if isinstance(value, bool):
        # False is a genuine resolved value; must be kept. Check before int.
        return value, True
    if isinstance(value, str) and value == "":
        return None, False
    if isinstance(value, dict):
        pruned = {}
        for k, v in value.items():
            pv, keep = _prune(v)
            if keep:
                pruned[k] = pv
        if not pruned:
            return None, False
        return pruned, True
    if isinstance(value, list) and len(value) == 0:
        return None, False
    return value, True


def build_event(event: str, ctx: "HookContext", **fields) -> dict:
    """Build a complete event object: common envelope plus caller-supplied fields.

    Envelope: schema_version, event, timestamp, harness, session_id (when
    resolvable), run_id (when resolvable).

    Every keyword whose value is None, empty-string, empty-dict, or empty-list
    is DROPPED. False and 0 are genuine values and are KEPT. Nested dicts are
    pruned recursively; a dict empty after pruning is itself dropped.
    """
    result: dict = {
        "schema_version": SCHEMA_VERSION,
        "event": event,
        "timestamp": ctx.timestamp,
        "harness": HARNESS,
    }
    if ctx.session_id:
        result["session_id"] = ctx.session_id
    if ctx.run_id:
        result["run_id"] = ctx.run_id

    for k, v in fields.items():
        pv, keep = _prune(v)
        if keep:
            result[k] = pv

    return result


# ---------------------------------------------------------------------------
# Effective run_id resolution
# ---------------------------------------------------------------------------

def effective_run_id(ctx: "HookContext") -> str:
    """Return ctx.run_id when present, or 'unknown-run' as the fallback bucket.

    This is the single decision point for run_id-to-path mapping. Handlers
    call this instead of guarding on ctx.run_id directly. The returned value
    is always a non-empty string suitable for use in path construction.

    Never raises.
    """
    if ctx.run_id:
        return ctx.run_id
    return "unknown-run"


# ---------------------------------------------------------------------------
# Write primitives
# ---------------------------------------------------------------------------

def append_event(path: pathlib.Path, event: dict) -> None:
    """Append one JSON object as one line to a JSONL file, creating parent dirs.

    Opens in append mode, writes one line, closes. No size cap. Swallows errors.
    """
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        line = json.dumps(event, ensure_ascii=False) + "\n"
        with open(path, "a", encoding="utf-8") as fh:
            fh.write(line)
    except Exception as exc:
        debug_log(f"append_event failed for {path}", exc)


def atomic_replace(path: pathlib.Path, data: bytes) -> bool:
    """Write data to a temp file in path's directory, then os.replace over path.

    Returns True on success, False on any failure. Never raises.
    The temp file stays in the same directory so the rename is within one filesystem.
    The target directory must already exist; this function does not create directories.
    """
    try:
        fd, tmp_str = tempfile.mkstemp(dir=path.parent)
        tmp_path = pathlib.Path(tmp_str)
        try:
            with os.fdopen(fd, "wb") as fh:
                fh.write(data)
            os.replace(tmp_path, path)
            return True
        except Exception:
            try:
                tmp_path.unlink()
            except Exception:
                pass
            raise
    except Exception as exc:
        debug_log(f"atomic_replace failed for {path}", exc)
        return False


def atomic_replace_text(path: pathlib.Path, text: str) -> bool:
    """UTF-8 convenience wrapper over atomic_replace."""
    return atomic_replace(path, text.encode("utf-8"))


# ---------------------------------------------------------------------------
# Path layout
# ---------------------------------------------------------------------------

def resolve_workspace_root(payload: dict) -> pathlib.Path:
    """Resolve workspace root: CLAUDE_PROJECT_DIR > payload cwd > process cwd."""
    env_val = os.environ.get(WORKSPACE_ENV_VAR, "")
    if env_val:
        return pathlib.Path(env_val)
    cwd = payload.get("cwd") or ""
    if cwd:
        return pathlib.Path(cwd)
    return pathlib.Path.cwd()


class LogPaths:
    """The complete on-disk layout expressed as one object.

    No path is ever concatenated at a call site; a layout change touches only
    this class.
    """

    def __init__(self, workspace_root: pathlib.Path):
        self.root: pathlib.Path = workspace_root / LOGS_DIRNAME

    def run_root(self, run_id: str) -> pathlib.Path:
        return self.root / run_id

    def agent_map_dir(self, run_id: str) -> pathlib.Path:
        return self.run_root(run_id) / AGENT_MAP_DIRNAME

    def agent_map_entry(self, run_id: str, agent_id: str) -> pathlib.Path:
        return self.agent_map_dir(run_id) / f"{sanitize_component(agent_id)}.json"

    def orchestrator_events(self, run_id: str) -> pathlib.Path:
        return self.run_root(run_id) / "00_orchestrator_events.jsonl"

    def orchestrator_raw(self, run_id: str) -> pathlib.Path:
        return self.run_root(run_id) / "00_orchestrator_session.raw"

    def invocation_dir(self, run_id: str, agent_instance_id: str) -> pathlib.Path:
        return self.run_root(run_id) / sanitize_component(agent_instance_id)

    def invocation_events(self, run_id: str, agent_instance_id: str) -> pathlib.Path:
        return self.invocation_dir(run_id, agent_instance_id) / "03_events.jsonl"

    def invocation_input(self, run_id: str, agent_instance_id: str) -> pathlib.Path:
        return self.invocation_dir(run_id, agent_instance_id) / "01_input.md"

    def invocation_output(self, run_id: str, agent_instance_id: str) -> pathlib.Path:
        return self.invocation_dir(run_id, agent_instance_id) / "02_output.md"

    def invocation_raw(self, run_id: str, agent_instance_id: str) -> pathlib.Path:
        return self.invocation_dir(run_id, agent_instance_id) / "04_session.raw"


def build_paths(workspace_root: pathlib.Path) -> LogPaths:
    """Construct the LogPaths tree. Creates no directories."""
    return LogPaths(workspace_root)


# ---------------------------------------------------------------------------
# Filename sanitization
# ---------------------------------------------------------------------------

def sanitize_component(name: str) -> str:
    """Sanitize a string for use as a single filesystem path component.

    - Replaces each of <>:"/\\|?* with _.
    - Replaces control characters (< 0x20) with _.
    - Strips trailing dots and spaces.
    - Returns '_' for an empty or fully-stripped result.
    Deterministic and total; not reversible.
    """
    if not name:
        return "_"
    result = []
    for ch in name:
        if ch in RESERVED_FILENAME_CHARS or ord(ch) < 0x20:
            result.append("_")
        else:
            result.append(ch)
    sanitized = "".join(result).rstrip(". ")
    return sanitized if sanitized else "_"


# ---------------------------------------------------------------------------
# Event sink routing
# ---------------------------------------------------------------------------

def event_sink_for(ctx: "HookContext", agent_instance_id: "str | None") -> pathlib.Path:
    """The single decision point for where an event line is written.

    agent_instance_id is None  -> orchestrator events file
    otherwise                  -> invocation events file
    """
    run_id = effective_run_id(ctx)
    if agent_instance_id is None:
        return ctx.paths.orchestrator_events(run_id)
    return ctx.paths.invocation_events(run_id, agent_instance_id)


# ---------------------------------------------------------------------------
# Adapter version
# ---------------------------------------------------------------------------

_VERSION_RE = re.compile(r"^version:\s*(.+?)\s*$")


def read_adapter_version(hook_yaml_path: pathlib.Path) -> "str | None":
    """Extract the top-level version value from hook.yaml without a YAML parser.

    Matches only zero-indented lines. Returns None on any ambiguity. Never raises.
    """
    try:
        matches = []
        with open(hook_yaml_path, "r", encoding="utf-8", errors="replace") as fh:
            for line in fh:
                # Skip blank lines, comments, and indented lines
                if not line or line[0] in ("#", " ", "\t"):
                    continue
                m = _VERSION_RE.match(line.rstrip("\r\n"))
                if m:
                    value = m.group(1)
                    # Strip one layer of surrounding single or double quotes
                    if len(value) >= 2 and (
                        (value[0] == '"' and value[-1] == '"') or
                        (value[0] == "'" and value[-1] == "'")
                    ):
                        value = value[1:-1]
                    if value:
                        matches.append(value)
        if len(matches) == 1:
            return matches[0]
        return None
    except Exception:
        return None


def adapter_version(ctx: "HookContext") -> "str | None":
    """Read version from the hook.yaml deployed alongside the adapter scripts."""
    hook_yaml = pathlib.Path(__file__).parent / "hook.yaml"
    return read_adapter_version(hook_yaml)


# ---------------------------------------------------------------------------
# Diagnostics
# ---------------------------------------------------------------------------

def debug_log(message: str, exc: "BaseException | None" = None) -> None:
    """Opt-in diagnostic sink. Writes to the file named by MOSAIC_LOGGER_DEBUG.
    No-op when that variable is unset or empty. Never raises, never writes stdout/stderr.
    """
    try:
        debug_path = os.environ.get(DEBUG_ENV_VAR, "")
        if not debug_path:
            return
        import traceback
        now = datetime.datetime.now(datetime.timezone.utc)
        ts = _iso_ms(now)
        with open(debug_path, "a", encoding="utf-8") as fh:
            fh.write(f"{ts} {message}\n")
            if exc is not None:
                traceback.print_exc(file=fh)
    except Exception:
        pass
