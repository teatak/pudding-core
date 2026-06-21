import { useInfiniteQuery } from "@tanstack/react-query";
import { ArrowDown, Check, ChevronDown, ChevronRight, CircleAlert, Copy, Loader2 } from "lucide-react";
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent } from "react";

import { listTurns, type ContentPart, type Message } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { PhaseDot } from "@/components/PhaseDot";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useStreamedText } from "@/hooks/useStreamedText";
import { useTranscriptScroll } from "@/hooks/useTranscriptScroll";
import { useI18n } from "@/i18n";
import { renderMarkdown } from "@/lib/markdown";
import { getShikiCodeRenderer, type CodeBlockRenderer } from "@/lib/shiki";
import { formatClock } from "@/lib/time";
import { cn } from "@/lib/utils";
import {
  isTurnPhaseActive,
  type AssistantOverlay,
  type PendingUserMessage,
  type TurnPhaseState,
  useOverlayStore,
} from "@/state/overlayStore";

const TURNS_PAGE_SIZE = 25;
const EMPTY_PENDING: PendingUserMessage[] = [];
const EMPTY_MESSAGES: Message[] = [];

type TranscriptProps = {
  token: string;
  sessionID: string;
  sessionRunning?: boolean;
  submitError?: string | null;
};

export function Transcript({ token, sessionID, sessionRunning = false, submitError }: TranscriptProps) {
  const { t } = useI18n();
  const markAssistantRevealed = useOverlayStore((state) => state.markAssistantRevealed);
  const reconcileMessages = useOverlayStore((state) => state.reconcileMessages);
  const pendingUsersBySession = useOverlayStore((state) => state.pendingUsers);
  const assistantsByID = useOverlayStore((state) => state.assistants);
  const turnPhase = useOverlayStore((state) => state.turnPhases[sessionID]);
  const turnsQuery = useInfiniteQuery({
    queryKey: queryKeys.turns(sessionID),
    queryFn: ({ pageParam }) => listTurns(token, sessionID, { before: pageParam, limit: TURNS_PAGE_SIZE }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => {
      if (!lastPage?.hasMore || !lastPage.turns?.length) {
        return undefined;
      }
      return lastPage.turns[0].id;
    },
    getPreviousPageParam: () => undefined,
    enabled: Boolean(token && sessionID),
  });
  const messages = useMemo(
    () => turnsQuery.data?.pages.slice().reverse().flatMap((page) => page.turns.flatMap((turn) => turn.messages)) || EMPTY_MESSAGES,
    [turnsQuery.data],
  );
  const pendingUsers = pendingUsersBySession[sessionID] || EMPTY_PENDING;
  const assistantOverlays = useMemo(
    () => Object.values(assistantsByID).filter((overlay) => overlay.sessionID === sessionID),
    [assistantsByID, sessionID],
  );
  const displayPhase = useMemo<TurnPhaseState | undefined>(() => {
    if (isTurnPhaseActive(turnPhase)) {
      return turnPhase;
    }
    if (!sessionRunning || assistantOverlays.length > 0) {
      return undefined;
    }
    return {
      phase: "awaiting_model",
      sessionID,
      updatedAt: "",
    };
  }, [assistantOverlays.length, sessionID, sessionRunning, turnPhase]);

  useEffect(() => {
    if (!turnsQuery.isSuccess) {
      return;
    }
    reconcileMessages(sessionID, messages);
  }, [messages, reconcileMessages, sessionID, turnsQuery.isSuccess]);

  const canonicalMessageIDs = useMemo(() => new Set(messages.map((message) => message.id)), [messages]);
  const heldOverlayTurnIDsKey = useMemo(
    () =>
      assistantOverlays
        .filter((overlay) => overlay.assistantMessageID && canonicalMessageIDs.has(overlay.assistantMessageID) && !overlay.revealed)
        .map((overlay) => overlay.turnID)
        .sort()
        .join("\n"),
    [assistantOverlays, canonicalMessageIDs],
  );
  const visibleAssistantOverlays = useMemo(
    () =>
      assistantOverlays.filter(
        (overlay) => !overlay.assistantMessageID || !canonicalMessageIDs.has(overlay.assistantMessageID) || !overlay.revealed,
      ),
    [assistantOverlays, canonicalMessageIDs],
  );
  const phaseHasOverlay = Boolean(
    displayPhase &&
      (displayPhase.turnID
        ? visibleAssistantOverlays.some((overlay) => overlay.turnID === displayPhase.turnID)
        : visibleAssistantOverlays.some((overlay) => overlay.status === "streaming")),
  );
  const phaseItem = displayPhase && !phaseHasOverlay ? displayPhase : undefined;
  const canonicalItemKeys = useMemo(() => {
    const held = setFromKey(heldOverlayTurnIDsKey);
    return messages
      .filter((message) => !(message.role === "assistant" && message.turnID && held.has(message.turnID)))
      .map(messageItemKey);
  }, [heldOverlayTurnIDsKey, messages]);
  const pendingItemKeys = useMemo(() => pendingUsers.map((message) => `user:${message.clientMessageID}`), [pendingUsers]);
  const overlayItemKeys = useMemo(
    () => visibleAssistantOverlays.map((overlay) => `assistant:${overlay.turnID}`),
    [visibleAssistantOverlays],
  );
  const itemKeys = useMemo(
    () => [
      ...canonicalItemKeys,
      ...pendingItemKeys,
      ...overlayItemKeys,
      ...(phaseItem ? [transcriptPhaseKey(phaseItem)] : []),
    ],
    [canonicalItemKeys, overlayItemKeys, pendingItemKeys, phaseItem],
  );
  const hasItems =
    canonicalItemKeys.length > 0 || pendingUsers.length > 0 || visibleAssistantOverlays.length > 0 || Boolean(phaseItem);
  const scroll = useTranscriptScroll({ itemKeys, sessionID });
  const { captureAnchor } = scroll;
  const handleAssistantRevealComplete = useCallback(
    (turnID: string) => {
      markAssistantRevealed(turnID);
      window.requestAnimationFrame(() => {
        scroll.stickToBottomIfNeeded({ stabilizeFrames: 4 });
      });
    },
    [markAssistantRevealed, scroll.stickToBottomIfNeeded],
  );
  const pendingIDsRef = useRef<Set<string>>(new Set());
  const topSentinelRef = useRef<HTMLDivElement | null>(null);

  useLayoutEffect(() => {
    const previous = pendingIDsRef.current;
    const next = new Set(pendingUsers.map((message) => message.clientMessageID));
    pendingIDsRef.current = next;
    if (pendingUsers.some((message) => !previous.has(message.clientMessageID))) {
      scroll.enterBottomMode({ stabilizeFrames: 1 });
    }
  }, [pendingUsers, scroll.enterBottomMode]);

  useEffect(() => {
    const sentinel = topSentinelRef.current;
    const root = sentinel?.closest("[data-transcript-viewport]") as HTMLElement | null;
    if (!sentinel || !turnsQuery.hasNextPage || turnsQuery.isFetchingNextPage) {
      return;
    }
    if (!root) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) {
          return;
        }
        if (!turnsQuery.hasNextPage || turnsQuery.isFetchingNextPage) {
          return;
        }
        captureAnchor();
        void turnsQuery.fetchNextPage();
      },
      { root, rootMargin: "160px 0px 0px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [
    captureAnchor,
    turnsQuery.fetchNextPage,
    turnsQuery.hasNextPage,
    turnsQuery.isFetchingNextPage,
  ]);

  if (turnsQuery.isPending) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center text-muted-foreground">
        <Loader2 className="size-5 animate-spin" aria-label={t("common.loading")} />
      </div>
    );
  }

  if (!turnsQuery.isLoading && !turnsQuery.isError && !hasItems && !submitError) {
    return <div className="min-h-0 flex-1" />;
  }

  return (
    // overflow-hidden:WKWebView 下文字字形渲染会溢出滚动 viewport 边界,
    // 在 composer 上沿漏出白色文字边缘,这里裁掉(浏览器无此问题但无害)
    <div className="relative min-h-0 flex-1 overflow-hidden">
      <div
        ref={scroll.viewportRef}
        className="h-full overflow-y-auto overscroll-contain"
        data-transcript-viewport
      >
        <div ref={scroll.contentRef}>
          <ChatColumn className="grid gap-4 pt-4 pb-8">
            <div ref={topSentinelRef} className="h-px" aria-hidden="true" />
            {turnsQuery.isFetchingNextPage ? (
              <div className="flex justify-center py-2 text-muted-foreground">
                <Loader2 className="size-4 animate-spin" aria-label={t("common.loading")} />
              </div>
            ) : null}
            {turnsQuery.isError ? (
              <Alert variant="destructive">
                <CircleAlert className="h-3.5 w-3.5" />
                <AlertDescription>{t("transcript.loadFailed")}</AlertDescription>
              </Alert>
            ) : null}
            <CanonicalMessageItems hiddenAssistantTurnIDsKey={heldOverlayTurnIDsKey} messages={messages} />
            {pendingUsers.map((item) => {
              const itemKey = `user:${item.clientMessageID}`;
              return (
                <div
                  key={itemKey}
                  className="min-w-0"
                  data-transcript-item-id={itemKey}
                  data-transcript-item-role="user"
                >
                  <PendingUserItem text={item.text} />
                </div>
              );
            })}
            {visibleAssistantOverlays.map((overlay) => {
              const itemKey = `assistant:${overlay.turnID}`;
              return (
                <div
                  key={itemKey}
                  className="min-w-0"
                  data-transcript-item-id={itemKey}
                  data-transcript-item-role="assistant"
                >
                  <AssistantOverlayItem
                    canonicalReady={Boolean(
                      overlay.assistantMessageID && canonicalMessageIDs.has(overlay.assistantMessageID),
                    )}
                    overlay={overlay}
                    phase={displayPhase?.turnID === overlay.turnID ? displayPhase : undefined}
                    onContentGrow={scroll.stickToBottomIfNeeded}
                    onRevealComplete={handleAssistantRevealComplete}
                  />
                </div>
              );
            })}
            {phaseItem ? (
              <div
                key={transcriptPhaseKey(phaseItem)}
                className="min-w-0"
                data-transcript-item-id={transcriptPhaseKey(phaseItem)}
                data-transcript-item-role="assistant"
              >
                <AssistantPhaseItem phase={phaseItem.phase} />
              </div>
            ) : null}
            {submitError ? (
              <Alert variant="destructive">
                <CircleAlert className="h-3.5 w-3.5" />
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            ) : null}
          </ChatColumn>
        </div>
      </div>
      {scroll.showJumpLatest && hasItems ? (
        <Button
          aria-label={t("transcript.jumpLatest")}
          className="absolute right-5 bottom-5 rounded-full border border-border bg-card shadow-md hover:bg-muted"
          size="icon"
          type="button"
          variant="ghost"
          onClick={() => scroll.enterBottomMode({ stabilizeFrames: 1 })}
        >
          <ArrowDown />
        </Button>
      ) : null}
    </div>
  );
}

function setFromKey(key: string) {
  return new Set(key ? key.split("\n") : []);
}

function messageItemKey(message: Message) {
  if (message.role === "user" && message.clientMessageID) {
    return `user:${message.clientMessageID}`;
  }
  if (message.role === "assistant" && message.turnID) {
    return `assistant:${message.turnID}`;
  }
  return `message:${message.id}`;
}

function transcriptPhaseKey(phase: TurnPhaseState) {
  if (phase.turnID) {
    return `assistant:${phase.turnID}`;
  }
  if (phase.clientMessageID) {
    return `assistant:pending:${phase.clientMessageID}`;
  }
  return `assistant:phase:${phase.sessionID}`;
}

const CanonicalMessageItems = memo(function CanonicalMessageItems({
  hiddenAssistantTurnIDsKey,
  messages,
}: {
  hiddenAssistantTurnIDsKey: string;
  messages: Message[];
}) {
  const hiddenAssistantTurnIDs = useMemo(() => setFromKey(hiddenAssistantTurnIDsKey), [hiddenAssistantTurnIDsKey]);
  return (
    <>
      {messages.map((message) => {
        if (message.role === "assistant" && message.turnID && hiddenAssistantTurnIDs.has(message.turnID)) {
          return null;
        }
        const itemKey = messageItemKey(message);
        return (
          <div
            key={itemKey}
            className="min-w-0"
            data-transcript-item-id={itemKey}
            data-transcript-item-role={message.role}
          >
            <MessageItem message={message} />
          </div>
        );
      })}
    </>
  );
});

// 任务流的渲染单位是 turn parts(docs/design.md 3.2):text-only 阶段只有
// text part,thought / tool 随事件协议 part 维度落地后在此 switch 扩展,
// 不改动任务流骨架。
type TurnPart =
  | { type: "text"; text: string }
  | { type: "thought"; text: string; active?: boolean }
  | {
      type: "tool_use";
      id?: string;
      name?: string;
      args?: unknown;
      argsText?: string;
      active?: boolean;
      dotPhase?: TurnPhaseState["phase"];
      phase?: "streaming_args" | "running" | "ok" | "error";
      summary?: string;
    }
  | { type: "tool_result"; id?: string; ok?: boolean; content?: string };

function partsFromText(text: string): TurnPart[] {
  return text ? [{ type: "text", text }] : [];
}

function partsFromMessage(message: Message): TurnPart[] {
  if (message.parts.length > 0) {
    return message.parts.map(partFromContentPart);
  }
  return partsFromText(message.text);
}

function partFromContentPart(part: ContentPart): TurnPart {
  switch (part.type) {
    case "text":
      return { type: "text", text: part.text };
    case "thought":
      return { type: "thought", text: part.text };
    case "tool_use":
      return { type: "tool_use", args: part.args, id: part.id, name: part.name };
    case "tool_result":
      return { type: "tool_result", content: part.content, id: part.id, ok: part.ok };
  }
}

function partsFromOverlay(
  overlay: AssistantOverlay,
  streamedText: string,
  activePhaseName: TurnPhaseState["phase"] | undefined,
): TurnPart[] {
  return [
    ...(overlay.thought ? [{ type: "thought" as const, active: activePhaseName === "thinking", text: overlay.thought }] : []),
    ...overlay.tools.map((tool, index) => {
      const active =
        index === overlay.tools.length - 1 &&
        (activePhaseName === "streaming_tool_args" ||
          activePhaseName === "executing_tool" ||
          activePhaseName === "awaiting_followup");
      return {
        type: "tool_use" as const,
        active,
        argsText: tool.argsText,
        dotPhase: active ? activePhaseName : toolPhaseDot(tool.phase),
        id: tool.callID,
        name: tool.name,
        phase: tool.phase,
        summary: tool.summary,
      };
    }),
    ...partsFromText(streamedText),
  ];
}

function toolPhaseDot(phase: Extract<TurnPart, { type: "tool_use" }>["phase"]): TurnPhaseState["phase"] {
  switch (phase) {
    case "streaming_args":
      return "streaming_tool_args";
    case "running":
      return "executing_tool";
    case "error":
      return "error";
    case "ok":
    default:
      return "executing_tool";
  }
}

function TurnParts({ parts }: { parts: TurnPart[] }) {
  return (
    <>
      {parts.map((part, index) => {
        switch (part.type) {
          case "text":
            return <MarkdownBody key={index} text={part.text} />;
          case "thought":
            return <ThoughtPart key={index} active={part.active} text={part.text} />;
          case "tool_use":
            return <ToolUsePart key={index} part={part} />;
          case "tool_result":
            return <ToolResultPart key={index} part={part} />;
        }
      })}
    </>
  );
}

const MessageItem = memo(function MessageItem({ message }: { message: Message }) {
  if (message.role === "user") {
    return <UserMessageBlock createdAt={message.createdAt} interrupted={message.interrupted} text={message.text} />;
  }
  return (
    <div className="group flex flex-col">
      <div className="selectable-text min-w-0 text-sm leading-6">
        <TurnParts parts={partsFromMessage(message)} />
        {message.interrupted ? <InterruptedBadge /> : null}
      </div>
      <MessageMeta createdAt={message.createdAt} text={message.text} />
    </div>
  );
});

// 用户消息沿用聊天气泡:靠右,meta 也贴右侧;助手消息保持正文流。
function UserMessageBlock({
  text,
  createdAt,
  interrupted,
  pending,
}: {
  text: string;
  createdAt?: string;
  interrupted?: boolean;
  pending?: boolean;
}) {
  return (
    <div className={cn("group flex flex-col items-end", pending && "opacity-70")}>
      <div className="pudding-user-message selectable-text min-w-0 max-w-[min(82%,42rem)] rounded-2xl rounded-br-md border border-border/60 px-3 py-2 text-left text-sm leading-6 break-words whitespace-pre-wrap shadow-sm">
        {text}
        {interrupted ? <InterruptedBadge /> : null}
      </div>
      {createdAt ? <MessageMeta align="end" createdAt={createdAt} text={text} /> : null}
    </div>
  );
}

// 入场动效只给"新出现"的内容(pending 用户消息 / streaming overlay):
// canonical 消息不动效,避免历史加载整页齐闪、overlay→canonical 交接重闪;
// reduced-motion 由全局规则降级(styles.css)
const PendingUserItem = memo(function PendingUserItem({ text }: { text: string }) {
  return (
    <div className="animate-in duration-150 fade-in slide-in-from-bottom-1">
      <UserMessageBlock pending text={text} />
    </div>
  );
});

function AssistantOverlayItem({
  canonicalReady = false,
  overlay,
  phase,
  onContentGrow,
  onRevealComplete,
}: {
  canonicalReady?: boolean;
  overlay: AssistantOverlay;
  phase?: TurnPhaseState;
  onContentGrow?: () => void;
  onRevealComplete?: (turnID: string) => void;
}) {
  const streaming = overlay.status === "streaming";
  // 平滑揭示:把 store 里逐 delta 累积的全量 overlay.text 按 rAF 匀速放出,
  // 抹平 provider/proxy 的 chunk 粗细;非 streaming 立即 snap 到全量。
  const text = useStreamedText(overlay.text, streaming);
  const revealingText = text.length < overlay.text.length;
  const activePhaseName: TurnPhaseState["phase"] | undefined =
    phase && isTurnPhaseActive(phase)
      ? phase.phase
      : streaming || revealingText
        ? overlay.text
          ? "streaming_text"
          : overlay.thought
            ? "thinking"
            : overlay.tools.length > 0
              ? "streaming_tool_args"
              : "awaiting_model"
        : undefined;
  const phaseCarriedByPart =
    (activePhaseName === "thinking" && Boolean(overlay.thought)) ||
    ((activePhaseName === "streaming_tool_args" ||
      activePhaseName === "executing_tool" ||
      activePhaseName === "awaiting_followup") &&
      overlay.tools.length > 0);
  const parts = useMemo(() => partsFromOverlay(overlay, text, activePhaseName), [activePhaseName, overlay, text]);
  const handoffPending =
    overlay.status === "completed" && Boolean(overlay.assistantMessageID) && !overlay.revealed && text === overlay.text;
  const footerPhaseName = phaseCarriedByPart ? undefined : activePhaseName || (handoffPending ? "streaming_text" : undefined);

  // 每帧揭示后贴底跟随(仅在用户处于 bottom mode 时由滚动 hook 判定)
  useEffect(() => {
    onContentGrow?.();
  }, [overlay.thought, overlay.tools, text, onContentGrow]);
  useLayoutEffect(() => {
    const waitingForCanonical = overlay.status === "completed" && Boolean(overlay.assistantMessageID) && !canonicalReady;
    if (streaming || overlay.revealed || text !== overlay.text) {
      return;
    }
    if (waitingForCanonical) {
      return;
    }
    onRevealComplete?.(overlay.turnID);
  }, [
    canonicalReady,
    onRevealComplete,
    overlay.assistantMessageID,
    overlay.revealed,
    overlay.status,
    overlay.text,
    overlay.turnID,
    streaming,
    text,
  ]);

  return (
    <div className="selectable-text animate-in min-w-0 text-sm leading-6 duration-150 fade-in slide-in-from-bottom-1">
      <div className="min-w-0">{parts.length > 0 ? <TurnParts parts={parts} /> : null}</div>
      {footerPhaseName ? <AssistantPhaseItem phase={footerPhaseName} /> : null}
      {overlay.status === "failed" && overlay.error ? (
        <Alert className="mt-2" variant="destructive">
          <CircleAlert className="h-3.5 w-3.5" />
          <AlertDescription>{overlay.error}</AlertDescription>
        </Alert>
      ) : null}
      {overlay.status === "cancelled" || overlay.interrupted ? <InterruptedBadge /> : null}
    </div>
  );
}

function AssistantPhaseItem({ phase }: { phase: TurnPhaseState["phase"] }) {
  return (
    <div className="flex h-6 w-full items-center gap-2 text-xs text-muted-foreground">
      <span className="flex h-6 w-2.5 items-center justify-center">
        <PhaseDot phase={phase} />
      </span>
    </div>
  );
}

function ThoughtPart({ active = false, text }: { active?: boolean; text: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const bodyRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!active || !open || !bodyRef.current) {
      return;
    }
    bodyRef.current.scrollTop = bodyRef.current.scrollHeight;
  }, [active, open, text]);

  return (
    <details
      className="relative text-[12px] leading-[1.5] text-muted-foreground"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      {open ? <span aria-hidden="true" className="pointer-events-none absolute top-[18px] bottom-0 left-[5px] border-l border-border" /> : null}
      <summary className="inline-grid h-6 cursor-pointer list-none grid-cols-[0.625rem_auto] items-center gap-1 pr-1 outline-none hover:text-foreground [&::-webkit-details-marker]:hidden">
        <span className="relative z-[1] inline-flex h-6 w-2.5 shrink-0 items-center justify-center opacity-90">
          <PhaseDot active={active} phase="thinking" size="md" />
        </span>
        <span className="flex min-w-0 flex-1 items-center gap-1">
          <span className="shrink-0 truncate">{active ? t("transcript.thinking") : t("transcript.thought")}</span>
          <span className="shrink-0 text-muted-foreground/50">
            {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
          </span>
        </span>
      </summary>
      <div className="ml-[5px] py-1 pl-2">
        <div
          ref={bodyRef}
          className="max-h-72 overflow-y-auto whitespace-pre-wrap break-words pr-2 text-[12px] leading-6 text-muted-foreground italic"
        >
          {text}
        </div>
      </div>
    </details>
  );
}

function ToolUsePart({ part }: { part: Extract<TurnPart, { type: "tool_use" }> }) {
  const { t } = useI18n();
  const args = formatToolArgs(part.argsText || part.args);
  return (
    <div className="my-2 rounded-md border border-border/60 bg-card px-3 py-2 text-xs text-muted-foreground">
      <div className="flex h-6 min-w-0 items-center gap-2">
        <span className="inline-flex h-6 w-2.5 shrink-0 items-center justify-center">
          <PhaseDot active={part.active} phase={part.dotPhase || "executing_tool"} size="md" />
        </span>
        <span className="truncate font-medium text-foreground">{part.name || t("transcript.tool")}</span>
        {part.phase ? (
          <Badge className="h-5 px-1.5 text-[10px]" variant="outline">
            {part.phase}
          </Badge>
        ) : null}
      </div>
      {args ? (
        <pre className="mt-2 max-h-40 overflow-auto rounded bg-muted/40 p-2 font-mono text-[11px] leading-4 whitespace-pre-wrap">
          {`${t("transcript.toolArgs")}: ${args}`}
        </pre>
      ) : null}
      {part.summary ? <div className="mt-2 leading-5">{part.summary}</div> : null}
    </div>
  );
}

function ToolResultPart({ part }: { part: Extract<TurnPart, { type: "tool_result" }> }) {
  const { t } = useI18n();
  if (!part.content) {
    return null;
  }
  return (
    <div className="my-2 rounded-md border border-border/60 bg-muted/25 px-3 py-2 text-xs text-muted-foreground">
      <div className="mb-1 flex h-6 items-center gap-2 font-medium">
        <span className="inline-flex h-6 w-2.5 shrink-0 items-center justify-center">
          <PhaseDot active={false} phase="executing_tool" size="md" />
        </span>
        <span>{t("transcript.toolResult")}</span>
      </div>
      <pre className="max-h-48 overflow-auto font-mono text-[11px] leading-4 whitespace-pre-wrap">{part.content}</pre>
    </div>
  );
}

function formatToolArgs(value: unknown) {
  if (value == null || value === "") {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function InterruptedBadge() {
  const { t } = useI18n();
  return (
    <div className="mt-2">
      <Badge variant="outline">{t("transcript.interrupted")}</Badge>
    </div>
  );
}

function MarkdownBody({ text }: { text: string }) {
  const { t } = useI18n();
  const [codeRenderer, setCodeRenderer] = useState<CodeBlockRenderer | null>(null);
  useEffect(() => {
    let cancelled = false;
    void getShikiCodeRenderer().then((renderer) => {
      if (!cancelled) {
        setCodeRenderer(() => renderer);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);
  const html = useMemo(
    () =>
      renderMarkdown(text, {
        codeCopiedLabel: t("common.copied"),
        codeCopyLabel: t("common.copy"),
        codeRenderer: codeRenderer || undefined,
      }),
    [codeRenderer, t, text],
  );
  const handleClick = (event: MouseEvent<HTMLDivElement>) => {
    const button = (event.target as HTMLElement).closest<HTMLButtonElement>('button[data-action="copy-code"]');
    if (!button) {
      return;
    }
    const code = button.closest(".code-block-wrap")?.querySelector("code");
    if (!code) {
      return;
    }
    void navigator.clipboard
      .writeText(code.textContent || "")
      .then(() => {
        button.dataset.copied = "1";
        button.setAttribute("aria-label", button.dataset.copiedLabel || t("common.copied"));
        window.setTimeout(() => {
          delete button.dataset.copied;
          button.setAttribute("aria-label", button.dataset.copyLabel || t("common.copy"));
        }, 1500);
      })
      .catch(() => {});
  };
  return <div className="pudding-markdown" dangerouslySetInnerHTML={{ __html: html }} onClick={handleClick} />;
}

function MessageMeta({ align = "start", createdAt, text }: { align?: "start" | "end"; createdAt: string; text: string }) {
  const { t } = useI18n();
  // 复制成功反馈(旧项目交互):按钮就地变绿色对勾 ~1.5s,不弹 toast
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | null>(null);
  useEffect(() => {
    return () => {
      if (resetTimer.current) {
        window.clearTimeout(resetTimer.current);
      }
    };
  }, []);
  return (
    <div
      className={cn(
        "flex h-6 w-full items-center gap-2 text-xs text-muted-foreground opacity-0 group-hover:opacity-100 group-focus-within:opacity-100",
        align === "end" && "justify-end",
      )}
    >
      <Button
        aria-label={t("common.copy")}
        className={cn(
          "size-6 bg-transparent transition-colors hover:bg-muted dark:hover:bg-muted/50 active:translate-y-0",
          align === "start" && "-ml-1",
        )}
        size="icon-xs"
        type="button"
        variant="ghost"
        onClick={() => {
          void navigator.clipboard.writeText(text).then(() => {
            setCopied(true);
            if (resetTimer.current) {
              window.clearTimeout(resetTimer.current);
            }
            resetTimer.current = window.setTimeout(() => setCopied(false), 1500);
          });
        }}
      >
        {copied ? <Check className="text-success" /> : <Copy />}
      </Button>
      <span>{formatClock(createdAt)}</span>
    </div>
  );
}
