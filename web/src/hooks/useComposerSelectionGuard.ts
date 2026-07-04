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
      const activeElement = document.activeElement;
      if (activeElement instanceof HTMLElement && root.contains(activeElement)) {
        activeElement.blur();
      }
      root.setAttribute("data-composer-selection-guard", "true");
    };

    const handleFocusIn = (event: FocusEvent) => {
      const root = rootRef.current;
      if (!root || !root.contains(event.target as Node)) {
        return;
      }
      clearGuard();
    };

    document.addEventListener("pointerdown", handlePointerDown, true);
    document.addEventListener("focusin", handleFocusIn, true);
    window.addEventListener("blur", clearGuard);

    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
      document.removeEventListener("focusin", handleFocusIn, true);
      window.removeEventListener("blur", clearGuard);
    };
  }, []);

  return rootRef;
}
