import { useEffect } from "react";

let activeSelectAllScope: HTMLElement | null = null;

export function useScopedSelectAll(scope: HTMLElement | null) {
  useEffect(() => {
    if (!scope) {
      return;
    }

    const handlePointerDown = (event: PointerEvent) => {
      if (event.target instanceof Node && scope.contains(event.target)) {
        activeSelectAllScope = scope;
      } else if (activeSelectAllScope === scope) {
        activeSelectAllScope = null;
      }
    };

    const handleFocusIn = (event: FocusEvent) => {
      if (event.target instanceof Node && scope.contains(event.target)) {
        activeSelectAllScope = scope;
      } else if (activeSelectAllScope === scope) {
        activeSelectAllScope = null;
      }
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      if (
        activeSelectAllScope !== scope ||
        !isSelectAllShortcut(event) ||
        event.defaultPrevented ||
        isEditableTarget(event.target)
      ) {
        return;
      }
      event.preventDefault();
      selectScopeContents(scope);
    };

    const handleSelectionChange = () => {
      const selection = window.getSelection();
      if (!selection || selection.rangeCount === 0 || selection.isCollapsed) {
        return;
      }
      const anchorInside = Boolean(selection.anchorNode && scope.contains(selection.anchorNode));
      const focusInside = Boolean(selection.focusNode && scope.contains(selection.focusNode));
      if (anchorInside && focusInside) {
        activeSelectAllScope = scope;
        return;
      }
      if (activeSelectAllScope === scope) {
        selectScopeContents(scope);
      }
    };

    window.addEventListener("pointerdown", handlePointerDown, true);
    window.addEventListener("focusin", handleFocusIn, true);
    window.addEventListener("keydown", handleKeyDown, true);
    document.addEventListener("selectionchange", handleSelectionChange);
    return () => {
      window.removeEventListener("pointerdown", handlePointerDown, true);
      window.removeEventListener("focusin", handleFocusIn, true);
      window.removeEventListener("keydown", handleKeyDown, true);
      document.removeEventListener("selectionchange", handleSelectionChange);
      if (activeSelectAllScope === scope) {
        activeSelectAllScope = null;
      }
    };
  }, [scope]);
}

function isSelectAllShortcut(event: KeyboardEvent) {
  return event.key.toLowerCase() === "a" && (event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey;
}

function isEditableTarget(target: EventTarget | null) {
  if (!(target instanceof Element)) {
    return false;
  }
  return Boolean(target.closest('input, textarea, select, [role="textbox"], [contenteditable]:not([contenteditable="false"])'));
}

function selectScopeContents(scope: HTMLElement) {
  if (!scope.isConnected) {
    return;
  }
  const selection = window.getSelection();
  if (!selection) {
    return;
  }
  const range = document.createRange();
  range.selectNodeContents(scope);
  selection.removeAllRanges();
  selection.addRange(range);
}
