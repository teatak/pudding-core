import { useQuery } from "@tanstack/react-query";
import { ArrowDown, Check, CircleAlert, Copy } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { listMessages, type Message } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Mascot } from "@/components/Mascot";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/i18n";
import { renderMarkdown } from "@/lib/markdown";
import { formatClock } from "@/lib/time";
import { cn } from "@/lib/utils";
import { type AssistantOverlay, type PendingUserMessage, useOverlayStore } from "@/state/overlayStore";

const EMPTY_PENDING: PendingUserMessage[] = [];
const EMPTY_MESSAGES: Message[] = [];

type TranscriptProps = {
  token: string;
  sessionID: string;
};

type TranscriptItem =
  | { kind: "message"; message: Message }
  | { kind: "pending"; id: string; text: string; createdAt: string }
  | { kind: "assistant"; overlay: AssistantOverlay };

export function Transcript({ token, sessionID }: TranscriptProps) {
  const { t } = useI18n();
  const scrollAreaRef = useRef<HTMLDivElement | null>(null);
  const [followingBottom, setFollowingBottom] = useState(true);
  const reconcileMessages = useOverlayStore((state) => state.reconcileMessages);
  const pendingUsersBySession = useOverlayStore((state) => state.pendingUsers);
  const assistantsByID = useOverlayStore((state) => state.assistants);
  const messagesQuery = useQuery({
    queryKey: queryKeys.messages(sessionID),
    queryFn: () => listMessages(token, sessionID),
    enabled: Boolean(token && sessionID),
  });
  const messages = messagesQuery.data?.messages || EMPTY_MESSAGES;
  const pendingUsers = pendingUsersBySession[sessionID] || EMPTY_PENDING;
  const assistantOverlays = useMemo(
    () => Object.values(assistantsByID).filter((overlay) => overlay.sessionID === sessionID),
    [assistantsByID, sessionID],
  );

  useEffect(() => {
    reconcileMessages(sessionID, messages);
  }, [messages, reconcileMessages, sessionID]);

  const viewport = useCallback(() => {
    return scrollAreaRef.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]') ?? null;
  }, []);

  const scrollToBottom = useCallback(() => {
    window.requestAnimationFrame(() => {
      const node = viewport();
      if (!node) {
        return;
      }
      node.scrollTop = node.scrollHeight;
      setFollowingBottom(true);
    });
  }, [viewport]);

  useEffect(() => {
    setFollowingBottom(true);
    scrollToBottom();
  }, [scrollToBottom, sessionID]);

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

  useEffect(() => {
    const node = viewport();
    if (!node) {
      return;
    }
    const onScroll = () => {
      const distance = node.scrollHeight - node.scrollTop - node.clientHeight;
      setFollowingBottom(distance < 48);
    };
    node.addEventListener("scroll", onScroll);
    onScroll();
    return () => node.removeEventListener("scroll", onScroll);
  }, [viewport, sessionID]);

  useEffect(() => {
    if (followingBottom) {
      scrollToBottom();
    }
  }, [followingBottom, items, scrollToBottom]);

  // 空消息态独立渲染:不进 ScrollArea(min-h 撑高会出滚动条),flex 居中填满
  if (!messagesQuery.isLoading && !messagesQuery.isError && items.length === 0) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 text-center text-sm text-muted-foreground">
        <Mascot className="w-16" />
        <div>{t("session.start")}</div>
      </div>
    );
  }

  return (
    // overflow-hidden:WKWebView 下文字字形渲染会溢出滚动 viewport 边界,
    // 在 composer 上沿漏出白色文字边缘,这里裁掉(浏览器无此问题但无害)
    <div ref={scrollAreaRef} className="relative min-h-0 flex-1 overflow-hidden">
      <ScrollArea className="h-full">
        <div className="mx-auto grid w-full max-w-3xl gap-4 px-5 py-5">
          {messagesQuery.isLoading ? <TranscriptSkeleton /> : null}
          {messagesQuery.isError ? (
            <Alert variant="destructive">
              <CircleAlert className="h-3.5 w-3.5" />
              <AlertDescription>{t("transcript.loadFailed")}</AlertDescription>
            </Alert>
          ) : null}
          {items.map((item) => {
            if (item.kind === "message") {
              return <MessageItem key={item.message.id} message={item.message} />;
            }
            if (item.kind === "pending") {
              return <PendingUserItem key={item.id} text={item.text} />;
            }
            return <AssistantOverlayItem key={item.overlay.turnID} overlay={item.overlay} />;
          })}
        </div>
      </ScrollArea>
      {!followingBottom && items.length > 0 ? (
        <Button
          aria-label={t("transcript.jumpLatest")}
          className="absolute right-5 bottom-5 rounded-full shadow-md"
          size="icon"
          type="button"
          variant="outline"
          onClick={scrollToBottom}
        >
          <ArrowDown />
        </Button>
      ) : null}
    </div>
  );
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
      <div className="min-w-0 text-sm leading-6">
        <TurnParts parts={partsFromText(message.text)} />
        {message.interrupted ? <InterruptedBadge /> : null}
      </div>
      <MessageMeta createdAt={message.createdAt} text={message.text} />
    </div>
  );
}

// 用户消息是"任务指令":全宽块 + 左侧主色细条,不做聊天气泡
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
    <div className={cn("group flex flex-col", pending && "opacity-70")}>
      <div className="rounded-r-lg border-l-2 border-primary bg-secondary/60 px-3.5 py-2 text-sm leading-6 whitespace-pre-wrap">
        {text}
        {interrupted ? <InterruptedBadge /> : null}
      </div>
      {createdAt ? <MessageMeta className="pl-3.5" createdAt={createdAt} text={text} /> : null}
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

function AssistantOverlayItem({ overlay }: { overlay: AssistantOverlay }) {
  return (
    <div className="animate-in min-w-0 text-sm leading-6 duration-150 fade-in slide-in-from-bottom-1">
      {overlay.text ? <TurnParts parts={partsFromText(overlay.text)} /> : null}
      {overlay.status === "streaming" ? (
        <span className="ml-1 inline-block animate-pulse text-primary">▍</span>
      ) : null}
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

function MessageMeta({ createdAt, text, className }: { createdAt: string; text: string; className?: string }) {
  const { t } = useI18n();
  // 复制成功的双反馈(旧项目交互):按钮就地变绿色对勾 ~1.5s + toast 回执
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
        "flex items-center gap-2 text-xs text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100",
        className,
      )}
    >
      <span>{formatClock(createdAt)}</span>
      <Button
        aria-label={t("common.copy")}
        size="icon-xs"
        type="button"
        variant="ghost"
        onClick={() => {
          void navigator.clipboard.writeText(text).then(() => {
            toast(t("common.copied"));
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
    </div>
  );
}

// 骨架贴任务流版式:用户指令块 + 文本行,不再是聊天气泡形状
function TranscriptSkeleton() {
  return (
    <div className="grid gap-4">
      <Skeleton className="h-12 w-full rounded-r-lg" />
      <div className="grid gap-2">
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    </div>
  );
}
