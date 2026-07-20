import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  ArchiveRestore,
  ArrowUp,
  Check,
  FileText,
  FolderOpen,
  MessageSquarePlus,
  NotebookText,
  Pause,
  PenLine,
  Play,
  ShieldCheck,
  X,
  type LucideIcon,
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

import { Spinner } from "@/components/Spinner";

import {
  APIError,
  approveApproval,
  cancelTurn,
  captureDesktopPhoto,
  captureDesktopScreenshot,
  compactSession,
  denyApproval,
  getAudioBindings,
  getTurn,
  listApps,
  listSkills,
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
import { ChatColumn } from "@/components/ChatColumn";
import { ChoiceMenu, type ChoiceMenuItem } from "@/components/ChoiceMenu";
import { ComposerAddButton } from "@/components/ComposerAddMenu";
import { ComposerFloatingPanel } from "@/components/ComposerFloatingPanel";
import { buildComposerMentionReferences } from "@/components/composerMentionData";
import { ComposerMentionMenu } from "@/components/ComposerMentionMenu";
import { useComposerMentions } from "@/components/useComposerMentions";
import { ContextUsageRing } from "@/components/ContextUsageRing";
import { GitCommitDiffDialog, type GitCommitApproval } from "@/components/GitCommitDiffDialog";
import { ImageLightbox, type ImageLightboxItem } from "@/components/ImageLightbox";
import { InputFlowPanel, type InputFlowSubmission } from "@/components/transcript/InputFlowToolPart";
import { Mascot, type MascotGaze, type MascotGazePoint, type MascotMood } from "@/components/Mascot";
import { PatchProposalDiffDialog, type PatchProposalApproval } from "@/components/PatchProposalDiffDialog";
import { ProjectComposerControls } from "@/components/ProjectComposerControls";
import { BackgroundProcessControl } from "@/components/BackgroundProcessControl";
import { SessionAudioControls } from "@/components/SessionAudioControls";
import { UIContextControl } from "@/components/UIContextControl";
import { upsertTurnIntoPages, type TurnsInfiniteData } from "@/components/transcript/useTranscriptTurns";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { type ResolvedModelSelection } from "@/lib/modelSelection";
import { reasoningEffortOptionsForSelection } from "@/components/ReasoningEffortChip";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useComposerSelectionGuard } from "@/hooks/useComposerSelectionGuard";
import { useImeCompositionGuard } from "@/hooks/useImeCompositionGuard";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { createPastedTextAttachmentFile, shouldAttachPastedText } from "@/lib/clipboardTextAttachment";
import { getLocalFilePath, pickDirectories } from "@/lib/desktopBridge";
import { newClientID } from "@/lib/id";
import { projectReferenceRangeLabel } from "@/lib/projectReferences";
import {
  createLocalFolderPath,
  type DroppedLocalItems,
  pickLocalFolderPaths,
  type LocalFolderPath,
} from "@/lib/localFolders";
import type { AppSearch } from "@/lib/route";
import { getSubmitFailure } from "@/lib/submitFailure";
import { buildDraftSubmitParts, orderedDraftItems, type DraftPartOrderItem } from "@/lib/submitParts";
import { getTextAreaCaretClientPoint } from "@/lib/textCaret";
import { cn } from "@/lib/utils";
import { useOverlayStore, type AssistantOverlay, type AssistantOverlayPart, type TurnPhaseState } from "@/state/overlayStore";
import { useInputFlowStore } from "@/state/inputFlowStore";
import { useSessionDraftStore, type SessionDraftAttachment } from "@/state/sessionDraftStore";
import {
  setUIContextEnabled,
  useUIContextEnabled,
  useVisibleUIContext,
} from "@/state/uiContextStore";
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

type ComposerApproval = Extract<AssistantOverlayPart, { type: "approval" }>;
type ApprovalMenuAction = "approve-session" | "approve-turn" | "deny" | "review-git" | "review-patch";
type SlashCommand = {
  command: string;
  description: string;
  hasArgs: boolean;
  icon: LucideIcon;
  id: "clear" | "compact" | "rename" | "summary";
  label: string;
};
type SlashSubmitCommand =
  | { id: "clear" }
  | { id: "compact"; hint: string }
  | { id: "rename"; title: string }
  | { id: "summary"; hint: string };

type ComposerAttachment = SessionDraftAttachment;
const emptyComposerAttachments: ComposerAttachment[] = [];
const emptyLocalFolders: LocalFolderPath[] = [];
const emptyPartOrder: DraftPartOrderItem[] = [];
const emptyProjectReferences: ProjectReference[] = [];

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
  const overlayRunning = useOverlayStore((state) => Boolean(state.runningTurns[sessionID]));
  const turnPhase = useOverlayStore((state) => state.turnPhases[sessionID]);
  const pendingApproval = useOverlayStore((state) => selectPendingApproval(state.assistants, sessionID, state.runningTurns[sessionID]));
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
  const [mascotErrorMessage, setMascotErrorMessage] = useState<string | null>(null);
  const [mascotErrorSignal, setMascotErrorSignal] = useState(0);
  const [textFocused, setTextFocused] = useState(false);
  const [attachmentPreviewIndex, setAttachmentPreviewIndex] = useState<number | null>(null);
  const [capturingPhoto, setCapturingPhoto] = useState(false);
  const [capturingScreenshot, setCapturingScreenshot] = useState(false);
  const [pickingAttachment, setPickingAttachment] = useState(false);
  const [pickingLocalFolder, setPickingLocalFolder] = useState(false);
  const [slashSelectedIndex, setSlashSelectedIndex] = useState(0);
  const workspaceOpen = useWorkspaceOpen();
  const uiContextEnabled = useUIContextEnabled();
  const visibleUIContext = useVisibleUIContext(sessionID);
  const draftUIContext = workspaceOpen && uiContextEnabled ? visibleUIContext : undefined;
  // clientMessageID 按"草稿"生成而不是按请求生成:失败重试和快速双击
  // 复用同一个 ID,服务端幂等去重才生效;成功后才轮换到下一个草稿 ID。
  const draftIDRef = useRef<string>(newClientID());
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const lastDroppedFilesNonceRef = useRef(0);
  const textAreaRef = useRef<HTMLTextAreaElement | null>(null);
  const mascotErrorTimerRef = useRef<number | null>(null);
  const mascotGazeRafRef = useRef(0);
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
  // 空消息(含纯空白)不可发:禁用发送按钮 + Enter 不触发提交,从源头避免
  // 弹出 zod min(1) 的校验错误。watch 让按钮态随输入实时更新。
  const draftText = form.watch("text");
  const trimmedDraftText = draftText.trim();
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
  const draftSlashCommand = hasAttachments || hasLocalFolders || hasProjectReferences ? null : parseSlashSubmitCommand(trimmedDraftText);
  const canSend = Boolean(trimmedDraftText || uploadedAttachments.length || hasLocalFolders || hasProjectReferences) && !hasPendingAttachments && !hasFailedAttachments;
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
  const slashQuery = slashCommandQuery(trimmedDraftText, slashCommands);
  const visibleSlashCommands =
    textFocused && slashQuery !== null
      ? slashCommands.filter((command) => command.command.startsWith("/" + slashQuery))
      : [];
  const slashMenuOpen = visibleSlashCommands.length > 0;
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
  const setComposerText = useCallback(
    (nextText: string) => {
      form.setValue("text", nextText, { shouldDirty: true });
      setSessionDraftText(sessionID, nextText);
      if (textAreaRef.current && textAreaRef.current.value !== nextText) {
        textAreaRef.current.value = nextText;
      }
    },
    [form, sessionID, setSessionDraftText],
  );
  const mentions = useComposerMentions({
    references: mentionReferences,
    text: draftText,
    setText: setComposerText,
    textAreaRef,
    onAction: (actionID) => {
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
    },
  });
  const mentionMenuOpen = textFocused && mentions.open && !slashMenuOpen;
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
    if (!slashMenuOpen) {
      setSlashSelectedIndex(0);
      return;
    }
    setSlashSelectedIndex((index) => Math.min(index, visibleSlashCommands.length - 1));
  }, [slashMenuOpen, visibleSlashCommands.length]);

  useEffect(() => {
    if (reasoningEffort && !reasoningOptions.includes(reasoningEffort)) {
      setSessionReasoningEffort("");
    }
  }, [reasoningEffort, reasoningOptions, setSessionReasoningEffort]);

  const clearMascotError = useCallback(() => {
    if (mascotErrorTimerRef.current) {
      window.clearTimeout(mascotErrorTimerRef.current);
      mascotErrorTimerRef.current = null;
    }
    setMascotErrorMessage(null);
  }, []);
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
  const openMentionMenuFromButton = useCallback(() => {
    setTextFocused(true);
    mentions.openManual();
  }, [mentions]);
  // Prefer this mascot feedback for composer-local validation and command errors.
  const showMascotError = useCallback(
    (message: string) => {
      onSubmitError?.(null);
      setMascotErrorMessage(message);
      setMascotErrorSignal((signal) => signal + 1);
      if (mascotErrorTimerRef.current) {
        window.clearTimeout(mascotErrorTimerRef.current);
      }
      mascotErrorTimerRef.current = window.setTimeout(() => {
        mascotErrorTimerRef.current = null;
        setMascotErrorMessage(null);
      }, 3600);
    },
    [onSubmitError],
  );
  const submitMutation = useMutation({
    mutationFn: async (value: z.infer<typeof composerSchema> & { parts: ContentPart[] }) => {
      const clientMessageID = draftIDRef.current;
      if (!resolvedModel) {
        throw new APIError(400, "no_model");
      }
      const { provider, model } = resolvedModel;
      addPendingUser({
        sessionID,
        clientMessageID,
        status: "submitting",
        text: value.text,
        parts: value.parts,
        createdAt: new Date().toISOString(),
      });
      if (session.provider !== provider || session.model !== model) {
        await updateSession(token, sessionID, { provider, model });
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
      clearMascotError();
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
      showMascotError(failure.message);
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
      clearMascotError();
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
      showMascotError(failure.message);
    },
  });
  const cancelMutation = useMutation({
    mutationFn: () => cancelTurn(token, sessionID),
  });
  const compactMutation = useMutation({
    mutationFn: ({ hint }: { hint: string }) => compactSession(token, sessionID, { hint }),
    onMutate: () => {
      clearMascotError();
      onSubmitError?.(null);
      startCompactRun(sessionID);
      resetSessionDraft();
    },
    onSuccess: async () => {
      clearMascotError();
      onSubmitError?.(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.turns(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessionUsage(sessionID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      showMascotError(compactErrorMessage(error, t));
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
      clearMascotError();
      onSubmitError?.(null);
      resetSessionDraft();
    },
    onSuccess: async () => {
      clearMascotError();
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
      showMascotError(failure.message);
    },
  });
  const renameMutation = useMutation({
    mutationFn: ({ title }: { title: string }) => updateSession(token, sessionID, { title }),
    onSuccess: async () => {
      clearMascotError();
      onSubmitError?.(null);
      resetSessionDraft();
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: () => {
      showMascotError(t("composer.renameFailed"));
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
  const showStopButton = running || cancelMutation.isPending;
  const showSendButton =
    !showStopButton ||
    (canSend && !submitMutation.isPending && !compactMutation.isPending && !systemSubmitMutation.isPending && !renameMutation.isPending);

  const runClearCommand = useCallback(() => {
    clearMascotError();
    onSubmitError?.(null);
    resetSessionDraft();
    setTextFocused(false);
    void navigate({
      to: "/",
      search: (prev) => {
        const next = { ...(prev as AppSearch), draft: "1" };
        delete next.session;
        return next;
      },
    });
  }, [clearMascotError, navigate, onSubmitError, resetSessionDraft]);

  const submitDraft = (value: z.infer<typeof composerSchema>) => {
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
        showMascotError(t("composer.renameMissing"));
        return;
      }
      clearMascotError();
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
    clearMascotError();
    onSubmitError?.(null);
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
      text,
      parts: draftUIContext ? [...inputParts, draftUIContext] : inputParts,
    });
  };

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
  const ime = useImeCompositionGuard({ onCompositionEnd: scheduleMascotInputGaze });
  const setTextAreaRef = (node: HTMLTextAreaElement | null) => {
    textAreaRef.current = node;
    textField.ref(node);
  };
  const handleTextBlur = (event: FocusEvent<HTMLTextAreaElement>) => {
    textField.onBlur(event);
    mentions.close();
    setTextFocused(false);
    setMascotInputGaze(null);
  };
  const handleTextFocus = () => {
    setTextFocused(true);
    if (textAreaRef.current) {
      mentions.notifyCursor(textAreaRef.current.selectionStart);
    }
    scheduleMascotInputGaze();
  };
  const handleTextCursorUpdate = (event: { currentTarget: HTMLTextAreaElement }) => {
    mentions.notifyCursor(event.currentTarget.selectionStart);
    scheduleMascotInputGaze();
  };
  const handleTextChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    const previousText = form.getValues("text");
    const nextText = event.target.value;
    void textField.onChange(event);
    setSessionDraftText(sessionID, nextText);
    mentions.notifyChange(nextText, previousText, event.target.selectionStart);
    if (mascotErrorMessage) {
      clearMascotError();
    }
    setTextFocused(true);
    scheduleMascotInputGaze();
  };
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
  const selectSlashCommand = (command: SlashCommand) => {
    if (!command.hasArgs) {
      runClearCommand();
      return;
    }
    const nextText = command.command + " ";
    form.setValue("text", nextText);
    setSessionDraftText(sessionID, nextText);
    window.requestAnimationFrame(() => {
      textAreaRef.current?.focus();
      scheduleMascotInputGaze();
    });
  };
  const handleTextKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "@" && !event.metaKey && !event.ctrlKey && !event.altKey) {
      mentions.notifyCursor(event.currentTarget.selectionStart + 1);
    }
    if (mentions.onKeyDown(event)) {
      scheduleMascotInputGaze();
      return;
    }
    if (slashMenuOpen) {
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          setSlashSelectedIndex((index) => (index + 1) % visibleSlashCommands.length);
          return;
        case "ArrowUp":
          event.preventDefault();
          setSlashSelectedIndex((index) => (index - 1 + visibleSlashCommands.length) % visibleSlashCommands.length);
          return;
        case "Enter":
        case "Tab": {
          event.preventDefault();
          const command = visibleSlashCommands[slashSelectedIndex] ?? visibleSlashCommands[0];
          if (command) {
            selectSlashCommand(command);
          }
          return;
        }
        case "Escape":
          event.preventDefault();
          setTextFocused(false);
          return;
      }
    }
    if (event.key === "Enter" && !event.shiftKey) {
      if (ime.isComposing(event)) {
        scheduleMascotInputGaze();
        return;
      }
      event.preventDefault();
      if (sendEnabled) {
        void form.handleSubmit(submitDraft)();
      }
    }
    scheduleMascotInputGaze();
  };

  useEffect(() => {
    return () => {
      if (mascotGazeRafRef.current) {
        window.cancelAnimationFrame(mascotGazeRafRef.current);
      }
      if (mascotErrorTimerRef.current) {
        window.clearTimeout(mascotErrorTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    setMascotGaze({ type: "pointer" });
  }, [sessionID]);

  return (
    <>
      <form
        className="relative shrink-0 pt-2 pb-4"
        onSubmit={form.handleSubmit(submitDraft)}
      >
      {/* 底部遮罩:滚动内容贴近输入区时淡出,随 composer 定位、宽度走 ChatColumn。
          文字边缘外漏不归遮罩管——那是 WKWebView 字形渲染溢出,Transcript overflow-hidden 裁掉 */}
      <div className="pointer-events-none absolute inset-x-0 -top-10">
        <ChatColumn>
          <div className="h-10 bg-gradient-to-t from-background to-transparent" />
        </ChatColumn>
      </div>
      <ChatColumn>
        <div ref={selectionGuardRef} className="relative">
          <ComposerApprovalBar approval={pendingApproval} token={token} />
          {pendingInputFlow ? (
            <InputFlowPanel key={pendingInputFlow.id} request={pendingInputFlow} onSubmit={(submission) => inputFlowSubmitMutation.mutate(submission)} />
          ) : null}
          {mentionMenuOpen ? (
            <ComposerMentionMenu
              references={mentions.filtered}
              query={mentions.query}
              selectedIndex={mentions.activeIndex}
              onHover={mentions.setActiveIndex}
              onSelect={mentions.select}
            />
          ) : slashMenuOpen ? (
            <SlashCommandMenu
              commands={visibleSlashCommands}
              selectedIndex={slashSelectedIndex}
              onHover={setSlashSelectedIndex}
              onSelect={selectSlashCommand}
            />
          ) : null}
          <div
            className={cn(
              "pudding-composer-shell relative isolate z-10 rounded-[18px] border border-border/70 bg-card/95 transition-shadow",
              micActive && "is-mic-active",
            )}
          >
            <input
              ref={fileInputRef}
              className="sr-only"
              accept="image/*,audio/*,text/*,application/pdf,.txt,.md,.csv,.json,.xml,.yaml,.yml"
              multiple
              type="file"
              onChange={handleAttachmentInputChange}
            />
            {attachments.length > 0 || localFolders.length > 0 || projectReferences.length > 0 ? (
              <div className="flex flex-wrap gap-2 px-3 pt-3">
                {orderedDraftItems(attachments, localFolders, partOrder, projectReferences).map((orderedItem) =>
                  orderedItem.type === "attachment" ? (
                    <ComposerAttachmentChip
                      key={`attachment:${orderedItem.item.id}`}
                      item={orderedItem.item}
                      previewIndex={attachmentPreviewIndexByID.get(orderedItem.item.id)}
                      removeLabel={t("composer.removeAttachment")}
                      token={token}
                      onPreview={setAttachmentPreviewIndex}
                      onRevealSource={revealLocalPath}
                      onRemove={() => removeAttachment(orderedItem.item.id)}
                    />
                  ) : orderedItem.type === "local_folder" ? (
                    <LocalFolderChip
                      key={`folder:${orderedItem.item.id}`}
                      folder={orderedItem.item}
                      label={t("composer.folderLabel")}
                      removeLabel={t("composer.removeFolder")}
                      onReveal={() => revealLocalPath(orderedItem.item.path)}
                      onRemove={() => removeLocalFolder(orderedItem.item.id)}
                    />
                  ) : (
                    <ProjectReferenceChip
                      key={`project-reference:${orderedItem.item.id}`}
                      reference={orderedItem.item}
                      fileLabel={t("composer.projectFileLabel")}
                      folderLabel={t("composer.projectFolderLabel")}
                      removeLabel={t("composer.removeProjectReference")}
                      onRemove={() => removeProjectReference(orderedItem.item.id)}
                    />
                  ),
                )}
              </div>
            ) : null}
            <div className="px-4 pt-3.5 pb-2.5">
              <Textarea
                data-composer-text-input="true"
                className="block max-h-36 min-h-6 resize-none overflow-y-auto rounded-none border-0 bg-transparent p-0 text-base leading-6 shadow-none focus-visible:ring-0 md:text-sm dark:bg-transparent"
                placeholder={
                  session.activeMode === "chat"
                    ? t("composer.messagePlaceholder")
                    : t("composer.modeMessagePlaceholder").replace(
                        "{mode}",
                        t(`mode.${session.activeMode}`),
                      )
                }
                rows={1}
                name={textField.name}
                ref={setTextAreaRef}
                onBlur={handleTextBlur}
                onChange={handleTextChange}
                onCompositionEnd={ime.onCompositionEnd}
                onCompositionStart={ime.onCompositionStart}
                onClick={handleTextCursorUpdate}
                onFocus={handleTextFocus}
                onKeyDown={handleTextKeyDown}
                onKeyUp={handleTextCursorUpdate}
                onMouseUp={handleTextCursorUpdate}
                onPaste={handleTextPaste}
                onSelect={handleTextCursorUpdate}
              />
            </div>
            <div className="flex min-w-0 items-center gap-1 px-2 pb-2">
              <ComposerAddButton
                active={mentionMenuOpen}
                busy={capturingPhoto || capturingScreenshot || pickingAttachment || pickingLocalFolder}
                label={t("composer.addMenuTitle")}
                onClick={openMentionMenuFromButton}
              />
              <ProjectComposerControls projectID={projectID} token={token} />
              {workspaceOpen && visibleUIContext ? (
                <UIContextControl
                  context={visibleUIContext}
                  enabled={uiContextEnabled}
                  onEnabledChange={setUIContextEnabled}
                />
              ) : null}
              <BackgroundProcessControl sessionID={sessionID} token={token} />
              {compactMutation.isPending ? (
                <span
                  aria-live="polite"
                  className="min-w-0 max-w-40 truncate px-1 text-xs text-muted-foreground"
                  role="status"
                >
                  {t("composer.compacting")}
                </span>
              ) : null}
              <div className="ml-auto flex min-w-0 items-center gap-1">
                <ContextUsageRing mode={session.activeMode} token={token} sessionID={sessionID} />
                <ModelReasoningPicker
                  className="min-w-0"
                  token={token}
                  session={session}
                  reasoningValue={reasoningEffort}
                  onAfterClose={focusTextarea}
                  onReasoningChange={(value) => {
                    setSessionReasoningEffort(value);
                  }}
                  onResolvedChange={handleResolvedModelChange}
                />
              </div>
              <SessionAudioControls audioInputSupported={audioInputSupported} bindings={audioBindings} token={token} sessionID={session.id} />
              {showStopButton ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      aria-label={t("composer.stop")}
                      className="rounded-full !bg-foreground !text-background shadow-sm hover:!bg-foreground/90 hover:!text-background dark:hover:!bg-foreground/90"
                      disabled={!stopEnabled}
                      size="icon"
                      type="button"
                      variant="ghost"
                      onClick={() => {
                        if (!stopEnabled) {
                          return;
                        }
                        cancelMutation.mutate();
                      }}
                    >
                      {cancelMutation.isPending ? (
                        <Spinner />
                      ) : (
                        <span aria-hidden="true" className="size-2.5 rounded-[2px] bg-current" />
                      )}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{t("composer.stop")}</TooltipContent>
                </Tooltip>
              ) : null}
              {showSendButton ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      aria-label={t("composer.send")}
                      className="rounded-full disabled:bg-control-disabled disabled:text-background disabled:opacity-100 disabled:shadow-none"
                      disabled={!sendEnabled}
                      size="icon"
                      type="submit"
                      variant={sendEnabled ? "default" : "secondary"}
                    >
                      {submitMutation.isPending || compactMutation.isPending || systemSubmitMutation.isPending || renameMutation.isPending ? (
                        <Spinner />
                      ) : (
                        <ArrowUp />
                      )}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{t("composer.send")}</TooltipContent>
                </Tooltip>
              ) : null}
            </div>
          </div>
          <span className="absolute z-30 size-12 overflow-visible" style={{ left: 6, top: -36 }}>
            <Mascot
              className="size-full overflow-visible"
              gaze={mascotErrorMessage ? { type: "center" } : mascotGaze}
              inputPitchBias={MASCOT_INPUT_PITCH_BIAS}
              mood={mascotErrorMessage ? "error" : mascotMoodFromPhase(turnPhase, running)}
              headShakeSignal={mascotErrorSignal}
              onPointerGaze={setMascotPointerGaze}
            />
          </span>
          {mascotErrorMessage ? (
            <span
              aria-live="polite"
              className="pointer-events-none absolute z-30 max-w-[min(28rem,calc(100%-4rem))] truncate px-1 text-xs font-semibold text-destructive"
              role="status"
              style={{ left: 56, top: -16 }}
            >
              {mascotErrorMessage}
            </span>
          ) : null}
        </div>
      </ChatColumn>
      </form>
      <ImageLightbox images={attachmentPreviewItems} openIndex={attachmentPreviewIndex} onOpenIndexChange={setAttachmentPreviewIndex} />
    </>
  );
}

function LocalFolderChip({
  folder,
  label,
  removeLabel,
  onReveal,
  onRemove,
}: {
  folder: LocalFolderPath;
  label: string;
  removeLabel: string;
  onReveal: () => void;
  onRemove: () => void;
}) {
  return (
    <div
      className="relative inline-flex h-10 max-w-full items-center gap-1.5 rounded-lg border border-border/70 bg-card pr-7 pl-2.5 text-sm whitespace-nowrap shadow-sm"
      title={folder.path}
    >
      <button className="inline-flex min-w-0 items-center gap-1.5 text-left whitespace-nowrap" type="button" onClick={onReveal}>
        <FolderOpen className="size-4 shrink-0 text-muted-foreground" strokeWidth={1.8} />
        <span className="min-w-0 truncate font-medium leading-5 text-foreground">{folder.name}</span>
        <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">{label}</span>
      </button>
      <button
        aria-label={removeLabel}
        className="absolute top-2 right-1.5 grid size-5 place-items-center rounded-full bg-foreground text-background shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
        type="button"
        onClick={onRemove}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}

function ProjectReferenceChip({
  reference,
  fileLabel,
  folderLabel,
  removeLabel,
  onRemove,
}: {
  reference: ProjectReference;
  fileLabel: string;
  folderLabel: string;
  removeLabel: string;
  onRemove: () => void;
}) {
  const Icon = reference.kind === "dir" ? FolderOpen : FileText;
  const range = projectReferenceRangeLabel(reference);
  return (
    <div
      className="relative inline-flex h-10 max-w-full items-center gap-1.5 rounded-lg border border-border/70 bg-card pr-7 pl-2.5 text-sm whitespace-nowrap shadow-sm"
      title={reference.path}
    >
      <Icon className="size-4 shrink-0 text-muted-foreground" strokeWidth={1.8} />
      <span className="min-w-0 truncate font-medium leading-5 text-foreground">{reference.name}</span>
      <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">
        {range || (reference.kind === "dir" ? folderLabel : fileLabel)}
      </span>
      <button
        aria-label={removeLabel}
        className="absolute top-2 right-1.5 grid size-5 place-items-center rounded-full bg-foreground text-background shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
        type="button"
        onClick={onRemove}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}

function ComposerAttachmentChip({
  item,
  previewIndex,
  removeLabel,
  token,
  onPreview,
  onRevealSource,
  onRemove,
}: {
  item: ComposerAttachment;
  previewIndex?: number;
  removeLabel: string;
  token: string;
  onPreview: (index: number) => void;
  onRevealSource: (path: string) => void;
  onRemove: () => void;
}) {
  const src = composerAttachmentImageSource(item, token);
  const image = isImageAttachmentLike(item.attachment?.mime, item.name) && src;
  const audio = isAudioAttachmentLike(item.attachment?.mime, item.name);
  const busy = item.status === "uploading";
  if (image) {
    return (
      <div
        className={cn(
          "group relative h-16 w-16 shrink-0 overflow-hidden rounded-lg border bg-muted/40 shadow-sm",
          item.status === "error" ? "border-destructive/40" : "border-border/70",
        )}
      >
        <button
          aria-label={item.name}
          className="block h-full w-full"
          type="button"
          onClick={() => {
            if (previewIndex !== undefined) {
              onPreview(previewIndex);
            }
          }}
        >
          <img alt={item.name} className="h-full w-full object-cover" src={src} />
        </button>
        {busy ? (
          <span className="absolute inset-0 grid place-items-center bg-background/45">
            <Spinner className="size-4 text-foreground" />
          </span>
        ) : null}
        <button
          aria-label={removeLabel}
          className="absolute top-1.5 right-1.5 z-10 grid size-5 place-items-center rounded-full border border-black/10 bg-white text-black shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
          type="button"
          onClick={onRemove}
        >
          <X className="size-3" />
        </button>
      </div>
    );
  }
  return (
    <div
      className={cn(
        "relative inline-flex h-10 max-w-full items-center gap-1.5 rounded-lg border bg-card pr-7 pl-2.5 text-sm whitespace-nowrap shadow-sm",
        item.attachment?.sourcePath && "cursor-pointer",
        item.status === "error" ? "border-destructive/40 bg-destructive/10 text-destructive" : "border-border/70 text-muted-foreground",
      )}
      title={`${item.name} ${formatAttachmentSize(item.size)}`}
      onClick={() => {
        if (item.attachment?.sourcePath) {
          onRevealSource(item.attachment.sourcePath);
        }
      }}
    >
      <span className="grid size-4 shrink-0 place-items-center text-muted-foreground">
        {busy ? (
          <Spinner className="size-4" />
        ) : audio ? (
          <AudioPreviewButton label={item.name} src={attachmentResourceURL(item.attachment, token)} />
        ) : (
          <FileText className="size-4" strokeWidth={1.8} />
        )}
      </span>
      <span className="inline-flex min-w-0 items-center gap-1.5 whitespace-nowrap">
        <span className="truncate font-medium leading-5 text-foreground">{item.name}</span>
        <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">{attachmentKindLabel(item.name, item.attachment?.mime)}</span>
      </span>
      <button
        aria-label={removeLabel}
        className="absolute top-2 right-1.5 grid size-5 place-items-center rounded-full bg-foreground text-background shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          onRemove();
        }}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}

function AudioPreviewButton({ label, src }: { label: string; src: string }) {
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [playing, setPlaying] = useState(false);
  return (
    <>
      <button
        aria-label={label}
        className="grid size-full place-items-center rounded-sm hover:text-foreground"
        disabled={!src}
        type="button"
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          const audio = audioRef.current;
          if (!audio) {
            return;
          }
          if (audio.paused) {
            void audio.play();
          } else {
            audio.pause();
          }
        }}
      >
        {playing ? <Pause className="size-4" fill="currentColor" strokeWidth={1.8} /> : <Play className="size-4" fill="currentColor" strokeWidth={1.8} />}
      </button>
      {src ? (
        <audio
          ref={audioRef}
          preload="none"
          src={src}
          onEnded={() => setPlaying(false)}
          onPause={() => setPlaying(false)}
          onPlay={() => setPlaying(true)}
        />
      ) : null}
    </>
  );
}

function SlashCommandMenu({
  commands,
  selectedIndex,
  onHover,
  onSelect,
}: {
  commands: SlashCommand[];
  selectedIndex: number;
  onHover: (index: number) => void;
  onSelect: (command: SlashCommand) => void;
}) {
  const selectedRef = useRef<HTMLButtonElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    scrollActiveSlashCommandIntoList(selectedRef.current, listRef.current);
  }, [selectedIndex]);

  return (
    <div
      ref={listRef}
      className="pudding-composer-suggestion absolute bottom-[calc(100%-3px)] left-16 z-[5] max-h-64 w-[min(30rem,calc(100%-6rem))] overflow-y-auto rounded-t-lg border border-b-0 bg-popover/95 p-1 text-sm text-popover-foreground backdrop-blur"
      role="listbox"
    >
      {commands.map((command, index) => (
        <button
          key={command.id}
          ref={index === selectedIndex ? selectedRef : undefined}
          aria-selected={index === selectedIndex}
          aria-label={`${command.label} ${command.description}`}
          className={cn(
            "flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-[12px] hover:bg-muted",
            index === selectedIndex && "bg-muted text-foreground",
          )}
          role="option"
          type="button"
          onMouseEnter={() => onHover(index)}
          onMouseDown={(event) => {
            event.preventDefault();
            onSelect(command);
          }}
        >
          <SlashCommandIcon command={command} />
          <span className="shrink-0 font-medium">{command.label}</span>
          <span className="ml-1 min-w-0 flex-1 truncate text-muted-foreground/65">{command.description}</span>
        </button>
      ))}
    </div>
  );
}

function SlashCommandIcon({ command }: { command: SlashCommand }) {
  const Icon = command.icon;
  return (
    <span className="grid size-5 shrink-0 place-items-center text-foreground/70">
      <Icon className="size-4" strokeWidth={2.15} />
    </span>
  );
}

function scrollActiveSlashCommandIntoList(active: HTMLElement | null, list: HTMLElement | null) {
  if (!active || !list) {
    return;
  }
  const activeTop = active.offsetTop;
  const activeBottom = activeTop + active.offsetHeight;
  const visibleTop = list.scrollTop;
  const visibleBottom = visibleTop + list.clientHeight;
  if (activeTop < visibleTop) {
    list.scrollTop = activeTop;
  } else if (activeBottom > visibleBottom) {
    list.scrollTop = activeBottom - list.clientHeight;
  }
}

function parseSlashSubmitCommand(text: string): SlashSubmitCommand | null {
  if (text === "/clear") {
    return { id: "clear" };
  }
  const compactMatch = text.match(/^\/compact(?:\s+([\s\S]*))?$/);
  if (compactMatch) {
    return { id: "compact", hint: (compactMatch[1] || "").trim() };
  }
  const renameMatch = text.match(/^\/rename(?:\s+([\s\S]*))?$/);
  if (renameMatch) {
    return { id: "rename", title: (renameMatch[1] || "").trim() };
  }
  const summaryMatch = text.match(/^\/summary(?:\s+([\s\S]*))?$/);
  if (summaryMatch) {
    return { id: "summary", hint: (summaryMatch[1] || "").trim() };
  }
  return null;
}

function formatAttachmentSize(size: number) {
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${Math.round(size / 1024)} KB`;
  }
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
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

function attachmentKindLabel(name: string, mime?: string) {
  const ext = name.split(".").pop()?.trim();
  if (ext && ext !== name) {
    return ext.toUpperCase();
  }
  const major = mime?.split("/")[0]?.trim();
  return major ? major.toUpperCase() : "FILE";
}

function isImageAttachmentLike(mime: string | undefined, name: string) {
  const cleaned = (mime || "").toLowerCase();
  if (cleaned.startsWith("image/") && cleaned !== "image/svg+xml") {
    return true;
  }
  return /\.(png|jpe?g|gif|webp)$/i.test(name);
}

function isAudioAttachmentLike(mime: string | undefined, name: string) {
  const cleaned = (mime || "").toLowerCase();
  if (cleaned.startsWith("audio/")) {
    return true;
  }
  return /\.(wav|mp3|m4a|aac|ogg|oga|flac|webm)$/i.test(name);
}

function composerAttachmentImageSource(item: ComposerAttachment, token: string) {
  return item.previewURL || attachmentResourceURL(item.attachment, token);
}

function revokeAttachmentPreview(item: ComposerAttachment) {
  if (item.previewURL) {
    URL.revokeObjectURL(item.previewURL);
  }
}

function summaryPrompt(hint: string, t: (key: string) => string) {
  if (hint) {
    return t("composer.summaryPromptWithHint").replace("{hint}", hint);
  }
  return t("composer.summaryPrompt");
}

function slashCommandQuery(text: string, commands: SlashCommand[]): string | null {
  if (!text.startsWith("/") || /\s/.test(text)) {
    return null;
  }
  const query = text.slice(1);
  if (commands.some((command) => command.command === text)) {
    return null;
  }
  return query;
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

function ComposerApprovalBar({ approval, token }: { approval?: ComposerApproval; token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [pendingAction, setPendingAction] = useState<"turn" | "session" | "deny" | null>(null);
  const [selectedProjectDirs, setSelectedProjectDirs] = useState<string[]>([]);
  const [pickingProjectDir, setPickingProjectDir] = useState(false);
  const [viewingPatchProposal, setViewingPatchProposal] = useState<PatchProposalApproval | null>(null);
  const [viewingGitCommit, setViewingGitCommit] = useState<GitCommitApproval | null>(null);
  useEffect(() => {
    setSelectedProjectDirs([]);
    setViewingPatchProposal(null);
    setViewingGitCommit(null);
  }, [approval?.approvalID]);
  if (!approval) {
    return null;
  }
  const current = approval;
  const isToolCallApproval = current.approvalKind === "tool_call";
  const targetMode = approvalTargetMode(current.payload);
  const title = approvalTitle(current, targetMode, t);
  const pending = pendingAction !== null;
  const isCodeApproval = targetMode === "code";
  const payloadProjectDirs = projectDirsFromPayload(current.payload);
  const hasPayloadProjectDirs = payloadProjectDirs.length > 0;
  const projectDirs = hasPayloadProjectDirs ? payloadProjectDirs : selectedProjectDirs;
  const suggestedDirName = suggestedProjectDirName(current.payload);
  const toolCallApproval = toolCallFromPayload(current.payload);
  const patchProposal = patchProposalFromPayload(current.payload);
  const gitCommitApproval = gitCommitFromPayload(current.payload);
  const approvalReason = isToolCallApproval ? toolCallReason(toolCallApproval.operation, t) || current.reason : current.reason;

  async function approve(scope: "turn" | "session") {
    if (pending) {
      return;
    }
    setPendingAction(scope);
    try {
      await approveApproval(token, current.sessionID, current.approvalID, scope, isCodeApproval ? projectDirs : []);
      if (scope === "session") {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
          queryClient.invalidateQueries({ queryKey: queryKeys.projects() }),
        ]);
      }
      setViewingPatchProposal(null);
      setViewingGitCommit(null);
    } finally {
      setPendingAction(null);
    }
  }

  async function pickProjectDirs() {
    if (pending || pickingProjectDir || hasPayloadProjectDirs) {
      return;
    }
    setPickingProjectDir(true);
    try {
      const dirs = await pickProjectDirectories(t);
      if (dirs.length > 0) {
        setSelectedProjectDirs((prev) => dedupeStrings([...prev, ...dirs]));
      }
    } catch {
      toast.error(t("transcript.approvalProjectDirPickFailed"));
    } finally {
      setPickingProjectDir(false);
    }
  }

  function removeProjectDir(dir: string) {
    setSelectedProjectDirs((prev) => prev.filter((item) => item !== dir));
  }

  async function deny() {
    if (pending) {
      return;
    }
    setPendingAction("deny");
    try {
      await denyApproval(token, current.sessionID, current.approvalID);
      setViewingPatchProposal(null);
      setViewingGitCommit(null);
    } finally {
      setPendingAction(null);
    }
  }

  const approvalMenuItems: Array<ChoiceMenuItem<ApprovalMenuAction>> = [];
  if (isToolCallApproval) {
    approvalMenuItems.push({
      id: "approve-turn",
      label: approvalToolActionLabel(toolCallApproval.operation, Boolean(patchProposal), Boolean(gitCommitApproval), t),
      value: "approve-turn",
      render: () => (
        <ApprovalMenuOption
          description={t("transcript.approvalAllowToolCallDesc")}
          icon={Check}
          label={approvalToolActionLabel(toolCallApproval.operation, Boolean(patchProposal), Boolean(gitCommitApproval), t)}
          loading={pendingAction === "turn"}
        />
      ),
    });
    if (patchProposal) {
      approvalMenuItems.push({
        id: "review-patch",
        label: t("transcript.approvalPatchReview"),
        value: "review-patch",
        render: () => <ApprovalMenuOption description={t("transcript.approvalReviewDesc")} icon={FileText} label={t("transcript.approvalPatchReview")} />,
      });
    }
    if (gitCommitApproval) {
      approvalMenuItems.push({
        id: "review-git",
        label: t("transcript.approvalGitCommitReview"),
        value: "review-git",
        render: () => <ApprovalMenuOption description={t("transcript.approvalReviewDesc")} icon={FileText} label={t("transcript.approvalGitCommitReview")} />,
      });
    }
  } else {
    approvalMenuItems.push(
      {
        id: "approve-turn",
        label: t("transcript.approvalAllowTurn"),
        value: "approve-turn",
        render: () => (
          <ApprovalMenuOption
            description={t("transcript.approvalAllowTurnDesc")}
            icon={Check}
            label={t("transcript.approvalAllowTurn")}
            loading={pendingAction === "turn"}
          />
        ),
      },
      {
        id: "approve-session",
        label: t("transcript.approvalAllowSession"),
        value: "approve-session",
        render: () => (
          <ApprovalMenuOption
            description={t("transcript.approvalAllowSessionDesc")}
            icon={ShieldCheck}
            label={t("transcript.approvalAllowSession")}
            loading={pendingAction === "session"}
          />
        ),
      },
    );
  }
  approvalMenuItems.push({
    id: "deny",
    label: approvalDenyLabel(toolCallApproval.operation, Boolean(patchProposal), Boolean(gitCommitApproval), t),
    value: "deny",
    render: () => (
      <ApprovalMenuOption
        description={t("transcript.approvalDenyDesc")}
        icon={X}
        label={approvalDenyLabel(toolCallApproval.operation, Boolean(patchProposal), Boolean(gitCommitApproval), t)}
        loading={pendingAction === "deny"}
      />
    ),
  });

  function selectApprovalAction(action: ApprovalMenuAction) {
    switch (action) {
      case "approve-turn":
        void approve("turn");
        return;
      case "approve-session":
        void approve("session");
        return;
      case "deny":
        void deny();
        return;
      case "review-patch":
        if (patchProposal) {
          setViewingPatchProposal(patchProposal);
        }
        return;
      case "review-git":
        if (gitCommitApproval) {
          setViewingGitCommit(gitCommitApproval);
        }
        return;
    }
  }

  return (
    <ComposerFloatingPanel className="right-4 grid gap-1 overflow-y-auto px-3 py-2 text-xs sm:right-8">
      <div className="flex min-w-0 items-center gap-1.5">
        <ShieldCheck className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 truncate font-medium">{title}</span>
      </div>
      {approvalReason ? <div className="line-clamp-2 leading-5 text-muted-foreground">{approvalReason}</div> : null}
      {isToolCallApproval && toolCallApproval.command ? (
        <div className="max-h-28 overflow-auto rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
          <pre className="whitespace-pre-wrap break-words"><span className="select-none text-muted-foreground">$ </span>{toolCallApproval.command}</pre>
        </div>
      ) : null}
      {isToolCallApproval && toolCallApproval.paths.length > 0 && !patchProposal && !gitCommitApproval ? (
        <div className="grid gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
          {toolCallApproval.paths.map((path) => (
            <div key={path} className="truncate" title={path}>
              {path}
            </div>
          ))}
        </div>
      ) : null}
      {isToolCallApproval && patchProposal ? (
        <div className="flex min-w-0 items-center gap-3 text-[11px] text-muted-foreground">
          <span>{t("transcript.approvalPatchFiles").replace("{count}", String(patchProposal.fileCount))}</span>
          <span className="font-mono text-success">+{patchProposal.additions}</span>
          <span className="font-mono text-destructive">-{patchProposal.deletions}</span>
        </div>
      ) : null}
      {isToolCallApproval && gitCommitApproval ? (
        <div className="flex min-w-0 items-center gap-3 text-[11px] text-muted-foreground">
          <span>{t("transcript.approvalPatchFiles").replace("{count}", String(gitCommitApproval.fileCount))}</span>
          <span className="font-mono text-success">+{gitCommitApproval.additions}</span>
          <span className="font-mono text-destructive">-{gitCommitApproval.deletions}</span>
          <span className="min-w-0 truncate">{gitCommitApproval.commitMessage}</span>
        </div>
      ) : null}
      {isCodeApproval ? (
        <div className="grid gap-1">
          <div className="text-[11px] font-medium text-muted-foreground">
            {t("transcript.approvalProjectDirs")}
          </div>
          {projectDirs.length > 0 ? (
            <div className="grid gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
              {projectDirs.map((dir) => (
                <div key={dir} className="flex min-w-0 items-center gap-1" title={dir}>
                  <span className="min-w-0 flex-1 truncate">{dir}</span>
                  {!hasPayloadProjectDirs ? (
                    <button
                      aria-label={t("common.delete")}
                      className="grid size-4 shrink-0 place-items-center rounded-full text-muted-foreground hover:text-foreground"
                      type="button"
                      onClick={() => removeProjectDir(dir)}
                    >
                      <X className="size-3" />
                    </button>
                  ) : null}
                </div>
              ))}
            </div>
          ) : null}
          {projectDirs.length === 0 ? (
            <div className="text-[11px] leading-4 text-muted-foreground">{t("transcript.approvalProjectDirsOptional")}</div>
          ) : null}
          {!hasPayloadProjectDirs ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <Button
                className="h-6 gap-1 rounded-full px-2 text-[11px]"
                disabled={pending || pickingProjectDir}
                size="sm"
                type="button"
                variant="secondary"
                onClick={() => void pickProjectDirs()}
              >
                {pickingProjectDir ? <Spinner className="size-3" /> : <FolderOpen className="size-3" />}
                {t("transcript.approvalProjectDirChoose")}
              </Button>
              {suggestedDirName ? <span className="text-[11px] text-muted-foreground">{t("transcript.approvalProjectDirsSuggested").replace("{name}", suggestedDirName)}</span> : null}
            </div>
          ) : null}
        </div>
      ) : null}
      <ChoiceMenu
        busy={pending}
        className="mt-0.5 border-t border-border/60 pt-1"
        focusMode="when-idle"
        items={approvalMenuItems}
        maxHeightClassName="max-h-44"
        onEscape={() => void deny()}
        onSelect={selectApprovalAction}
      />
      <PatchProposalDiffDialog
        applying={pendingAction === "turn"}
        proposal={viewingPatchProposal}
        rejecting={pendingAction === "deny"}
        onApply={() => void approve("turn")}
        onOpenChange={(open) => !open && setViewingPatchProposal(null)}
        onReject={() => void deny()}
      />
      <GitCommitDiffDialog
        approval={viewingGitCommit}
        committing={pendingAction === "turn"}
        rejecting={pendingAction === "deny"}
        onCommit={() => void approve("turn")}
        onOpenChange={(open) => !open && setViewingGitCommit(null)}
        onReject={() => void deny()}
      />
    </ComposerFloatingPanel>
  );
}

function ApprovalMenuOption({
  description,
  icon: Icon,
  label,
  loading = false,
}: {
  description: string;
  icon: LucideIcon;
  label: string;
  loading?: boolean;
}) {
  return (
    <div className="flex min-w-0 items-start gap-2 py-0.5">
      <span className="mt-0.5 grid size-4 shrink-0 place-items-center text-muted-foreground">
        {loading ? <Spinner className="size-3.5" /> : <Icon className="size-3.5" />}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-xs font-medium text-foreground">{label}</span>
        <span className="mt-0.5 block truncate text-[11px] leading-4 text-muted-foreground">{description}</span>
      </span>
    </div>
  );
}

function approvalToolActionLabel(operation: string, patchProposal: boolean, gitCommit: boolean, t: (key: string) => string) {
  if (patchProposal) {
    return t("transcript.approvalPatchApply");
  }
  if (gitCommit) {
    return t("transcript.approvalGitCommit");
  }
  switch (operation) {
    case "shell":
      return t("transcript.approvalRunCommand");
    case "process_start":
      return t("transcript.approvalStartProcess");
    default:
      return t("transcript.approvalAllowToolCall");
  }
}

function approvalDenyLabel(operation: string, patchProposal: boolean, gitCommit: boolean, t: (key: string) => string) {
  if (patchProposal) {
    return t("transcript.approvalDoNotApply");
  }
  if (gitCommit) {
    return t("transcript.approvalDoNotCommit");
  }
  if (operation === "shell" || operation === "process_start") {
    return t("transcript.approvalDoNotRun");
  }
  if (operation) {
    return t("transcript.approvalDoNotExecute");
  }
  return t("transcript.approvalDoNotAllow");
}

function selectPendingApproval(assistants: Record<string, AssistantOverlay>, sessionID: string, runningTurnID?: string): ComposerApproval | undefined {
  if (runningTurnID) {
    const running = assistants[runningTurnID];
    const approval = firstPendingApproval(running);
    if (approval) {
      return approval;
    }
  }
  for (const overlay of Object.values(assistants)) {
    if (overlay.turnID === runningTurnID || overlay.sessionID !== sessionID) {
      continue;
    }
    const approval = firstPendingApproval(overlay);
    if (approval) {
      return approval;
    }
  }
  return undefined;
}

function firstPendingApproval(overlay: AssistantOverlay | undefined): ComposerApproval | undefined {
  if (!overlay) {
    return undefined;
  }
  const approval = overlay.parts.find(isPendingApprovalPart);
  if (!approval) {
    return undefined;
  }
  if (overlay.status === "streaming") {
    return approval;
  }
  return undefined;
}

function isPendingApprovalPart(part: AssistantOverlayPart): part is ComposerApproval {
  return part.type === "approval" && !part.status;
}

function approvalTargetMode(payload: unknown) {
  if (payload && typeof payload === "object" && "targetMode" in payload && typeof payload.targetMode === "string") {
    return payload.targetMode;
  }
  return "";
}

function projectDirsFromPayload(payload: unknown) {
  if (!payload || typeof payload !== "object") {
    return [];
  }
  const data = payload as Record<string, unknown>;
  const dirs = Array.isArray(data.projectDirs) ? data.projectDirs : Array.isArray(data.rootDirs) ? data.rootDirs : [];
  return dedupeStrings(dirs.filter((value): value is string => typeof value === "string").map((value) => value.trim()).filter(Boolean));
}

function suggestedProjectDirName(payload: unknown) {
  if (payload && typeof payload === "object" && "suggestedDirName" in payload && typeof payload.suggestedDirName === "string") {
    return payload.suggestedDirName.trim();
  }
  return "";
}

function toolCallFromPayload(payload: unknown) {
  if (!payload || typeof payload !== "object") {
    return { command: "", operation: "", paths: [] as string[] };
  }
  const data = payload as Record<string, unknown>;
  const operation = typeof data.operation === "string" ? data.operation.trim() : "";
  const script = typeof data.script === "string" ? data.script : "";
  const argv = Array.isArray(data.argv) ? data.argv.filter((value): value is string => typeof value === "string") : [];
  const command = script || argv.map(formatApprovalArg).join(" ");
  const paths = Array.isArray(data.paths)
    ? data.paths.filter((value): value is string => typeof value === "string").map((value) => value.trim()).filter(Boolean)
    : [];
  return { command, operation, paths: dedupeStrings(paths) };
}

function formatApprovalArg(value: string) {
  return /^[A-Za-z0-9_./:@%+=,-]+$/.test(value) ? value : JSON.stringify(value);
}

function patchProposalFromPayload(payload: unknown): PatchProposalApproval | null {
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const data = payload as Record<string, unknown>;
  const proposalID = typeof data.proposalID === "string" ? data.proposalID.trim() : "";
  const projectRoot = typeof data.projectRoot === "string" ? data.projectRoot.trim() : "";
  const diff = typeof data.diff === "string" ? data.diff : "";
  if (!proposalID || !diff || !Array.isArray(data.files)) {
    return null;
  }
  const files = data.files.flatMap((value) => {
    if (!value || typeof value !== "object") {
      return [];
    }
    const file = value as Record<string, unknown>;
    const path = typeof file.path === "string" ? file.path.trim() : "";
    const operation: PatchProposalApproval["files"][number]["operation"] | null =
      file.operation === "create" || file.operation === "update" || file.operation === "delete" ? file.operation : null;
    if (!path || !operation) {
      return [];
    }
    return [{
      additions: typeof file.additions === "number" ? file.additions : 0,
      deletions: typeof file.deletions === "number" ? file.deletions : 0,
      operation,
      path,
    }];
  });
  if (files.length === 0) {
    return null;
  }
  return {
    additions: typeof data.additions === "number" ? data.additions : 0,
    deletions: typeof data.deletions === "number" ? data.deletions : 0,
    diff,
    fileCount: typeof data.fileCount === "number" ? data.fileCount : files.length,
    files,
    projectRoot,
    proposalID,
  };
}

function gitCommitFromPayload(payload: unknown): GitCommitApproval | null {
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const data = payload as Record<string, unknown>;
  if (data.operation !== "git_commit" || typeof data.diff !== "string" || !Array.isArray(data.files)) {
    return null;
  }
  const files = data.files.flatMap((value) => {
    if (!value || typeof value !== "object") {
      return [];
    }
    const file = value as Record<string, unknown>;
    const path = typeof file.path === "string" ? file.path.trim() : "";
    if (!path) {
      return [];
    }
    return [{
      additions: typeof file.additions === "number" ? file.additions : 0,
      deletions: typeof file.deletions === "number" ? file.deletions : 0,
      path,
    }];
  });
  if (files.length === 0) {
    return null;
  }
  return {
    additions: typeof data.additions === "number" ? data.additions : 0,
    branch: typeof data.branch === "string" ? data.branch : "",
    commitMessage: typeof data.commitMessage === "string" ? data.commitMessage : "",
    deletions: typeof data.deletions === "number" ? data.deletions : 0,
    diff: data.diff,
    fileCount: typeof data.fileCount === "number" ? data.fileCount : files.length,
    files,
    repoRoot: typeof data.repoRoot === "string" ? data.repoRoot : "",
    truncated: data.truncated === true,
  };
}

function toolCallReason(operation: string, t: (key: string) => string) {
  switch (operation) {
    case "write":
    case "patch":
    case "delete":
    case "move":
    case "copy":
    case "patch_apply":
    case "git_stage":
    case "git_unstage":
    case "git_commit":
    case "app_save":
    case "shell":
    case "process_start":
      return t(`transcript.approvalToolCall.${operation}`);
    default:
      return "";
  }
}

function dedupeStrings(values: string[]) {
  return values.filter((value, index) => values.indexOf(value) === index);
}

async function pickProjectDirectories(t: (key: string) => string) {
  const dirs = await pickDirectories({
    buttonLabel: t("transcript.approvalProjectDirChoose"),
    message: t("transcript.approvalProjectDirChooseMessage"),
    title: t("transcript.approvalProjectDirChooseTitle"),
  });
  return dedupeStrings(dirs.map((dir) => dir.trim()).filter(Boolean));
}

function approvalTitle(approval: ComposerApproval, targetMode: string, t: (key: string) => string) {
  if (approval.approvalKind === "tool_call") {
    return t("transcript.approvalToolCallTitle");
  }
  if (approval.approvalKind === "capability") {
    const mode = targetMode ? t(`mode.${targetMode}`) : "";
    if (mode) {
      return t("transcript.approvalCapabilityTitle").replace("{mode}", mode);
    }
  }
  return approval.title || t("transcript.approvalTitle");
}

function mascotMoodFromPhase(phase: TurnPhaseState | undefined, running: boolean): MascotMood {
  if (phase?.phase === "streaming_text") {
    return "ready";
  }
  if (running || phase?.phase === "submitting" || phase?.phase === "awaiting_model") {
    return "thinking";
  }
  return "idle";
}
