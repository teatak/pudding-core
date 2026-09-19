// Only server-defined reason codes are shown; no environment values or raw
// command output are retained by the local approval counters.
export const commandApprovalReasonKeys = {
  ask_mode: "commandApproval.reason.ask",
  dynamic_command: "commandApproval.reason.dynamic",
  sensitive_command: "commandApproval.reason.sensitive",
  download_and_execute: "commandApproval.reason.download",
  custom_environment: "commandApproval.reason.environment",
  destructive_command: "commandApproval.reason.destructive",
  host_execution: "commandApproval.reason.host",
  outside_project: "commandApproval.reason.outside",
} as const;

export function commandApprovalReasonKey(reason: string) {
  return Object.hasOwn(commandApprovalReasonKeys, reason)
    ? commandApprovalReasonKeys[reason as keyof typeof commandApprovalReasonKeys]
    : null;
}

export function commandApprovalReasonLabels(payload: unknown, t: (key: string) => string): string[] {
  if (!payload || typeof payload !== "object") return [];
  const data = payload as Record<string, unknown>;
  if (data.toolName !== "builtin_command_run" || !Array.isArray(data.approvalReasons)) return [];
  return [...new Set(data.approvalReasons.flatMap((reason) => {
    const key = typeof reason === "string" ? commandApprovalReasonKey(reason) : null;
    return key ? [t(key)] : [];
  }))];
}
