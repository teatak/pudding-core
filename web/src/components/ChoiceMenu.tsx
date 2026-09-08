import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from "react";

import { ArrowRight } from "@/components/icons";
import { cn } from "@/lib/utils";

export type ChoiceMenuItem<T> = {
  checked?: boolean;
  description?: string;
  disabled?: boolean;
  id: string;
  label: string;
  noActiveStyle?: boolean;
  render?: (active: boolean) => ReactNode;
  value: T;
};

type ChoiceMenuFocusMode = "always" | "when-idle" | "none";

export function ChoiceMenu<T>({
  busy = false,
  className,
  focusMode = "always",
  items,
  maxHeightClassName = "max-h-56",
  variant = "default",
  onEscape,
  onSelect,
}: {
  busy?: boolean;
  className?: string;
  focusMode?: ChoiceMenuFocusMode;
  items: Array<ChoiceMenuItem<T>>;
  maxHeightClassName?: string;
  variant?: "default" | "question";
  onEscape?: () => void;
  onSelect: (value: T) => void;
}) {
  const listRef = useRef<HTMLDivElement | null>(null);
  const signature = items.map((item) => `${item.id}:${item.disabled ? "0" : "1"}`).join("|");
  const [selectedIndex, setSelectedIndex] = useState(() => firstEnabledIndex(items));

  useEffect(() => {
    selectIndex(firstEnabledIndex(items));
  }, [signature, variant]);

  useEffect(() => {
    if (focusMode === "none" || (focusMode === "when-idle" && isTextEntryInUse(document.activeElement))) {
      return;
    }
    listRef.current?.focus({ preventScroll: variant === "question" });
  }, [focusMode, signature, variant]);

  function selectIndex(index: number) {
    setSelectedIndex(index);
    const list = listRef.current;
    const viewport = variant === "question" ? list?.closest<HTMLElement>("[data-input-flow-body]") ?? null : list;
    if (variant === "question" && index === 0 && viewport) {
      viewport.scrollTop = 0;
    } else {
      scrollActiveIntoList((list?.children[index] as HTMLElement | undefined) ?? null, viewport);
    }
  }

  function move(delta: number) {
    selectIndex(nextEnabledIndex(items, selectedIndex, delta));
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (busy || event.nativeEvent.isComposing) {
      return;
    }
    if (variant === "question") {
      // Embedded custom inputs own their keystrokes, including number shortcuts.
      if (event.target instanceof Element && event.target.closest('input, textarea, select, button:not([role="option"]), [contenteditable="true"], [role="textbox"]')) {
        return;
      }
      if (!event.metaKey && !event.ctrlKey && !event.altKey && /^[1-9]$/.test(event.key)) {
        const index = Number(event.key) - 1;
        const item = items[index];
        if (item && !item.disabled) {
          event.preventDefault();
          onSelect(item.value);
        }
        return;
      }
    }
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        move(1);
        return;
      case "ArrowUp":
        event.preventDefault();
        move(-1);
        return;
      case "Home":
        event.preventDefault();
        selectIndex(firstEnabledIndex(items));
        return;
      case "End":
        event.preventDefault();
        selectIndex(lastEnabledIndex(items));
        return;
      case "Enter":
      case " ":
        event.preventDefault();
        if (items[selectedIndex] && !items[selectedIndex].disabled) {
          onSelect(items[selectedIndex].value);
        }
        return;
      case "Escape":
        if (onEscape) {
          event.preventDefault();
          onEscape();
        }
        return;
      default:
        return;
    }
  }

  return (
    <div
      ref={listRef}
      aria-busy={busy || undefined}
      aria-multiselectable={items.some((item) => item.checked !== undefined) || undefined}
      className={cn("grid outline-none", variant === "question" ? "gap-1" : cn("gap-0.5 overflow-y-auto pr-1", maxHeightClassName), className)}
      data-variant={variant}
      role="listbox"
      tabIndex={0}
      onKeyDown={handleKeyDown}
    >
      {items.map((item, index) => {
        const disabled = busy || item.disabled;
        const itemClassName = cn(
          "min-w-0 rounded-md px-2.5 py-1.5 text-left transition-opacity",
          disabled && "opacity-50",
          variant === "question" && "flex min-h-11 items-center rounded-lg",
          !item.noActiveStyle &&
            "hover:bg-interactive-hover active:bg-interactive-pressed",
          index === selectedIndex &&
            !item.noActiveStyle &&
            "bg-interactive-selected text-foreground hover:bg-interactive-selected",
        );
        const commonProps = {
          "aria-selected": item.checked ?? index === selectedIndex,
          "aria-keyshortcuts": variant === "question" && index < 9 ? String(index + 1) : undefined,
          className: itemClassName,
          onMouseEnter: () => {
            if (!disabled) {
              setSelectedIndex(index);
            }
          },
          onMouseDown: (event: MouseEvent) => {
            if (variant === "question" && item.noActiveStyle) {
              return;
            }
            event.preventDefault();
            if (variant === "default" && !disabled && !item.noActiveStyle) {
              onSelect(item.value);
            }
          },
          onClick: () => {
            if (variant === "question" && !disabled && !item.noActiveStyle) {
              onSelect(item.value);
            }
          },
          role: "option",
          tabIndex: -1,
        };
        if (item.render) {
          return (
            <div
              key={item.id}
              aria-disabled={disabled || undefined}
              {...commonProps}
            >
              {item.render(index === selectedIndex)}
            </div>
          );
        }
        return (
          <button
            key={item.id}
            {...commonProps}
            className={cn(itemClassName, variant === "question" && "gap-2.5")}
            disabled={disabled}
            type="button"
          >
            {variant === "question" ? (
              <ChoiceMenuNumber number={index + 1} />
            ) : null}
            <div className="min-w-0 flex-1">
              <div className={cn("text-sm", variant === "question" ? "whitespace-normal break-words font-medium leading-6" : "truncate font-normal")}>{item.label}</div>
              {item.description ? <div className={cn("mt-0.5 text-xs text-muted-foreground", variant === "question" ? "whitespace-normal break-words leading-5" : "truncate")}>{item.description}</div> : null}
            </div>
            {variant === "question" ? <ArrowRight aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" /> : null}
          </button>
        );
      })}
    </div>
  );
}

export function ChoiceMenuNumber({ number }: { number: number }) {
  return (
    <span aria-hidden="true" className="flex size-7 shrink-0 items-center justify-center rounded-full border border-foreground/10 bg-foreground/5 text-sm tabular-nums text-muted-foreground">
      {number}
    </span>
  );
}

function firstEnabledIndex(items: Array<{ disabled?: boolean }>) {
  const index = items.findIndex((item) => !item.disabled);
  return index >= 0 ? index : 0;
}

function lastEnabledIndex(items: Array<{ disabled?: boolean }>) {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (!items[index]?.disabled) {
      return index;
    }
  }
  return 0;
}

function nextEnabledIndex(items: Array<{ disabled?: boolean }>, current: number, delta: number) {
  if (items.length === 0) {
    return 0;
  }
  let index = current;
  for (let count = 0; count < items.length; count += 1) {
    index = (index + delta + items.length) % items.length;
    if (!items[index]?.disabled) {
      return index;
    }
  }
  return current;
}

function scrollActiveIntoList(active: HTMLElement | null, list: HTMLElement | null) {
  if (!active || !list) {
    return;
  }
  const activeRect = active.getBoundingClientRect();
  const listRect = list.getBoundingClientRect();
  const padding = 4;
  if (activeRect.top < listRect.top + padding) {
    list.scrollTop -= listRect.top + padding - activeRect.top;
  } else if (activeRect.bottom > listRect.bottom - padding) {
    list.scrollTop += activeRect.bottom - (listRect.bottom - padding);
  }
}

function isTextEntryInUse(element: Element | null) {
  if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement) {
    return element.value.length > 0;
  }
  if (!(element instanceof HTMLElement)) {
    return false;
  }
  if (element.matches('[contenteditable="true"]')) {
    return Boolean(element.textContent);
  }
  return element.matches('[role="textbox"]');
}
