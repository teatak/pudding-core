import {createContext, useContext, useEffect, useRef, useState, type Dispatch, type SetStateAction, type ReactNode} from "react";
import {useQuery, useQueryClient} from "@tanstack/react-query";
import {actOnUserInput, getUserInputRequest} from "@/api/client";
import {queryKeys} from "@/api/queryKeys";
import {useI18n} from "@/i18n";
import {dismissInputFlow, inputFlowRequestKey, useInputFlowStore, type InputFlowRequest} from "@/state/inputFlowStore";
import {inputPanelCountdown, inputPanelRemaining, inputWaitCountdown} from "./inputFlowTiming";

const LifecycleContext = createContext<{countdown: ReactNode; dismiss: () => void} | null>(null);
export function useInputFlowLifecycle() {
  const lifecycle = useContext(LifecycleContext);
  if (!lifecycle) throw new Error("InputFlowLifecycle is required");
  return lifecycle;
}

export function useInputFlowDraft<T>(request: InputFlowRequest, key: string, initial: T): [T, Dispatch<SetStateAction<T>>] {
  const [value, setValue] = useState<T>(() => (useInputFlowStore.getState().drafts[inputFlowRequestKey(request)]?.[key] as T | undefined) ?? initial);
  useEffect(() => {useInputFlowStore.getState().setDraft(request, key, value);}, [request, key, value]);
  return [value, setValue];
}

export function InputFlowLifecycle({request, token, children}: {request: InputFlowRequest; token?: string; children: ReactNode}) {
  const {t} = useI18n();
  const client = useQueryClient();
  const lastInteraction = useRef(Date.now());
  const [now, setNow] = useState(Date.now());
  const requestQuery = useQuery({
    queryKey: queryKeys.inputRequest(request.sessionID, request.id),
    queryFn: () => getUserInputRequest(token!, request.sessionID, request.id),
    enabled: token !== undefined,
    retry: false,
    refetchInterval: (query) => query.state.data?.status === "waiting" ? 1000 : false,
  });
  const status = requestQuery.data?.status;
  const previousStatus = useRef(status);
  useEffect(() => {
    if (status === "answered" || (previousStatus.current === "waiting" && status === "cancelled")) {
      dismissInputFlow(request);
      if (status === "answered") useInputFlowStore.getState().clearDraft(request);
    }
    previousStatus.current = status;
  }, [status, request]);
  useEffect(() => {
    const timer = window.setInterval(() => {
      const current = Date.now();
      setNow(current);
      if (inputPanelRemaining(lastInteraction.current, current) === 0) dismissInputFlow(request);
    }, 250);
    return () => window.clearInterval(timer);
  }, [request]);
  function send() {
    if (token === undefined) return;
    void actOnUserInput(token, request.sessionID, request.id, "dismiss").then((result) => {
      // The dismiss response is the authoritative snapshot; a late response
      // must not overwrite a later withdrawal.
      client.setQueryData(queryKeys.inputRequest(request.sessionID, request.id), result.request);
    }).catch(() => {void requestQuery.refetch();});
  }
  function interact() {
    lastInteraction.current = Date.now();
    // Let the existing timer refresh countdowns. Rendering during input capture
    // can restore a controlled number input before its onChange saves the value.
  }
  const panelSeconds = inputPanelCountdown(lastInteraction.current, now);
  const waitSeconds = status === "waiting" ? inputWaitCountdown(requestQuery.data?.deadline, Number(request.args.waitSeconds ?? 60), now) : null;
  const countdown = <span className="ml-auto flex items-center gap-2 py-1 text-xs text-muted-foreground" aria-live="polite">
    {waitSeconds !== null ? <span data-input-wait-countdown>{t("inputFlow.waitCountdown").replace("{seconds}", String(waitSeconds))}</span> : null}
    {panelSeconds !== null ? <span data-input-panel-countdown>{t("inputFlow.panelCountdown").replace("{seconds}", String(panelSeconds))}</span> : null}
  </span>;
  return <LifecycleContext.Provider value={{countdown, dismiss: () => {send(); dismissInputFlow(request);}}}>
    <div className="contents" onPointerDownCapture={interact} onInputCapture={interact} onKeyDownCapture={interact}>
      {children}
    </div>
  </LifecycleContext.Provider>;
}
