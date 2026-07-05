import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  ArchiveRestore,
  ArrowUp,
  Check,
  FileText,
  FolderOpen,
  Loader2,
  MessageSquarePlus,
  Mic,
  NotebookText,
  Pause,
  PenLine,
  Play,
  ShieldCheck,
  Volume2,
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

import {
  APIError,
  approveApproval,
  bindAudioInput,
  bindAudioOutput,
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
  type AudioBindings,
  type Session,
  type SkillDraft,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { ComposerAddButton } from "@/components/ComposerAddMenu";
import { buildComposerMentionReferences } from "@/components/composerMentionData";
import { ComposerMentionMenu } from "@/components/ComposerMentionMenu";
import { useComposerMentions } from "@/components/useComposerMentions";
import { ContextUsageRing } from "@/components/ContextUsageRing";
import { ImageLightbox, type ImageLightboxItem } from "@/components/ImageLightbox";
import { InputFlowPanel } from "@/components/transcript/InputFlowToolPart";
import { Mascot, type MascotGaze, type MascotGazePoint, type MascotMood } from "@/components/Mascot";
import { SkillDraftDiffDialog } from "@/components/SkillDraftDiffDialog";
import { upsertTurnIntoPages, type TurnsInfiniteData } from "@/components/transcript/useTranscriptTurns";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { type ResolvedModelSelection } from "@/lib/modelSelection";
import { reasoningEffortOptionsForSelection } from "@/components/ReasoningEffortChip";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useComposerSelectionGuard } from "@/hooks/useComposerSelectionGuard";
import { useImeCompositionGuard } from "@/hooks/useImeCompositionGuard";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { createPastedTextAttachmentFile, shouldAttachPastedText } from "@/lib/clipboardTextAttachment";
import { newClientID } from "@/lib/id";
import {
  createLocalFolderPath,
  type DroppedLocalItems,
  pickLocalFolderPaths,
  type LocalFolderPath,
} from "@/lib/localFolders";
import type { AppSearch } from "@/lib/route";
import { getSubmitFailure } from "@/lib/submitFailure";
import { getTextAreaCaretClientPoint } from "@/lib/textCaret";
import { cn } from "@/lib/utils";
import { useOverlayStore, type AssistantOverlay, type AssistantOverlayPart, type TurnPhaseState } from "@/state/overlayStore";
import { useInputFlowStore } from "@/state/inputFlowStore";
import { useSessionDraftStore, type SessionDraftAttachment } from "@/state/sessionDraftStore";

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
  const currentMode = session.modeLease === "session" ? session.activeMode : "chat";
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
  const attachments = useSessionDraftStore((state) => state.drafts[sessionID]?.attachments ?? []);
  const localFolders = useSessionDraftStore((state) => state.drafts[sessionID]?.localFolders ?? []);
  const setSessionDraftAttachments = useSessionDraftStore((state) => state.setAttachments);
  const setSessionDraftLocalFolders = useSessionDraftStore((state) => state.setLocalFolders);
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
  const draftSlashCommand = hasAttachments || hasLocalFolders ? null : parseSlashSubmitCommand(trimmedDraftText);
  const canSend = Boolean(trimmedDraftText || uploadedAttachments.length || hasLocalFolders) && !hasPendingAttachments && !hasFailedAttachments;
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
    (files: File[], options?: { origin?: "temp"; uploadSessionID?: string }) => {
      const nextFiles = files.filter((file) => file.size > 0);
      if (nextFiles.length === 0) {
        return;
      }
      const uploadSessionID = options?.uploadSessionID || sessionID;
      const items = nextFiles.map((file) => ({
        id: newClientID(),
        name: file.name,
        previewURL: file.type.toLowerCase().startsWith("image/") ? URL.createObjectURL(file) : undefined,
        size: file.size,
        status: "uploading" as const,
      }));
      setSessionDraftAttachments(sessionID, (current) => [...current, ...items]);
      items.forEach((item, index) => {
        const file = nextFiles[index];
        void uploadAttachment(token, uploadSessionID, file, options?.origin ? { origin: options.origin } : undefined)
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
          });
      });
    },
    [sessionID, setSessionDraftAttachments, t, token],
  );
  const removeAttachment = useCallback((id: string) => {
    setSessionDraftAttachments(sessionID, (current) => {
      const removed = current.find((item) => item.id === id);
      if (removed) {
        revokeAttachmentPreview(removed);
      }
      return current.filter((item) => item.id !== id);
    });
  }, [sessionID, setSessionDraftAttachments]);
  const addUploadedAttachments = useCallback((values: Attachment[]) => {
    if (values.length === 0) {
      return;
    }
    setSessionDraftAttachments(sessionID, (current) => [
      ...current,
      ...values.map((attachment) => ({
        id: newClientID(),
        attachment,
        name: attachment.name,
        size: attachment.size,
        status: "uploaded" as const,
      })),
    ]);
  }, [sessionID, setSessionDraftAttachments]);
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
    setSessionDraftLocalFolders(sessionID, (current) => {
      const existing = new Set(current.map((folder) => folder.path));
      return [...current, ...folders.filter((folder) => !existing.has(folder.path))];
    });
  }, [sessionID, setSessionDraftLocalFolders]);
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
  }, [sessionID, setSessionDraftLocalFolders]);
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
    mutationFn: async (value: z.infer<typeof composerSchema> & { attachments: Attachment[]; localFolders: LocalFolderPath[] }) => {
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
        attachments: value.attachments,
        localFolders: value.localFolders,
        createdAt: new Date().toISOString(),
      });
      if (session.provider !== provider || session.model !== model) {
        await updateSession(token, sessionID, { provider, model });
      }
      const result = await submitMessage(token, sessionID, {
        clientMessageID,
        text: value.text,
        attachments: value.attachments,
        localFolders: value.localFolders,
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
    const attachmentsToSubmit = uploadedAttachments;
    const localFoldersToSubmit = localFolders;
    if (
      (!text && attachmentsToSubmit.length === 0 && localFoldersToSubmit.length === 0) ||
      submitMutation.isPending ||
      compactMutation.isPending ||
      systemSubmitMutation.isPending ||
      renameMutation.isPending ||
      hasPendingAttachments ||
      hasFailedAttachments
    ) {
      return;
    }
    const slashCommand = attachmentsToSubmit.length === 0 && localFoldersToSubmit.length === 0 ? parseSlashSubmitCommand(text) : null;
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
    submitMutation.mutate({ text, attachments: attachmentsToSubmit, localFolders: localFoldersToSubmit });
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
      addFiles(droppedFiles.files);
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
        className={cn("relative shrink-0 pb-4", pendingApproval ? "pt-36" : "pt-2")}
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
          {pendingInputFlow ? <InputFlowPanel key={pendingInputFlow.id} request={pendingInputFlow} /> : null}
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
          <div className="relative z-10 rounded-3xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/60 focus-within:ring-2 focus-within:ring-ring/25">
            <input
              ref={fileInputRef}
              className="sr-only"
              accept="image/*,audio/*,text/*,application/pdf,.txt,.md,.csv,.json,.xml,.yaml,.yml"
              multiple
              type="file"
              onChange={handleAttachmentInputChange}
            />
            {attachments.length > 0 || localFolders.length > 0 ? (
              <div className="flex flex-wrap gap-2 px-3 pt-3">
                {attachments.map((item) => (
                  <ComposerAttachmentChip
                    key={item.id}
                    item={item}
                    previewIndex={attachmentPreviewIndexByID.get(item.id)}
                    removeLabel={t("composer.removeAttachment")}
                    token={token}
                    onPreview={setAttachmentPreviewIndex}
                    onRevealSource={revealLocalPath}
                    onRemove={() => removeAttachment(item.id)}
                  />
                ))}
                {localFolders.map((folder) => (
                  <LocalFolderChip
                    key={folder.id}
                    folder={folder}
                    label={t("composer.folderLabel")}
                    removeLabel={t("composer.removeFolder")}
                    onReveal={() => revealLocalPath(folder.path)}
                    onRemove={() => removeLocalFolder(folder.id)}
                  />
                ))}
              </div>
            ) : null}
            <div className="px-4 pt-4 pb-2">
              <Textarea
                className="block max-h-36 min-h-6 resize-none overflow-y-auto rounded-none border-0 bg-transparent p-0 text-base leading-6 shadow-none focus-visible:ring-0 md:text-sm dark:bg-transparent"
                placeholder={t("composer.messagePlaceholder")}
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
              {currentMode === "workspace" ? <WorkspaceDirsControl session={session} token={token} /> : null}
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
                <ContextUsageRing token={token} sessionID={sessionID} />
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
              <SessionAudioControls token={token} session={session} />
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
                        <Loader2 className="animate-spin" />
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
                        <Loader2 className="animate-spin" />
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

function SessionAudioControls({ token, session }: { token: string; session: Session }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const bindingsQuery = useQuery({
    queryKey: queryKeys.audioBindings(),
    queryFn: () => getAudioBindings(token, session.id),
    enabled: Boolean(token && session.id),
    refetchInterval: 2000,
  });
  const bindings = bindingsQuery.data?.bindings;
  const inputActive = bindings?.inputOwner === session.id;
  const outputActive = bindings?.outputOwner === session.id;
  const invalidateAudioBindings = () => queryClient.invalidateQueries({ queryKey: queryKeys.audioBindings() });
  const setBindings = (next: AudioBindings) => {
    queryClient.setQueryData(queryKeys.audioBindings(), { bindings: next });
    void invalidateAudioBindings();
  };
  const inputMutation = useMutation({
    mutationFn: (enabled: boolean) => bindAudioInput(token, session.id, enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.audioBindings() });
      const previous = queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings());
      const current = previous?.bindings ?? { inputOwner: "", outputOwner: "" };
      queryClient.setQueryData(queryKeys.audioBindings(), {
        bindings: {
          ...current,
          inputOwner: enabled ? session.id : current.inputOwner === session.id ? "" : current.inputOwner,
        },
      });
      return { previous };
    },
    onSuccess: (result) => setBindings(result.bindings),
    onError: (_error, _enabled, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.audioBindings(), context.previous);
      }
      toast.error(t("voice.inputFailed"));
    },
  });
  const outputMutation = useMutation({
    mutationFn: (enabled: boolean) => bindAudioOutput(token, session.id, enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.audioBindings() });
      const previous = queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings());
      const current = previous?.bindings ?? { inputOwner: "", outputOwner: "" };
      queryClient.setQueryData(queryKeys.audioBindings(), {
        bindings: {
          ...current,
          outputOwner: enabled ? session.id : current.outputOwner === session.id ? "" : current.outputOwner,
        },
      });
      return { previous };
    },
    onSuccess: (result) => setBindings(result.bindings),
    onError: (_error, _enabled, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.audioBindings(), context.previous);
      }
      toast.error(t("voice.outputFailed"));
    },
  });
  const inputLabel = inputActive ? t("voice.inputOn") : t("voice.inputOff");
  const outputLabel = outputActive ? t("voice.outputOn") : t("voice.outputOff");

  return (
    <div className="flex shrink-0 items-center gap-1" aria-label={t("voice.controls")}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-disabled={inputMutation.isPending}
            aria-label={inputLabel}
            aria-pressed={inputActive}
            className="rounded-full"
            size="icon"
            type="button"
            variant={inputActive ? "default" : "ghost"}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              if (inputMutation.isPending) {
                return;
              }
              inputMutation.mutate(!inputActive);
            }}
          >
            {inputMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <Mic className="size-4" />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{inputLabel}</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-disabled={outputMutation.isPending}
            aria-label={outputLabel}
            aria-pressed={outputActive}
            className="rounded-full"
            size="icon"
            type="button"
            variant={outputActive ? "default" : "ghost"}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              if (outputMutation.isPending) {
                return;
              }
              outputMutation.mutate(!outputActive);
            }}
          >
            {outputMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <Volume2 className="size-4" />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{outputLabel}</TooltipContent>
      </Tooltip>
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
            <Loader2 className="size-4 animate-spin text-foreground" />
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
          <Loader2 className="size-4 animate-spin" strokeWidth={1.8} />
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
      className="absolute bottom-full left-16 z-20 max-h-64 w-[min(30rem,calc(100%-6rem))] overflow-y-auto rounded-t-lg border border-border/70 bg-popover/95 p-1 text-sm text-popover-foreground shadow-sm backdrop-blur"
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

function WorkspaceDirsControl({ session, token }: { session: Session; token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [picking, setPicking] = useState(false);
  const [savingDir, setSavingDir] = useState("");
  const dirs = session.workspaceDirs || [];
  const title = dirs.length > 0
    ? t("composer.workspaceDirsCount").replace("{count}", String(dirs.length))
    : t("composer.workspaceDirsEmpty");

  async function saveWorkspaceDirs(nextDirs: string[]) {
    const normalized = dedupeStrings(nextDirs.map((dir) => dir.trim()).filter(Boolean));
    setSavingDir("__all__");
    try {
      const updated = await updateSession(token, session.id, { workspaceDirs: normalized });
      updateSessionInCache(queryClient, updated);
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    } catch {
      toast.error(t("composer.workspaceDirsSaveFailed"));
    } finally {
      setSavingDir("");
    }
  }

  async function addWorkspaceDirs() {
    if (picking || savingDir) {
      return;
    }
    setPicking(true);
    try {
      const picked = await pickWorkspaceDirectories(t);
      if (picked.length > 0) {
        await saveWorkspaceDirs([...dirs, ...picked]);
      }
    } catch {
      toast.error(t("transcript.approvalWorkspaceDirPickFailed"));
    } finally {
      setPicking(false);
    }
  }

  async function removeWorkspaceDir(dir: string) {
    if (savingDir) {
      return;
    }
    setSavingDir(dir);
    try {
      const updated = await updateSession(token, session.id, { workspaceDirs: dirs.filter((item) => item !== dir) });
      updateSessionInCache(queryClient, updated);
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    } catch {
      toast.error(t("composer.workspaceDirsSaveFailed"));
    } finally {
      setSavingDir("");
    }
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          aria-label={title}
          className={cn(
            "size-6 rounded-full border-0 bg-transparent text-muted-foreground hover:text-foreground",
            dirs.length === 0 && "text-warning hover:text-warning",
          )}
          size="icon-xs"
          title={title}
          type="button"
          variant="ghost"
        >
          <FolderOpen className="size-3.5" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="grid w-[min(24rem,calc(100vw-2rem))] gap-2 p-2.5" side="top" sideOffset={8}>
        <div className="grid gap-0.5">
          <div className="text-sm font-medium">{t("composer.workspaceDirsTitle")}</div>
          <div className="text-xs leading-5 text-muted-foreground">{t("composer.workspaceDirsDesc")}</div>
        </div>
        {dirs.length > 0 ? (
          <div className="grid max-h-40 gap-1 overflow-y-auto rounded-md border bg-background/70 p-1.5">
            {dirs.map((dir) => (
              <div key={dir} className="flex min-w-0 items-center gap-1 rounded px-1.5 py-1 text-xs" title={dir}>
                <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground">{formatWorkspaceDirLabel(dir)}</span>
                <Button
                  aria-label={t("common.delete")}
                  className="size-6 shrink-0 text-muted-foreground hover:text-destructive"
                  disabled={Boolean(savingDir)}
                  size="icon-xs"
                  type="button"
                  variant="ghost"
                  onClick={() => void removeWorkspaceDir(dir)}
                >
                  {savingDir === dir ? <Loader2 className="size-3 animate-spin" /> : <X className="size-3" />}
                </Button>
              </div>
            ))}
          </div>
        ) : (
          <div className="rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">{t("composer.workspaceDirsNone")}</div>
        )}
        <Button
          className="h-8 gap-1.5 rounded-full px-3 text-xs"
          disabled={picking || Boolean(savingDir)}
          size="sm"
          type="button"
          variant="secondary"
          onClick={() => void addWorkspaceDirs()}
        >
          {picking || savingDir === "__all__" ? <Loader2 className="size-3 animate-spin" /> : <FolderOpen className="size-3.5" />}
          {t("composer.workspaceDirsAdd")}
        </Button>
      </PopoverContent>
    </Popover>
  );
}

function updateSessionInCache(queryClient: ReturnType<typeof useQueryClient>, updated: Session) {
  queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
    if (!previous) {
      return previous;
    }
    return {
      sessions: previous.sessions.map((item) => (item.id === updated.id ? updated : item)),
    };
  });
}

function ComposerApprovalBar({ approval, token }: { approval?: ComposerApproval; token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [pendingAction, setPendingAction] = useState<"turn" | "session" | "deny" | null>(null);
  const [selectedWorkspaceDirs, setSelectedWorkspaceDirs] = useState<string[]>([]);
  const [pickingWorkspaceDir, setPickingWorkspaceDir] = useState(false);
  const [viewingSkillDraft, setViewingSkillDraft] = useState<SkillDraft | null>(null);
  useEffect(() => {
    setSelectedWorkspaceDirs([]);
    setViewingSkillDraft(null);
  }, [approval?.approvalID]);
  if (!approval) {
    return null;
  }
  const current = approval;
  const isSkillDraftApproval = current.approvalKind === "skill_draft";
  const targetMode = approvalTargetMode(current.payload);
  const title = approvalTitle(current, targetMode, t);
  const pending = pendingAction !== null;
  const isWorkspaceApproval = targetMode === "workspace";
  const payloadWorkspaceDirs = workspaceDirsFromPayload(current.payload);
  const hasPayloadWorkspaceDirs = payloadWorkspaceDirs.length > 0;
  const workspaceDirs = hasPayloadWorkspaceDirs ? payloadWorkspaceDirs : selectedWorkspaceDirs;
  const needsWorkspaceDir = needsWorkspaceDirFromPayload(current.payload);
  const workspaceDirsRequired = isWorkspaceApproval && needsWorkspaceDir && workspaceDirs.length === 0;
  const suggestedDirName = suggestedWorkspaceDirName(current.payload);
  const skillDraftApproval = skillDraftFromPayload(current.payload);
  const skillDraft = skillDraftApproval?.draft || null;

  async function approve(scope: "turn" | "session") {
    if (pending) {
      return;
    }
    setPendingAction(scope);
    try {
      await approveApproval(token, current.sessionID, current.approvalID, scope, isWorkspaceApproval ? workspaceDirs : []);
      if (scope === "session") {
        await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      }
      if (isSkillDraftApproval) {
        setViewingSkillDraft(null);
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: queryKeys.skills() }),
          queryClient.invalidateQueries({ queryKey: queryKeys.skillDrafts() }),
        ]);
      }
    } finally {
      setPendingAction(null);
    }
  }

  async function pickWorkspaceDirs() {
    if (pending || pickingWorkspaceDir || hasPayloadWorkspaceDirs) {
      return;
    }
    setPickingWorkspaceDir(true);
    try {
      const dirs = await pickWorkspaceDirectories(t);
      if (dirs.length > 0) {
        setSelectedWorkspaceDirs((prev) => dedupeStrings([...prev, ...dirs]));
      }
    } catch {
      toast.error(t("transcript.approvalWorkspaceDirPickFailed"));
    } finally {
      setPickingWorkspaceDir(false);
    }
  }

  function removeWorkspaceDir(dir: string) {
    setSelectedWorkspaceDirs((prev) => prev.filter((item) => item !== dir));
  }

  async function deny() {
    if (pending) {
      return;
    }
    setPendingAction("deny");
    try {
      await denyApproval(token, current.sessionID, current.approvalID);
      setViewingSkillDraft(null);
    } finally {
      setPendingAction(null);
    }
  }

  return (
    <div className="absolute right-8 bottom-full left-16 z-0 grid gap-1 rounded-t-lg border border-border/70 bg-popover/95 px-3 py-2 text-xs text-popover-foreground shadow-sm backdrop-blur">
      <div className="flex min-w-0 items-center gap-1.5">
        <ShieldCheck className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 truncate font-medium">{title}</span>
      </div>
      {current.reason ? <div className="line-clamp-2 leading-5 text-muted-foreground">{current.reason}</div> : null}
      {isSkillDraftApproval && skillDraft ? (
        <div className="grid gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1.5 text-[11px] leading-4 text-muted-foreground">
          <div className="flex min-w-0 items-center gap-1">
            <span className="shrink-0 font-medium text-foreground">{skillDraft.id}</span>
            {skillDraft.path ? <span className="min-w-0 truncate font-mono">{skillDraft.path}</span> : null}
          </div>
          {typeof skillDraftApproval?.fileCount === "number" ? (
            <div>{t("transcript.approvalSkillDraftFiles").replace("{count}", String(skillDraftApproval.fileCount))}</div>
          ) : null}
        </div>
      ) : null}
      {!isSkillDraftApproval && isWorkspaceApproval ? (
        <div className="grid gap-1">
          <div className="text-[11px] font-medium text-muted-foreground">
            {t("transcript.approvalWorkspaceDirs")}
          </div>
          {workspaceDirs.length > 0 ? (
            <div className="grid gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
              {workspaceDirs.map((dir) => (
                <div key={dir} className="flex min-w-0 items-center gap-1" title={dir}>
                  <span className="min-w-0 flex-1 truncate">{dir}</span>
                  {!hasPayloadWorkspaceDirs ? (
                    <button
                      aria-label={t("common.delete")}
                      className="grid size-4 shrink-0 place-items-center rounded-full text-muted-foreground hover:text-foreground"
                      type="button"
                      onClick={() => removeWorkspaceDir(dir)}
                    >
                      <X className="size-3" />
                    </button>
                  ) : null}
                </div>
              ))}
            </div>
          ) : null}
          {workspaceDirsRequired ? (
            <div className="text-[11px] leading-4 text-warning">{t("transcript.approvalWorkspaceDirsRequired")}</div>
          ) : null}
          {!hasPayloadWorkspaceDirs ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <Button
                className="h-6 gap-1 rounded-full px-2 text-[11px]"
                disabled={pending || pickingWorkspaceDir}
                size="sm"
                type="button"
                variant="secondary"
                onClick={() => void pickWorkspaceDirs()}
              >
                {pickingWorkspaceDir ? <Loader2 className="size-3 animate-spin" /> : <FolderOpen className="size-3" />}
                {t("transcript.approvalWorkspaceDirChoose")}
              </Button>
              {suggestedDirName ? <span className="text-[11px] text-muted-foreground">{t("transcript.approvalWorkspaceDirsSuggested").replace("{name}", suggestedDirName)}</span> : null}
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-1.5">
        {isSkillDraftApproval ? (
          <>
            <Button
              className="h-6 gap-1 rounded-full px-2 text-[11px]"
              disabled={pending || !skillDraft}
              size="sm"
              type="button"
              variant="secondary"
              onClick={() => skillDraft && setViewingSkillDraft(skillDraft)}
            >
              {t("settings.skills.viewDiff")}
            </Button>
            <Button
              className="h-6 gap-1 rounded-full px-2 text-[11px]"
              disabled={pending}
              size="sm"
              type="button"
              onClick={() => void approve("turn")}
            >
              {pendingAction === "turn" ? <Loader2 className="size-3 animate-spin" /> : <Check className="size-3" />}
              {t("transcript.approvalPublishSkillDraft")}
            </Button>
          </>
        ) : (
          <>
            <Button
              className="h-6 gap-1 rounded-full px-2 text-[11px]"
              disabled={pending || workspaceDirsRequired}
              size="sm"
              type="button"
              onClick={() => void approve("turn")}
            >
              {pendingAction === "turn" ? <Loader2 className="size-3 animate-spin" /> : <Check className="size-3" />}
              {t("transcript.approvalAllowTurn")}
            </Button>
            <Button
              className="h-6 gap-1 rounded-full px-2 text-[11px]"
              disabled={pending || workspaceDirsRequired}
              size="sm"
              type="button"
              variant="secondary"
              onClick={() => void approve("session")}
            >
              {pendingAction === "session" ? <Loader2 className="size-3 animate-spin" /> : <ShieldCheck className="size-3" />}
              {t("transcript.approvalAllowSession")}
            </Button>
          </>
        )}
        <Button
          className="h-6 gap-1 rounded-full px-1.5 text-[11px]"
          disabled={pending}
          size="sm"
          type="button"
          variant="ghost"
          onClick={() => void deny()}
        >
          {pendingAction === "deny" ? <Loader2 className="size-3 animate-spin" /> : <X className="size-3" />}
          {t("transcript.approvalDeny")}
        </Button>
      </div>
      <SkillDraftDiffDialog
        applying={pendingAction === "turn"}
        draft={viewingSkillDraft}
        rejecting={pendingAction === "deny"}
        token={token}
        onApply={() => void approve("turn")}
        onOpenChange={(open) => !open && setViewingSkillDraft(null)}
        onReject={() => void deny()}
      />
    </div>
  );
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
  if (overlay.status === "streaming" || approval.approvalKind === "skill_draft") {
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

function workspaceDirsFromPayload(payload: unknown) {
  if (!payload || typeof payload !== "object" || !("workspaceDirs" in payload) || !Array.isArray(payload.workspaceDirs)) {
    return [];
  }
  return dedupeStrings(payload.workspaceDirs.filter((value): value is string => typeof value === "string").map((value) => value.trim()).filter(Boolean));
}

function needsWorkspaceDirFromPayload(payload: unknown) {
  return Boolean(payload && typeof payload === "object" && "needsWorkspaceDir" in payload && payload.needsWorkspaceDir === true);
}

function suggestedWorkspaceDirName(payload: unknown) {
  if (payload && typeof payload === "object" && "suggestedDirName" in payload && typeof payload.suggestedDirName === "string") {
    return payload.suggestedDirName.trim();
  }
  return "";
}

function skillDraftFromPayload(payload: unknown) {
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const data = payload as Record<string, unknown>;
  const draft = data.draft && typeof data.draft === "object" ? (data.draft as Record<string, unknown>) : {};
  const id = typeof data.draft_id === "string" ? data.draft_id.trim() : typeof draft.id === "string" ? draft.id.trim() : "";
  const path = typeof draft.path === "string" ? draft.path.trim() : "";
  const description = typeof draft.description === "string" ? draft.description : "";
  const change: SkillDraft["change"] = draft.change === "modified" ? "modified" : "added";
  const validation = draft.validation && typeof draft.validation === "object" ? (draft.validation as SkillDraft["validation"]) : { ok: true };
  const fileCount = typeof data.fileCount === "number" ? data.fileCount : undefined;
  if (!id) {
    return null;
  }
  return {
    draft: {
      change,
      description,
      id,
      name: typeof draft.name === "string" ? draft.name : id,
      path,
      validation,
    },
    fileCount,
  };
}

function dedupeStrings(values: string[]) {
  return values.filter((value, index) => values.indexOf(value) === index);
}

function formatWorkspaceDirLabel(dir: string) {
  const normalized = dir.trim().replace(/\\/g, "/");
  const parts = normalized.split("/").filter(Boolean);
  if (parts.length <= 3) {
    return dir;
  }
  return `.../${parts.slice(-3).join("/")}`;
}

async function pickWorkspaceDirectories(t: (key: string) => string) {
  const { Dialogs } = await import("@wailsio/runtime");
  const result = await Dialogs.OpenFile({
    AllowsMultipleSelection: true,
    ButtonText: t("transcript.approvalWorkspaceDirChoose"),
    CanChooseDirectories: true,
    CanChooseFiles: false,
    CanCreateDirectories: true,
    Message: t("transcript.approvalWorkspaceDirChooseMessage"),
    Title: t("transcript.approvalWorkspaceDirChooseTitle"),
  });
  const dirs = Array.isArray(result) ? result : result ? [result] : [];
  return dedupeStrings(dirs.map((dir) => dir.trim()).filter(Boolean));
}

function approvalTitle(approval: ComposerApproval, targetMode: string, t: (key: string) => string) {
  if (approval.approvalKind === "skill_draft") {
    return t("transcript.approvalSkillDraftTitle");
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
