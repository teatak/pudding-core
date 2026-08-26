import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from "react";

import { cn } from "@/lib/utils";

export type ChoiceMenuItem<T> = {
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
  onEscape,
  onSelect,
}: {
  busy?: boolean;
  className?: string;
  focusMode?: ChoiceMenuFocusMode;
  items: Array<ChoiceMenuItem<T>>;
  maxHeightClassName?: string;
  onEscape?: () => void;
  onSelect: (value: T) => void;
}) {
  const listRef = useRef<HTMLDivElement | null>(null);
  const selectedRef = useRef<HTMLElement | null>(null);
  const signature = items.map((item) => `${item.id}:${item.disabled ? "0" : "1"}`).join("|");
  const [selectedIndex, setSelectedIndex] = useState(() => firstEnabledIndex(items));

  useEffect(() => {
    setSelectedIndex(firstEnabledIndex(items));
  }, [signature]);

  useEffect(() => {
    if (focusMode === "none" || (focusMode === "when-idle" && isTextEntryInUse(document.activeElement))) {
      return;
    }
    listRef.current?.focus();
  }, [focusMode, signature]);

  useEffect(() => {
    scrollActiveIntoList(selectedRef.current, listRef.current);
  }, [selectedIndex, signature]);

  function move(delta: number) {
    setSelectedIndex((current) => nextEnabledIndex(items, current, delta));
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (busy) {
      return;
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
        setSelectedIndex(firstEnabledIndex(items));
        return;
      case "End":
        event.preventDefault();
        setSelectedIndex(lastEnabledIndex(items));
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
      className={cn("grid gap-0.5 overflow-y-auto pr-1 outline-none", maxHeightClassName, className)}
      role="listbox"
      tabIndex={0}
      onKeyDown={handleKeyDown}
    >
      {items.map((item, index) => {
        const disabled = busy || item.disabled;
        const itemClassName = cn(
          "min-w-0 rounded-md px-2.5 py-1.5 text-left transition-opacity disabled:opacity-50",
          !item.noActiveStyle &&
            "hover:bg-interactive-hover active:bg-interactive-pressed",
          index === selectedIndex &&
            !item.noActiveStyle &&
            "bg-interactive-selected text-foreground hover:bg-interactive-selected",
          item.noActiveStyle && "px-0 py-0.5",
        );
        const commonProps = {
          "aria-selected": index === selectedIndex,
          className: itemClassName,
          onMouseEnter: () => {
            if (!disabled) {
              setSelectedIndex(index);
            }
          },
          onMouseDown: (event: MouseEvent) => {
            event.preventDefault();
            if (!disabled && !item.noActiveStyle) {
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
              ref={(node) => {
                if (index === selectedIndex) {
                  selectedRef.current = node;
                }
              }}
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
            ref={(node) => {
              if (index === selectedIndex) {
                selectedRef.current = node;
              }
            }}
            {...commonProps}
            disabled={disabled}
            type="button"
          >
            <div className="truncate text-sm font-medium">{item.label}</div>
            {item.description ? <div className="mt-0.5 truncate text-xs text-muted-foreground">{item.description}</div> : null}
          </button>
        );
      })}
    </div>
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
