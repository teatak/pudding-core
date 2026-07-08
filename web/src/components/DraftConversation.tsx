import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowUp, CircleAlert, FileText, FolderOpen, Loader2, Pause, Play, Upload, X } from "lucide-react";
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
  bindAudioInput,
  bindAudioOutput,
  captureDesktopPhoto,
  captureDesktopScreenshot,
  createSession,
  deleteSession,
  getAudioBindings,
  listApps,
  listSkills,
  listProviders,
  submitMessage,
  updateSession,
  uploadAttachment,
  revealDesktopPath,
  type Attachment,
  type ContentPart,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { ComposerAddButton } from "@/components/ComposerAddMenu";
import { buildComposerMentionReferences } from "@/components/composerMentionData";
import { ComposerMentionMenu } from "@/components/ComposerMentionMenu";
import { useComposerMentions } from "@/components/useComposerMentions";
import { ImageLightbox, type ImageLightboxItem } from "@/components/ImageLightbox";
import { Mascot, type MascotGaze, type MascotGazePoint } from "@/components/Mascot";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { AudioControlButtons } from "@/components/SessionAudioControls";
import { type ResolvedModelSelection } from "@/lib/modelSelection";
import { ProviderProfileEditorDialog } from "@/components/ProviderProfileEditorDialog";
import { ProviderCustomCard, ProviderPresetCreateDialog, ProviderPresetGrid } from "@/components/ProviderPresetCreateDialog";
import { reasoningEffortOptionsForSelection } from "@/components/ReasoningEffortChip";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useComposerSelectionGuard } from "@/hooks/useComposerSelectionGuard";
import { useImeCompositionGuard } from "@/hooks/useImeCompositionGuard";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { createPastedTextAttachmentFile, shouldAttachPastedText } from "@/lib/clipboardTextAttachment";
import { newClientID } from "@/lib/id";
import {
  createLocalFolderPath,
  droppedLocalItemsFromDataTransfer,
  type DroppedLocalItems,
  pickLocalFolderPaths,
  type LocalFolderPath,
} from "@/lib/localFolders";
import type { AppSearch } from "@/lib/route";
import { fetchStarterPromptCatalog, localizeStarterPrompts, STARTER_PROMPTS_CACHE_TTL_MS } from "@/lib/starterPrompts";
import { getSubmitFailure } from "@/lib/submitFailure";
import { buildDraftSubmitParts, orderedDraftItems } from "@/lib/submitParts";
import { getTextAreaCaretClientPoint } from "@/lib/textCaret";
import { cn } from "@/lib/utils";
import { getOrderedProviderPresets, type ProviderPreset } from "@/provider/presets";
import { useDraftStore, type DraftAttachment, type DraftModelValue } from "@/state/draftStore";
import { useOverlayStore } from "@/state/overlayStore";

const draftSchema = z.object({
  text: z.string(),
});

type DraftValue = z.infer<typeof draftSchema>;
type DraftDroppedFilesBatch = DroppedLocalItems & {
  attachments?: Attachment[];
  failedFiles?: string[];
  failedFileCount?: number;
  nonce: number;
};
type QuickSubmit = { id: number; text: string };

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
  const starterPromptsQuery = useQuery({
    queryKey: queryKeys.starterPrompts(),
    queryFn: () => fetchStarterPromptCatalog(),
    retry: false,
    staleTime: STARTER_PROMPTS_CACHE_TTL_MS,
  });
  const profiles = providersQuery.data?.providers || [];
  const hasConfiguredModel = profiles.some((profile) => profile.models.some((model) => model.id));
  const draftModelIsValid = providersQuery.isSuccess && isDraftModelAvailable(profiles, modelValue);
  const composerModelValue = draftModelIsValid ? modelValue : emptyDraftModel;
  const showPresetSetup = providersQuery.isSuccess && !hasConfiguredModel;
  const starterPrompts = useMemo(
    () => localizeStarterPrompts(starterPromptsQuery.data?.items || [], locale),
    [locale, starterPromptsQuery.data?.items],
  );
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
          {!showPresetSetup && starterPrompts.length > 0 ? (
            <ChatColumn className="pudding-draft-suggestions mt-8 flex flex-wrap items-center justify-center gap-2 px-2">
              {starterPrompts.map((item, index) => {
                return (
                  <button
                    key={item.id}
                    className="rounded-full border border-input bg-background px-4 py-1.5 text-[13px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    type="button"
                    onClick={() => setQuickSubmit({ id: Date.now() + index, text: item.prompt })}
                  >
                    {item.label}
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
  const attachments = useDraftStore((state) => state.attachments);
  const localFolders = useDraftStore((state) => state.localFolders);
  const partOrder = useDraftStore((state) => state.partOrder);
  const setDraftAttachments = useDraftStore((state) => state.setAttachments);
  const setDraftLocalFolders = useDraftStore((state) => state.setLocalFolders);
  const setDraftPartOrder = useDraftStore((state) => state.setPartOrder);
  const setDraftText = useDraftStore((state) => state.setText);
  const clearDraft = useDraftStore((state) => state.clear);
  const [resolvedModel, setResolvedModel] = useState<ResolvedModelSelection | null>(null);
  const [draftReasoningEffort, setDraftReasoningEffortValue] = useState("");
  const [draftReasoningModelKey, setDraftReasoningModelKey] = useState("");
  const [attachmentPreviewIndex, setAttachmentPreviewIndex] = useState<number | null>(null);
  const [capturingPhoto, setCapturingPhoto] = useState(false);
  const [capturingScreenshot, setCapturingScreenshot] = useState(false);
  const [textFocused, setTextFocused] = useState(false);
  const [pickingAttachment, setPickingAttachment] = useState(false);
  const [pickingLocalFolder, setPickingLocalFolder] = useState(false);
  const [draftVoiceInputActive, setDraftVoiceInputActive] = useState(false);
  const [draftVoiceOutputActive, setDraftVoiceOutputActive] = useState(false);
  const [draftVoiceOutputPending, setDraftVoiceOutputPending] = useState(false);
  const [draftVoiceSession, setDraftVoiceSession] = useState<Session | null>(null);
  const draftIDRef = useRef<string>(newClientID());
  const draftVoiceInputActiveRef = useRef(false);
  const draftVoiceOutputActiveRef = useRef(false);
  const draftVoiceRevealedRef = useRef(false);
  const draftVoiceSessionRef = useRef<Session | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const lastDroppedFilesNonceRef = useRef(0);
  const quickSubmitIDRef = useRef<number | null>(null);
  const textAreaRef = useRef<HTMLTextAreaElement | null>(null);
  const mascotGazeRafRef = useRef(0);
  const selectionGuardRef = useComposerSelectionGuard<HTMLDivElement>();
  const form = useForm<DraftValue>({
    resolver: zodResolver(draftSchema),
    defaultValues: { text: draftText },
  });
  const watchedText = form.watch("text");
  const uploadedAttachments = attachments.flatMap((item) => (item.status === "uploaded" && item.attachment ? [item.attachment] : []));
  const hasPendingAttachments = attachments.some((item) => item.status === "uploading");
  const canSend = Boolean(watchedText.trim() || uploadedAttachments.length || localFolders.length) && !hasPendingAttachments;
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
      setDraftText(nextText);
      if (textAreaRef.current && textAreaRef.current.value !== nextText) {
        textAreaRef.current.value = nextText;
      }
    },
    [form, setDraftText],
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
  const mentionMenuOpen = textFocused && mentions.open;
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
  const draftVoiceSessionID = draftVoiceSession?.id;
  useSessionEvents(draftVoiceSessionID, token);
  const draftVoiceBindingsQuery = useQuery({
    queryKey: queryKeys.audioBindings(),
    queryFn: () => {
      if (!draftVoiceSessionID) {
        throw new APIError(400, "no_session");
      }
      return getAudioBindings(token, draftVoiceSessionID);
    },
    enabled: Boolean(token && draftVoiceSessionID),
    staleTime: 30_000,
  });
  const draftVoiceBindings = draftVoiceBindingsQuery.data?.bindings;
  const draftVoiceInputLevel =
    draftVoiceInputActive && draftVoiceBindings?.inputOwner === draftVoiceSessionID ? draftVoiceBindings?.inputLevel ?? 0 : 0;
  const draftVoiceHasInput = useOverlayStore((state) => {
    if (!draftVoiceSessionID) {
      return false;
    }
    return Boolean(
      state.runningTurns[draftVoiceSessionID] ||
        state.turnPhases[draftVoiceSessionID] ||
        (state.pendingUsers[draftVoiceSessionID] || []).length,
    );
  });
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

  const cleanupDraftVoiceSession = useCallback(
    async (sessionID: string) => {
      await Promise.all([
        bindAudioInput(token, sessionID, false).catch(() => undefined),
        bindAudioOutput(token, sessionID, false).catch(() => undefined),
      ]);
      await deleteSession(token, sessionID).catch(() => undefined);
      removeCachedSession(queryClient, sessionID);
      await queryClient.invalidateQueries({ queryKey: queryKeys.audioBindings() });
    },
    [queryClient, token],
  );

  const revealDraftVoiceSession = useCallback(
    async (created: Session) => {
      if (draftVoiceRevealedRef.current) {
        return;
      }
      draftVoiceRevealedRef.current = true;
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
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.turns(created.id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.messages(created.id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(created.id) }),
      ]);
    },
    [navigate, queryClient],
  );

  const draftVoiceInputMutation = useMutation({
    mutationFn: async (enabled: boolean) => {
      const current = draftVoiceSessionRef.current;
      if (!enabled) {
        setDraftVoiceInputActive(false);
        draftVoiceInputActiveRef.current = false;
        if (!current) {
          return null;
        }
        const result = await bindAudioInput(token, current.id, false);
        queryClient.setQueryData(queryKeys.audioBindings(), { bindings: result.bindings });
        if (!draftVoiceOutputActiveRef.current) {
          await cleanupDraftVoiceSession(current.id);
          draftVoiceSessionRef.current = null;
          setDraftVoiceSession(null);
        }
        return current;
      }
      const activeReasoningEffort = reasoningEffort && reasoningOptions.includes(reasoningEffort) ? reasoningEffort : "";
      if (!modelValue.provider || !modelValue.model) {
        throw new APIError(400, "no_model");
      }
      let created = current;
      if (!created) {
        created = await createSession(token, {
          title: "",
          provider: modelValue.provider,
          model: modelValue.model,
        });
        if (activeReasoningEffort) {
          created = await updateSession(token, created.id, { reasoningEffort: activeReasoningEffort });
        }
        draftVoiceRevealedRef.current = false;
        draftVoiceSessionRef.current = created;
        setDraftVoiceSession(created);
      }
      draftVoiceInputActiveRef.current = true;
      setDraftVoiceInputActive(true);
      const result = await bindAudioInput(token, created.id, true);
      queryClient.setQueryData(queryKeys.audioBindings(), { bindings: result.bindings });
      if (draftVoiceOutputActiveRef.current) {
        const outputResult = await bindAudioOutput(token, created.id, true);
        queryClient.setQueryData(queryKeys.audioBindings(), { bindings: outputResult.bindings });
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.audioBindings() });
      return created;
    },
    onError: (error, enabled) => {
      if (enabled) {
        draftVoiceInputActiveRef.current = false;
        setDraftVoiceInputActive(false);
        const current = draftVoiceSessionRef.current;
        if (current && !draftVoiceRevealedRef.current) {
          draftVoiceSessionRef.current = null;
          setDraftVoiceSession(null);
          void cleanupDraftVoiceSession(current.id);
        }
      }
      const failure = getSubmitFailure(error, {
        noModel: t("composer.noModel"),
        providerConfig: t("composer.providerConfig"),
        submitFailed: t("voice.inputFailed"),
        turnRunning: t("composer.turnRunning"),
      });
      toast.error(failure.message);
    },
  });
  const handleDraftVoiceInputClick = useCallback(() => {
    if (!draftVoiceInputActiveRef.current && !modelReady) {
      toast.error(t("composer.noModel"));
      return;
    }
    draftVoiceInputMutation.mutate(!draftVoiceInputActiveRef.current);
  }, [draftVoiceInputMutation, modelReady, t]);
  const handleDraftVoiceOutputClick = useCallback(() => {
    const nextActive = !draftVoiceOutputActiveRef.current;
    const current = draftVoiceSessionRef.current;
    draftVoiceOutputActiveRef.current = nextActive;
    setDraftVoiceOutputActive(nextActive);
    if (!current) {
      return;
    }
    setDraftVoiceOutputPending(true);
    void bindAudioOutput(token, current.id, nextActive)
      .then(async (result) => {
        queryClient.setQueryData(queryKeys.audioBindings(), { bindings: result.bindings });
        await queryClient.invalidateQueries({ queryKey: queryKeys.audioBindings() });
        if (!nextActive && !draftVoiceInputActiveRef.current) {
          await cleanupDraftVoiceSession(current.id);
          draftVoiceSessionRef.current = null;
          setDraftVoiceSession(null);
        }
      })
      .catch(() => {
        draftVoiceOutputActiveRef.current = !nextActive;
        setDraftVoiceOutputActive(!nextActive);
        toast.error(t("voice.outputFailed"));
      })
      .finally(() => setDraftVoiceOutputPending(false));
  }, [cleanupDraftVoiceSession, queryClient, t, token]);
  const sendEnabled = canSend && modelReady && !mentionMenuOpen && !draftVoiceInputMutation.isPending;

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

  const clearDraftAttachments = useCallback(() => {
    attachments.forEach(revokeDraftAttachmentPreview);
    setDraftAttachments([]);
    setDraftLocalFolders([]);
    setDraftPartOrder([]);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }, [attachments, setDraftAttachments, setDraftLocalFolders, setDraftPartOrder]);

  const addFiles = useCallback(
    (files: File[], options?: { origin?: "temp" }) => {
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
      setDraftAttachments((current) => [...current, ...items]);
      setDraftPartOrder((current) => [...current, ...items.map((item) => ({ type: "attachment" as const, id: item.id }))]);
      onSubmitError(null);
      items.forEach((item, index) => {
        const file = nextFiles[index];
        void uploadAttachment(token, draftAttachmentSessionID, file, options?.origin ? { origin: options.origin } : undefined)
          .then((attachment) => {
            setDraftAttachments((current) =>
              current.map((currentItem) =>
                currentItem.id === item.id
                  ? { ...currentItem, attachment, name: attachment.name, size: attachment.size, status: "uploaded" }
                  : currentItem,
              ),
            );
          })
          .catch((error) => {
            console.warn("draft attachment upload failed", error);
            toast.error(uploadFailedMessage(item.name, draftAttachmentUploadErrorMessage(error, t)));
            setDraftAttachments((current) => {
              const failed = current.find((currentItem) => currentItem.id === item.id);
              if (failed) {
                revokeDraftAttachmentPreview(failed);
              }
              return current.filter((currentItem) => currentItem.id !== item.id);
            });
            setDraftPartOrder((current) => current.filter((orderItem) => orderItem.type !== "attachment" || orderItem.id !== item.id));
          });
      });
    },
    [onSubmitError, setDraftAttachments, setDraftPartOrder, t, token],
  );

  const removeAttachment = useCallback((id: string) => {
    setDraftAttachments((current) => {
      const removed = current.find((item) => item.id === id);
      if (removed) {
        revokeDraftAttachmentPreview(removed);
      }
      return current.filter((item) => item.id !== id);
    });
    setDraftPartOrder((current) => current.filter((item) => item.type !== "attachment" || item.id !== id));
  }, [setDraftAttachments, setDraftPartOrder]);
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
    setDraftAttachments((current) => [...current, ...items]);
    setDraftPartOrder((current) => [...current, ...items.map((item) => ({ type: "attachment" as const, id: item.id }))]);
  }, [setDraftAttachments, setDraftPartOrder]);
  const captureScreenshot = useCallback(async () => {
    if (capturingScreenshot) {
      return;
    }
    setCapturingScreenshot(true);
    try {
      addUploadedAttachments(await captureDesktopScreenshot(token, draftAttachmentSessionID));
      onSubmitError(null);
      window.requestAnimationFrame(() => textAreaRef.current?.focus({ preventScroll: true }));
    } catch (error) {
      if (error instanceof APIError && error.code === "screenshot_cancelled") {
        return;
      }
      toast.error(t("composer.screenshotFailed"));
    } finally {
      setCapturingScreenshot(false);
    }
  }, [addUploadedAttachments, capturingScreenshot, onSubmitError, t, token]);
  const capturePhoto = useCallback(async () => {
    if (capturingPhoto) {
      return;
    }
    setCapturingPhoto(true);
    try {
      addUploadedAttachments([await captureDesktopPhoto(token, draftAttachmentSessionID)]);
      onSubmitError(null);
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
  }, [addUploadedAttachments, capturingPhoto, onSubmitError, t, token]);
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
    setDraftLocalFolders((current) => {
      const currentPaths = new Set(current.map((folder) => folder.path));
      return [...current, ...nextFolders.filter((folder) => !currentPaths.has(folder.path))];
    });
    setDraftPartOrder((current) => [...current, ...nextFolders.map((folder) => ({ type: "local_folder" as const, id: folder.id }))]);
  }, [localFolders, setDraftLocalFolders, setDraftPartOrder]);
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
    setDraftLocalFolders((current) => current.filter((folder) => folder.id !== id));
    setDraftPartOrder((current) => current.filter((item) => item.type !== "local_folder" || item.id !== id));
  }, [setDraftLocalFolders, setDraftPartOrder]);
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

  useEffect(() => {
    if (reasoningEffort && !reasoningOptions.includes(reasoningEffort)) {
      setDraftReasoningEffort("");
    }
  }, [reasoningEffort, reasoningOptions, setDraftReasoningEffort]);

  useEffect(() => {
    draftVoiceInputActiveRef.current = draftVoiceInputActive;
  }, [draftVoiceInputActive]);

  useEffect(() => {
    draftVoiceOutputActiveRef.current = draftVoiceOutputActive;
  }, [draftVoiceOutputActive]);

  useEffect(() => {
    draftVoiceSessionRef.current = draftVoiceSession;
  }, [draftVoiceSession]);

  useEffect(() => {
    if (!draftVoiceSession || !draftVoiceHasInput) {
      return;
    }
    void revealDraftVoiceSession(draftVoiceSession);
  }, [draftVoiceHasInput, draftVoiceSession, revealDraftVoiceSession]);

  useEffect(() => {
    return () => {
      const current = draftVoiceSessionRef.current;
      if (!current || draftVoiceRevealedRef.current) {
        return;
      }
      void cleanupDraftVoiceSession(current.id);
    };
  }, [cleanupDraftVoiceSession]);

  useEffect(() => {
    return () => {
      if (mascotGazeRafRef.current) {
        window.cancelAnimationFrame(mascotGazeRafRef.current);
      }
      onMascotInputGazeChange(null);
    };
  }, [onMascotInputGazeChange]);

  const submitMutation = useMutation({
    mutationFn: async (value: DraftValue & { parts: ContentPart[] }) => {
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
        parts: value.parts,
        createdAt: new Date().toISOString(),
      });
      startSubmittingTurn(created.id, clientMessageID);
      try {
        const result = await submitMessage(token, created.id, {
          clientMessageID,
          reasoningEffort: activeReasoningEffort || undefined,
          text: value.text,
          parts: value.parts,
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
      const text = raw.trim();
      const attachmentItemsToSubmit = attachments.filter((item) => item.status === "uploaded" && item.attachment);
      const attachmentsToSubmit = attachmentItemsToSubmit.flatMap((item) => (item.attachment ? [item.attachment] : []));
      const localFoldersToSubmit = localFolders;
      const hasPending = attachments.some((item) => item.status === "uploading");
      if ((!text && attachmentsToSubmit.length === 0 && localFoldersToSubmit.length === 0) || !modelReady || submitMutation.isPending || hasPending) {
        return;
      }
      onSubmitError(null);
      submitMutation.mutate({
        text,
        parts: buildDraftSubmitParts(text, attachmentItemsToSubmit, localFoldersToSubmit, partOrder),
      });
    },
    [attachments, localFolders, modelReady, onSubmitError, partOrder, submitMutation],
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
    setTextFocused(false);
    onMascotInputGazeChange(null);
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
    const nextText = event.currentTarget.value;
    void textField.onChange(event);
    setDraftText(nextText);
    mentions.notifyChange(nextText, previousText, event.currentTarget.selectionStart);
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
    addFiles([createPastedTextAttachmentFile(text)], { origin: "temp" });
  };
  const handleTextKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "@" && !event.metaKey && !event.ctrlKey && !event.altKey) {
      mentions.notifyCursor(event.currentTarget.selectionStart + 1);
    }
    if (mentions.onKeyDown(event)) {
      scheduleMascotInputGaze();
      return;
    }
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
          <div ref={selectionGuardRef} className="relative">
            {mentionMenuOpen ? (
              <ComposerMentionMenu
                align="start"
                references={mentions.filtered}
                query={mentions.query}
                selectedIndex={mentions.activeIndex}
                onHover={mentions.setActiveIndex}
                onSelect={mentions.select}
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
                {orderedDraftItems(attachments, localFolders, partOrder).map((orderedItem) =>
                  orderedItem.type === "attachment" ? (
                    <DraftAttachmentChip
                      key={`attachment:${orderedItem.item.id}`}
                      item={orderedItem.item}
                      locked={submitMutation.isPending}
                      previewIndex={attachmentPreviewIndexByID.get(orderedItem.item.id)}
                      removeLabel={t("composer.removeAttachment")}
                      token={token}
                      onPreview={setAttachmentPreviewIndex}
                      onRevealSource={revealLocalPath}
                      onRemove={() => removeAttachment(orderedItem.item.id)}
                    />
                  ) : (
                    <DraftLocalFolderChip
                      key={`folder:${orderedItem.item.id}`}
                      folder={orderedItem.item}
                      label={t("composer.folderLabel")}
                      removeLabel={t("composer.removeFolder")}
                      onReveal={() => revealLocalPath(orderedItem.item.path)}
                      onRemove={() => removeLocalFolder(orderedItem.item.id)}
                    />
                  ),
                )}
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
              <AudioControlButtons
                controlsLabel={t("voice.controls")}
                inputActive={draftVoiceInputActive}
                inputLabel={draftVoiceInputActive ? t("voice.inputOn") : t("voice.inputOff")}
                inputLevel={draftVoiceInputLevel}
                inputBusy={draftVoiceInputMutation.isPending}
                inputPending={draftVoiceInputMutation.isPending && draftVoiceInputMutation.variables === true}
                outputActive={draftVoiceOutputActive}
                outputLabel={draftVoiceOutputActive ? t("voice.outputOn") : t("voice.outputOff")}
                outputPending={draftVoiceOutputPending}
                onInputClick={handleDraftVoiceInputClick}
                onOutputClick={handleDraftVoiceOutputClick}
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

function DraftAttachmentChip({
  item,
  locked,
  previewIndex,
  removeLabel,
  token,
  onPreview,
  onRevealSource,
  onRemove,
}: {
  item: DraftAttachment;
  locked: boolean;
  previewIndex?: number;
  removeLabel: string;
  token: string;
  onPreview: (index: number) => void;
  onRevealSource: (path: string) => void;
  onRemove: () => void;
}) {
  const src = draftAttachmentImageSource(item, token);
  const image = src && isImageAttachmentLike(item.attachment?.mime || item.file?.type, item.name);
  const audio = isAudioAttachmentLike(item.attachment?.mime || item.file?.type, item.name);
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
        <button
          aria-label={removeLabel}
          className="absolute top-1.5 right-1.5 z-10 grid size-5 place-items-center rounded-full border border-black/10 bg-white text-black shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none disabled:opacity-50"
          disabled={locked}
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
          <DraftAudioPreviewButton label={item.name} src={draftAttachmentImageSource(item, token)} />
        ) : (
          <FileText className="size-4" strokeWidth={1.8} />
        )}
      </span>
      <span className="inline-flex min-w-0 items-center gap-1.5 whitespace-nowrap">
        <span className="truncate font-medium leading-5 text-foreground">{item.name}</span>
        <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">{attachmentKindLabel(item.name, item.attachment?.mime || item.file?.type)}</span>
      </span>
      <button
        aria-label={removeLabel}
        className="absolute top-2 right-1.5 grid size-5 place-items-center rounded-full bg-foreground text-background shadow-sm focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none disabled:opacity-50"
        disabled={locked}
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

function DraftAudioPreviewButton({ label, src }: { label: string; src: string }) {
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

function uploadFailedMessage(name: string, message: string) {
  return `${name}: ${message}`;
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
