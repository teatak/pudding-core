import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from "react";

import {
  type ComposerMentionActionID,
  filterComposerMentionReferences,
  findComposerMentionTrigger,
  type ComposerMentionReference,
  type ComposerMentionTrigger,
} from "@/components/composerMentionReferences";

export type ComposerMentionState = {
  open: boolean;
  filtered: ComposerMentionReference[];
  activeIndex: number;
  query: string;
  setActiveIndex: (index: number) => void;
  close: () => void;
  openManual: () => void;
  select: (reference: ComposerMentionReference) => void;
  onKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => boolean;
  notifyChange: (newValue: string, oldValue: string, cursor: number) => void;
  notifyCursor: (cursor: number) => void;
};

export function useComposerMentions({
  references,
  text,
  setText,
  textAreaRef,
  onAction,
}: {
  references: ComposerMentionReference[];
  text: string;
  setText: (text: string) => void;
  textAreaRef: RefObject<HTMLTextAreaElement | null>;
  onAction?: (actionID: ComposerMentionActionID) => void;
}): ComposerMentionState {
  const [cursor, setCursor] = useState(0);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [manualTrigger, setManualTrigger] = useState<ComposerMentionTrigger | null>(null);
  const userClosedRef = useRef(false);
  const textTrigger = useMemo(() => findComposerMentionTrigger(text, cursor), [cursor, text]);
  const trigger = manualTrigger || textTrigger;
  const filtered = useMemo(
    () => (trigger ? filterComposerMentionReferences(references, trigger.query) : []),
    [references, trigger],
  );

  useEffect(() => {
    if (manualTrigger) {
      return;
    }
    if (!textTrigger) {
      setOpen(false);
      userClosedRef.current = false;
      return;
    }
    if (userClosedRef.current) {
      return;
    }
    setOpen(true);
    setActiveIndex(0);
  }, [manualTrigger, textTrigger]);

  useEffect(() => {
    if (activeIndex >= filtered.length) {
      setActiveIndex(Math.max(0, filtered.length - 1));
    }
  }, [activeIndex, filtered.length]);

  const setSelection = (position: number) => {
    window.requestAnimationFrame(() => {
      const textArea = textAreaRef.current;
      if (!textArea) {
        return;
      }
      textArea.focus({ preventScroll: true });
      textArea.setSelectionRange(position, position);
      setCursor(position);
    });
  };

  const notifyChange = (newValue: string, oldValue: string, nextCursor: number) => {
    setCursor(nextCursor);
    setManualTrigger(null);
    if (newValue.length - oldValue.length > 2) {
      setOpen(false);
      userClosedRef.current = true;
      return;
    }
    if (!findComposerMentionTrigger(newValue, nextCursor)) {
      userClosedRef.current = false;
    }
  };

  const notifyCursor = (nextCursor: number) => {
    setCursor(nextCursor);
  };

  const close = () => {
    setManualTrigger(null);
    setOpen(false);
    userClosedRef.current = true;
  };

  const openManual = () => {
    const textArea = textAreaRef.current;
    const position = Math.max(0, Math.min(textArea?.selectionStart ?? cursor, text.length));
    setCursor(position);
    setManualTrigger({ start: position, end: position, query: "" });
    setOpen(true);
    setActiveIndex(0);
    userClosedRef.current = false;
    setSelection(position);
  };

  const replaceTrigger = (replacement: string, keepOpen = false) => {
    if (!trigger) {
      return;
    }
    const next = text.slice(0, trigger.start) + replacement + text.slice(trigger.end);
    const nextCursor = trigger.start + replacement.length;
    setText(next);
    setManualTrigger(null);
    setOpen(keepOpen);
    userClosedRef.current = !keepOpen;
    setSelection(nextCursor);
  };

  const select = (reference: ComposerMentionReference) => {
    if (reference.kind === "action" && reference.actionID) {
      replaceTrigger("");
      onAction?.(reference.actionID);
      return;
    }
    const suffix = reference.keepOpen || reference.insertText.endsWith("/") ? "" : " ";
    replaceTrigger(reference.insertText + suffix, Boolean(reference.keepOpen));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (!open) {
      return false;
    }
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        setActiveIndex((index) => Math.min(index + 1, Math.max(0, filtered.length - 1)));
        return true;
      case "ArrowUp":
        event.preventDefault();
        setActiveIndex((index) => Math.max(0, index - 1));
        return true;
      case "Enter":
      case "Tab": {
        if (filtered.length === 0) {
          return false;
        }
        event.preventDefault();
        const reference = filtered[activeIndex] ?? filtered[0];
        if (reference) {
          select(reference);
        }
        return true;
      }
      case "Escape":
        event.preventDefault();
        setManualTrigger(null);
        setOpen(false);
        userClosedRef.current = true;
        return true;
      default:
        return false;
    }
  };

  return {
    open,
    filtered,
    activeIndex,
    query: trigger?.query ?? "",
    setActiveIndex,
    close,
    openManual,
    select,
    onKeyDown,
    notifyChange,
    notifyCursor,
  };
}
