import { ChevronDown, ChevronUp, Clock3 } from "@/components/icons";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef } from "react";

import type { Session } from "@/api/client";
import { PhaseDot } from "@/components/PhaseDot";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import { TranscriptTurn } from "@/components/transcript/TranscriptTurn";
import { UserInput } from "@/components/transcript/UserInput";
import {
  selectFloatingQueuedTurns,
  selectFloatingTurn,
} from "@/components/transcript/floatingTurn";
import { describeFloatingTurnActivity } from "@/components/transcript/turnActivitySummary";
import { useTranscriptData } from "@/components/transcript/useTranscriptData";
import { useElapsedDuration } from "@/components/transcript/time";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import {
  setFloatingConsoleExpanded,
  useFloatingConsoleExpanded,
} from "@/state/agentConsoleStore";
import { useInputFlowStore } from "@/state/inputFlowStore";
import { isTurnPhaseActive, useOverlayStore } from "@/state/overlayStore";

export function FloatingTurnConsole({
  session,
  submitError,
  token,
}: {
  session: Session;
  submitError?: string | null;
  token: string;
}) {
  const { locale, t } = useI18n();
  const expanded = useFloatingConsoleExpanded();
  const runningTurnID = useOverlayStore((state) => state.runningTurns[session.id]);
  const phase = useOverlayStore((state) => state.turnPhases[session.id]);
  const activeTurnID = runningTurnID || phase?.turnID;
  const overlay = useOverlayStore((state) => {
    const turnID = runningTurnID || state.turnPhases[session.id]?.turnID;
    return turnID ? state.assistants[turnID] : undefined;
  });
  const pendingInputFlow = useInputFlowStore((state) =>
    state.requests.find((request) => request.sessionID === session.id),
  );
  const blockingInteraction =
    phase?.phase === "awaiting_approval" ||
    Boolean(pendingInputFlow) ||
    Boolean(overlay?.parts.some((part) => part.type === "approval" && !part.status));
  const {
    markAssistantRevealed,
    steerQueued,
    transcript,
    updateQueued,
  } = useTranscriptData({
    sessionID: session.id,
    sessionRunning: session.running,
    token,
  });
  const selectedTurn = useMemo(
    () => selectFloatingTurn(transcript.turnVMs, activeTurnID),
    [activeTurnID, transcript.turnVMs],
  );
  const queuedTurns = useMemo(
    () => selectFloatingQueuedTurns(transcript.turnVMs),
    [transcript.turnVMs],
  );
  const running = Boolean(runningTurnID || session.running || isTurnPhaseActive(phase));
  const phaseElapsed = useElapsedDuration(running ? phase?.updatedAt : undefined, locale);
  const activity = describeFloatingTurnActivity({ overlay, phase, running, t });
  const ActivityIcon = activity.toolIcon;
  const completedDuration = !running ? turnDuration(selectedTurn) : "";
  const elapsed = phaseElapsed || completedDuration;
  const detailViewportRef = useRef<HTMLDivElement | null>(null);
  const detailContentRef = useRef<HTMLDivElement | null>(null);
  const atLatestRef = useRef(true);
  const previouslyExpandedRef = useRef(false);
  const autoExpandedForBlockingRef = useRef(false);

  useEffect(() => {
    setFloatingConsoleExpanded(false);
  }, [session.id]);

  useEffect(() => {
    if (blockingInteraction && !expanded) {
      autoExpandedForBlockingRef.current = true;
      setFloatingConsoleExpanded(true);
      return;
    }
    if (!blockingInteraction && autoExpandedForBlockingRef.current) {
      autoExpandedForBlockingRef.current = false;
      setFloatingConsoleExpanded(false);
    }
  }, [blockingInteraction, expanded]);

  useLayoutEffect(() => {
    const viewport = detailViewportRef.current;
    const opening = expanded && !previouslyExpandedRef.current;
    previouslyExpandedRef.current = expanded;
    if (opening) {
      atLatestRef.current = true;
    }
    if (!viewport || !expanded || (!opening && !atLatestRef.current)) {
      return;
    }
    viewport.scrollTop = viewport.scrollHeight;
  }, [expanded, selectedTurn]);

  useLayoutEffect(() => {
    const viewport = detailViewportRef.current;
    const content = detailContentRef.current;
    if (!expanded || !viewport || !content) {
      return;
    }
    const observer = new ResizeObserver(() => {
      if (atLatestRef.current) {
        viewport.scrollTop = viewport.scrollHeight;
      }
    });
    observer.observe(content);
    return () => observer.disconnect();
  }, [expanded]);

  const cancelQueued = useCallback(
    (clientMessageID: string) => updateQueued(clientMessageID, { status: "cancelled" }),
    [updateQueued],
  );
  const startQueuedEdit = useCallback(
    (clientMessageID: string) => updateQueued(clientMessageID, { status: "editing" }),
    [updateQueued],
  );
  const saveQueued = useCallback(
    (clientMessageID: string, text: string) =>
      updateQueued(clientMessageID, { status: "queued", text }),
    [updateQueued],
  );
  const guideQueued = useCallback(
    (clientMessageID: string) => {
      if (!activeTurnID) {
        return Promise.reject(new Error("turn_not_active"));
      }
      return steerQueued(clientMessageID, activeTurnID);
    },
    [activeTurnID, steerQueued],
  );

  const label = pendingInputFlow
    ? t("agentConsole.needsInput")
    : selectedTurn || running
      ? activity.label
      : t("agentConsole.ready");
  const statusDetail = [activity.detail, elapsed].filter(Boolean).join(" · ");
  const changeExpanded = (next: boolean) => {
    if (!next && blockingInteraction) {
      return;
    }
    setFloatingConsoleExpanded(next);
  };

  return (
    <Collapsible
      className={cn(
        "pudding-floating-turn-console pointer-events-auto mx-auto flex w-[560px] max-w-[calc(100%_-_2.5rem)] min-h-0 flex-none flex-col overflow-hidden rounded-t-[16px] rounded-b-none border border-b-0 border-border/70 bg-card",
        expanded && "h-full",
      )}
      open={expanded}
      onOpenChange={changeExpanded}
    >
      <div
        aria-label={t(expanded ? "agentConsole.collapse" : "agentConsole.expand")}
        aria-expanded={expanded}
        className={cn(
          "flex h-8 shrink-0 cursor-default items-center gap-2 px-3 text-xs",
          expanded && "border-b border-border/70",
        )}
        role="button"
        tabIndex={0}
        onClick={(event) => {
          const target = event.target;
          if (target instanceof Element && target.closest("button, a, input, textarea, select")) {
            return;
          }
          changeExpanded(!expanded);
        }}
        onKeyDown={(event) => {
          if (
            event.target !== event.currentTarget ||
            (event.key !== "Enter" && event.key !== " ")
          ) {
            return;
          }
          event.preventDefault();
          changeExpanded(!expanded);
        }}
      >
        {expanded ? null : (
          <>
            {ActivityIcon ? (
              <ActivityIcon
                aria-hidden="true"
                className="size-3.5 shrink-0 text-muted-foreground"
              />
            ) : (
              <PhaseDot active={activity.active} phase={activity.phase ?? "streaming_text"} size="sm" />
            )}
            <span className="min-w-0 flex-1 truncate font-medium text-foreground/80">
              {label}
              {statusDetail ? (
                <span className="font-normal text-muted-foreground"> · {statusDetail}</span>
              ) : null}
            </span>
            {queuedTurns.length > 0 ? (
              <span className="no-drag-region inline-flex shrink-0 items-center gap-1 text-muted-foreground">
                <Clock3 className="size-3.5" />
                {t("agentConsole.queuedCount").replace("{count}", String(queuedTurns.length))}
              </span>
            ) : null}
          </>
        )}
        {expanded ? (
          <span className="min-w-0 flex-1 truncate font-medium text-foreground/80">
            {t("agentConsole.latestTurn")}
          </span>
        ) : null}
        <Button
          aria-label={t(expanded ? "agentConsole.collapse" : "agentConsole.expand")}
          className="no-drag-region size-7"
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => changeExpanded(!expanded)}
        >
          {expanded ? <ChevronDown /> : <ChevronUp />}
        </Button>
      </div>
      <CollapsibleContent className="min-h-0 flex-1 overflow-hidden">
        <div
          ref={detailViewportRef}
          className="h-full overflow-x-hidden overflow-y-auto px-4 py-4"
          onScroll={(event) => {
            const node = event.currentTarget;
            atLatestRef.current = node.scrollHeight - node.scrollTop - node.clientHeight < 8;
          }}
        >
          <div ref={detailContentRef}>
            {selectedTurn ? (
              <TranscriptTurn
                sessionID={session.id}
                token={token}
                turn={selectedTurn}
                onAssistantRevealComplete={markAssistantRevealed}
              />
            ) : (
              <div className="flex min-h-24 items-center justify-center text-sm text-muted-foreground">
                {t("agentConsole.noTurn")}
              </div>
            )}
            {submitError ? (
              <div className="mt-3 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
                {submitError}
              </div>
            ) : null}
            {queuedTurns.length > 0 ? (
              <div className="mt-4 grid gap-3 border-t border-border/70 pt-3">
                <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                  <Clock3 className="size-3.5" />
                  {t("agentConsole.queuedCount").replace("{count}", String(queuedTurns.length))}
                </div>
                {queuedTurns.map((turn) =>
                  turn.user ? (
                    <UserInput
                      key={turn.key}
                      token={token}
                      user={turn.user}
                      onQueuedCancel={cancelQueued}
                      onQueuedEditStart={startQueuedEdit}
                      onQueuedSave={saveQueued}
                      onQueuedSteer={activeTurnID ? guideQueued : undefined}
                    />
                  ) : null,
                )}
              </div>
            ) : null}
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function turnDuration(turn: ReturnType<typeof selectFloatingTurn>) {
  if (!turn) {
    return "";
  }
  const assistants = [
    ...(turn.assistant ? [turn.assistant] : []),
    ...(turn.sequence || [])
      .filter((item) => item.kind === "assistant")
      .map((item) => item.kind === "assistant" ? item.assistant : undefined)
      .filter(Boolean),
  ];
  for (let index = assistants.length - 1; index >= 0; index -= 1) {
    const assistant = assistants[index];
    if (assistant?.kind === "canonical" && assistant.duration) {
      return assistant.duration;
    }
  }
  return "";
}
