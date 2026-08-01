import { useCallback, useEffect, useRef, type CompositionEvent, type RefObject } from "react";

export function useStableImeTextAreaHeight(textAreaRef: RefObject<HTMLTextAreaElement | null>) {
  const lockedNodeRef = useRef<HTMLTextAreaElement | null>(null);
  const previousInlineHeightRef = useRef("");
  const releaseFrameRef = useRef(0);

  const release = useCallback(() => {
    if (releaseFrameRef.current) {
      window.cancelAnimationFrame(releaseFrameRef.current);
      releaseFrameRef.current = 0;
    }
    const node = lockedNodeRef.current;
    if (!node) {
      return;
    }
    node.style.height = previousInlineHeightRef.current;
    lockedNodeRef.current = null;
    previousInlineHeightRef.current = "";
  }, []);

  const handleCompositionStart = useCallback((event: CompositionEvent<HTMLTextAreaElement>) => {
    if (releaseFrameRef.current) {
      window.cancelAnimationFrame(releaseFrameRef.current);
      releaseFrameRef.current = 0;
    }
    const node = event.currentTarget;
    if (lockedNodeRef.current && lockedNodeRef.current !== node) {
      release();
    }
    if (!lockedNodeRef.current) {
      lockedNodeRef.current = node;
      previousInlineHeightRef.current = node.style.height;
    }
    node.style.height = `${node.getBoundingClientRect().height}px`;
  }, [release]);

  const handleCompositionEnd = useCallback((_event: CompositionEvent<HTMLTextAreaElement>) => {
    if (releaseFrameRef.current) {
      window.cancelAnimationFrame(releaseFrameRef.current);
    }
    releaseFrameRef.current = window.requestAnimationFrame(() => {
      releaseFrameRef.current = 0;
      release();
    });
  }, [release]);

  useEffect(() => {
    return () => release();
  }, [release]);

  return {
    onBlur: release,
    onCompositionEnd: handleCompositionEnd,
    onCompositionStart: handleCompositionStart,
  };
}
