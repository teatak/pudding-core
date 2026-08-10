export type AgentConsoleResizePhase = "start" | "end";

const AGENT_CONSOLE_RESIZE_EVENT = "pudding:agent-console-resize";

export function emitAgentConsoleResizePhase(phase: AgentConsoleResizePhase) {
  window.dispatchEvent(new CustomEvent<AgentConsoleResizePhase>(AGENT_CONSOLE_RESIZE_EVENT, { detail: phase }));
}

export function onAgentConsoleResizePhase(listener: (phase: AgentConsoleResizePhase) => void) {
  const handleResizePhase = (event: Event) => {
    listener((event as CustomEvent<AgentConsoleResizePhase>).detail);
  };
  window.addEventListener(AGENT_CONSOLE_RESIZE_EVENT, handleResizePhase);
  return () => window.removeEventListener(AGENT_CONSOLE_RESIZE_EVENT, handleResizePhase);
}
