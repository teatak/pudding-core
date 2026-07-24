import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  ArchiveRestore,
  MessageSquarePlus,
  NotebookText,
  PenLine,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type ChangeEvent,
  type FocusEvent,
  type KeyboardEvent,
} from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";

import {
  APIError,
  cancelTurn,
  captureDesktopPhoto,
  captureDesktopScreenshot,
  compactSession,
  getAudioBindings,
  getTurn,
  listApps,
  listSkills,
  steerTurn,
  submitMessage,
  updateSession,
  uploadAttachment,
  revealDesktopPath,
  type Attachment,
  type ContentPart,
  type ProjectReference,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { readElectronBrowserSelection } from "@/browser/electronBridge";
import { ChatColumn } from "@/components/ChatColumn";
import { ComposerApprovalBar, selectPendingApproval } from "@/components/ComposerApprovalBar";
import {
  ComposerAttachments,
  composerAttachmentImageSource,
  isImageAttachmentLike,
  revokeAttachmentPreview,
} from "@/components/ComposerAttachments";
import { buildComposerMentionReferences } from "@/components/composerMentionData";
import { ComposerMentionMenu } from "@/components/ComposerMentionMenu";
import { ComposerTextArea, parseSlashSubmitCommand, type ComposerTextAreaHandle, type SlashCommand, type SlashSubmitCommand } from "@/components/ComposerTextArea";
import { ComposerToolbar } from "@/components/ComposerToolbar";
import { ComposerTurnProgress } from "@/components/ComposerTurnProgress";
import { ImageLightbox, type ImageLightboxItem } from "@/components/ImageLightbox";
import { InputFlowPanel, type InputFlowSubmission } from "@/components/transcript/InputFlowToolPart";
import { Mascot, type MascotGaze, type MascotGazePoint } from "@/components/Mascot";
import { upsertTurnIntoPages, type TurnsInfiniteData } from "@/components/transcript/useTranscriptTurns";
import { WorkspaceActivityCard } from "@/components/WorkspaceActivityCard";
import { type ResolvedModelSelection } from "@/lib/modelSelection";
import { reasoningEffortOptionsForSelection } from "@/components/ReasoningEffortChip";
import { useComposerSelectionGuard } from "@/hooks/useComposerSelectionGuard";
import { useI18n } from "@/i18n";
import { createPastedTextAttachmentFile, shouldAttachPastedText } from "@/lib/clipboardTextAttachment";
import { getLocalFilePath } from "@/lib/desktopBridge";
import { newClientID } from "@/lib/id";
import {
  createLocalFolderPath,
  type DroppedLocalItems,
  pickLocalFolderPaths,
  type LocalFolderPath,
} from "@/lib/localFolders";
import type { AppSearch } from "@/lib/route";
import { getSubmitFailure } from "@/lib/submitFailure";
import { buildDraftSubmitParts, type DraftPartOrderItem } from "@/lib/submitParts";
import { getTextAreaCaretClientPoint } from "@/lib/textCaret";
import { cn } from "@/lib/utils";
import { useOverlayStore } from "@/state/overlayStore";
import { useInputFlowStore } from "@/state/inputFlowStore";
import { useSessionDraftStore, type SessionDraftAttachment } from "@/state/sessionDraftStore";
import {
  getVisibleUIContext,
  setUIContextEnabled,
  type UIContextPart,
  useUIContextEnabled,
  useVisibleUIContext,
} from "@/state/uiContextStore";
import { useWorkspaceActivities } from "@/state/workspaceActivityStore";
import { useWorkspaceOpen } from "@/state/workspaceStore";

const composerSchema = z.object({
  text: z.string(),
});

const MASCOT_INPUT_PITCH_BIAS = 0.65;
const draftAttachmentSessionID = "draft";

type ComposerProps = {
  droppedFiles?: DroppedFilesBatch | null;
  token: string;
  session: Session;
  onSubmitError?: (message: string | null) => void;
};

export type DroppedFilesBatch = DroppedLocalItems & {
  attachments?: Attachment[];
  failedFiles?: string[];
  failedFileCount?: number;
  nonce: number;
};

type ComposerAttachment = SessionDraftAttachment;
type RunningDeliveryMode = "steer" | "queue";
const emptyComposerAttachments: ComposerAttachment[] = [];
const emptyLocalFolders: LocalFolderPath[] = [];
const emptyPartOrder: DraftPartOrderItem[] = [];
const emptyProjectReferences: ProjectReference[] = [];

async function captureBrowserSelection(sessionID: string, context: UIContextPart) {
  if (context.surface !== "browser" || context.resource !== "browser_tab" || !context.id) {
    return context;
  }
  const selection = await readElectronBrowserSelection(sessionID, context.id);
  const selectionText = selection.selectionText || context.selectionText?.trim() || "";
  return selectionText
    ? {
        ...context,
        selectionText,
      }
    : context;
}

export function Composer({ droppedFiles, token, session, onSubmitError }: ComposerProps) {
  const sessionID = session.id;
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const addPendingUser = useOverlayStore((state) => state.addPendingUser);
  const acceptSubmittingTurn = useOverlayStore((state) => state.acceptSubmittingTurn);
  const clearSubmittingTurn = useOverlayStore((state) => state.clearSubmittingTurn);
  const finishCompactRun = useOverlayStore((state) => state.finishCompactRun);
  const removePendingUser = useOverlayStore((state) => state.removePendingUser);
  const startCompactRun = useOverlayStore((state) => state.startCompactRun);
  const startSubmittingTurn = useOverlayStore((state) => state.startSubmittingTurn);
  const clearSessionDraft = useSessionDraftStore((state) => state.clear);
  const ensureSessionDraft = useSessionDraftStore((state) => state.ensure);
  const setSessionDraftText = useSessionDraftStore((state) => state.setText);
  // 停止态双源:overlay 的 runningTurns(本地实时)|| session 快照的 running
  // (后端 turns 表派生)。中途刷新走 SSE tail 不回放 turn.started,若此时
  // provider 暂无 delta,overlay 不知道有 turn 在跑——session.running 兜底,
  // 保证停止按钮不丢(cancel 按 sessionID 取消,无需 turnID)。
  const runningTurnID = useOverlayStore((state) => state.runningTurns[sessionID]);
  const overlayRunning = Boolean(runningTurnID);
  const pendingApproval = useOverlayStore((state) => selectPendingApproval(state.assistants, sessionID, state.runningTurns[sessionID]));
  const activeTurnPlan = useOverlayStore((state) => state.activeTurnPlans[sessionID]);
  const pendingInputFlow = useInputFlowStore((state) => state.requests.find((request) => request.sessionID === sessionID));
  const running = overlayRunning || session.running;
  const projectID = session.projectID || "";
  const audioBindingsQuery = useQuery({
    queryKey: queryKeys.audioBindings(),
    queryFn: () => getAudioBindings(token, sessionID),
    enabled: Boolean(token && sessionID),
  });
  const audioBindings = audioBindingsQuery.data?.bindings;
  const micActive = audioBindings?.inputOwner === sessionID;
  const [resolvedModel, setResolvedModel] = useState<ResolvedModelSelection | null>(null);
  const [mascotGaze, setMascotGaze] = useState<MascotGaze>({ type: "pointer" });
  const [attachmentPreviewIndex, setAttachmentPreviewIndex] = useState<number | null>(null);
  const [capturingPhoto, setCapturingPhoto] = useState(false);
  const [capturingScreenshot, setCapturingScreenshot] = useState(false);
  const [pickingAttachment, setPickingAttachment] = useState(false);
  const [pickingLocalFolder, setPickingLocalFolder] = useState(false);
  const [slashSelectedIndex, setSlashSelectedIndex] = useState(0);
  const workspaceOpen = useWorkspaceOpen();
  const workspaceActivities = useWorkspaceActivities(sessionID);
  const uiContextEnabled = useUIContextEnabled();
  const visibleUIContext = useVisibleUIContext(sessionID);
  // clientMessageID 按"草稿"生成而不是按请求生成:失败重试和快速双击
  // 复用同一个 ID,服务端幂等去重才生效;成功后才轮换到下一个草稿 ID。
  const draftIDRef = useRef<string>(newClientID());
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const lastDroppedFilesNonceRef = useRef(0);
  const textAreaRef = useRef<HTMLTextAreaElement | null>(null);
  const mascotGazeRafRef = useRef(0);
  const submitPreparationRef = useRef(false);
  const form = useForm<z.infer<typeof composerSchema>>({
    resolver: zodResolver(composerSchema),
    defaultValues: { text: "" },
  });
  const attachments = useSessionDraftStore((state) => state.drafts[sessionID]?.attachments ?? emptyComposerAttachments);
  const localFolders = useSessionDraftStore((state) => state.drafts[sessionID]?.localFolders ?? emptyLocalFolders);
  const partOrder = useSessionDraftStore((state) => state.drafts[sessionID]?.partOrder ?? emptyPartOrder);
  const projectReferences = useSessionDraftStore(
    (state) => state.drafts[sessionID]?.projectReferences ?? emptyProjectReferences,
  );
  const setSessionDraftAttachments = useSessionDraftStore((state) => state.setAttachments);
  const setSessionDraftLocalFolders = useSessionDraftStore((state) => state.setLocalFolders);
  const setSessionDraftPartOrder = useSessionDraftStore((state) => state.setPartOrder);
  const setSessionDraftProjectReferences = useSessionDraftStore((state) => state.setProjectReferences);
  const selectionGuardRef = useComposerSelectionGuard<HTMLDivElement>();
  useEffect(() => {
    const draft = ensureSessionDraft(sessionID);
    draftIDRef.current = draft.clientMessageID;
    form.reset({ text: draft.text });
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }, [ensureSessionDraft, form, sessionID]);
  const [canSend, setCanSend] = useState(false);
  const [hasInput, setHasInput] = useState(false);
  const [mentionMenuOpen, setMentionMenuOpen] = useState(false);
  const [slashMenuOpen, setSlashMenuOpen] = useState(false);
  const showApprovalPanel = Boolean(pendingApproval);
  const showInputFlowPanel = !showApprovalPanel && Boolean(pendingInputFlow);
  const showTurnProgress = Boolean(
    !showApprovalPanel
      && !showInputFlowPanel
      && running
      && activeTurnPlan,
  );
  const showWorkspaceActivities = Boolean(
    !showApprovalPanel
      && !showInputFlowPanel
      && !showTurnProgress
      && !workspaceOpen
      && workspaceActivities.length > 0,
  );
  const showComposerTopStatus = showTurnProgress || showWorkspaceActivities;
  const [draftSlashCommand, setDraftSlashCommand] = useState<SlashSubmitCommand | null>(null);
  const textAreaHandleRef = useRef<ComposerTextAreaHandle | null>(null);
  const uploadedAttachments = attachments.flatMap((item) => (item.status === "uploaded" && item.attachment ? [item.attachment] : []));
  const hasPendingAttachments = attachments.some((item) => item.status === "uploading");
  const hasFailedAttachments = attachments.some((item) => item.status === "error");
  const hasAttachments = attachments.length > 0;
  const hasLocalFolders = localFolders.length > 0;
  const hasProjectReferences = projectReferences.length > 0;
  const attachmentPreviewItems = useMemo(
    () =>
      attachments.flatMap((item): ImageLightboxItem[] => {
        const url = composerAttachmentImageSource(item, token);
        if (!url || !isImageAttachmentLike(item.attachment?.mime, item.name)) {
          return [];
        }
        return [{ id: item.id, name: item.name, size: item.size, url }];
      }),
    [attachments, token],
  );
  const attachmentPreviewIndexByID = useMemo(
    () => new Map(attachmentPreviewItems.map((item, index) => [item.id, index])),
    [attachmentPreviewItems],
  );
  const textField = form.register("text");
  const slashCommands: SlashCommand[] = [
    {
      command: "/compact",
      description: t("composer.commandCompactDesc"),
      hasArgs: true,
      icon: ArchiveRestore,
      id: "compact",
      label: t("composer.commandCompact"),
    },
    {
      command: "/clear",
      description: t("composer.commandClearDesc"),
      hasArgs: false,
      icon: MessageSquarePlus,
      id: "clear",
      label: t("composer.commandClear"),
    },
    {
      command: "/rename",
      description: t("composer.commandRenameDesc"),
      hasArgs: true,
      icon: PenLine,
      id: "rename",
      label: t("composer.commandRename"),
    },
    {
      command: "/summary",
      description: t("composer.commandSummaryDesc"),
      hasArgs: true,
      icon: NotebookText,
      id: "summary",
      label: t("composer.commandSummary"),
    },
  ];
  const appsQuery = useQuery({
    queryKey: queryKeys.apps(),
    queryFn: () => listApps(token),
    enabled: Boolean(token),
    staleTime: 30_000,
  });
  const skillsQuery = useQuery({
    queryKey: queryKeys.skills(),
    queryFn: () => listSkills(token),
    enabled: Boolean(token),
    staleTime: 30_000,
  });
  const mentionReferences = useMemo(
    () => buildComposerMentionReferences({ apps: appsQuery.data?.apps ?? [], skills: skillsQuery.data?.skills ?? [], t, token }),
    [appsQuery.data?.apps, skillsQuery.data?.skills, t, token],
  );
  const reasoningOptions = useMemo(() => reasoningEffortOptionsForSelection(resolvedModel), [resolvedModel]);
  const audioInputSupported = resolvedModel ? resolvedModel.modelConfig?.capabilities?.audio === true : undefined;
  const resolvedModelKey = resolvedModel ? `${resolvedModel.provider}:${resolvedModel.model}` : "";
  const reasoningEffort = resolvedModelKey && session.reasoningModelKey === resolvedModelKey ? session.reasoningEffort || "" : "";
  const setSessionReasoningEffort = useCallback(
    (value: string) => {
      if (!resolvedModelKey) {
        return;
      }
      void updateSession(token, sessionID, { reasoningEffort: value })
        .then((updated) => {
          queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
            if (!previous) {
              return previous;
            }
            return {
              sessions: previous.sessions.map((item) => (item.id === updated.id ? updated : item)),
            };
          });
        })
        .catch((error) => {
          console.warn("failed to update reasoning effort", error);
        })
        .finally(() => {
          void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
        });
    },
    [queryClient, resolvedModelKey, sessionID, token],
  );
  useEffect(() => {
    if (reasoningEffort && !reasoningOptions.includes(reasoningEffort)) {
      setSessionReasoningEffort("");
    }
  }, [reasoningEffort, reasoningOptions, setSessionReasoningEffort]);

  const clearSubmitError = useCallback(() => onSubmitError?.(null), [onSubmitError]);
  const resetSessionDraft = useCallback(() => {
    const current = ensureSessionDraft(sessionID);
    current.attachments.forEach(revokeAttachmentPreview);
    const draft = clearSessionDraft(sessionID);
    draftIDRef.current = draft.clientMessageID;
    form.reset({ text: draft.text });
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }, [clearSessionDraft, ensureSessionDraft, form, sessionID]);
  const addFiles = useCallback(
    (files: File[], options?: { origin?: "temp"; sourcePaths?: string[]; uploadSessionID?: string }) => {
      const nextFiles = files
        .map((file, index) => ({
          file,
          sourcePath: (options?.sourcePaths?.[index] || getLocalFilePath(file)).trim(),
        }))
        .filter((item) => item.file.size > 0);
      if (nextFiles.length === 0) {
        return;
      }
      const uploadSessionID = options?.uploadSessionID || sessionID;
      const items = nextFiles.map(({ file }) => ({
        id: newClientID(),
        name: file.name,
        previewURL: file.type.toLowerCase().startsWith("image/") ? URL.createObjectURL(file) : undefined,
        size: file.size,
        status: "uploading" as const,
      }));
      setSessionDraftAttachments(sessionID, (current) => [...current, ...items]);
      setSessionDraftPartOrder(sessionID, (current) => [...current, ...items.map((item) => ({ type: "attachment" as const, id: item.id }))]);
      items.forEach((item, index) => {
        const { file, sourcePath } = nextFiles[index];
        const uploadOptions = {
          ...(options?.origin ? { origin: options.origin } : {}),
          ...(sourcePath ? { sourcePath } : {}),
        };
        void uploadAttachment(token, uploadSessionID, file, uploadOptions)
          .then((attachment) => {
            setSessionDraftAttachments(sessionID, (current) =>
              current.map((currentItem) =>
                currentItem.id === item.id
                  ? { ...currentItem, attachment, name: attachment.name, size: attachment.size, status: "uploaded" }
                  : currentItem,
              ),
            );
          })
          .catch((error) => {
            console.warn("attachment upload failed", error);
            toast.error(uploadFailedMessage(item.name, t));
            setSessionDraftAttachments(sessionID, (current) => {
              const failed = current.find((currentItem) => currentItem.id === item.id);
              if (failed) {
                revokeAttachmentPreview(failed);
              }
              return current.filter((currentItem) => currentItem.id !== item.id);
            });
            setSessionDraftPartOrder(sessionID, (current) => current.filter((orderItem) => orderItem.type !== "attachment" || orderItem.id !== item.id));
          });
      });
    },
    [sessionID, setSessionDraftAttachments, setSessionDraftPartOrder, t, token],
  );
  const removeAttachment = useCallback((id: string) => {
    setSessionDraftAttachments(sessionID, (current) => {
      const removed = current.find((item) => item.id === id);
      if (removed) {
        revokeAttachmentPreview(removed);
      }
      return current.filter((item) => item.id !== id);
    });
    setSessionDraftPartOrder(sessionID, (current) => current.filter((item) => item.type !== "attachment" || item.id !== id));
  }, [sessionID, setSessionDraftAttachments, setSessionDraftPartOrder]);
  const addUploadedAttachments = useCallback((values: Attachment[]) => {
    if (values.length === 0) {
      return;
    }
    const items = values.map((attachment) => ({
      id: newClientID(),
      attachment,
      name: attachment.name,
      size: attachment.size,
      status: "uploaded" as const,
    }));
    setSessionDraftAttachments(sessionID, (current) => [...current, ...items]);
    setSessionDraftPartOrder(sessionID, (current) => [...current, ...items.map((item) => ({ type: "attachment" as const, id: item.id }))]);
  }, [sessionID, setSessionDraftAttachments, setSessionDraftPartOrder]);
  const captureScreenshot = useCallback(async () => {
    if (capturingScreenshot) {
      return;
    }
    setCapturingScreenshot(true);
    try {
      addUploadedAttachments(await captureDesktopScreenshot(token, sessionID));
      onSubmitError?.(null);
      window.requestAnimationFrame(() => textAreaRef.current?.focus({ preventScroll: true }));
    } catch (error) {
      if (error instanceof APIError && error.code === "screenshot_cancelled") {
        return;
      }
      toast.error(t("composer.screenshotFailed"));
    } finally {
      setCapturingScreenshot(false);
    }
  }, [addUploadedAttachments, capturingScreenshot, onSubmitError, sessionID, t, token]);
  const capturePhoto = useCallback(async () => {
    if (capturingPhoto) {
      return;
    }
    setCapturingPhoto(true);
    try {
      addUploadedAttachments([await captureDesktopPhoto(token, sessionID)]);
      onSubmitError?.(null);
      window.requestAnimationFrame(() => textAreaRef.current?.focus({ preventScroll: true }));
    } catch (error) {
      if (error instanceof APIError) {
        if (error.code === "camera_timeout") {
          toast.error(t("composer.photoTimeout"));
          return;
        }
        if (error.code === "camera_permission_denied") {
          toast.error(t("composer.photoPermissionDenied"));
          return;
        }
        if (error.code === "camera_unavailable" || error.code === "camera_unsupported") {
          toast.error(t("composer.photoUnavailable"));
          return;
        }
      }
      toast.error(t("composer.photoFailed"));
    } finally {
      setCapturingPhoto(false);
    }
  }, [addUploadedAttachments, capturingPhoto, onSubmitError, sessionID, t, token]);
  const addLocalFolderPaths = useCallback((paths: string[]) => {
    const folders = paths.flatMap((path) => {
      const folder = createLocalFolderPath(path);
      return folder ? [folder] : [];
    });
    if (folders.length === 0) {
      return;
    }
    const existing = new Set(localFolders.map((folder) => folder.path));
    const nextFolders = folders.filter((folder) => !existing.has(folder.path));
    if (nextFolders.length === 0) {
      return;
    }
    setSessionDraftLocalFolders(sessionID, (current) => {
      const currentPaths = new Set(current.map((folder) => folder.path));
      return [...current, ...nextFolders.filter((folder) => !currentPaths.has(folder.path))];
    });
    setSessionDraftPartOrder(sessionID, (current) => [...current, ...nextFolders.map((folder) => ({ type: "local_folder" as const, id: folder.id }))]);
  }, [localFolders, sessionID, setSessionDraftLocalFolders, setSessionDraftPartOrder]);
  const pickLocalFolder = useCallback(async () => {
    if (pickingLocalFolder) {
      return;
    }
    setPickingLocalFolder(true);
    try {
      addLocalFolderPaths(await pickLocalFolderPaths(t));
      window.requestAnimationFrame(() => textAreaRef.current?.focus({ preventScroll: true }));
    } catch {
      toast.error(t("composer.folderPickFailed"));
    } finally {
      setPickingLocalFolder(false);
    }
  }, [addLocalFolderPaths, pickingLocalFolder, t]);
  const removeLocalFolder = useCallback((id: string) => {
    setSessionDraftLocalFolders(sessionID, (current) => current.filter((folder) => folder.id !== id));
    setSessionDraftPartOrder(sessionID, (current) => current.filter((item) => item.type !== "local_folder" || item.id !== id));
  }, [sessionID, setSessionDraftLocalFolders, setSessionDraftPartOrder]);
  const removeProjectReference = useCallback((id: string) => {
    setSessionDraftProjectReferences(sessionID, (current) => current.filter((reference) => reference.id !== id));
    setSessionDraftPartOrder(sessionID, (current) =>
      current.filter((item) => item.type !== "project_reference" || item.id !== id),
    );
  }, [sessionID, setSessionDraftPartOrder, setSessionDraftProjectReferences]);
  const revealLocalPath = useCallback((path: string) => {
    if (!path.trim()) {
      return;
    }
    void revealDesktopPath(token, path).catch(() => toast.error(t("composer.revealFailed")));
  }, [t, token]);
  const pickAttachment = useCallback(() => {
    if (pickingAttachment) {
      return;
    }
    setPickingAttachment(true);
    const clearPicking = () => {
      window.setTimeout(() => {
        setPickingAttachment(false);
      }, 200);
    };
    window.addEventListener("focus", clearPicking, { once: true });
    window.requestAnimationFrame(() => {
      fileInputRef.current?.click();
      textAreaRef.current?.focus({ preventScroll: true });
    });
  }, [pickingAttachment]);
  const handleAttachmentInputChange = (event: ChangeEvent<HTMLInputElement>) => {
    setPickingAttachment(false);
    addFiles(Array.from(event.target.files || []));
    event.target.value = "";
  };
  const handleTextPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(event.clipboardData.files || []);
    if (files.length > 0) {
      event.preventDefault();
      addFiles(files);
      return;
    }
    const text = event.clipboardData.getData("text/plain");
    if (!shouldAttachPastedText(text)) {
      return;
    }
    event.preventDefault();
    addFiles([createPastedTextAttachmentFile(text)], { origin: "temp", uploadSessionID: draftAttachmentSessionID });
  };
  const openMentionMenuFromButton = useCallback(() => {
    textAreaHandleRef.current?.openMentionMenu();
  }, []);
  const showSubmitError = useCallback((message: string) => onSubmitError?.(message), [onSubmitError]);
  const submitMutation = useMutation({
    mutationFn: async (
      value: z.infer<typeof composerSchema> & { deliveryMode?: RunningDeliveryMode; parts: ContentPart[] },
    ) => {
      const clientMessageID = draftIDRef.current;
      if (!resolvedModel) {
        throw new APIError(400, "no_model");
      }
      const { provider, model } = resolvedModel;
      const guideNow = Boolean(runningTurnID && value.deliveryMode === "steer");
      addPendingUser({
        sessionID,
        clientMessageID,
        status: guideNow ? "steering" : "submitting",
        text: value.text,
        parts: value.parts,
        createdAt: new Date().toISOString(),
        turnID: guideNow ? runningTurnID : undefined,
      });
      if (session.provider !== provider || session.model !== model) {
        await updateSession(token, sessionID, { provider, model });
      }
      if (runningTurnID && guideNow) {
        const result = await steerTurn(token, sessionID, runningTurnID, {
          clientMessageID,
          text: value.text,
          parts: value.parts,
        });
        clearSubmittingTurn(sessionID, clientMessageID);
        return result;
      }
      const result = await submitMessage(token, sessionID, {
        clientMessageID,
        text: value.text,
        parts: value.parts,
      });
      if (result.queued || !result.turnID) {
        clearSubmittingTurn(sessionID, clientMessageID);
      } else {
        acceptSubmittingTurn(sessionID, clientMessageID, result.turnID);
      }
      return result;
    },
    onSuccess: async (result) => {
      clearSubmitError();
      onSubmitError?.(null);
      resetSessionDraft();
      // 标题自动生成由后端 titler 负责(provisional + LLM,session.titled
      // 事件回推),前端不写标题
      if (result.duplicate && result.turnID) {
        try {
          const turn = await getTurn(token, sessionID, result.turnID);
          queryClient.setQueryData<TurnsInfiniteData>(queryKeys.turns(sessionID), (previous) =>
            upsertTurnIntoPages(previous, turn),
          );
        } catch (error) {
          console.warn("failed to sync duplicate turn", error);
        }
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      // 提交未被接受,canonical 不会出现这条消息:pending 气泡必须撤掉,
      // 文本留在 composer 里供重试(同一 draft ID)
      clearSubmittingTurn(sessionID, draftIDRef.current);
      removePendingUser(sessionID, draftIDRef.current);
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("composer.submitFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      showSubmitError(failure.message);
    },
    onSettled: () => {
      submitPreparationRef.current = false;
    },
  });
  const inputFlowSubmitMutation = useMutation({
    mutationFn: async (submission: InputFlowSubmission) => {
      const clientMessageID = `input-flow-${submission.request.id}`;
      if (!resolvedModel) {
        throw new APIError(400, "no_model");
      }
      const { provider, model } = resolvedModel;
      if (session.provider !== provider || session.model !== model) {
        await updateSession(token, sessionID, { provider, model });
      }
      const result = await submitMessage(token, sessionID, {
        clientMessageID,
        text: submission.text,
        parts: [submission.formResult, { type: "text", text: submission.text }],
      });
      if (result.queued || !result.turnID) {
        clearSubmittingTurn(sessionID, clientMessageID);
      } else {
        acceptSubmittingTurn(sessionID, clientMessageID, result.turnID);
      }
      return result;
    },
    onMutate: (submission) => {
      const clientMessageID = `input-flow-${submission.request.id}`;
      clearSubmitError();
      onSubmitError?.(null);
      if (!running) {
        startSubmittingTurn(sessionID, clientMessageID);
      }
      addPendingUser({
        sessionID,
        clientMessageID,
        status: "submitting",
        text: submission.text,
        parts: [submission.formResult, { type: "text", text: submission.text }],
        createdAt: new Date().toISOString(),
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error, submission) => {
      const clientMessageID = `input-flow-${submission.request.id}`;
      clearSubmittingTurn(sessionID, clientMessageID);
      removePendingUser(sessionID, clientMessageID);
      useInputFlowStore.getState().addRequest(submission.request);
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("composer.submitFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      showSubmitError(failure.message);
    },
  });
  const cancelMutation = useMutation({
    mutationFn: () => cancelTurn(token, sessionID),
  });
  const compactMutation = useMutation({
    mutationFn: ({ hint }: { hint: string }) => compactSession(token, sessionID, { hint }),
    onMutate: () => {
      clearSubmitError();
      onSubmitError?.(null);
      startCompactRun(sessionID);
      resetSessionDraft();
    },
    onSuccess: async () => {
      clearSubmitError();
      onSubmitError?.(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.turns(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessionUsage(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      showSubmitError(compactErrorMessage(error, t));
    },
    onSettled: () => {
      finishCompactRun(sessionID);
    },
  });
  const systemSubmitMutation = useMutation({
    mutationFn: async ({ clientMessageID, text }: { clientMessageID: string; text: string }) => {
      if (!resolvedModel) {
        throw new APIError(400, "no_model");
      }
      const { provider, model } = resolvedModel;
      if (session.provider !== provider || session.model !== model) {
        await updateSession(token, sessionID, { provider, model });
      }
      return submitMessage(token, sessionID, { clientMessageID, kind: "system", text });
    },
    onMutate: () => {
      clearSubmitError();
      onSubmitError?.(null);
      resetSessionDraft();
    },
    onSuccess: async () => {
      clearSubmitError();
      onSubmitError?.(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("composer.summaryFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      showSubmitError(failure.message);
    },
  });
  const renameMutation = useMutation({
    mutationFn: ({ title }: { title: string }) => updateSession(token, sessionID, { title }),
    onSuccess: async () => {
      clearSubmitError();
      onSubmitError?.(null);
      resetSessionDraft();
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: () => {
      showSubmitError(t("composer.renameFailed"));
    },
  });
  const sendEnabled =
    canSend &&
    !mentionMenuOpen &&
    !slashMenuOpen &&
    !submitMutation.isPending &&
    !compactMutation.isPending &&
    !systemSubmitMutation.isPending &&
    !renameMutation.isPending &&
    (Boolean(resolvedModel) || Boolean(draftSlashCommand && draftSlashCommand.id !== "summary"));
  const stopEnabled = running && !cancelMutation.isPending;
  const showStopButton = (running || cancelMutation.isPending) && !hasInput;
  const showSendButton =
    !showStopButton ||
    (canSend && !submitMutation.isPending && !compactMutation.isPending && !systemSubmitMutation.isPending && !renameMutation.isPending);

  const runClearCommand = useCallback(() => {
    clearSubmitError();
    onSubmitError?.(null);
    resetSessionDraft();
    void navigate({
      to: "/",
      search: (prev) => {
        const next = { ...(prev as AppSearch), draft: "1" };
        delete next.session;
        return next;
      },
    });
  }, [clearSubmitError, navigate, onSubmitError, resetSessionDraft]);

  const submitDraftWithMode = async (
    value: z.infer<typeof composerSchema>,
    deliveryMode?: RunningDeliveryMode,
  ) => {
    const text = value.text.trim();
    const attachmentItemsToSubmit = attachments.filter((item) => item.status === "uploaded" && item.attachment);
    const attachmentsToSubmit = attachmentItemsToSubmit.flatMap((item) => (item.attachment ? [item.attachment] : []));
    const localFoldersToSubmit = localFolders;
    const projectReferencesToSubmit = projectReferences;
    if (
      (!text && attachmentsToSubmit.length === 0 && localFoldersToSubmit.length === 0 && projectReferencesToSubmit.length === 0) ||
      submitMutation.isPending ||
      compactMutation.isPending ||
      systemSubmitMutation.isPending ||
      renameMutation.isPending ||
      submitPreparationRef.current ||
      hasPendingAttachments ||
      hasFailedAttachments
    ) {
      return;
    }
    const slashCommand =
      attachmentsToSubmit.length === 0 && localFoldersToSubmit.length === 0 && projectReferencesToSubmit.length === 0
        ? parseSlashSubmitCommand(text)
        : null;
    if (slashCommand?.id === "clear") {
      runClearCommand();
      return;
    }
    if (slashCommand?.id === "compact") {
      compactMutation.mutate({ hint: slashCommand.hint });
      return;
    }
    if (slashCommand?.id === "rename") {
      if (!slashCommand.title) {
        showSubmitError(t("composer.renameMissing"));
        return;
      }
      clearSubmitError();
      onSubmitError?.(null);
      renameMutation.mutate({ title: slashCommand.title });
      return;
    }
    if (slashCommand?.id === "summary") {
      systemSubmitMutation.mutate({
        clientMessageID: draftIDRef.current,
        text: summaryPrompt(slashCommand.hint, t),
      });
      return;
    }
    clearSubmitError();
    onSubmitError?.(null);
    submitPreparationRef.current = true;
    try {
      const currentUIContext = workspaceOpen && uiContextEnabled
        ? getVisibleUIContext(sessionID)
        : undefined;
      const capturedUIContext = currentUIContext
        ? await captureBrowserSelection(sessionID, currentUIContext)
        : undefined;
      if (submitMutation.isPending || compactMutation.isPending || systemSubmitMutation.isPending || renameMutation.isPending) {
        submitPreparationRef.current = false;
        return;
      }
      if (!running) {
        startSubmittingTurn(sessionID, draftIDRef.current);
      }
      const inputParts = buildDraftSubmitParts(
        text,
        attachmentItemsToSubmit,
        localFoldersToSubmit,
        partOrder,
        projectReferencesToSubmit,
      );
      submitMutation.mutate({
        deliveryMode,
        text,
        parts: capturedUIContext ? [...inputParts, capturedUIContext] : inputParts,
      });
    } catch {
      submitPreparationRef.current = false;
    }
  };
  const submitDraft = (value: z.infer<typeof composerSchema>) => submitDraftWithMode(value);

  const handleResolvedModelChange = useCallback((next: ResolvedModelSelection | null) => {
    setResolvedModel((current) => {
      if (!next) {
        return current ? null : current;
      }
      if (
        current?.provider === next.provider &&
        current.model === next.model &&
        current.providerProtocol === next.providerProtocol &&
        current.providerBrand === next.providerBrand &&
        current.modelConfig === next.modelConfig
      ) {
        return current;
      }
      return next;
    });
  }, []);
  const setMascotPointerGaze = useCallback(() => {
    if (mascotGazeRafRef.current) {
      window.cancelAnimationFrame(mascotGazeRafRef.current);
      mascotGazeRafRef.current = 0;
    }
    setMascotGaze((current) => (current.type === "pointer" ? current : { type: "pointer" }));
  }, []);
  const setMascotInputGaze = useCallback((target: MascotGazePoint | null) => {
    setMascotGaze(target ? { type: "input", target } : { type: "pointer" });
  }, []);
  const updateMascotInputGaze = useCallback(() => {
    const textArea = textAreaRef.current;
    if (!textArea || document.activeElement !== textArea) {
      return;
    }
    setMascotInputGaze(getTextAreaCaretClientPoint(textArea));
  }, [setMascotInputGaze]);
  const scheduleMascotInputGaze = useCallback(() => {
    if (mascotGazeRafRef.current) {
      window.cancelAnimationFrame(mascotGazeRafRef.current);
    }
    mascotGazeRafRef.current = window.requestAnimationFrame(() => {
      mascotGazeRafRef.current = 0;
      updateMascotInputGaze();
    });
  }, [updateMascotInputGaze]);
  const focusTextarea = useCallback(() => {
    window.requestAnimationFrame(() => {
      textAreaRef.current?.focus({ preventScroll: true });
      scheduleMascotInputGaze();
    });
  }, [scheduleMascotInputGaze]);
  useEffect(() => {
    if (!droppedFiles || droppedFiles.nonce === lastDroppedFilesNonceRef.current) {
      return;
    }
    lastDroppedFilesNonceRef.current = droppedFiles.nonce;
    if (droppedFiles.files.length > 0) {
      addFiles(droppedFiles.files, { sourcePaths: droppedFiles.fileSourcePaths });
    }
    if (droppedFiles.attachments && droppedFiles.attachments.length > 0) {
      addUploadedAttachments(droppedFiles.attachments);
    }
    if (droppedFiles.folderPaths.length > 0) {
      addLocalFolderPaths(droppedFiles.folderPaths);
    }
    if (droppedFiles.failedFileCount) {
      toast.error(dropFailedMessage(droppedFiles.failedFiles, t));
    }
    if (droppedFiles.folderPathUnavailable) {
      toast.error(t("composer.folderDropPathUnavailable"));
    }
    focusTextarea();
  }, [addFiles, addLocalFolderPaths, addUploadedAttachments, droppedFiles, focusTextarea, t]);
  useEffect(() => {
    if (attachmentPreviewIndex !== null && attachmentPreviewIndex >= attachmentPreviewItems.length) {
      setAttachmentPreviewIndex(null);
    }
  }, [attachmentPreviewIndex, attachmentPreviewItems.length]);
  useEffect(() => {
    return () => {
      if (mascotGazeRafRef.current) {
        window.cancelAnimationFrame(mascotGazeRafRef.current);
      }
    };
  }, []);
  useEffect(() => {
    setMascotGaze({ type: "pointer" });
  }, [sessionID]);

  return (
    <>
      <form
        className={cn(
          "relative shrink-0 pb-4",
          showComposerTopStatus ? "pt-11" : "pt-2",
        )}
        onSubmit={form.handleSubmit(submitDraft)}
      >
      {showComposerTopStatus ? (
        <aside className="pointer-events-none absolute inset-x-0 top-0 z-30 h-9">
          <ChatColumn className="relative flex h-full items-center justify-center">
            {showTurnProgress && activeTurnPlan ? (
              <ComposerTurnProgress progress={activeTurnPlan} />
            ) : null}
            {showWorkspaceActivities ? (
              <div className="min-w-0">
                <WorkspaceActivityCard activities={workspaceActivities} />
              </div>
            ) : null}
          </ChatColumn>
        </aside>
      ) : null}
      {/* 底部遮罩:滚动内容贴近输入区时淡出,随 composer 定位、宽度走 ChatColumn。
          文字边缘外漏不归遮罩管——那是 WKWebView 字形渲染溢出,Transcript overflow-hidden 裁掉 */}
      <div className="pointer-events-none absolute inset-x-0 -top-10">
        <ChatColumn>
          <div className="h-10 bg-gradient-to-t from-background to-transparent" />
        </ChatColumn>
      </div>
      <ChatColumn>
        <div ref={selectionGuardRef} className="relative">
          {pendingApproval ? (
            <ComposerApprovalBar approval={pendingApproval} token={token} />
          ) : pendingInputFlow ? (
            <InputFlowPanel key={pendingInputFlow.id} request={pendingInputFlow} onSubmit={(submission) => inputFlowSubmitMutation.mutate(submission)} />
          ) : null}
          <div
            className={cn(
              "pudding-composer-shell relative rounded-[18px] border border-border/70 bg-card transition-shadow",
              micActive && "is-mic-active",
            )}
          >
            <input
              ref={fileInputRef}
              className="sr-only"
              accept="image/*,audio/*,text/*,application/pdf,.txt,.md,.csv,.json,.xml,.yaml,.yml"
              multiple
              tabIndex={-1}
              type="file"
              onChange={handleAttachmentInputChange}
            />
            <ComposerAttachments
              attachments={attachments}
              localFolders={localFolders}
              partOrder={partOrder}
              previewIndexByID={attachmentPreviewIndexByID}
              projectReferences={projectReferences}
              token={token}
              onPreview={setAttachmentPreviewIndex}
              onRemoveAttachment={removeAttachment}
              onRemoveLocalFolder={removeLocalFolder}
              onRemoveProjectReference={removeProjectReference}
              onRevealPath={revealLocalPath}
            />
            <div className="px-4 pt-3.5 pb-2.5">
              <ComposerTextArea
                ref={textAreaHandleRef}
                control={form.control}
                textField={textField}
                textAreaRef={textAreaRef}
                mentionReferences={mentionReferences}
                slashCommands={slashCommands}
                placeholder={
                  session.activeMode === "chat"
                    ? t("composer.messagePlaceholder")
                    : t("composer.modeMessagePlaceholder").replace(
                        "{mode}",
                        t(`mode.${session.activeMode}`),
                      )
                }
                hasAttachments={hasAttachments}
                hasLocalFolders={hasLocalFolders}
                hasProjectReferences={hasProjectReferences}
                hasPendingAttachments={hasPendingAttachments}
                hasFailedAttachments={hasFailedAttachments}
                uploadedAttachmentsCount={uploadedAttachments.length}
                formSetValue={form.setValue}
                setSessionDraftText={setSessionDraftText}
                sessionID={sessionID}
                onCanSendChange={setCanSend}
                onHasContentChange={setHasInput}
                onMentionMenuOpenChange={setMentionMenuOpen}
                onSlashMenuOpenChange={setSlashMenuOpen}
                onDraftSlashCommandChange={setDraftSlashCommand}
                onAction={(actionID) => {
                  if (actionID === "files") {
                    pickAttachment();
                    return;
                  }
                  if (actionID === "folder") {
                    pickLocalFolder();
                    return;
                  }
                  if (actionID === "screenshot") {
                    captureScreenshot();
                    return;
                  }
                  if (actionID === "photo") {
                    capturePhoto();
                  }
                }}
                onSlashCommandSelect={(command) => {
                  if (!command.hasArgs) {
                    runClearCommand();
                    return;
                  }
                }}
                onEnter={(info) => {
                  const hasModel = Boolean(resolvedModel) || Boolean(info.draftSlashCommand && info.draftSlashCommand.id !== "summary");
                  const pending = submitMutation.isPending || compactMutation.isPending || systemSubmitMutation.isPending || renameMutation.isPending;
                  if (info.canSend && !info.mentionMenuOpen && !info.slashMenuOpen && !pending && hasModel) {
                    void form.handleSubmit((value) =>
                      submitDraftWithMode(value, info.guideNow && runningTurnID ? "steer" : undefined),
                    )();
                  }
                }}
                onPaste={handleTextPaste}
                onBlur={() => setMascotInputGaze(null)}
                onClearError={clearSubmitError}
                scheduleMascotInputGaze={scheduleMascotInputGaze}
              />
            </div>
            <ComposerToolbar
              addBusy={capturingPhoto || capturingScreenshot || pickingAttachment || pickingLocalFolder}
              audioBindings={audioBindings}
              audioInputSupported={audioInputSupported}
              cancelPending={cancelMutation.isPending}
              compacting={compactMutation.isPending}
              context={workspaceOpen ? visibleUIContext : undefined}
              mentionMenuOpen={mentionMenuOpen}
              projectID={projectID}
              reasoningEffort={reasoningEffort}
              sendEnabled={sendEnabled}
              session={session}
              showSendButton={showSendButton}
              showStopButton={showStopButton}
              steering={Boolean(runningTurnID)}
              stopEnabled={stopEnabled}
              submitPending={
                submitMutation.isPending ||
                compactMutation.isPending ||
                systemSubmitMutation.isPending ||
                renameMutation.isPending
              }
              token={token}
              uiContextEnabled={uiContextEnabled}
              onAddClick={openMentionMenuFromButton}
              onCancel={() => {
                if (stopEnabled) {
                  cancelMutation.mutate();
                }
              }}
              onModelPickerClose={focusTextarea}
              onReasoningChange={setSessionReasoningEffort}
              onResolvedModelChange={handleResolvedModelChange}
              onUIContextEnabledChange={setUIContextEnabled}
            />
          </div>
          <span
            className="absolute z-30 size-12 overflow-visible"
            style={{ left: 6, top: -36 }}
          >
            <Mascot
              className="size-full overflow-visible"
              gaze={mascotGaze}
              inputPitchBias={MASCOT_INPUT_PITCH_BIAS}
              mood={running ? "thinking" : "idle"}
              onPointerGaze={setMascotPointerGaze}
            />
          </span>
        </div>
      </ChatColumn>
      </form>
      <ImageLightbox images={attachmentPreviewItems} openIndex={attachmentPreviewIndex} onOpenIndexChange={setAttachmentPreviewIndex} />
    </>
  );
}

function uploadFailedMessage(name: string, t: (key: string) => string) {
  return `${name}: ${t("composer.uploadFailed")}`;
}

function dropFailedMessage(paths: string[] | undefined, t: (key: string) => string) {
  const names = (paths || []).map(pathBaseName).filter(Boolean).slice(0, 3);
  if (names.length === 0) {
    return t("composer.uploadFailed");
  }
  return `${names.join(", ")}: ${t("composer.uploadFailed")}`;
}

function pathBaseName(path: string) {
  const normalized = path.replaceAll("\\", "/").replace(/\/+$/, "");
  const name = normalized.split("/").pop();
  return name || path;
}

function summaryPrompt(hint: string, t: (key: string) => string) {
  if (hint) {
    return t("composer.summaryPromptWithHint").replace("{hint}", hint);
  }
  return t("composer.summaryPrompt");
}

function compactErrorMessage(error: unknown, t: (key: string) => string) {
  if (error instanceof APIError) {
    switch (error.code) {
      case "compact_empty":
        return t("composer.compactEmpty");
      case "compact_running":
        return t("composer.compactRunning");
      case "turn_running":
        return t("composer.turnRunning");
      case "no_model":
        return t("composer.noModel");
      case "provider_config":
        return t("composer.providerConfig");
    }
  }
  return t("composer.compactFailed");
}
