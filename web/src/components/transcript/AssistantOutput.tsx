import { Archive, CircleAlert } from "@/components/icons";
import { memo, useEffect, useLayoutEffect, useMemo } from "react";

import type { Message } from "@/api/client";
import { PhaseDot } from "@/components/PhaseDot";
import { Spinner } from "@/components/Spinner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useI18n } from "@/i18n";
import { isTurnPhaseActive, type TurnPhaseState } from "@/state/overlayStore";

import { InterruptedBadge, MessageMeta } from "./MessageMeta";
import { useElapsedDuration } from "./time";
import {
  assistantTextFromMessages,
  partsFromMessages,
  partsFromOverlay,
  TurnParts,
} from "./TurnParts";
import type { AssistantOutputVM, TurnDisclosureState } from "./types";
import type { TranscriptDisplaySettings } from "./types";

export const AssistantOutput = memo(function AssistantOutput({
  assistant,
  disclosure,
  displaySettings,
  onContentGrow,
  onRevealComplete,
  sessionID,
  token,
  turnID,
}: {
  assistant: AssistantOutputVM;
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  onContentGrow?: () => void;
  onRevealComplete?: (turnID: string) => void;
  sessionID: string;
  token: string;
  turnID: string;
}) {
  if (assistant.kind === "canonical") {
    return <CanonicalAssistantOutput assistant={assistant} disclosure={disclosure} displaySettings={displaySettings} sessionID={sessionID} token={token} turnID={turnID} />;
  }
  if (assistant.kind === "live") {
    return (
      <LiveAssistantOutput
        assistant={assistant}
        disclosure={disclosure}
        displaySettings={displaySettings}
        sessionID={sessionID}
        token={token}
        turnID={turnID}
        onContentGrow={onContentGrow}
        onRevealComplete={onRevealComplete}
      />
    );
  }
  return <AssistantPhaseItem phase={assistant.phase} />;
});

function CanonicalAssistantOutput({
  assistant,
  disclosure,
  displaySettings,
  sessionID,
  token,
  turnID,
}: {
  assistant: Extract<AssistantOutputVM, { kind: "canonical" }>;
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  sessionID: string;
  token: string;
  turnID: string;
}) {
  const parts = useMemo(() => partsFromMessages(assistant.messages), [assistant.messages]);
  const text = useMemo(() => assistantTextFromMessages(assistant.messages), [assistant.messages]);
  const lastMessage = assistant.messages[assistant.messages.length - 1];
  if (!lastMessage) {
    return null;
  }
  const compactMessage = assistant.messages.find(isCompactMessage);
  if (compactMessage) {
    return <CompactMarker message={compactMessage} sessionID={sessionID} showSummary={displaySettings?.showCompactSummary ?? true} summaryText={text} />;
  }
  return (
    <div className="group flex min-w-0 flex-col">
      <div className="selectable-text min-w-0 text-sm leading-6">
        <TurnParts disclosure={disclosure} displaySettings={displaySettings} parts={parts} sessionID={sessionID} token={token} turnID={turnID} />
        {assistant.messages.some((message) => message.interrupted) ? <InterruptedBadge /> : null}
      </div>
    </div>
  );
}

export function AssistantOutputMeta({ assistant }: { assistant: AssistantOutputVM }) {
  if (assistant.kind !== "canonical" || assistant.messages.some(isCompactMessage)) {
    return null;
  }
  const lastMessage = assistant.messages[assistant.messages.length - 1];
  if (!lastMessage) {
    return null;
  }
  return (
    <MessageMeta
      createdAt={lastMessage.createdAt}
      duration={assistant.duration}
      hoverGroup="assistant-turn"
      model={assistant.model}
      text={assistantTextFromMessages(assistant.messages)}
    />
  );
}

function CompactMarker({ message, sessionID, showSummary, summaryText }: { message: Message; sessionID: string; showSummary: boolean; summaryText: string }) {
  const { t } = useI18n();
  const compact = compactMetadata(message);
  const sourceCount = compact?.source_message_ids?.length || 0;
  const tailCount = compact?.tail_message_ids?.length || 0;
  const sourceTurnCount = compact?.source_turn_count || 0;
  const tailTurnCount = compact?.tail_turn_count || 0;
  const statsText =
    tailTurnCount > 0
      ? t("transcript.compactStatsTurns")
          .replace("{source}", String(sourceTurnCount))
          .replace("{tail}", String(tailTurnCount))
      : t("transcript.compactStats")
          .replace("{source}", String(sourceCount))
          .replace("{tail}", String(tailCount));
  return (
    <div className="selectable-text my-1 overflow-hidden rounded-lg border border-border/70 bg-background text-sm">
      <div className="flex min-h-11 min-w-0 items-center gap-2 bg-muted/35 px-3 py-2 text-muted-foreground">
        <Archive className="size-3.5 shrink-0" />
        <span className="shrink-0 font-medium text-foreground">{t("transcript.compactMark")}</span>
        <span className="min-w-0 truncate text-xs">{statsText}</span>
      </div>
      {showSummary && summaryText.trim() ? (
        <details className="min-w-0 max-w-full overflow-hidden border-t border-border/70 bg-background px-3 py-1 text-xs text-muted-foreground">
          <summary className="flex min-h-8 cursor-pointer items-center select-none" tabIndex={-1}>{t("transcript.compactSummary")}</summary>
          <div className="mt-2 min-w-0 max-w-full border-t border-border/70 pt-2 text-foreground/80">
            <TurnParts parts={partsFromMessages([message])} sessionID={sessionID} token="" turnID={message.turnID} />
          </div>
        </details>
      ) : null}
    </div>
  );
}

export function CompactPendingMarker() {
  const { t } = useI18n();
  return (
    <div className="selectable-text my-1 flex min-h-11 items-center rounded-lg border border-border/70 bg-muted/35 px-3 py-2 text-sm">
      <div className="flex min-w-0 items-center gap-2 text-muted-foreground">
        <Archive className="size-3.5 shrink-0" />
        <span className="shrink-0 font-medium leading-none text-foreground">{t("transcript.compactRunning")}</span>
        <Spinner className="size-3.5 align-middle" />
      </div>
    </div>
  );
}

function isCompactMessage(message: Message) {
  return Boolean(compactMetadata(message));
}

function compactMetadata(
  message: Message,
): { source_message_ids?: string[]; source_turn_count?: number; tail_message_ids?: string[]; tail_turn_count?: number } | null {
  const meta = message.metadata;
  if (!meta || typeof meta !== "object" || !("compact" in meta)) {
    return null;
  }
  const compact = (meta as { compact?: unknown }).compact;
  if (!compact || typeof compact !== "object") {
    return null;
  }
  return compact as {
    source_message_ids?: string[];
    source_turn_count?: number;
    tail_message_ids?: string[];
    tail_turn_count?: number;
  };
}

function LiveAssistantOutput({
  assistant,
  disclosure,
  displaySettings,
  onContentGrow,
  onRevealComplete,
  sessionID,
  token,
  turnID,
}: {
  assistant: Extract<AssistantOutputVM, { kind: "live" }>;
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  onContentGrow?: () => void;
  onRevealComplete?: (turnID: string) => void;
  sessionID: string;
  token: string;
  turnID: string;
}) {
  const { overlay, phase } = assistant;
  const streaming = overlay.status === "streaming";
  const text = overlay.text;
  const hasThoughtPart = overlay.parts.some((part) => part.type === "thought");
  const hasToolPart = overlay.parts.some((part) => part.type === "tool");
  const hasApprovalPart = overlay.parts.some((part) => part.type === "approval");
  const activePhaseName: TurnPhaseState["phase"] | undefined =
    phase && isTurnPhaseActive(phase)
      ? phase.phase
      : streaming
        ? overlay.text
          ? "streaming_text"
          : hasThoughtPart
            ? "thinking"
            : hasToolPart
              ? "streaming_tool_args"
              : "awaiting_model"
        : undefined;
  const visibleActivePhaseName = activePhaseName;
  const phaseCarriedByPart =
    (visibleActivePhaseName === "thinking" && hasThoughtPart) ||
    (visibleActivePhaseName === "awaiting_approval" && hasApprovalPart) ||
    ((visibleActivePhaseName === "streaming_tool_args" ||
      visibleActivePhaseName === "executing_tool" ||
      visibleActivePhaseName === "awaiting_followup") &&
      hasToolPart);
  const activePhaseUpdatedAt =
    phase && isTurnPhaseActive(phase) && phase.phase === visibleActivePhaseName ? phase.updatedAt : undefined;
  const parts = useMemo(
    () => partsFromOverlay(overlay, text, visibleActivePhaseName, activePhaseUpdatedAt),
    [activePhaseUpdatedAt, overlay, text, visibleActivePhaseName],
  );
  const footerPhaseName = phaseCarriedByPart || visibleActivePhaseName === "streaming_text" ? undefined : visibleActivePhaseName;
  const footerPhase =
    footerPhaseName && phase
      ? { ...phase, phase: footerPhaseName }
      : footerPhaseName
        ? { phase: footerPhaseName, sessionID: overlay.sessionID, turnID: overlay.turnID, updatedAt: "" }
        : undefined;

  useLayoutEffect(() => {
    onContentGrow?.();
  }, [overlay.parts, text, onContentGrow]);
  useLayoutEffect(() => {
    const waitingForCanonical = overlay.status === "completed" && Boolean(overlay.assistantMessageID) && !assistant.canonicalReady;
    if (streaming || overlay.revealed || text !== overlay.text || waitingForCanonical) {
      return;
    }
    onRevealComplete?.(overlay.turnID);
  }, [
    assistant.canonicalReady,
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
      <div className="min-w-0">
        {parts.length > 0 ? <TurnParts disclosure={disclosure} displaySettings={displaySettings} parts={parts} sessionID={sessionID} token={token} turnID={turnID} /> : null}
      </div>
      {footerPhase ? <AssistantPhaseItem phase={footerPhase} /> : null}
      {overlay.status === "failed" && overlay.error ? (
        <Alert className="mt-2 min-w-0" variant="destructive">
          <CircleAlert className="h-3.5 w-3.5" />
          <AlertDescription className="min-w-0 overflow-hidden break-words">{overlay.error}</AlertDescription>
        </Alert>
      ) : null}
      {overlay.status === "cancelled" || overlay.interrupted ? <InterruptedBadge /> : null}
    </div>
  );
}

function AssistantPhaseItem({ phase }: { phase: TurnPhaseState }) {
  const { locale, t } = useI18n();
  const elapsed = useElapsedDuration(phase.updatedAt, locale);
  return (
    <div className="grid h-6 w-full grid-cols-[0.75rem_auto] items-center gap-1 text-xs text-muted-foreground">
      <span className="relative z-[1] inline-flex h-6 w-3 shrink-0 items-center justify-center opacity-90">
        <PhaseDot phase={phase.phase} />
      </span>
      <span className="flex min-w-0 items-center gap-1.5">
        <span className="shrink-0 text-muted-foreground/70">{phaseLabel(phase, t)}</span>
        {elapsed ? <span className="shrink-0 text-muted-foreground/50">{elapsed}</span> : null}
      </span>
    </div>
  );
}

function phaseLabel(phase: TurnPhaseState, t: (key: string) => string) {
  switch (phase.phase) {
    case "submitting":
      return t("transcript.phaseSubmitting");
    case "awaiting_model":
      return t(phase.activity === "steering" ? "transcript.phaseSteering" : "transcript.phaseAwaitingModel");
    case "thinking":
      return t("transcript.thinking");
    case "streaming_tool_args":
      return t("transcript.toolReadingArgs");
    case "executing_tool":
      return t("transcript.toolRunning");
    case "awaiting_approval":
      return t("transcript.phaseAwaitingApproval");
    case "awaiting_followup":
      return t("transcript.phaseAwaitingFollowup");
    case "error":
      return t("transcript.toolFailed");
    case "cancelled":
      return t("transcript.interrupted");
    case "streaming_text":
    default:
      return "";
  }
}
