import { useInfiniteQuery } from "@tanstack/react-query";
import { ArrowDown, Check, CircleAlert, Copy, Loader2 } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { listMessages, type Message } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useStreamedText } from "@/hooks/useStreamedText";
import { useTranscriptScroll } from "@/hooks/useTranscriptScroll";
import { useI18n } from "@/i18n";
import { renderMarkdown } from "@/lib/markdown";
import { formatClock } from "@/lib/time";
import { cn } from "@/lib/utils";
import { type AssistantOverlay, type PendingUserMessage, useOverlayStore } from "@/state/overlayStore";

const MESSAGES_PAGE_SIZE = 50;
const EMPTY_PENDING: PendingUserMessage[] = [];
const EMPTY_MESSAGES: Message[] = [];

type TranscriptProps = {
  token: string;
  sessionID: string;
  submitError?: string | null;
};

type TranscriptItem =
  | { kind: "message"; message: Message }
  | { kind: "pending"; id: string; text: string; createdAt: string }
  | { kind: "assistant"; overlay: AssistantOverlay };

export function Transcript({ token, sessionID, submitError }: TranscriptProps) {
  const { t } = useI18n();
  const reconcileMessages = useOverlayStore((state) => state.reconcileMessages);
  const pendingUsersBySession = useOverlayStore((state) => state.pendingUsers);
  const assistantsByID = useOverlayStore((state) => state.assistants);
  const messagesQuery = useInfiniteQuery({
    queryKey: queryKeys.messages(sessionID),
    queryFn: ({ pageParam }) => listMessages(token, sessionID, { before: pageParam, limit: MESSAGES_PAGE_SIZE }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => {
      if (!lastPage?.hasMore || !lastPage.messages?.length) {
        return undefined;
      }
      return lastPage.messages[0].id;
    },
    getPreviousPageParam: () => undefined,
    enabled: Boolean(token && sessionID),
  });
  const messages = useMemo(
    () => messagesQuery.data?.pages.slice().reverse().flatMap((page) => page.messages) || EMPTY_MESSAGES,
    [messagesQuery.data],
  );
  const pendingUsers = pendingUsersBySession[sessionID] || EMPTY_PENDING;
  const assistantOverlays = useMemo(
    () => Object.values(assistantsByID).filter((overlay) => overlay.sessionID === sessionID),
    [assistantsByID, sessionID],
  );

  useEffect(() => {
    if (!messagesQuery.isSuccess) {
      return;
    }
    reconcileMessages(sessionID, messages);
  }, [messages, messagesQuery.isSuccess, reconcileMessages, sessionID]);

  const items = useMemo<TranscriptItem[]>(() => {
    // overlay 在对应 canonical message 出现后退场("等数据到达再清"由 store 对账,
    // 这里只做渲染期过滤,避免短暂双显)
    const canonicalIDs = new Set(messages.map((message) => message.id));
    return [
      ...messages.map((message) => ({ kind: "message" as const, message })),
      ...pendingUsers.map((message) => ({
        kind: "pending" as const,
        id: message.clientMessageID,
        text: message.text,
        createdAt: message.createdAt,
      })),
      ...assistantOverlays
        .filter((overlay) => !overlay.assistantMessageID || !canonicalIDs.has(overlay.assistantMessageID))
        .map((overlay) => ({ kind: "assistant" as const, overlay })),
    ];
  }, [assistantOverlays, messages, pendingUsers]);
  const itemKeys = useMemo(() => items.map(transcriptItemKey), [items]);
  const scroll = useTranscriptScroll({ itemKeys, sessionID });
  const { captureAnchor } = scroll;
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
    if (!sentinel || !messagesQuery.hasNextPage || messagesQuery.isFetchingNextPage) {
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
        if (!messagesQuery.hasNextPage || messagesQuery.isFetchingNextPage) {
          return;
        }
        captureAnchor();
        void messagesQuery.fetchNextPage();
      },
      { root, rootMargin: "160px 0px 0px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [
    captureAnchor,
    messagesQuery.fetchNextPage,
    messagesQuery.hasNextPage,
    messagesQuery.isFetchingNextPage,
  ]);

  if (messagesQuery.isPending) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center text-muted-foreground">
        <Loader2 className="size-5 animate-spin" aria-label={t("common.loading")} />
      </div>
    );
  }

  if (!messagesQuery.isLoading && !messagesQuery.isError && items.length === 0 && !submitError) {
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
            {messagesQuery.isFetchingNextPage ? (
              <div className="flex justify-center py-2 text-muted-foreground">
                <Loader2 className="size-4 animate-spin" aria-label={t("common.loading")} />
              </div>
            ) : null}
            {messagesQuery.isError ? (
              <Alert variant="destructive">
                <CircleAlert className="h-3.5 w-3.5" />
                <AlertDescription>{t("transcript.loadFailed")}</AlertDescription>
              </Alert>
            ) : null}
            {items.map((item) => {
              const itemKey = transcriptItemKey(item);
              if (item.kind === "message") {
                return (
                  <div
                    key={itemKey}
                    className="min-w-0"
                    data-transcript-item-id={itemKey}
                    data-transcript-item-role={item.message.role}
                  >
                    <MessageItem message={item.message} />
                  </div>
                );
              }
              if (item.kind === "pending") {
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
              }
              return (
                <div
                  key={itemKey}
                  className="min-w-0"
                  data-transcript-item-id={itemKey}
                  data-transcript-item-role="assistant"
                >
                  <AssistantOverlayItem
                    overlay={item.overlay}
                    onContentGrow={scroll.stickToBottomIfNeeded}
                  />
                </div>
              );
            })}
            {submitError ? (
              <Alert variant="destructive">
                <CircleAlert className="h-3.5 w-3.5" />
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            ) : null}
          </ChatColumn>
        </div>
      </div>
      {scroll.showJumpLatest && items.length > 0 ? (
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

function transcriptItemKey(item: TranscriptItem) {
  if (item.kind === "message") {
    if (item.message.role === "user" && item.message.clientMessageID) {
      return `user:${item.message.clientMessageID}`;
    }
    if (item.message.role === "assistant" && item.message.turnID) {
      return `assistant:${item.message.turnID}`;
    }
    return `message:${item.message.id}`;
  }
  if (item.kind === "pending") {
    return `user:${item.id}`;
  }
  return `assistant:${item.overlay.turnID}`;
}

// 任务流的渲染单位是 turn parts(docs/design.md 3.2):text-only 阶段只有
// text part,thought / tool 随事件协议 part 维度落地后在此 switch 扩展,
// 不改动任务流骨架。
type TurnPart = { type: "text"; text: string };

function partsFromText(text: string): TurnPart[] {
  return [{ type: "text", text }];
}

function TurnParts({ parts }: { parts: TurnPart[] }) {
  return (
    <>
      {parts.map((part, index) => {
        switch (part.type) {
          case "text":
            return <MarkdownBody key={index} text={part.text} />;
        }
      })}
    </>
  );
}

function MessageItem({ message }: { message: Message }) {
  if (message.role === "user") {
    return <UserMessageBlock createdAt={message.createdAt} interrupted={message.interrupted} text={message.text} />;
  }
  return (
    <div className="group flex flex-col">
      <div className="selectable-text min-w-0 text-sm leading-6">
        <TurnParts parts={partsFromText(message.text)} />
        {message.interrupted ? <InterruptedBadge /> : null}
      </div>
      <MessageMeta createdAt={message.createdAt} text={message.text} />
    </div>
  );
}

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
function PendingUserItem({ text }: { text: string }) {
  return (
    <div className="animate-in duration-150 fade-in slide-in-from-bottom-1">
      <UserMessageBlock pending text={text} />
    </div>
  );
}

function AssistantOverlayItem({
  overlay,
  onContentGrow,
}: {
  overlay: AssistantOverlay;
  onContentGrow?: () => void;
}) {
  const streaming = overlay.status === "streaming";
  // 平滑揭示:把 store 里逐 delta 累积的全量 overlay.text 按 rAF 匀速放出,
  // 抹平 provider/proxy 的 chunk 粗细;非 streaming 立即 snap 到全量。
  const text = useStreamedText(overlay.text, streaming);
  const cursorVisible = streaming;

  // 每帧揭示后贴底跟随(仅在用户处于 bottom mode 时由滚动 hook 判定)
  useEffect(() => {
    onContentGrow?.();
  }, [text, onContentGrow]);

  return (
    <div className="selectable-text animate-in min-w-0 text-sm leading-6 duration-150 fade-in slide-in-from-bottom-1">
      {text ? <TurnParts parts={partsFromText(text)} /> : null}
      <span
        aria-hidden="true"
        className={cn("ml-1 inline-block text-primary", cursorVisible ? "animate-pulse opacity-100" : "opacity-0")}
      >
        ▍
      </span>
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

function InterruptedBadge() {
  const { t } = useI18n();
  return (
    <div className="mt-2">
      <Badge variant="outline">{t("transcript.interrupted")}</Badge>
    </div>
  );
}

function MarkdownBody({ text }: { text: string }) {
  return <div className="pudding-markdown" dangerouslySetInnerHTML={{ __html: renderMarkdown(text) }} />;
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
          "size-6 bg-transparent transition-colors hover:bg-transparent active:translate-y-0",
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
