import { useCallback, useRef, useState } from "react";

export function useUnsavedChangesGuard(dirty: boolean) {
  const pendingActionRef = useRef<(() => void) | null>(null);
  const [confirmationOpen, setConfirmationOpen] = useState(false);

  const changeConfirmationOpen = useCallback((open: boolean) => {
    if (!open) {
      pendingActionRef.current = null;
    }
    setConfirmationOpen(open);
  }, []);

  const request = useCallback(
    (action: () => void) => {
      if (!dirty) {
        action();
        return;
      }
      pendingActionRef.current = action;
      setConfirmationOpen(true);
    },
    [dirty],
  );

  const discard = useCallback(() => {
    const action = pendingActionRef.current;
    pendingActionRef.current = null;
    setConfirmationOpen(false);
    action?.();
  }, []);

  return {
    confirmationOpen,
    discard,
    request,
    setConfirmationOpen: changeConfirmationOpen,
  };
}
