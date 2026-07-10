import { useCallback, useEffect, useState, type CSSProperties } from "react";

const FADE_WIDTH = 12;
const OVERFLOW_EPSILON = 1;

function maskImageForEdges(left: boolean, right: boolean) {
  if (left && right) {
    return `linear-gradient(to right, transparent 0, black ${FADE_WIDTH}px, black calc(100% - ${FADE_WIDTH}px), transparent 100%)`;
  }
  if (left) {
    return `linear-gradient(to right, transparent 0, black ${FADE_WIDTH}px)`;
  }
  if (right) {
    return `linear-gradient(to right, black calc(100% - ${FADE_WIDTH}px), transparent 100%)`;
  }
  return undefined;
}

export function useHorizontalScrollMask<T extends HTMLElement>() {
  const [element, setElement] = useState<T | null>(null);
  const [maskImage, setMaskImage] = useState<string>();
  const ref = useCallback((node: T | null) => setElement(node), []);

  const updateMask = useCallback(() => {
    if (!element) {
      return;
    }
    const overflow = element.scrollWidth - element.clientWidth > OVERFLOW_EPSILON;
    const left = overflow && element.scrollLeft > OVERFLOW_EPSILON;
    const right = overflow && element.scrollLeft + element.clientWidth < element.scrollWidth - OVERFLOW_EPSILON;
    const nextMaskImage = maskImageForEdges(left, right);
    setMaskImage((current) => (current === nextMaskImage ? current : nextMaskImage));
  }, [element]);

  useEffect(() => {
    if (!element) {
      return;
    }
    updateMask();
    element.addEventListener("scroll", updateMask, { passive: true });
    window.addEventListener("resize", updateMask);

    const observer = typeof ResizeObserver === "undefined" ? undefined : new ResizeObserver(updateMask);
    observer?.observe(element);
    if (element.firstElementChild instanceof HTMLElement) {
      observer?.observe(element.firstElementChild);
    }

    return () => {
      element.removeEventListener("scroll", updateMask);
      window.removeEventListener("resize", updateMask);
      observer?.disconnect();
    };
  }, [updateMask]);

  const style: CSSProperties | undefined = maskImage
    ? { maskImage, WebkitMaskImage: maskImage }
    : undefined;

  return { ref, style };
}
