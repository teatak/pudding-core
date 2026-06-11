import { useQuery } from "@tanstack/react-query";
import { Bot, CircleAlert, User } from "lucide-react";
import { useEffect, useMemo } from "react";

import { listMessages, type Message } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { type AssistantOverlay, useOverlayStore } from "@/state/overlayStore";

const EMPTY_PENDING: [] = [];
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

  const items = useMemo<TranscriptItem[]>(() => {
    const canonicalIDs = new Set(messages.map((message) => message.id));
    const finalAssistantIDs = new Set(messages.map((message) => message.id));
    return [
      ...messages.map((message) => ({ kind: "message" as const, message })),
      ...pendingUsers.map((message) => ({
        kind: "pending" as const,
        id: message.clientMessageID,
        text: message.text,
        createdAt: message.createdAt,
      })),
      ...assistantOverlays
        .filter((overlay) => !overlay.assistantMessageID || !finalAssistantIDs.has(overlay.assistantMessageID))
        .map((overlay) => ({ kind: "assistant" as const, overlay })),
    ].filter((item) => item.kind !== "message" || canonicalIDs.has(item.message.id));
  }, [assistantOverlays, messages, pendingUsers]);

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto grid w-full max-w-4xl gap-3 px-5 py-5">
        {items.length === 0 ? (
          <div className="flex min-h-80 items-center justify-center text-sm text-muted-foreground">
            Start a conversation
          </div>
        ) : null}
        {items.map((item) => {
          if (item.kind === "message") {
            return <MessageBubble key={item.message.id} message={item.message} />;
          }
          if (item.kind === "pending") {
            return <PendingBubble key={item.id} text={item.text} />;
          }
          return <AssistantOverlayBubble key={item.overlay.turnID} overlay={item.overlay} />;
        })}
      </div>
    </ScrollArea>
  );
}

function MessageBubble({ message }: { message: Message }) {
  const isUser = message.role === "user";
  return (
    <div className={cn("flex gap-3", isUser && "justify-end")}>
      {!isUser ? <AvatarIcon assistant /> : null}
      <div
        className={cn(
          "max-w-[78%] whitespace-pre-wrap rounded-lg border px-3 py-2 text-sm leading-6 shadow-sm",
          isUser ? "bg-primary text-primary-foreground" : "bg-card",
        )}
      >
        {message.text}
        {message.interrupted ? <InterruptedBadge /> : null}
      </div>
      {isUser ? <AvatarIcon /> : null}
    </div>
  );
}

function PendingBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-end gap-3 opacity-70">
      <div className="max-w-[78%] whitespace-pre-wrap rounded-lg border bg-primary px-3 py-2 text-sm leading-6 text-primary-foreground shadow-sm">
        {text}
      </div>
      <AvatarIcon />
    </div>
  );
}

function AssistantOverlayBubble({ overlay }: { overlay: AssistantOverlay }) {
  return (
    <div className="flex gap-3">
      <AvatarIcon assistant />
      <div className="max-w-[78%] whitespace-pre-wrap rounded-lg border bg-card px-3 py-2 text-sm leading-6 shadow-sm">
        {overlay.text || "…"}
        {overlay.status === "failed" && overlay.error ? (
          <Alert className="mt-2" variant="destructive">
            <CircleAlert className="h-3.5 w-3.5" />
            <AlertDescription>{overlay.error}</AlertDescription>
          </Alert>
        ) : null}
        {overlay.status === "cancelled" || overlay.interrupted ? <InterruptedBadge /> : null}
      </div>
    </div>
  );
}

function AvatarIcon({ assistant = false }: { assistant?: boolean }) {
  return (
    <Avatar className="mt-1" size="sm">
      <AvatarFallback>{assistant ? <Bot className="h-3.5 w-3.5" /> : <User className="h-3.5 w-3.5" />}</AvatarFallback>
    </Avatar>
  );
}

function InterruptedBadge() {
  return (
    <div className="mt-2">
      <Badge variant="outline">interrupted</Badge>
    </div>
  );
}
