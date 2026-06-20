export type TextCaretClientPoint = {
  clientX: number;
  clientY: number;
};

export function getTextAreaCaretClientPoint(textArea: HTMLTextAreaElement): TextCaretClientPoint {
  const selectionEnd = textArea.selectionEnd ?? textArea.value.length;
  const doc = textArea.ownerDocument;
  const win = doc.defaultView;
  const rect = textArea.getBoundingClientRect();
  if (!win) {
    return { clientX: rect.left, clientY: rect.top + rect.height / 2 };
  }

  const style = win.getComputedStyle(textArea);
  const mirror = doc.createElement("div");
  mirror.style.position = "fixed";
  mirror.style.visibility = "hidden";
  mirror.style.pointerEvents = "none";
  mirror.style.left = `${rect.left}px`;
  mirror.style.top = `${rect.top}px`;
  mirror.style.width = `${rect.width}px`;
  mirror.style.minHeight = `${rect.height}px`;
  mirror.style.boxSizing = style.boxSizing;
  mirror.style.whiteSpace = "pre-wrap";
  mirror.style.overflowWrap = "break-word";
  mirror.style.wordBreak = style.wordBreak;
  mirror.style.overflow = "hidden";

  for (const prop of caretMirrorStyleProps) {
    mirror.style.setProperty(prop, style.getPropertyValue(prop));
  }

  mirror.textContent = textArea.value.slice(0, selectionEnd);
  const marker = doc.createElement("span");
  marker.textContent = "\u200b";
  mirror.appendChild(marker);
  doc.body.appendChild(mirror);

  const mark = marker.getBoundingClientRect();
  mirror.remove();

  return {
    clientX: clamp(mark.left - textArea.scrollLeft, rect.left, rect.right),
    clientY: clamp(mark.top + mark.height / 2 - textArea.scrollTop, rect.top, rect.bottom),
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

const caretMirrorStyleProps = [
  "border-top-width",
  "border-right-width",
  "border-bottom-width",
  "border-left-width",
  "font-family",
  "font-size",
  "font-style",
  "font-weight",
  "letter-spacing",
  "line-height",
  "padding-top",
  "padding-right",
  "padding-bottom",
  "padding-left",
  "tab-size",
  "text-indent",
  "text-transform",
] as const;
