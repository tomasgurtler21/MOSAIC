/**
 * runner_mode.ts — Runner-mode resolution from the process environment.
 *
 * Runner mode is selected ONLY by MOSAIC_ROLE (set by the Runner). Mode is
 * never inferred from prompt content. Without MOSAIC_ROLE the plugin is in
 * native mode and behaves exactly as it always has.
 */

export const ROLE_ENV_VAR = "MOSAIC_ROLE";
export const RUN_ID_ENV_VAR = "MOSAIC_RUN_ID";
export const AGENT_INSTANCE_ENV_VAR = "MOSAIC_AGENT_INSTANCE_ID";

export type RunnerRole = "subagent" | "orchestrator";

export interface RunnerMode {
  role: RunnerRole;
  runId?: string;
  agentInstanceId?: string;
  agentType?: string;
}

const RUN_ID_FORMAT = /^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$/;

/** undefined when MOSAIC_ROLE is absent or empty (native). env defaults to process.env. Never throws. */
export function readRunnerMode(
  env?: Record<string, string | undefined>,
): RunnerMode | undefined {
  try {
    const source = env ?? process.env;
    const rawRole = (source[ROLE_ENV_VAR] ?? "").trim().toLowerCase();
    if (rawRole === "") return undefined;

    const mode: RunnerMode = { role: "orchestrator" };

    const rawRunId = source[RUN_ID_ENV_VAR];
    if (typeof rawRunId === "string" && RUN_ID_FORMAT.test(rawRunId)) {
      mode.runId = rawRunId;
    }

    const instanceId = source[AGENT_INSTANCE_ENV_VAR];
    if (rawRole === "subagent" && typeof instanceId === "string" && instanceId !== "") {
      mode.role = "subagent";
      mode.agentInstanceId = instanceId;
      const hash = instanceId.lastIndexOf("#");
      if (hash > 0) mode.agentType = instanceId.slice(0, hash);
    }
    return mode;
  } catch {
    return undefined;
  }
}
