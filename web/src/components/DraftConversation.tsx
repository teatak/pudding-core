import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowUp, CircleAlert, FolderOpen, Loader2, Paperclip, Upload, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type ChangeEvent,
  type DragEvent,
  type FocusEvent,
  type KeyboardEvent,
} from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";

import {
  APIError,
  createSession,
  deleteSession,
  listProviders,
  submitMessage,
  updateSession,
  uploadAttachment,
  type Attachment,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { ComposerAddMenu } from "@/components/ComposerAddMenu";
import { ImageLightbox, type ImageLightboxItem } from "@/components/ImageLightbox";
import { Mascot, type MascotGaze, type MascotGazePoint } from "@/components/Mascot";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { type ResolvedModelSelection } from "@/components/ModelPicker";
import { ProviderProfileEditorDialog } from "@/components/ProviderProfileEditorDialog";
import { ProviderCustomCard, ProviderPresetCreateDialog, ProviderPresetGrid } from "@/components/ProviderPresetCreateDialog";
import { reasoningEffortOptionsForSelection } from "@/components/ReasoningEffortChip";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useImeCompositionGuard } from "@/hooks/useImeCompositionGuard";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { bindDesktopFileDrop, nativeFileDropLikelyAvailable } from "@/lib/desktopFileDrop";
import { newClientID } from "@/lib/id";
import {
  appendLocalFolderPaths,
  createLocalFolderPath,
  droppedLocalItemsFromDataTransfer,
  type DroppedLocalItems,
  formatLocalFolderLabel,
  pickLocalFolderPaths,
  type LocalFolderPath,
} from "@/lib/localFolders";
import type { AppSearch } from "@/lib/route";
import { getSubmitFailure } from "@/lib/submitFailure";
import { getTextAreaCaretClientPoint } from "@/lib/textCaret";
import { cn } from "@/lib/utils";
import { getOrderedProviderPresets, type ProviderPreset } from "@/provider/presets";
import { useDraftStore, type DraftModelValue } from "@/state/draftStore";
import { useOverlayStore } from "@/state/overlayStore";

const draftSchema = z.object({
  text: z.string(),
});

type DraftValue = z.infer<typeof draftSchema>;
type DraftDroppedFilesBatch = DroppedLocalItems & {
  attachments?: Attachment[];
  failedFileCount?: number;
  nonce: number;
};
type DraftAttachment = {
  id: string;
  file?: File;
  name: string;
  previewURL?: string;
  size: number;
  status: "uploading" | "uploaded" | "error";
  attachment?: Attachment;
};
type QuickSubmit = { id: number; text: string };

const suggestionKeys = ["draft.suggest.1", "draft.suggest.2", "draft.suggest.3"] as const;
const draftAttachmentSessionID = "draft";
const emptyDraftModel: DraftModelValue = {};

export function DraftConversation({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const [quickSubmit, setQuickSubmit] = useState<QuickSubmit | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [dragActive, setDragActive] = useState(false);
  const [droppedFiles, setDroppedFiles] = useState<DraftDroppedFilesBatch | null>(null);
  const modelValue = useDraftStore((state) => state.model);
  const setModelValue = useDraftStore((state) => state.setModel);
  const droppedFilesNonceRef = useRef(0);
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const profiles = providersQuery.data?.providers || [];
  const hasConfiguredModel = profiles.some((profile) => profile.models.some((model) => model.id));
  const draftModelIsValid = providersQuery.isSuccess && isDraftModelAvailable(profiles, modelValue);
  const composerModelValue = draftModelIsValid ? modelValue : emptyDraftModel;
  const showPresetSetup = providersQuery.isSuccess && !hasConfiguredModel;
  const [mascotGaze, setMascotGaze] = useState<MascotGaze>({ type: "pointer" });
  const setMascotPointerGaze = useCallback(() => {
    setMascotGaze((current) => (current.type === "pointer" ? current : { type: "pointer" }));
  }, []);
  const setMascotInputGaze = useCallback((target: MascotGazePoint | null) => {
    setMascotGaze(target ? { type: "input", target } : { type: "pointer" });
  }, []);
  const resetDragState = useCallback(() => {
    setDragActive(false);
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

  useEffect(
    () =>
      bindDesktopFileDrop({ kind: "draft" }, (drop) => {
        droppedFilesNonceRef.current += 1;
        setDroppedFiles({
          attachments: drop.attachments,
          failedFileCount: drop.failedFileCount,
          files: [],
          folderPathUnavailable: false,
          folderPaths: drop.directories,
          nonce: droppedFilesNonceRef.current,
        });
      }),
    [],
  );

  const handleDragEnter = useCallback((event: DragEvent<HTMLDivElement>) => {
    if (!dataTransferHasFiles(event.dataTransfer)) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    setDragActive(true);
  }, []);

  const handleDragOver = useCallback((event: DragEvent<HTMLDivElement>) => {
    if (!dataTransferHasFiles(event.dataTransfer)) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "copy";
    setDragActive(true);
  }, []);

  const handleDragLeave = useCallback(
    (event: DragEvent<HTMLDivElement>) => {
      if (!dataTransferHasFiles(event.dataTransfer)) {
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
      if (!dataTransferHasFiles(event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      resetDragState();
      const dropped = droppedLocalItemsFromDataTransfer(event.dataTransfer);
      if (nativeFileDropLikelyAvailable()) {
        dropped.files = [];
        dropped.folderPaths = [];
        dropped.folderPathUnavailable = false;
      }
      if (dropped.files.length > 0 || dropped.folderPaths.length > 0 || dropped.folderPathUnavailable) {
        droppedFilesNonceRef.current += 1;
        setDroppedFiles({ ...dropped, nonce: droppedFilesNonceRef.current });
      }
    },
    [resetDragState],
  );

  useEffect(() => {
    if (!providersQuery.isSuccess || (!modelValue.provider && !modelValue.model) || draftModelIsValid) {
      return;
    }
    setModelValue(emptyDraftModel);
  }, [draftModelIsValid, modelValue.model, modelValue.provider, providersQuery.isSuccess, setModelValue]);

  if (providersQuery.isPending) {
    return <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden" />;
  }

  return (
    <div
      className="pudding-draft-stage relative flex min-h-0 flex-1 flex-col overflow-hidden [&.file-drop-target-active_.pudding-drop-overlay]:opacity-100"
      data-file-drop-target=""
      data-pudding-drop-target="draft"
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      <div className="pudding-draft-body">
        <div className="pudding-draft-title">
          <Mascot
            className="pudding-draft-mascot"
            gaze={mascotGaze}
            mood="idle"
            onPointerGaze={setMascotPointerGaze}
          />
          <h1 className="pudding-draft-heading">{t("draft.title")}</h1>
        </div>
        <div className={cn("pudding-draft-stack", showPresetSetup && "pudding-draft-stack-with-setup")}>
          {submitError ? (
            <ChatColumn className="mb-3">
              <Alert variant="destructive">
                <CircleAlert className="h-3.5 w-3.5" />
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            </ChatColumn>
          ) : null}
          {showPresetSetup ? (
            <DraftPresetSetup
              className="mb-12"
              profiles={profiles}
              presets={getOrderedProviderPresets(locale)}
              token={token}
              onCreated={(profile, model) => {
                setModelValue({ provider: profile.id, model });
                setSubmitError(null);
              }}
            />
          ) : null}
          <DraftComposer
            droppedFiles={droppedFiles}
            quickSubmit={quickSubmit}
            token={token}
            modelReady={draftModelIsValid}
            modelValue={composerModelValue}
            onModelValueChange={setModelValue}
            onMascotInputGazeChange={setMascotInputGaze}
            onSubmitError={setSubmitError}
          />
          {!showPresetSetup ? (
            <ChatColumn className="mt-8 flex flex-wrap items-center justify-center gap-2 px-2">
              {suggestionKeys.map((key, index) => {
                const text = t(key);
                return (
                  <button
                    key={key}
                    className="rounded-full border border-input bg-background px-4 py-1.5 text-[13px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    type="button"
                    onClick={() => setQuickSubmit({ id: Date.now() + index, text })}
                  >
                    {text}
                  </button>
                );
              })}
            </ChatColumn>
          ) : null}
        </div>
      </div>
      <DraftDropOverlay active={dragActive} />
    </div>
  );
}

function isRejectedSubmit(error: unknown) {
  return error instanceof APIError;
}

function DraftPresetSetup({
  className,
  profiles,
  presets,
  token,
  onCreated,
}: {
  className?: string;
  profiles: ProviderProfile[];
  presets: ProviderPreset[];
  token: string;
  onCreated: (profile: ProviderProfile, model: string) => void;
}) {
  const [selected, setSelected] = useState<ProviderPreset | null>(null);
  const [customOpen, setCustomOpen] = useState(false);

  return (
    <div className={cn("pudding-draft-preset-panel", className)}>
      <ProviderPresetGrid className="pudding-draft-preset-grid" presets={presets} onSelect={setSelected}>
        <ProviderCustomCard onSelect={() => setCustomOpen(true)} />
      </ProviderPresetGrid>
      <ProviderPresetCreateDialog
        open={Boolean(selected)}
        preset={selected}
        profiles={profiles}
        token={token}
        onCreated={(profile, variant) => {
          const model = profile.models[0]?.id || variant.models[0]?.id || "";
          if (model) {
            onCreated(profile, model);
          }
        }}
        onOpenChange={(open) => {
          if (!open) {
            setSelected(null);
          }
        }}
      />
      <ProviderProfileEditorDialog
        open={customOpen}
        profiles={profiles}
        token={token}
        onOpenChange={setCustomOpen}
        onSaved={(profile) => {
          const model = profile.models[0]?.id || "";
          if (model) {
            onCreated(profile, model);
          }
        }}
      />
    </div>
  );
}

function DraftComposer({
  droppedFiles,
  token,
  quickSubmit,
  modelReady,
  modelValue,
  onModelValueChange,
  onMascotInputGazeChange,
  onSubmitError,
}: {
  droppedFiles?: DraftDroppedFilesBatch | null;
  token: string;
  quickSubmit: QuickSubmit | null;
  modelReady: boolean;
  modelValue: DraftModelValue;
  onModelValueChange: (model: DraftModelValue) => void;
  onMascotInputGazeChange: (target: MascotGazePoint | null) => void;
  onSubmitError: (message: string | null) => void;
}) {
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const addPendingUser = useOverlayStore((state) => state.addPendingUser);
  const acceptSubmittingTurn = useOverlayStore((state) => state.acceptSubmittingTurn);
  const clearSubmittingTurn = useOverlayStore((state) => state.clearSubmittingTurn);
  const removePendingUser = useOverlayStore((state) => state.removePendingUser);
  const startSubmittingTurn = useOverlayStore((state) => state.startSubmittingTurn);
  const draftText = useDraftStore((state) => state.text);
  const setDraftText = useDraftStore((state) => state.setText);
  const clearDraft = useDraftStore((state) => state.clear);
  const [resolvedModel, setResolvedModel] = useState<ResolvedModelSelection | null>(null);
  const [draftReasoningEffort, setDraftReasoningEffortValue] = useState("");
  const [draftReasoningModelKey, setDraftReasoningModelKey] = useState("");
  const [attachments, setAttachments] = useState<DraftAttachment[]>([]);
  const [attachmentPreviewIndex, setAttachmentPreviewIndex] = useState<number | null>(null);
  const [localFolders, setLocalFolders] = useState<LocalFolderPath[]>([]);
  const [pickingLocalFolder, setPickingLocalFolder] = useState(false);
  const draftIDRef = useRef<string>(newClientID());
  const attachmentsRef = useRef<DraftAttachment[]>([]);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const lastDroppedFilesNonceRef = useRef(0);
  const quickSubmitIDRef = useRef<number | null>(null);
  const textAreaRef = useRef<HTMLTextAreaElement | null>(null);
  const mascotGazeRafRef = useRef(0);
  const form = useForm<DraftValue>({
    resolver: zodResolver(draftSchema),
    defaultValues: { text: draftText },
  });
  const watchedText = form.watch("text");
  const uploadedAttachments = attachments.flatMap((item) => (item.status === "uploaded" && item.attachment ? [item.attachment] : []));
  const hasPendingAttachments = attachments.some((item) => item.status === "uploading");
  const canSend = Boolean(watchedText.trim() || uploadedAttachments.length || localFolders.length) && !hasPendingAttachments;
  const sendEnabled = canSend && modelReady;
  const textField = form.register("text");
  const attachmentPreviewItems = useMemo(
    () =>
      attachments.flatMap((item): ImageLightboxItem[] => {
        const url = draftAttachmentImageSource(item, token);
        if (!url || !isImageAttachmentLike(item.attachment?.mime || item.file?.type, item.name)) {
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
  const reasoningOptions = useMemo(() => reasoningEffortOptionsForSelection(resolvedModel), [resolvedModel]);
  const resolvedModelKey = resolvedModel ? `${resolvedModel.provider}:${resolvedModel.model}` : "";
  const reasoningEffort = resolvedModelKey && draftReasoningModelKey === resolvedModelKey ? draftReasoningEffort : "";
  const setDraftReasoningEffort = useCallback(
    (value: string) => {
      if (!resolvedModelKey) {
        return;
      }
      setDraftReasoningModelKey(resolvedModelKey);
      setDraftReasoningEffortValue(value);
    },
    [resolvedModelKey],
  );

  const updateMascotInputGaze = useCallback(() => {
    const textArea = textAreaRef.current;
    if (!textArea || document.activeElement !== textArea) {
      return;
    }
    onMascotInputGazeChange(mascotGazePointFromCaret(textArea));
  }, [onMascotInputGazeChange]);

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
  const ime = useImeCompositionGuard({ onCompositionEnd: scheduleMascotInputGaze });

  useEffect(() => {
    attachmentsRef.current = attachments;
  }, [attachments]);

  const clearDraftAttachments = useCallback(() => {
    setAttachments((current) => {
      current.forEach(revokeDraftAttachmentPreview);
      return [];
    });
    setLocalFolders([]);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }, []);

  const addFiles = useCallback(
    (files: File[]) => {
      const nextFiles = files.filter((file) => file.size > 0);
      if (nextFiles.length === 0) {
        return;
      }
      const items = nextFiles.map((file) => ({
        id: newClientID(),
        file,
        name: file.name,
        previewURL: isImageAttachmentLike(file.type, file.name) ? URL.createObjectURL(file) : undefined,
        size: file.size,
        status: "uploading" as const,
      }));
      setAttachments((current) => [...current, ...items]);
      onSubmitError(null);
      items.forEach((item, index) => {
        const file = nextFiles[index];
        void uploadAttachment(token, draftAttachmentSessionID, file)
          .then((attachment) => {
            setAttachments((current) =>
              current.map((currentItem) =>
                currentItem.id === item.id
                  ? { ...currentItem, attachment, name: attachment.name, size: attachment.size, status: "uploaded" }
                  : currentItem,
              ),
            );
          })
          .catch((error) => {
            console.warn("draft attachment upload failed", error);
            toast.error(draftAttachmentUploadErrorMessage(error, t));
            setAttachments((current) => {
              const failed = current.find((currentItem) => currentItem.id === item.id);
              if (failed) {
                revokeDraftAttachmentPreview(failed);
              }
              return current.filter((currentItem) => currentItem.id !== item.id);
            });
          });
      });
    },
    [onSubmitError, t, token],
  );

  const removeAttachment = useCallback((id: string) => {
    setAttachments((current) => {
      const removed = current.find((item) => item.id === id);
      if (removed) {
        revokeDraftAttachmentPreview(removed);
      }
      return current.filter((item) => item.id !== id);
    });
  }, []);
  const addUploadedAttachments = useCallback((values: Attachment[]) => {
    if (values.length === 0) {
      return;
    }
    setAttachments((current) => [
      ...current,
      ...values.map((attachment) => ({
        id: newClientID(),
        attachment,
        name: attachment.name,
        size: attachment.size,
        status: "uploaded" as const,
      })),
    ]);
  }, []);
  const addLocalFolderPaths = useCallback((paths: string[]) => {
    const folders = paths.flatMap((path) => {
      const folder = createLocalFolderPath(path);
      return folder ? [folder] : [];
    });
    if (folders.length === 0) {
      return;
    }
    setLocalFolders((current) => {
      const existing = new Set(current.map((folder) => folder.path));
      return [...current, ...folders.filter((folder) => !existing.has(folder.path))];
    });
  }, []);
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
    setLocalFolders((current) => current.filter((folder) => folder.id !== id));
  }, []);

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
      toast.error(t("composer.uploadFailed"));
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
    if (reasoningEffort && !reasoningOptions.includes(reasoningEffort)) {
      setDraftReasoningEffort("");
    }
  }, [reasoningEffort, reasoningOptions, setDraftReasoningEffort]);

  useEffect(() => {
    return () => {
      attachmentsRef.current.forEach(revokeDraftAttachmentPreview);
      if (mascotGazeRafRef.current) {
        window.cancelAnimationFrame(mascotGazeRafRef.current);
      }
      onMascotInputGazeChange(null);
    };
  }, [onMascotInputGazeChange]);

  const submitMutation = useMutation({
    mutationFn: async (value: DraftValue & { attachments: Attachment[] }) => {
      const clientMessageID = draftIDRef.current;
      const activeReasoningEffort = reasoningEffort && reasoningOptions.includes(reasoningEffort) ? reasoningEffort : "";
      if (!modelValue.provider || !modelValue.model) {
        throw new APIError(400, "no_model");
      }
      let created = await createSession(token, {
        title: "",
        provider: modelValue.provider,
        model: modelValue.model,
      });
      if (activeReasoningEffort) {
        created = await updateSession(token, created.id, { reasoningEffort: activeReasoningEffort });
      }
      cacheCreatedSession(queryClient, created);
      await navigate({
        to: "/",
        search: (prev) => {
          const next = { ...(prev as AppSearch), session: created.id };
          delete next.draft;
          return next;
        },
        replace: true,
      });
      addPendingUser({
        sessionID: created.id,
        clientMessageID,
        status: "submitting",
        text: value.text,
        attachments: value.attachments,
        createdAt: new Date().toISOString(),
      });
      startSubmittingTurn(created.id, clientMessageID);
      try {
        const result = await submitMessage(token, created.id, {
          clientMessageID,
          reasoningEffort: activeReasoningEffort || undefined,
          text: value.text,
          attachments: value.attachments,
        });
        if (result.queued || !result.turnID) {
          clearSubmittingTurn(created.id, clientMessageID);
        } else {
          acceptSubmittingTurn(created.id, clientMessageID, result.turnID);
        }
      } catch (error) {
        // 只有拿到后端明确拒绝的响应时才回滚 draft session。
        // 网络中断/响应解析失败可能发生在后端已经 accepted 之后,这类错误保留 session 交给 SSE/query 对账。
        if (isRejectedSubmit(error)) {
          clearSubmittingTurn(created.id, clientMessageID);
          removePendingUser(created.id, clientMessageID);
          try {
            await deleteSession(token, created.id);
          } catch {
            // 清理失败不覆盖真正的提交错误;下一次 sessions refetch 会对齐状态。
          }
          removeCachedSession(queryClient, created.id);
          await navigate({
            to: "/",
            search: (prev) => {
              const next = { ...(prev as AppSearch), draft: "1" };
              delete next.session;
              return next;
            },
            replace: true,
          });
        }
        throw error;
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.turns(created.id) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.messages(created.id) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(created.id) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      clearDraft();
      return created;
    },
    onSuccess: () => {
      onSubmitError(null);
      draftIDRef.current = newClientID();
      form.reset({ text: "" });
      clearDraftAttachments();
    },
    onError: (error) => {
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("composer.submitFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      if (failure.surface === "conversation") {
        onSubmitError(failure.message);
        return;
      }
      onSubmitError(null);
      toast.error(failure.message);
    },
  });

  const submitText = useCallback(
    (raw: string) => {
      const text = appendLocalFolderPaths(raw, localFolders);
      const draftAttachments = attachmentsRef.current;
      const attachmentsToSubmit = draftAttachments.flatMap((item) => (item.status === "uploaded" && item.attachment ? [item.attachment] : []));
      const hasPending = draftAttachments.some((item) => item.status === "uploading");
      if ((!text && attachmentsToSubmit.length === 0) || !modelReady || submitMutation.isPending || hasPending) {
        return;
      }
      onSubmitError(null);
      submitMutation.mutate({ text, attachments: attachmentsToSubmit });
    },
    [localFolders, modelReady, onSubmitError, submitMutation],
  );

  useEffect(() => {
    if (!quickSubmit) {
      return;
    }
    if (quickSubmitIDRef.current === quickSubmit.id) {
      return;
    }
    quickSubmitIDRef.current = quickSubmit.id;
    form.setValue("text", quickSubmit.text);
    setDraftText(quickSubmit.text);
    submitText(quickSubmit.text);
  }, [form, quickSubmit, setDraftText, submitText]);

  const submitDraft = (value: DraftValue) => submitText(value.text);
  const handleResolvedModelChange = useCallback((next: ResolvedModelSelection | null) => {
    setResolvedModel(next);
    if (next) {
      onModelValueChange({ provider: next.provider, model: next.model });
    }
  }, [onModelValueChange]);
  const setTextAreaRef = (node: HTMLTextAreaElement | null) => {
    textAreaRef.current = node;
    textField.ref(node);
  };
  const handleTextBlur = (event: FocusEvent<HTMLTextAreaElement>) => {
    textField.onBlur(event);
    onMascotInputGazeChange(null);
  };
  const handleTextChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    void textField.onChange(event);
    setDraftText(event.currentTarget.value);
    scheduleMascotInputGaze();
  };
  const handleAttachmentInputChange = (event: ChangeEvent<HTMLInputElement>) => {
    addFiles(Array.from(event.target.files || []));
    event.target.value = "";
  };
  const handleTextPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(event.clipboardData.files || []);
    if (files.length === 0) {
      return;
    }
    event.preventDefault();
    addFiles(files);
  };
  const handleTextKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      if (ime.isComposing(event)) {
        scheduleMascotInputGaze();
        return;
      }
      event.preventDefault();
      if (sendEnabled && !submitMutation.isPending) {
        void form.handleSubmit(submitDraft)();
      }
    }
    scheduleMascotInputGaze();
  };

  return (
    <>
      <form className="relative shrink-0" onSubmit={form.handleSubmit(submitDraft)}>
        <ChatColumn>
          <div className="relative rounded-3xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/60 focus-within:ring-2 focus-within:ring-ring/25">
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
                {localFolders.map((folder) => (
                  <DraftLocalFolderChip
                    key={folder.id}
                    folder={folder}
                    label={t("composer.folderLabel")}
                    removeLabel={t("composer.removeFolder")}
                    onRemove={() => removeLocalFolder(folder.id)}
                  />
                ))}
                {attachments.map((item) => (
                  <DraftAttachmentChip
                    key={item.id}
                    item={item}
                    locked={submitMutation.isPending}
                    previewIndex={attachmentPreviewIndexByID.get(item.id)}
                    removeLabel={t("composer.removeAttachment")}
                    token={token}
                    onPreview={setAttachmentPreviewIndex}
                    onRemove={() => removeAttachment(item.id)}
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
                onClick={scheduleMascotInputGaze}
                onFocus={scheduleMascotInputGaze}
                onKeyDown={handleTextKeyDown}
                onKeyUp={scheduleMascotInputGaze}
                onMouseUp={scheduleMascotInputGaze}
                onPaste={handleTextPaste}
                onSelect={scheduleMascotInputGaze}
              />
            </div>
            <div className="flex min-w-0 items-center gap-1 px-2 pb-2">
              <ComposerAddMenu
                attachFolderLabel={t("composer.attachFolder")}
                attachLabel={t("composer.attach")}
                menuTitle={t("composer.addMenuTitle")}
                pickingFolder={pickingLocalFolder}
                onAttachFiles={() => fileInputRef.current?.click()}
                onAttachFolder={() => void pickLocalFolder()}
              />
              <ModelReasoningPicker
                className="ml-auto min-w-0"
                token={token}
                value={modelValue}
                reasoningValue={reasoningEffort}
                onChange={onModelValueChange}
                onAfterClose={focusTextarea}
                onReasoningChange={setDraftReasoningEffort}
                onResolvedChange={handleResolvedModelChange}
              />
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    aria-label={t("composer.send")}
                    className="rounded-full disabled:bg-control-disabled disabled:text-background disabled:opacity-100 disabled:shadow-none"
                    disabled={!sendEnabled || submitMutation.isPending}
                    size="icon"
                    type="submit"
                    variant={sendEnabled ? "default" : "secondary"}
                  >
                    {submitMutation.isPending ? <Loader2 className="animate-spin" /> : <ArrowUp />}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("composer.send")}</TooltipContent>
              </Tooltip>
            </div>
          </div>
        </ChatColumn>
      </form>
      <ImageLightbox images={attachmentPreviewItems} openIndex={attachmentPreviewIndex} onOpenIndexChange={setAttachmentPreviewIndex} />
    </>
  );
}

function isDraftModelAvailable(profiles: ProviderProfile[], value: DraftModelValue): boolean {
  if (!value.provider || !value.model) {
    return false;
  }
  const profile = profiles.find((item) => item.id === value.provider);
  return Boolean(profile?.models.some((model) => model.id === value.model));
}

function cacheCreatedSession(queryClient: ReturnType<typeof useQueryClient>, created: Session) {
  queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
    if (!previous) {
      return { sessions: [created] };
    }
    return { sessions: [created, ...previous.sessions.filter((session) => session.id !== created.id)] };
  });
}

function removeCachedSession(queryClient: ReturnType<typeof useQueryClient>, sessionID: string) {
  queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
    if (!previous) {
      return previous;
    }
    return { sessions: previous.sessions.filter((session) => session.id !== sessionID) };
  });
}

function mascotGazePointFromCaret(textArea: HTMLTextAreaElement): MascotGazePoint {
  return getTextAreaCaretClientPoint(textArea);
}

function DraftLocalFolderChip({
  folder,
  label,
  removeLabel,
  onRemove,
}: {
  folder: LocalFolderPath;
  label: string;
  removeLabel: string;
  onRemove: () => void;
}) {
  return (
    <div
      className="inline-flex min-w-0 max-w-full items-center gap-2 rounded-md border border-border/70 bg-muted/40 px-2 py-1 text-xs leading-5 text-muted-foreground"
      title={folder.path}
    >
      <FolderOpen className="size-3 shrink-0" />
      <span className="shrink-0 text-muted-foreground/70">{label}</span>
      <span className="min-w-0 truncate font-medium text-foreground">{folder.name}</span>
      <span className="min-w-0 max-w-44 truncate font-mono text-muted-foreground/70">{formatLocalFolderLabel(folder.path)}</span>
      <Button
        aria-label={removeLabel}
        className="-mr-1 size-5 bg-transparent hover:bg-background/70"
        size="icon-xs"
        type="button"
        variant="ghost"
        onClick={onRemove}
      >
        <X className="size-3" />
      </Button>
    </div>
  );
}

function DraftAttachmentChip({
  item,
  locked,
  previewIndex,
  removeLabel,
  token,
  onPreview,
  onRemove,
}: {
  item: DraftAttachment;
  locked: boolean;
  previewIndex?: number;
  removeLabel: string;
  token: string;
  onPreview: (index: number) => void;
  onRemove: () => void;
}) {
  const src = draftAttachmentImageSource(item, token);
  const image = src && isImageAttachmentLike(item.attachment?.mime || item.file?.type, item.name);
  const busy = item.status === "uploading";
  if (image) {
    return (
      <div
        className={cn(
          "group relative h-16 w-20 overflow-hidden rounded-md border bg-muted/40",
          item.status === "error" ? "border-destructive/40" : "border-border/70",
        )}
        title={`${item.name} ${formatAttachmentSize(item.size)}`}
      >
        <button
          aria-label={item.name}
          className="block h-full w-full"
          disabled={previewIndex === undefined}
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
        <Button
          aria-label={removeLabel}
          className="absolute top-1 right-1 z-10 size-5 bg-background/85 text-foreground shadow-sm hover:bg-background"
          disabled={locked}
          size="icon-xs"
          type="button"
          variant="ghost"
          onClick={onRemove}
        >
          <X className="size-3" />
        </Button>
      </div>
    );
  }
  return (
    <div
      className={cn(
        "inline-flex min-w-0 max-w-full items-center gap-1 rounded-md border px-2 py-1 text-xs leading-5",
        item.status === "error" ? "border-destructive/40 bg-destructive/10 text-destructive" : "border-border/70 bg-muted/40 text-muted-foreground",
      )}
      title={`${item.name} ${formatAttachmentSize(item.size)}`}
    >
      {busy ? <Loader2 className="size-3 shrink-0 animate-spin" /> : <Paperclip className="size-3 shrink-0" />}
      <span className="min-w-0 truncate">{item.name}</span>
      <span className="shrink-0 text-muted-foreground/70">{formatAttachmentSize(item.size)}</span>
      <Button
        aria-label={removeLabel}
        className="-mr-1 size-5 bg-transparent hover:bg-background/70"
        disabled={locked}
        size="icon-xs"
        type="button"
        variant="ghost"
        onClick={onRemove}
      >
        <X className="size-3" />
      </Button>
    </div>
  );
}

function DraftDropOverlay({ active }: { active: boolean }) {
  return (
    <div
      className={
        "pudding-drop-overlay pointer-events-none absolute inset-0 z-30 bg-primary/[0.055] backdrop-blur-[1px] transition-opacity dark:bg-primary/[0.08] " +
        (active ? "opacity-100" : "opacity-0")
      }
    >
      <div className="absolute inset-0 flex items-center justify-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-background/90 text-primary shadow-sm ring-1 ring-border/80">
          <Upload className="h-5 w-5" strokeWidth={1.9} />
        </div>
      </div>
    </div>
  );
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

function isImageAttachmentLike(mime: string | undefined, name: string) {
  const cleaned = (mime || "").toLowerCase();
  if (cleaned.startsWith("image/") && cleaned !== "image/svg+xml") {
    return true;
  }
  return /\.(png|jpe?g|gif|webp)$/i.test(name);
}

function draftAttachmentImageSource(item: DraftAttachment, token: string) {
  return item.previewURL || attachmentResourceURL(item.attachment, token);
}

function revokeDraftAttachmentPreview(item: DraftAttachment) {
  if (item.previewURL) {
    URL.revokeObjectURL(item.previewURL);
  }
}

function dataTransferHasFiles(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types || []).includes("Files");
}

function draftAttachmentUploadErrorMessage(error: unknown, t: (key: string) => string) {
  if (error instanceof APIError && error.status === 404) {
    return t("composer.draftUploadUnavailable");
  }
  return t("composer.uploadFailed");
}
