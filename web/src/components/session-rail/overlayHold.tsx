import { createContext, useContext, useEffect, useId } from "react";

export const RailOverlayHoldContext = createContext<((id: string, open: boolean) => void) | null>(null);

export function useRailOverlayHold(open: boolean) {
  const setOverlayHold = useContext(RailOverlayHoldContext);
  const id = useId();

  useEffect(() => {
    if (!setOverlayHold) {
      return;
    }
    setOverlayHold(id, open);
    return () => setOverlayHold(id, false);
  }, [id, open, setOverlayHold]);
}
