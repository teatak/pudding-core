import { create } from "zustand";

export type InputFlowResult = {
  ok: boolean;
  reason?: string;
  requestID: string;
  result?: Record<string, unknown>;
  sessionID: string;
  status: "completed" | "cancelled";
  title: string;
};

export type InputFlowRequest = {
  args: Record<string, unknown>;
  createdAt: string;
  id: string;
  sessionID: string;
  title: string;
};

type InputFlowState = {
  requests: InputFlowRequest[];
  addRequest: (request: InputFlowRequest) => void;
  removeRequest: (id: string) => void;
};

const resolvers = new Map<string, (result: InputFlowResult) => void>();

export const useInputFlowStore = create<InputFlowState>((set) => ({
  requests: [],
  addRequest: (request) =>
    set((state) => ({
      requests: [...state.requests.filter((item) => item.sessionID !== request.sessionID), request],
    })),
  removeRequest: (id) => set((state) => ({ requests: state.requests.filter((item) => item.id !== id) })),
}));

export function waitForInputFlow(input: {
  args: Record<string, unknown>;
  sessionID: string;
  title: string;
}): Promise<InputFlowResult> {
  const request: InputFlowRequest = {
    args: input.args,
    createdAt: new Date().toISOString(),
    id: newRequestID(),
    sessionID: input.sessionID,
    title: input.title,
  };
  cancelSessionInputFlows(input.sessionID, "superseded");
  useInputFlowStore.getState().addRequest(request);
  return new Promise((resolve) => {
    resolvers.set(request.id, resolve);
  });
}

export function completeInputFlow(request: InputFlowRequest, result: Record<string, unknown>) {
  resolveInputFlow(request, {
    ok: true,
    requestID: request.id,
    result,
    sessionID: request.sessionID,
    status: "completed",
    title: request.title,
  });
}

export function cancelInputFlow(request: InputFlowRequest, reason = "user_cancelled") {
  resolveInputFlow(request, {
    ok: false,
    reason,
    requestID: request.id,
    sessionID: request.sessionID,
    status: "cancelled",
    title: request.title,
  });
}

function cancelSessionInputFlows(sessionID: string, reason: string) {
  const existing = useInputFlowStore.getState().requests.filter((request) => request.sessionID === sessionID);
  for (const request of existing) {
    cancelInputFlow(request, reason);
  }
}

function resolveInputFlow(request: InputFlowRequest, result: InputFlowResult) {
  useInputFlowStore.getState().removeRequest(request.id);
  const resolve = resolvers.get(request.id);
  resolvers.delete(request.id);
  resolve?.(result);
}

function newRequestID() {
  if (typeof crypto?.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `input-flow-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}
