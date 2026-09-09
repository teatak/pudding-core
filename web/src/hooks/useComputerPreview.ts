import { useEffect, useState } from "react";

import { acknowledgeComputerPreview, onComputerPreview, subscribeComputerPreview, type DesktopComputerPreviewFrame } from "@/lib/desktopBridge";
import { updateComputerPreviews } from "@/lib/automationPreview";
import { useOverlayStore } from "@/state/overlayStore";

export function useComputerPreview(sessionID: string) {
  const turnID = useOverlayStore(state => state.runningTurns[sessionID]);
  const [visible, setVisible] = useState(document.visibilityState === "visible");
  const [previews, setPreviews] = useState<DesktopComputerPreviewFrame[]>([]);
  const terminalStatus = useOverlayStore(state => previews[0] ? state.assistants[previews[0].turnID]?.status : undefined);

  useEffect(() => {
    const changed = () => setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", changed);
    return () => document.removeEventListener("visibilitychange", changed);
  }, []);
  useEffect(() => {
    // A new turn/session invalidates the retained result, even without new
    // Computer Use activity. Merely hiding it would revive it when chat ends.
    setPreviews(current => current.filter(frame => frame.sessionID === sessionID && (!turnID || frame.turnID === turnID)));
    let frame = 0;
    const stop = onComputerPreview(next => {
      if (next.sessionID !== sessionID || next.turnID !== turnID) return;
      setPreviews(current => updateComputerPreviews(current, next));
      // One outstanding frame per renderer; hidden/busy renderers cannot build a queue.
      frame = requestAnimationFrame(() => acknowledgeComputerPreview(next));
    });
    return () => { stop(); cancelAnimationFrame(frame); };
  }, [sessionID, turnID]);
  useEffect(() => {
    if (!visible) setPreviews([]);
    void subscribeComputerPreview(sessionID, turnID || "", visible).catch(() => setPreviews([]));
  }, [sessionID, turnID, visible]);
  useEffect(() => () => { void subscribeComputerPreview(sessionID, "", false); }, [sessionID]);
  useEffect(() => {
    if (!previews.length) return;
    if (!turnID && terminalStatus === "cancelled") { setPreviews([]); return; }
    // A frame never renews activity. Use the native deadline even after turn completion.
    const timer = window.setTimeout(() => setPreviews(current => current.filter(frame => frame.expiresAt > Date.now())),
      Math.max(0, Math.min(...previews.map(frame => frame.expiresAt)) - Date.now()));
    return () => window.clearTimeout(timer);
  }, [turnID, previews, terminalStatus]);

  return {
    previews: visible ? previews.filter(frame => frame.sessionID === sessionID && (!turnID || frame.turnID === turnID)) : [],
    completed: !turnID,
  };
}
