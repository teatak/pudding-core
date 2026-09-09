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
  drafts: Record<string, Record<string, unknown>>;
  setDraft: (request: InputFlowIdentity, key: string, value: unknown) => void;
  clearDraft: (request: InputFlowIdentity) => void;
  addRequest: (request: InputFlowRequest) => void;
  removeRequest: (request: InputFlowIdentity) => void;
};

type InputFlowIdentity = Pick<InputFlowRequest, "sessionID" | "id">;
export function inputFlowRequestKey(request: InputFlowIdentity) {
  return JSON.stringify([request.sessionID, request.id]);
}

export const useInputFlowStore = create<InputFlowState>((set) => ({
  requests: [],
  drafts: {},
  setDraft: (request, key, value) => set((state) => {
    const id = inputFlowRequestKey(request);
    return {drafts: {...state.drafts, [id]: {...state.drafts[id], [key]: value}}};
  }),
  clearDraft: (request) => set((state) => { const drafts = {...state.drafts}; delete drafts[inputFlowRequestKey(request)]; return {drafts}; }),
  addRequest: (request) =>
    set((state) => ({
      requests: [...state.requests.filter((item) => item.sessionID !== request.sessionID), request],
    })),
  removeRequest: (request) => set((state) => ({ requests: state.requests.filter((item) => item.sessionID !== request.sessionID || item.id !== request.id) })),
}));

export function showInputFlow(input: {
  id?: string;
  args: Record<string, unknown>;
  sessionID: string;
  title: string;
}): InputFlowRequest {
  const request: InputFlowRequest = {
    args: input.args,
    createdAt: new Date().toISOString(),
    id: input.id ?? newRequestID(),
    sessionID: input.sessionID,
    title: input.title,
  };
  useInputFlowStore.getState().addRequest(request);
  return request;
}

export function dismissInputFlow(request: InputFlowRequest) {
  useInputFlowStore.getState().removeRequest(request);
}

function newRequestID() {
  if (typeof crypto?.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `input-flow-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}
