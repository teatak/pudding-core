import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState, type ChangeEvent, type ClipboardEvent, type FocusEvent, type KeyboardEvent, type RefObject } from "react";
import { useWatch, type Control, type UseFormRegisterReturn } from "react-hook-form";
import type { LucideIcon } from "@/components/icons";

import { ComposerMentionMenu } from "@/components/ComposerMentionMenu";
import { composerSuggestionPanelClassName } from "@/components/composerControlStyles";
import { useComposerMentions } from "@/components/useComposerMentions";
import { useImeCompositionGuard } from "@/hooks/useImeCompositionGuard";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import type { ComposerMentionActionID, ComposerMentionReference } from "@/components/composerMentionReferences";

export type SlashCommand = {
  command: string;
  description: string;
  hasArgs: boolean;
  icon: LucideIcon;
  id: "clear" | "compact" | "rename" | "summary";
  label: string;
};

export type SlashSubmitCommand =
  | { id: "clear" }
  | { id: "compact"; hint: string }
  | { id: "rename"; title: string }
  | { id: "summary"; hint: string };

export type ComposerTextAreaHandle = {
  openMentionMenu: () => void;
};

type ComposerTextAreaProps = {
  control: Control<{ text: string }>;
  textField: UseFormRegisterReturn<"text">;
  textAreaRef: RefObject<HTMLTextAreaElement | null>;
  mentionReferences: ComposerMentionReference[];
  slashCommands: SlashCommand[];
  placeholder: string;
  hasAttachments: boolean;
  hasLocalFolders: boolean;
  hasProjectReferences: boolean;
  hasPendingAttachments: boolean;
  hasFailedAttachments: boolean;
  uploadedAttachmentsCount: number;
  formSetValue: (name: "text", value: string, options?: { shouldDirty?: boolean }) => void;
  setSessionDraftText: (sessionID: string, text: string) => void;
  sessionID: string;
  onCanSendChange: (canSend: boolean) => void;
  onHasContentChange: (hasContent: boolean) => void;
  onMentionMenuOpenChange: (open: boolean) => void;
  onSlashMenuOpenChange: (open: boolean) => void;
  onDraftSlashCommandChange: (command: SlashSubmitCommand | null) => void;
  onAction: (actionID: ComposerMentionActionID) => void;
  onSlashCommandSelect: (command: SlashCommand) => void;
  onEnter: (info: {
    canSend: boolean;
    guideNow: boolean;
    mentionMenuOpen: boolean;
    slashMenuOpen: boolean;
    draftSlashCommand: SlashSubmitCommand | null;
  }) => void;
  onPaste: (event: ClipboardEvent<HTMLTextAreaElement>) => void;
  onBlur: () => void;
  onClearError: () => void;
  scheduleMascotInputGaze: () => void;
};

export function parseSlashSubmitCommand(text: string): SlashSubmitCommand | null {
  if (text === "/clear") {
    return { id: "clear" };
  }
  const compactMatch = text.match(/^\/compact(?:\s+([\s\S]*))?$/);
  if (compactMatch) {
    return { id: "compact", hint: (compactMatch[1] || "").trim() };
  }
  const renameMatch = text.match(/^\/rename(?:\s+([\s\S]*))?$/);
  if (renameMatch) {
    return { id: "rename", title: (renameMatch[1] || "").trim() };
  }
  const summaryMatch = text.match(/^\/summary(?:\s+([\s\S]*))?$/);
  if (summaryMatch) {
    return { id: "summary", hint: (summaryMatch[1] || "").trim() };
  }
  return null;
}

function slashCommandQuery(text: string, commands: SlashCommand[]): string | null {
  if (!text.startsWith("/") || /\s/.test(text)) {
    return null;
  }
  const query = text.slice(1);
  if (commands.some((command) => command.command === text)) {
    return null;
  }
  return query;
}

function SlashCommandIcon({ command }: { command: SlashCommand }) {
  const Icon = command.icon;
  return (
    <span className="grid size-5 shrink-0 place-items-center text-foreground/70">
      <Icon className="size-4" data-icon-weight="strong" />
    </span>
  );
}

function SlashCommandMenu({
  commands,
  selectedIndex,
  onHover,
  onSelect,
}: {
  commands: SlashCommand[];
  selectedIndex: number;
  onHover: (index: number) => void;
  onSelect: (command: SlashCommand) => void;
}) {
  const selectedRef = useRef<HTMLButtonElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    scrollActiveSlashCommandIntoList(selectedRef.current, listRef.current);
  }, [selectedIndex]);

  return (
    <div
      ref={listRef}
      className={cn(
        "absolute bottom-full left-16 z-40 max-h-64 w-[min(30rem,calc(100%-6rem))] overflow-y-auto p-1 text-sm",
        composerSuggestionPanelClassName,
      )}
      role="listbox"
    >
      {commands.map((command, index) => (
        <button
          key={command.id}
          ref={index === selectedIndex ? selectedRef : undefined}
          aria-selected={index === selectedIndex}
          aria-label={`${command.label} ${command.description}`}
          className={cn(
            "flex h-9 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-[12px] hover:bg-control-hover active:bg-control-active",
            index === selectedIndex && "bg-control-hover text-foreground",
          )}
          role="option"
          type="button"
          onMouseEnter={() => onHover(index)}
          onMouseDown={(event) => {
            event.preventDefault();
            onSelect(command);
          }}
        >
          <SlashCommandIcon command={command} />
          <span className="shrink-0 font-medium">{command.label}</span>
          <span className="ml-1 min-w-0 flex-1 truncate text-muted-foreground/65">{command.description}</span>
        </button>
      ))}
    </div>
  );
}

function scrollActiveSlashCommandIntoList(active: HTMLElement | null, list: HTMLElement | null) {
  if (!active || !list) {
    return;
  }
  const activeTop = active.offsetTop;
  const activeBottom = activeTop + active.offsetHeight;
  const visibleTop = list.scrollTop;
  const visibleBottom = visibleTop + list.clientHeight;
  if (activeTop < visibleTop) {
    list.scrollTop = activeTop;
  } else if (activeBottom > visibleBottom) {
    list.scrollTop = activeBottom - list.clientHeight;
  }
}

export const ComposerTextArea = forwardRef<ComposerTextAreaHandle, ComposerTextAreaProps>(function ComposerTextArea(
  {
    control,
    textField,
    textAreaRef,
    mentionReferences,
    slashCommands,
    placeholder,
    hasAttachments,
    hasLocalFolders,
    hasProjectReferences,
    hasPendingAttachments,
    hasFailedAttachments,
    uploadedAttachmentsCount,
    formSetValue,
    setSessionDraftText,
    sessionID,
    onCanSendChange,
    onHasContentChange,
    onMentionMenuOpenChange,
    onSlashMenuOpenChange,
    onDraftSlashCommandChange,
    onAction,
    onSlashCommandSelect,
    onEnter,
    onPaste,
    onBlur,
    onClearError,
    scheduleMascotInputGaze,
  },
  ref,
) {
  const [textFocused, setTextFocused] = useState(false);
  const [slashSelectedIndex, setSlashSelectedIndex] = useState(0);

  const draftText = useWatch({ control, name: "text", defaultValue: "" });
  const trimmedDraftText = draftText.trim();

  const hasContent = Boolean(trimmedDraftText || hasAttachments || hasLocalFolders || hasProjectReferences);
  const canSend = Boolean(trimmedDraftText || uploadedAttachmentsCount || hasLocalFolders || hasProjectReferences) && !hasPendingAttachments && !hasFailedAttachments;
  const draftSlashCommand = hasAttachments || hasLocalFolders || hasProjectReferences ? null : parseSlashSubmitCommand(trimmedDraftText);
  const slashQuery = slashCommandQuery(trimmedDraftText, slashCommands);
  const visibleSlashCommands =
    textFocused && slashQuery !== null
      ? slashCommands.filter((command) => command.command.startsWith("/" + slashQuery))
      : [];
  const slashMenuOpen = visibleSlashCommands.length > 0;

  const setComposerText = useCallback(
    (nextText: string) => {
      formSetValue("text", nextText, { shouldDirty: true });
      setSessionDraftText(sessionID, nextText);
      if (textAreaRef.current && textAreaRef.current.value !== nextText) {
        textAreaRef.current.value = nextText;
      }
    },
    [formSetValue, sessionID, setSessionDraftText, textAreaRef],
  );

  const mentions = useComposerMentions({
    references: mentionReferences,
    text: draftText,
    setText: setComposerText,
    textAreaRef,
    onAction,
  });

  const mentionMenuOpen = textFocused && mentions.open && !slashMenuOpen;

  const ime = useImeCompositionGuard({ onCompositionEnd: scheduleMascotInputGaze });

  const prevCanSendRef = useRef<boolean | undefined>(undefined);
  useEffect(() => {
    if (prevCanSendRef.current !== canSend) {
      prevCanSendRef.current = canSend;
      onCanSendChange(canSend);
    }
  }, [canSend, onCanSendChange]);

  const prevHasContentRef = useRef<boolean | undefined>(undefined);
  useEffect(() => {
    if (prevHasContentRef.current !== hasContent) {
      prevHasContentRef.current = hasContent;
      onHasContentChange(hasContent);
    }
  }, [hasContent, onHasContentChange]);

  const prevMentionMenuOpenRef = useRef(mentionMenuOpen);
  useEffect(() => {
    if (prevMentionMenuOpenRef.current !== mentionMenuOpen) {
      prevMentionMenuOpenRef.current = mentionMenuOpen;
      onMentionMenuOpenChange(mentionMenuOpen);
    }
  }, [mentionMenuOpen, onMentionMenuOpenChange]);

  const prevSlashMenuOpenRef = useRef(slashMenuOpen);
  useEffect(() => {
    if (prevSlashMenuOpenRef.current !== slashMenuOpen) {
      prevSlashMenuOpenRef.current = slashMenuOpen;
      onSlashMenuOpenChange(slashMenuOpen);
    }
  }, [slashMenuOpen, onSlashMenuOpenChange]);

  const prevDraftSlashCommandRef = useRef<string | null>(null);
  const serializedCommand = draftSlashCommand ? JSON.stringify(draftSlashCommand) : null;
  useEffect(() => {
    if (prevDraftSlashCommandRef.current !== serializedCommand) {
      prevDraftSlashCommandRef.current = serializedCommand;
      onDraftSlashCommandChange(draftSlashCommand);
    }
  }, [serializedCommand, draftSlashCommand, onDraftSlashCommandChange]);

  useEffect(() => {
    if (!slashMenuOpen) {
      setSlashSelectedIndex(0);
      return;
    }
    setSlashSelectedIndex((index) => Math.min(index, visibleSlashCommands.length - 1));
  }, [slashMenuOpen, visibleSlashCommands.length]);

  const setTextAreaRef = (node: HTMLTextAreaElement | null) => {
    textAreaRef.current = node;
    textField.ref(node);
  };

  const handleTextBlur = (event: FocusEvent<HTMLTextAreaElement>) => {
    textField.onBlur(event);
    mentions.close();
    setTextFocused(false);
    onBlur();
  };

  const handleTextFocus = () => {
    setTextFocused(true);
    if (textAreaRef.current) {
      mentions.notifyCursor(textAreaRef.current.selectionStart);
    }
    scheduleMascotInputGaze();
  };

  const handleTextCursorUpdate = (event: { currentTarget: HTMLTextAreaElement }) => {
    mentions.notifyCursor(event.currentTarget.selectionStart);
    scheduleMascotInputGaze();
  };

  const handleTextChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    const previousText = draftText;
    const nextText = event.target.value;
    void textField.onChange(event);
    setSessionDraftText(sessionID, nextText);
    mentions.notifyChange(nextText, previousText, event.target.selectionStart);
    onClearError();
    setTextFocused(true);
    scheduleMascotInputGaze();
  };

  const selectSlashCommand = (command: SlashCommand) => {
    if (!command.hasArgs) {
      onSlashCommandSelect(command);
      return;
    }
    const nextText = command.command + " ";
    formSetValue("text", nextText);
    setSessionDraftText(sessionID, nextText);
    onSlashCommandSelect(command);
    window.requestAnimationFrame(() => {
      textAreaRef.current?.focus();
      scheduleMascotInputGaze();
    });
  };

  const handleTextKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "@" && !event.metaKey && !event.ctrlKey && !event.altKey) {
      mentions.notifyCursor(event.currentTarget.selectionStart + 1);
    }
    if (mentions.onKeyDown(event)) {
      scheduleMascotInputGaze();
      return;
    }
    if (slashMenuOpen) {
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          setSlashSelectedIndex((index) => (index + 1) % visibleSlashCommands.length);
          return;
        case "ArrowUp":
          event.preventDefault();
          setSlashSelectedIndex((index) => (index - 1 + visibleSlashCommands.length) % visibleSlashCommands.length);
          return;
        case "Enter":
        case "Tab": {
          event.preventDefault();
          const command = visibleSlashCommands[slashSelectedIndex] ?? visibleSlashCommands[0];
          if (command) {
            selectSlashCommand(command);
          }
          return;
        }
        case "Escape":
          event.preventDefault();
          setTextFocused(false);
          return;
      }
    }
    if (event.key === "Enter" && !event.shiftKey) {
      if (ime.isComposing(event)) {
        scheduleMascotInputGaze();
        return;
      }
      event.preventDefault();

      const currentText = event.currentTarget.value.trim();
      const currentCanSend = Boolean(currentText || uploadedAttachmentsCount || hasLocalFolders || hasProjectReferences) && !hasPendingAttachments && !hasFailedAttachments;
      const currentSlashCommand = hasAttachments || hasLocalFolders || hasProjectReferences ? null : parseSlashSubmitCommand(currentText);

      onEnter({
        canSend: currentCanSend,
        guideNow: event.metaKey,
        mentionMenuOpen,
        slashMenuOpen,
        draftSlashCommand: currentSlashCommand,
      });
    }
    scheduleMascotInputGaze();
  };

  const openMentionMenu = useCallback(() => {
    setTextFocused(true);
    mentions.openManual();
  }, [mentions]);

  useImperativeHandle(ref, () => ({ openMentionMenu }), [openMentionMenu]);

  return (
    <>
      {mentionMenuOpen ? (
        <ComposerMentionMenu
          references={mentions.filtered}
          query={mentions.query}
          selectedIndex={mentions.activeIndex}
          onHover={mentions.setActiveIndex}
          onSelect={mentions.select}
        />
      ) : slashMenuOpen ? (
        <SlashCommandMenu
          commands={visibleSlashCommands}
          selectedIndex={slashSelectedIndex}
          onHover={setSlashSelectedIndex}
          onSelect={selectSlashCommand}
        />
      ) : null}
      <Textarea
        data-composer-text-input="true"
        className="block max-h-36 min-h-6 resize-none overflow-y-auto rounded-none border-0 bg-transparent p-0 text-base leading-6 shadow-none focus-visible:ring-0 md:text-sm dark:bg-transparent"
        placeholder={placeholder}
        rows={1}
        name={textField.name}
        ref={setTextAreaRef}
        onBlur={handleTextBlur}
        onChange={handleTextChange}
        onCompositionEnd={ime.onCompositionEnd}
        onCompositionStart={ime.onCompositionStart}
        onClick={handleTextCursorUpdate}
        onFocus={handleTextFocus}
        onKeyDown={handleTextKeyDown}
        onKeyUp={handleTextCursorUpdate}
        onMouseUp={handleTextCursorUpdate}
        onPaste={onPaste}
        onSelect={handleTextCursorUpdate}
      />
    </>
  );
});
