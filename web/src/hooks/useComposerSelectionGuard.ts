import { useEffect, useRef } from "react";

export function useComposerSelectionGuard<T extends HTMLElement>() {
  const rootRef = useRef<T | null>(null);

  useEffect(() => {
    const clearGuard = () => {
      rootRef.current?.removeAttribute("data-composer-selection-guard");
    };

    const handlePointerDown = (event: PointerEvent) => {
      const root = rootRef.current;
      if (!root) {
        return;
      }
      if (root.contains(event.target as Node)) {
        clearGuard();
        return;
      }
      root.setAttribute("data-composer-selection-guard", "true");
    };

    document.addEventListener("pointerdown", handlePointerDown, true);
    document.addEventListener("pointerup", clearGuard, true);
    document.addEventListener("pointercancel", clearGuard, true);
    window.addEventListener("blur", clearGuard);

    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
      document.removeEventListener("pointerup", clearGuard, true);
      document.removeEventListener("pointercancel", clearGuard, true);
      window.removeEventListener("blur", clearGuard);
    };
  }, []);

  return rootRef;
}
