import { CircleAlert } from "lucide-react";
import { memo, useEffect, useLayoutEffect, useMemo } from "react";

import { PhaseDot } from "@/components/PhaseDot";
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

export const AssistantOutput = memo(function AssistantOutput({
  assistant,
  disclosure,
  onContentGrow,
  onRevealComplete,
  turnID,
}: {
  assistant: AssistantOutputVM;
  disclosure?: TurnDisclosureState;
  onContentGrow?: () => void;
  onRevealComplete?: (turnID: string) => void;
  turnID: string;
}) {
  if (assistant.kind === "canonical") {
    return <CanonicalAssistantOutput assistant={assistant} disclosure={disclosure} turnID={turnID} />;
  }
  if (assistant.kind === "live") {
    return (
      <LiveAssistantOutput
        assistant={assistant}
        disclosure={disclosure}
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
  turnID,
}: {
  assistant: Extract<AssistantOutputVM, { kind: "canonical" }>;
  disclosure?: TurnDisclosureState;
  turnID: string;
}) {
  const parts = useMemo(() => partsFromMessages(assistant.messages), [assistant.messages]);
  const text = useMemo(() => assistantTextFromMessages(assistant.messages), [assistant.messages]);
  const lastMessage = assistant.messages[assistant.messages.length - 1];
  if (!lastMessage) {
    return null;
  }
  return (
    <div className="group flex flex-col">
      <div className="selectable-text min-w-0 text-sm leading-6">
        <TurnParts disclosure={disclosure} parts={parts} turnID={turnID} />
        {assistant.messages.some((message) => message.interrupted) ? <InterruptedBadge /> : null}
      </div>
      <MessageMeta createdAt={lastMessage.createdAt} duration={assistant.duration} model={assistant.model} text={text} />
    </div>
  );
}

function LiveAssistantOutput({
  assistant,
  disclosure,
  onContentGrow,
  onRevealComplete,
  turnID,
}: {
  assistant: Extract<AssistantOutputVM, { kind: "live" }>;
  disclosure?: TurnDisclosureState;
  onContentGrow?: () => void;
  onRevealComplete?: (turnID: string) => void;
  turnID: string;
}) {
  const { overlay, phase } = assistant;
  const streaming = overlay.status === "streaming";
  const text = overlay.text;
  const hasThoughtPart = overlay.parts.some((part) => part.type === "thought");
  const hasToolPart = overlay.parts.some((part) => part.type === "tool");
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
  const phaseCarriedByPart =
    (activePhaseName === "thinking" && hasThoughtPart) ||
    ((activePhaseName === "streaming_tool_args" ||
      activePhaseName === "executing_tool" ||
      activePhaseName === "awaiting_followup") &&
      hasToolPart);
  const activePhaseUpdatedAt =
    phase && isTurnPhaseActive(phase) && phase.phase === activePhaseName ? phase.updatedAt : undefined;
  const parts = useMemo(
    () => partsFromOverlay(overlay, text, activePhaseName, activePhaseUpdatedAt),
    [activePhaseName, activePhaseUpdatedAt, overlay, text],
  );
  const footerPhaseName = phaseCarriedByPart || activePhaseName === "streaming_text" ? undefined : activePhaseName;
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
      <div className="min-w-0">{parts.length > 0 ? <TurnParts disclosure={disclosure} parts={parts} turnID={turnID} /> : null}</div>
      {footerPhase ? <AssistantPhaseItem phase={footerPhase} /> : null}
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

function AssistantPhaseItem({ phase }: { phase: TurnPhaseState }) {
  const { locale, t } = useI18n();
  const elapsed = useElapsedDuration(phase.updatedAt, locale);
  return (
    <div className="grid h-6 w-full grid-cols-[0.75rem_auto] items-center gap-1 text-xs text-muted-foreground">
      <span className="relative z-[1] inline-flex h-6 w-3 shrink-0 items-center justify-center opacity-90">
        <PhaseDot phase={phase.phase} />
      </span>
      <span className="flex min-w-0 items-center gap-1.5">
        <span className="shrink-0 text-muted-foreground/70">{phaseLabel(phase.phase, t)}</span>
        {elapsed ? <span className="shrink-0 text-muted-foreground/50">{elapsed}</span> : null}
      </span>
    </div>
  );
}

function phaseLabel(phase: TurnPhaseState["phase"], t: (key: string) => string) {
  switch (phase) {
    case "submitting":
      return t("transcript.phaseSubmitting");
    case "awaiting_model":
      return t("transcript.phaseAwaitingModel");
    case "thinking":
      return t("transcript.thinking");
    case "streaming_tool_args":
      return t("transcript.toolReadingArgs");
    case "executing_tool":
      return t("transcript.toolRunning");
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
