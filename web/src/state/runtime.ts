import { isElectronShell } from "@/state/shell";

const RUNTIME_ID_HEADER = "X-Pudding-Runtime-ID";

const runtimeID = createRuntimeID();

export function getRuntimeID() {
  return runtimeID;
}

export function getRuntimeType() {
  return isElectronShell() ? "desktop" : "web";
}

export function runtimeRequestHeaders() {
  return { [RUNTIME_ID_HEADER]: getRuntimeID() };
}

function createRuntimeID() {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return `runtime_${crypto.randomUUID()}`;
  }
  return `runtime_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`;
}
