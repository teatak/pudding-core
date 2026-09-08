import { useEffect, useState } from "react";

import { acknowledgeComputerPreview, onComputerPreview, subscribeComputerPreview, type DesktopComputerPreview } from "@/lib/desktopBridge";
import { AUTOMATION_PREVIEW_DISMISS_DELAY_MS } from "@/lib/automationPreview";
import { useOverlayStore } from "@/state/overlayStore";
import { useWorkspaceOpen } from "@/state/workspaceStore";

export function useComputerPreview(sessionID: string) {
  const turnID = useOverlayStore(state => state.runningTurns[sessionID]);
  const workspaceOpen = useWorkspaceOpen(sessionID);
  const [pageVisible, setPageVisible] = useState(document.visibilityState === "visible");
  const [preview, setPreview] = useState<DesktopComputerPreview | null>(null);
  const terminalStatus = useOverlayStore(state => preview ? state.assistants[preview.turnID]?.status : undefined);
  const visible = pageVisible && !workspaceOpen;

  useEffect(() => {
    const changed = () => setPageVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", changed);
    return () => document.removeEventListener("visibilitychange", changed);
  }, []);
  useEffect(() => {
    // A new turn/session invalidates the retained result, even without new
    // Computer Use activity. Merely hiding it would revive it when chat ends.
    setPreview(current => current?.sessionID === sessionID && (!turnID || current.turnID === turnID) ? current : null);
    let frame = 0;
    const stop = onComputerPreview(next => {
      if (next.sessionID !== sessionID || next.turnID !== turnID) return;
      setPreview(next);
      // One outstanding frame per renderer; hidden/busy renderers cannot build a queue.
      frame = requestAnimationFrame(() => acknowledgeComputerPreview(next));
    });
    return () => { stop(); cancelAnimationFrame(frame); };
  }, [sessionID, turnID]);
  useEffect(() => {
    void subscribeComputerPreview(sessionID, turnID || "", visible).catch(() => setPreview(null));
  }, [sessionID, turnID, visible]);
  useEffect(() => () => { void subscribeComputerPreview(sessionID, "", false); }, [sessionID]);
  useEffect(() => {
    if (turnID || !preview) return;
    if (terminalStatus === "cancelled") { setPreview(null); return; }
    const timer = window.setTimeout(() => setPreview(null), AUTOMATION_PREVIEW_DISMISS_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [turnID, preview?.turnID, terminalStatus]);

  return {
    preview: visible && preview?.sessionID === sessionID && (!turnID || preview.turnID === turnID) ? preview : null,
    completed: !turnID,
  };
}
