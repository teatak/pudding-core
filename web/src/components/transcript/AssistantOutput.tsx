import { CircleAlert, CircleGauge, Split } from "@/components/icons";
import { memo, useEffect, useLayoutEffect, useMemo } from "react";

import type { Message } from "@/api/client";
import { PhaseDot } from "@/components/PhaseDot";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { isTurnPhaseActive, type CompactRun, type TurnPhaseState } from "@/state/overlayStore";

import { InterruptedBadge, MessageMeta } from "./MessageMeta";
import { useElapsedDuration } from "./time";
import {
  assistantTextFromMessages,
  partsFromMessages,
  partsFromOverlay,
  TurnParts,
} from "./TurnParts";
import { ToolHoverCopyButton } from "./CodeToolDetails";
import { TranscriptDisclosure } from "./TranscriptDisclosure";
import { compactLiveOutput, isCompactMessage, splitCompactMessages } from "./compactOutput";
import type { AssistantOutputVM, TurnDisclosureState } from "./types";
import type { TranscriptDisplaySettings } from "./types";

export const AssistantOutput = memo(function AssistantOutput({
  assistant,
  disclosure,
  disclosureRootKey,
  displaySettings,
  onContentGrow,
  onRevealComplete,
  sessionID,
  token,
}: {
  assistant: AssistantOutputVM;
  disclosure?: TurnDisclosureState;
  disclosureRootKey: string;
  displaySettings?: TranscriptDisplaySettings;
  onContentGrow?: () => void;
  onRevealComplete?: (turnID: string) => void;
  sessionID: string;
  token: string;
}) {
  if (assistant.kind === "canonical") {
    return <CanonicalAssistantOutput assistant={assistant} disclosure={disclosure} disclosureRootKey={disclosureRootKey} displaySettings={displaySettings} sessionID={sessionID} token={token} />;
  }
  if (assistant.kind === "live") {
    return (
      <LiveAssistantOutput
        assistant={assistant}
        disclosure={disclosure}
        disclosureRootKey={disclosureRootKey}
        displaySettings={displaySettings}
        sessionID={sessionID}
        token={token}
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
  disclosureRootKey,
  displaySettings,
  sessionID,
  token,
}: {
  assistant: Extract<AssistantOutputVM, { kind: "canonical" }>;
  disclosure?: TurnDisclosureState;
  disclosureRootKey: string;
  displaySettings?: TranscriptDisplaySettings;
  sessionID: string;
  token: string;
}) {
  const groups = useMemo(() => splitCompactMessages(assistant.messages), [assistant.messages]);
  return (
    <div className="group flex min-w-0 flex-col" data-transcript-message-role="assistant">
      <div className="selectable-text min-w-0 text-sm leading-6">
        {groups.map((messages) => isCompactMessage(messages[0]) ? (
          <CompactMarker key={messages[0].id} message={messages[0]} sessionID={sessionID} showSummary={displaySettings?.showCompactSummary ?? true} summaryText={assistantTextFromMessages(messages)} />
        ) : (
          <TurnParts key={messages[0].id} disclosure={disclosure} disclosureRootKey={disclosureRootKey} displaySettings={displaySettings} parts={partsFromMessages(messages)} sessionID={sessionID} token={token} />
        ))}
        {assistant.error ? <AssistantError error={assistant.error} /> : null}
        {assistant.messages.some((message) => message.interrupted) ? <InterruptedBadge /> : null}
      </div>
    </div>
  );
}

export function AssistantOutputMeta({
  assistant,
  cloningMessageID,
  onCloneMessage,
}: {
  assistant: AssistantOutputVM;
  cloningMessageID?: string;
  onCloneMessage?: (messageID: string) => void;
}) {
  const { t } = useI18n();
  if (assistant.kind !== "canonical" || assistant.messages.every(isCompactMessage)) {
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
      trailingActions={
        onCloneMessage ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                aria-label={t("transcript.cloneToNewChat")}
                className="size-6 bg-transparent active:translate-y-0"
                data-slot="button"
                disabled={Boolean(cloningMessageID)}
                size="icon-xs"
                tabIndex={-1}
                type="button"
                variant="ghost"
                onClick={() => onCloneMessage(lastMessage.id)}
              >
                {cloningMessageID === lastMessage.id ? <Spinner className="size-3" /> : <Split className="size-3 rotate-90" />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("transcript.cloneToNewChat")}</TooltipContent>
          </Tooltip>
        ) : null
      }
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
    sourceTurnCount > 0 && tailTurnCount > 0
      ? t("transcript.compactStatsTurns")
          .replace("{source}", String(sourceTurnCount))
          .replace("{tail}", String(tailTurnCount))
      : t("transcript.compactStats")
          .replace("{source}", String(sourceCount))
          .replace("{tail}", String(tailCount));
  const summaryAvailable = showSummary && Boolean(summaryText.trim());
  const before = compact?.before_input_estimate || 0;
  const after = compact?.after_input_estimate || 0;
  const budget = compact?.input_budget || 0;
  const hasEstimate = before > 0 && after > 0;
  const overBudget = budget > 0 && after > budget;
  const savingsText = hasEstimate ? t("transcript.compactSavings")
    .replace("{saved}", (before - after).toLocaleString())
    .replace("{percent}", String(Math.round((before - after) / before * 100))) : statsText;
  return (
    <div className="selectable-text" data-compact-message-id={message.id}>
      <TranscriptDisclosure
        icon={<CircleGauge className="size-3.5" />}
        summary={savingsText}
        title={t(overBudget ? "transcript.compactOverBudget" : "transcript.compactMark")}
      >
        {summaryAvailable || hasEstimate ? (
          <div className="min-w-0 max-w-full overflow-hidden rounded-md border border-border/50 bg-muted/20 p-2 text-foreground/80">
            {hasEstimate ? <p>{t("transcript.compactInputEstimate").replace("{before}", before.toLocaleString()).replace("{after}", after.toLocaleString())} · {statsText}</p> : null}
            {overBudget ? <p>{t("transcript.compactBudgetDetail").replace("{limit}", budget.toLocaleString())}</p> : null}
            {summaryAvailable ? <TurnParts disclosureRootKey={message.id} parts={partsFromMessages([message])} sessionID={sessionID} token="" /> : null}
          </div>
        ) : null}
      </TranscriptDisclosure>
    </div>
  );
}

export function CompactRunMarker({ run }: { run: CompactRun }) {
  const { t } = useI18n();
  const noGain = run.error?.code === "compact_not_reduced";
  return (
    <div className="selectable-text" data-compact-run-id={run.clientMessageID}>
      <TranscriptDisclosure
        icon={noGain ? <CircleGauge /> : run.error ? <CircleAlert /> : <Spinner />}
        iconClassName={run.error && !noGain ? "text-destructive/70" : undefined}
        title={t(noGain ? "composer.compactNoGain" : run.error ? "composer.compactFailed" : "transcript.compactRunning")}
      >
        {run.error ? <div className={`rounded-md border border-border/50 bg-muted/20 p-2 ${noGain ? "text-foreground/80" : "text-destructive/80"}`} role={noGain ? "status" : "alert"}>{run.error.message}</div> : null}
      </TranscriptDisclosure>
    </div>
  );
}

type CompactMetadata = {
  source_message_ids?: string[];
  source_turn_count?: number;
  tail_message_ids?: string[];
  tail_turn_count?: number;
  before_input_estimate?: number;
  after_input_estimate?: number;
  input_budget?: number;
};

function compactMetadata(message: Message): CompactMetadata | null {
  const meta = message.metadata;
  if (!meta || typeof meta !== "object" || !("compact" in meta)) {
    return null;
  }
  const compact = (meta as { compact?: unknown }).compact;
  if (!compact || typeof compact !== "object") {
    return null;
  }
  return compact as CompactMetadata;
}

function LiveAssistantOutput({
  assistant,
  disclosure,
  disclosureRootKey,
  displaySettings,
  onContentGrow,
  onRevealComplete,
  sessionID,
  token,
}: {
  assistant: Extract<AssistantOutputVM, { kind: "live" }>;
  disclosure?: TurnDisclosureState;
  disclosureRootKey: string;
  displaySettings?: TranscriptDisplaySettings;
  onContentGrow?: () => void;
  onRevealComplete?: (turnID: string) => void;
  sessionID: string;
  token: string;
}) {
  const projection = useMemo(() => compactLiveOutput(assistant.messages || [], assistant.overlay), [assistant.messages, assistant.overlay]);
  const { overlay } = projection;
  const { phase } = assistant;
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
  }, [overlay.parts, text, projection.messages, onContentGrow]);
  useLayoutEffect(() => {
    const waitingForCanonical =
      overlay.status !== "streaming" && Boolean(overlay.assistantMessageID) && !assistant.canonicalReady;
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
    <div className="selectable-text min-w-0 text-sm leading-6">
      {projection.messages.length > 0 ? (
        <CanonicalAssistantOutput assistant={{ kind: "canonical", messages: projection.messages }} disclosure={disclosure} disclosureRootKey={disclosureRootKey} displaySettings={displaySettings} sessionID={sessionID} token={token} />
      ) : null}
      <div className="min-w-0">
        {parts.length > 0 ? <TurnParts disclosure={disclosure} disclosureRootKey={disclosureRootKey} displaySettings={displaySettings} parts={parts} sessionID={sessionID} token={token} /> : null}
      </div>
      {footerPhase ? <AssistantPhaseItem phase={footerPhase} /> : null}
      {overlay.status === "failed" && overlay.error ? <AssistantError error={overlay.error} /> : null}
      {overlay.status === "cancelled" || overlay.interrupted ? <InterruptedBadge /> : null}
    </div>
  );
}

function AssistantError({ error }: { error: string }) {
  const { t } = useI18n();
  const status = error.match(/\bstatus\s+(\d{3})\b/i)?.[1];
  const unavailable = status === "503" || /\b(?:service unavailable|too busy|overloaded)\b/i.test(error);
  const summary = t(unavailable ? "transcript.errorServiceUnavailable" : "transcript.errorRequestFailed");
  const summaryWithStatus = status ? `${summary} (${status})` : summary;
  return (
    <div className="min-w-0" role="alert">
      <TranscriptDisclosure
        icon={<CircleAlert className="size-3.5" />}
        iconClassName="text-destructive/70"
        title={summaryWithStatus}
      >
        <div className="group/error-detail relative min-w-0 max-w-full overflow-hidden rounded-md border border-border/50 bg-muted/20 p-2 pr-8">
          <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] leading-4 text-muted-foreground">{error}</pre>
          <ToolHoverCopyButton className="absolute top-1 right-1 group-hover/error-detail:opacity-100" text={error} />
        </div>
      </TranscriptDisclosure>
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
