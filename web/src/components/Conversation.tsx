import { Paperclip, Upload } from "@/components/icons";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type DragEvent } from "react";

import type { Session } from "@/api/client";
import {
  BrowserAutomationPip,
  useBrowserAutomationActivity,
} from "@/browser/BrowserRuntimeProvider";
import { ChatColumn } from "@/components/ChatColumn";
import { Composer, type DroppedFilesBatch } from "@/components/Composer";
import { ConversationSearchBar } from "@/components/ConversationSearchBar";
import { FloatingTurnConsole } from "@/components/FloatingTurnConsole";
import { Transcript } from "@/components/Transcript";
import type { TranscriptSearchState } from "@/components/transcript/types";
import { WorkspaceActivityCard } from "@/components/WorkspaceActivityCard";
import { droppedLocalItemsFromDataTransfer } from "@/lib/localFolders";
import { dataTransferHasProjectReference, readProjectReferenceDrag } from "@/lib/projectReferences";
import { addProjectReferenceToSessionDraft } from "@/state/sessionDraftStore";
import { type WorkspaceActivity, useWorkspaceActivities } from "@/state/workspaceActivityStore";
import { mergeWorkspaceTabOrder, useWorkspaceTabOrder } from "@/state/workspaceTabOrderStore";
import { useWorkspaceOpen } from "@/state/workspaceStore";

const SUBMIT_ERROR_DURATION_MS = 5000;

// 会话体:Transcript 的滚动视口始终铺满整列,Composer 作为底部浮层覆盖在其上。
// Composer 的实测高度通过 CSS 变量提供给 Transcript 的底部占位,因此输入区变高时
// 只改变 scrollHeight,不会再挤压滚动视口的 clientHeight。
export function Conversation({
  searchFocusSignal,
  searchOpen,
  searchSlot,
  session,
  token,
  presentation = "default",
  onSearchOpenChange,
}: {
  searchFocusSignal: number;
  searchOpen: boolean;
  searchSlot: "primary" | "split";
  session: Session;
  token: string;
  presentation?: "default" | "floating";
  onSearchOpenChange: (open: boolean) => void;
}) {
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitSignal, setSubmitSignal] = useState(0);
  const [dragMode, setDragMode] = useState<"files" | "project_reference" | null>(null);
  const [droppedFiles, setDroppedFiles] = useState<DroppedFilesBatch | null>(null);
  const [activityRailFits, setActivityRailFits] = useState(true);
  const [searchState, setSearchState] = useState<TranscriptSearchState>({ terms: [] });
  const droppedFilesNonceRef = useRef(0);
  const submitErrorTimerRef = useRef<number | null>(null);
  const conversationRef = useRef<HTMLDivElement | null>(null);
  const composerOverlayRef = useRef<HTMLDivElement | null>(null);
  const floating = presentation === "floating";
  const workspaceOpen = useWorkspaceOpen(session.id);
  const workspaceActivities = useWorkspaceActivities(session.id);
  const workspaceTabOrder = useWorkspaceTabOrder(session.id);
  const orderedWorkspaceActivities = useMemo(
    () => orderWorkspaceActivities(workspaceActivities, workspaceTabOrder),
    [workspaceActivities, workspaceTabOrder],
  );
  const browserAutomationActivity = useBrowserAutomationActivity(session.id);
  const hasVisibleActivities = orderedWorkspaceActivities.length > 0
    || Boolean(browserAutomationActivity);
  const showActivitySurface = !floating
    && !workspaceOpen
    && hasVisibleActivities;
  const showActivityRail = showActivitySurface && activityRailFits;
  const showComposerActivities = showActivitySurface && !activityRailFits;
  const handleSubmitStart = useCallback(() => {
    setSubmitSignal((signal) => signal + 1);
  }, []);
  const handleSubmitError = useCallback((message: string | null) => {
    if (submitErrorTimerRef.current !== null) {
      window.clearTimeout(submitErrorTimerRef.current);
      submitErrorTimerRef.current = null;
    }
    setSubmitError(message);
    if (message) {
      submitErrorTimerRef.current = window.setTimeout(() => {
        submitErrorTimerRef.current = null;
        setSubmitError(null);
      }, SUBMIT_ERROR_DURATION_MS);
    }
  }, []);

  useEffect(() => {
    return () => {
      if (submitErrorTimerRef.current !== null) {
        window.clearTimeout(submitErrorTimerRef.current);
      }
    };
  }, []);

  useLayoutEffect(() => {
    const conversation = conversationRef.current;
    const composerOverlay = composerOverlayRef.current;
    if (!conversation || !composerOverlay) {
      return;
    }
    let currentHeight = -1;
    const updateComposerInset = (entry?: ResizeObserverEntry) => {
      const nextHeight = Math.ceil(
        entry?.borderBoxSize[0]?.blockSize ?? composerOverlay.offsetHeight,
      );
      if (currentHeight === nextHeight) {
        return;
      }
      currentHeight = nextHeight;
      conversation.style.setProperty("--pudding-composer-overlay-height", `${nextHeight}px`);
    };
    updateComposerInset();
    const observer = new ResizeObserver(([entry]) => updateComposerInset(entry));
    observer.observe(composerOverlay, { box: "border-box" });
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    const conversation = conversationRef.current;
    if (!conversation || floating) {
      return;
    }
    const minimumWidth = Number.parseFloat(
      getComputedStyle(conversation).getPropertyValue("--pudding-activity-rail-min-width"),
    );
    const updateActivityRailFit = (entry?: ResizeObserverEntry) => {
      const width = entry?.borderBoxSize[0]?.inlineSize ?? conversation.clientWidth;
      setActivityRailFits(width >= minimumWidth);
    };
    updateActivityRailFit();
    const observer = new ResizeObserver(([entry]) => updateActivityRailFit(entry));
    observer.observe(conversation, { box: "border-box" });
    return () => observer.disconnect();
  }, [floating]);

  const resetDragState = useCallback(() => {
    setDragMode(null);
  }, []);

  useEffect(() => {
    window.addEventListener("dragend", resetDragState);
    window.addEventListener("drop", resetDragState);
    window.addEventListener("blur", resetDragState);
    return () => {
      window.removeEventListener("dragend", resetDragState);
      window.removeEventListener("drop", resetDragState);
      window.removeEventListener("blur", resetDragState);
    };
  }, [resetDragState]);

  const handleDragEnter = useCallback((event: DragEvent<HTMLDivElement>) => {
    const mode = composerDragMode(event.dataTransfer);
    if (!mode) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    setDragMode(mode);
  }, []);

  const handleDragOver = useCallback((event: DragEvent<HTMLDivElement>) => {
    const mode = composerDragMode(event.dataTransfer);
    if (!mode) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "copy";
    setDragMode(mode);
  }, []);

  const handleDragLeave = useCallback(
    (event: DragEvent<HTMLDivElement>) => {
      if (!composerDragMode(event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      const nextTarget = event.relatedTarget;
      if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) {
        return;
      }
      resetDragState();
    },
    [resetDragState],
  );

  const handleDrop = useCallback(
    (event: DragEvent<HTMLDivElement>) => {
      if (!composerDragMode(event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      resetDragState();
      const projectReference = readProjectReferenceDrag(event.dataTransfer);
      if (projectReference) {
        addProjectReferenceToSessionDraft(session.id, projectReference);
        return;
      }
      const dropped = droppedLocalItemsFromDataTransfer(event.dataTransfer);
      if (dropped.files.length > 0 || dropped.folderPaths.length > 0 || dropped.folderPathUnavailable) {
        droppedFilesNonceRef.current += 1;
        setDroppedFiles({ ...dropped, nonce: droppedFilesNonceRef.current });
      }
    },
    [resetDragState, session.id],
  );

  return (
    <div
      ref={conversationRef}
      className={
        "pudding-conversation relative flex min-h-0 flex-1 flex-col overflow-hidden [--pudding-composer-mask-height:1rem] [--pudding-composer-overlay-height:0px] [&.file-drop-target-active_.pudding-drop-overlay]:opacity-100 " +
        (floating ? "bg-transparent" : "bg-background")
      }
      data-file-drop-target=""
      data-activity-rail={showActivityRail ? "true" : undefined}
      data-pudding-drop-target="conversation"
      data-session-id={session.id}
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      {floating ? null : (
        <div className="pointer-events-none absolute inset-x-0 top-0 z-20">
          <ChatColumn>
            <div className="h-6 bg-gradient-to-b from-background to-transparent" />
          </ChatColumn>
        </div>
      )}
      {floating ? (
        <div
          className="pointer-events-none absolute inset-x-0 top-0 z-20 flex items-end"
          style={{ bottom: "calc(var(--pudding-composer-overlay-height) - 1px)" }}
        >
          <FloatingTurnConsole session={session} submitError={submitError} token={token} />
        </div>
      ) : (
        <Transcript
          searchSlot={searchSlot}
          searchState={searchState}
          sessionID={session.id}
          sessionRunning={session.running}
          submitSignal={submitSignal}
          token={token}
        />
      )}
      {showActivityRail ? (
        <aside
          className="pointer-events-none absolute right-0 top-0 z-10 flex w-[var(--pudding-activity-rail-reserve)] flex-col gap-3 py-4 pr-4 pl-8"
        >
          <WorkspaceActivityCard
            activities={orderedWorkspaceActivities}
            browserPreview={browserAutomationActivity
              ? {
                  content: <BrowserAutomationPip embedded sessionID={session.id} />,
                  resourceID: browserAutomationActivity.tabID,
                }
              : undefined}
          />
        </aside>
      ) : null}
      <div
        ref={composerOverlayRef}
        className={
          floating
            ? "pointer-events-none relative z-30 mt-auto shrink-0"
            : "pointer-events-none absolute inset-x-0 bottom-0"
        }
      >
        {showComposerActivities ? (
          <ChatColumn className="relative z-10 mb-3 flex flex-col items-center gap-3">
            <WorkspaceActivityCard
              activities={orderedWorkspaceActivities}
              presentation="composer"
            />
            {browserAutomationActivity ? (
              <div className="w-60 max-w-full">
                <BrowserAutomationPip sessionID={session.id} />
              </div>
            ) : null}
          </ChatColumn>
        ) : null}
        <div className={floating ? undefined : "relative z-30"}>
          <Composer
            droppedFiles={droppedFiles}
            presentation={floating ? "floating" : "default"}
            submitError={submitError}
            token={token}
            session={session}
            onSubmitStart={handleSubmitStart}
            onSubmitError={handleSubmitError}
          />
        </div>
      </div>
      {floating ? null : (
        <ConversationSearchBar
          focusSignal={searchFocusSignal}
          open={searchOpen}
          sessionID={session.id}
          token={token}
          onOpenChange={onSearchOpenChange}
          onSearchChange={setSearchState}
        />
      )}
      <ChatDropOverlay mode={dragMode} />
    </div>
  );
}

function orderWorkspaceActivities(
  activities: WorkspaceActivity[],
  savedOrder: string[],
) {
  const activityByTabID = new Map<string, WorkspaceActivity>();
  const activitiesWithoutTabID: WorkspaceActivity[] = [];
  activities.forEach((activity) => {
    const tabID = workspaceActivityTabID(activity);
    if (tabID) {
      activityByTabID.set(tabID, activity);
    } else {
      activitiesWithoutTabID.push(activity);
    }
  });
  const orderedTabIDs = mergeWorkspaceTabOrder(savedOrder, [...activityByTabID.keys()]);
  return [
    ...orderedTabIDs.flatMap((tabID) => {
      const activity = activityByTabID.get(tabID);
      return activity ? [activity] : [];
    }),
    ...activitiesWithoutTabID,
  ];
}

function workspaceActivityTabID(activity: WorkspaceActivity) {
  if (!activity.resourceID) {
    return "";
  }
  return `${activity.kind === "browser" ? "browser" : "widget"}:${activity.resourceID}`;
}

function ChatDropOverlay({ mode }: { mode: "files" | "project_reference" | null }) {
  const Icon = mode === "project_reference" ? Paperclip : Upload;
  return (
    <div
      className={
        "pudding-drop-overlay pointer-events-none absolute inset-0 z-50 bg-info/[0.055] backdrop-blur-[1px] transition-opacity dark:bg-info/[0.08] " +
        (mode ? "opacity-100" : "opacity-0")
      }
    >
      <div className="absolute inset-0 flex items-center justify-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-background/90 text-info shadow-sm ring-1 ring-border/80">
          <Icon className="h-5 w-5" />
        </div>
      </div>
    </div>
  );
}

function dataTransferHasFiles(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types || []).includes("Files");
}

function composerDragMode(dataTransfer: DataTransfer): "files" | "project_reference" | null {
  if (dataTransferHasProjectReference(dataTransfer)) {
    return "project_reference";
  }
  return dataTransferHasFiles(dataTransfer) ? "files" : null;
}
