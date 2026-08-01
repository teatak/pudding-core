import { Paperclip, Upload } from "@/components/icons";
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type DragEvent } from "react";

import type { Session } from "@/api/client";
import { ChatColumn } from "@/components/ChatColumn";
import { Composer, type DroppedFilesBatch } from "@/components/Composer";
import { ConversationSearchBar } from "@/components/ConversationSearchBar";
import { Transcript } from "@/components/Transcript";
import type { TranscriptSearchState } from "@/components/transcript/types";
import { droppedLocalItemsFromDataTransfer } from "@/lib/localFolders";
import { dataTransferHasProjectReference, readProjectReferenceDrag } from "@/lib/projectReferences";
import { addProjectReferenceToSessionDraft } from "@/state/sessionDraftStore";

// 会话体:Transcript 的滚动视口始终铺满整列,Composer 作为底部浮层覆盖在其上。
// Composer 的实测高度通过 CSS 变量提供给 Transcript 的底部占位,因此输入区变高时
// 只改变 scrollHeight,不会再挤压滚动视口的 clientHeight。
export function Conversation({
  searchFocusSignal,
  searchOpen,
  searchSlot,
  session,
  token,
  onSearchOpenChange,
}: {
  searchFocusSignal: number;
  searchOpen: boolean;
  searchSlot: "primary" | "split";
  session: Session;
  token: string;
  onSearchOpenChange: (open: boolean) => void;
}) {
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [dragMode, setDragMode] = useState<"files" | "project_reference" | null>(null);
  const [droppedFiles, setDroppedFiles] = useState<DroppedFilesBatch | null>(null);
  const [searchState, setSearchState] = useState<TranscriptSearchState>({ terms: [] });
  const droppedFilesNonceRef = useRef(0);
  const conversationRef = useRef<HTMLDivElement | null>(null);
  const composerOverlayRef = useRef<HTMLDivElement | null>(null);

  useLayoutEffect(() => {
    const conversation = conversationRef.current;
    const composerOverlay = composerOverlayRef.current;
    if (!conversation || !composerOverlay) {
      return;
    }
    const updateComposerInset = () => {
      const nextHeight = Math.ceil(composerOverlay.getBoundingClientRect().height);
      conversation.style.setProperty("--pudding-composer-overlay-height", `${nextHeight}px`);
    };
    updateComposerInset();
    const observer = new ResizeObserver(updateComposerInset);
    observer.observe(composerOverlay);
    return () => observer.disconnect();
  }, []);

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
      className="pudding-conversation relative flex min-h-0 flex-1 flex-col overflow-hidden bg-background [--pudding-composer-mask-height:2.5rem] [--pudding-composer-overlay-height:0px] [&.file-drop-target-active_.pudding-drop-overlay]:opacity-100"
      data-file-drop-target=""
      data-pudding-drop-target="conversation"
      data-session-id={session.id}
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      <div className="pointer-events-none absolute inset-x-0 top-0 z-20">
        <ChatColumn>
          <div className="h-6 bg-gradient-to-b from-background to-transparent" />
        </ChatColumn>
      </div>
      <Transcript
        searchSlot={searchSlot}
        searchState={searchState}
        sessionID={session.id}
        sessionRunning={session.running}
        submitError={submitError}
        token={token}
      />
      <div ref={composerOverlayRef} className="pointer-events-none absolute inset-x-0 bottom-0 z-30">
        <Composer droppedFiles={droppedFiles} token={token} session={session} onSubmitError={setSubmitError} />
      </div>
      <ConversationSearchBar
        focusSignal={searchFocusSignal}
        open={searchOpen}
        sessionID={session.id}
        token={token}
        onOpenChange={onSearchOpenChange}
        onSearchChange={setSearchState}
      />
      <ChatDropOverlay mode={dragMode} />
    </div>
  );
}

function ChatDropOverlay({ mode }: { mode: "files" | "project_reference" | null }) {
  const Icon = mode === "project_reference" ? Paperclip : Upload;
  return (
    <div
      className={
        "pudding-drop-overlay pointer-events-none absolute inset-0 z-50 bg-primary/[0.055] backdrop-blur-[1px] transition-opacity dark:bg-primary/[0.08] " +
        (mode ? "opacity-100" : "opacity-0")
      }
    >
      <div className="absolute inset-0 flex items-center justify-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-background/90 text-primary shadow-sm ring-1 ring-border/80">
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
