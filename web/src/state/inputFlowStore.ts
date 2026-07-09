import { create } from "zustand";

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

export const useInputFlowStore = create<InputFlowState>((set) => ({
  requests: [],
  addRequest: (request) =>
    set((state) => ({
      requests: [...state.requests.filter((item) => item.sessionID !== request.sessionID), request],
    })),
  removeRequest: (id) => set((state) => ({ requests: state.requests.filter((item) => item.id !== id) })),
}));

export function showInputFlow(input: {
  args: Record<string, unknown>;
  sessionID: string;
  title: string;
}): InputFlowRequest {
  const request: InputFlowRequest = {
    args: input.args,
    createdAt: new Date().toISOString(),
    id: newRequestID(),
    sessionID: input.sessionID,
    title: input.title,
  };
  dismissSessionInputFlows(input.sessionID);
  useInputFlowStore.getState().addRequest(request);
  return request;
}

export function dismissInputFlow(request: InputFlowRequest) {
  useInputFlowStore.getState().removeRequest(request.id);
}

function dismissSessionInputFlows(sessionID: string) {
  const existing = useInputFlowStore.getState().requests.filter((request) => request.sessionID === sessionID);
  for (const request of existing) {
    dismissInputFlow(request);
  }
}

function newRequestID() {
  if (typeof crypto?.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `input-flow-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}
