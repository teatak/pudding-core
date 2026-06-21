import { useCallback, useRef, type KeyboardEvent } from "react";

const COMPOSITION_END_GRACE_MS = 30;

export function useImeCompositionGuard({ onCompositionEnd }: { onCompositionEnd?: () => void } = {}) {
  const composingRef = useRef(false);
  const lastCompositionEndRef = useRef(0);

  const handleCompositionStart = useCallback(() => {
    composingRef.current = true;
  }, []);

  const handleCompositionEnd = useCallback(() => {
    composingRef.current = false;
    lastCompositionEndRef.current = Date.now();
    onCompositionEnd?.();
  }, [onCompositionEnd]);

  const isComposing = useCallback((event: KeyboardEvent<HTMLElement>) => {
    return (
      composingRef.current ||
      event.nativeEvent.isComposing ||
      event.keyCode === 229 ||
      Date.now() - lastCompositionEndRef.current < COMPOSITION_END_GRACE_MS
    );
  }, []);

  return {
    isComposing,
    onCompositionEnd: handleCompositionEnd,
    onCompositionStart: handleCompositionStart,
  };
}
